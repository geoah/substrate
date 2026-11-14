package substrate

import (
	"context"
	"time"
)

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
// Texts are the declarations, as record paths, whose author's text (an
// agent's prompt) names the package in `~` and is left as written. Release
// is the first release, once the publish ran.
type PublishPlan struct {
	Package     string   `json:"package"`
	Published   string   `json:"published"`
	Kinds       []string `json:"kinds"`
	Packages    []string `json:"packages"`
	Records     int64    `json:"records"`
	References  int64    `json:"references"`
	Blockers    []string `json:"blockers"`
	Texts       []string `json:"texts"`
	Name        string   `json:"name"`
	Record      string   `json:"record"`
	DocumentURL string   `json:"documentURL"`
	Proven      bool     `json:"proven"`
	Reason      string   `json:"reason,omitempty"`
	Release     *Release `json:"release,omitempty"`
}

// ReleaseInput names a package this repository publishes, for its next
// release (POST /api/v1/packages/release). Confirm writes it; without it the
// answer is what it would write.
type ReleaseInput struct {
	Package string `json:"package"`
	Confirm bool   `json:"confirm,omitempty"`
}

// ReleasePlan is a package's next release, stated before it is written and
// answered after: the version it takes and the one before it, the SHA-256
// of the closure it serves, the declarations the closure holds, as record
// paths, and what refuses it now. Release is the release, once written.
type ReleasePlan struct {
	Package      string   `json:"package"`
	Version      int64    `json:"version"`
	Previous     int64    `json:"previous"`
	SHA256       string   `json:"sha256"`
	Declarations []string `json:"declarations"`
	Blockers     []string `json:"blockers"`
	Release      *Release `json:"release,omitempty"`
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

type publishProofKey struct{}

// WithPublishProof carries a door's check of an authority's proof (its
// `_substrate` record, decision record 0291), which a publish or a release
// runs once its input is admitted and before it writes anything: a refusal
// of what was asked comes before a refusal of the proof, and the door runs
// the publish once.
func WithPublishProof(ctx context.Context, prove func(authority string) error) context.Context {
	return context.WithValue(ctx, publishProofKey{}, prove)
}

// PublishProofFrom is the proof check WithPublishProof carries, nil for
// none.
func PublishProofFrom(ctx context.Context) func(authority string) error {
	prove, _ := ctx.Value(publishProofKey{}).(func(string) error)
	return prove
}

type releaseResolverKey struct{}

// ReleaseResolver resolves a package another publisher publishes, or the
// server ships, to the declarations an installing server receives for it at
// a version at or above floor (the catalog's resolver).
type ReleaseResolver func(ctx context.Context, id string, floor int64) ([]map[string]any, error)

// WithReleaseResolver carries a door's ReleaseResolver, which a release's
// admission reads the packages it requires through (decision record 0291).
func WithReleaseResolver(ctx context.Context, resolve ReleaseResolver) context.Context {
	return context.WithValue(ctx, releaseResolverKey{}, resolve)
}

// ReleaseResolverFrom is the resolver WithReleaseResolver carries, nil for
// none.
func ReleaseResolverFrom(ctx context.Context) ReleaseResolver {
	resolve, _ := ctx.Value(releaseResolverKey{}).(ReleaseResolver)
	return resolve
}
