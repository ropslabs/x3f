package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ropslabs/x3f/internal/httpjson"
	"github.com/ropslabs/x3f/internal/software"
)

const (
	// DefaultBaseURL is the GitHub REST API.
	DefaultBaseURL = "https://api.github.com"
	// maxBodyBytes limits each response page. 100 releases including
	// their changelogs are a few MB; the limit guards against surprises.
	maxBodyBytes = 32 << 20
)

// Source fetches releases and security advisories from GitHub and caches
// them on disk.
//
// The cache file per repository and list holds only the ETag, the time of
// the last successful fetch and the reduced list; never request or
// response headers and never the token.
type Source struct {
	// HTTP is the client for GitHub requests; nil means a default client.
	HTTP *http.Client
	// BaseURL defaults to DefaultBaseURL (tests point it to a mock server).
	BaseURL string
	// Token is an optional GitHub token (from GITHUB_TOKEN). With a token
	// the rate limit is 5000 instead of 60 requests per hour, and
	// conditional requests answered with 304 are free.
	Token string
	// CacheDir is where cache files are stored; empty disables the cache.
	CacheDir string
	// TTL is how long cached data is used without asking GitHub at all.
	TTL time.Duration
	// Now returns the current time; nil means time.Now. Tests override it.
	Now func() time.Time
	// UserAgent is sent to GitHub; empty means "x3f".
	UserAgent string
}

// Result is the release list of one repository.
type Result struct {
	Releases []Release
	// FetchedAt is when the data was last confirmed by GitHub.
	FetchedAt time.Time
	// RefreshErr is set when GitHub could not be asked and older cached
	// data is returned instead. The caller decides how much that matters.
	RefreshErr error
	// Cached is true when the data came from the cache without asking
	// GitHub in this call (fresh within the TTL).
	Cached bool
}

// list describes one kind of per-repository list that x3f fetches.
type list struct {
	// kind names the list in errors and marks its cache files.
	kind string
	// path is appended to /repos/{owner}/{repo}/.
	path string
	// file is appended to the repository name to form the cache file name.
	file string
	// maxPages is how many pages are fetched by following the Link
	// header. Releases need only the newest page.
	maxPages int
	// format is the version of the cached items. The cache stores the
	// decoded items, so a field added to them is missing in older cache
	// files; raising format makes those count as missing.
	format int
}

var (
	releasesList = list{
		kind: "releases", path: "releases?per_page=100",
		file: ".json", maxPages: 1,
		// 1: releases carry html_url and mentions_security (x3f v0.3.0).
		format: 1,
	}
	advisoriesList = list{
		kind: "security advisories", path: "security-advisories?state=published&per_page=100",
		file: ".advisories.json", maxPages: 10,
		// 1: vulnerabilities carry the package name (x3f v0.3.0).
		format: 1,
	}
)

// listResult is the outcome of getList; Result and AdvisoryResult are
// built from it.
type listResult[T any] struct {
	Items      []T
	FetchedAt  time.Time
	RefreshErr error
	Cached     bool
}

