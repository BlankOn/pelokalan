package repository

import (
	"context"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/source"
)

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
	Suggestions(ctx context.Context, userID int64, language string, msgid string, excludeEntryID int64, limit int) ([]source.Suggestion, error)
	// SetSourceURL remembers the link a file was downloaded from
	SetSourceURL(ctx context.Context, id int64, url string) error
	// SetUpstreamPage stores the file's page on the GNOME translation site ("" = none)
	SetUpstreamPage(ctx context.Context, id int64, page string) error
	// SetRepoPath stores the file's path in its module group's git repository
	SetRepoPath(ctx context.Context, id int64, path string) error
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
	// ResolveComment marks an open review comment resolved
	ResolveComment(ctx context.Context, id, byUserID int64) error
	// OpenReviewComments counts the open review comments on a file's entries, by entry key hash
	OpenReviewComments(ctx context.Context, projectID, catalogID int64) (map[string]int, error)
	UpdateComment(ctx context.Context, id int64, body string, mentions []int64) error
	DeleteComment(ctx context.Context, id, byUserID int64) error
	// DeleteDiscussion removes a discussion with its comments, mentions and attachment links
	DeleteDiscussion(ctx context.Context, id int64) error
}

// ReviewRepository stores review requests of modules for repositories Pelokalan writes to (module_reviews)
type ReviewRepository interface {
	// Create stores a request; ErrConflict when the file already has an open one
	Create(ctx context.Context, r *models.ModuleReview) error
	Get(ctx context.Context, id int64) (*models.ModuleReview, error)
	// Open lists a project's requests waiting for review, oldest first
	Open(ctx context.Context, projectID int64) ([]*models.ModuleReview, error)
	// Latest returns the newest review of each of the catalogs
	Latest(ctx context.Context, catalogIDs []int64) (map[int64]*models.ModuleReview, error)
	// Update stores the outcome (status, reviewer, note, commit, pushed data, error, entries)
	Update(ctx context.Context, r *models.ModuleReview) error
	// LastPushed returns the repository file (gzip) as last committed for a catalog (nil = never)
	LastPushed(ctx context.Context, catalogID int64) ([]byte, error)
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
