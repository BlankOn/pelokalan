package postgres

import (
	"context"
	"database/sql"

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
		       COALESCE(SUM(words) FILTER (WHERE `+evNew+`), 0),
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
		  (SELECT COUNT(*) FROM catalogs c WHERE c.owner_id = $1 AND c.deleted_at IS NULL AND NOT c.is_template
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
			INSERT INTO upstream_landings (user_id, project_id, catalog_id, group_id, entry_key, module, filename, words)
			VALUES ($1, $2, $3, NULLIF($4, 0), $5, $6, $7, $8)
			ON CONFLICT (user_id, catalog_id, entry_key) DO NOTHING`,
			l.UserID, l.ProjectID, l.CatalogID, l.GroupID, l.EntryKey, l.Module, l.Filename, l.Words)
		if err != nil {
			return added, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}
	return added, nil
}
