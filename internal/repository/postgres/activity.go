package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"pelokalan/internal/models"
)

type activityRepository struct{ db *sql.DB }

// NewActivityRepository creates the activity log repository
func NewActivityRepository(db *sql.DB) *activityRepository { return &activityRepository{db: db} }

func (r *activityRepository) Record(ctx context.Context, e *models.ActivityEvent) error {
	data := []byte("{}")
	if len(e.Data) > 0 {
		var err error
		if data, err = json.Marshal(e.Data); err != nil {
			return err
		}
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO activity_events (kind, user_id, project_id, catalog_id, entry_id, language,
		        status_before, status_after, origin, text_changed, words, data)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		e.Kind, e.UserID, e.ProjectID, e.CatalogID, e.EntryID, e.Language,
		e.StatusBefore, e.StatusAfter, e.Origin, e.TextChanged, e.Words, data)
	return err
}

func (r *activityRepository) Summary(ctx context.Context, projectID int64, since *time.Time) ([]*models.ActivitySummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT * FROM (
		SELECT a.user_id, u.username, u.name,
		       COUNT(*) FILTER (WHERE kind = 'entry.saved' AND status_before = 'untranslated' AND status_after <> 'untranslated'),
		       COALESCE(SUM(words) FILTER (WHERE kind = 'entry.saved' AND status_before = 'untranslated' AND status_after <> 'untranslated'), 0),
		       COUNT(*) FILTER (WHERE kind = 'entry.saved' AND status_before = 'fuzzy' AND status_after = 'translated'),
		       COUNT(*) FILTER (WHERE kind = 'entry.saved' AND status_before = 'translated' AND text_changed),
		       COUNT(*) FILTER (WHERE kind = 'entry.saved' AND origin <> '' AND NOT text_changed),
		       COUNT(*) FILTER (WHERE kind = 'entry.saved' AND origin <> '' AND text_changed),
		       COUNT(*) FILTER (WHERE kind = 'comment.posted'),
		       COUNT(*) FILTER (WHERE kind = 'commit.created'),
		       MAX(a.created_at)
		FROM activity_events a JOIN users u ON u.id = a.user_id
		WHERE a.project_id = $1 AND ($2::timestamptz IS NULL OR a.created_at >= $2)
		GROUP BY a.user_id, u.username, u.name
		) t(user_id, username, name, translated, words, reviewed, edited, approved, fixed, comments, commits, last_active)
		ORDER BY translated + reviewed + edited + fixed + approved DESC, comments DESC, last_active DESC`, projectID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ActivitySummary
	for rows.Next() {
		s := &models.ActivitySummary{}
		if err := rows.Scan(&s.UserID, &s.Username, &s.Name, &s.Translated, &s.TranslatedWords, &s.Reviewed,
			&s.Edited, &s.DraftsApproved, &s.DraftsFixed, &s.Comments, &s.Commits, &s.LastActive); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *activityRepository) Since(ctx context.Context, projectID int64) (*time.Time, error) {
	var t sql.NullTime
	if err := r.db.QueryRowContext(ctx, `SELECT MIN(created_at) FROM activity_events WHERE project_id = $1`, projectID).Scan(&t); err != nil {
		return nil, err
	}
	if !t.Valid {
		return nil, nil
	}
	return &t.Time, nil
}

func (r *activityRepository) DailyTranslated(ctx context.Context, projectID int64) ([]models.ActivityDay, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT date_trunc('day', created_at), COUNT(*), COALESCE(SUM(words), 0)
		FROM activity_events
		WHERE project_id = $1 AND kind = 'entry.saved' AND status_before = 'untranslated' AND status_after <> 'untranslated'
		GROUP BY 1 ORDER BY 1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ActivityDay
	for rows.Next() {
		var d models.ActivityDay
		if err := rows.Scan(&d.Day, &d.Strings, &d.Words); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
