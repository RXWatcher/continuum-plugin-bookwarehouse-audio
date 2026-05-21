package catalog

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
)

// ToSummary converts upstream Book into the contract summary shape.
func ToSummary(b bookwarehouse.Book) AudiobookSummary {
	out := AudiobookSummary{
		ID:              b.ID,
		Title:           b.Title,
		Authors:         b.Authors,
		Narrators:       b.Narrators,
		DurationSeconds: b.DurationSeconds,
		CoverURL:        b.CoverURL,
		HasCover:        b.HasCover,
		Year:            b.Year,
		Rating:          b.Rating,
		CoverPath:       b.CoverURL,
		AddedAtMs:       b.AddedAtMs,
		UpdatedAtMs:     b.UpdatedAtMs,
	}
	if len(b.Authors) > 0 {
		out.AuthorRefs = make([]AuthorRef, 0, len(b.Authors))
		for _, name := range b.Authors {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			out.AuthorRefs = append(out.AuthorRefs, AuthorRef{ID: Slugify(name), Name: name})
		}
	}
	if strings.TrimSpace(b.Series) != "" {
		seq := ""
		if b.SeriesIndex != 0 {
			seq = formatSequence(b.SeriesIndex)
		}
		out.SeriesRefs = []SeriesRef{{ID: Slugify(b.Series), Name: b.Series, Sequence: seq}}
	}
	return out
}

// Slugify lowercases name and replaces runs of non-alphanumerics with '-'.
// Matches the BookWarehouse upstream's author/series ID convention so a
// derived ID can round-trip to a real /audiobooks/authors/{id} lookup.
func Slugify(name string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// formatSequence renders a series_index (float) as a short string, dropping
// trailing zeros: 1.0 → "1", 1.5 → "1.5".
func formatSequence(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// ToDetail extends ToSummary with description, chapters, files, etc.
func ToDetail(d bookwarehouse.BookDetail) AudiobookDetail {
	out := AudiobookDetail{
		AudiobookSummary: ToSummary(d.Book),
		Description:      d.Description,
		ISBN:             d.ISBN,
		Publisher:        d.Publisher,
		Series:           d.Series,
		SeriesIndex:      d.SeriesIndex,
		Genres:           d.Genres,
	}
	if len(d.Chapters) > 0 {
		out.Chapters = make([]Chapter, len(d.Chapters))
		for i, c := range d.Chapters {
			out.Chapters[i] = Chapter{StartSeconds: c.StartSeconds, EndSeconds: c.EndSeconds, Title: c.Title}
		}
	}
	if len(d.Files) > 0 {
		out.Files = make([]AudiobookFile, len(d.Files))
		for i, f := range d.Files {
			out.Files[i] = AudiobookFile{
				Index:           f.Index,
				Format:          codecToFormat(f.Codec),
				SizeBytes:       f.SizeBytes,
				DurationSeconds: f.DurationSeconds,
				MimeType:        CodecToMime(f.Codec),
			}
		}
	}
	return out
}

// CodecToMime mirrors librarymanagerre's lib/abs/constants.ts CODEC_MIME_TYPES.
func CodecToMime(codec string) string {
	c := strings.ToLower(codec)
	switch {
	case strings.Contains(c, "m4b"):
		return "audio/x-m4b"
	case strings.Contains(c, "m4a"), strings.Contains(c, "aac"):
		return "audio/mp4"
	case strings.Contains(c, "mp4"):
		return "audio/mp4"
	case strings.Contains(c, "flac"):
		return "audio/flac"
	case strings.Contains(c, "ogg"), strings.Contains(c, "opus"), strings.Contains(c, "vorbis"):
		return "audio/ogg"
	case strings.Contains(c, "wav"), strings.Contains(c, "pcm"):
		return "audio/wav"
	case strings.Contains(c, "mp3"), strings.Contains(c, "mpeg"):
		return "audio/mpeg"
	}
	return "audio/mpeg"
}

func codecToFormat(codec string) string {
	c := strings.ToLower(codec)
	switch {
	case strings.Contains(c, "m4b"):
		return "m4b"
	case strings.Contains(c, "m4a"):
		return "m4a"
	case strings.Contains(c, "mp4"):
		return "m4a"
	case strings.Contains(c, "flac"):
		return "flac"
	case strings.Contains(c, "opus"):
		return "opus"
	case strings.Contains(c, "ogg"), strings.Contains(c, "vorbis"):
		return "ogg"
	case strings.Contains(c, "wav"):
		return "wav"
	case strings.Contains(c, "mp3"), strings.Contains(c, "mpeg"), c == "":
		return "mp3"
	}
	return c
}
