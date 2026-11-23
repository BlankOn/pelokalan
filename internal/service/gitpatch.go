package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"pelokalan/internal/gitpatch"
	"pelokalan/internal/models"
	"pelokalan/internal/po"
	"pelokalan/internal/repository"
)

// SetProjectFiles gives the service access to project files, for git patches against the repository's files
func (s *CatalogService) SetProjectFiles(r repository.GlossaryRepository) { s.projectFiles = r }

// SetRepoPath stores a file's path in its module group's git repository
func (s *CatalogService) SetRepoPath(ctx context.Context, user *models.User, c *models.Catalog, path string) error {
	if !c.WritableBy(user.ID) {
		return ErrForbidden
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
func (s *CatalogService) reference(ctx context.Context, c *models.Catalog) map[string]poState {
	list, err := s.commits.List(ctx, c.ID) // newest first
	if err != nil {
		return nil
	}
	for _, cm := range list {
		if cm.Kind != models.CommitInitial && !strings.HasPrefix(cm.Message, CommitMsgFromUpstream) {
			continue
		}
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

func gunzip(data []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

// GitPatch builds one `git am` patch with the member's own changes to repository files (files with a
// RepoPath, owned by user). Only entries the member changed in Pelokalan since taking or last updating
// the file go in, against the repository file as last synced, so newer translations in the repository
// are never reverted. AI drafts nobody reviewed and emptied translations are left out. Files
// changes (0 = nothing to send) tells whether there is anything.
func (s *CatalogService) GitPatch(ctx context.Context, user *models.User, p *models.Project, catalogs []*models.Catalog, projectURL string) (*GitPatchResult, error) {
	if s.projectFiles == nil {
		return nil, ErrNotFound
	}
	res := &GitPatchResult{}
	var diffs []*gitpatch.FileDiff
	var lines []string
	for _, c := range catalogs {
		if c.RepoPath == "" || !c.WritableBy(user.ID) {
			continue
		}
		pf, g, err := s.projectFiles.ProjectFileByRepoPath(ctx, p.ID, c.RepoPath)
		if errors.Is(err, repository.ErrNotFound) {
			continue
		} else if err != nil {
			return nil, err
		}
		base, err := gunzip(pf.Data)
		if err != nil {
			return nil, err
		}
		entries, err := s.catalogs.ListEntries(ctx, c.ID, false)
		if err != nil {
			return nil, err
		}
		ref := s.reference(ctx, c)
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
			want[pe.Key()] = gitpatch.Target{Translation: e.MsgStr, Fuzzy: e.Fuzzy}
		}
		d := gitpatch.Diff(c.RepoPath, base, want)
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
