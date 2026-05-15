package catalog_test

import (
	"reflect"
	"testing"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/catalog"
)

func TestToSummary_HappyPath(t *testing.T) {
	in := bookwarehouse.Book{
		ID:              "bw-1",
		Title:           "Atlas Shrugged",
		Authors:         []string{"Ayn Rand"},
		Narrators:       []string{"Scott Brick"},
		DurationSeconds: 234567,
		CoverURL:        "https://upstream/c/1",
		HasCover:        true,
		Year:            1957,
		Rating:          4.2,
	}
	got := catalog.ToSummary(in)
	want := catalog.AudiobookSummary{
		ID: "bw-1", Title: "Atlas Shrugged",
		Authors: []string{"Ayn Rand"}, Narrators: []string{"Scott Brick"},
		DurationSeconds: 234567, CoverURL: "https://upstream/c/1",
		HasCover: true, Year: 1957, Rating: 4.2,
		AuthorRefs: []catalog.AuthorRef{{ID: "ayn-rand", Name: "Ayn Rand"}},
		CoverPath:  "https://upstream/c/1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToSummary: got %+v want %+v", got, want)
	}
}

func TestToSummary_DerivesSeriesRef(t *testing.T) {
	in := bookwarehouse.Book{
		ID: "bw-3", Title: "Foundation",
		Authors: []string{"Isaac Asimov"},
		Series:  "The Foundation Series", SeriesIndex: 1.5,
	}
	got := catalog.ToSummary(in)
	if len(got.SeriesRefs) != 1 ||
		got.SeriesRefs[0].ID != "the-foundation-series" ||
		got.SeriesRefs[0].Sequence != "1.5" {
		t.Errorf("series_refs = %+v", got.SeriesRefs)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Andy Weir":        "andy-weir",
		"  Ayn  Rand  ":    "ayn-rand",
		"J.R.R. Tolkien":   "j-r-r-tolkien",
		"Iain M. Banks":    "iain-m-banks",
		"José Saramago":    "josé-saramago",
		"":                 "",
	}
	for in, want := range cases {
		if got := catalog.Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q want %q", in, got, want)
		}
	}
}

func TestToDetail_IncludesChaptersAndFiles(t *testing.T) {
	in := bookwarehouse.BookDetail{
		Book: bookwarehouse.Book{ID: "bw-2", Title: "X"},
		Chapters: []bookwarehouse.Chapter{
			{StartSeconds: 0, EndSeconds: 100, Title: "Ch 1"},
		},
		Files: []bookwarehouse.File{
			{Index: 0, Filename: "p1.m4b", Codec: "m4b", SizeBytes: 1024, DurationSeconds: 100},
		},
	}
	got := catalog.ToDetail(in)
	if len(got.Chapters) != 1 || got.Chapters[0].Title != "Ch 1" {
		t.Errorf("chapters: %+v", got.Chapters)
	}
	if len(got.Files) != 1 || got.Files[0].Format != "m4b" || got.Files[0].MimeType != "audio/x-m4b" {
		t.Errorf("files: %+v", got.Files)
	}
}

func TestCodecToMime(t *testing.T) {
	cases := map[string]string{
		"m4b":    "audio/x-m4b",
		"mp3":    "audio/mpeg",
		"flac":   "audio/flac",
		"opus":   "audio/ogg",
		"":       "audio/mpeg",
		"random": "audio/mpeg",
	}
	for codec, want := range cases {
		if got := catalog.CodecToMime(codec); got != want {
			t.Errorf("CodecToMime(%q) = %q, want %q", codec, got, want)
		}
	}
}
