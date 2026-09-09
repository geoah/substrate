package api

import (
	"net/http"

	"github.com/geoah/substrate/internal/build"
	"github.com/geoah/substrate/internal/substrate"
)

// Discovery: GET /.well-known/substrate/server.json answers what this
// deployment serves, so a client reads the stability of each feature, the
// compatibility of each surface and what registration asks for instead of
// trying a route. It is unversioned, unauthenticated, and touches no
// repository — the same class of endpoint as /healthz — and its well-known
// path is what lets an outside system tell whether a domain is a substrate
// at all before it speaks the rest of the contract.

// discoveryDoc is the GET /.well-known/substrate/server.json body.
type discoveryDoc struct {
	// Versions are the served API prefixes, each with the status it is served
	// under.
	Versions []apiVersionInfo `json:"versions"`
	// Server names the running build.
	Server serverInfo `json:"server"`
	// Changelog carries the retention horizon: the oldest resumable seq, 0
	// while nothing is pruned.
	Changelog changelogInfo `json:"changelog"`
	// Features is the feature list a client reads instead of feature-probing.
	Features []featureInfo `json:"features"`
	// Surfaces is the compatibility verdict per request surface, keyed by the
	// same names a feature's `surfaces` carries. It is a different axis from
	// a feature's stability: stability says how far ONE feature's shape has
	// settled, compatibility says which surface a client builds on at all
	// (decision 0053).
	Surfaces surfacesInfo `json:"surfaces"`
	// Grammar is how this deployment spells a kind and a record reference —
	// the one thing a client must agree with the substrate about before it can
	// address anything.
	Grammar grammarInfo `json:"grammar"`
	// Endpoints are the doors that sit BESIDE the versioned API, outside every
	// version prefix. There is no repository segment
	// anywhere: the token implies the repository.
	Endpoints endpointsInfo `json:"endpoints"`
	// Registration is what the register door ASKS FOR, which a client cannot
	// infer from the paths alone.
	Registration registrationInfo `json:"registration"`
}

// registrationInfo is the register door's shape. It states a requirement,
// never a verdict: what a caller must present, decided by configuration,
// with no repository opened and nothing about any user in it. The door is
// always open; what varies is what it asks for.
type registrationInfo struct {
	// InviteRequired is false only on a deployment with no
	// SUBSTRATE_INVITE_CODE — a local one. A client reads it to stop asking
	// for a code nothing reads; the door admits without one either way.
	InviteRequired bool `json:"inviteRequired"`
	// TOTPRequired is false only on a deployment that booted with
	// SUBSTRATE_INSECURE_DISABLE_TOTP — a local one. A client reads it to
	// stop asking for a code nothing checks; it is not permission to skip
	// anything, because the service refuses on its own terms either way.
	TOTPRequired bool `json:"totpRequired"`
}

// grammarInfo states the kind grammar in the form a client can check against.
type grammarInfo struct {
	// Kind is "<authority>/<package>/<name>": every kind carries an authority
	// and a package (decisions 0042, 0047).
	Kind string `json:"kind"`
	// Record is a record reference: the kind reference, then the id.
	Record string `json:"record"`
	// Collection is the REST collection path under a version prefix: the kind
	// reference's three segments, with the id making a record's four.
	Collection string `json:"collection"`
	// Actors is the closed actor domain. The first three are
	// the doors a request may name in X-Substrate-Actor; the rest are the
	// substrate's own writing hands and are refused on that header.
	Actors []string `json:"actors"`
}

// surfacesInfo names the two request surfaces with their endpoint and their
// compatibility. REST is the supported developer interface; every part of
// GraphQL, the generated types and the root operations and scalars alike, is
// a preview that may change without a v1 wire break (decision 0053). Both are
// served today: the verdict is about what a client may pin, never about
// whether the door is open.
type surfacesInfo struct {
	REST    surfaceInfo `json:"rest"`
	GraphQL surfaceInfo `json:"graphql"`
}

