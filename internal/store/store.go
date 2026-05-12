// Package store wraps pgx for the bookwarehouse-audio plugin. Each method
// accepts a context and operates against the configured pool.
package store

import "github.com/jackc/pgx/v5/pgxpool"

// Store is a thin pgx wrapper.
type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool exposes the underlying pool for transactional callers.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }
