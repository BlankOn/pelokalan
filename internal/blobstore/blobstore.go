// Package blobstore stores files by content, like an object storage bucket.
// Keys are the hex SHA-256 of the content, so storing the same bytes twice
// keeps one copy. The Postgres backend can be swapped for an S3-compatible
// one without changing callers.
package blobstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// ErrNotFound is returned when a key does not exist
var ErrNotFound = errors.New("blob not found")

// Store is an object storage bucket
type Store interface {
	// Put stores data and returns its key (idempotent)
	Put(ctx context.Context, data []byte) (string, error)
	// Get returns the data of a key
	Get(ctx context.Context, key string) ([]byte, error)
	// Delete removes a key
	Delete(ctx context.Context, key string) error
	// GC removes blobs that no comment attachment or wiki file references and that are older than the age
	GC(ctx context.Context, olderThan time.Duration) (int64, error)
}

// Key returns the key data is stored under
func Key(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Postgres keeps blobs in the "blobs" table
type Postgres struct {
	db *sql.DB
}

// NewPostgres creates a Postgres-backed store
func NewPostgres(db *sql.DB) *Postgres { return &Postgres{db: db} }

func (p *Postgres) Put(ctx context.Context, data []byte) (string, error) {
	key := Key(data)
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO blobs (key, size, data) VALUES ($1, $2, $3) ON CONFLICT (key) DO NOTHING`,
		key, len(data), data)
	return key, err
}

func (p *Postgres) Get(ctx context.Context, key string) ([]byte, error) {
	var data []byte
	err := p.db.QueryRowContext(ctx, `SELECT data FROM blobs WHERE key = $1`, key).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return data, err
}

func (p *Postgres) Delete(ctx context.Context, key string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM blobs WHERE key = $1`, key)
	return err
}

func (p *Postgres) GC(ctx context.Context, olderThan time.Duration) (int64, error) {
	res, err := p.db.ExecContext(ctx,
		`DELETE FROM blobs b WHERE b.created_at < $1
		 AND NOT EXISTS (SELECT 1 FROM attachments a WHERE a.blob_key = b.key)
		 AND NOT EXISTS (SELECT 1 FROM wiki_files f WHERE f.blob_key = b.key)`,
		time.Now().Add(-olderThan))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
