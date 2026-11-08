package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

type contextRepository struct{ db *sql.DB }

// NewContextRepository creates the Konteks repository
func NewContextRepository(db *sql.DB) repository.ContextRepository { return &contextRepository{db: db} }

const contextColumns = `x.catalog_id, x.note, x.updated_by, x.updated_at,
	COALESCE((SELECT COALESCE(NULLIF(u.name, ''), u.username) FROM users u WHERE u.id = x.updated_by), ''),
	cat.filename, o.username, cat.language, cat.is_template`

const contextFrom = ` FROM catalog_contexts x
	JOIN catalogs cat ON cat.id = x.catalog_id
	JOIN users o ON o.id = cat.owner_id `

func scanContext(row interface{ Scan(...any) error }) (*models.CatalogContext, error) {
	c := &models.CatalogContext{}
	err := row.Scan(&c.CatalogID, &c.Note, &c.UpdatedBy, &c.UpdatedAt, &c.UpdaterName,
		&c.Filename, &c.OwnerUsername, &c.Language, &c.IsTemplate)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return c, err
}

func (r *contextRepository) Get(ctx context.Context, catalogID int64) (*models.CatalogContext, error) {
	return scanContext(r.db.QueryRowContext(ctx, `SELECT `+contextColumns+contextFrom+` WHERE x.catalog_id = $1`, catalogID))
}

func (r *contextRepository) Set(ctx context.Context, c *models.CatalogContext) error {
	if strings.TrimSpace(c.Note) == "" {
		_, err := r.db.ExecContext(ctx, `DELETE FROM catalog_contexts WHERE catalog_id = $1`, c.CatalogID)
		return err
	}
	return r.db.QueryRowContext(ctx,
		`INSERT INTO catalog_contexts (catalog_id, note, updated_by, updated_at) VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (catalog_id) DO UPDATE SET note = EXCLUDED.note, updated_by = EXCLUDED.updated_by, updated_at = NOW()
		 RETURNING updated_at`, c.CatalogID, c.Note, c.UpdatedBy).Scan(&c.UpdatedAt)
}

func (r *contextRepository) ListForProject(ctx context.Context, projectID int64, q string) ([]*models.CatalogContext, error) {
	args := []any{projectID}
	filter := ""
	if q = strings.ToLower(strings.TrimSpace(q)); q != "" {
		args = append(args, "%"+escapeLike(q)+"%")
		filter = ` AND (lower(x.note) LIKE $2 OR lower(cat.filename) LIKE $2 OR lower(o.username) LIKE $2)`
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+contextColumns+contextFrom+
		` WHERE cat.project_id = $1`+filter+` ORDER BY x.updated_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.CatalogContext
	for rows.Next() {
		c, err := scanContext(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
