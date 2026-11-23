package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"pelokalan/internal/glossary"
	"pelokalan/internal/models"
	"pelokalan/internal/po"
)

// Detecting work accepted upstream. Pelokalan re-downloads the upstream files at every sync (a GNOME
// release archive, which follows what is committed in each module's git, or a GitHub branch archive), so
// it can tell when a member's translation made it in, however it got there (a pull request, a reviewer
// committing an uploaded PO file, a maintainer applying a patch): the new upstream file has, word for
// word, a translation the member wrote in Pelokalan. "Wrote" means it differs from the file as the member
// took it or last updated it from upstream (the same reference as git patches), so translations that were
// already upstream, or that someone else's earlier work put there, never count for them.

// UpstreamFile is a PO file as just downloaded from upstream
type UpstreamFile struct {
	Name     string // Pelokalan file name
	RepoPath string // path in a git repository ("" otherwise)
	Data     []byte
}

// landingWorthy leaves out strings too short to tell one person's work from a coincidence ("OK", "_Save")
func landingWorthy(msgid string) bool {
	return len(strings.Fields(msgid)) >= 3 || utf8.RuneCountInString(msgid) >= 15
}

// DetectLandings records the strings of members' files in project projectID (language lang) that the
// just-imported upstream files of module group groupID now contain as the member wrote them. language
// forces the files' language ("" = detect per file, as imports do). It returns how many are new.
func (s *CatalogService) DetectLandings(ctx context.Context, projectID, groupID int64, lang, language string, files []UpstreamFile) (int, error) {
	if s.activity == nil || len(files) == 0 {
		return 0, nil
	}
	all, err := s.catalogs.ListAllByProject(ctx, projectID)
	if err != nil {
		return 0, err
	}
	var mine []*models.Catalog
	for _, c := range all {
		if !c.IsTemplate && c.DeletedAt == nil && c.Language == lang {
			mine = append(mine, c)
		}
	}
	if len(mine) == 0 {
		return 0, nil
	}
	added := 0
	for _, f := range files {
		var matched []*models.Catalog
		for _, c := range mine {
			switch {
			case f.RepoPath != "" && c.RepoPath != "":
				if c.RepoPath == f.RepoPath {
					matched = append(matched, c)
				}
			case c.Filename == f.Name || moduleKey(c.Filename) == moduleKey(f.Name):
				matched = append(matched, c)
			}
		}
		if len(matched) == 0 {
			continue
		}
		cat, err := po.ParseBytes(f.Data)
		if err != nil {
			continue
		}
		if language == "" && glossary.DetectLanguage(cat, f.Name) != lang {
			continue
		}
		upstream := make(map[string][]string, len(cat.Entries))
		for _, e := range cat.Entries {
			if !e.Obsolete && !e.Fuzzy && e.IsTranslated() {
				upstream[e.Key()] = e.Translation
			}
		}
		for _, c := range matched {
			n, err := s.landingsIn(ctx, c, groupID, upstream)
			if err != nil {
				return added, err
			}
			added += n
		}
	}
	return added, nil
}

// landingsIn records the strings of c the member wrote that upstream now has
func (s *CatalogService) landingsIn(ctx context.Context, c *models.Catalog, groupID int64, upstream map[string][]string) (int, error) {
	ref := s.reference(ctx, c)
	if ref == nil { // no version to tell the member's work from what they started with
		return 0, nil
	}
	entries, err := s.catalogs.ListEntries(ctx, c.ID, false)
	if err != nil {
		return 0, err
	}
	var out []models.UpstreamLanding
	for _, e := range entries {
		if e.Obsolete || e.Fuzzy || e.IsAI() || !e.IsTranslated() || !landingWorthy(e.MsgID) {
			continue
		}
		key := e.Key()
		if r, ok := ref[key]; ok && !r.fuzzy && sameStrings(r.tr, e.MsgStr) {
			continue // the member did not change it: it was there when they started
		}
		if up, ok := upstream[key]; !ok || !sameStrings(up, e.MsgStr) {
			continue
		}
		out = append(out, models.UpstreamLanding{UserID: c.OwnerID, ProjectID: c.ProjectID, CatalogID: c.ID, GroupID: groupID,
			EntryKey: e.KeyHash(), Module: moduleKey(c.Filename), Filename: c.Filename, Words: len(strings.Fields(e.MsgID))})
	}
	if len(out) == 0 {
		return 0, nil
	}
	return s.activity.repo.AddLandings(ctx, out)
}
