package langtools

import (
	"context"
	"database/sql"
)

type spellingRepository struct {
	db *sql.DB
}

// NewSpellingRepository creates the PostgreSQL spelling repository
func NewSpellingRepository(db *sql.DB) SpellingRepository {
	return &spellingRepository{db: db}
}

func (r *spellingRepository) CountWords(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM spelling_words`).Scan(&n)
	return n, err
}

func (r *spellingRepository) Words(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT word FROM spelling_words ORDER BY word`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *spellingRepository) ReplaceWords(ctx context.Context, words []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM spelling_words`); err != nil {
		return err
	}
	for start := 0; start < len(words); start += 5000 { // one statement per batch
		end := min(start+5000, len(words))
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO spelling_words (word) SELECT DISTINCT unnest($1::text[]) ON CONFLICT DO NOTHING`,
			words[start:end]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *spellingRepository) ExtraWords(ctx context.Context, projectID int64) ([]ExtraWord, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT x.word, COALESCE(u.username, ''), x.created_at
		 FROM spelling_extra x LEFT JOIN users u ON u.id = x.added_by
		 WHERE x.project_id = $1 ORDER BY x.word`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExtraWord
	for rows.Next() {
		var w ExtraWord
		if err := rows.Scan(&w.Word, &w.AddedBy, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *spellingRepository) AddExtraWord(ctx context.Context, projectID int64, word string, userID int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO spelling_extra (project_id, word, added_by) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		projectID, word, userID)
	return err
}

func (r *spellingRepository) DeleteExtraWord(ctx context.Context, projectID int64, word string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM spelling_extra WHERE project_id = $1 AND word = $2`, projectID, word)
	return err
}
