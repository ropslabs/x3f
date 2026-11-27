package releases

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ropslabs/x3f/internal/software"
)

// advisoryFixture returns the recorded advisories of repo
// (testdata/github/advisories, see SOURCES.md).
func advisoryFixture(t *testing.T, repo software.Repo) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "github", "advisories", repo.Owner+"_"+repo.Name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// fakeAdvisories serves the recorded advisories of one repository. With
// pageSize > 0 the list is split into pages linked by a Link header in the
// format of the GitHub docs ("Using pagination in the REST API").
type fakeAdvisories struct {
	srv      *httptest.Server
	requests atomic.Int32
	lastINM  atomic.Value
	lastAuth atomic.Value
	// linkHost, if set, replaces the host in the Link header.
	linkHost string
	// endless makes every page link to another one.
	endless bool
}

func newFakeAdvisories(t *testing.T, repo software.Repo, pageSize int) *fakeAdvisories {
	t.Helper()
	var all []json.RawMessage
	if err := json.Unmarshal(advisoryFixture(t, repo), &all); err != nil {
		t.Fatal(err)
	}
	f := &fakeAdvisories{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		f.lastINM.Store(r.Header.Get("If-None-Match"))
		f.lastAuth.Store(r.Header.Get("Authorization"))
		q := r.URL.Query()
		if r.URL.Path != "/repos/"+repo.String()+"/security-advisories" || q.Get("state") != "published" || q.Get("per_page") != "100" {
			http.NotFound(w, r)
			return
		}
		const etag = `W/"adv1"`
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		items := all
		if pageSize > 0 {
			page := 1
			fmt.Sscan(q.Get("page"), &page)
			start := min((page-1)*pageSize, len(all))
			end := min(start+pageSize, len(all))
			items = all[start:end]
			if end < len(all) || f.endless {
				host := r.Host
				if f.linkHost != "" {
					host = f.linkHost
				}
				w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?state=published&per_page=100&page=%d>; rel="next"`, host, r.URL.Path, page+1))
			}
		}
		w.Header().Set("ETag", etag)
		json.NewEncoder(w).Encode(items)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

var besu = software.Repo{Owner: "besu-eth", Name: "besu"}

func TestAdvisoriesFetchAndCache(t *testing.T) {
	f := newFakeAdvisories(t, geth, 0)
	c := &clock{t: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}
	s := &Source{BaseURL: f.srv.URL, Token: testToken, CacheDir: t.TempDir(), TTL: time.Hour, Now: c.Now}

	res, err := s.Advisories(context.Background(), geth)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Advisories) != 17 {
		t.Fatalf("got %d advisories, want 17", len(res.Advisories))
	}
	first := res.Advisories[0]
	want := Advisory{
		GHSAID:      "GHSA-m6j8-rg6r-7mv8",
		CVEID:       "CVE-2026-26315",
		HTMLURL:     "https://github.com/ethereum/go-ethereum/security/advisories/GHSA-m6j8-rg6r-7mv8",
		Summary:     "Improper ECIES Public Key Validation in RLPx Handshake",
		Severity:    "medium",
		PublishedAt: time.Date(2026, 2, 17, 0, 0, 0, 0, time.UTC),
		Vulnerabilities: []AdvisoryVulnerability{
			{VulnerableVersionRange: "<= 1.16.8", PatchedVersions: ">= 1.16.9"},
		},
	}
	// The publication time of day is irrelevant here; compare the date.
	first.PublishedAt = first.PublishedAt.Truncate(24 * time.Hour)
	if fmt.Sprint(first) != fmt.Sprint(want) {
		t.Errorf("first advisory\n got %+v\nwant %+v", first, want)
	}
	if got := f.lastAuth.Load(); got != "Bearer "+testToken {
		t.Errorf("Authorization header = %q", got)
	}

	// Within the TTL: from the cache.
	c.t = c.t.Add(30 * time.Minute)
	res, err = s.Advisories(context.Background(), geth)
	if err != nil || !res.Cached || len(res.Advisories) != 17 || f.requests.Load() != 1 {
		t.Fatalf("cache hit: cached=%v, %d advisories, err %v, %d requests", res.Cached, len(res.Advisories), err, f.requests.Load())
	}

	// After the TTL: conditional request, 304 keeps the data.
	c.t = c.t.Add(2 * time.Hour)
	res, err = s.Advisories(context.Background(), geth)
	if err != nil || len(res.Advisories) != 17 || f.lastINM.Load() != `W/"adv1"` {
		t.Fatalf("after 304: %d advisories, err %v, If-None-Match %q", len(res.Advisories), err, f.lastINM.Load())
	}

	// A separate cache file, next to the one for releases, with no
	// secrets or headers in it.
	data, err := os.ReadFile(filepath.Join(s.CacheDir, "github", "ethereum", "go-ethereum.advisories.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{testToken, "Bearer", "Authorization", "X-RateLimit", "Content-Type"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("cache file contains %q", forbidden)
		}
	}
	if !strings.Contains(string(data), `"kind":"security advisories"`) {
		t.Error("cache file lacks its kind")
	}
}

func TestAdvisoriesAndReleasesDoNotShareCache(t *testing.T) {
	rel := newFakeGitHub(t)
	adv := newFakeAdvisories(t, geth, 0)
	c := &clock{t: time.Now()}
	dir := t.TempDir()
	s := &Source{BaseURL: rel.srv.URL, CacheDir: dir, TTL: time.Hour, Now: c.Now}
	if _, err := s.Releases(context.Background(), geth); err != nil {
		t.Fatal(err)
	}
	s.BaseURL = adv.srv.URL
	res, err := s.Advisories(context.Background(), geth)
	if err != nil || res.Cached || len(res.Advisories) != 17 {
		t.Fatalf("advisories: cached=%v, %d, err %v", res.Cached, len(res.Advisories), err)
	}
	s.BaseURL = rel.srv.URL
	r, err := s.Releases(context.Background(), geth)
	if err != nil || !r.Cached || len(r.Releases) != 100 {
		t.Fatalf("releases: cached=%v, %d, err %v", r.Cached, len(r.Releases), err)
	}
}

func TestAdvisoriesPagination(t *testing.T) {
	f := newFakeAdvisories(t, besu, 20) // 44 advisories: pages of 20, 20, 4
	c := &clock{t: time.Now()}
	s := &Source{BaseURL: f.srv.URL, Token: testToken, CacheDir: t.TempDir(), TTL: time.Hour, Now: c.Now}

	res, err := s.Advisories(context.Background(), besu)
	if err != nil {
		t.Fatal(err)
	}
	var want []Advisory
	if err := json.Unmarshal(advisoryFixture(t, besu), &want); err != nil {
		t.Fatal(err)
	}
	if len(res.Advisories) != 44 || f.requests.Load() != 3 {
		t.Fatalf("got %d advisories in %d requests, want 44 in 3", len(res.Advisories), f.requests.Load())
	}
	for i := range want {
		if res.Advisories[i].GHSAID != want[i].GHSAID {
			t.Fatalf("advisory %d = %s, want %s (order must be kept)", i, res.Advisories[i].GHSAID, want[i].GHSAID)
		}
	}

	// A multi-page list keeps no ETag: a 304 for page 1 would not prove
	// that the later pages are unchanged.
	c.t = c.t.Add(2 * time.Hour)
	if _, err := s.Advisories(context.Background(), besu); err != nil {
		t.Fatal(err)
	}
	if got := f.lastINM.Load(); got != "" {
		t.Errorf("If-None-Match = %q, want none for a multi-page list", got)
	}
}

func TestAdvisoriesLinkToOtherHost(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request to the other host (Authorization %q)", r.Header.Get("Authorization"))
	}))
	defer other.Close()
	f := newFakeAdvisories(t, besu, 20)
	f.linkHost = strings.TrimPrefix(other.URL, "http://")
	s := &Source{BaseURL: f.srv.URL, Token: testToken, TTL: time.Hour}

	_, err := s.Advisories(context.Background(), besu)
	if err == nil || !strings.Contains(err.Error(), "another host") {
		t.Fatalf("error = %v, want a refused link", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("error leaks token: %v", err)
	}
}

func TestAdvisoriesTooManyPages(t *testing.T) {
	f := newFakeAdvisories(t, besu, 1)
	f.endless = true
	s := &Source{BaseURL: f.srv.URL, TTL: time.Hour}

	res, err := s.Advisories(context.Background(), besu)
	if err == nil || !strings.Contains(err.Error(), "more than 10 pages") {
		t.Fatalf("error = %v, want the page limit", err)
	}
	if len(res.Advisories) != 0 || f.requests.Load() != 10 {
		t.Errorf("got %d advisories in %d requests, want none in 10", len(res.Advisories), f.requests.Load())
	}
}

func TestAdvisoriesOldCacheFormatIsRefetched(t *testing.T) {
	// x3f v0.1.0 wrote release caches without "kind" and with "releases"
	// instead of "items". Such a file must not count as an empty list.
	f := newFakeGitHub(t)
	c := &clock{t: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	s := newSource(t, f, c)
	path := filepath.Join(s.CacheDir, "github", "ethereum", "go-ethereum.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	old := `{"repo":"ethereum/go-ethereum","etag":"W/\"abc123\"","fetched_at":"2026-11-24T11:50:00Z","releases":[{"tag_name":"v1.17.10"}]}`
	os.WriteFile(path, []byte(old), 0o644)

	res, err := s.Releases(context.Background(), geth)
	if err != nil || res.Cached || len(res.Releases) != 100 || f.requests.Load() != 1 {
		t.Fatalf("cached=%v, %d releases, err %v, %d requests", res.Cached, len(res.Releases), err, f.requests.Load())
	}
	if got := f.lastINM.Load(); got != "" {
		t.Errorf("If-None-Match = %q, want none: the old ETag belongs to an unusable entry", got)
	}
}

func TestAdvisoriesCacheWithoutPackagesIsRefetched(t *testing.T) {
	// x3f v0.2.0 cached advisories without package names and without
	// "format". Kept, such a file would hide the names until the list
	// changes on GitHub, since a 304 keeps the cached items.
	f := newFakeAdvisories(t, geth, 0)
	c := &clock{t: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}
	s := &Source{BaseURL: f.srv.URL, CacheDir: t.TempDir(), TTL: time.Hour, Now: c.Now}
	path := filepath.Join(s.CacheDir, "github", "ethereum", "go-ethereum.advisories.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	old := `{"repo":"ethereum/go-ethereum","kind":"security advisories","etag":"W/\"adv1\"","fetched_at":"2026-11-25T11:50:00Z",` +
		`"items":[{"ghsa_id":"GHSA-m6j8-rg6r-7mv8","vulnerabilities":[{"vulnerable_version_range":"<= 1.16.8","patched_versions":">= 1.16.9"}]}]}`
	os.WriteFile(path, []byte(old), 0o644)

	res, err := s.Advisories(context.Background(), geth)
	if err != nil || res.Cached || len(res.Advisories) != 17 || f.requests.Load() != 1 {
		t.Fatalf("cached=%v, %d advisories, err %v, %d requests", res.Cached, len(res.Advisories), err, f.requests.Load())
	}
	if got := f.lastINM.Load(); got != "" {
		t.Errorf("If-None-Match = %q, want none: the old ETag belongs to an unusable entry", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `"format":1`) || !strings.Contains(string(data), `"package":{"name":"github.com/ethereum/go-ethereum"}`) {
		t.Errorf("rewritten cache file lacks the format or the package names (err %v)", err)
	}
}

func TestWithoutWithdrawn(t *testing.T) {
	withdrawn := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := withoutWithdrawn([]Advisory{{GHSAID: "a"}, {GHSAID: "b", WithdrawnAt: &withdrawn}, {GHSAID: "c"}})
	if len(got) != 2 || got[0].GHSAID != "a" || got[1].GHSAID != "c" {
		t.Errorf("got %+v", got)
	}
}

func TestNextLink(t *testing.T) {
	tests := []struct{ header, want string }{
		// Examples from the GitHub docs, "Using pagination in the REST API".
		{`<https://api.github.com/repositories/1300192/issues?page=2>; rel="prev", <https://api.github.com/repositories/1300192/issues?page=4>; rel="next", <https://api.github.com/repositories/1300192/issues?page=515>; rel="last", <https://api.github.com/repositories/1300192/issues?page=1>; rel="first"`,
			"https://api.github.com/repositories/1300192/issues?page=4"},
		{`<https://api.github.com/repositories/1300192/issues?per_page=2&page=2>; rel="next", <https://api.github.com/repositories/1300192/issues?per_page=2&page=7715>; rel="last"`,
			"https://api.github.com/repositories/1300192/issues?per_page=2&page=2"},
		// Last page: no "next".
		{`<https://api.github.com/repositories/1300192/issues?page=514>; rel="prev", <https://api.github.com/repositories/1300192/issues?page=1>; rel="first"`, ""},
		{"", ""},
		{"garbage", ""},
	}
	for _, tt := range tests {
		if got := nextLink(tt.header); got != tt.want {
			t.Errorf("nextLink(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}
