// Package urlscan is a client for the urlscan.io search and artefact endpoints.
package urlscan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://urlscan.io"

const MaxSearchSize = 10000

const (
	maxScreenshotBytes = 32 << 20
	maxDOMBytes        = 64 << 20
)

var idRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

type Client struct {
	key     string
	baseURL string
	http    *http.Client
}

type Option func(*Client)

func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

func New(key string, opts ...Option) *Client {
	c := &Client{
		key:     key,
		baseURL: DefaultBaseURL,
		http:    &http.Client{Timeout: 90 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type APIError struct {
	StatusCode int
	Endpoint   string
	Body       string
	RetryAfter string
}

func (e *APIError) Error() string {
	switch e.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Sprintf("urlscan %s: rejected (%d) — check the API key", e.Endpoint, e.StatusCode)
	case http.StatusNotFound:
		return fmt.Sprintf("urlscan %s: not found (404) — unknown scan, scan still running, or artefact not stored", e.Endpoint)
	case http.StatusTooManyRequests:
		if e.RetryAfter != "" {
			return fmt.Sprintf("urlscan %s: rate limited (429) — retry in %ss", e.Endpoint, e.RetryAfter)
		}
		return fmt.Sprintf("urlscan %s: rate limited (429)", e.Endpoint)
	}
	body := strings.TrimSpace(e.Body)
	if len(body) > 200 {
		body = body[:200]
	}
	return fmt.Sprintf("urlscan %s: HTTP %d: %s", e.Endpoint, e.StatusCode, body)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, endpoint string) (*http.Response, error) {
	if c.key == "" {
		return nil, fmt.Errorf("urlscan %s: no API key", endpoint)
	}
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("API-Key", c.key)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("urlscan %s: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		retry := resp.Header.Get("X-Rate-Limit-Reset-After")
		if retry == "" {
			retry = resp.Header.Get("Retry-After")
		}
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Endpoint:   endpoint,
			Body:       string(body),
			RetryAfter: retry,
		}
	}
	return resp, nil
}

func NormaliseID(s string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(s))
	if !idRe.MatchString(id) {
		return "", fmt.Errorf("urlscan: %q is not a scan UUID", s)
	}
	return id, nil
}

type SearchParams struct {
	Query       string
	Size        int
	SearchAfter string
	Collapse    string
}

type SearchPage struct {
	Results []map[string]any `json:"results"`
	Total   int64            `json:"total"`
	Took    int64            `json:"took"`
	HasMore bool             `json:"has_more"`
}

func (c *Client) Search(ctx context.Context, p SearchParams) (SearchPage, error) {
	query := strings.TrimSpace(p.Query)
	if query == "" {
		return SearchPage{}, fmt.Errorf("urlscan search: a query is required")
	}
	q := url.Values{}
	q.Set("q", query)
	if p.Size > 0 {
		q.Set("size", strconv.Itoa(p.Size))
	}
	if sa := strings.TrimSpace(p.SearchAfter); sa != "" {
		q.Set("search_after", sa)
	}
	if col := strings.TrimSpace(p.Collapse); col != "" {
		q.Set("collapse", col)
	}

	resp, err := c.get(ctx, "/api/v1/search", q, "search")
	if err != nil {
		return SearchPage{}, err
	}
	defer resp.Body.Close()

	var page SearchPage
	dec := json.NewDecoder(resp.Body)
	// sort holds ms timestamps; as float64 they print in exponent form and break search_after.
	dec.UseNumber()
	if err := dec.Decode(&page); err != nil {
		return SearchPage{}, fmt.Errorf("urlscan search: decoding results: %w", err)
	}
	return page, nil
}

func NextSearchAfter(page SearchPage) string {
	if len(page.Results) == 0 {
		return ""
	}
	sort, ok := page.Results[len(page.Results)-1]["sort"].([]any)
	if !ok || len(sort) == 0 {
		return ""
	}
	parts := make([]string, len(sort))
	for i, v := range sort {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ",")
}

func (c *Client) Screenshot(ctx context.Context, id string) ([]byte, error) {
	id, err := NormaliseID(id)
	if err != nil {
		return nil, err
	}
	resp, err := c.get(ctx, "/screenshots/"+id+".png", nil, "screenshot")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	png, err := readBounded(resp.Body, maxScreenshotBytes)
	if err != nil {
		return nil, fmt.Errorf("urlscan screenshot %s: %w", id, err)
	}
	if !bytes.HasPrefix(png, pngMagic) {
		return nil, fmt.Errorf("urlscan screenshot %s: the response is not a PNG", id)
	}
	return png, nil
}

func (c *Client) DOM(ctx context.Context, id string) (string, error) {
	id, err := NormaliseID(id)
	if err != nil {
		return "", err
	}
	resp, err := c.get(ctx, "/dom/"+id+"/", nil, "dom")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	dom, err := readBounded(resp.Body, maxDOMBytes)
	if err != nil {
		return "", fmt.Errorf("urlscan dom %s: %w", id, err)
	}
	return string(dom), nil
}

func readBounded(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("the artefact is larger than %d bytes", max)
	}
	return b, nil
}
