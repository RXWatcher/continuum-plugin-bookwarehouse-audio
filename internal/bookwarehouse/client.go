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
	"sync"
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
	mu      sync.RWMutex
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
func (c *Client) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// Reconfigure updates the upstream base URL and API key in place so admin
// config saves take effect without a plugin restart.
func (c *Client) Reconfigure(baseURL, apiKey string) {
	c.mu.Lock()
	c.baseURL = strings.TrimRight(baseURL, "/")
	c.apiKey = apiKey
	c.mu.Unlock()
}

func (c *Client) configSnapshot() (string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL, c.apiKey
}

// Get performs a GET against baseURL+path and returns the body bytes.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	baseURL, apiKey := c.configSnapshot()
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("X-API-Key", apiKey)
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
	baseURL, apiKey := c.configSnapshot()
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("X-API-Key", apiKey)
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
	// BookWarehouse uses page-based pagination on the catalog endpoint, same
	// as the browse endpoints. We translate cursor→page here so the portal
	// can stay cursor-based; an empty cursor means page 1.
	q := url.Values{}
	if p.Cursor != "" {
		q.Set("page", p.Cursor)
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
	path := "/api/v1/audiobooks"
	if p.Query != "" {
		q.Set("q", p.Query)
		path = "/api/v1/audiobooks/search"
	}
	full := path
	if e := q.Encode(); e != "" {
		full = path + "?" + e
	}
	body, err := c.Get(ctx, full)
	if err != nil {
		return Paged[Book]{}, err
	}
	var out struct {
		Items      []Book `json:"items"`
		Audiobooks []Book `json:"audiobooks"`
		NextCursor string `json:"next_cursor,omitempty"`
		Total      int    `json:"total,omitempty"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Paged[Book]{}, fmt.Errorf("decode books: %w", err)
	}
	items := out.Items
	if len(items) == 0 && len(out.Audiobooks) > 0 {
		items = out.Audiobooks
	}
	for i := range items {
		normalizeBookSummary(&items[i])
	}
	// Prefer the upstream's next_cursor when present; fall back to inferring
	// from page-number arithmetic when BookWarehouse only advertises page +
	// total (which is the actual BW behavior for /api/v1/audiobooks).
	next := out.NextCursor
	if next == "" {
		next = nextCursorFromPage(p, len(items))
	}
	return Paged[Book]{
		Items:      items,
		NextCursor: next,
		Total:      out.Total,
	}, nil
}

// GetBook fetches one book's full detail.
func (c *Client) GetBook(ctx context.Context, id string) (BookDetail, error) {
	body, err := c.Get(ctx, "/api/v1/audiobooks/"+url.PathEscape(id))
	if err != nil {
		return BookDetail{}, err
	}
	var out BookDetail
	if err := json.Unmarshal(body, &out); err != nil {
		return BookDetail{}, fmt.Errorf("decode book: %w", err)
	}
	normalizeBookSummary(&out.Book)
	return out, nil
}

// browseQuery encodes the portal's cursor as BookWarehouse's page number.
// The portal sends cursors so frontend code can stay backend-agnostic; we
// translate at this seam. An empty cursor means page 1.
func browseQuery(p ListParams) url.Values {
	q := url.Values{}
	if p.Cursor != "" {
		q.Set("page", p.Cursor)
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.LibraryID > 0 {
		q.Set("library_id", strconv.FormatInt(p.LibraryID, 10))
	}
	return q
}

// nextCursorFromPage returns the next page number as a string if the response
// looks full (items == limit), otherwise empty (no more pages). BookWarehouse
// doesn't advertise total pages in a way we can trust, so we infer end-of-list
// from a partial page.
func nextCursorFromPage(p ListParams, returned int) string {
	if p.Limit <= 0 || returned < p.Limit {
		return ""
	}
	current := 1
	if p.Cursor != "" {
		if n, err := strconv.Atoi(p.Cursor); err == nil && n > 0 {
			current = n
		}
	}
	return strconv.Itoa(current + 1)
}

// ListAuthors maps BookWarehouse's {authors:[...], page, total} shape to the
// {items, next_cursor, total} envelope the portal expects. Each author's
// num_books is mapped to count.
func (c *Client) ListAuthors(ctx context.Context, p ListParams) (Paged[Author], error) {
	q := browseQuery(p)
	full := "/api/v1/audiobooks/authors"
	if e := q.Encode(); e != "" {
		full = full + "?" + e
	}
	body, err := c.Get(ctx, full)
	if err != nil {
		return Paged[Author]{}, err
	}
	var raw struct {
		Authors []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			NumBooks int    `json:"num_books"`
		} `json:"authors"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Paged[Author]{}, fmt.Errorf("decode authors: %w", err)
	}
	items := make([]Author, len(raw.Authors))
	for i, a := range raw.Authors {
		items[i] = Author{ID: a.ID, Name: a.Name, Count: a.NumBooks}
	}
	return Paged[Author]{
		Items:      items,
		Total:      raw.Total,
		NextCursor: nextCursorFromPage(p, len(items)),
	}, nil
}

