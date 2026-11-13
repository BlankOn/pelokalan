package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

type catalogRepository struct {
	db *sql.DB
}

// NewCatalogRepository creates a new PostgreSQL catalog repository
func NewCatalogRepository(db *sql.DB) repository.CatalogRepository {
	return &catalogRepository{db: db}
}

const catalogColumns = `id, project_id, owner_id, (SELECT username FROM users WHERE users.id = catalogs.owner_id),
	(SELECT slug FROM projects WHERE projects.id = catalogs.project_id), is_template, language, filename, header, header_comments,
	plural_forms, nplurals, created_by, created_at, updated_at, deleted_at, deleted_by,
	COALESCE((SELECT COALESCE(NULLIF(u.name, ''), u.username) FROM users u WHERE u.id = catalogs.deleted_by), ''), source_url, upstream_page`

func scanCatalog(row interface{ Scan(...any) error }) (*models.Catalog, error) {
	c := &models.Catalog{}
	err := row.Scan(&c.ID, &c.ProjectID, &c.OwnerID, &c.OwnerUsername, &c.ProjectSlug, &c.IsTemplate, &c.Language, &c.Filename, &c.Header, &c.HeaderComments,
		&c.PluralForms, &c.NPlurals, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.DeletedAt, &c.DeletedBy, &c.DeleterName, &c.SourceURL, &c.UpstreamPage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return c, err
}

// untranslatedExpr is true when at least one plural form is empty
const untranslatedExpr = `EXISTS (SELECT 1 FROM jsonb_array_elements_text(msgstr) s WHERE s = '')`

func (r *catalogRepository) Create(ctx context.Context, c *models.Catalog, entries []*models.Entry) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	err = tx.QueryRowContext(ctx,
		`INSERT INTO catalogs (project_id, owner_id, is_template, language, filename, header, header_comments, plural_forms, nplurals, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, created_at, updated_at`,
		c.ProjectID, c.OwnerID, c.IsTemplate, c.Language, c.Filename, c.Header, c.HeaderComments, c.PluralForms, c.NPlurals, c.CreatedBy,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return err
	}
	if err := insertEntries(ctx, tx, c.ID, entries); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET updated_at = NOW() WHERE id = $1`, c.ProjectID); err != nil {
		return err
	}
	return tx.Commit()
}

func insertEntries(ctx context.Context, tx *sql.Tx, catalogID int64, entries []*models.Entry) error {
	const cols = 15
	const batch = 400
	for start := 0; start < len(entries); start += batch {
		end := min(start+batch, len(entries))
		var sb strings.Builder
		sb.WriteString(`INSERT INTO entries (catalog_id, position, msgctxt, msgid, msgid_plural, msgstr, fuzzy, flags,
			translator_comments, extracted_comments, refs, previous, obsolete, updated_by, origin) VALUES `)
		args := make([]any, 0, (end-start)*cols)
		for i, e := range entries[start:end] {
			if i > 0 {
				sb.WriteByte(',')
			}
			n := len(args)
			fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
				n+1, n+2, n+3, n+4, n+5, n+6, n+7, n+8, n+9, n+10, n+11, n+12, n+13, n+14, n+15)
			msgstr, err := json.Marshal(e.MsgStr)
			if err != nil {
				return err
			}
			args = append(args, catalogID, e.Position, e.Context, e.MsgID, e.MsgIDPlural, string(msgstr), e.Fuzzy,
				e.Flags, e.TranslatorComments, e.ExtractedComments, e.References, e.Previous, e.Obsolete, e.UpdatedBy, e.Origin)
		}
		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func (r *catalogRepository) GetByID(ctx context.Context, id int64) (*models.Catalog, error) {
	return scanCatalog(r.db.QueryRowContext(ctx, `SELECT `+catalogColumns+` FROM catalogs WHERE id = $1`, id))
}

func (r *catalogRepository) GetByPath(ctx context.Context, projectID int64, ownerUsername, filename string) (*models.Catalog, error) {
	return scanCatalog(r.db.QueryRowContext(ctx,
		`SELECT `+catalogColumns+` FROM catalogs
		 WHERE project_id = $1 AND filename = $3 AND owner_id = (SELECT id FROM users WHERE username = $2)
		 ORDER BY deleted_at DESC NULLS FIRST, id DESC LIMIT 1`, // the current file, else the latest deleted one
		projectID, ownerUsername, filename))
}

func (r *catalogRepository) FilenameTaken(ctx context.Context, projectID, ownerID int64, filename string, excludeID int64) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM catalogs WHERE project_id = $1 AND owner_id = $2 AND filename = $3 AND id <> $4 AND deleted_at IS NULL`,
		projectID, ownerID, filename, excludeID).Scan(&n)
	return n > 0, err
}

func (r *catalogRepository) GetTemplate(ctx context.Context, projectID, ownerID int64) (*models.Catalog, error) {
	return scanCatalog(r.db.QueryRowContext(ctx,
		`SELECT `+catalogColumns+` FROM catalogs WHERE project_id = $1 AND owner_id = $2 AND is_template AND deleted_at IS NULL`, projectID, ownerID))
}

