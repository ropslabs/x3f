package releases

import (
	"context"
	"time"

	"github.com/ropslabs/x3f/internal/software"
)

// Advisory is one published GitHub security advisory of a client
// repository, reduced to the fields x3f uses. Source:
// GET /repos/{owner}/{repo}/security-advisories ("List repository security
// advisories" in the GitHub REST API docs).
type Advisory struct {
	GHSAID string `json:"ghsa_id"`
	// CVEID is empty if the advisory has no CVE (JSON null).
	CVEID   string `json:"cve_id"`
	HTMLURL string `json:"html_url"`
	Summary string `json:"summary"`
	// Severity is "critical", "high", "medium" or "low"; empty if unset.
	Severity    string     `json:"severity"`
	PublishedAt time.Time  `json:"published_at"`
	WithdrawnAt *time.Time `json:"withdrawn_at,omitempty"`
	// Vulnerabilities name the affected and patched versions. The package
	// name is ignored: every repository x3f reads uses its own release
	// versions there, whatever it calls the package.
	Vulnerabilities []AdvisoryVulnerability `json:"vulnerabilities"`
}

// AdvisoryVulnerability is one affected package entry of an advisory. Both
// fields are free text written by the repository maintainers; see
// ParseRange for the spellings x3f understands.
type AdvisoryVulnerability struct {
	VulnerableVersionRange string `json:"vulnerable_version_range"`
	PatchedVersions        string `json:"patched_versions"`
}

// AdvisoryResult is the advisory list of one repository; the fields mean
// the same as in Result.
type AdvisoryResult struct {
	Advisories []Advisory
	FetchedAt  time.Time
	RefreshErr error
	Cached     bool
}

// Advisories returns the published security advisories of repo, with the
// same caching and error rules as Releases. Withdrawn advisories are left
// out.
func (s *Source) Advisories(ctx context.Context, repo software.Repo) (AdvisoryResult, error) {
	r, err := getList[Advisory](ctx, s, repo, advisoriesList, false)
	return AdvisoryResult{
		Advisories: withoutWithdrawn(r.Items),
		FetchedAt:  r.FetchedAt,
		RefreshErr: r.RefreshErr,
		Cached:     r.Cached,
	}, err
}

// withoutWithdrawn drops advisories that were withdrawn. The request asks
// for state=published only; this is a second line of defense.
func withoutWithdrawn(advs []Advisory) []Advisory {
	var out []Advisory
	for _, a := range advs {
		if a.WithdrawnAt == nil {
			out = append(out, a)
		}
	}
	return out
}
