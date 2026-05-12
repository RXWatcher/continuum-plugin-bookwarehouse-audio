package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ForwardedRequest tracks a request the portal sent us that we forwarded to
// BookWarehouse's monitoring endpoint.
type ForwardedRequest struct {
	RequestID  string
	ExternalID string
	Status     string // submitted | acknowledged | imported | failed
	LastPolled time.Time
	ErrorText  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ErrNotFound is returned by GetForwardedRequest when the row does not exist.
var ErrNotFound = errors.New("not found")

// UpsertForwardedRequest inserts or updates by request_id (the portal's id).
func (s *Store) UpsertForwardedRequest(ctx context.Context, r ForwardedRequest) error {
	if r.RequestID == "" {
		return fmt.Errorf("request_id required")
	}
	if r.Status == "" {
		return fmt.Errorf("status required")
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now()
	}

	// pgx treats time.Time{} as the zero value when scanning, but PostgreSQL
	// will reject it on insert. Convert to nullable values explicitly.
	var (
		externalID *string
		lastPolled *time.Time
		errorText  *string
	)
	if r.ExternalID != "" {
		externalID = &r.ExternalID
	}
	if !r.LastPolled.IsZero() {
		lastPolled = &r.LastPolled
	}
	if r.ErrorText != "" {
		errorText = &r.ErrorText
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO forwarded_request (request_id, external_id, status, last_polled, error_text, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (request_id) DO UPDATE SET
			external_id = COALESCE(EXCLUDED.external_id, forwarded_request.external_id),
			status      = EXCLUDED.status,
			last_polled = COALESCE(EXCLUDED.last_polled, forwarded_request.last_polled),
			error_text  = COALESCE(EXCLUDED.error_text, forwarded_request.error_text),
			updated_at  = EXCLUDED.updated_at
	`, r.RequestID, externalID, r.Status, lastPolled, errorText, r.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert forwarded_request: %w", err)
	}
	return nil
}

// GetForwardedRequest reads by request_id. Returns ErrNotFound on miss.
func (s *Store) GetForwardedRequest(ctx context.Context, requestID string) (ForwardedRequest, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT request_id, COALESCE(external_id,''), status,
		       COALESCE(last_polled, 'epoch'::timestamptz),
		       COALESCE(error_text,''), created_at, updated_at
		FROM forwarded_request WHERE request_id = $1
	`, requestID)
	var r ForwardedRequest
	if err := row.Scan(&r.RequestID, &r.ExternalID, &r.Status, &r.LastPolled, &r.ErrorText, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ForwardedRequest{}, ErrNotFound
		}
		return ForwardedRequest{}, fmt.Errorf("get forwarded_request: %w", err)
	}
	return r, nil
}

// GetByExternalID reads by external_id (BookWarehouse monitoring id).
func (s *Store) GetByExternalID(ctx context.Context, externalID string) (ForwardedRequest, error) {
	if externalID == "" {
		return ForwardedRequest{}, ErrNotFound
	}
	row := s.pool.QueryRow(ctx, `
		SELECT request_id, COALESCE(external_id,''), status,
		       COALESCE(last_polled, 'epoch'::timestamptz),
		       COALESCE(error_text,''), created_at, updated_at
		FROM forwarded_request WHERE external_id = $1
	`, externalID)
	var r ForwardedRequest
	if err := row.Scan(&r.RequestID, &r.ExternalID, &r.Status, &r.LastPolled, &r.ErrorText, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ForwardedRequest{}, ErrNotFound
		}
		return ForwardedRequest{}, fmt.Errorf("get forwarded_request by external_id: %w", err)
	}
	return r, nil
}
