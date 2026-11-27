package pretranslate

import (
	"context"
	"time"

	"pelokalan/internal/models"
	"pelokalan/pkg/ai"
)

// AIRepository stores LLM settings and pre-translation jobs
type Repository interface {
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

	// glossary extraction from long strings (Spend and FailRunning include these jobs)
	CreateExtractJob(ctx context.Context, j *models.TermExtractionJob) error
	UpdateExtractJob(ctx context.Context, j *models.TermExtractionJob) error
	// LatestExtractJob returns the project's newest extraction job (store.ErrNotFound when none)
	LatestExtractJob(ctx context.Context, projectID int64) (*models.TermExtractionJob, error)
	// ExtractedPairs returns the stored results of the strings (by hash) that were extracted before
	ExtractedPairs(ctx context.Context, language string, hashes []string) (map[string][]ai.Pair, error)
	// SaveExtractedPairs stores the results of strings (hash -> checked pairs, possibly none)
	SaveExtractedPairs(ctx context.Context, language, model string, pairs map[string][]ai.Pair) error
}
