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
	// settled, compatibility says what a client may pin (decision 0053).
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
	// Collection is the one list route under the version prefix: every kind's
	// records, narrowed by `filter`, ranked by `q`, tailed by `watch=1`.
	Collection string `json:"collection"`
	// RecordPath is a record's URL under the version prefix: its reference.
	RecordPath string `json:"recordPath"`
	// Actors is the closed actor domain. The first three are
	// the doors a request may name in X-Substrate-Actor; the rest are the
	// substrate's own writing hands and are refused on that header.
	Actors []string `json:"actors"`
}

// surfacesInfo names the request surface with its endpoint and its
// compatibility. REST is the one surface and the supported developer
// interface; the object stays keyed so a second surface, should one come,
// lands as a new key beside it rather than a reshaped document.
type surfacesInfo struct {
	REST surfaceInfo `json:"rest"`
}

type surfaceInfo struct {
	// Endpoint is the path the surface is served under, so a preview surface
	// is locatable from discovery alone.
	Endpoint string `json:"endpoint"`
	// Compatibility is compatibilitySupported.
	Compatibility string `json:"compatibility"`
}

// The compatibility value a surface carries. It is NOT a stability value: a
// feature's stability is stamped per feature and answers a different
// question.
const compatibilitySupported = "supported"

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
// Surfaces is never empty, and each name is a key of the document's
// top-level `surfaces` object. They are about the feature's OWN operations,
// never about its records: a trigger and a blob manifest are ordinary
// records, readable through `/records` and the record path whatever this
// list says.
type featureInfo struct {
	Name      string   `json:"name"`
	Stability string   `json:"stability"`
	Surfaces  []string `json:"surfaces"`
}

// The request surfaces a feature can be served on. A client reads them off
// discovery instead of trying a route to see whether it exists.
const surfaceREST = "rest"

// features is what this deployment serves, written out. It is a literal
// because substrate.Dataset is one interface with one implementation: there
// is nothing to ask, and every entry below is served wherever this binary
// runs.
//
// Each stability says how far the feature's shape has settled, and `stable`
// means frozen for v1: additive only, a break announced (see
// substrate.StabilityStable). `agents` and `embeddings` stay alpha because
// both shapes are still moving, and `search` beta while the ranked page is
// young.
//
// Each entry's surfaces are the doors that actually exist today, and they are
// about the feature's OWN verbs, never its records. Search is
// `GET /records?q=`, embeddings reach a caller as its semantic arm, the
// changefeed is `/changes` and the `watch=1` tail of `/records`, and
// everything else is a set of REST verbs.
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
	// The changefeed: paged backward (`GET …/changes?before=`), resumed
	// forward (`?from=`) and tailed (`?watch=1`, on /changes and on /records).
	{Name: "changefeed", Stability: substrate.StabilityStable, Surfaces: []string{surfaceREST}},
	// Search is `GET /records?q=`: served, and beta while the hit shape (the
	// per-arm scores beside the page) is young.
	{Name: "search", Stability: substrate.StabilityBeta, Surfaces: []string{surfaceREST}},
	// Embeddings are alpha: they reach a caller as the semantic arm of that
	// same ranked read, and the vector width is a constant in the engine
	// (vectorDim) that no declaration can move. The provider is a
	// repository's own llm/provider row, not a host setting an operator can
	// omit, so the feature is served wherever the substrate is; a repository
	// that declares no row is told so by its first query, naming the property.
	{Name: "embeddings", Stability: substrate.StabilityAlpha, Surfaces: []string{surfaceREST}},
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
			REST: surfaceInfo{Endpoint: "/api/" + APIVersion, Compatibility: compatibilitySupported},
		},
		Grammar: grammarInfo{
			Kind:       "<authority>/<package>/<name>",
			Record:     "<authority>/<package>/<kind>/<id>",
			Collection: "/api/" + APIVersion + recordsRoute,
			RecordPath: "/api/" + APIVersion + "/{authority}/{package}/{kind}/{id}",
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
