// Package status turns query results into green / yellow / red
// assessments with human-readable reasons.
//
// Rules (agreed for build step 3):
//
//	Version  green:  latest stable release or newer, but not newer than
//	                 every known release
//	         yellow: outdated within the grace period; pre-release, dev build
//	                 or unrecognized suffix; newer than every known release
//	                 (proxy or custom build); client or version not recognized;
//	                 latest release unknown or release data older than 7 days;
//	                 version request rejected by the server
//	         red:    outdated longer than the grace period (counted from the
//	                 first newer stable release); client unreachable
//	Sync     green:  synced (CL: and neither optimistic nor EL offline)
//	         yellow: indexing; optimistic; at most 32 blocks/slots behind;
//	                 target unknown; malformed sync response
//	         red:    EL offline; more than 32 blocks/slots behind;
//	                 client unreachable
package status

import (
	"fmt"
	"slices"
	"time"

	"github.com/ropslabs/x3f/internal/ethereum"
	"github.com/ropslabs/x3f/internal/network"
	"github.com/ropslabs/x3f/internal/releases"
)

// Level is the severity of an assessment.
type Level int

const (
	OK   Level = iota // green
	Warn              // yellow
	Crit              // red
)

func (l Level) String() string {
	switch l {
	case OK:
		return "OK"
	case Warn:
		return "WARN"
	default:
		return "CRIT"
	}
}

