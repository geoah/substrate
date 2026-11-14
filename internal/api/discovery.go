package api

import (
	"net/http"

	"github.com/geoah/substrate/internal/build"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Discovery: GET /.well-known/substrate/server.json answers what this
// deployment serves, so a client reads the stability and the revision of each
// feature, the compatibility of each surface and what registration asks for
// instead of trying a route. It is unversioned, unauthenticated, and touches no
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
	// Vocabulary carries the declaration format this server reads, which a
	// publisher compares with the dialect a package version needs (decision
	// 0268). Added at the `discovery` feature's revision 2.
	Vocabulary vocabularyInfo `json:"vocabulary"`
	// Features is the feature list a client reads instead of feature-probing:
	// featureTable's entries (features.go), each with its label and revision.
	Features []featureInfo `json:"features"`
	// Surfaces is the compatibility verdict per request surface, keyed by the
	// same names a feature's `surfaces` carries. It is a different axis from
	// a feature's stability: stability says how ONE feature's shape may
	// change, compatibility says what a client may pin (decision 0053).
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
	// Site is the deployer's brand and landing (SUBSTRATE_SITE_DIR), omitted
	// where none is configured.
	Site *substrate.SiteInfo `json:"site,omitempty"`
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
	// NameHost is the host a short name is completed under, at a
	// registration and a rename: the host of SUBSTRATE_PUBLIC_URL, never the
	// address a request arrived at (decision record 0288). Always present,
	// and empty where none is configured, when a name is typed whole: a
	// client tells that from an older server, which sends no field and
	// completes under the request's host. A client previews the name with
	// it and sends what was typed; the door completes it.
	NameHost string `json:"nameHost"`
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
	// ActorTypes are the types of `substrate.reamde.dev/core/actor` record
	// a write may link as its actor (decision 0270). No request names one: a
	// person's token writes as the person, an app's as its app.
	ActorTypes []string `json:"actorTypes"`
}

// surfacesInfo names each request surface with its endpoint and its
// compatibility. REST is the supported developer interface; MCP is the
// assistants' door (decision 0230), a preview while its protocol revision
// and its tools settle.
type surfacesInfo struct {
	REST surfaceInfo `json:"rest"`
	MCP  surfaceInfo `json:"mcp"`
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
const (
	compatibilitySupported = "supported"
	// compatibilityPreview is a surface whose shape may still move without
	// a break being announced.
	compatibilityPreview = "preview"
)

type endpointsInfo struct {
	Register string `json:"register"`
	Login    string `json:"login"`
	Tokens   string `json:"tokens"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
	// Authorize and Token are the app authorization flow's two endpoints
	// (decision 0194); its OAuth metadata document says the rest.
	Authorize string `json:"authorize"`
	Token     string `json:"token"`
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
	// Instance is the id this server minted at its first boot. A server
	// reading a domain's `_substrate` record fetches the `server=` origin's
	// document and compares it with its own: a consistency check that the
	// record reaches this server, not authentication of it, since a proxy
	// relays the document (decision 0208). Empty on a read-only process that
	// found none.
	Instance string `json:"instance,omitempty"`
}

type changelogInfo struct {
	Horizon int64 `json:"horizon"`
}

// vocabularyInfo is the declaration format this server reads: the newest
// vocabulary dialect, which every older one is below.
type vocabularyInfo struct {
	Dialect int `json:"dialect"`
}

// getDiscovery serves GET /.well-known/substrate/server.json. No auth, no DB.
func (h *handler) getDiscovery(w http.ResponseWriter, _ *http.Request) {
	doc := discoveryDoc{
		Versions:   []apiVersionInfo{{Name: APIVersion, Status: "served"}},
		Server:     serverInfo{Version: build.Version(), Build: build.Commit(), Instance: h.svc.InstanceID()},
		Changelog:  changelogInfo{Horizon: retentionHorizon()},
		Vocabulary: vocabularyInfo{Dialect: vocabulary.Dialect},
		Features:   h.features(),
		Surfaces: surfacesInfo{
			REST: surfaceInfo{Endpoint: "/api/" + APIVersion, Compatibility: compatibilitySupported},
			MCP:  surfaceInfo{Endpoint: mcpPath, Compatibility: compatibilityPreview},
		},
		Grammar: grammarInfo{
			Kind:       "<authority>/<package>/<name>",
			Record:     "<authority>/<package>/<kind>/<id>",
			Collection: "/api/" + APIVersion + recordsRoute,
			RecordPath: "/api/" + APIVersion + "/{authority}/{package}/{kind}/{id}",
			ActorTypes: substrate.ActorTypes,
		},
		Endpoints: endpointsInfo{
			Register: "/register", Login: "/login", Tokens: "/tokens",
			Password: "/password", TOTP: "/totp",
			Authorize: "/oauth/authorize", Token: "/oauth/token",
		},
		Registration: registrationInfo{
			InviteRequired: h.inviteCode != "",
			TOTPRequired:   !h.totpDisabled,
			NameHost:       h.names.Host,
		},
	}
	if h.site != nil {
		info := h.site.Info()
		doc.Site = &info
	}
	writeJSON(w, http.StatusOK, doc)
}