// cacheEntry is the on-disk format. Kind guards against reading one list as
// another, and makes cache files from x3f v0.1.0 (which had no kind) count
// as missing: they are simply fetched again. Format does the same for
// files written before the list's current format (see list.format).
type cacheEntry[T any] struct {
	Repo      string    `json:"repo"`
	Kind      string    `json:"kind"`
	Format    int       `json:"format,omitempty"`
	ETag      string    `json:"etag,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
	Items     []T       `json:"items"`
}

// Releases returns the releases of repo, from the cache if it is fresh,
// otherwise from GitHub. If GitHub fails but cached data exists, the cached
// data is returned with RefreshErr set. An error is returned only if there
// is no data at all.
func (s *Source) Releases(ctx context.Context, repo software.Repo) (Result, error) {
	r, err := getList[Release](ctx, s, repo, releasesList, false)
	return releaseResult(r), err
}

// Refresh is like Releases but ignores the TTL and always asks GitHub (with
// a conditional request, so unchanged data costs little). Use it when the
// cached data looks outdated, e.g. a node runs a version newer than every
// cached release.
func (s *Source) Refresh(ctx context.Context, repo software.Repo) (Result, error) {
	r, err := getList[Release](ctx, s, repo, releasesList, true)
	return releaseResult(r), err
}

func releaseResult(r listResult[Release]) Result {
	return Result{Releases: r.Items, FetchedAt: r.FetchedAt, RefreshErr: r.RefreshErr, Cached: r.Cached}
}

// getList is a generic function: T is Release or Advisory. Go methods
// cannot have type parameters of their own, so this is a plain function
// that takes the Source as an argument.
func getList[T any](ctx context.Context, s *Source, repo software.Repo, l list, force bool) (listResult[T], error) {
	now := s.now()
	cached, haveCache := readCache[T](s, repo, l)
	if haveCache && !force && now.Sub(cached.FetchedAt) < s.TTL {
		return listResult[T]{Items: cached.Items, FetchedAt: cached.FetchedAt, Cached: true}, nil
	}

	etag := ""
	if haveCache {
		etag = cached.ETag
	}
	fresh, notModified, newETag, err := fetchList[T](ctx, s, repo, l, etag)
	switch {
	case err != nil && haveCache:
		return listResult[T]{Items: cached.Items, FetchedAt: cached.FetchedAt, RefreshErr: err}, nil
	case err != nil:
		return listResult[T]{}, err
	case notModified:
		// 304: our data is still current. Only the timestamp changes.
		cached.FetchedAt = now
		writeCache(s, repo, l, cached)
		return listResult[T]{Items: cached.Items, FetchedAt: now}, nil
	default:
		entry := cacheEntry[T]{Repo: repo.String(), Kind: l.kind, Format: l.format, ETag: newETag, FetchedAt: now, Items: fresh}
		writeCache(s, repo, l, entry)
		return listResult[T]{Items: fresh, FetchedAt: now}, nil
	}
}

// fetchList asks GitHub for the list, following up to l.maxPages pages.
// The ETag is only used for single-page lists: a 304 for the first page
// says nothing about the later ones.
func fetchList[T any](ctx context.Context, s *Source, repo software.Repo, l list, etag string) (items []T, notModified bool, newETag string, err error) {
	base := s.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, false, "", fmt.Errorf("GitHub base URL: %w", err)
	}
	next := fmt.Sprintf("%s/repos/%s/%s/%s", strings.TrimSuffix(base, "/"), repo.Owner, repo.Name, l.path)

	for page := 1; ; page++ {
		var pageItems []T
		var link string
		pageItems, notModified, newETag, link, err = fetchPage[T](ctx, s, next, repo, l, etag)
		if err != nil || notModified {
			return nil, notModified, newETag, err
		}
		items = append(items, pageItems...)
		if link == "" || page >= l.maxPages {
			if link != "" && l.maxPages > 1 {
				// Missing pages could hide an advisory; better no data
				// than data that looks complete but is not.
				return nil, false, "", fmt.Errorf("GitHub %s of %s: more than %d pages", l.kind, repo, l.maxPages)
			}
			if page > 1 {
				newETag = ""
			}
			return items, false, newETag, nil
		}
		// The next page must be on the same host as the API: requests
		// carry the token, which must never go anywhere else.
		u, err := url.Parse(link)
		if err != nil || u.Scheme != baseURL.Scheme || u.Host != baseURL.Host {
			return nil, false, "", fmt.Errorf("GitHub %s of %s: next page link points to another host", l.kind, repo)
		}
		next, etag = link, ""
	}
}

// fetchPage requests one page. link is the URL of the next page from the
// Link header, or empty.
func fetchPage[T any](ctx context.Context, s *Source, pageURL string, repo software.Repo, l list, etag string) (items []T, notModified bool, newETag, link string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, false, "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	ua := s.UserAgent
	if ua == "" {
		ua = "x3f"
	}
	req.Header.Set("User-Agent", ua)
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	httpClient := s.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	// Redirects (e.g. for moved repositories) are followed by the client,
	// as GitHub recommends. Go drops the Authorization header if a redirect
	// leaves the original host.
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, false, "", "", fmt.Errorf("GitHub %s of %s: %w", l.kind, repo, describeError(err))
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return nil, true, etag, "", nil
	case (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
		resp.Header.Get("X-RateLimit-Remaining") == "0":
		return nil, false, "", "", fmt.Errorf("GitHub %s of %s: rate limit exceeded (set GITHUB_TOKEN for a higher limit)", l.kind, repo)
	case resp.StatusCode != http.StatusOK:
		return nil, false, "", "", fmt.Errorf("GitHub %s of %s: unexpected HTTP status %s", l.kind, repo, httpjson.StatusText(resp.StatusCode))
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&items); err != nil {
		return nil, false, "", "", fmt.Errorf("GitHub %s of %s: decode response: %w", l.kind, repo, err)
	}
	return items, false, resp.Header.Get("ETag"), nextLink(resp.Header.Get("Link")), nil
}

// nextLink returns the URL marked rel="next" in a Link header, e.g.
//
//	<https://api.github.com/repositories/1/security-advisories?page=2>; rel="next", <…>; rel="last"
//
// (format: RFC 8288; GitHub documents it under "Using pagination in the
// REST API").
func nextLink(header string) string {
	for part := range strings.SplitSeq(header, ",") {
		target, params, ok := strings.Cut(strings.TrimSpace(part), ";")
		if !ok || !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			continue
		}
		for p := range strings.SplitSeq(params, ";") {
			if strings.TrimSpace(p) == `rel="next"` {
				return strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">")
			}
		}
	}
	return ""
}

// describeError shortens transport errors. GitHub URLs contain no secrets,
// but the *url.Error wrapper repeats the long URL, which is just noise.
func describeError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("timeout")
	}
	var urlErr interface{ Unwrap() error }
	if errors.As(err, &urlErr) {
		if inner := urlErr.Unwrap(); inner != nil {
			return inner
		}
	}
	return err
}

func (s *Source) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func cachePath(s *Source, repo software.Repo, l list) string {
	return filepath.Join(s.CacheDir, "github", repo.Owner, repo.Name+l.file)
}

// readCache returns the cached entry. Missing or unreadable cache files are
// treated as "no cache"; the data is simply fetched again.
func readCache[T any](s *Source, repo software.Repo, l list) (cacheEntry[T], bool) {
	if s.CacheDir == "" {
		return cacheEntry[T]{}, false
	}
	data, err := os.ReadFile(cachePath(s, repo, l))
	if err != nil {
		return cacheEntry[T]{}, false
	}
	var e cacheEntry[T]
	if err := json.Unmarshal(data, &e); err != nil || e.Repo != repo.String() || e.Kind != l.kind || e.Format != l.format {
		return cacheEntry[T]{}, false
	}
	return e, true
}

// writeCache stores an entry. Errors are ignored on purpose: a cache that
// cannot be written only costs another request next time.
func writeCache[T any](s *Source, repo software.Repo, l list, e cacheEntry[T]) {
	if s.CacheDir == "" {
		return
	}
	path := cachePath(s, repo, l)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	// Write to a temporary file and rename it. A rename within one
	// directory is atomic, so a crash or a parallel run never leaves a
	// half-written cache file behind.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	os.Rename(tmp.Name(), path)
}
