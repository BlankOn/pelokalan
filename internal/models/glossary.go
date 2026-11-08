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
