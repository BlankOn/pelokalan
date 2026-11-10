package postgres

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

type notificationRepository struct{ db *sql.DB }

// NewNotificationRepository creates the notification settings repository
func NewNotificationRepository(db *sql.DB) repository.NotificationRepository {
	return &notificationRepository{db: db}
}

func (r *notificationRepository) Settings(ctx context.Context, projectID int64, userIDs []int64) (map[int64]*models.NotificationSettings, error) {
	out := map[int64]*models.NotificationSettings{}
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT user_id, muted, array_to_string(off, ',') FROM notification_settings WHERE project_id = $1 AND user_id = ANY($2)`, projectID, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var muted bool
		var off string
		if err := rows.Scan(&id, &muted, &off); err != nil {
			return nil, err
		}
		s := &models.NotificationSettings{Muted: muted, Off: map[string]bool{}}
		for _, k := range strings.Split(off, ",") {
			if k != "" {
				s.Off[k] = true
			}
		}
		out[id] = s
	}
	return out, rows.Err()
}

func (r *notificationRepository) Save(ctx context.Context, userID, projectID int64, s *models.NotificationSettings) error {
	off := []string{}
	for k, v := range s.Off {
		if v {
			off = append(off, k)
		}
	}
	sort.Strings(off)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO notification_settings (user_id, project_id, muted, off, updated_at) VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (user_id, project_id) DO UPDATE SET muted = EXCLUDED.muted, off = EXCLUDED.off, updated_at = NOW()`,
		userID, projectID, s.Muted, off)
	return err
}
