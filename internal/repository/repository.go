package repository

import (
	"context"
	"errors"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/thesaurus"
)

// ErrNotFound is returned when a record does not exist
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a record was changed by someone else
var ErrConflict = errors.New("conflict")

// UserRepository defines methods for user data access
type UserRepository interface {
	Create(ctx context.Context, user *models.User) error
	GetByID(ctx context.Context, id int64) (*models.User, error)
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	GetByUsername(ctx context.Context, username string) (*models.User, error)
	Update(ctx context.Context, user *models.User) error
	TouchLogin(ctx context.Context, id int64) error
}

// OTPRepository defines methods for one-time code data access
type OTPRepository interface {
	Create(ctx context.Context, otp *models.OTPCode) error
	GetLatest(ctx context.Context, email, purpose string) (*models.OTPCode, error)
	// LatestForEmail returns the most recent code of an address, any purpose
	LatestForEmail(ctx context.Context, email string) (*models.OTPCode, error)
	IncrementAttempts(ctx context.Context, id int64) error
	MarkUsed(ctx context.Context, id int64) error
	InvalidateAll(ctx context.Context, email string) error
	CleanupExpired(ctx context.Context) error
}

// ProjectRepository defines methods for project, member and invitation data access
type ProjectRepository interface {
	Create(ctx context.Context, p *models.Project) error
	GetByID(ctx context.Context, id int64) (*models.Project, error)
	GetBySlug(ctx context.Context, slug string) (*models.Project, error)
	ListForUser(ctx context.Context, userID int64) ([]*models.Project, error)
	// ListAll returns every project; Role is filled for projects the user belongs to (userID 0 = visitor)
	ListAll(ctx context.Context, userID int64) ([]*models.Project, error)
	Update(ctx context.Context, p *models.Project) error
	Delete(ctx context.Context, id int64) error

	GetMemberRole(ctx context.Context, projectID, userID int64) (models.ProjectRole, error)
	ListMembers(ctx context.Context, projectID int64) ([]*models.ProjectMember, error)
	AddMember(ctx context.Context, projectID, userID int64, role models.ProjectRole) error
	UpdateMemberRole(ctx context.Context, projectID, userID int64, role models.ProjectRole) error
	RemoveMember(ctx context.Context, projectID, userID int64) error

	CreateInvitation(ctx context.Context, inv *models.Invitation) error
	GetInvitation(ctx context.Context, id int64) (*models.Invitation, error)
	GetPendingInvitation(ctx context.Context, projectID int64, email string) (*models.Invitation, error)
	ListPendingInvitationsForProject(ctx context.Context, projectID int64) ([]*models.Invitation, error)
	ListPendingInvitationsForEmail(ctx context.Context, email string) ([]*models.Invitation, error)
	MarkInvitationAccepted(ctx context.Context, id int64) error
	MarkInvitationDeclined(ctx context.Context, id int64) error
	DeleteInvitation(ctx context.Context, id int64) error
	CreateInviteLink(ctx context.Context, l *models.InviteLink) error
	GetInviteLinkByToken(ctx context.Context, token string) (*models.InviteLink, error)
	ListInviteLinks(ctx context.Context, projectID int64) ([]*models.InviteLink, error)
	DeleteInviteLink(ctx context.Context, projectID, id int64) error
	// JoinByInviteLink counts a use of a still valid link and adds the user as a member,
	// in one transaction. It returns ErrNotFound when the link is no longer usable.
	JoinByInviteLink(ctx context.Context, linkID, userID int64) error
}

