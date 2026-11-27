// Package testdb gives integration tests a throwaway PostgreSQL database.
//
// Tests run against the server named by TEST_DATABASE_URL (for example
// postgres://postgres:postgres@localhost:5433/postgres?sslmode=disable; `make test-integration`
// sets it from .env). Each test package gets its own new database named pelokalan_test_<random>,
// migrated from ./migrations and dropped when the package's tests end, so the development
// database is never touched. Without TEST_DATABASE_URL the integration tests are skipped.
package testdb

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	"pelokalan/internal/platform/config"
)

// EnvURL is the environment variable naming the PostgreSQL server for integration tests
const EnvURL = "TEST_DATABASE_URL"

// prefix marks the databases this package creates; only those are ever dropped
const prefix = "pelokalan_test_"

// DB is one throwaway database
type DB struct {
	Config config.DatabaseConfig
	SQL    *sql.DB
	admin  *sql.DB
}

// Available reports whether integration tests can run (TEST_DATABASE_URL is set)
func Available() bool { return os.Getenv(EnvURL) != "" }

// Root is the project directory (the one holding go.mod), found from the working directory
func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("testdb: go.mod not found above the working directory")
		}
		dir = parent
	}
}

// Create makes a new empty database on the TEST_DATABASE_URL server. The caller migrates it
// (app.Migrate) and calls Drop at the end.
func Create() (*DB, error) {
	raw := os.Getenv(EnvURL)
	if raw == "" {
		return nil, fmt.Errorf("testdb: %s is not set", EnvURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("testdb: %s: %w", EnvURL, err)
	}
	port := 5432
	if p := u.Port(); p != "" {
		if port, err = strconv.Atoi(p); err != nil {
			return nil, fmt.Errorf("testdb: port: %w", err)
		}
	}
	pass, _ := u.User.Password()
	cfg := config.DatabaseConfig{
		Host:     u.Hostname(),
		Port:     port,
		User:     u.User.Username(),
		Password: pass,
		Database: strings.TrimPrefix(u.Path, "/"),
		SSLMode:  u.Query().Get("sslmode"),
	}
	if cfg.Database == "" {
		cfg.Database = "postgres"
	}
	if cfg.SSLMode == "" {
		cfg.SSLMode = "disable"
	}
	admin, err := sql.Open("pgx", cfg.DSN())
	if err != nil {
		return nil, err
	}
	if err := admin.Ping(); err != nil {
		admin.Close()
		return nil, fmt.Errorf("testdb: cannot reach %s:%d (run make prep): %w", cfg.Host, cfg.Port, err)
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		admin.Close()
		return nil, err
	}
	cfg.Database = prefix + hex.EncodeToString(b)
	if _, err := admin.Exec("CREATE DATABASE " + cfg.Database); err != nil {
		admin.Close()
		return nil, fmt.Errorf("testdb: create database: %w", err)
	}
	db, err := sql.Open("pgx", cfg.DSN())
	if err != nil {
		admin.Exec("DROP DATABASE IF EXISTS " + cfg.Database + " WITH (FORCE)")
		admin.Close()
		return nil, err
	}
	db.SetMaxOpenConns(10)
	return &DB{Config: cfg, SQL: db, admin: admin}, nil
}

// Drop closes and removes the database
func (d *DB) Drop() error {
	d.SQL.Close()
	defer d.admin.Close()
	if !strings.HasPrefix(d.Config.Database, prefix) { // never anything this package did not create
		return fmt.Errorf("testdb: refusing to drop %q", d.Config.Database)
	}
	_, err := d.admin.Exec("DROP DATABASE IF EXISTS " + d.Config.Database + " WITH (FORCE)")
	return err
}
