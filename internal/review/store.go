package review

import (
	"context"

	"pelokalan/internal/models"
)

// ReviewRepository stores review requests of modules for repositories Pelokalan writes to (module_reviews)
type Repository interface {
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
