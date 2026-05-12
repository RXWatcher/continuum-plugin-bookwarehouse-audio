// Package testutil provides shared helpers for integration tests across this
// plugin's packages.
package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// StartPG starts a fresh Postgres 18 container, creates the
// `bookwarehouse_audio` schema, and returns a DSN scoped via search_path.
//
// The container is stopped on test cleanup. Each call gets its own container,
// which keeps tests isolated.
func StartPG(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	c, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("continuum"),
		tcpostgres.WithUsername("plugin_bookwarehouse_audio"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Skipf("skip: docker postgres unavailable: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })

	dsn, err := c.ConnectionString(ctx, "sslmode=disable&search_path=bookwarehouse_audio")
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}

	// Pre-create the schema (in production the operator does this).
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS bookwarehouse_audio"); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return dsn
}
