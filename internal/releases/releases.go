package releases

import (
	"time"

	"github.com/ropslabs/x3f/internal/software"
)

// Release is one GitHub release, reduced to the fields x3f uses.
type Release struct {
	Tag         string    `json:"tag_name"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
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
