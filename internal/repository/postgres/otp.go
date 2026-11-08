package postgres

import (
	"context"
	"database/sql"
	"errors"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

type otpRepository struct {
	db *sql.DB
}

// NewOTPRepository creates a new PostgreSQL one-time code repository
func NewOTPRepository(db *sql.DB) repository.OTPRepository {
	return &otpRepository{db: db}
}

func (r *otpRepository) Create(ctx context.Context, o *models.OTPCode) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO otp_codes (email, purpose, code_hash, name, username, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		o.Email, o.Purpose, o.CodeHash, o.Name, o.Username, o.ExpiresAt,
	).Scan(&o.ID, &o.CreatedAt)
}

func (r *otpRepository) GetLatest(ctx context.Context, email, purpose string) (*models.OTPCode, error) {
	o := &models.OTPCode{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, email, purpose, code_hash, name, username, attempts, expires_at, used_at, created_at
		 FROM otp_codes WHERE email = $1 AND purpose = $2
		 ORDER BY created_at DESC, id DESC LIMIT 1`, email, purpose,
	).Scan(&o.ID, &o.Email, &o.Purpose, &o.CodeHash, &o.Name, &o.Username, &o.Attempts, &o.ExpiresAt, &o.UsedAt, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return o, err
}

func (r *otpRepository) LatestForEmail(ctx context.Context, email string) (*models.OTPCode, error) {
	o := &models.OTPCode{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, email, purpose, code_hash, name, username, attempts, expires_at, used_at, created_at
		 FROM otp_codes WHERE email = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, email,
	).Scan(&o.ID, &o.Email, &o.Purpose, &o.CodeHash, &o.Name, &o.Username, &o.Attempts, &o.ExpiresAt, &o.UsedAt, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return o, err
}

func (r *otpRepository) IncrementAttempts(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE otp_codes SET attempts = attempts + 1 WHERE id = $1`, id)
	return err
}

func (r *otpRepository) MarkUsed(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `UPDATE otp_codes SET used_at = NOW() WHERE id = $1 AND used_at IS NULL`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return repository.ErrConflict
	}
	return nil
}

func (r *otpRepository) InvalidateAll(ctx context.Context, email string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE otp_codes SET used_at = NOW() WHERE email = $1 AND used_at IS NULL`, email)
	return err
}

func (r *otpRepository) CleanupExpired(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM otp_codes WHERE expires_at < NOW() - INTERVAL '1 day'`)
	return err
}
