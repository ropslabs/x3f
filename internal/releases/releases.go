package releases

import (
	"encoding/json"
	"regexp"
	"slices"
	"time"

	"github.com/ropslabs/x3f/internal/software"
)

// Release is one GitHub release, reduced to the fields x3f uses.
type Release struct {
	Tag         string    `json:"tag_name"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
	// HTMLURL is the release page on GitHub.
	HTMLURL string `json:"html_url,omitempty"`
	// MentionsSecurity is set if the release notes contain the word
	// "security" (any case, as a whole word). It is decided when the
	// release is read from GitHub; the cache keeps only the result, not
	// the release notes.
	MentionsSecurity bool `json:"mentions_security,omitempty"`
}

// securityRe matches the word "security" in release notes, e.g. in
// "a security release" or a heading "## Security & correctness".
var securityRe = regexp.MustCompile(`(?i)\bsecurity\b`)

// UnmarshalJSON implements json.Unmarshaler: it reads a release from the
// GitHub API, whose release notes are in "body", or from the cache, which
// has mentions_security instead.
func (r *Release) UnmarshalJSON(data []byte) error {
	// plain has the fields of Release but not its methods, so decoding
	// into it does not call this method again.
	type plain Release
	var aux struct {
		plain
		Body string `json:"body"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*r = Release(aux.plain)
	r.MentionsSecurity = r.MentionsSecurity || securityRe.MatchString(aux.Body)
	return nil
}

// stable reports whether r counts as a stable release of client: not a
// draft, not flagged as pre-release, a parseable version tag and a release
// suffix. Both checks are needed because the GitHub flag is unreliable in
// both directions (e.g. reth v1.0.0-rc.2 is not flagged, besu 24.5.0 is).
func stable(client software.Name, r Release) (Version, bool) {
	if r.Draft || r.Prerelease {
		return Version{}, false
	}
	v, ok := ParseVersion(r.Tag)
	if !ok || software.ClassifySuffix(client, v.Suffix) != software.KindRelease {
		return Version{}, false
	}
	return v, true
}

// LatestStable returns the highest stable version. It uses the version
// number, not the publication date, so a backport to an older release line
// published later does not count as "latest".
func LatestStable(client software.Name, rels []Release) (Version, bool) {
	var latest Version
	found := false
	for _, r := range rels {
		v, ok := stable(client, r)
		if ok && (!found || Compare(client, v, latest) > 0) {
			latest, found = v, true
		}
	}
	return latest, found
}

// NewestKnown returns the highest version among all releases that are not
// drafts, including pre-releases and any suffix. A running version above
// this is not any known release: a proxy's made-up version, a custom build,
// or a release newer than the data.
func NewestKnown(client software.Name, rels []Release) (Version, bool) {
	var newest Version
	found := false
	for _, r := range rels {
		if r.Draft {
			continue
		}
		v, ok := ParseVersion(r.Tag)
		if ok && (!found || Compare(client, v, newest) > 0) {
			newest, found = v, true
		}
	}
	return newest, found
}

// NewerThanAll reports whether running is newer than every known release.
func NewerThanAll(client software.Name, rels []Release, running Version) bool {
	newest, ok := NewestKnown(client, rels)
	return ok && Compare(client, running, newest) > 0
}

// ReleasedAtLeast reports whether a stable release of v or newer exists. An
// advisory may name a patched version before that release is out.
func ReleasedAtLeast(client software.Name, rels []Release, v Version) bool {
	for _, r := range rels {
		if s, ok := stable(client, r); ok && Compare(client, s, v) >= 0 {
			return true
		}
	}
	return false
}

// Missing returns the stable releases newer than running, lowest version
// first.
func Missing(client software.Name, rels []Release, running Version) []Release {
	type found struct {
		r Release
		v Version
	}
	var out []found
	for _, r := range rels {
		if v, ok := stable(client, r); ok && Compare(client, v, running) > 0 {
			out = append(out, found{r, v})
		}
	}
	slices.SortStableFunc(out, func(a, b found) int { return Compare(client, a.v, b.v) })
	rs := make([]Release, len(out))
	for i, f := range out {
		rs[i] = f.r
	}
	return rs
}

// FirstMissing returns the earliest published stable release that is newer
// than running: the moment the node started to be outdated. ok is false if
// no newer stable release exists.
func FirstMissing(client software.Name, rels []Release, running Version) (Release, bool) {
	var first Release
	found := false
	for _, r := range rels {
		v, ok := stable(client, r)
		if !ok || Compare(client, v, running) <= 0 {
			continue
		}
		if !found || r.PublishedAt.Before(first.PublishedAt) {
			first, found = r, true
		}
	}
	return first, found
}
