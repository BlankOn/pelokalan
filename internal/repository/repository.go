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