type surfaceInfo struct {
	// Endpoint is the path the surface is served under, so a preview surface
	// is locatable from discovery alone.
	Endpoint string `json:"endpoint"`
	// Compatibility is compatibilitySupported or compatibilityPreview.
	Compatibility string `json:"compatibility"`
}

// The compatibility values a surface carries. They are NOT stability values:
// a feature's stability is stamped per feature and answers a different
// question, so "preview" is never a fourth entry in substrate/stability.go.
const (
	compatibilitySupported = "supported"
	compatibilityPreview   = "preview"
)

// graphqlRoute is the GraphQL door under the version prefix. The router mounts
// it and discovery advertises it from this one spelling, so the two cannot
// drift.
const graphqlRoute = "/graphql"

type endpointsInfo struct {
	Register string `json:"register"`
	Login    string `json:"login"`
	Tokens   string `json:"tokens"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
}

type apiVersionInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "served" | "deprecated"
}

// serverInfo names the running build. Both fields come from internal/build,
// which is where a release stamps them; neither is configurable, and a
// deployment that reports "dev" was not cut from one.
type serverInfo struct {
	Version string `json:"version"`
	Build   string `json:"build,omitempty"`
}

type changelogInfo struct {
	Horizon int64 `json:"horizon"`
}

// featureInfo names one feature, how far its shape has settled, and WHICH
// surfaces serve it. Stability is one of substrate.StabilityAlpha,
// StabilityBeta or StabilityStable, and it describes CHANGE, not quality:
// everything listed is served today.
//
// The two surfaces are not equivalent: REST is the supported interface, GraphQL
// is a preview projection over the same records (decision 0053), so a feature
// only one of them serves has to say so here. Surfaces is never empty, and each
// name is a key of the document's top-level `surfaces` object.
//
// Surfaces are about the feature's OWN operations, never about its records: a
// trigger and a blob manifest are ordinary records, readable through
// `records`/`record` on both surfaces whatever this list says. What
// `["rest"]` means is that the feature's verbs (a replay, an install, a
// function call, a blob's bytes) have REST paths and no GraphQL field.
type featureInfo struct {
	Name      string   `json:"name"`
	Stability string   `json:"stability"`
	Surfaces  []string `json:"surfaces"`
}

// The request surfaces a feature can be served on. A client reads them off
// discovery instead of trying a route to see whether it exists.
const (
	surfaceREST    = "rest"
	surfaceGraphQL = "graphql"
)

// features is what this deployment serves, written out. It is a literal
// because substrate.Dataset is one interface with one implementation: there
// is nothing to ask, and every entry below is served wherever this binary
// runs.
//
// Each stability says how far the feature's shape has settled, it binds on
// the REST door alone, and `stable` means frozen for v1: additive only, a
// break announced (see substrate.StabilityStable). The GraphQL door is
// `preview` for every feature whatever the feature stamps (the `surfaces`
// object, decision 0053), so a stable `changefeed` does not make the
// `changelog` field stable, and `search`, served on GraphQL alone, stays
// beta. `agents` stays alpha on `rest` and `embeddings` alpha on `graphql`,
// because both shapes are still moving.
//
// Each entry's surfaces are the doors that actually exist today, and they are
// about the feature's OWN verbs, never its records: a trigger and a blob
// manifest are ordinary records, readable through `records`/`record` on both
// surfaces whatever this list says. Search and embeddings are the two REST
// does not serve: REST filters (`?filter=`) and GraphQL's
// `search(q, mode, kinds, k)` ranks, and the semantic arm of that same query
// is the only door to a vector. The changefeed is read on both; everything
// else is a set of REST verbs with no GraphQL field.
//
// A feature added here is a route added beside it, and a route removed takes
// its entry: nothing computes the list, so the two are held together by
// review and by the roster in discovery_errors_watch_test.go.
var features = []featureInfo{
	{Name: "triggers", Stability: substrate.StabilityStable, Surfaces: []string{surfaceREST}},
	{Name: "functions", Stability: substrate.StabilityStable, Surfaces: []string{surfaceREST}},
	// The bundle entry stands for the lifecycle verbs and catalog install
	// together: a client reads one entry and gets every route behind it.
	{Name: "bundles", Stability: substrate.StabilityStable, Surfaces: []string{surfaceREST}},
	{Name: "blobs", Stability: substrate.StabilityStable, Surfaces: []string{surfaceREST}},
	// The recovery export is one REST verb, `GET /export`, streaming a tar in
	// the snapshot format (decision 0069). Beta: the archive's layout is the
	// repository directory's and its snapshot.json is the operator's, both
	// settled, but this is the surface's first release.
	{Name: "export", Stability: substrate.StabilityBeta, Surfaces: []string{surfaceREST}},
	// The changefeed is the one feature both surfaces read: REST pages it
	// (`GET …/changes?before=`), resumes it forward (`?from=`) and tails it
	// (`?watch=1`), GraphQL resumes it forward (`changelog(from, filter,
	// first)`) but streams nothing, because there is no subscription. The
	// stamp freezes the REST routes; the GraphQL field stays a preview with
	// the rest of its surface.
	{Name: "changefeed", Stability: substrate.StabilityStable, Surfaces: []string{surfaceREST, surfaceGraphQL}},
	// Search's only door is the GraphQL schema, a preview generated per
	// repository from that repository's kinds (docs/api.md), so the feature
	// stays beta.
	{Name: "search", Stability: substrate.StabilityBeta, Surfaces: []string{surfaceGraphQL}},
	// Embeddings are alpha and GraphQL is their only door: they reach a caller
	// as the semantic arm of that same query, and the vector width is a
	// constant in the engine (vectorDim) that no declaration can move. The
	// provider is a repository's own llmprovider row, not a host setting an
	// operator can omit, so the feature is served wherever the substrate is; a
	// repository that declares no row is told so by its first query, naming
	// the property.
	{Name: "embeddings", Stability: substrate.StabilityAlpha, Surfaces: []string{surfaceGraphQL}},
	{Name: substrate.FeatureAgents, Stability: substrate.AgentStability, Surfaces: []string{surfaceREST}},
}

// getDiscovery serves GET /.well-known/substrate/server.json. No auth, no DB.
func (h *handler) getDiscovery(w http.ResponseWriter, _ *http.Request) {
	doc := discoveryDoc{
		Versions:  []apiVersionInfo{{Name: APIVersion, Status: "served"}},
		Server:    serverInfo{Version: build.Version(), Build: build.Commit()},
		Changelog: changelogInfo{Horizon: retentionHorizon()},
		Features:  features,
		Surfaces: surfacesInfo{
			REST:    surfaceInfo{Endpoint: "/api/" + APIVersion, Compatibility: compatibilitySupported},
			GraphQL: surfaceInfo{Endpoint: "/api/" + APIVersion + graphqlRoute, Compatibility: compatibilityPreview},
		},
		Grammar: grammarInfo{
			Kind:       "<authority>/<package>/<name>",
			Record:     "<authority>/<package>/<kind>/<id>",
			Collection: "/api/" + APIVersion + "/{authority}/{package}/{kind}[/{id}]",
			Actors: []string{
				string(substrate.ActorAPI), string(substrate.ActorConsole), string(substrate.ActorCLI),
				substrate.BundleActorPrefix + "<authority>:<package>",
				substrate.FunctionActorPrefix + "<authority>:<package>:<name>",
				substrate.AgentActorPrefix + "<authority>:<package>:<name>",
				string(substrate.ActorSystem),
			},
		},
		Endpoints: endpointsInfo{
			Register: "/register", Login: "/login", Tokens: "/tokens",
			Password: "/password", TOTP: "/totp",
		},
		Registration: registrationInfo{
			InviteRequired: h.inviteCode != "",
			TOTPRequired:   !h.totpDisabled,
		},
	}
	writeJSON(w, http.StatusOK, doc)
}