// MarshalText implements encoding.TextMarshaler, so a Level is stored as
// "OK", "WARN" or "CRIT" in JSON (also as a map value) instead of a number.
func (l Level) MarshalText() ([]byte, error) {
	return []byte(l.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, the reverse of
// MarshalText.
func (l *Level) UnmarshalText(b []byte) error {
	for _, c := range []Level{OK, Warn, Crit} {
		if string(b) == c.String() {
			*l = c
			return nil
		}
	}
	return fmt.Errorf("unknown level %q", b)
}

// Finding is one reason behind an assessment. A finding with level OK is a
// note: worth showing, but it does not change the status.
type Finding struct {
	Level   Level
	Message string
}

// Assessment is the result for one table cell.
type Assessment struct {
	Level    Level
	Findings []Finding
}

// add records a finding and raises the level if needed.
func (a *Assessment) add(l Level, format string, args ...any) {
	a.Findings = append(a.Findings, Finding{Level: l, Message: fmt.Sprintf(format, args...)})
	a.Level = max(a.Level, l) // max is built in since Go 1.21
}

const (
	// SyncThreshold is the largest lag (in blocks or slots) that is still
	// yellow. 32 slots are one epoch, about 6.4 minutes.
	SyncThreshold = 32
	// MaxReleaseDataAge is how old cached release data may be, while
	// GitHub is unreachable, before it turns the status yellow. Shorter
	// outages only produce a note, so monitoring does not alert on every
	// GitHub hiccup.
	MaxReleaseDataAge = 7 * 24 * time.Hour
)

// VersionInput is everything needed to assess a client version.
type VersionInput struct {
	Version ethereum.ClientVersion
	// Err is the error of the version request, if any.
	Err error
	// Releases is the GitHub data; nil if it could not be obtained.
	Releases *releases.Result
	// ReleasesErr explains why Releases is nil.
	ReleasesErr error
	Now         time.Time
	Grace       time.Duration
}

// AssessVersion applies the version rules.
func AssessVersion(in VersionInput) Assessment {
	var a Assessment
	if in.Err != nil {
		if ethereum.IsTransportError(in.Err) {
			a.add(Crit, "unreachable: %v", in.Err)
		} else {
			a.add(Warn, "version query failed: %v", in.Err)
		}
		return a
	}

	v := in.Version
	if v.Client == ethereum.Unknown || !v.HasVersion() {
		a.add(Warn, "client or version not recognized: %q", v.Raw)
		return a
	}
	running, ok := releases.ParseVersion(v.Version)
	if !ok {
		a.add(Warn, "version %q not comparable", v.Version)
		return a
	}
	running.Suffix = v.Suffix
	name := fmt.Sprintf("%s %s", v.Client, v.Version)

	kind := releases.ClassifySuffix(v.Client, v.Suffix)
	var rels []releases.Release
	if in.Releases != nil {
		rels = in.Releases.Releases
	}
	latest, haveLatest := releases.LatestStable(v.Client, rels)
	missing, outdated := releases.FirstMissing(v.Client, rels, running)
	missingVersion, _ := releases.ParseVersion(missing.Tag)

	// A pre-release or dev build whose own stable release is already out
	// (e.g. 23.1.1-dev while 23.1.1 exists) gets one combined finding
	// that says exactly that, instead of "is a dev build" plus an
	// "outdated" line that would name 23.1.1 as the newer release.
	merge := outdated && kind != releases.KindRelease && !missing.PublishedAt.IsZero() &&
		sameNumbers(missingVersion, running)
	if !merge {
		switch kind {
		case releases.KindPreRelease:
			a.add(Warn, "%s is a pre-release (%q)", name, v.Suffix)
		case releases.KindDevBuild:
			a.add(Warn, "%s is a dev build (%q)", name, v.Suffix)
		case releases.KindUnknown:
			a.add(Warn, "%s has an unrecognized suffix (%q)", name, v.Suffix)
		}
	}

	if in.Releases == nil {
		a.add(Warn, "latest release unknown: %v", in.ReleasesErr)
		return a
	}
	if err := in.Releases.RefreshErr; err != nil {
		age := in.Now.Sub(in.Releases.FetchedAt)
		if age > MaxReleaseDataAge {
			a.add(Warn, "release data is %s old, GitHub not reachable: %v", days(age), err)
		} else {
			a.add(OK, "GitHub not reachable, using release data from %s: %v",
				in.Releases.FetchedAt.Format(time.DateOnly), err)
		}
	}

	if !haveLatest {
		a.add(Warn, "no stable release found on GitHub")
		return a
	}
	if !outdated {
		// A release-type version above every known release (pre-releases
		// included) matches nothing on GitHub. Seen live: dRPC answers
		// "Geth/v10.0.0/drpc". The caller refreshes the release data
		// before assessing, so a just-published release does not end up
		// here. Pre-releases and dev builds ahead of the latest release
		// are expected and already yellow.
		if kind == releases.KindRelease && releases.NewerThanAll(v.Client, rels, running) {
			newest, _ := releases.NewestKnown(v.Client, rels)
			a.add(Warn, "%s is newer than any known release (newest %s): proxy or custom build?", name, newest)
		}
		return a
	}
	if missing.PublishedAt.IsZero() {
		// GitHub sets published_at for every published release; be safe
		// anyway instead of computing an absurd age.
		a.add(Warn, "%s outdated: latest %s; release date of %s unknown", name, latest, plainTag(missing.Tag))
		return a
	}
	behind := in.Now.Sub(missing.PublishedAt)
	level := Warn
	if behind > in.Grace {
		level = Crit
	}
	if merge {
		// e.g. "Besu dev build of 23.1.1 ("dev"): stable 23.1.1 released
		// 1357 days ago; latest 26.11.2 (grace period 30 days)"
		a.add(level, "%s %s: stable %s released %s ago; latest %s (grace period %s)",
			v.Client, describeBuild(kind, missingVersion, v.Suffix), missingVersion, days(behind), latest, days(in.Grace))
		return a
	}
	a.add(level, "%s outdated: latest %s; first newer release %s published %s ago (grace period %s)",
		name, latest, plainTag(missing.Tag), days(behind), days(in.Grace))
	return a
}

// plainTag drops the "v" of a release tag ("v1.17.10" becomes "1.17.10"),
// so that a version reads the same everywhere in the output. Tags without
// it, such as Besu's "26.11.2", stay as they are.
func plainTag(tag string) string {
	if len(tag) > 1 && (tag[0] == 'v' || tag[0] == 'V') {
		return tag[1:]
	}
	return tag
}

// sameNumbers reports whether a and b have the same MAJOR.MINOR.PATCH.
func sameNumbers(a, b releases.Version) bool {
	return a.Major == b.Major && a.Minor == b.Minor && a.Patch == b.Patch
}

// describeBuild names a non-release build, e.g. `dev build of 23.1.1 ("dev")`.
func describeBuild(kind releases.SuffixKind, v releases.Version, suffix string) string {
	switch kind {
	case releases.KindPreRelease:
		return fmt.Sprintf("pre-release of %s (%q)", v, suffix)
	case releases.KindDevBuild:
		return fmt.Sprintf("dev build of %s (%q)", v, suffix)
	default:
		return fmt.Sprintf("build of %s with unrecognized suffix (%q)", v, suffix)
	}
}

// Latest returns the latest stable version, for display. ok is false if it
// is unknown.
func Latest(in VersionInput) (releases.Version, bool) {
	if in.Releases == nil || in.Err != nil {
		return releases.Version{}, false
	}
	return releases.LatestStable(in.Version.Client, in.Releases.Releases)
}

// SyncInput describes the outcome of a sync request.
type SyncInput struct {
	// Skipped is true when the request was not sent because the client
	// was unreachable; that is already reported by the version assessment.
	Skipped bool
	Err     error
}

// AssessExecutionSync applies the sync rules to an execution client.
func AssessExecutionSync(in SyncInput, s ethereum.ExecutionSyncStatus) Assessment {
	var a Assessment
	if done := syncError(&a, in); done {
		return a
	}
	switch {
	case !s.Syncing:
	case s.Indexing():
		a.add(Warn, "blocks complete, still building indexes")
	default:
		behind, ok := s.BlocksBehind()
		switch {
		case !ok:
			a.add(Warn, "syncing, target block unknown")
		case behind > SyncThreshold:
			a.add(Crit, "syncing, %s behind", count(behind, "block"))
		default:
			a.add(Warn, "syncing, %s behind", count(behind, "block"))
		}
	}
	return a
}

// AssessConsensusSync applies the sync rules to a consensus client.
func AssessConsensusSync(in SyncInput, s ethereum.ConsensusSyncStatus) Assessment {
	var a Assessment
	if done := syncError(&a, in); done {
		return a
	}
	if s.IsSyncing {
		if s.SyncDistance > SyncThreshold {
			a.add(Crit, "syncing, %s behind", count(s.SyncDistance, "slot"))
		} else {
			a.add(Warn, "syncing, %s behind", count(s.SyncDistance, "slot"))
		}
	}
	if s.IsOptimistic != nil && *s.IsOptimistic {
		a.add(Warn, "optimistic: head not yet verified by the execution client")
	}
	if s.ELOffline != nil && *s.ELOffline {
		a.add(Crit, "execution client offline (reported by the consensus client)")
	}
	return a
}

// syncError handles skipped and failed requests; done means nothing else
// is to be assessed.
func syncError(a *Assessment, in SyncInput) (done bool) {
	switch {
	case in.Skipped:
		return true
	case in.Err == nil:
		return false
	case ethereum.IsTransportError(in.Err):
		a.add(Crit, "sync status unreachable: %v", in.Err)
	default:
		a.add(Warn, "sync status query failed: %v", in.Err)
	}
	return true
}

// days formats a duration in whole days, e.g. "12 days" or "1 day".
func days(d time.Duration) string {
	n := int(d.Hours() / 24)
	if n == 0 {
		return "less than 1 day"
	}
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

// count formats a count with a singular or plural noun: "1 slot", "2 slots".
func count(n uint64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// NetworkInput holds the chain IDs reported by both clients of a node.
type NetworkInput struct {
	// Expected is the network from the config ("" if not set).
	Expected string

	ExecutionID      uint64
	ExecutionErr     error
	ExecutionSkipped bool // client unreachable, already reported

	ConsensusID      uint64
	ConsensusErr     error
	ConsensusSkipped bool
}

// AssessNetwork checks that both clients are on the same network, and on
// the expected one if the config names it.
//
//	red:    execution and consensus client on different networks; a client
//	        on another network than configured; client unreachable
//	yellow: network query failed
//	note:   network not known to x3f (e.g. a devnet): fork checks will not
//	        be possible
func AssessNetwork(in NetworkInput) Assessment {
	var a Assessment
	el, haveEL := chainID(&a, "execution", in.ExecutionID, in.ExecutionErr, in.ExecutionSkipped)
	cl, haveCL := chainID(&a, "consensus", in.ConsensusID, in.ConsensusErr, in.ConsensusSkipped)

	if haveEL && haveCL && el != cl {
		a.add(Crit, "execution client is on %s, consensus client on %s",
			network.Describe(el), network.Describe(cl))
	}
	if in.Expected != "" {
		if haveEL && network.Describe(el) != in.Expected {
			a.add(Crit, "expected %s, execution client is on %s", in.Expected, network.Describe(el))
		}
		if haveCL && network.Describe(cl) != in.Expected {
			a.add(Crit, "expected %s, consensus client is on %s", in.Expected, network.Describe(cl))
		}
	}

	// One note per unknown chain ID.
	var unknown []uint64
	for _, id := range []struct {
		id   uint64
		have bool
	}{{el, haveEL}, {cl, haveCL}} {
		if _, known := network.ByChainID(id.id); id.have && !known && !slices.Contains(unknown, id.id) {
			unknown = append(unknown, id.id)
			a.add(OK, "unknown network (chain %d): no fork data available", id.id)
		}
	}
	return a
}

// chainID reports a failed or skipped query and returns the ID if usable.
func chainID(a *Assessment, layer string, id uint64, err error, skipped bool) (uint64, bool) {
	switch {
	case skipped:
		return 0, false
	case err == nil:
		return id, true
	case ethereum.IsTransportError(err):
		a.add(Crit, "%s network query unreachable: %v", layer, err)
	default:
		a.add(Warn, "%s network detection failed: %v", layer, err)
	}
	return 0, false
}
