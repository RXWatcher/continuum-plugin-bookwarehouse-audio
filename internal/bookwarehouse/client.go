// Package bookwarehouse is a typed HTTP client for the upstream BookWarehouse
// service. Mirrors /opt/librarymanagerre/lib/audiobooks/client.ts.
package bookwarehouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultTimeout = 15 * time.Second

// maxResponseBytes caps the body the client will read from the upstream
// BookWarehouse service. JSON list/detail responses are well under this in
// normal operation; the cap defends against memory exhaustion if the
// upstream returns a runaway body (broken, compromised, hostile).
const maxResponseBytes = 10 << 20 // 10 MiB

// errBodySnippet caps how much of an upstream error body is inlined into an
// error string. The body can be up to maxResponseBytes and the error
// propagates into logs and responses.
const errBodySnippet = 512

func truncForError(b []byte) string {
	if len(b) <= errBodySnippet {
		return string(b)
	}
	return string(b[:errBodySnippet]) + "…(truncated)"
}

// Client is the typed BookWarehouse REST client.
type Client struct {
	baseURL string
	apiKey  string
	hc      *http.Client
}

// NewClient builds a Client with the default 15s timeout.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		hc:      &http.Client{Timeout: defaultTimeout},
	}
}

// BaseURL exposes the trimmed base URL (used by cover/stream redirect handlers).
func (c *Client) BaseURL() string { return c.baseURL }

// Get performs a GET against baseURL+path and returns the body bytes.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, truncForError(body))
	}
	return body, nil
}

// PostJSON sends a JSON-encoded body and returns the response body bytes.
func (c *Client) PostJSON(ctx context.Context, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, truncForError(respBody))
	}
	return respBody, nil
}

// ListParams is the query shape for ListBooks. When Query is non-empty, the
// request is dispatched to /api/v1/books/search instead.
type ListParams struct {
	Cursor    string
	Limit     int
	Sort      string // added | title | duration | rating
	Order     string // asc | desc
	Query     string
	LibraryID int64
}

// ListBooks fetches a page of books from the upstream.
func (c *Client) ListBooks(ctx context.Context, p ListParams) (Paged[Book], error) {
	q := url.Values{}
	if p.Cursor != "" {
		q.Set("cursor", p.Cursor)
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Sort != "" {
		q.Set("sort", p.Sort)
	}
	if p.Order != "" {
		q.Set("order", p.Order)
	}
	if p.LibraryID > 0 {
		q.Set("library_id", strconv.FormatInt(p.LibraryID, 10))
	}
	path := "/api/v1/books"
	if p.Query != "" {
		q.Set("q", p.Query)
		path = "/api/v1/books/search"
	}
	full := path
	if e := q.Encode(); e != "" {
		full = path + "?" + e
	}
	body, err := c.Get(ctx, full)
	if err != nil {
		return Paged[Book]{}, err
	}
	var out Paged[Book]
	if err := json.Unmarshal(body, &out); err != nil {
		return Paged[Book]{}, fmt.Errorf("decode books: %w", err)
	}
	return out, nil
}

// GetBook fetches one book's full detail.
func (c *Client) GetBook(ctx context.Context, id string) (BookDetail, error) {
	body, err := c.Get(ctx, "/api/v1/books/"+url.PathEscape(id))
	if err != nil {
		return BookDetail{}, err
	}
	var out BookDetail
	if err := json.Unmarshal(body, &out); err != nil {
		return BookDetail{}, fmt.Errorf("decode book: %w", err)
	}
	return out, nil
}

// listBrowse is the shared shape used by browse endpoints. T is the item type.
func listBrowse[T any](ctx context.Context, c *Client, path string, p ListParams) (Paged[T], error) {
	q := url.Values{}
	if p.Cursor != "" {
		q.Set("cursor", p.Cursor)
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.LibraryID > 0 {
		q.Set("library_id", strconv.FormatInt(p.LibraryID, 10))
	}
	full := path
	if e := q.Encode(); e != "" {
		full = path + "?" + e
	}
	body, err := c.Get(ctx, full)
	if err != nil {
		return Paged[T]{}, err
	}
	var out Paged[T]
	if err := json.Unmarshal(body, &out); err != nil {
		return Paged[T]{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return out, nil
}

// ListAuthors / ListSeries / ListNarrators fetch the corresponding browse pages.
func (c *Client) ListAuthors(ctx context.Context, p ListParams) (Paged[Author], error) {
	return listBrowse[Author](ctx, c, "/api/v1/authors", p)
}

func (c *Client) ListSeries(ctx context.Context, p ListParams) (Paged[Series], error) {
	return listBrowse[Series](ctx, c, "/api/v1/series", p)
}

func (c *Client) ListNarrators(ctx context.Context, p ListParams) (Paged[Narrator], error) {
	return listBrowse[Narrator](ctx, c, "/api/v1/narrators", p)
}

// StreamURL returns the upstream URL for streaming a given file of an audiobook.
func (c *Client) StreamURL(bookID string, fileIdx int) string {
	return fmt.Sprintf("%s/api/v1/books/%s/files/%d/stream", c.baseURL, url.PathEscape(bookID), fileIdx)
}

// CoverURL returns the upstream URL for a cover at a given size.
func (c *Client) CoverURL(bookID, size string) string {
	if size == "" {
		size = "large"
	}
	return fmt.Sprintf("%s/api/v1/books/%s/cover/%s", c.baseURL, url.PathEscape(bookID), url.PathEscape(size))
}
