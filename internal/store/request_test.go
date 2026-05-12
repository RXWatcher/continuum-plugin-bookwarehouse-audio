package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/migrate"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/store"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/testutil"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := testutil.StartPG(t)
	if err := migrate.Run(context.Background(), dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

func TestUpsertForwardedRequest_NewRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertForwardedRequest(ctx, store.ForwardedRequest{
		RequestID: "req-1",
		Status:    "submitted",
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.GetForwardedRequest(ctx, "req-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "submitted" {
		t.Errorf("status = %q, want submitted", got.Status)
	}
	if got.RequestID != "req-1" {
		t.Errorf("request_id = %q", got.RequestID)
	}
}

func TestUpsertForwardedRequest_UpdatesExisting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.UpsertForwardedRequest(ctx, store.ForwardedRequest{
		RequestID: "req-1",
		Status:    "submitted",
		UpdatedAt: time.Now(),
	})
	_ = s.UpsertForwardedRequest(ctx, store.ForwardedRequest{
		RequestID:  "req-1",
		Status:     "acknowledged",
		ExternalID: "bw-42",
		UpdatedAt:  time.Now(),
	})
	got, err := s.GetForwardedRequest(ctx, "req-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "acknowledged" || got.ExternalID != "bw-42" {
		t.Errorf("after second upsert: %+v", got)
	}
}

func TestGetForwardedRequest_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetForwardedRequest(context.Background(), "missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
