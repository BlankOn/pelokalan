package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"pelokalan/internal/glossary"
	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

type glossaryRepository struct {
	db *sql.DB
}

// NewGlossaryRepository creates a new PostgreSQL glossary repository
func NewGlossaryRepository(db *sql.DB) repository.GlossaryRepository {
	return &glossaryRepository{db: db}
}

// listSep joins module and context lists; it cannot appear in file names or contexts
const listSep = "\x1f"

func (r *glossaryRepository) GetOrCreateGroup(ctx context.Context, projectID int64, name, version string) (*models.ModuleGroup, error) {
	v := &models.ModuleGroup{ProjectID: projectID, Name: name, Version: version}
	// the no-op update makes RETURNING work when the row already exists
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO module_groups (project_id, name, version) VALUES ($1, $2, $3)
		 ON CONFLICT (project_id, name, version) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id, created_at`, projectID, name, version,
	).Scan(&v.ID, &v.CreatedAt)
	return v, err
}

const versionColumns = `v.id, v.project_id, v.name, v.version, v.created_at, v.sync_url, v.sync_language,
	v.feed_created_by, v.last_fetched_at, v.last_changed_at, v.last_fetch_error, v.fetch_etag, v.fetch_last_modified,
	v.fetch_content_hash, (SELECT COUNT(*) FROM module_group_imports i WHERE i.group_id = v.id)`

func scanGroup(row interface{ Scan(...any) error }) (*models.ModuleGroup, error) {
	v := &models.ModuleGroup{}
	err := row.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Version, &v.CreatedAt, &v.SyncURL, &v.SyncLanguage,
		&v.FeedCreatedBy, &v.LastFetchedAt, &v.LastChangedAt, &v.LastFetchError, &v.FetchETag, &v.FetchLastModified,
		&v.FetchContentHash, &v.ImportCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return v, err
}

func (r *glossaryRepository) listGroups(ctx context.Context, query string, args ...any) ([]*models.ModuleGroup, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ModuleGroup
	for rows.Next() {
		v, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *glossaryRepository) ListGroups(ctx context.Context, projectID int64) ([]*models.ModuleGroup, error) {
	return r.listGroups(ctx, `SELECT `+versionColumns+` FROM module_groups v WHERE v.project_id = $1
		ORDER BY v.created_at DESC, v.id DESC`, projectID)
}

func (r *glossaryRepository) GetGroup(ctx context.Context, id int64) (*models.ModuleGroup, error) {
	return scanGroup(r.db.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM module_groups v WHERE v.id = $1`, id))
}

func (r *glossaryRepository) DeleteGroup(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM module_groups WHERE id = $1`, id)
	return err
}

func (r *glossaryRepository) SetGroupSync(ctx context.Context, groupID int64, url, language string, userID *int64) error {
	// a new URL starts fresh so the next fetch downloads it fully
	_, err := r.db.ExecContext(ctx,
		`UPDATE module_groups SET sync_url = $2, sync_language = $3, feed_created_by = $4,
		        last_fetched_at = NULL, last_fetch_error = '', fetch_etag = '', fetch_last_modified = '', fetch_content_hash = ''
		 WHERE id = $1`, groupID, url, language, userID)
	return err
}

func (r *glossaryRepository) DueGroupSyncs(ctx context.Context, before time.Time) ([]*models.ModuleGroup, error) {
	return r.listGroups(ctx, `SELECT `+versionColumns+` FROM module_groups v
		WHERE v.sync_url <> '' AND (v.last_fetched_at IS NULL OR v.last_fetched_at < $1
		   -- imported before project files were kept: fetch again to fill them in
		   OR (EXISTS (SELECT 1 FROM module_group_imports i WHERE i.group_id = v.id)
		       AND NOT EXISTS (SELECT 1 FROM project_files f WHERE f.group_id = v.id)))
		ORDER BY v.last_fetched_at NULLS FIRST LIMIT 50`, before)
}

func (r *glossaryRepository) RecordFetch(ctx context.Context, v *models.ModuleGroup, changed bool) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE module_groups SET last_fetched_at = NOW(), last_fetch_error = $2, fetch_etag = $3,
		        fetch_last_modified = $4, fetch_content_hash = $5,
		        last_changed_at = CASE WHEN $6 THEN NOW() ELSE last_changed_at END
		 WHERE id = $1`,
		v.ID, v.LastFetchError, v.FetchETag, v.FetchLastModified, v.FetchContentHash, changed)
	return err
}