// CatalogRepository defines methods for translation file data access
type CatalogRepository interface {
	// Create inserts the catalog and its entries in one transaction
	Create(ctx context.Context, c *models.Catalog, entries []*models.Entry) error
	GetByID(ctx context.Context, id int64) (*models.Catalog, error)
	// GetByPath finds a file by project, owner username and file name
	GetByPath(ctx context.Context, projectID int64, ownerUsername, filename string) (*models.Catalog, error)
	// FilenameTaken reports whether the owner has another file with that name in the project
	FilenameTaken(ctx context.Context, projectID, ownerID int64, filename string, excludeID int64) (bool, error)
	GetTemplate(ctx context.Context, projectID, ownerID int64) (*models.Catalog, error)
	// ListByProject returns the files a user owns in a project
	ListByProject(ctx context.Context, projectID, ownerID int64) ([]*models.Catalog, error)
	// ListAllByProject returns the files of every member of a project
	ListAllByProject(ctx context.Context, projectID int64) ([]*models.Catalog, error)
	UpdateHeader(ctx context.Context, c *models.Catalog) error
	// Delete marks a file deleted (kept read-only with its entries and history)
	Delete(ctx context.Context, id, byUserID int64) error
	// ReplaceEntries replaces all entries (used by merges and re-uploads)
	ReplaceEntries(ctx context.Context, c *models.Catalog, entries []*models.Entry) error

	// ListEntries returns entries in file order
	ListEntries(ctx context.Context, catalogID int64, includeObsolete bool) ([]*models.Entry, error)
	GetEntry(ctx context.Context, id int64) (*models.Entry, error)
	// UpdateEntry saves translation fields when version still matches, and bumps it
	UpdateEntry(ctx context.Context, e *models.Entry, userID int64) error
	Stats(ctx context.Context, catalogID int64) (models.CatalogStats, error)
	// Suggestions returns translations of the same source text in the user's
	// other catalogs of the same language
	// PreTranslate fills untranslated singular entries with exact matches from
	// the user's other catalogs of the same language
	PreTranslate(ctx context.Context, catalogID, userID int64, language string, markFuzzy bool) (int, error)
	// MemoryMatches returns, per untranslated singular entry, an exact translation
	// from the user's other catalogs of the same language (without saving it)
	MemoryMatches(ctx context.Context, catalogID, userID int64, language string) (map[int64]string, error)
	Suggestions(ctx context.Context, userID int64, language string, msgid string, excludeEntryID int64, limit int) ([]Suggestion, error)
	// SetSourceURL remembers the link a file was downloaded from
	SetSourceURL(ctx context.Context, id int64, url string) error
	// SetUpstreamPage stores the file's page on the GNOME translation site ("" = none)
	SetUpstreamPage(ctx context.Context, id int64, page string) error
}

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
type GlossaryRepository interface {
	// project files: the PO files of glossary sources
	AddProjectFiles(ctx context.Context, files []*models.ProjectFile) error
	// ListProjectFiles pages the project files matching query, limited to groupIDs when given
	ListProjectFiles(ctx context.Context, projectID int64, query string, groupIDs []int64, limit, offset int) ([]*models.ProjectFile, int, error)
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
	// Stamp changes whenever imports of a project and language are added, replaced or removed
	Stamp(ctx context.Context, projectID int64, language string) (string, error)
	GetOrCreateGroup(ctx context.Context, projectID int64, name, version string) (*models.ModuleGroup, error)
	ListGroups(ctx context.Context, projectID int64) ([]*models.ModuleGroup, error)
	GetGroup(ctx context.Context, id int64) (*models.ModuleGroup, error)
	// DeleteGroup removes a module group with its imports, terms and download URL
	DeleteGroup(ctx context.Context, id int64) error
	// SetGroupSync sets (or, with an empty URL, removes) the download URL of a module group
	SetGroupSync(ctx context.Context, groupID int64, url, language string, userID *int64) error
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

// AIRepository stores LLM settings and pre-translation jobs
type AIRepository interface {
	GetSettings(ctx context.Context, projectID int64) (*models.AISettings, error)
	SaveSettings(ctx context.Context, s *models.AISettings) error
	DeleteSettings(ctx context.Context, projectID int64) error

	CreateJob(ctx context.Context, j *models.AIJob) error
	UpdateJob(ctx context.Context, j *models.AIJob) error
	GetJob(ctx context.Context, id int64) (*models.AIJob, error)
	LatestJob(ctx context.Context, catalogID int64) (*models.AIJob, error)
	RequestCancel(ctx context.Context, id int64) error
	IsCancelRequested(ctx context.Context, id int64) (bool, error)
	// Spend returns the USD cost of a project's jobs since a time
	Spend(ctx context.Context, projectID int64, since time.Time) (float64, error)
	// FailRunning marks jobs left running by a previous process as failed
	FailRunning(ctx context.Context) error
}

// CommitRepository stores catalog versions
type CommitRepository interface {
	Create(ctx context.Context, c *models.Commit, snapshot []byte) error
	List(ctx context.Context, catalogID int64) ([]*models.Commit, error)
	Get(ctx context.Context, id int64) (*models.Commit, error)
	Latest(ctx context.Context, catalogID int64) (*models.Commit, error)
	// SetMessage changes the message of a commit
	SetMessage(ctx context.Context, id int64, message string) error
	// Snapshot returns the uncompressed PO text of a commit
	Snapshot(ctx context.Context, id int64) ([]byte, error)
}

// DiscussionFilter narrows a discussion listing
type DiscussionFilter struct {
	MentionedUserID int64 // only discussions mentioning this user
	SubjectType     string
	Query           string // every word must appear in the title or a visible comment
	Limit, Offset   int
}

// DiscussionRepository stores generic discussions and their comments
type DiscussionRepository interface {
	Get(ctx context.Context, projectID int64, subjectType, subjectKey string) (*models.Discussion, error)
	GetByID(ctx context.Context, id int64) (*models.Discussion, error)
	// GetOrCreate returns the discussion of a subject, creating it when missing
	GetOrCreate(ctx context.Context, d *models.Discussion) error
	List(ctx context.Context, projectID int64, f DiscussionFilter) ([]*models.Discussion, int, error)
	// Counts returns comment counts per subject key
	Counts(ctx context.Context, projectID int64, subjectType string, keys []string) (map[string]int, error)

	// AddComment stores a comment with its mentions and attachments (blobs must exist)
	AddComment(ctx context.Context, c *models.Comment, mentions []int64, attachments []*models.Attachment) error
	// Attachment returns an attachment with the project of its discussion
	Attachment(ctx context.Context, id int64) (*models.Attachment, int64, error)
	Comments(ctx context.Context, discussionID int64) ([]*models.Comment, error)
	GetComment(ctx context.Context, id int64) (*models.Comment, error)
	UpdateComment(ctx context.Context, id int64, body string, mentions []int64) error
	DeleteComment(ctx context.Context, id, byUserID int64) error
	// DeleteDiscussion removes a discussion with its comments, mentions and attachment links
	DeleteDiscussion(ctx context.Context, id int64) error
}

// Repositories holds all repositories
type Repositories struct {
	User     UserRepository
	OTP      OTPRepository
	Project  ProjectRepository
	Catalog  CatalogRepository
	Glossary GlossaryRepository
	AI       AIRepository
	Commit   CommitRepository
	Discuss  DiscussionRepository
	Activity ActivityRepository
}

// ActivityRepository stores the activity log (activity_events)
type ActivityRepository interface {
	Record(ctx context.Context, e *models.ActivityEvent) error
	// Summary adds up each user's activity in a project since the given time (nil = all time),
	// most active first
	Summary(ctx context.Context, projectID int64, since *time.Time) ([]*models.ActivitySummary, error)
	// HourlyTranslated returns the strings people saved with a translation per hour in a project (typed by
	// hand, AI drafts saved unchanged or edited, existing translations edited), oldest first (Day = the hour)
	HourlyTranslated(ctx context.Context, projectID int64) ([]models.ActivityDay, error)
	// Since returns the time of the project's first recorded event (nil when there is none)
	Since(ctx context.Context, projectID int64) (*time.Time, error)
}

// WikiRepository stores wiki pages and their files
type WikiRepository interface {
	ListPages(ctx context.Context, projectID int64) ([]*models.WikiPage, error) // without bodies
	GetPage(ctx context.Context, projectID int64, slug string) (*models.WikiPage, error)
	GetPageByID(ctx context.Context, projectID, id int64) (*models.WikiPage, error)
	SlugExists(ctx context.Context, projectID int64, slug string) (bool, error)
	// Redirect returns the current slug of the page that used to live at slug
	Redirect(ctx context.Context, projectID int64, slug string) (string, error)
	CreatePage(ctx context.Context, p *models.WikiPage) error
	// UpdatePage saves slug, title and body when the page still has the given updated_at
	// (UnixNano); it returns ErrConflict otherwise. A changed slug leaves a redirect behind.
	UpdatePage(ctx context.Context, p *models.WikiPage, version int64) error
	DeletePage(ctx context.Context, projectID, id int64) error
	HasChildren(ctx context.Context, id int64) (bool, error)
	// Reorder makes ids the children of parentID (nil = top level) in that order; it does not
	// change updated_at, so moving a page never conflicts with someone editing it
	Reorder(ctx context.Context, projectID int64, parentID *int64, ids []int64) error
	// Edits lists who saved a page and when, newest first
	Edits(ctx context.Context, pageID int64, limit int) ([]models.WikiEdit, error)
	CreateFile(ctx context.Context, f *models.WikiFile) error
	GetFile(ctx context.Context, projectID, id int64) (*models.WikiFile, error)
}

// ModuleCount is a module (PO file) and how many glossary rows come from it
type ModuleCount struct {
	Name  string
	Terms int64
}

// NotificationRepository stores members' email choices per project
type NotificationRepository interface {
	// Settings returns the choices of the given users (missing users have the defaults)
	Settings(ctx context.Context, projectID int64, userIDs []int64) (map[int64]*models.NotificationSettings, error)
	Save(ctx context.Context, userID, projectID int64, s *models.NotificationSettings) error
}

// ContextRepository stores Konteks notes, one per module of a project
type ContextRepository interface {
	Get(ctx context.Context, projectID int64, module string) (*models.ModuleContext, error)
	// Save stores the note and records the edit. version is the version the editor
	// started from (0 = none yet); ErrConflict when someone saved in between.
	Save(ctx context.Context, c *models.ModuleContext, version int64) error
	Edits(ctx context.Context, contextID int64, limit int) ([]models.ContextEdit, error)
	// ListForProject lists the project's non-empty notes, optionally matching q
	ListForProject(ctx context.Context, projectID int64, q string) ([]*models.ModuleContext, error)
}

// ThesaurusRepository stores the thesaurus
type ThesaurusRepository interface {
	Count(ctx context.Context) (int, error)
	All(ctx context.Context) ([]thesaurus.Entry, error)
	// ReplaceAll swaps the whole thesaurus in one transaction
	ReplaceAll(ctx context.Context, entries []thesaurus.Entry) error
}
