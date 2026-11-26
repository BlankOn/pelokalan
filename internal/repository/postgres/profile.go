package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"pelokalan/internal/models"
)

// A contribution is an entry save that did translation work, by the same rules as the Statistik tab:
// a translation given, a string that needed review confirmed, a translated string changed, or a
// machine draft saved (kept or corrected).
const (
	evNew        = `(kind = 'entry.saved' AND status_before = 'untranslated' AND status_after <> 'untranslated')`
	evReviewed   = `(kind = 'entry.saved' AND status_before = 'fuzzy' AND status_after = 'translated')`
	evEdited     = `(kind = 'entry.saved' AND status_before = 'translated' AND text_changed)`
	evContribute = `(kind = 'entry.saved' AND (` + evNew + ` OR ` + evReviewed + ` OR ` + evEdited + ` OR origin <> ''))`
)

func (r *activityRepository) UserDays(ctx context.Context, userID int64, tz string) ([]models.ProfileDay, error) {
	rows, err := r.db.QueryContext(ctx, `
		WITH ev AS (
			SELECT created_at AT TIME ZONE $2 AS t, kind, status_before, status_after, origin, text_changed, words
			FROM activity_events WHERE user_id = $1 AND kind = 'entry.saved')
		SELECT t::date,
		       COUNT(*) FILTER (WHERE `+evContribute+`),
		       COUNT(*) FILTER (WHERE `+evNew+`),
		       COALESCE(SUM(words) FILTER (WHERE `+evContribute+`), 0),
		       COUNT(*) FILTER (WHERE `+evReviewed+`),
		       COUNT(*) FILTER (WHERE `+evEdited+`),
		       COUNT(*) FILTER (WHERE `+evContribute+` AND EXTRACT(HOUR FROM t) < 5),
		       COUNT(*) FILTER (WHERE `+evContribute+` AND EXTRACT(HOUR FROM t) BETWEEN 5 AND 6)
		FROM ev GROUP BY 1 HAVING COUNT(*) FILTER (WHERE `+evContribute+`) > 0 ORDER BY 1`, userID, tz)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ProfileDay
	for rows.Next() {
		var d models.ProfileDay
		if err := rows.Scan(&d.Day, &d.Contributions, &d.Translated, &d.Words, &d.Reviewed, &d.Edited, &d.Night, &d.Early); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *activityRepository) UserTotals(ctx context.Context, userID int64) (*models.ProfileTotals, error) {
	t := &models.ProfileTotals{}
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.slug, p.name, COUNT(*), COUNT(*) FILTER (WHERE `+evNew+`), MAX(a.created_at)
		FROM activity_events a JOIN projects p ON p.id = a.project_id
		WHERE a.user_id = $1 AND `+evContribute+`
		GROUP BY p.id, p.slug, p.name ORDER BY 4 DESC, 6 DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p := &models.ProfileProject{}
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Contributions, &p.Translated, &p.LastActive); err != nil {
			return nil, err
		}
		t.Projects = append(t.Projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var first sql.NullTime
	err = r.db.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(DISTINCT catalog_id) FROM activity_events WHERE user_id = $1 AND `+evContribute+`),
		  (SELECT COUNT(DISTINCT language) FROM activity_events WHERE user_id = $1 AND language <> '' AND `+evContribute+`),
		  -- completed files: deleted ones too (a soft delete keeps the entries), so tidying up never takes a badge away
		  (SELECT COUNT(*) FROM catalogs c WHERE c.owner_id = $1 AND NOT c.is_template
		     AND EXISTS (SELECT 1 FROM entries e WHERE e.catalog_id = c.id AND NOT e.obsolete)
		     AND NOT EXISTS (SELECT 1 FROM entries e WHERE e.catalog_id = c.id AND NOT e.obsolete
		                     AND (e.fuzzy OR e.origin = 'ai' OR `+untranslatedExpr+`))
		     AND EXISTS (SELECT 1 FROM activity_events a WHERE a.user_id = $1 AND a.catalog_id = c.id AND `+evContribute+`)),
		  (SELECT COUNT(*) FROM comments WHERE author_id = $1 AND deleted_at IS NULL),
		  (SELECT COUNT(*) FROM discussions WHERE created_by = $1 AND subject_type = 'topic'),
		  (SELECT COUNT(*) FROM wiki_edits WHERE user_id = $1),
		  (SELECT COUNT(*) FROM module_context_edits WHERE user_id = $1),
		  (SELECT COUNT(*) FROM catalog_commits WHERE author_id = $1 AND kind = 'manual'),
		  (SELECT COUNT(*) FROM upstream_landings WHERE user_id = $1),
		  (SELECT COALESCE(SUM(words), 0) FROM upstream_landings WHERE user_id = $1),
		  (SELECT COUNT(DISTINCT (project_id, module)) FROM upstream_landings WHERE user_id = $1),
		  (SELECT MIN(created_at) FROM activity_events WHERE user_id = $1 AND `+evContribute+`)`, userID,
	).Scan(&t.Files, &t.Languages, &t.CompletedFiles, &t.Comments, &t.Topics, &t.WikiEdits, &t.ContextEdits, &t.Commits,
		&t.Landed, &t.LandedWords, &t.LandedModules, &first)
	if err != nil {
		return nil, err
	}
	if first.Valid {
		t.FirstActive = &first.Time
	}
	return t, nil
}

func (r *activityRepository) AddLandings(ctx context.Context, landings []models.UpstreamLanding) (int, error) {
	added := 0
	for _, l := range landings {
		res, err := r.db.ExecContext(ctx, `
			INSERT INTO upstream_landings (user_id, project_id, catalog_id, group_id, entry_key, module, filename, words, landed_at)
			VALUES ($1, $2, $3, NULLIF($4, 0), $5, $6, $7, $8, COALESCE($9, NOW()))
			ON CONFLICT (user_id, catalog_id, entry_key) DO NOTHING`,
			l.UserID, l.ProjectID, l.CatalogID, l.GroupID, l.EntryKey, l.Module, l.Filename, l.Words, l.LandedAt)
		if err != nil {
			return added, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}
	return added, nil
}

// moduleWorkKinds are the filters and counts of UserModuleWork
var moduleWorkKinds = map[string][2]string{ // kind: {filter, count}
	"contrib":  {evContribute, "COUNT(*)"},
	"new":      {evNew, "COUNT(*)"},
	"words":    {evContribute, "COALESCE(SUM(a.words), 0)"},
	"reviewed": {evReviewed, "COUNT(*)"},
	"edited":   {evEdited, "COUNT(*)"},
	"night":    {evContribute + ` AND EXTRACT(HOUR FROM a.created_at AT TIME ZONE $2) < 5`, "COUNT(*)"},
	"early":    {evContribute + ` AND EXTRACT(HOUR FROM a.created_at AT TIME ZONE $2) BETWEEN 5 AND 6`, "COUNT(*)"},
	"weekend":  {evContribute + ` AND EXTRACT(ISODOW FROM a.created_at AT TIME ZONE $2) >= 6`, "COUNT(*)"},
}

func scanModuleWork(rows *sql.Rows) ([]*models.ModuleWork, error) {
	defer rows.Close()
	var out []*models.ModuleWork
	for rows.Next() {
		m := &models.ModuleWork{}
		if err := rows.Scan(&m.ProjectSlug, &m.ProjectName, &m.Module, &m.Filename, &m.Count, &m.First, &m.Last); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *activityRepository) UserModuleWork(ctx context.Context, userID int64, tz, kind string, from, to *time.Time, limit int) ([]*models.ModuleWork, error) {
	k, ok := moduleWorkKinds[kind]
	if !ok {
		k = moduleWorkKinds["contrib"]
	}
	// the module is the file name before the first dot (service.ModuleOf); files deleted since keep their name
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.slug, p.name, split_part(lower(COALESCE(c.filename, '')), '.', 1) AS module,
		       (array_agg(COALESCE(c.filename, '') ORDER BY a.created_at DESC))[1],
		       `+k[1]+`, MIN(a.created_at), MAX(a.created_at)
		FROM activity_events a
		JOIN projects p ON p.id = a.project_id
		LEFT JOIN catalogs c ON c.id = a.catalog_id
		WHERE a.user_id = $1 AND ($2::text IS NOT NULL) AND `+k[0]+`
		  AND ($3::timestamptz IS NULL OR a.created_at >= $3) AND ($4::timestamptz IS NULL OR a.created_at < $4)
		GROUP BY p.slug, p.name, module
		ORDER BY 5 DESC, 7 DESC LIMIT $5`, userID, tz, from, to, limit)
	if err != nil {
		return nil, err
	}
	return scanModuleWork(rows)
}