func (r *glossaryRepository) CreateGroupImport(ctx context.Context, imp *models.ModuleGroupImport, terms []repository.GlossaryTerm) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM module_group_imports WHERE group_id = $1 AND language = $2`, imp.GroupID, imp.Language); err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx,
		`INSERT INTO module_group_imports (project_id, group_id, filename, language, file_count, entry_count, term_count, uploaded_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		imp.ProjectID, imp.GroupID, imp.Filename, imp.Language, imp.FileCount, imp.EntryCount, imp.TermCount, imp.UploadedBy,
	).Scan(&imp.ID, &imp.CreatedAt)
	if err != nil {
		return err
	}

	const cols = 13
	const batch = 500
	for start := 0; start < len(terms); start += batch {
		end := min(start+batch, len(terms))
		var sb strings.Builder
		sb.WriteString(`INSERT INTO glossary_terms (import_id, project_id, group_id, language, source, source_norm, translation,
			translation_norm, word_count, occurrences, modules, contexts, source_hash) VALUES `)
		args := make([]any, 0, (end-start)*cols)
		for i, t := range terms[start:end] {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('(')
			for c := 1; c <= cols; c++ {
				if c > 1 {
					sb.WriteByte(',')
				}
				fmt.Fprintf(&sb, "$%d", len(args)+c)
			}
			sb.WriteByte(')')
			args = append(args, imp.ID, imp.ProjectID, imp.GroupID, imp.Language, t.Source, t.SourceNorm, t.Translation,
				t.TranslationNorm, t.WordCount, t.Occurrences,
				strings.Join(t.Modules, listSep), strings.Join(t.Contexts, listSep), glossary.TermHash(t.SourceNorm))
		}
		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const importColumns = `i.id, i.project_id, i.group_id, v.name || CASE WHEN v.version = '' THEN '' ELSE '-' || v.version END, i.filename, i.language, i.file_count, i.entry_count, i.term_count,
	i.uploaded_by, COALESCE(NULLIF(u.name, ''), u.email, ''), i.created_at`

func scanImport(row interface{ Scan(...any) error }) (*models.ModuleGroupImport, error) {
	imp := &models.ModuleGroupImport{}
	err := row.Scan(&imp.ID, &imp.ProjectID, &imp.GroupID, &imp.GroupLabel, &imp.Filename, &imp.Language, &imp.FileCount, &imp.EntryCount,
		&imp.TermCount, &imp.UploadedBy, &imp.UploaderName, &imp.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return imp, err
}

func (r *glossaryRepository) ListGroupImports(ctx context.Context, projectID int64) ([]*models.ModuleGroupImport, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+importColumns+` FROM module_group_imports i
		 JOIN module_groups v ON v.id = i.group_id
		 LEFT JOIN users u ON u.id = i.uploaded_by
		 WHERE i.project_id = $1 ORDER BY v.created_at DESC, i.language`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ModuleGroupImport
	for rows.Next() {
		imp, err := scanImport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, imp)
	}
	return out, rows.Err()
}

func (r *glossaryRepository) GetGroupImport(ctx context.Context, id int64) (*models.ModuleGroupImport, error) {
	return scanImport(r.db.QueryRowContext(ctx,
		`SELECT `+importColumns+` FROM module_group_imports i JOIN module_groups v ON v.id = i.group_id
		 LEFT JOIN users u ON u.id = i.uploaded_by WHERE i.id = $1`, id))
}

func (r *glossaryRepository) DeleteGroupImport(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM module_group_imports WHERE id = $1`, id)
	return err
}

func (r *glossaryRepository) Languages(ctx context.Context, projectID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT language FROM module_group_imports WHERE project_id = $1 GROUP BY language ORDER BY SUM(term_count) DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *glossaryRepository) Term(ctx context.Context, projectID int64, hash string) (*models.GlossaryTerm, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT MIN(t.source), MIN(t.word_count), v.id,
		        v.name || CASE WHEN v.version = '' THEN '' ELSE '-' || v.version END, t.language,
		        MIN(t.translation), SUM(t.occurrences),
		        string_agg(t.modules, '`+listSep+`'), string_agg(t.contexts, '`+listSep+`')
		 FROM glossary_terms t JOIN module_groups v ON v.id = t.group_id
		 WHERE t.project_id = $1 AND t.source_hash = $2
		 GROUP BY v.id, v.name, v.version, t.language, t.translation_norm
		 ORDER BY t.language, SUM(t.occurrences) DESC`, projectID, hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	term := &models.GlossaryTerm{Hash: hash}
	for rows.Next() {
		var row models.GlossaryTermRow
		var source, modules, contexts string
		var words int
		if err := rows.Scan(&source, &words, &row.GroupID, &row.GroupLabel, &row.Language, &row.Translation,
			&row.Count, &modules, &contexts); err != nil {
			return nil, err
		}
		if term.Source == "" || source < term.Source {
			term.Source = source
		}
		term.Words = words
		row.Modules = uniqueSplit(modules, 60)
		row.Contexts = uniqueSplit(contexts, 20)
		term.Rows = append(term.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(term.Rows) == 0 {
		return nil, repository.ErrNotFound
	}
	return term, nil
}

func (r *glossaryRepository) PreferredTerms(ctx context.Context, projectID int64, language string, groupID int64) ([]repository.PreferredTerm, error) {
	args := []any{projectID, language}
	filter := ""
	if groupID > 0 {
		args = append(args, groupID)
		filter = " AND group_id = $3"
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH t AS (
			SELECT source_norm, translation_norm, MIN(source) AS src, MIN(translation) AS tr,
			       SUM(occurrences) AS n, MIN(word_count) AS wc
			FROM glossary_terms
			WHERE project_id = $1 AND language = $2`+filter+`
			GROUP BY source_norm, translation_norm
		), ranked AS (
			SELECT *, SUM(n) OVER (PARTITION BY source_norm) AS total,
			       ROW_NUMBER() OVER (PARTITION BY source_norm ORDER BY n DESC, translation_norm) AS rk
			FROM t
		)
		SELECT src, tr, n::float8 / total, wc FROM ranked WHERE rk = 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repository.PreferredTerm
	for rows.Next() {
		var t repository.PreferredTerm
		if err := rows.Scan(&t.Source, &t.Translation, &t.Share, &t.Words); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (r *glossaryRepository) Search(ctx context.Context, q repository.GlossaryQuery) ([]*models.GlossaryGroup, int, error) {
	args := []any{q.ProjectID, q.Language}
	// scope applies to every glossary_terms reference (alias t)
	scope := "t.project_id = $1 AND t.language = $2"
	if q.GroupID > 0 {
		args = append(args, q.GroupID)
		scope += fmt.Sprintf(" AND t.group_id = $%d", len(args))
	}

	// A group matches when its source or any of its translations contains the
	// query. Everything is decided in one grouped scan (no IN subquery), which
	// keeps the plan fast even before the table has statistics.
	where := []string{"TRUE"}
	query := strings.ToLower(strings.TrimSpace(q.Query))
	if query != "" {
		args = append(args, "%"+escapeLike(query)+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(t.source_norm LIKE $%d OR t.translation_norm LIKE $%d)", n, n))
	}
	if q.MaxWords > 0 {
		args = append(args, q.MaxWords)
		where = append(where, fmt.Sprintf("t.word_count <= $%d", len(args)))
	}
	if q.Module != "" {
		// modules are stored joined by listSep; match a whole name
		args = append(args, listSep+q.Module+listSep)
		where = append(where, fmt.Sprintf("strpos('%s' || t.modules || '%s', $%d) > 0", listSep, listSep, len(args)))
	}
	having := "HAVING bool_or(" + strings.Join(where, " AND ") + ")"
	if q.Inconsistent {
		having += " AND COUNT(DISTINCT t.translation_norm) > 1"
	}

	groups := `SELECT t.source_norm, SUM(t.occurrences) AS total, COUNT(DISTINCT t.translation_norm) AS variants
		FROM glossary_terms t
		WHERE ` + scope + `
		GROUP BY t.source_norm ` + having

	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+groups+`) g`, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	order := glossaryOrder(q.Order, q.Dir)
	if query != "" {
		// exact matches first
		args = append(args, query)
		order = fmt.Sprintf("(g.source_norm = $%d) DESC, ", len(args)) + order
	}
	args = append(args, q.Limit, q.Offset)
	page := `SELECT g.source_norm, g.total, g.variants FROM (` + groups + `) g ORDER BY ` + order +
		fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx,
		`WITH page AS (`+page+`)
		 SELECT g.source_norm, g.total, MIN(t.source), MIN(t.translation), SUM(t.occurrences),
		        string_agg(t.modules, '`+listSep+`'), string_agg(t.contexts, '`+listSep+`'),
		        string_agg(DISTINCT v.name || CASE WHEN v.version = '' THEN '' ELSE '-' || v.version END, '`+listSep+`')
		 FROM page g
		 JOIN glossary_terms t ON `+scope+` AND t.source_norm = g.source_norm
		 JOIN module_groups v ON v.id = t.group_id
		 		 GROUP BY g.source_norm, g.total, g.variants, t.translation_norm
		 ORDER BY `+order+`, SUM(t.occurrences) DESC`,
		args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*models.GlossaryGroup
	var cur *models.GlossaryGroup
	curKey := ""
	for rows.Next() {
		var key, source, translation, modules, contexts, groupLabels string
		var groupTotal, count int
		if err := rows.Scan(&key, &groupTotal, &source, &translation, &count, &modules, &contexts, &groupLabels); err != nil {
			return nil, 0, err
		}
		if cur == nil || key != curKey {
			cur = &models.GlossaryGroup{Hash: glossary.TermHash(key), Source: source, Total: groupTotal}
			curKey = key
			out = append(out, cur)
		}
		cur.Translations = append(cur.Translations, models.GlossaryTranslation{
			Text:     translation,
			Count:    count,
			Modules:  uniqueSplit(modules, 40),
			Contexts: uniqueSplit(contexts, 10),
			Groups:   uniqueSplit(groupLabels, 40),
		})
	}
	return out, total, rows.Err()
}

// glossaryOrder builds the ORDER BY of a glossary search. Sorts: "alpha"
// (source text), "frequency" (uses) and "variants" (number of different
// translations, i.e. how inconsistent a term is; the default). dir is "asc" or
// "desc"; empty picks the natural direction of the sort.
func glossaryOrder(sort, dir string) string {
	d := map[string]string{"asc": "ASC", "desc": "DESC"}[dir]
	switch sort {
	case "alpha":
		if d == "" {
			d = "ASC"
		}
		return "g.source_norm " + d
	case "frequency":
		if d == "" {
			d = "DESC"
		}
		return "g.total " + d + ", g.source_norm"
	default: // "variants"
		if d == "" {
			d = "DESC"
		}
		return "g.variants " + d + ", g.total DESC, g.source_norm"
	}
}

func uniqueSplit(s string, max int) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range strings.Split(s, listSep) {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
		if len(out) >= max {
			break
		}
	}
	return out
}

func (r *glossaryRepository) Stamp(ctx context.Context, projectID int64, language string) (string, error) {
	var n, maxID, terms int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MAX(id), 0), COALESCE(SUM(term_count), 0)
		 FROM module_group_imports WHERE project_id = $1 AND language = $2`, projectID, language).Scan(&n, &maxID, &terms)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d-%d-%d", n, maxID, terms), nil
}

func (r *glossaryRepository) Modules(ctx context.Context, projectID int64, language string, groupID int64) ([]repository.ModuleCount, error) {
	args := []any{projectID, language, listSep}
	filter := ""
	if groupID > 0 {
		args = append(args, groupID)
		filter = " AND t.group_id = $4"
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT m, COUNT(*) FROM glossary_terms t, unnest(string_to_array(t.modules, $3)) m
		WHERE t.project_id = $1 AND t.language = $2`+filter+` AND m <> ''
		GROUP BY m ORDER BY m`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repository.ModuleCount
	for rows.Next() {
		var m repository.ModuleCount
		if err := rows.Scan(&m.Name, &m.Terms); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *glossaryRepository) AddProjectFiles(ctx context.Context, files []*models.ProjectFile) error {
	if len(files) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO project_files
		(import_id, project_id, group_id, filename, language, total, translated, fuzzy, size, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, f := range files {
		if _, err := stmt.ExecContext(ctx, f.ImportID, f.ProjectID, f.GroupID, f.Filename, f.Language,
			f.Total, f.Translated, f.Fuzzy, f.Size, f.Data); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const projectFileColumns = `f.id, f.import_id, f.project_id, f.group_id, v.name || '-' || v.version, f.filename, f.language,
	f.total, f.translated, f.fuzzy, f.size, f.created_at`

func (r *glossaryRepository) ListProjectFiles(ctx context.Context, projectID int64, query string, groupIDs []int64, limit, offset int) ([]*models.ProjectFile, int, error) {
	// every word must appear in the file name, language or module group label
	where := `f.project_id = $1`
	args := []any{projectID}
	if len(groupIDs) > 0 {
		args = append(args, groupIDs)
		where += fmt.Sprintf(` AND f.group_id = ANY($%d)`, len(args))
	}
	for _, w := range strings.Fields(query) {
		args = append(args, "%"+escapeLike(w)+"%")
		where += fmt.Sprintf(` AND (f.filename || ' ' || f.language || ' ' || v.name || '-' || v.version) ILIKE $%d`, len(args))
	}
	from := ` FROM project_files f JOIN module_groups v ON v.id = f.group_id WHERE ` + where
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*)`+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx, `SELECT `+projectFileColumns+from+`
		ORDER BY f.translated::float8 / GREATEST(f.total, 1), f.total - f.translated DESC, f.filename, f.id DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*models.ProjectFile
	for rows.Next() {
		f := &models.ProjectFile{}
		if err := rows.Scan(&f.ID, &f.ImportID, &f.ProjectID, &f.GroupID, &f.GroupLabel, &f.Filename, &f.Language,
			&f.Total, &f.Translated, &f.Fuzzy, &f.Size, &f.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, f)
	}
	return out, total, rows.Err()
}

func (r *glossaryRepository) GetProjectFile(ctx context.Context, projectID, id int64) (*models.ProjectFile, error) {
	f := &models.ProjectFile{}
	err := r.db.QueryRowContext(ctx, `SELECT `+projectFileColumns+`, f.data
		FROM project_files f JOIN module_groups v ON v.id = f.group_id
		WHERE f.project_id = $1 AND f.id = $2`, projectID, id).Scan(&f.ID, &f.ImportID, &f.ProjectID, &f.GroupID,
		&f.GroupLabel, &f.Filename, &f.Language, &f.Total, &f.Translated, &f.Fuzzy, &f.Size, &f.CreatedAt, &f.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return f, err
}

func (r *glossaryRepository) CountGroupFiles(ctx context.Context, groupID int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_files WHERE group_id = $1`, groupID).Scan(&n)
	return n, err
}

func (r *glossaryRepository) ProjectFilesForLanguage(ctx context.Context, projectID int64, language string) ([]*models.ProjectFile, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+projectFileColumns+`
		FROM project_files f JOIN module_groups v ON v.id = f.group_id
		WHERE f.project_id = $1 AND f.language = $2 ORDER BY f.created_at DESC, f.id DESC`, projectID, language)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ProjectFile
	for rows.Next() {
		f := &models.ProjectFile{}
		if err := rows.Scan(&f.ID, &f.ImportID, &f.ProjectID, &f.GroupID, &f.GroupLabel, &f.Filename, &f.Language,
			&f.Total, &f.Translated, &f.Fuzzy, &f.Size, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *glossaryRepository) GroupsWithFiles(ctx context.Context, projectID int64) ([]*models.ModuleGroupOption, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT v.id, v.name, v.version, COUNT(*)
		FROM project_files f JOIN module_groups v ON v.id = f.group_id
		WHERE f.project_id = $1 GROUP BY v.id, v.name, v.version`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ModuleGroupOption
	for rows.Next() {
		v := &models.ModuleGroupOption{}
		if err := rows.Scan(&v.ID, &v.Name, &v.Version, &v.Files); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *glossaryRepository) ProjectFileNames(ctx context.Context, projectID int64) (map[int64][]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT group_id, filename FROM project_files WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = append(out[id], name)
	}
	return out, rows.Err()
}

func (r *glossaryRepository) RecordGroupStats(ctx context.Context, groupID int64, minGap time.Duration) error {
	// one row per module group and language (all module groups when groupID is 0); skipped when the
	// last point is newer than minGap and has the same numbers
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO group_stats (project_id, group_id, language, total, translated, fuzzy)
		SELECT f.project_id, f.group_id, f.language, SUM(f.total), SUM(f.translated), SUM(f.fuzzy)
		  FROM project_files f
		 WHERE $1 = 0 OR f.group_id = $1
		 GROUP BY f.project_id, f.group_id, f.language
		HAVING NOT EXISTS (
		       SELECT 1 FROM (SELECT g.total, g.translated, g.fuzzy, g.recorded_at FROM group_stats g
		                       WHERE g.group_id = f.group_id AND g.language = f.language
		                       ORDER BY g.recorded_at DESC LIMIT 1) last
		        WHERE last.recorded_at > NOW() - make_interval(secs => $2)
		          AND last.total = SUM(f.total) AND last.translated = SUM(f.translated) AND last.fuzzy = SUM(f.fuzzy))`,
		groupID, minGap.Seconds())
	return err
}

func (r *glossaryRepository) GroupStats(ctx context.Context, groupID int64, language string, since time.Time) ([]models.GroupStat, error) {
	if language == "" { // the group's main language
		_ = r.db.QueryRowContext(ctx, `SELECT language FROM group_stats WHERE group_id = $1
			GROUP BY language ORDER BY MAX(total) DESC LIMIT 1`, groupID).Scan(&language)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT group_id, language, total, translated, fuzzy, recorded_at
		FROM group_stats WHERE group_id = $1 AND language = $2 AND recorded_at >= $3 ORDER BY recorded_at`,
		groupID, language, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.GroupStat
	for rows.Next() {
		var g models.GroupStat
		if err := rows.Scan(&g.GroupID, &g.Language, &g.Total, &g.Translated, &g.Fuzzy, &g.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *glossaryRepository) UpstreamCandidates(ctx context.Context) ([]models.UpstreamCandidate, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT f.filename, f.language, p.language, v.name, v.version, v.sync_url
		  FROM project_files f
		  JOIN module_groups v ON v.id = f.group_id
		  JOIN projects p ON p.id = f.project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.UpstreamCandidate
	for rows.Next() {
		var c models.UpstreamCandidate
		if err := rows.Scan(&c.Filename, &c.FileLanguage, &c.ProjectLanguage, &c.GroupName, &c.GroupVersion, &c.SyncURL); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *glossaryRepository) UpstreamStates(ctx context.Context, release, language string) (map[string]*models.UpstreamRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT release, language, stem, listed, module, branch, domain, state, translated, fuzzy, untranslated, po_file, checked_at
		  FROM upstream_states WHERE release = $1 AND language = $2`, release, language)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*models.UpstreamRecord{}
	for rows.Next() {
		u := &models.UpstreamRecord{}
		if err := rows.Scan(&u.Release, &u.Language, &u.Stem, &u.Listed, &u.Module, &u.Branch, &u.Domain, &u.State,
			&u.Translated, &u.Fuzzy, &u.Untranslated, &u.POFile, &u.CheckedAt); err != nil {
			return nil, err
		}
		out[u.Stem] = u
	}
	return out, rows.Err()
}

func (r *glossaryRepository) LinkedUpstreamPages(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT upstream_page FROM catalogs WHERE upstream_page <> '' AND deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *glossaryRepository) SaveUpstreamState(ctx context.Context, u *models.UpstreamRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO upstream_states (release, language, stem, listed, module, branch, domain, state,
		                             translated, fuzzy, untranslated, po_file, checked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (release, language, stem) DO UPDATE SET listed = EXCLUDED.listed, module = EXCLUDED.module,
		    branch = EXCLUDED.branch, domain = EXCLUDED.domain, state = EXCLUDED.state,
		    translated = EXCLUDED.translated, fuzzy = EXCLUDED.fuzzy, untranslated = EXCLUDED.untranslated,
		    po_file = EXCLUDED.po_file, checked_at = EXCLUDED.checked_at`,
		u.Release, u.Language, u.Stem, u.Listed, u.Module, u.Branch, u.Domain, u.State,
		u.Translated, u.Fuzzy, u.Untranslated, u.POFile, u.CheckedAt)
	return err
}
