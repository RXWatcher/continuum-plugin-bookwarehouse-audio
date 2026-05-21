package bookwarehouse_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
)

// A broken/hostile upstream can return a huge error body. It must not be
// inlined whole into the error string (it propagates into logs / responses).
func TestClient_TruncatesErrorBody(t *testing.T) {
	big := strings.Repeat("x", 60000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()
	c := bookwarehouse.NewClient(srv.URL, "k")
	_, err := c.GetBook(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(err.Error()) > 1024 {
		t.Errorf("error not truncated: %d bytes", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status: %q", err.Error())
	}
}

// book_id flows from a URL param into the redirect target. A value with
// path/query metacharacters must be percent-escaped so it can't rewrite the
// upstream path (open redirect / path injection).
func TestClient_CoverStreamURL_EscapeID(t *testing.T) {
	c := bookwarehouse.NewClient("https://up.example", "k")
	cover := c.CoverURL("a/../b?x", "large")
	if strings.Contains(cover, "a/../b?x") {
		t.Errorf("CoverURL did not escape id: %s", cover)
	}
	if !strings.Contains(cover, "/api/v1/audiobooks/a%2F..%2Fb%3Fx/cover?api_key=k") {
		t.Errorf("CoverURL = %s", cover)
	}
	st := c.StreamURL("a/../b?x", 3)
	if strings.Contains(st, "a/../b?x") {
		t.Errorf("StreamURL did not escape id: %s", st)
	}
	if !strings.Contains(st, "/api/v1/audiobooks/a%2F..%2Fb%3Fx/stream?file_id=3&api_key=k") {
		t.Errorf("StreamURL = %s", st)
	}
}

func TestClient_SendsAPIKeyHeader(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := bookwarehouse.NewClient(srv.URL, "secret-key")
	if _, err := c.Get(context.Background(), "/api/v1/ping"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotKey != "secret-key" {
		t.Errorf("X-API-Key = %q, want secret-key", gotKey)
	}
}

func TestClient_TrimsTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := bookwarehouse.NewClient(srv.URL+"/", "k")
	_, _ = c.Get(context.Background(), "/api/v1/x")
	if gotPath != "/api/v1/x" {
		t.Errorf("path = %q, want /api/v1/x", gotPath)
	}
}

func TestClient_ListBooks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/audiobooks" || r.URL.Query().Get("limit") != "20" {
			t.Errorf("path/query = %s ?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"a","title":"A"}],"total":1}`))
	}))
	defer srv.Close()
	c := bookwarehouse.NewClient(srv.URL, "k")
	out, err := c.ListBooks(context.Background(), bookwarehouse.ListParams{Limit: 20})
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].ID != "a" {
		t.Errorf("got %+v", out)
	}
}

func TestClient_ListBooks_LiveBookWarehouseEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"audiobooks": [{
				"id": "bw-1",
				"title": "Isolation",
				"author": "Devon C. Ford",
				"narrators": [{"id": 7, "name": "Todd McLaren"}],
				"duration_seconds": 47369,
				"has_cover": true
			}],
			"total": 239443
		}`))
	}))
	defer srv.Close()

	c := bookwarehouse.NewClient(srv.URL, "k")
	out, err := c.ListBooks(context.Background(), bookwarehouse.ListParams{Limit: 20})
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1; out=%+v", len(out.Items), out)
	}
	if out.Items[0].ID != "bw-1" || out.Items[0].Title != "Isolation" {
		t.Fatalf("item = %+v", out.Items[0])
	}
	if len(out.Items[0].Authors) != 1 || out.Items[0].Authors[0] != "Devon C. Ford" {
		t.Fatalf("authors = %#v", out.Items[0].Authors)
	}
	if len(out.Items[0].Narrators) != 1 || out.Items[0].Narrators[0] != "Todd McLaren" {
		t.Fatalf("narrators = %#v", out.Items[0].Narrators)
	}
	if out.Items[0].CoverURL != "/cover/bw-1/large" {
		t.Fatalf("cover_url = %q", out.Items[0].CoverURL)
	}
	if out.Total != 239443 {
		t.Fatalf("total = %d, want 239443", out.Total)
	}
}

func TestClient_ListBooks_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/audiobooks/search" || r.URL.Query().Get("q") != "andy" {
			t.Errorf("path/query = %s ?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"b","title":"B"}]}`))
	}))
	defer srv.Close()
	c := bookwarehouse.NewClient(srv.URL, "k")
	out, err := c.ListBooks(context.Background(), bookwarehouse.ListParams{Query: "andy"})
	if err != nil {
		t.Fatalf("ListBooks search: %v", err)
	}
	if out.Items[0].ID != "b" {
		t.Errorf("got %+v", out)
	}
}

