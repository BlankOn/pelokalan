package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/store"
	"pelokalan/internal/repository"
)

type reservationRepository struct{ db *sql.DB }

// NewReservationRepository stores module reservations
func NewReservationRepository(db *sql.DB) repository.ReservationRepository {
	return &reservationRepository{db: db}
}

// reservationSelect reads active reservations with the holder and the newest save in their files of the
// module (module = file name before the first dot, lowercase, as service.ModuleOf)
const reservationSelect = `
	SELECT r.id, r.project_id, r.module, r.user_id, u.username, u.name, r.reserved_at, r.reminded_at,
	       GREATEST(r.reserved_at, COALESCE((SELECT MAX(c.updated_at) FROM catalogs c
	           WHERE c.project_id = r.project_id AND c.owner_id = r.user_id AND c.deleted_at IS NULL
	             AND split_part(lower(c.filename), '.', 1) = r.module), r.reserved_at))
	FROM module_reservations r JOIN users u ON u.id = r.user_id
	WHERE r.released_at IS NULL`

func scanReservations(rows *sql.Rows) ([]*models.ModuleReservation, error) {
	defer rows.Close()
	var out []*models.ModuleReservation
	for rows.Next() {
		m := &models.ModuleReservation{}
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.Module, &m.UserID, &m.Username, &m.Name, &m.ReservedAt, &m.RemindedAt, &m.LastSave); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *reservationRepository) Active(ctx context.Context, projectID int64) ([]*models.ModuleReservation, error) {
	rows, err := r.db.QueryContext(ctx, reservationSelect+` AND r.project_id = $1 ORDER BY r.reserved_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	return scanReservations(rows)
}

func (r *reservationRepository) ActiveFor(ctx context.Context, projectID int64, module string) (*models.ModuleReservation, error) {
	rows, err := r.db.QueryContext(ctx, reservationSelect+` AND r.project_id = $1 AND r.module = $2`, projectID, module)
	if err != nil {
		return nil, err
	}
	list, err := scanReservations(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, store.ErrNotFound
	}
	return list[0], nil
}

func (r *reservationRepository) Create(ctx context.Context, projectID int64, module string, userID int64) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO module_reservations (project_id, module, user_id) VALUES ($1, $2, $3)`,
		projectID, module, userID)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" { // someone else got it first
		return store.ErrConflict
	}
	return err
}

func (r *reservationRepository) Release(ctx context.Context, id int64, by *int64, reason string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE module_reservations SET released_at = NOW(), released_by = $2, release_reason = $3
		WHERE id = $1 AND released_at IS NULL`, id, by, reason)
	return err
}

func (r *reservationRepository) Expiring(ctx context.Context) ([]*models.ModuleReservation, error) {
	rows, err := r.db.QueryContext(ctx, reservationSelect+` AND EXISTS (SELECT 1 FROM projects p
		WHERE p.id = r.project_id AND p.reservations AND p.reservation_days > 0)`)
	if err != nil {
		return nil, err
	}
	return scanReservations(rows)
}

func (r *reservationRepository) SetReminded(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE module_reservations SET reminded_at = NOW() WHERE id = $1`, id)
	return err
}
