package catalog

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/platform/gz"
	"pelokalan/internal/platform/store"
	"pelokalan/internal/source"
	"pelokalan/pkg/gitpatch"
	"pelokalan/pkg/po"
)

// SetProjectFiles gives the service access to project files, for git patches against the repository's files
func (s *Service) SetProjectFiles(r source.Repository) { s.projectFiles = r }

// SetRepoPath stores a file's path in its module group's git repository
func (s *Service) SetRepoPath(ctx context.Context, user *models.User, c *models.Catalog, path string) error {
	if !c.WritableBy(user.ID) {
		return apperr.ErrForbidden
	}
	c.RepoPath = path
	return s.catalogs.SetRepoPath(ctx, c.ID, path)
}

// GitPatchResult is a patch of a member's files for their module group's repository
type GitPatchResult struct {
	Data     []byte
	Filename string
	Files    int // files with changes
	Entries  int // entries changed
	Repo     string
	Branch   string
}

type poState struct {
	tr    []string
	fuzzy bool
}

// reference is what the member started from: the file as taken, or as last updated from upstream
// (the newest "initial" or "from upstream" version); nil when the file has no such version
func (s *Service) reference(ctx context.Context, c *models.Catalog) map[string]poState {
	return s.referenceAt(ctx, c, nil)
}

// referenceAt is reference as it was at time at (nil = now): the newest such version made by then, else the
// oldest one (a file taken after at)
func (s *Service) referenceAt(ctx context.Context, c *models.Catalog, at *time.Time) map[string]poState {
	list, err := s.commits.List(ctx, c.ID) // newest first
	if err != nil {
		return nil
	}
	var refs []*models.Commit
	for _, cm := range list {
		if cm.Kind == models.CommitInitial || strings.HasPrefix(cm.Message, CommitMsgFromUpstream) {
			refs = append(refs, cm)
		}
	}
	if at != nil && len(refs) > 1 {
		pick := refs[len(refs)-1] // the oldest
		for _, cm := range refs {
			if !cm.CreatedAt.After(*at) {
				pick = cm
				break
			}
		}
		refs = []*models.Commit{pick}
	}
	for _, cm := range refs {
		data, err := s.commits.Snapshot(ctx, cm.ID)
		if err != nil {
			return nil
		}
		cat, err := po.ParseBytes(data)
		if err != nil {
			return nil
		}
		out := make(map[string]poState, len(cat.Entries))
		for _, e := range cat.Entries {
			out[e.Key()] = poState{e.Translation, e.Fuzzy}
		}
		return out
	}
	return nil
}

// GitPatch builds one `git am` patch with the member's own changes to repository files (files with a
// RepoPath, owned by user). Only entries the member changed in Pelokalan since taking or last updating
// the file go in, against the repository file as last synced, so newer translations in the repository
// are never reverted. AI drafts nobody reviewed and emptied translations are left out. Files
// changes (0 = nothing to send) tells whether there is anything.
// TranslatorCredit is how a translator is credited in the header of files sent to a repository: their credit
// identity (profile "Identitas penerjemah", else the account) and this year
func TranslatorCredit(u *models.User) *gitpatch.Credit {
	if u == nil {
		return nil
	}
	return &gitpatch.Credit{Name: u.CreditName(), Email: u.CreditEmail(), Year: time.Now().Year()}
}

func (s *Service) GitPatch(ctx context.Context, user *models.User, p *models.Project, catalogs []*models.Catalog, projectURL string) (*GitPatchResult, error) {
	if s.projectFiles == nil {
		return nil, apperr.ErrNotFound
	}
	res := &GitPatchResult{}
	var diffs []*gitpatch.FileDiff
	var lines []string
	for _, c := range catalogs {
		if c.RepoPath == "" || !c.WritableBy(user.ID) {
			continue
		}
		pf, g, err := s.projectFiles.ProjectFileByRepoPath(ctx, p.ID, c.RepoPath)
		if errors.Is(err, store.ErrNotFound) {
			continue
		} else if err != nil {
			return nil, err
		}
		base, err := gz.Decompress(pf.Data)
		if err != nil {
			return nil, err
		}
		want, err := s.ChangedTargets(ctx, c, nil)
		if err != nil {
			return nil, err
		}
		d := gitpatch.DiffCredit(c.RepoPath, base, want, TranslatorCredit(user))
		if d == nil {
			continue
		}
		diffs = append(diffs, d)
		res.Entries += d.Changed
		res.Repo, res.Branch = g.GitRepo, g.GitBranch
		lines = append(lines, fmt.Sprintf("- %s (%d)", c.RepoPath, d.Changed))
	}
	res.Files = len(diffs)
	if res.Files == 0 {
		return res, nil
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Path < diffs[j].Path })
	sort.Strings(lines)
	subject := "Update translation of " + diffs[0].Path
	if len(diffs) > 1 {
		subject = fmt.Sprintf("Update translation of %d files", len(diffs))
	}
	body := strings.Join(lines, "\n") + "\n\nExported from Pelokalan: " + projectURL
	now := time.Now()
	res.Data = gitpatch.FormatPatch(gitpatch.Commit{AuthorName: user.CreditName(), AuthorEmail: user.CreditEmail(),
		Date: now, Subject: subject, Body: body}, diffs)
	name := res.Repo
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	res.Filename = fmt.Sprintf("%s-%s-pelokalan-%s.patch", name, strings.ReplaceAll(res.Branch, "/", "-"), now.Format("20060102-1504"))
	return res, nil
}

