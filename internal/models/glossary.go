package models

import (
	"strings"
	"time"
)

// GlossaryImport is one uploaded set of existing translations
type GlossaryImport struct {
	ID           int64
	ProjectID    int64
	VersionID    int64
	VersionLabel string
	Filename     string
	Language     string
	FileCount    int
	EntryCount   int
	TermCount    int
	UploadedBy   *int64
	UploaderName string
	CreatedAt    time.Time
}

// GlossaryGroup is one source term with all translations seen for it
type GlossaryGroup struct {
	Hash         string // stable id, see glossary.TermHash
	Comments     int    // comments in the term's discussion
	Source       string
	Total        int
	Translations []GlossaryTranslation
}

// IsInconsistent reports whether the term was translated in more than one way
func (g *GlossaryGroup) IsInconsistent() bool { return len(g.Translations) > 1 }

// GlossaryTranslation is one way a term was translated
type GlossaryTranslation struct {
	Text     string
	Count    int
	Modules  []string
	Contexts []string
	Versions []string // version labels using this translation
}

// Share returns the percentage of occurrences using this translation
func (t GlossaryTranslation) Share(total int) int {
	if total == 0 {
		return 0
	}
	return t.Count * 100 / total
}

// ProjectVersion is one release of a project, e.g. name "gnome", version "51"
type ProjectVersion struct {
	ID        int64
	ProjectID int64
	Name      string
	Version   string
	CreatedAt time.Time

	// Automatic download of the glossary source
	GlossaryURL       string
	GlossaryLanguage  string
	FeedCreatedBy     *int64
	LastFetchedAt     *time.Time
	LastChangedAt     *time.Time
	LastFetchError    string
	FetchETag         string
	FetchLastModified string
	FetchContentHash  string

	// Filled by listing queries
	ImportCount int
}

// HasFeed reports whether the version is downloaded automatically
func (v *ProjectVersion) HasFeed() bool { return v.GlossaryURL != "" }

// Label returns the display form, e.g. "gnome-51"
func (v *ProjectVersion) Label() string { return VersionLabel(v.Name, v.Version) }

// VersionLabel joins a name and version, e.g. "gnome-51"
func VersionLabel(name, version string) string {
	if version == "" {
		return name
	}
	return name + "-" + version
}

// GlossaryTermRow is one translation of a term in one version and language
type GlossaryTermRow struct {
	VersionID    int64
	VersionLabel string
	Language     string
	Translation  string
	Count        int
	Modules      []string
	Contexts     []string
}

// GlossaryTerm is everything known about one glossary string
type GlossaryTerm struct {
	Hash   string
	Source string
	Words  int
	Rows   []GlossaryTermRow
}

// ProjectFile is a PO file contained in a glossary source (e.g. a release tarball)
type ProjectFile struct {
	ID           int64
	ImportID     int64
	ProjectID    int64
	VersionID    int64
	VersionLabel string
	Filename     string
	Language     string
	Total        int
	Translated   int
	Fuzzy        int
	Size         int64
	Data         []byte   // gzip-compressed; only filled when one file is loaded
	Mine         *Catalog // the viewer's own file of the same module, if any (not stored)
	UpstreamInfo          // state on the GNOME translation site (not stored)
	CreatedAt    time.Time
}

// Percent is the share of translated entries
// GroupStat is a recorded total of a module group's project files at one moment
type GroupStat struct {
	VersionID  int64
	Language   string
	Total      int
	Translated int
	Fuzzy      int
	RecordedAt time.Time
}

// Untranslated is what is neither translated nor fuzzy
func (g GroupStat) Untranslated() int { return g.Total - g.Translated - g.Fuzzy }

// ProjectFileVersion is a glossary version that has project files (filter option)
type ProjectFileVersion struct {
	ID      int64
	Name    string
	Version string
	Files   int
}

// Label is "name-version", e.g. "gnome-51"
func (v *ProjectFileVersion) Label() string { return VersionLabel(v.Name, v.Version) }

