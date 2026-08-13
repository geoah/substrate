package api

import (
	"net/http"
	"runtime/debug"
	"sync"

	"github.com/geoah/substrate/internal/substrate"
)

// Discovery: GET /api answers what this deployment serves so a
// client adapts to capability presence WITHOUT probing for 501s. It is
// unversioned, unauthenticated, and touches no repository — the same class of
// endpoint as /healthz.

// discoveryDoc is the GET /api body.
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
	// Auth is what those doors ASK FOR, which a client cannot infer from the
	// paths: whether this deployment verifies a second factor at all.
	Auth authInfo `json:"auth"`
}

// authInfo is the door's shape. It states a requirement, never a verdict: what
// a caller must present, decided by configuration, with no repository opened
// and nothing about any user in it.
type authInfo struct {
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
	// Collection is the REST collection path under a version prefix: an
	// authority segment and a plural, or just the plural for a bare kind.
	Collection string `json:"collection"`
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

// getDiscovery serves GET /api. No auth, no DB.
func (h *handler) getDiscovery(w http.ResponseWriter, _ *http.Request) {
	version, build := serverBuild()
	doc := discoveryDoc{
		Versions: []apiVersionInfo{{Name: APIVersion, Status: "served"}},
		Server:   serverInfo{Version: version, Build: build},
		Vocabulary: vocabularyInfo{
			MaxDialect: h.maxDialect,
			Note:       "binary maximum; the stored dialect is per-repository, in that repository's own vocabulary_dialect row, and is not served",
		},
		Changelog: changelogInfo{Horizon: retentionHorizon()},
		Features:  features(),
		Grammar: grammarInfo{
			Kind:       "<authority>/<name> | <name>",
			Record:     "<authority>/<kind>/<id> | <kind>/<id>",
			Collection: "/api/" + APIVersion + "/{authority}/{plural}[/{id}] | /api/" + APIVersion + "/{plural}[/{id}]",
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
		Auth: authInfo{TOTPRequired: !h.totpDisabled},
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

var buildOnce struct {
	sync.Once
	version string
	build   string
}

// serverBuild reads the module version and VCS revision the binary was built
// from; both fall back to "dev" for a plain `go run`/`go test`.
func serverBuild() (version, build string) {
	buildOnce.Do(func() {
		buildOnce.version = "dev"
		buildOnce.build = ""
		info, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			buildOnce.version = info.Main.Version
		}
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				buildOnce.build = s.Value
			}
		}
	})
	return buildOnce.version, buildOnce.build
}