func (r *catalogRepository) ListByProject(ctx context.Context, projectID, ownerID int64) ([]*models.Catalog, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+catalogColumns+` FROM catalogs WHERE project_id = $1 AND owner_id = $2 AND deleted_at IS NULL ORDER BY is_template DESC, language`,
		projectID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Catalog
	for rows.Next() {
		c, err := scanCatalog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, c := range out {
		if c.Stats, err = r.Stats(ctx, c.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *catalogRepository) ListAllByProject(ctx context.Context, projectID int64) ([]*models.Catalog, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+catalogColumns+` FROM catalogs WHERE project_id = $1 AND deleted_at IS NULL ORDER BY owner_id, is_template DESC, language`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Catalog
	for rows.Next() {
		c, err := scanCatalog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, c := range out {
		if c.Stats, err = r.Stats(ctx, c.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *catalogRepository) UpdateHeader(ctx context.Context, c *models.Catalog) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE catalogs SET filename = $2, header = $3, header_comments = $4, plural_forms = $5, nplurals = $6, updated_at = NOW()
		 WHERE id = $1`, c.ID, c.Filename, c.Header, c.HeaderComments, c.PluralForms, c.NPlurals)
	return err
}

// Delete marks a file deleted; its entries and history are kept (read-only)
func (r *catalogRepository) Delete(ctx context.Context, id, byUserID int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE catalogs SET deleted_at = NOW(), deleted_by = $2 WHERE id = $1 AND deleted_at IS NULL`, id, byUserID)
	return err
}

func (r *catalogRepository) ReplaceEntries(ctx context.Context, c *models.Catalog, entries []*models.Entry) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM entries WHERE catalog_id = $1`, c.ID); err != nil {
		return err
	}
	if err := insertEntries(ctx, tx, c.ID, entries); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE catalogs SET filename = $2, header = $3, header_comments = $4, plural_forms = $5, nplurals = $6, updated_at = NOW()
		 WHERE id = $1`, c.ID, c.Filename, c.Header, c.HeaderComments, c.PluralForms, c.NPlurals); err != nil {
		return err
	}
	return tx.Commit()
}

const entryColumns = `e.id, e.catalog_id, e.position, e.msgctxt, e.msgid, e.msgid_plural, e.msgstr, e.fuzzy, e.flags,
	e.translator_comments, e.extracted_comments, e.refs, e.previous, e.obsolete, e.version, e.updated_by,
	COALESCE(NULLIF(u.name, ''), u.email, ''), e.updated_at, e.origin, e.replaced_msgstr`

func scanEntry(row interface{ Scan(...any) error }) (*models.Entry, error) {
	e := &models.Entry{}
	var msgstr, replaced []byte
	err := row.Scan(&e.ID, &e.CatalogID, &e.Position, &e.Context, &e.MsgID, &e.MsgIDPlural, &msgstr, &e.Fuzzy, &e.Flags,
		&e.TranslatorComments, &e.ExtractedComments, &e.References, &e.Previous, &e.Obsolete, &e.Version, &e.UpdatedBy,
		&e.UpdatedByName, &e.UpdatedAt, &e.Origin, &replaced)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(replaced) > 0 {
		_ = json.Unmarshal(replaced, &e.ReplacedMsgStr)
	}
	if err := json.Unmarshal(msgstr, &e.MsgStr); err != nil {
		return nil, err
	}
	if len(e.MsgStr) == 0 {
		e.MsgStr = []string{""}
	}
	return e, nil
}

func (r *catalogRepository) ListEntries(ctx context.Context, catalogID int64, includeObsolete bool) ([]*models.Entry, error) {
	q := `SELECT ` + entryColumns + ` FROM entries e LEFT JOIN users u ON u.id = e.updated_by WHERE e.catalog_id = $1`
	if !includeObsolete {
		q += ` AND NOT e.obsolete`
	}
	q += ` ORDER BY e.position, e.id`
	rows, err := r.db.QueryContext(ctx, q, catalogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *catalogRepository) GetEntry(ctx context.Context, id int64) (*models.Entry, error) {
	return scanEntry(r.db.QueryRowContext(ctx,
		`SELECT `+entryColumns+` FROM entries e LEFT JOIN users u ON u.id = e.updated_by WHERE e.id = $1`, id))
}

func (r *catalogRepository) UpdateEntry(ctx context.Context, e *models.Entry, userID int64) error {
	msgstr, err := json.Marshal(e.MsgStr)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var replaced any // NULL unless a machine replaced a translation
	if e.ReplacedMsgStr != nil {
		b, err := json.Marshal(e.ReplacedMsgStr)
		if err != nil {
			return err
		}
		replaced = string(b)
	}
	err = tx.QueryRowContext(ctx,
		`UPDATE entries SET msgstr = $3, fuzzy = $4, translator_comments = $5,
		        previous = CASE WHEN $4 THEN previous ELSE '' END,
		        version = version + 1, updated_by = $6, updated_at = NOW(), origin = $7, replaced_msgstr = $8
		 WHERE id = $1 AND version = $2
		 RETURNING version, updated_at`,
		e.ID, e.Version, string(msgstr), e.Fuzzy, e.TranslatorComments, userID, e.Origin, replaced,
	).Scan(&e.Version, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrConflict
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE catalogs SET updated_at = NOW() WHERE id = $1`, e.CatalogID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *catalogRepository) Stats(ctx context.Context, catalogID int64) (models.CatalogStats, error) {
	var s models.CatalogStats
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COUNT(*) FILTER (WHERE NOT fuzzy AND NOT `+untranslatedExpr+`),
		        COUNT(*) FILTER (WHERE fuzzy AND NOT `+untranslatedExpr+`)
		 FROM entries WHERE catalog_id = $1 AND NOT obsolete`, catalogID,
	).Scan(&s.Total, &s.Translated, &s.Fuzzy)
	s.Untranslated = s.Total - s.Translated - s.Fuzzy
	return s, err
}

func (r *catalogRepository) Suggestions(ctx context.Context, userID int64, language, msgid string, excludeEntryID int64, limit int) ([]repository.Suggestion, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT e.msgstr->>0 AS t, MIN(p.name), COUNT(*)
		 FROM entries e
		 JOIN catalogs c ON c.id = e.catalog_id
		 JOIN projects p ON p.id = c.project_id
		 WHERE c.owner_id = $1 AND c.language = $2 AND c.deleted_at IS NULL AND md5(e.msgid) = md5($3) AND e.msgid = $3 AND e.id <> $4
		   AND NOT e.fuzzy AND NOT e.obsolete AND e.msgid_plural = '' AND COALESCE(e.msgstr->>0, '') <> ''
		 GROUP BY t ORDER BY COUNT(*) DESC, t LIMIT $5`,
		userID, language, msgid, excludeEntryID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repository.Suggestion
	for rows.Next() {
		var s repository.Suggestion
		if err := rows.Scan(&s.Text, &s.ProjectName, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *catalogRepository) MemoryMatches(ctx context.Context, catalogID, userID int64, language string) (map[int64]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.id, src.txt
		 FROM entries t
		 JOIN (
		     SELECT DISTINCT ON (e.msgid) e.msgid, e.msgstr->>0 AS txt
		     FROM entries e
		     JOIN catalogs c ON c.id = e.catalog_id
		     WHERE c.owner_id = $2 AND c.language = $3 AND c.deleted_at IS NULL AND e.catalog_id <> $1 AND NOT e.fuzzy AND NOT e.obsolete
		       AND e.msgid_plural = '' AND COALESCE(e.msgstr->>0, '') <> ''
		     ORDER BY e.msgid, e.updated_at DESC
		 ) src ON src.msgid = t.msgid
		 WHERE t.catalog_id = $1 AND t.msgid_plural = '' AND NOT t.obsolete`,
		catalogID, userID, language)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var txt string
		if err := rows.Scan(&id, &txt); err != nil {
			return nil, err
		}
		out[id] = txt
	}
	return out, rows.Err()
}

func (r *catalogRepository) PreTranslate(ctx context.Context, catalogID, userID int64, language string, markFuzzy bool) (int, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE entries t
		 SET msgstr = jsonb_build_array(src.txt), fuzzy = $4, version = t.version + 1, updated_by = $2, updated_at = NOW(),
		     origin = 'memory'
		 FROM (
		     SELECT DISTINCT ON (e.msgid) e.msgid, e.msgstr->>0 AS txt
		     FROM entries e
		     JOIN catalogs c ON c.id = e.catalog_id
		     WHERE c.owner_id = $2 AND c.language = $3 AND c.deleted_at IS NULL AND e.catalog_id <> $1 AND NOT e.fuzzy AND NOT e.obsolete
		       AND e.msgid_plural = '' AND COALESCE(e.msgstr->>0, '') <> ''
		     ORDER BY e.msgid, e.updated_at DESC
		 ) src
		 WHERE t.catalog_id = $1 AND t.msgid = src.msgid AND t.msgid_plural = '' AND NOT t.obsolete
		   AND COALESCE(t.msgstr->>0, '') = ''`,
		catalogID, userID, language, markFuzzy)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		_, _ = r.db.ExecContext(ctx, `UPDATE catalogs SET updated_at = NOW() WHERE id = $1`, catalogID)
	}
	return int(n), nil
}

func (r *catalogRepository) SetUpstreamPage(ctx context.Context, id int64, page string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE catalogs SET upstream_page = $2 WHERE id = $1`, id, page)
	return err
}

func (r *catalogRepository) SetSourceURL(ctx context.Context, id int64, url string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE catalogs SET source_url = $2 WHERE id = $1`, id, url)
	return err
}
