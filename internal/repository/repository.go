package repository

import (
	"context"
	"time"

	"pelokalan/internal/models"
)

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
