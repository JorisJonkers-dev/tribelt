// Package pg is the Postgres adapter: connection pool, embedded migrations and sqlc queries.
package pg

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/JorisJonkers-dev/tribelt/db"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

// Store owns the pool. An empty DSN means the libpq environment (PGHOST, PGUSER, ...).
type Store struct {
	pool *pgxpool.Pool
	q    *queries.Queries
}

// Open connects and verifies the connection.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("pg: config: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pg: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pg: ping: %w", err)
	}
	return &Store{pool: pool, q: queries.New(pool)}, nil
}

// Close releases every pooled connection.
func (s *Store) Close() { s.pool.Close() }

// Pool exposes the pool for transactions and streaming reads.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Q exposes the generated queries.
func (s *Store) Q() *queries.Queries { return s.q }

// Ping reports whether the database answers.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Migrate applies every pending embedded migration.
func Migrate(ctx context.Context, dsn string) error { return MigrateFS(ctx, dsn, db.Migrations) }

// MigrateFS applies the migrations under fsys/migrations.
func MigrateFS(ctx context.Context, dsn string, fsys fs.FS) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("pg: config: %w", err)
	}
	sqlDB := stdlib.OpenDB(*cfg.ConnConfig)
	defer func() { _ = sqlDB.Close() }()
	return migrateDB(ctx, sqlDB, fsys)
}

func migrateDB(ctx context.Context, sqlDB *sql.DB, fsys fs.FS) error {
	sub, err := fs.Sub(fsys, "migrations")
	if err != nil {
		return fmt.Errorf("pg: migrations dir: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, sub)
	if err != nil {
		return fmt.Errorf("pg: migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("pg: migrate up: %w", err)
	}
	return nil
}
