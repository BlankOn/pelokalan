package catalog

import (
	"context"
	"strings"
	"sync"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/gz"
	"pelokalan/pkg/po"
)

// Plural-Forms in written files: Pelokalan keeps the language's plural rule for its own checks (Catalog.PluralForms),
// but writes the header field only when the file needs it (it has plural strings) or already had it. Files uploaded
// before this rule got the field added to their stored header; they lose it again on export when the upstream copy
// they came from (the project file: same repository path, else same name, else same module) has none.

type upstreamPlural struct {
	has, known bool
	at         time.Time
}

var upstreamPluralCache sync.Map // catalog id -> upstreamPlural

// upstreamPluralTTL keeps the answer for a while: Build runs on every download, raw edit and version
const upstreamPluralTTL = 10 * time.Minute

// pluralFormsHeader returns header with the Plural-Forms field as the file should be written
func (s *Service) pluralFormsHeader(ctx context.Context, c *models.Catalog, header string, plural bool) string {
	has := po.GetHeaderField(header, "Plural-Forms") != ""
	switch {
	case plural && c.PluralForms != "":
		return po.SetHeaderField(header, "Plural-Forms", c.PluralForms) // needed for the plural strings
	case !has:
		return header // nothing to add
	}
	if up, known := s.upstreamHasPlural(ctx, c); known && !up {
		return po.RemoveHeaderField(header, "Plural-Forms") // added by an earlier upload, not by upstream
	}
	return header
}

// upstreamHasPlural reports whether the file's upstream copy has a Plural-Forms field (known false: no copy found)
func (s *Service) upstreamHasPlural(ctx context.Context, c *models.Catalog) (has, known bool) {
	if v, ok := upstreamPluralCache.Load(c.ID); ok {
		if u := v.(upstreamPlural); time.Since(u.at) < upstreamPluralTTL {
			return u.has, u.known
		}
	}
	has, known = s.lookupUpstreamPlural(ctx, c)
	upstreamPluralCache.Store(c.ID, upstreamPlural{has: has, known: known, at: time.Now()})
	return has, known
}

func (s *Service) lookupUpstreamPlural(ctx context.Context, c *models.Catalog) (bool, bool) {
	if s.projectFiles == nil {
		return false, false
	}
	var id int64
	if c.RepoPath != "" {
		if pf, _, err := s.projectFiles.ProjectFileByRepoPath(ctx, c.ProjectID, c.RepoPath); err == nil {
			id = pf.ID
		}
	}
	if id == 0 {
		files, err := s.projectFiles.ProjectFilesForLanguage(ctx, c.ProjectID, c.Language) // newest first
		if err != nil {
			return false, false
		}
		for _, f := range files {
			if strings.EqualFold(f.Filename, c.Filename) {
				id = f.ID
				break
			}
		}
		if id == 0 {
			for _, f := range files {
				if models.ModuleKey(f.Filename) == models.ModuleKey(c.Filename) {
					id = f.ID
					break
				}
			}
		}
	}
	if id == 0 {
		return false, false
	}
	pf, err := s.projectFiles.GetProjectFile(ctx, c.ProjectID, id)
	if err != nil {
		return false, false
	}
	data, err := gz.Decompress(pf.Data)
	if err != nil {
		return false, false
	}
	up, err := po.ParseBytes(data)
	if err != nil {
		return false, false
	}
	return up.HeaderField("Plural-Forms") != "", true
}
