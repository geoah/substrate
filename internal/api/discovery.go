package api

import (
	"net/http"

	"github.com/geoah/substrate/internal/build"
	"github.com/geoah/substrate/internal/substrate"
)

// Discovery: GET /.well-known/substrate/server.json answers what this
// deployment serves so a client adapts to capability presence WITHOUT
// probing for 501s. It is unversioned, unauthenticated, and touches no
// repository — the same class of endpoint as /healthz — and its well-known
// path is what lets an outside system tell whether a domain is a substrate
// at all before it speaks the rest of the contract.

// discoveryDoc is the GET /.well-known/substrate/server.json body.
type discoveryDoc struct {
	// Versions are the served API prefixes: the primary v1 plus the sunset
	// alias marked deprecated with its replacement.
	Versions []apiVersionInfo `json:"versions"`
	// Server names the running build.
	Server serverInfo `json:"server"`
	// Vocabulary reports the binary's max schema dialect. The per-repository
	// STORED dialect is not on the wire anywhere: it lives in each
	// repository's own `vocabulary_dialect` row, and this endpoint opens none.
	Vocabulary vocabularyInfo `json:"vocabulary"`
	// Changelog carries the retention horizon: the oldest resumable seq
	// (0 today; ruling A4).
	Changelog changelogInfo `json:"changelog"`
	// Features is the capability list a client reads instead of feature-probing.
	Features []featureInfo `json:"features"`
	// Grammar is how this deployment spells a kind and a record reference —
	// the one thing a client must agree with the substrate about before it can
	// address anything.
	Grammar grammarInfo `json:"grammar"`
	// Endpoints are the doors that sit BESIDE the versioned API, outside every
	// version prefix. There is no repository segment
	// anywhere: the token implies the repository.
	Endpoints endpointsInfo `json:"endpoints"`
	// Registration is what the register door ASKS FOR and WHETHER it is even
	// open, which a client cannot infer from the paths alone.
	Registration registrationInfo `json:"registration"`
}

// registrationInfo is the register door's shape. It states a requirement,
// never a verdict: what a caller must present, decided by configuration,
// with no repository opened and nothing about any user in it.
type registrationInfo struct {
	// Open is false only on a deployment with no invite code configured — the
	// register endpoints answer `unsupported` either way, this just lets a
	// client say so before trying.
	Open bool `json:"open"`
	// TOTPRequired is false only on a deployment that booted with
	// SUBSTRATE_INSECURE_DISABLE_TOTP — a local one. A client reads it to
	// stop asking for a code nothing checks; it is not permission to skip
	// anything, because the service refuses on its own terms either way.
	TOTPRequired bool `json:"totpRequired"`
}

// grammarInfo states the kind grammar in the form a client can check against.
type grammarInfo struct {
	// Kind is "<authority>/<name>" for a published kind, "<name>" for a
	// repository-local one. There is no `local/` prefix.
	Kind string `json:"kind"`
	// Record is a record reference: the kind reference, then the id.
	Record string `json:"record"`
	// Collection is the REST collection path under a version prefix: the kind
	// reference, so everything after the prefix is Record above and a record's
	// URL is the value a `reference` property stores.
	Collection string `json:"collection"`
	// Verb is the segment reserved for verbs at every depth. A client reads it
	// to know that the segment can never be an id, a kind or an authority.
	Verb string `json:"verb"`
	// Actors is the closed actor domain. The first three are
	// the doors a request may name in X-Substrate-Actor; the rest are the
	// substrate's own writing hands and are refused on that header.
	Actors []string `json:"actors"`
}

type endpointsInfo struct {
	Register string `json:"register"`
	Login    string `json:"login"`
	Tokens   string `json:"tokens"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
}

type apiVersionInfo struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // "served" | "deprecated"
	ReplacedBy string `json:"replacedBy,omitempty"`
}

// serverInfo names the running build. Both fields come from internal/build,
// which is where a release stamps them; neither is configurable, and a
// deployment that reports "dev" was not cut from one.
type serverInfo struct {
	Version string `json:"version"`
	Build   string `json:"build,omitempty"`
}

type vocabularyInfo struct {
	MaxDialect int    `json:"maxDialect"`
	Note       string `json:"note"`
}

type changelogInfo struct {
	Horizon int64 `json:"horizon"`
}

type featureInfo struct {
	Name      string `json:"name"`
	Stability string `json:"stability"`
}

// getDiscovery serves GET /.well-known/substrate/server.json. No auth, no DB.
func (h *handler) getDiscovery(w http.ResponseWriter, _ *http.Request) {
	doc := discoveryDoc{
		Versions: []apiVersionInfo{{Name: APIVersion, Status: "served"}},
		Server:   serverInfo{Version: build.Version(), Build: build.Commit()},
		Vocabulary: vocabularyInfo{
			MaxDialect: h.maxDialect,
			Note:       "binary maximum; the stored dialect is per-repository, in that repository's own vocabulary_dialect row, and is not served",
		},
		Changelog: changelogInfo{Horizon: retentionHorizon()},
		Features:  features(),
		Grammar: grammarInfo{
			Kind:       "<authority>/<name> | <name>",
			Record:     "<authority>/<kind>/<id> | <kind>/<id>",
			Collection: "/api/" + APIVersion + "/{authority}/{kind}[/{id}] | /api/" + APIVersion + "/{kind}[/{id}]",
			Verb:       verbSegment,
			Actors: []string{
				string(substrate.ActorAPI), string(substrate.ActorConsole), string(substrate.ActorCLI),
				substrate.ConnectorActorPrefix + "<name>", substrate.FunctionActorPrefix + "<name>",
				substrate.BundleActorPrefix + "<name>", string(substrate.ActorSystem),
			},
		},
		Endpoints: endpointsInfo{
			Register: "/register", Login: "/login", Tokens: "/tokens",
			Password: "/password", TOTP: "/totp",
		},
		Registration: registrationInfo{
			Open:         h.inviteCode != "",
			TOTPRequired: !h.totpDisabled,
		},
	}
	writeJSON(w, http.StatusOK, doc)
}

// features is the deployment's capability list. Everything the frozen core
// serves is stable; the agent surface carries its declared stability
// straight from the substrate marker, so "alpha" surfaces here
// rather than being hard-coded.
func features() []featureInfo {
	return []featureInfo{
		{Name: "triggers", Stability: substrate.StabilityStable},
		{Name: "functions", Stability: substrate.StabilityStable},
		{Name: "bundles", Stability: substrate.StabilityStable},
		{Name: "blobs", Stability: substrate.StabilityStable},
		{Name: "changefeed", Stability: substrate.StabilityStable},
		{Name: "search", Stability: substrate.StabilityStable},
		{Name: "embeddings", Stability: substrate.StabilityStable},
		{Name: substrate.FeatureAgents, Stability: substrate.AgentStability},
	}
}
