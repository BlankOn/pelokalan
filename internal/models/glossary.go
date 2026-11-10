package models

import "time"

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
