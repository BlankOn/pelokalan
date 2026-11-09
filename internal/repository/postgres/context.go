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

const contextColumns = `x.id, x.project_id, x.module, x.note, x.version, x.updated_by, x.updated_at,
	COALESCE((SELECT COALESCE(NULLIF(u.name, ''), u.username) FROM users u WHERE u.id = x.updated_by), '')`

func scanContext(row interface{ Scan(...any) error }) (*models.ModuleContext, error) {
	c := &models.ModuleContext{}
	err := row.Scan(&c.ID, &c.ProjectID, &c.Module, &c.Note, &c.Version, &c.UpdatedBy, &c.UpdatedAt, &c.UpdaterName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return c, err
}

func (r *contextRepository) Get(ctx context.Context, projectID int64, module string) (*models.ModuleContext, error) {
	return scanContext(r.db.QueryRowContext(ctx,
		`SELECT `+contextColumns+` FROM module_contexts x WHERE x.project_id = $1 AND x.module = $2`, projectID, module))
}

func (r *contextRepository) Save(ctx context.Context, c *models.ModuleContext, version int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version == 0 {
		err = tx.QueryRowContext(ctx,
			`INSERT INTO module_contexts (project_id, module, note, updated_by) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (project_id, module) DO NOTHING
			 RETURNING id, version, updated_at`, c.ProjectID, c.Module, c.Note, c.UpdatedBy).Scan(&c.ID, &c.Version, &c.UpdatedAt)
	} else {
		err = tx.QueryRowContext(ctx,
			`UPDATE module_contexts SET note = $3, updated_by = $4, updated_at = NOW(), version = version + 1
			 WHERE project_id = $1 AND module = $2 AND version = $5
			 RETURNING id, version, updated_at`, c.ProjectID, c.Module, c.Note, c.UpdatedBy, version).Scan(&c.ID, &c.Version, &c.UpdatedAt)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrConflict
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO module_context_edits (context_id, user_id, note) VALUES ($1, $2, $3)`,
		c.ID, c.UpdatedBy, c.Note); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *contextRepository) Edits(ctx context.Context, contextID int64, limit int) ([]models.ContextEdit, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT COALESCE(u.username, ''), e.created_at
		FROM module_context_edits e LEFT JOIN users u ON u.id = e.user_id
		WHERE e.context_id = $1 ORDER BY e.created_at DESC, e.id DESC LIMIT $2`, contextID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ContextEdit
	for rows.Next() {
		var e models.ContextEdit
		if err := rows.Scan(&e.Username, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *contextRepository) ListForProject(ctx context.Context, projectID int64, q string) ([]*models.ModuleContext, error) {
	args := []any{projectID}
	filter := ""
	for _, w := range strings.Fields(strings.ToLower(q)) {
		args = append(args, "%"+escapeLike(w)+"%")
		n := len(args)
		filter += ` AND (lower(x.note) LIKE $` + itoaArg(n) + ` OR x.module LIKE $` + itoaArg(n) + `)`
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+contextColumns+` FROM module_contexts x
		WHERE x.project_id = $1 AND btrim(x.note) <> ''`+filter+` ORDER BY x.updated_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ModuleContext
	for rows.Next() {
		c, err := scanContext(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
