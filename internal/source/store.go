package source

import (
	"context"
	"time"

	"pelokalan/internal/models"
)

// Suggestion is a translation memory match
type Suggestion struct {
	Text        string
	ProjectName string
	Count       int
}

// GlossaryQuery selects glossary groups
type GlossaryQuery struct {
	ProjectID    int64
	Language     string
	GroupID      int64  // 0 = all module groups
	Query        string // matches source or translation (case-insensitive substring)
	MaxWords     int    // 0 = any length
	Inconsistent bool   // only terms with more than one translation
	Module       string // only terms used in this module (PO file), e.g. "gtk.gtk-4-24"; all translations stay
	Order        string // "variants" (default), "frequency" or "alpha"
	Dir          string // "asc", "desc" or "" (natural direction of the sort)
	Limit        int
	Offset       int
}

// GlossaryTerm is a row to insert into the glossary
type GlossaryTerm struct {
	Source, SourceNorm, Translation, TranslationNorm string
	WordCount, Occurrences                           int
	Modules, Contexts                                []string
}

// GlossaryRepository defines methods for glossary data access
type Repository interface {
	// project files: the PO files of glossary sources
	AddProjectFiles(ctx context.Context, files []*models.ProjectFile) error
	// ListProjectFiles pages the project files matching query, limited to groupIDs when given, in a sort
	// order of ProjectFileSorts ("" or unknown = most left first)
	ListProjectFiles(ctx context.Context, projectID int64, query string, groupIDs []int64, sort string, limit, offset int) ([]*models.ProjectFile, int, error)
	GroupsWithFiles(ctx context.Context, projectID int64) ([]*models.ModuleGroupOption, error)
	// UpstreamCandidates lists every distinct project file with what is needed to find it upstream
	UpstreamCandidates(ctx context.Context) ([]models.UpstreamCandidate, error)
	// UpstreamStates returns the stored upstream states of a release and language, by stem
	UpstreamStates(ctx context.Context, release, language string) (map[string]*models.UpstreamRecord, error)
	// SaveUpstreamState stores one upstream state (insert or replace)
	SaveUpstreamState(ctx context.Context, rec *models.UpstreamRecord) error
	// LinkedUpstreamPages lists the distinct upstream pages entered on files that are not deleted
	LinkedUpstreamPages(ctx context.Context) ([]string, error)
	// ProjectFileNames lists (module group id, file name) of the project files, for module groups
	ProjectFileNames(ctx context.Context, projectID int64) (map[int64][]string, error)
	// RecordGroupStats stores the current totals of a module group's project files per language
	// (every module group when groupID is 0), skipping ones whose last point is newer than
	// minGap and unchanged
	RecordGroupStats(ctx context.Context, groupID int64, minGap time.Duration) error
	// GroupStats returns a module group's recorded totals for a language since a moment, oldest first
	GroupStats(ctx context.Context, groupID int64, language string, since time.Time) ([]models.GroupStat, error)
	GetProjectFile(ctx context.Context, projectID, id int64) (*models.ProjectFile, error)
	CountGroupFiles(ctx context.Context, groupID int64) (int, error)
	ProjectFilesForLanguage(ctx context.Context, projectID int64, language string) ([]*models.ProjectFile, error)
	// ProjectFileTotals adds up the project files' strings, counting each module (per language) once, from its
	// newest copy
	ProjectFileTotals(ctx context.Context, projectID int64) (total, translated, fuzzy int, err error)
	// Stamp changes whenever imports of a project and language are added, replaced or removed
	Stamp(ctx context.Context, projectID int64, language string) (string, error)
	GetOrCreateGroup(ctx context.Context, projectID int64, name, version string) (*models.ModuleGroup, error)
	ListGroups(ctx context.Context, projectID int64) ([]*models.ModuleGroup, error)
	GetGroup(ctx context.Context, id int64) (*models.ModuleGroup, error)
	// DeleteGroup removes a module group with its imports, terms and download URL
	DeleteGroup(ctx context.Context, id int64) error
	// SetGroupSync sets (or, with an empty URL, removes) the download URL of a module group
	SetGroupSync(ctx context.Context, groupID int64, url, language string, userID *int64) error
	// SetGroupGit stores the GitHub repository ("owner/name") and branch a module group is synced from
	SetGroupGit(ctx context.Context, groupID int64, repo, branch string) error
	// SetGroupGitAccess records an access check of the GitHub App on a group's repository (no write access
	// also turns full integration off)
	SetGroupGitAccess(ctx context.Context, groupID, installation int64, write bool, errText string) error
	// SetGroupGitDirect turns full integration with the group's repository on or off
	SetGroupGitDirect(ctx context.Context, groupID int64, on bool) error
	// SetGroupTP stores the Translation Project team a module group is synced from ("" = none)
	SetGroupTP(ctx context.Context, groupID int64, team string) error
	// TPDomains returns a Translation Project team's domains as last read (data only when withData)
	TPDomains(ctx context.Context, team string, withData bool) ([]*models.TPDomain, error)
	// SaveTPDomains replaces a team's domains (rows without Data keep the stored file)
	SaveTPDomains(ctx context.Context, team string, domains []*models.TPDomain) error
	// ProjectFileByRepoPath returns the newest project file with this path from a git module group
	// (with its data) and that group
	ProjectFileByRepoPath(ctx context.Context, projectID int64, path string) (*models.ProjectFile, *models.ModuleGroup, error)
	// DueGroupSyncs returns module groups with a URL not fetched since the given time
	DueGroupSyncs(ctx context.Context, before time.Time) ([]*models.ModuleGroup, error)
	// RecordFetch stores the outcome of a download
	RecordFetch(ctx context.Context, v *models.ModuleGroup, changed bool) error
	// CreateGroupImport stores an import, replacing earlier imports of the same module group and language
	CreateGroupImport(ctx context.Context, imp *models.ModuleGroupImport, terms []GlossaryTerm) error
	ListGroupImports(ctx context.Context, projectID int64) ([]*models.ModuleGroupImport, error)
	GetGroupImport(ctx context.Context, id int64) (*models.ModuleGroupImport, error)
	DeleteGroupImport(ctx context.Context, id int64) error
	Languages(ctx context.Context, projectID int64) ([]string, error)
	// Term returns all translations of one glossary string, or ErrNotFound
	Term(ctx context.Context, projectID int64, hash string) (*models.GlossaryTerm, error)
	// PreferredTerms returns the most used translation of every source term
	// (groupID 0 = all module groups)
	PreferredTerms(ctx context.Context, projectID int64, language string, groupID int64) ([]PreferredTerm, error)
	// Modules lists the modules (PO files) glossary terms come from, with their term counts
	Modules(ctx context.Context, projectID int64, language string, groupID int64) ([]ModuleCount, error)
	// Search returns one page of groups and the total number of matching groups
	Search(ctx context.Context, q GlossaryQuery) ([]*models.GlossaryGroup, int, error)
}

// PreferredTerm is the dominant translation of a glossary term
type PreferredTerm struct {
	Source      string
	Translation string
	Share       float64
	Words       int
}

// ModuleCount is a module (PO file) and how many glossary rows come from it
type ModuleCount struct {
	Name  string
	Terms int64
}
