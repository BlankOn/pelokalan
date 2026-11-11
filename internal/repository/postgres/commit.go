package postgres

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"io"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

type commitRepository struct {
	db *sql.DB
}

// NewCommitRepository creates a new PostgreSQL commit repository
func NewCommitRepository(db *sql.DB) repository.CommitRepository {
	return &commitRepository{db: db}
}

const commitColumns = `c.id, c.catalog_id, c.parent_id, c.kind, c.message, c.author_id,
	COALESCE(NULLIF(u.name, ''), u.email, ''), c.content_hash, c.total, c.translated, c.fuzzy,
	c.added, c.changed, c.removed, c.reviewed, c.restored_from, c.created_at`

func scanCommit(row interface{ Scan(...any) error }) (*models.Commit, error) {
	c := &models.Commit{}
	err := row.Scan(&c.ID, &c.CatalogID, &c.ParentID, &c.Kind, &c.Message, &c.AuthorID, &c.AuthorName,
		&c.ContentHash, &c.Total, &c.Translated, &c.Fuzzy, &c.Added, &c.Changed, &c.Removed, &c.Reviewed, &c.RestoredFrom, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return c, err
}

func (r *commitRepository) Create(ctx context.Context, c *models.Commit, snapshot []byte) error {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if _, err := zw.Write(snapshot); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return r.db.QueryRowContext(ctx,
		`INSERT INTO catalog_commits (catalog_id, parent_id, kind, message, author_id, snapshot, content_hash,
		        total, translated, fuzzy, added, changed, removed, restored_from, reviewed)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING id, created_at`,
		c.CatalogID, c.ParentID, c.Kind, c.Message, c.AuthorID, buf.Bytes(), c.ContentHash,
		c.Total, c.Translated, c.Fuzzy, c.Added, c.Changed, c.Removed, c.RestoredFrom, c.Reviewed,
	).Scan(&c.ID, &c.CreatedAt)
}

func (r *commitRepository) List(ctx context.Context, catalogID int64) ([]*models.Commit, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+commitColumns+` FROM catalog_commits c LEFT JOIN users u ON u.id = c.author_id
		 WHERE c.catalog_id = $1 ORDER BY c.id DESC`, catalogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Commit
	for rows.Next() {
		c, err := scanCommit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *commitRepository) Get(ctx context.Context, id int64) (*models.Commit, error) {
	return scanCommit(r.db.QueryRowContext(ctx,
		`SELECT `+commitColumns+` FROM catalog_commits c LEFT JOIN users u ON u.id = c.author_id WHERE c.id = $1`, id))
}

func (r *commitRepository) Latest(ctx context.Context, catalogID int64) (*models.Commit, error) {
	return scanCommit(r.db.QueryRowContext(ctx,
		`SELECT `+commitColumns+` FROM catalog_commits c LEFT JOIN users u ON u.id = c.author_id
		 WHERE c.catalog_id = $1 ORDER BY c.id DESC LIMIT 1`, catalogID))
}

func (r *commitRepository) Snapshot(ctx context.Context, id int64) ([]byte, error) {
	var data []byte
	err := r.db.QueryRowContext(ctx, `SELECT snapshot FROM catalog_commits WHERE id = $1`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(io.LimitReader(zr, 256<<20))
}

func (r *commitRepository) SetMessage(ctx context.Context, id int64, message string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE catalog_commits SET message = $2 WHERE id = $1`, id, message)
	return err
}
