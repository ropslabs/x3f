package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// DefaultBaseURL is the GitHub REST API.
	DefaultBaseURL = "https://api.github.com"
	// maxBodyBytes limits the releases response. 100 releases including
	// their changelogs are a few MB; the limit guards against surprises.
	maxBodyBytes = 32 << 20
)

// Source fetches releases from GitHub and caches them on disk.
//
// The cache file per repository holds only the ETag, the time of the last
// successful fetch and the reduced release list; never request or response
// headers and never the token.
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
}

// Result is the release list of one repository.
type Result struct {
	Releases []Release
	// FetchedAt is when the data was last confirmed by GitHub.
	FetchedAt time.Time
	// RefreshErr is set when GitHub could not be asked and older cached
	// data is returned instead. The caller decides how much that matters.
	RefreshErr error
}

// cacheEntry is the on-disk format.
type cacheEntry struct {
	Repo      string    `json:"repo"`
	ETag      string    `json:"etag,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
	Releases  []Release `json:"releases"`
}

// Releases returns the releases of repo, from the cache if it is fresh,
// otherwise from GitHub. If GitHub fails but cached data exists, the cached
// data is returned with RefreshErr set. An error is returned only if there
// is no data at all.
func (s *Source) Releases(ctx context.Context, repo Repo) (Result, error) {
	now := s.now()
	cached, haveCache := s.readCache(repo)
	if haveCache && now.Sub(cached.FetchedAt) < s.TTL {
		return Result{Releases: cached.Releases, FetchedAt: cached.FetchedAt}, nil
	}

	etag := ""
	if haveCache {
		etag = cached.ETag
	}
	fresh, notModified, newETag, err := s.fetch(ctx, repo, etag)
	switch {
	case err != nil && haveCache:
		return Result{Releases: cached.Releases, FetchedAt: cached.FetchedAt, RefreshErr: err}, nil
	case err != nil:
		return Result{}, err
	case notModified:
		// 304: our data is still current. Only the timestamp changes.
		cached.FetchedAt = now
		s.writeCache(repo, cached)
		return Result{Releases: cached.Releases, FetchedAt: now}, nil
	default:
		entry := cacheEntry{Repo: repo.String(), ETag: newETag, FetchedAt: now, Releases: fresh}
		s.writeCache(repo, entry)
		return Result{Releases: fresh, FetchedAt: now}, nil
	}
}

func (s *Source) fetch(ctx context.Context, repo Repo, etag string) (rels []Release, notModified bool, newETag string, err error) {
	base := s.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=100", base, repo.Owner, repo.Name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "x3f")
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
		return nil, false, "", fmt.Errorf("GitHub releases of %s: %w", repo, describeError(err))
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return nil, true, etag, nil
	case (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
		resp.Header.Get("X-RateLimit-Remaining") == "0":
		return nil, false, "", fmt.Errorf("GitHub releases of %s: rate limit exceeded (set GITHUB_TOKEN for a higher limit)", repo)
	case resp.StatusCode != http.StatusOK:
		return nil, false, "", fmt.Errorf("GitHub releases of %s: unexpected HTTP status %s", repo, resp.Status)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&rels); err != nil {
		return nil, false, "", fmt.Errorf("GitHub releases of %s: decode response: %w", repo, err)
	}
	return rels, false, resp.Header.Get("ETag"), nil
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

func (s *Source) cachePath(repo Repo) string {
	return filepath.Join(s.CacheDir, "github", repo.Owner, repo.Name+".json")
}

// readCache returns the cached entry. Missing or unreadable cache files are
// treated as "no cache"; the data is simply fetched again.
func (s *Source) readCache(repo Repo) (cacheEntry, bool) {
	if s.CacheDir == "" {
		return cacheEntry{}, false
	}
	data, err := os.ReadFile(s.cachePath(repo))
	if err != nil {
		return cacheEntry{}, false
	}
	var e cacheEntry
	if err := json.Unmarshal(data, &e); err != nil || e.Repo != repo.String() {
		return cacheEntry{}, false
	}
	return e, true
}

// writeCache stores an entry. Errors are ignored on purpose: a cache that
// cannot be written only costs another request next time.
func (s *Source) writeCache(repo Repo, e cacheEntry) {
	if s.CacheDir == "" {
		return
	}
	path := s.cachePath(repo)
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