func (r *activityRepository) UserLandingWork(ctx context.Context, userID int64, limit int) ([]*models.ModuleWork, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.slug, p.name, l.module, (array_agg(l.filename ORDER BY l.landed_at DESC))[1],
		       COUNT(*), MIN(l.landed_at), MAX(l.landed_at)
		FROM upstream_landings l JOIN projects p ON p.id = l.project_id
		WHERE l.user_id = $1
		GROUP BY p.slug, p.name, l.module ORDER BY 5 DESC, 7 DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	return scanModuleWork(rows)
}

// communitySQL selects (project id, time) of a person's community work of each kind
var communitySQL = map[string]string{
	"comments": `SELECT d.project_id, c.created_at FROM comments c JOIN discussions d ON d.id = c.discussion_id WHERE c.author_id = $1 AND c.deleted_at IS NULL`,
	"topics":   `SELECT project_id, created_at FROM discussions WHERE created_by = $1 AND subject_type = 'topic'`,
	"wiki":     `SELECT w.project_id, e.created_at FROM wiki_edits e JOIN wiki_pages w ON w.id = e.page_id WHERE e.user_id = $1`,
	"context":  `SELECT m.project_id, e.created_at FROM module_context_edits e JOIN module_contexts m ON m.id = e.context_id WHERE e.user_id = $1`,
	"commits":  `SELECT c.project_id, k.created_at FROM catalog_commits k JOIN catalogs c ON c.id = k.catalog_id WHERE k.author_id = $1 AND k.kind = 'manual'`,
}

