package urlscan

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testUUID = "0198b1f1-3f4a-4b2e-8c71-2d9a5f0e7b11"

const searchJSON = `{
  "results": [
    {
      "task": {"url": "https://example.com/", "time": "2023-07-22T05:46:40.000Z"},
      "page": {"domain": "example.com", "ip": "93.184.216.34"},
      "_id": "0198b1f1-3f4a-4b2e-8c71-2d9a5f0e7b11",
      "sort": [1690000000000, "0198b1f1-3f4a-4b2e-8c71-2d9a5f0e7b11"]
    }
  ],
  "total": 1,
  "took": 37,
  "has_more": false
}`

func pngBody(tail string) string {
	return "\x89PNG\r\n\x1a\n" + tail
}

func noCallServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the server was called: %s", r.URL)
	}))
}

func TestSearchSendsKeyAndParameters(t *testing.T) {
	var gotKey, gotPath string
	var gotQuery, gotSize, gotAfter, gotCollapse string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("API-Key")
		gotPath = r.URL.Path
		q := r.URL.Query()
		gotQuery, gotSize = q.Get("q"), q.Get("size")
		gotAfter, gotCollapse = q.Get("search_after"), q.Get("collapse")
		w.Write([]byte(searchJSON))
	}))
	defer srv.Close()

	c := New("KEY", WithBaseURL(srv.URL))
	page, err := c.Search(context.Background(), SearchParams{
		Query:       "page.domain:example.com",
		Size:        250,
		SearchAfter: "1690000000000,abc",
		Collapse:    "page.domain",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotKey != "KEY" {
		t.Errorf("API-Key = %q", gotKey)
	}
	if gotPath != "/api/v1/search" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "page.domain:example.com" {
		t.Errorf("q = %q", gotQuery)
	}
	if gotSize != "250" {
		t.Errorf("size = %q", gotSize)
	}
	if gotAfter != "1690000000000,abc" {
		t.Errorf("search_after = %q", gotAfter)
	}
	if gotCollapse != "page.domain" {
		t.Errorf("collapse = %q", gotCollapse)
	}
	if page.Total != 1 || page.Took != 37 || page.HasMore {
		t.Errorf("page = %+v", page)
	}
	if len(page.Results) != 1 {
		t.Fatalf("%d results", len(page.Results))
	}
}

func TestNextSearchAfterKeepsTimestampsIntact(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(searchJSON))
	}))
	defer srv.Close()

	c := New("KEY", WithBaseURL(srv.URL))
	page, err := c.Search(context.Background(), SearchParams{Query: "domain:example.com"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	want := "1690000000000," + testUUID
	if got := NextSearchAfter(page); got != want {
		t.Errorf("NextSearchAfter = %q, want %q", got, want)
	}

	if got := NextSearchAfter(SearchPage{}); got != "" {
		t.Errorf("empty page gave %q", got)
	}
	if got := NextSearchAfter(SearchPage{Results: []map[string]any{{"_id": testUUID}}}); got != "" {
		t.Errorf("a result with no sort gave %q", got)
	}
}

func TestSearchRejectsEmptyQuery(t *testing.T) {
	srv := noCallServer(t)
	defer srv.Close()

	c := New("KEY", WithBaseURL(srv.URL))
	for _, bad := range []string{"", "   "} {
		if _, err := c.Search(context.Background(), SearchParams{Query: bad}); err == nil {
			t.Errorf("Search(%q) was accepted", bad)
		}
	}
}

func TestArtefactsRejectInvalidUUIDWithoutCallingTheServer(t *testing.T) {
	srv := noCallServer(t)
	defer srv.Close()

	c := New("KEY", WithBaseURL(srv.URL))
	bad := []string{"", "not-a-uuid", "0198b1f1-3f4a-4b2e-8c71-2d9a5f0e7b1", "../../etc/passwd"}
	for _, id := range bad {
		if _, err := c.Screenshot(context.Background(), id); err == nil {
			t.Errorf("Screenshot(%q) was accepted", id)
		}
		if _, err := c.DOM(context.Background(), id); err == nil {
			t.Errorf("DOM(%q) was accepted", id)
		}
	}
}

func TestArtefactsNormaliseTheUUID(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if strings.HasPrefix(r.URL.Path, "/dom/") {
			w.Write([]byte("<html></html>"))
			return
		}
		w.Write([]byte(pngBody("pixels")))
	}))
	defer srv.Close()

	c := New("KEY", WithBaseURL(srv.URL))
	upper := strings.ToUpper(testUUID)

	if _, err := c.Screenshot(context.Background(), " "+upper+" "); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if gotPath != "/screenshots/"+testUUID+".png" {
		t.Errorf("screenshot path = %q", gotPath)
	}

	dom, err := c.DOM(context.Background(), upper)
	if err != nil {
		t.Fatalf("DOM: %v", err)
	}
	if gotPath != "/dom/"+testUUID+"/" {
		t.Errorf("dom path = %q", gotPath)
	}
	if dom != "<html></html>" {
		t.Errorf("dom = %q", dom)
	}
}

func TestScreenshotRejectsANonPNGBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>an error page, not an image</html>"))
	}))
	defer srv.Close()

	c := New("KEY", WithBaseURL(srv.URL))
	if _, err := c.Screenshot(context.Background(), testUUID); err == nil {
		t.Fatal("a non-PNG body was accepted")
	}
}

func TestNotFoundSaysWhatItMeans(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New("KEY", WithBaseURL(srv.URL))
	_, err := c.Screenshot(context.Background(), testUUID)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v (%T), want *APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Error(), "not stored") {
		t.Errorf("message does not explain a missing artefact: %s", apiErr)
	}
}

func TestRateLimitCarriesTheResetDelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Rate-Limit-Reset-After", "12")
		http.Error(w, "throttled", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New("KEY", WithBaseURL(srv.URL))
	_, err := c.Search(context.Background(), SearchParams{Query: "domain:example.com"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v (%T), want *APIError", err, err)
	}
	if apiErr.RetryAfter != "12" {
		t.Errorf("RetryAfter = %q, want %q", apiErr.RetryAfter, "12")
	}
	msg := apiErr.Error()
	if !strings.Contains(msg, "rate limited") || !strings.Contains(msg, "retry in 12s") {
		t.Errorf("message = %s", msg)
	}
}

func TestNoKeyIsAnError(t *testing.T) {
	c := New("")
	if _, err := c.Search(context.Background(), SearchParams{Query: "domain:example.com"}); err == nil {
		t.Error("a search with no key succeeded")
	}
	if _, err := c.Screenshot(context.Background(), testUUID); err == nil {
		t.Error("a screenshot with no key succeeded")
	}
	if _, err := c.DOM(context.Background(), testUUID); err == nil {
		t.Error("a DOM fetch with no key succeeded")
	}
}
