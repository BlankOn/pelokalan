package service

import (
	"context"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/gz"
	"pelokalan/pkg/glossary"
	"pelokalan/pkg/po"
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
		matched := matchUpstream(mine, f.Name, f.RepoPath)
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
		upstream := upstreamTranslations(cat)
		for _, c := range matched {
			n, err := s.landingsIn(ctx, c, groupID, upstream, nil, nil)
			if err != nil {
				return added, err
			}
			added += n
		}
	}
	return added, nil
}

// matchUpstream picks the member files an upstream file is for: the same repository path, else the same
// file name or module
func matchUpstream(mine []*models.Catalog, name, repoPath string) []*models.Catalog {
	var out []*models.Catalog
	for _, c := range mine {
		switch {
		case repoPath != "" && c.RepoPath != "":
			if c.RepoPath == repoPath {
				out = append(out, c)
			}
		case c.Filename == name || models.ModuleKey(c.Filename) == models.ModuleKey(name):
			out = append(out, c)
		}
	}
	return out
}

// upstreamTranslations maps an upstream file's finished translations by entry key
func upstreamTranslations(cat *po.Catalog) map[string][]string {
	out := make(map[string][]string, len(cat.Entries))
	for _, e := range cat.Entries {
		if !e.Obsolete && !e.Fuzzy && e.IsTranslated() {
			out[e.Key()] = e.Translation
		}
	}
	return out
}

// landingsIn records the strings of c the member wrote that upstream now has. refAt measures "wrote" against
// the member's starting point at that time (nil = now); landedAt is stored as the time (nil = now).
func (s *CatalogService) landingsIn(ctx context.Context, c *models.Catalog, groupID int64, upstream map[string][]string, refAt, landedAt *time.Time) (int, error) {
	ref := s.referenceAt(ctx, c, refAt)
	if ref == nil { // no version to tell the member's work from what they started with
		return 0, nil
	}
	entries, err := s.catalogs.ListEntries(ctx, c.ID, false)
	if err != nil {
		return 0, err
	}
	var out []models.UpstreamLanding
	changed, pending := 0, 0 // the member's changes, and those upstream does not have yet
	for _, e := range entries {
		if e.Obsolete || e.Fuzzy || e.IsAI() || !e.IsTranslated() {
			continue
		}
		key := e.Key()
		if r, ok := ref[key]; ok && !r.fuzzy && sameStrings(r.tr, e.MsgStr) {
			continue // the member did not change it: it was there when they started
		}
		changed++
		if up, ok := upstream[key]; !ok || !sameStrings(up, e.MsgStr) {
			pending++
			continue
		}
		if !landingWorthy(e.MsgID) {
			continue
		}
		out = append(out, models.UpstreamLanding{UserID: c.OwnerID, ProjectID: c.ProjectID, CatalogID: c.ID, GroupID: groupID,
			EntryKey: e.KeyHash(), Module: models.ModuleKey(c.Filename), Filename: c.Filename, Words: len(strings.Fields(e.MsgID)), LandedAt: landedAt})
	}
	// all the member's changes are upstream: their reservation of the module is done (live syncs only)
	if refAt == nil && changed > 0 && pending == 0 && s.reservations != nil {
		s.reservations.ReleaseMerged(ctx, c.ProjectID, models.ModuleKey(c.Filename), c.OwnerID)
	}
	if len(out) == 0 {
		return 0, nil
	}
	return s.activity.AddLandings(ctx, out)
}

// SetReservations lets syncs release a module whose holder's changes are all upstream
func (s *CatalogService) SetReservations(r *ReservationService) { s.reservations = r }

// BackfillLandingsJob names the one-time catch-up in one_time_jobs
const BackfillLandingsJob = "backfill-upstream-landings-2026-11"

// BackfillLandings is a one-time catch-up for work accepted upstream before detection existed: every
// member file is compared with the upstream files stored at earlier syncs (project files), measuring the
// member's work against their starting point as it was at since (so work merged and then pulled back
// with "Perbarui" after since still counts). A string found is dated when that upstream file was stored.
// Run once at start-up (main.go, one_time_jobs); it returns how many strings it recorded.
func (s *CatalogService) BackfillLandings(ctx context.Context, projects []*models.Project, since time.Time) (int, error) {
	if s.activity == nil || s.projectFiles == nil {
		return 0, nil
	}
	added := 0
	for _, p := range projects {
		all, err := s.catalogs.ListAllByProject(ctx, p.ID)
		if err != nil {
			return added, err
		}
		byLang := map[string][]*models.Catalog{}
		for _, c := range all {
			if !c.IsTemplate && c.DeletedAt == nil && c.Language != "" {
				byLang[c.Language] = append(byLang[c.Language], c)
			}
		}
		for lang, mine := range byLang {
			files, err := s.projectFiles.ProjectFilesForLanguage(ctx, p.ID, lang)
			if err != nil {
				return added, err
			}
			for _, f := range files {
				matched := matchUpstream(mine, f.Filename, f.RepoPath)
				if len(matched) == 0 {
					continue
				}
				full, err := s.projectFiles.GetProjectFile(ctx, p.ID, f.ID)
				if err != nil {
					continue
				}
				data, err := gz.Decompress(full.Data)
				if err != nil {
					continue
				}
				cat, err := po.ParseBytes(data)
				if err != nil {
					continue
				}
				upstream := upstreamTranslations(cat)
				at := f.CreatedAt
				for _, c := range matched {
					n, err := s.landingsIn(ctx, c, f.GroupID, upstream, &since, &at)
					if err != nil {
						log.Printf("upstream landings backfill: file %d: %v", c.ID, err)
						continue
					}
					added += n
				}
			}
		}
	}
	return added, nil
}
