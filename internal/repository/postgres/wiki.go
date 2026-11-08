package postgres

import (
	"context"
	"database/sql"
	"errors"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

type wikiRepository struct{ db *sql.DB }

// NewWikiRepository creates the wiki repository
func NewWikiRepository(db *sql.DB) repository.WikiRepository { return &wikiRepository{db: db} }

const wikiColumns = `w.id, w.project_id, w.parent_id, w.slug, w.title, w.position, w.created_by, w.updated_by,
	w.created_at, w.updated_at, COALESCE(NULLIF(u.name, ''), u.username, '')`

const wikiFrom = ` FROM wiki_pages w LEFT JOIN users u ON u.id = w.updated_by `

func scanWiki(row interface{ Scan(...any) error }, withBody bool) (*models.WikiPage, error) {
	p := &models.WikiPage{}
	dest := []any{&p.ID, &p.ProjectID, &p.ParentID, &p.Slug, &p.Title, &p.Position, &p.CreatedBy, &p.UpdatedBy,
		&p.CreatedAt, &p.UpdatedAt, &p.UpdaterName}
	if withBody {
		dest = append(dest, &p.Body)
	}
	err := row.Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return p, err
}

func (r *wikiRepository) ListPages(ctx context.Context, projectID int64) ([]*models.WikiPage, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+wikiColumns+wikiFrom+
		` WHERE w.project_id = $1 ORDER BY w.position, lower(w.title), w.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.WikiPage
	for rows.Next() {
		p, err := scanWiki(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *wikiRepository) GetPage(ctx context.Context, projectID int64, slug string) (*models.WikiPage, error) {
	return scanWiki(r.db.QueryRowContext(ctx, `SELECT `+wikiColumns+`, w.body`+wikiFrom+
		` WHERE w.project_id = $1 AND w.slug = $2`, projectID, slug), true)
}

func (r *wikiRepository) GetPageByID(ctx context.Context, projectID, id int64) (*models.WikiPage, error) {
	return scanWiki(r.db.QueryRowContext(ctx, `SELECT `+wikiColumns+`, w.body`+wikiFrom+
		` WHERE w.project_id = $1 AND w.id = $2`, projectID, id), true)
}

func (r *wikiRepository) SlugExists(ctx context.Context, projectID int64, slug string) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM wiki_pages WHERE project_id = $1 AND slug = $2)`, projectID, slug).Scan(&ok)
	return ok, err
}

func (r *wikiRepository) CreatePage(ctx context.Context, p *models.WikiPage) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO wiki_pages (project_id, parent_id, slug, title, body, position, created_by, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $7) RETURNING id, created_at, updated_at`,
		p.ProjectID, p.ParentID, p.Slug, p.Title, p.Body, p.Position, p.CreatedBy,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

func (r *wikiRepository) UpdatePage(ctx context.Context, p *models.WikiPage, version int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cur sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT updated_at FROM wiki_pages WHERE id = $1 AND project_id = $2 FOR UPDATE`,
		p.ID, p.ProjectID).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrNotFound
	} else if err != nil {
		return err
	}
	// timestamps come back with microsecond precision
	if cur.Time.UnixMicro() != version/1000 {
		return repository.ErrConflict
	}
	var oldSlug string
	if err := tx.QueryRowContext(ctx, `SELECT slug FROM wiki_pages WHERE id = $1`, p.ID).Scan(&oldSlug); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx,
		`UPDATE wiki_pages SET slug = $2, title = $3, body = $4, updated_by = $5, updated_at = NOW()
		 WHERE id = $1 RETURNING updated_at`,
		p.ID, p.Slug, p.Title, p.Body, p.UpdatedBy).Scan(&p.UpdatedAt); err != nil {
		return err
	}
	if oldSlug != p.Slug {
		// the old address now points here; the new one is a real page again
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO wiki_redirects (project_id, slug, page_id) VALUES ($1, $2, $3)
			 ON CONFLICT (project_id, slug) DO UPDATE SET page_id = EXCLUDED.page_id`, p.ProjectID, oldSlug, p.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM wiki_redirects WHERE project_id = $1 AND slug = $2`, p.ProjectID, p.Slug); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *wikiRepository) DeletePage(ctx context.Context, projectID, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM wiki_pages WHERE id = $1 AND project_id = $2`, id, projectID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (r *wikiRepository) HasChildren(ctx context.Context, id int64) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM wiki_pages WHERE parent_id = $1)`, id).Scan(&ok)
	return ok, err
}

func (r *wikiRepository) CreateFile(ctx context.Context, f *models.WikiFile) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO wiki_files (project_id, blob_key, filename, content_type, size, uploaded_by)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		f.ProjectID, f.BlobKey, f.Filename, f.ContentType, f.Size, f.UploadedBy,
	).Scan(&f.ID, &f.CreatedAt)
}

func (r *wikiRepository) GetFile(ctx context.Context, projectID, id int64) (*models.WikiFile, error) {
	f := &models.WikiFile{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, project_id, blob_key, filename, content_type, size, uploaded_by, created_at
		 FROM wiki_files WHERE id = $1 AND project_id = $2`, id, projectID,
	).Scan(&f.ID, &f.ProjectID, &f.BlobKey, &f.Filename, &f.ContentType, &f.Size, &f.UploadedBy, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return f, err
}

func (r *wikiRepository) Reorder(ctx context.Context, projectID int64, parentID *int64, ids []int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE wiki_pages SET parent_id = $3, position = $4 WHERE id = $1 AND project_id = $2`,
			id, projectID, parentID, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *wikiRepository) Redirect(ctx context.Context, projectID int64, slug string) (string, error) {
	var cur string
	err := r.db.QueryRowContext(ctx,
		`SELECT w.slug FROM wiki_redirects x JOIN wiki_pages w ON w.id = x.page_id
		 WHERE x.project_id = $1 AND x.slug = $2`, projectID, slug).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) {
		return "", repository.ErrNotFound
	}
	return cur, err
}