func TestClient_GetBook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/audiobooks/bw-42" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"bw-42","title":"X","files":[{"index":0,"file_path":"x.m4b","codec":"m4b","file_size":1000}]}`))
	}))
	defer srv.Close()
	c := bookwarehouse.NewClient(srv.URL, "k")
	d, err := c.GetBook(context.Background(), "bw-42")
	if err != nil {
		t.Fatalf("GetBook: %v", err)
	}
	if d.ID != "bw-42" || len(d.Files) != 1 || d.Files[0].Filename != "x.m4b" {
		t.Errorf("got %+v", d)
	}
}

func TestClient_GetBook_LiveBookWarehouseDetailEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/audiobooks/bw-live-42" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"id": "bw-live-42",
			"title": "The Kings' List",
			"author": "Jade Presley",
			"publisher": "Tantor Media",
			"published_date": "2026-05-19T00:00:00.000Z",
			"duration_seconds": 50561,
			"has_cover": true,
			"created_at": "2026-05-20T12:14:07.063358963Z",
			"updated_at": "2026-05-20T12:14:07.063359053Z",
			"narrators": [
				{"id": 8613, "name": "Jake Bordeaux"},
				{"id": 13452, "name": "Michelle Price"}
			],
			"files": [{
				"id": 239878,
				"file_path": "Jade Presley - The Never List 2 - The Kings' List (2026).m4b",
				"storage_key": "/media/books/the-kings-list.m4b",
				"file_size": 413679982,
				"duration_seconds": 50561,
				"codec": "aac"
			}],
			"chapters": [{
				"id": 7247483,
				"chapter_index": 0,
				"title": "Chapter 1",
				"start_seconds": 0,
				"end_seconds": 588.37
			}],
			"genres": [
				{"id": 4, "name": "Fantasy"},
				{"id": 7, "name": "Romance"}
			]
		}`))
	}))
	defer srv.Close()

	c := bookwarehouse.NewClient(srv.URL, "k")
	d, err := c.GetBook(context.Background(), "bw-live-42")
	if err != nil {
		t.Fatalf("GetBook: %v", err)
	}
	if d.ID != "bw-live-42" || d.Title != "The Kings' List" {
		t.Fatalf("detail = %+v", d)
	}
	if len(d.Authors) != 1 || d.Authors[0] != "Jade Presley" {
		t.Fatalf("authors = %#v", d.Authors)
	}
	if len(d.Narrators) != 2 || d.Narrators[0] != "Jake Bordeaux" || d.Narrators[1] != "Michelle Price" {
		t.Fatalf("narrators = %#v", d.Narrators)
	}
	if d.CoverURL != "/cover/bw-live-42/large" {
		t.Fatalf("cover_url = %q", d.CoverURL)
	}
	if d.Year != 2026 {
		t.Fatalf("year = %d", d.Year)
	}
	if len(d.Genres) != 2 || d.Genres[0] != "Fantasy" || d.Genres[1] != "Romance" {
		t.Fatalf("genres = %#v", d.Genres)
	}
	if len(d.Chapters) != 1 || d.Chapters[0].Title != "Chapter 1" || d.Chapters[0].EndSeconds != 588 {
		t.Fatalf("chapters = %#v", d.Chapters)
	}
	if len(d.Files) != 1 || d.Files[0].Filename == "" || d.Files[0].Codec != "aac" {
		t.Fatalf("files = %#v", d.Files)
	}
	if d.AddedAtMs == 0 || d.UpdatedAtMs == 0 {
		t.Fatalf("timestamps not parsed: added=%d updated=%d", d.AddedAtMs, d.UpdatedAtMs)
	}
}

func TestClient_ListAuthors(t *testing.T) {
	// BookWarehouse returns {"authors":[...],"limit","page","total"} — the
	// plugin maps it to the portal-facing {"items","next_cursor","total"}.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/audiobooks/authors" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"authors":[{"id":"a1","name":"Andy Weir","num_books":3},{"id":"a2","name":"Brandon Sanderson","num_books":12}],"limit":2,"page":1,"total":97117}`))
	}))
	defer srv.Close()
	c := bookwarehouse.NewClient(srv.URL, "k")
	out, err := c.ListAuthors(context.Background(), bookwarehouse.ListParams{Limit: 2})
	if err != nil {
		t.Fatalf("ListAuthors: %v", err)
	}
	if len(out.Items) != 2 || out.Items[0].Name != "Andy Weir" || out.Items[0].Count != 3 {
		t.Errorf("items = %#v", out.Items)
	}
	if out.Total != 97117 {
		t.Errorf("total = %d, want 97117", out.Total)
	}
	if out.NextCursor != "2" {
		t.Errorf("next_cursor = %q, want \"2\" (full page → more available)", out.NextCursor)
	}
}

