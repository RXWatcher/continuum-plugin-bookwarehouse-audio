package catalog

import (
	"strings"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
)

// ToSummary converts upstream Book into the contract summary shape.
func ToSummary(b bookwarehouse.Book) AudiobookSummary {
	return AudiobookSummary{
		ID:              b.ID,
		Title:           b.Title,
		Authors:         b.Authors,
		Narrators:       b.Narrators,
		DurationSeconds: b.DurationSeconds,
		CoverURL:        b.CoverURL,
		HasCover:        b.HasCover,
		Year:            b.Year,
		Rating:          b.Rating,
	}
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
