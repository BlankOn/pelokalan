package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/repository"
)

type aiRepository struct {
	db *sql.DB
}

// NewAIRepository creates a new PostgreSQL AI repository
func NewAIRepository(db *sql.DB) repository.AIRepository {
	return &aiRepository{db: db}
}

func (r *aiRepository) GetSettings(ctx context.Context, projectID int64) (*models.AISettings, error) {
	s := &models.AISettings{}
	err := r.db.QueryRowContext(ctx,
		`SELECT project_id, base_url, model, api_key_enc, api_key_hint, price_input::float8, price_output::float8,
		        monthly_budget::float8, extra_context, updated_by, updated_at
		 FROM project_ai_settings WHERE project_id = $1`, projectID,
	).Scan(&s.ProjectID, &s.BaseURL, &s.Model, &s.APIKeyEnc, &s.APIKeyHint, &s.PriceInput, &s.PriceOutput,
		&s.MonthlyBudget, &s.ExtraContext, &s.UpdatedBy, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return s, err
}

func (r *aiRepository) SaveSettings(ctx context.Context, s *models.AISettings) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO project_ai_settings (project_id, base_url, model, api_key_enc, api_key_hint, price_input,
		        price_output, monthly_budget, updated_by, extra_context)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (project_id) DO UPDATE SET base_url = EXCLUDED.base_url, model = EXCLUDED.model,
		        api_key_enc = EXCLUDED.api_key_enc, api_key_hint = EXCLUDED.api_key_hint,
		        price_input = EXCLUDED.price_input, price_output = EXCLUDED.price_output,
		        monthly_budget = EXCLUDED.monthly_budget, extra_context = EXCLUDED.extra_context,
		        updated_by = EXCLUDED.updated_by, updated_at = NOW()
		 RETURNING updated_at`,
		s.ProjectID, s.BaseURL, s.Model, s.APIKeyEnc, s.APIKeyHint, s.PriceInput, s.PriceOutput, s.MonthlyBudget, s.UpdatedBy,
		s.ExtraContext,
	).Scan(&s.UpdatedAt)
}

func (r *aiRepository) DeleteSettings(ctx context.Context, projectID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM project_ai_settings WHERE project_id = $1`, projectID)
	return err
}

const jobColumns = `id, project_id, catalog_id, user_id, status, model, total, processed, from_memory, from_glossary, translated,
	flagged, skipped, prompt_tokens, completion_tokens, cost::float8, estimated_cost::float8, cancel_requested, error,
	created_at, finished_at`

func scanJob(row interface{ Scan(...any) error }) (*models.AIJob, error) {
	j := &models.AIJob{}
	err := row.Scan(&j.ID, &j.ProjectID, &j.CatalogID, &j.UserID, &j.Status, &j.Model, &j.Total, &j.Processed,
		&j.FromMemory, &j.FromGlossary, &j.Translated, &j.Flagged, &j.Skipped, &j.PromptTokens, &j.CompletionTokens, &j.Cost,
		&j.EstimatedCost, &j.CancelRequested, &j.Error, &j.CreatedAt, &j.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	return j, err
}

func (r *aiRepository) CreateJob(ctx context.Context, j *models.AIJob) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO ai_jobs (project_id, catalog_id, user_id, status, model, total, estimated_cost, single)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		j.ProjectID, j.CatalogID, j.UserID, j.Status, j.Model, j.Total, j.EstimatedCost, j.Single,
	).Scan(&j.ID, &j.CreatedAt)
}

func (r *aiRepository) UpdateJob(ctx context.Context, j *models.AIJob) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE ai_jobs SET status = $2, processed = $3, from_glossary = $4, translated = $5, flagged = $6,
		        skipped = $7, prompt_tokens = $8, completion_tokens = $9, cost = $10, error = $11, finished_at = $12,
		        from_memory = $13
		 WHERE id = $1`,
		j.ID, j.Status, j.Processed, j.FromGlossary, j.Translated, j.Flagged, j.Skipped, j.PromptTokens,
		j.CompletionTokens, j.Cost, j.Error, j.FinishedAt, j.FromMemory)
	return err
}

func (r *aiRepository) GetJob(ctx context.Context, id int64) (*models.AIJob, error) {
	return scanJob(r.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM ai_jobs WHERE id = $1`, id))
}

func (r *aiRepository) LatestJob(ctx context.Context, catalogID int64) (*models.AIJob, error) {
	return scanJob(r.db.QueryRowContext(ctx,
		`SELECT `+jobColumns+` FROM ai_jobs WHERE catalog_id = $1 AND NOT single ORDER BY created_at DESC, id DESC LIMIT 1`, catalogID))
}

func (r *aiRepository) RequestCancel(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE ai_jobs SET cancel_requested = TRUE WHERE id = $1 AND status = 'running'`, id)
	return err
}

func (r *aiRepository) IsCancelRequested(ctx context.Context, id int64) (bool, error) {
	var v bool
	err := r.db.QueryRowContext(ctx, `SELECT cancel_requested FROM ai_jobs WHERE id = $1`, id).Scan(&v)
	return v, err
}

func (r *aiRepository) Spend(ctx context.Context, projectID int64, since time.Time) (float64, error) {
	var v float64
	err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost), 0)::float8 FROM ai_jobs WHERE project_id = $1 AND created_at >= $2`,
		projectID, since).Scan(&v)
	return v, err
}

func (r *aiRepository) FailRunning(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE ai_jobs SET status = 'failed', error = 'interrupted by server restart', finished_at = NOW()
		 WHERE status = 'running'`)
	return err
}