func TestClient_ListNarrators_IntID(t *testing.T) {
	// BookWarehouse uses integer narrator ids; the plugin must stringify them
	// so the portal's facet routes (string ids everywhere) work.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"narrators":[{"id":42,"name":"Ray Porter","num_books":18}],"total":54529}`))
	}))
	defer srv.Close()
	c := bookwarehouse.NewClient(srv.URL, "k")
	out, err := c.ListNarrators(context.Background(), bookwarehouse.ListParams{Limit: 1})
	if err != nil {
		t.Fatalf("ListNarrators: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].ID != "42" || out.Items[0].Name != "Ray Porter" {
		t.Errorf("items = %#v", out.Items)
	}
	// Full page (limit=1, returned=1) → next_cursor advances to page 2. We
	// only know we hit the end when the response is shorter than the limit.
	if out.NextCursor != "2" {
		t.Errorf("next_cursor = %q, want \"2\" for full page", out.NextCursor)
	}
}

func TestNextCursorFromPage_PartialEndsList(t *testing.T) {
	// When the returned count is less than the requested limit BookWarehouse
	// has no more rows; the plugin must report an empty next_cursor so the
	// portal stops asking.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"series":[{"id":"s1","name":"Mistborn","num_books":7}],"total":3000}`))
	}))
	defer srv.Close()
	c := bookwarehouse.NewClient(srv.URL, "k")
	out, err := c.ListSeries(context.Background(), bookwarehouse.ListParams{Limit: 50})
	if err != nil {
		t.Fatalf("ListSeries: %v", err)
	}
	if out.NextCursor != "" {
		t.Errorf("partial page should clear next_cursor, got %q", out.NextCursor)
	}
}

func TestBookEnvelope_Decode(t *testing.T) {
	raw := []byte(`{
		"id": "bw-42",
		"title": "Project Hail Mary",
		"authors": ["Andy Weir"],
		"narrators": ["Ray Porter"],
		"duration_seconds": 57132,
		"cover_url": "https://bw.example/c/42",
		"has_cover": true,
		"year": 2021,
		"description": "..."
	}`)
	var b bookwarehouse.Book
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if b.ID != "bw-42" || b.Title != "Project Hail Mary" {
		t.Errorf("got %+v", b)
	}
	if len(b.Authors) != 1 || b.Authors[0] != "Andy Weir" {
		t.Errorf("authors = %v", b.Authors)
	}
	if b.DurationSeconds != 57132 {
		t.Errorf("duration = %d", b.DurationSeconds)
	}
}
