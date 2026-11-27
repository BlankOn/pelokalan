package pretranslate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/store"
	"pelokalan/pkg/ai"
)

const extractJobColumns = `id, project_id, group_id, user_id, status, model, total, processed, pairs, prompt_tokens,
	completion_tokens, cost::float8, error, created_at, finished_at`

func (r *aiRepository) CreateExtractJob(ctx context.Context, j *models.TermExtractionJob) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO term_extraction_jobs (project_id, group_id, user_id, status, model, total)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		j.ProjectID, j.GroupID, j.UserID, j.Status, j.Model, j.Total).Scan(&j.ID, &j.CreatedAt)
}

func (r *aiRepository) UpdateExtractJob(ctx context.Context, j *models.TermExtractionJob) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE term_extraction_jobs SET status = $2, total = $3, processed = $4, pairs = $5, prompt_tokens = $6,
		        completion_tokens = $7, cost = $8, error = $9, finished_at = $10 WHERE id = $1`,
		j.ID, j.Status, j.Total, j.Processed, j.Pairs, j.PromptTokens, j.CompletionTokens, j.Cost, j.Error, j.FinishedAt)
	return err
}

func (r *aiRepository) LatestExtractJob(ctx context.Context, projectID int64) (*models.TermExtractionJob, error) {
	j := &models.TermExtractionJob{}
	err := r.db.QueryRowContext(ctx, `SELECT `+extractJobColumns+` FROM term_extraction_jobs
		WHERE project_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, projectID).Scan(
		&j.ID, &j.ProjectID, &j.GroupID, &j.UserID, &j.Status, &j.Model, &j.Total, &j.Processed, &j.Pairs,
		&j.PromptTokens, &j.CompletionTokens, &j.Cost, &j.Error, &j.CreatedAt, &j.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return j, err
}

func (r *aiRepository) ExtractedPairs(ctx context.Context, language string, hashes []string) (map[string][]ai.Pair, error) {
	out := make(map[string][]ai.Pair, len(hashes))
	const chunk = 2000
	for start := 0; start < len(hashes); start += chunk {
		part := hashes[start:min(start+chunk, len(hashes))]
		rows, err := r.db.QueryContext(ctx,
			`SELECT string_hash, pairs FROM term_extractions WHERE language = $1 AND string_hash = ANY($2)`,
			language, "{"+strings.Join(part, ",")+"}")
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var hash string
			var data []byte
			if err := rows.Scan(&hash, &data); err != nil {
				rows.Close()
				return nil, err
			}
			var pairs []ai.Pair
			if err := json.Unmarshal(data, &pairs); err != nil {
				rows.Close()
				return nil, fmt.Errorf("term extraction %s: %w", hash, err)
			}
			out[hash] = pairs
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *aiRepository) SaveExtractedPairs(ctx context.Context, language, model string, pairs map[string][]ai.Pair) error {
	if len(pairs) == 0 {
		return nil
	}
	hashes := make([]string, 0, len(pairs))
	for h := range pairs {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	var sb strings.Builder
	sb.WriteString(`INSERT INTO term_extractions (language, string_hash, pairs, model) VALUES `)
	args := []any{language, model}
	for i, h := range hashes {
		if i > 0 {
			sb.WriteByte(',')
		}
		p := pairs[h]
		if p == nil {
			p = []ai.Pair{}
		}
		data, _ := json.Marshal(p)
		args = append(args, h, string(data))
		fmt.Fprintf(&sb, "($1, $%d, $%d::jsonb, $2)", len(args)-1, len(args))
	}
	sb.WriteString(` ON CONFLICT (language, string_hash) DO UPDATE SET pairs = EXCLUDED.pairs, model = EXCLUDED.model,
		created_at = NOW()`)
	_, err := r.db.ExecContext(ctx, sb.String(), args...)
	return err
}
