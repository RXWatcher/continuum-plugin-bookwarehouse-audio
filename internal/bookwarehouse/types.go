package bookwarehouse

// Book is the upstream summary of an audiobook. Some fields are optional and
// may not appear in every response.
type Book struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Authors         []string `json:"authors"`
	Narrators       []string `json:"narrators"`
	DurationSeconds int      `json:"duration_seconds"`
	CoverURL        string   `json:"cover_url"`
	HasCover        bool     `json:"has_cover"`
	Year            int      `json:"year,omitempty"`
	Rating          float64  `json:"rating,omitempty"`
	Description     string   `json:"description,omitempty"`
	ISBN            string   `json:"isbn,omitempty"`
	Publisher       string   `json:"publisher,omitempty"`
	Series          string   `json:"series,omitempty"`
	SeriesIndex     float64  `json:"series_index,omitempty"`
	Genres          []string `json:"genres,omitempty"`
	// AddedAtMs and UpdatedAtMs are Unix milliseconds; when the upstream
	// surfaces created_at / updated_at as RFC3339 strings, callers parse
	// those out-of-band and populate these fields.
	AddedAtMs   int64 `json:"added_at_ms,omitempty"`
	UpdatedAtMs int64 `json:"updated_at_ms,omitempty"`
}

// BookDetail extends Book with chapters and files.
type BookDetail struct {
	Book
	Chapters []Chapter `json:"chapters,omitempty"`
	Files    []File    `json:"files,omitempty"`
}

// Chapter is an upstream chapter marker.
type Chapter struct {
	StartSeconds int    `json:"start_seconds"`
	EndSeconds   int    `json:"end_seconds"`
	Title        string `json:"title"`
}

// File describes one streamable audio file in a BookDetail.
type File struct {
	Index           int    `json:"index"`
	Filename        string `json:"file_path"`
	StorageKey      string `json:"storage_key,omitempty"`
	Codec           string `json:"codec"`
	SizeBytes       int64  `json:"file_size"`
	DurationSeconds int    `json:"duration_seconds"`
}

// Paged is the standard pagination envelope used by all list endpoints.
type Paged[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	Total      int    `json:"total,omitempty"`
}

// Author / Series / Narrator are the browse list items.
type Author struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count,omitempty"`
}

type Series struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count,omitempty"`
}

type Narrator struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count,omitempty"`
}
