package bookwarehouse_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
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

func TestClient_AddMonitoring(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/monitoring/add" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"mon-99","status":"queued"}`))
	}))
	defer srv.Close()
	c := bookwarehouse.NewClient(srv.URL, "k")
	out, err := c.AddMonitoring(context.Background(), bookwarehouse.MonitoringRequest{Title: "X"})
	if err != nil {
		t.Fatalf("AddMonitoring: %v", err)
	}
	if out.ID != "mon-99" {
		t.Errorf("got %+v", out)
	}
}

// externalID reaches GetMonitoring from the /api/v1/requests/{external_id}
// URL param. A value with path/query metacharacters must be percent-escaped
// so it can't redirect the upstream request (SSRF / path traversal).
func TestClient_GetMonitoring_EscapesID(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"id":"x","status":"queued"}`))
	}))
	defer srv.Close()
	c := bookwarehouse.NewClient(srv.URL, "k")
	if _, err := c.GetMonitoring(context.Background(), "a?b"); err != nil {
		t.Fatalf("GetMonitoring: %v", err)
	}
	if gotPath != "/api/v1/monitoring/a?b" || gotQuery != "" {
		t.Errorf("upstream path=%q query=%q (externalID not escaped)", gotPath, gotQuery)
	}
}

func TestClient_ListAuthors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/audiobooks/authors" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"a1","name":"Andy Weir","count":3}]}`))
	}))
	defer srv.Close()
	c := bookwarehouse.NewClient(srv.URL, "k")
	out, err := c.ListAuthors(context.Background(), bookwarehouse.ListParams{})
	if err != nil {
		t.Fatalf("ListAuthors: %v", err)
	}
	if out.Items[0].Name != "Andy Weir" {
		t.Errorf("got %+v", out)
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
