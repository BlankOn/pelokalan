package catalog

import (
	"context"

	"pelokalan/internal/models"
	"pelokalan/internal/source"
)

// CatalogRepository defines methods for translation file data access
type Repository interface {
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
