package modulecontext

import (
	"context"

	"pelokalan/internal/models"
)

// ContextRepository stores Konteks notes, one per module of a project
type Repository interface {
	Get(ctx context.Context, projectID int64, module string) (*models.ModuleContext, error)
	// Save stores the note and records the edit. version is the version the editor
	// started from (0 = none yet); ErrConflict when someone saved in between.
	Save(ctx context.Context, c *models.ModuleContext, version int64) error
	Edits(ctx context.Context, contextID int64, limit int) ([]models.ContextEdit, error)
	// ListForProject lists the project's non-empty notes, optionally matching q
	ListForProject(ctx context.Context, projectID int64, q string) ([]*models.ModuleContext, error)
}