// Stats returns the file's progress in the shape the progress bar expects
func (f *ProjectFile) Stats() CatalogStats {
	return CatalogStats{Total: f.Total, Translated: f.Translated, Fuzzy: f.Fuzzy,
		Untranslated: f.Total - f.Translated - f.Fuzzy}
}

func (f *ProjectFile) Percent() int {
	if f.Total == 0 {
		return 0
	}
	return f.Translated * 100 / f.Total
}

// UpstreamStats is a project file's translation on the GNOME translation site
type UpstreamStats struct {
	State        string // workflow state as the site names it, e.g. "Translating"; "None" = nobody works on it
	Translated   int
	Fuzzy        int
	Untranslated int
	PageURL      string // the translation's page on the site
	POFileURL    string // the current PO file on the site ("" = none)
}

// Total is the number of strings upstream
func (u *UpstreamStats) Total() int { return u.Translated + u.Fuzzy + u.Untranslated }

// Percent is the share of translated strings upstream
func (u *UpstreamStats) Percent() int {
	if u.Total() == 0 {
		return 0
	}
	return u.Translated * 100 / u.Total()
}

// upstreamStates are the workflow states with a translated label
var upstreamStates = map[string]string{
	"none": "none", "translating": "translating", "translated": "translated",
	"proofreading": "proofreading", "proofread": "proofread", "toreview": "to_review",
	"tocommit": "to_commit", "committing": "committing", "committed": "committed",
}

// StateKey is the i18n key of the workflow state ("" = unknown state, show it as is)
func (u *UpstreamStats) StateKey() string {
	if k, ok := upstreamStates[strings.ToLower(u.State)]; ok {
		return "upstream.state_" + k
	}
	return ""
}

// StateClass names the state for styling ("to_review"; "other" for states the site may add later)
func (u *UpstreamStats) StateClass() string {
	if k, ok := upstreamStates[strings.ToLower(u.State)]; ok {
		return k
	}
	return "other"
}

// UpstreamInfo is what is known of a file upstream (embedded in ProjectFile and Catalog, not stored)
type UpstreamInfo struct {
	Upstream        *UpstreamStats // nil = not on the site or not checked yet
	UpstreamChecked bool           // the site was asked: Upstream nil then means the release has no such file
	UpstreamLink    string         // without a state: the site's page for the file, to check by hand
}

// UpstreamClass is the file's value for the upstream state filter: a StateClass,
// "missing" (the release has no such file) or "unknown" (not checked yet / no release upstream)
func (i UpstreamInfo) UpstreamClass() string {
	switch {
	case i.Upstream != nil:
		return i.Upstream.StateClass()
	case i.UpstreamChecked:
		return "missing"
	}
	return "unknown"
}

// UpstreamFilters are the options of the upstream state filter, in workflow order
var UpstreamFilters = []string{"none", "translating", "translated", "proofreading", "proofread",
	"to_review", "to_commit", "committing", "committed", "other", "missing", "unknown"}

// UpstreamFilterSet reads the filter values (u params); unknown values are ignored, nil = no filter
func UpstreamFilterSet(values []string) map[string]bool {
	var out map[string]bool
	for _, v := range values {
		for _, f := range UpstreamFilters {
			if v == f {
				if out == nil {
					out = map[string]bool{}
				}
				out[v] = true
			}
		}
	}
	return out
}

// UpstreamCandidate is a project file to look up upstream (listed across all projects)
type UpstreamCandidate struct {
	Filename        string
	FileLanguage    string
	ProjectLanguage string
	VersionName     string
	Version         string
	GlossaryURL     string
}

// UpstreamRecord is a stored upstream state (table upstream_states)
type UpstreamRecord struct {
	Release      string
	Language     string
	Stem         string
	Listed       bool
	Module       string
	Branch       string
	Domain       string
	State        string
	Translated   int
	Fuzzy        int
	Untranslated int
	POFile       string
	CheckedAt    time.Time
}

// UpstreamStateKeys lists every i18n key StateKey can return (for tests)
func UpstreamStateKeys() []string {
	var out []string
	for _, k := range upstreamStates {
		out = append(out, "upstream.state_"+k)
	}
	return out
}