// ChangedTargets are the entries the member changed in Pelokalan since taking or last updating the file
// (the reference version), as patch targets; unreviewed AI drafts and empty strings stay out. Entries
// whose state equals the one in sent (a repository file as Pelokalan last committed it; nil = none)
// are left out too, so strings already sent never overwrite newer repository translations.
func (s *Service) ChangedTargets(ctx context.Context, c *models.Catalog, sent []byte) (map[string]gitpatch.Target, error) {
	entries, err := s.catalogs.ListEntries(ctx, c.ID, false)
	if err != nil {
		return nil, err
	}
	ref := s.reference(ctx, c)
	var done map[string]poState
	if sent != nil {
		if cat, err := po.ParseBytes(sent); err == nil {
			done = make(map[string]poState, len(cat.Entries))
			for _, e := range cat.Entries {
				done[e.Key()] = poState{e.Translation, e.Fuzzy}
			}
		}
	}
	want := map[string]gitpatch.Target{}
	for _, e := range entries {
		if e.Obsolete || e.IsAI() || !e.IsTranslated() { // unreviewed AI drafts and empty strings stay out
			continue
		}
		pe := e.ToPO()
		st := poState{e.MsgStr, e.Fuzzy}
		if r, ok := ref[pe.Key()]; ok && sameStrings(r.tr, st.tr) && r.fuzzy == st.fuzzy {
			continue // not changed in Pelokalan: the repository's version stands
		}
		if r, ok := done[pe.Key()]; ok && sameStrings(r.tr, st.tr) && r.fuzzy == st.fuzzy {
			continue // already committed by Pelokalan
		}
		want[pe.Key()] = gitpatch.Target{Translation: e.MsgStr, Fuzzy: e.Fuzzy}
	}
	return want, nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// UpstreamChanges counts the strings the newest upstream file (a project file's contents) would bring to the
// member's file: strings it does not have yet, and strings whose upstream translation changed since the file's
// starting point (the newest "initial"/"from upstream" version) and differs from the member's. Differences that
// are the member's own work (upstream unchanged since the starting point) do not count. Without a starting point
// only new strings and translations of strings the member left empty count.
func (s *Service) UpstreamChanges(ctx context.Context, c *models.Catalog, upstream []byte) (int, error) {
	cat, err := po.ParseBytes(upstream)
	if err != nil {
		return 0, err
	}
	entries, err := s.catalogs.ListEntries(ctx, c.ID, false)
	if err != nil {
		return 0, err
	}
	mine := make(map[string]poState, len(entries))
	for _, e := range entries {
		if !e.Obsolete {
			mine[e.Key()] = poState{e.MsgStr, e.Fuzzy}
		}
	}
	ref := s.reference(ctx, c)
	n := 0
	for _, e := range cat.Entries {
		if e.Obsolete {
			continue
		}
		k := e.Key()
		up := poState{e.Translation, e.Fuzzy}
		cur, ok := mine[k]
		switch {
		case !ok:
			n++ // a new string upstream
		case sameState(up, cur):
		case ref != nil:
			if r, ok := ref[k]; !ok || !sameState(r, up) {
				n++ // upstream changed it since the file's starting point
			}
		case !translated(cur.tr) && translated(up.tr):
			n++
		}
	}
	return n, nil
}

func sameState(a, b poState) bool { return a.fuzzy == b.fuzzy && sameStrings(a.tr, b.tr) }

func translated(tr []string) bool {
	for _, t := range tr {
		if t != "" {
			return true
		}
	}
	return false
}

// UpstreamConflicts lists (by source text) the wanted entries someone else changed upstream meanwhile: the
// branch's current file (branch) has a translation that differs both from what the member started from and
// from the member's. The member's starting point for an entry is what Pelokalan last committed for it (sent),
// else the file's reference version. Committing would overwrite those, so the commit is refused until the
// member pulls the upstream changes.
func (s *Service) UpstreamConflicts(ctx context.Context, c *models.Catalog, branch []byte, want map[string]gitpatch.Target, sent []byte) []string {
	cat, err := po.ParseBytes(branch)
	if err != nil {
		return nil
	}
	base := s.reference(ctx, c)
	var done map[string]poState
	if sent != nil {
		if sc, err := po.ParseBytes(sent); err == nil {
			done = make(map[string]poState, len(sc.Entries))
			for _, e := range sc.Entries {
				done[e.Key()] = poState{e.Translation, e.Fuzzy}
			}
		}
	}
	var out []string
	for _, e := range cat.Entries {
		t, ok := want[e.Key()]
		if !ok || e.Obsolete {
			continue
		}
		start, known := done[e.Key()]
		if !known {
			start, known = base[e.Key()]
		}
		if !known {
			continue // no starting point to compare with
		}
		up := poState{e.Translation, e.Fuzzy}
		if !sameStrings(up.tr, start.tr) && !sameStrings(up.tr, t.Translation) {
			out = append(out, e.ID)
		}
	}
	return out
}
