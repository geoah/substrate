package substrate

import "time"

// PublishInput is a publish (POST /api/v1/packages/publish, decision record
// 0289): one of the repository's own packages, in `~`, and the authority it
// is published under, a domain whose `_substrate` record names this
// repository's authority document. Confirm runs it; without it the answer is
// what it would do.
type PublishInput struct {
	Package   string `json:"package"`
	Authority string `json:"authority"`
	Confirm   bool   `json:"confirm,omitempty"`
}

// PublishPlan is what a publish does, stated before it runs and answered
// after: the package in `~` and the name it gets, the kinds and the other
// packages of the repository's own whose declarations follow it, how many
// records it carries and how many records elsewhere have their references
// repointed, and what refuses it now. The proof half is the `_substrate`
// record to publish at Name, whether DNS says so yet (Proven), and why not.
// Release is the first release, once the publish ran.
type PublishPlan struct {
	Package     string   `json:"package"`
	Published   string   `json:"published"`
	Kinds       []string `json:"kinds"`
	Packages    []string `json:"packages"`
	Records     int64    `json:"records"`
	References  int64    `json:"references"`
	Blockers    []string `json:"blockers"`
	Name        string   `json:"name"`
	Record      string   `json:"record"`
	DocumentURL string   `json:"documentURL"`
	Proven      bool     `json:"proven"`
	Reason      string   `json:"reason,omitempty"`
	Release     *Release `json:"release,omitempty"`
}

// ReleaseInput names a package this repository publishes, for its next
// release (POST /api/v1/packages/release).
type ReleaseInput struct {
	Package string `json:"package"`
}

// Release is one version of a package this repository publishes: the
// version, the SHA-256 of the closure's bytes, the vocabulary dialect its
// declarations need where they state one, and when it was released.
type Release struct {
	Package    string    `json:"package"`
	Version    int64     `json:"version"`
	SHA256     string    `json:"sha256"`
	Dialect    int64     `json:"dialect,omitempty"`
	ReleasedAt time.Time `json:"releasedAt"`
}

// Releases is the answer of GET /api/v1/packages/releases: every release of
// the packages the repository publishes.
type Releases struct {
	Items []Release `json:"items"`
}