// ListSeries maps BookWarehouse's {series:[...], page, total} to the portal
// envelope. Same num_books → count mapping as ListAuthors.
func (c *Client) ListSeries(ctx context.Context, p ListParams) (Paged[Series], error) {
	q := browseQuery(p)
	full := "/api/v1/audiobooks/series"
	if e := q.Encode(); e != "" {
		full = full + "?" + e
	}
	body, err := c.Get(ctx, full)
	if err != nil {
		return Paged[Series]{}, err
	}
	var raw struct {
		Series []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			NumBooks int    `json:"num_books"`
		} `json:"series"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Paged[Series]{}, fmt.Errorf("decode series: %w", err)
	}
	items := make([]Series, len(raw.Series))
	for i, s := range raw.Series {
		items[i] = Series{ID: s.ID, Name: s.Name, Count: s.NumBooks}
	}
	return Paged[Series]{
		Items:      items,
		Total:      raw.Total,
		NextCursor: nextCursorFromPage(p, len(items)),
	}, nil
}

// ListNarrators maps BookWarehouse's {narrators:[...], total} to the portal
// envelope. BookWarehouse returns narrator IDs as integers — we stringify so
// every facet ID is consistent on the portal side (downstream URL encoding,
// React keys, route segments all assume string).
func (c *Client) ListNarrators(ctx context.Context, p ListParams) (Paged[Narrator], error) {
	q := browseQuery(p)
	full := "/api/v1/audiobooks/narrators"
	if e := q.Encode(); e != "" {
		full = full + "?" + e
	}
	body, err := c.Get(ctx, full)
	if err != nil {
		return Paged[Narrator]{}, err
	}
	var raw struct {
		Narrators []struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			NumBooks int    `json:"num_books"`
		} `json:"narrators"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Paged[Narrator]{}, fmt.Errorf("decode narrators: %w", err)
	}
	items := make([]Narrator, len(raw.Narrators))
	for i, n := range raw.Narrators {
		items[i] = Narrator{ID: strconv.FormatInt(n.ID, 10), Name: n.Name, Count: n.NumBooks}
	}
	return Paged[Narrator]{
		Items:      items,
		Total:      raw.Total,
		NextCursor: nextCursorFromPage(p, len(items)),
	}, nil
}

// StreamURL returns the upstream audiobook stream URL. The browser is
// redirected here, so the API key is passed as a query param (the upstream
// accepts api_key as well as the X-API-Key header) — without it the
// authenticated endpoint would 401.
func (c *Client) StreamURL(bookID string, fileIdx int) string {
	baseURL, apiKey := c.configSnapshot()
	return fmt.Sprintf("%s/api/v1/audiobooks/%s/stream?file_id=%d%s",
		baseURL, url.PathEscape(bookID), fileIdx, apiKeyQuery(apiKey, "&"))
}

// CoverURL returns the upstream audiobook cover URL. The upstream cover
// endpoint takes no size and requires auth; the key is passed as a query
// param for the same redirect reason as StreamURL.
func (c *Client) CoverURL(bookID, _ string) string {
	baseURL, apiKey := c.configSnapshot()
	return fmt.Sprintf("%s/api/v1/audiobooks/%s/cover%s",
		baseURL, url.PathEscape(bookID), apiKeyQuery(apiKey, "?"))
}

func normalizeBookSummary(b *Book) {
	if b == nil {
		return
	}
	if b.HasCover && b.CoverURL == "" && b.ID != "" {
		b.CoverURL = "/cover/" + url.PathEscape(b.ID) + "/large"
	}
}

// apiKeyQuery returns "<sep>api_key=<escaped>" when an API key is configured,
// else "". sep is "?" or "&" depending on whether the URL already has a query.
func apiKeyQuery(apiKey, sep string) string {
	if apiKey == "" {
		return ""
	}
	return sep + "api_key=" + url.QueryEscape(apiKey)
}
