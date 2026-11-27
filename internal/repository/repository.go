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
