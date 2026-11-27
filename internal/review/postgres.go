package review

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/store"
)

// Review requests of modules for repositories Pelokalan writes to (module_reviews, migration 048)

type reviewRepository struct{ db *sql.DB }

// NewReviewRepository stores module review requests
func NewRepository(db *sql.DB) Repository { return &reviewRepository{db: db} }

const reviewColumns = `r.id, r.project_id, r.catalog_id, r.group_id, r.module, r.repo_path, r.status, r.requested_by, r.note,
	r.entries, r.created_at, r.reviewed_by, r.reviewed_at, r.review_note, r.commit_sha, r.commit_url, r.error,
	ru.username, COALESCE(NULLIF(ru.name, ''), ru.username), COALESCE(rv.username, ''), c.filename, ou.username, p.slug`

const reviewFrom = ` FROM module_reviews r
	JOIN users ru ON ru.id = r.requested_by
	LEFT JOIN users rv ON rv.id = r.reviewed_by
	JOIN catalogs c ON c.id = r.catalog_id
	JOIN users ou ON ou.id = c.owner_id
	JOIN projects p ON p.id = r.project_id`

func scanReview(row interface{ Scan(...any) error }) (*models.ModuleReview, error) {
	r := &models.ModuleReview{}
	err := row.Scan(&r.ID, &r.ProjectID, &r.CatalogID, &r.GroupID, &r.Module, &r.RepoPath, &r.Status, &r.RequestedBy, &r.Note,
		&r.Entries, &r.CreatedAt, &r.ReviewedBy, &r.ReviewedAt, &r.ReviewNote, &r.CommitSHA, &r.CommitURL, &r.Error,
		&r.RequesterUsername, &r.RequesterName, &r.ReviewerUsername, &r.Filename, &r.OwnerUsername, &r.ProjectSlug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return r, err
}

func (s *reviewRepository) list(ctx context.Context, query string, args ...any) ([]*models.ModuleReview, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ModuleReview
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *reviewRepository) Create(ctx context.Context, r *models.ModuleReview) error {
	err := s.db.QueryRowContext(ctx, `INSERT INTO module_reviews (project_id, catalog_id, group_id, module, repo_path, status,
		requested_by, note, entries, reviewed_by, reviewed_at, commit_sha, commit_url, pushed_data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id, created_at`,
		r.ProjectID, r.CatalogID, r.GroupID, r.Module, r.RepoPath, r.Status, r.RequestedBy, r.Note, r.Entries,
		r.ReviewedBy, r.ReviewedAt, r.CommitSHA, r.CommitURL, r.PushedData).Scan(&r.ID, &r.CreatedAt)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return store.ErrConflict
	}
	return err
}

func (s *reviewRepository) Get(ctx context.Context, id int64) (*models.ModuleReview, error) {
	return scanReview(s.db.QueryRowContext(ctx, `SELECT `+reviewColumns+reviewFrom+` WHERE r.id = $1`, id))
}

func (s *reviewRepository) Open(ctx context.Context, projectID int64) ([]*models.ModuleReview, error) {
	return s.list(ctx, `SELECT `+reviewColumns+reviewFrom+` WHERE r.project_id = $1 AND r.status = 'requested'
		AND c.deleted_at IS NULL ORDER BY r.created_at, r.id`, projectID)
}

func (s *reviewRepository) Latest(ctx context.Context, catalogIDs []int64) (map[int64]*models.ModuleReview, error) {
	out := map[int64]*models.ModuleReview{}
	if len(catalogIDs) == 0 {
		return out, nil
	}
	list, err := s.list(ctx, `SELECT DISTINCT ON (r.catalog_id) `+reviewColumns+reviewFrom+`
		WHERE r.catalog_id = ANY($1) ORDER BY r.catalog_id, r.created_at DESC, r.id DESC`, catalogIDs)
	if err != nil {
		return nil, err
	}
	for _, r := range list {
		out[r.CatalogID] = r
	}
	return out, nil
}

func (s *reviewRepository) Update(ctx context.Context, r *models.ModuleReview) error {
	_, err := s.db.ExecContext(ctx, `UPDATE module_reviews SET status = $2, reviewed_by = $3, reviewed_at = $4, review_note = $5,
		commit_sha = $6, commit_url = $7, pushed_data = COALESCE($8, pushed_data), error = $9, entries = $10 WHERE id = $1`,
		r.ID, r.Status, r.ReviewedBy, r.ReviewedAt, r.ReviewNote, r.CommitSHA, r.CommitURL, r.PushedData, r.Error, r.Entries)
	return err
}

func (s *reviewRepository) LastPushed(ctx context.Context, catalogID int64) ([]byte, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT pushed_data FROM module_reviews WHERE catalog_id = $1 AND status = 'pushed'
		AND pushed_data IS NOT NULL ORDER BY reviewed_at DESC NULLS LAST, id DESC LIMIT 1`, catalogID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return data, err
}