func (r *activityRepository) UserCommunityWork(ctx context.Context, userID int64, kind string, limit int) ([]*models.ModuleWork, error) {
	q, ok := communitySQL[kind]
	if !ok {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.slug, p.name, '', '', COUNT(*), MIN(w.created_at), MAX(w.created_at)
		FROM (`+q+`) w(project_id, created_at) JOIN projects p ON p.id = w.project_id
		GROUP BY p.slug, p.name ORDER BY 5 DESC, 7 DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	return scanModuleWork(rows)
}

func (r *activityRepository) NthTime(ctx context.Context, userID int64, kind string, n int) (*time.Time, error) {
	var q string
	switch kind {
	case "landed":
		q = `SELECT landed_at FROM upstream_landings WHERE user_id = $1 ORDER BY landed_at OFFSET $2 LIMIT 1`
	case "landed_modules": // when the nth distinct module first had a string accepted
		q = `SELECT first FROM (SELECT MIN(landed_at) AS first FROM upstream_landings WHERE user_id = $1
		     GROUP BY project_id, module) m ORDER BY first OFFSET $2 LIMIT 1`
	default:
		c, ok := communitySQL[kind]
		if !ok {
			return nil, nil
		}
		q = `SELECT created_at FROM (` + c + `) w(project_id, created_at) ORDER BY created_at OFFSET $2 LIMIT 1`
	}
	var t time.Time
	err := r.db.QueryRowContext(ctx, q, userID, max(0, n-1)).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *activityRepository) ClaimOneTimeJob(ctx context.Context, name string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `INSERT INTO one_time_jobs (name) VALUES ($1) ON CONFLICT DO NOTHING`, name)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (r *activityRepository) BadgeUsers(ctx context.Context) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT user_id FROM activity_events WHERE user_id IS NOT NULL AND `+evContribute+`
		UNION SELECT user_id FROM upstream_landings
		UNION SELECT author_id FROM comments WHERE author_id IS NOT NULL AND deleted_at IS NULL
		UNION SELECT user_id FROM wiki_edits WHERE user_id IS NOT NULL
		UNION SELECT user_id FROM module_context_edits WHERE user_id IS NOT NULL
		UNION SELECT author_id FROM catalog_commits WHERE author_id IS NOT NULL AND kind = 'manual'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *activityRepository) AwardedTiers(ctx context.Context, userID int64) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT badge, MAX(tier) FROM badge_awards WHERE user_id = $1 GROUP BY badge`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var b string
		var t int
		if err := rows.Scan(&b, &t); err != nil {
			return nil, err
		}
		out[b] = t
	}
	return out, rows.Err()
}

func (r *activityRepository) AddAwards(ctx context.Context, userID int64, awards []models.BadgeAward, emailed bool) error {
	for _, a := range awards {
		if _, err := r.db.ExecContext(ctx, `INSERT INTO badge_awards (user_id, badge, tier, emailed) VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING`, userID, a.Badge, a.Tier, emailed); err != nil {
			return err
		}
	}
	return nil
}
