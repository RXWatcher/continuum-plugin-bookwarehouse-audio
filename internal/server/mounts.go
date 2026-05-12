package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/catalog"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/requesthandler"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/store"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/stream"
)

// mountCatalog wires the catalog routes under /api/v1 (the parent route group).
func (s *Server) mountCatalog(r chi.Router) {
	cli, ok := s.deps.BookwarehouseClient.(*bookwarehouse.Client)
	if !ok || cli == nil {
		return
	}
	h := catalog.NewHandler(cli)
	r.Get("/catalog", h.List())
	r.Get("/catalog/search", h.Search())
	r.Get("/catalog/{id}", h.Detail())
	r.Get("/browse/authors", h.BrowseAuthors())
	r.Get("/browse/series", h.BrowseSeries())
	r.Get("/browse/narrators", h.BrowseNarrators())
	r.Get("/cover/{book_id}/{size}", h.Cover())
}

// mountStream wires the streaming redirect.
func (s *Server) mountStream(r chi.Router) {
	cli, ok := s.deps.BookwarehouseClient.(*bookwarehouse.Client)
	if !ok || cli == nil {
		return
	}
	h := stream.NewHandler(cli)
	r.Get("/stream/{book_id}/{file_idx}", h.Stream())
}

// mountRequests wires the request status snapshot endpoint.
func (s *Server) mountRequests(r chi.Router) {
	st, ok := s.deps.Store.(*store.Store)
	if !ok || st == nil {
		return
	}
	cli, _ := s.deps.BookwarehouseClient.(*bookwarehouse.Client)
	h := requesthandler.NewHandler(st, cli)
	r.Get("/requests/{external_id}", h.Snapshot())
}
