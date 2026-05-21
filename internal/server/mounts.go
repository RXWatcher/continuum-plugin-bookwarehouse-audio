package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/catalog"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/covers"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/stream"
)

// mountCatalog wires the catalog routes under /api/v1 (the parent route group).
func (s *Server) mountCatalog(r chi.Router) {
	cli, ok := s.deps.BookwarehouseClient.(*bookwarehouse.Client)
	if !ok || cli == nil {
		return
	}
	cv, _ := s.deps.Covers.(*covers.Service)
	h := catalog.NewHandler(cli, cv, s.deps.Config.StreamSigningSecret)
	r.Get("/catalog", h.List())
	r.Get("/catalog/libraries", h.Libraries())
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
	cfg, _ := s.deps.StreamConfig.(stream.Config)
	h := stream.NewHandler(cli, cfg)
	r.Get("/stream/{book_id}/{file_idx}", h.Stream())
}
