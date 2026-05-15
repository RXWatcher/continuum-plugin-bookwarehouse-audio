// Package catalog defines the audiobook_backend.v1 contract response types
// and the upstream→contract translator. These types mirror the spec's
// "Response shapes" section verbatim.
package catalog

// AuthorRef carries a stable ID + display name for an author. The ID is
// derived as a slug-from-name when the upstream doesn't expose one (the
// upstream BookWarehouse exposes slug-IDs from its /audiobooks/authors
// endpoint that match this slug convention).
type AuthorRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SeriesRef is the same idea for series; sequence may be empty.
type SeriesRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Sequence string `json:"sequence,omitempty"`
}

// AudiobookSummary is the short shape returned by /catalog list endpoints.
type AudiobookSummary struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Authors         []string `json:"authors,omitempty"`
	Narrators       []string `json:"narrators,omitempty"`
	DurationSeconds int      `json:"duration_seconds"`
	CoverURL        string   `json:"cover_url,omitempty"`
	HasCover        bool     `json:"has_cover"`
	Year            int      `json:"year,omitempty"`
	Rating          float64  `json:"rating,omitempty"`
	// AuthorRefs / SeriesRefs supply stable IDs and display names. They run
	// alongside the legacy Authors/Series string fields so older consumers
	// keep working; the audiobookshelf-shaped client uses the *Refs.
	AuthorRefs []AuthorRef `json:"author_refs,omitempty"`
	SeriesRefs []SeriesRef `json:"series_refs,omitempty"`
	// CoverPath is the host-relative cover URL the ABS spec expects in
	// media.coverPath. We populate it with CoverURL so clients have a
	// non-empty path string (ABS dislikes empty coverPath).
	CoverPath string `json:"cover_path,omitempty"`
	// AddedAtMs / UpdatedAtMs are Unix milliseconds (0 when unknown).
	AddedAtMs   int64 `json:"added_at_ms,omitempty"`
	UpdatedAtMs int64 `json:"updated_at_ms,omitempty"`
}

// AudiobookFile describes one streamable audio file within a book.
type AudiobookFile struct {
	Index           int    `json:"index"`
	Format          string `json:"format"` // m4b | mp3 | opus | flac | mp4 | wav
	SizeBytes       int64  `json:"size_bytes"`
	DurationSeconds int    `json:"duration_seconds"`
	MimeType        string `json:"mime_type"`
}

// Chapter describes a chapter marker.
type Chapter struct {
	StartSeconds int    `json:"start_seconds"`
	EndSeconds   int    `json:"end_seconds"`
	Title        string `json:"title"`
}

// AudiobookDetail is the full shape returned by /catalog/{id}.
type AudiobookDetail struct {
	AudiobookSummary
	Description string          `json:"description,omitempty"`
	ISBN        string          `json:"isbn,omitempty"`
	Publisher   string          `json:"publisher,omitempty"`
	Series      string          `json:"series,omitempty"`
	SeriesIndex float64         `json:"series_index,omitempty"`
	Genres      []string        `json:"genres,omitempty"`
	Chapters    []Chapter       `json:"chapters,omitempty"`
	Files       []AudiobookFile `json:"files,omitempty"`
}

// PageEnvelope is the cursor-paged response shape for list/browse endpoints.
type PageEnvelope[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	Total      int    `json:"total,omitempty"`
}

// AuthorSummary / SeriesSummary / NarratorSummary mirror the browse list items.
type AuthorSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count,omitempty"`
}

type SeriesSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count,omitempty"`
}

type NarratorSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count,omitempty"`
}
