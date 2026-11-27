package wiki

import (
	"context"

	"pelokalan/internal/models"
)

// WikiRepository stores wiki pages and their files
type Repository interface {
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
