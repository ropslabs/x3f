package releases

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "ghp_testtoken0123456789"

// fakeGitHub serves the recorded geth releases with an ETag and answers
// conditional requests with 304. It counts requests and records the last
// request headers. status, if set, overrides the answer.
type fakeGitHub struct {
	srv      *httptest.Server
	requests atomic.Int32 // atomic: the handler runs in another goroutine
	lastAuth atomic.Value
	lastINM  atomic.Value
	status   atomic.Int32
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	body, err := os.ReadFile(fixturePath(Repo{"ethereum", "go-ethereum"}))
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGitHub{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		f.lastAuth.Store(r.Header.Get("Authorization"))
		f.lastINM.Store(r.Header.Get("If-None-Match"))
		if r.URL.Path != "/repos/ethereum/go-ethereum/releases" || r.URL.Query().Get("per_page") != "100" {
			http.NotFound(w, r)
			return
		}
		if s := f.status.Load(); s != 0 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(int(s))
			return
		}
		const etag = `W/"abc123"`
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Write(body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// clock is a settable time source for tests.
type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

var geth = Repo{"ethereum", "go-ethereum"}

func newSource(t *testing.T, f *fakeGitHub, c *clock) *Source {
	return &Source{BaseURL: f.srv.URL, Token: testToken, CacheDir: t.TempDir(), TTL: time.Hour, Now: c.Now}
}

func TestReleasesFetchAndCache(t *testing.T) {
	f := newFakeGitHub(t)
	c := &clock{t: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	s := newSource(t, f, c)

	// 1. First call fetches from GitHub.
	res, err := s.Releases(context.Background(), geth)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Releases) != 100 || res.Releases[0].Tag != "v1.17.10" || !res.FetchedAt.Equal(c.t) || res.RefreshErr != nil {
		t.Fatalf("unexpected result: %d releases, first %q, fetched %s, err %v",
			len(res.Releases), res.Releases[0].Tag, res.FetchedAt, res.RefreshErr)
	}
	if got := f.lastAuth.Load(); got != "Bearer "+testToken {
		t.Errorf("Authorization header = %q", got)
	}

	// 2. Within the TTL the cache is used without a request.
	c.t = c.t.Add(30 * time.Minute)
	if _, err := s.Releases(context.Background(), geth); err != nil {
		t.Fatal(err)
	}
	if n := f.requests.Load(); n != 1 {
		t.Errorf("requests = %d, want 1 (cache hit)", n)
	}

	// 3. After the TTL a conditional request is sent; 304 refreshes the
	// timestamp and keeps the cached data.
	c.t = c.t.Add(2 * time.Hour)
	res, err = s.Releases(context.Background(), geth)
	if err != nil {
		t.Fatal(err)
	}
	if n := f.requests.Load(); n != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
	if got := f.lastINM.Load(); got != `W/"abc123"` {
		t.Errorf("If-None-Match = %q", got)
	}
	if len(res.Releases) != 100 || !res.FetchedAt.Equal(c.t) {
		t.Errorf("after 304: %d releases, fetched %s", len(res.Releases), res.FetchedAt)
	}
}

func TestCacheFileHoldsNoSecretsOrHeaders(t *testing.T) {
	f := newFakeGitHub(t)
	c := &clock{t: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	s := newSource(t, f, c)
	if _, err := s.Releases(context.Background(), geth); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(s.CacheDir, "github", "ethereum", "go-ethereum.json"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, forbidden := range []string{testToken, "Bearer", "Authorization", "X-RateLimit", "Content-Type"} {
		if strings.Contains(content, forbidden) {
			t.Errorf("cache file contains %q", forbidden)
		}
	}
	for _, want := range []string{`"repo":"ethereum/go-ethereum"`, `"etag":"W/\"abc123\""`, `"tag_name":"v1.17.10"`} {
		if !strings.Contains(content, want) {
			t.Errorf("cache file lacks %s", want)
		}
	}
	// No leftover temporary files from the atomic write.
	entries, _ := os.ReadDir(filepath.Join(s.CacheDir, "github", "ethereum"))
	if len(entries) != 1 {
		t.Errorf("cache dir has %d entries, want 1", len(entries))
	}
}

func TestReleasesGitHubDown(t *testing.T) {
	f := newFakeGitHub(t)
	c := &clock{t: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	s := newSource(t, f, c)
	fetched := c.t
	if _, err := s.Releases(context.Background(), geth); err != nil {
		t.Fatal(err)
	}

	// With cache: old data plus RefreshErr, FetchedAt unchanged.
	f.status.Store(http.StatusInternalServerError)
	c.t = c.t.Add(3 * 24 * time.Hour)
	res, err := s.Releases(context.Background(), geth)
	if err != nil {
		t.Fatalf("expected cached data, got error %v", err)
	}
	if res.RefreshErr == nil || !strings.Contains(res.RefreshErr.Error(), "500") {
		t.Errorf("RefreshErr = %v, want HTTP 500", res.RefreshErr)
	}
	if !res.FetchedAt.Equal(fetched) || len(res.Releases) != 100 {
		t.Errorf("stale result: fetched %s, %d releases", res.FetchedAt, len(res.Releases))
	}

	// Without cache: an error.
	s.CacheDir = t.TempDir()
	if _, err := s.Releases(context.Background(), geth); err == nil {
		t.Error("expected an error without cache")
	}
}

func TestReleasesRateLimit(t *testing.T) {
	f := newFakeGitHub(t)
	f.status.Store(http.StatusForbidden)
	s := newSource(t, f, &clock{t: time.Now()})

	_, err := s.Releases(context.Background(), geth)
	if err == nil || !strings.Contains(err.Error(), "rate limit exceeded") || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("error = %v, want rate limit hint", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error leaks token: %v", err)
	}
}

func TestReleasesWithoutTokenSendsNoAuthorization(t *testing.T) {
	f := newFakeGitHub(t)
	s := newSource(t, f, &clock{t: time.Now()})
	s.Token = ""
	if _, err := s.Releases(context.Background(), geth); err != nil {
		t.Fatal(err)
	}
	if got := f.lastAuth.Load(); got != "" {
		t.Errorf("Authorization header = %q, want none", got)
	}
}

func TestReleasesCorruptCacheIsRefetched(t *testing.T) {
	f := newFakeGitHub(t)
	c := &clock{t: time.Now()}
	s := newSource(t, f, c)
	path := filepath.Join(s.CacheDir, "github", "ethereum", "go-ethereum.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("{not json"), 0o644)

	res, err := s.Releases(context.Background(), geth)
	if err != nil || len(res.Releases) != 100 || f.requests.Load() != 1 {
		t.Fatalf("got %d releases, err %v, %d requests", len(res.Releases), err, f.requests.Load())
	}
}

func TestReleasesFollowsRedirect(t *testing.T) {
	// Moved repositories answer with 301 to the new path.
	f := newFakeGitHub(t)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, f.srv.URL+"/repos/ethereum/go-ethereum/releases?per_page=100", http.StatusMovedPermanently)
	}))
	defer redirect.Close()

	s := newSource(t, f, &clock{t: time.Now()})
	s.BaseURL = redirect.URL
	res, err := s.Releases(context.Background(), Repo{"old-owner", "go-ethereum"})
	if err != nil || len(res.Releases) != 100 {
		t.Fatalf("got %d releases, err %v", len(res.Releases), err)
	}
}
