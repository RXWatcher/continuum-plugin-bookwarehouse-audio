package bookwarehouse

import (
	"encoding/json"
	"strconv"
	"time"
)

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

func (b *Book) UnmarshalJSON(data []byte) error {
	var aux struct {
		ID              string          `json:"id"`
		Title           string          `json:"title"`
		DurationSeconds int             `json:"duration_seconds"`
		CoverURL        string          `json:"cover_url"`
		HasCover        bool            `json:"has_cover"`
		Year            int             `json:"year,omitempty"`
		Rating          float64         `json:"rating,omitempty"`
		Description     string          `json:"description,omitempty"`
		ISBN            string          `json:"isbn,omitempty"`
		Publisher       string          `json:"publisher,omitempty"`
		Series          string          `json:"series,omitempty"`
		SeriesIndex     float64         `json:"series_index,omitempty"`
		AddedAtMs       int64           `json:"added_at_ms,omitempty"`
		UpdatedAtMs     int64           `json:"updated_at_ms,omitempty"`
		Author          string          `json:"author"`
		Authors         []string        `json:"authors"`
		NarratorsRaw    json.RawMessage `json:"narrators"`
		GenresRaw       json.RawMessage `json:"genres"`
		CreatedAt       string          `json:"created_at"`
		UpdatedAt       string          `json:"updated_at"`
		PublishedDate   string          `json:"published_date"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	*b = Book{
		ID:              aux.ID,
		Title:           aux.Title,
		DurationSeconds: aux.DurationSeconds,
		CoverURL:        aux.CoverURL,
		HasCover:        aux.HasCover,
		Year:            aux.Year,
		Rating:          aux.Rating,
		Description:     aux.Description,
		ISBN:            aux.ISBN,
		Publisher:       aux.Publisher,
		Series:          aux.Series,
		SeriesIndex:     aux.SeriesIndex,
		AddedAtMs:       aux.AddedAtMs,
		UpdatedAtMs:     aux.UpdatedAtMs,
	}

	if len(aux.Authors) > 0 {
		b.Authors = aux.Authors
	} else if aux.Author != "" {
		b.Authors = []string{aux.Author}
	}

	if len(aux.NarratorsRaw) > 0 && string(aux.NarratorsRaw) != "null" {
		var names []string
		if err := json.Unmarshal(aux.NarratorsRaw, &names); err == nil {
			b.Narrators = names
		} else {
			var narrators []struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(aux.NarratorsRaw, &narrators); err == nil {
				out := make([]string, 0, len(narrators))
				for _, narrator := range narrators {
					if narrator.Name != "" {
						out = append(out, narrator.Name)
					}
				}
				b.Narrators = out
			}
		}
	}
	if len(aux.GenresRaw) > 0 && string(aux.GenresRaw) != "null" {
		if genres, err := decodeStringList(aux.GenresRaw); err == nil {
			b.Genres = genres
		} else {
			return err
		}
	}

	if aux.CreatedAt != "" && b.AddedAtMs == 0 {
		if t, err := time.Parse(time.RFC3339Nano, aux.CreatedAt); err == nil {
			b.AddedAtMs = t.UnixMilli()
		}
	}
	if aux.UpdatedAt != "" && b.UpdatedAtMs == 0 {
		if t, err := time.Parse(time.RFC3339Nano, aux.UpdatedAt); err == nil {
			b.UpdatedAtMs = t.UnixMilli()
		}
	}
	if aux.PublishedDate != "" && b.Year == 0 && len(aux.PublishedDate) >= 4 {
		if y, err := strconv.Atoi(aux.PublishedDate[:4]); err == nil {
			b.Year = y
		}
	}

	return nil
}

// BookDetail extends Book with chapters and files.
type BookDetail struct {
	Book
	Chapters []Chapter `json:"chapters,omitempty"`
	Files    []File    `json:"files,omitempty"`
}

func (d *BookDetail) UnmarshalJSON(data []byte) error {
	var book Book
	if err := json.Unmarshal(data, &book); err != nil {
		return err
	}

	var aux struct {
		Description string          `json:"description,omitempty"`
		ISBN        string          `json:"isbn,omitempty"`
		Publisher   string          `json:"publisher,omitempty"`
		Series      string          `json:"series,omitempty"`
		SeriesIndex float64         `json:"series_index,omitempty"`
		GenresRaw   json.RawMessage `json:"genres,omitempty"`
		Chapters    []Chapter       `json:"chapters,omitempty"`
		Files       []File          `json:"files,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	d.Book = book
	d.Description = aux.Description
	d.ISBN = aux.ISBN
	d.Publisher = aux.Publisher
	d.Series = aux.Series
	d.SeriesIndex = aux.SeriesIndex
	if len(aux.GenresRaw) > 0 && string(aux.GenresRaw) != "null" {
		if genres, err := decodeStringList(aux.GenresRaw); err == nil {
			d.Genres = genres
		} else {
			return err
		}
	}
	d.Chapters = aux.Chapters
	d.Files = aux.Files
	for i := range d.Files {
		if d.Files[i].Index == 0 && i > 0 {
			d.Files[i].Index = i
		}
	}
	return nil
}

// Chapter is an upstream chapter marker.
type Chapter struct {
	StartSeconds int    `json:"start_seconds"`
	EndSeconds   int    `json:"end_seconds"`
	Title        string `json:"title"`
}

func (c *Chapter) UnmarshalJSON(data []byte) error {
	var aux struct {
		StartSeconds json.RawMessage `json:"start_seconds"`
		EndSeconds   json.RawMessage `json:"end_seconds"`
		Title        string          `json:"title"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	start, err := decodeWholeSeconds(aux.StartSeconds)
	if err != nil {
		return err
	}
	end, err := decodeWholeSeconds(aux.EndSeconds)
	if err != nil {
		return err
	}

	c.StartSeconds = start
	c.EndSeconds = end
	c.Title = aux.Title
	return nil
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

func decodeStringList(data []byte) ([]string, error) {
	var names []string
	if err := json.Unmarshal(data, &names); err == nil {
		return names, nil
	}

	var objects []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &objects); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(objects))
	for _, object := range objects {
		if object.Name != "" {
			out = append(out, object.Name)
		}
	}
	return out, nil
}

func decodeWholeSeconds(raw json.RawMessage) (int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}

	var whole int
	if err := json.Unmarshal(raw, &whole); err == nil {
		return whole, nil
	}

	var fractional float64
	if err := json.Unmarshal(raw, &fractional); err != nil {
		return 0, err
	}
	return int(fractional), nil
}
