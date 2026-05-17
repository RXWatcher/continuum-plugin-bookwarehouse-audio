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

// Event delivery is at-least-once: a duplicate/late/replayed
// request_submitted (status submitted/acknowledged) must not resurrect a row
// that already reached a terminal state (imported or failed).
func TestUpsertForwardedRequest_TerminalGuard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, terminal := range []string{"imported", "failed"} {
		id := "term-" + terminal
		if err := s.UpsertForwardedRequest(ctx, store.ForwardedRequest{
			RequestID: id, Status: terminal, ExternalID: "bw-1", UpdatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("seed %s: %v", terminal, err)
		}
		if err := s.UpsertForwardedRequest(ctx, store.ForwardedRequest{
			RequestID: id, Status: "submitted", UpdatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("replay %s: %v", terminal, err)
		}
		if got, _ := s.GetForwardedRequest(ctx, id); got.Status != terminal {
			t.Errorf("%s row resurrected to %q", terminal, got.Status)
		}
	}
	// A non-terminal row must still advance.
	_ = s.UpsertForwardedRequest(ctx, store.ForwardedRequest{RequestID: "live", Status: "submitted", UpdatedAt: time.Now()})
	_ = s.UpsertForwardedRequest(ctx, store.ForwardedRequest{RequestID: "live", Status: "acknowledged", ExternalID: "bw-9", UpdatedAt: time.Now()})
	if got, _ := s.GetForwardedRequest(ctx, "live"); got.Status != "acknowledged" {
		t.Errorf("non-terminal row should advance; got %q", got.Status)
	}
}
