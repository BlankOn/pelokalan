package postgres

import (
	"context"
	"database/sql"
	"encoding/json"

	"pelokalan/internal/repository"
	"pelokalan/internal/thesaurus"
)

type thesaurusRepository struct{ db *sql.DB }

// NewThesaurusRepository creates the thesaurus repository
func NewThesaurusRepository(db *sql.DB) repository.ThesaurusRepository {
	return &thesaurusRepository{db: db}
}

func (r *thesaurusRepository) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM thesaurus`).Scan(&n)
	return n, err
}

func (r *thesaurusRepository) All(ctx context.Context) ([]thesaurus.Entry, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT word, tag, synonyms, antonyms FROM thesaurus`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []thesaurus.Entry
	for rows.Next() {
		var e thesaurus.Entry
		var syn, ant []byte
		if err := rows.Scan(&e.Word, &e.Tag, &syn, &ant); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(syn, &e.Synonyms)
		_ = json.Unmarshal(ant, &e.Antonyms)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *thesaurusRepository) ReplaceAll(ctx context.Context, entries []thesaurus.Entry) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM thesaurus`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO thesaurus (word, tag, synonyms, antonyms) VALUES ($1, $2, $3, $4)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		syn, _ := json.Marshal(nonNil(e.Synonyms))
		ant, _ := json.Marshal(nonNil(e.Antonyms))
		if _, err := stmt.ExecContext(ctx, e.Word, e.Tag, syn, ant); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
