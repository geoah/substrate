package api

import "github.com/geoah/substrate/internal/substrate"

// A feature is one group of routes a client may build on: a name, the label
// that says how its shape may change (substrate.Stabilities,
// docs/stability.md) and a revision. featureTable is the one list of them.
// Discovery serializes it (getDiscovery), and features_test.go holds it to
// the router both ways: every route the router serves is in exactly one
// feature, and a feature names only routes the router serves. A route added
// to the router is therefore added here, in the feature it belongs to.
//
// A revision starts at 1 and only goes up. It goes up when the feature gains
// something a client may want to check for before it uses it: a route, a
// request field, a parameter, a response field. A client then asks for "at
// least 3" instead of trying a request to see whether it is refused.
// testdata/features.golden.json records each feature's routes and revision,
// and its test refuses a route set that changed under an unchanged revision
// and a revision that went down; `mise run features:check` refuses the same
// against the base branch. Both also refuse what would take something away
// from a client unsaid: a route or a feature that left the table without
// being declared (feature.removed, removedFeatures), and a `stable` label
// that went down. A parameter or a field is seen by neither: raising the
// revision for one is held by review.

// featureInfo is one feature as discovery serves it. Stability is one of
// substrate.Stabilities, and it describes CHANGE, not quality: everything
// listed is served today.
//
// Surfaces is never empty, and each name is a key of the document's
// top-level `surfaces` object. They are about the feature's OWN operations,
// never about its records: a trigger and a blob manifest are ordinary
// records, readable through `/records` and the record path whatever this
// list says.
type featureInfo struct {
	Name      string   `json:"name"`
	Stability string   `json:"stability"`
	Revision  int      `json:"revision"`
	Surfaces  []string `json:"surfaces"`
	// Notice announces a coming break to a beta feature, or a deprecation,
	// one minor release ahead (docs/stability.md).
	Notice string `json:"notice,omitempty"`
}

// The request surfaces a feature can be served on. A client reads them off
// discovery instead of trying a route to see whether it exists.
const (
	surfaceREST = "rest"
	surfaceMCP  = "mcp"
)

// feature is one row of featureTable: what discovery serves, and the routes
// the row stands for.
type feature struct {
	name      string
	stability string
	revision  int
	surfaces  []string
	notice    string
	// routes are the routes this feature alone holds, each `METHOD pattern`
	// with the pattern as the router registers it (`*` for a route that
	// answers every method).
	routes []string
	// removed are the routes this feature once held and the server no longer
	// answers. A route that goes is declared here and never inferred from its
	// absence, as a retired name is (decision record 0055): the revision
	// guard refuses a route that left the table without being listed, so
	// taking one away is a line a reviewer reads as the break it is. The list
	// only grows, and an address on it is never served again, by this
	// feature or another.
	removed []string
	// modeOf is the route, held by another feature, that this feature is a
	// mode of: a parameter of it, with no route of its own.
	modeOf string
	// listed says whether this server announces the entry; nil is always.
	// The routes are the table's either way.
	listed func(h *handler) bool
}

func (f feature) info() featureInfo {
	return featureInfo{Name: f.name, Stability: f.stability, Revision: f.revision, Surfaces: f.surfaces, Notice: f.notice}
}

// recordsRead is the one list route, which three features are modes of.
const recordsRead = "GET /api/v1/records"

// removedFeature is a feature this server once announced and no longer
// serves: its name, and every route it held when it went or had removed
// before.
type removedFeature struct {
	name   string
	routes []string
}

// removedFeatures is every feature that went. One that goes is listed here,
// never only dropped from the table, and neither its name nor an address it
// held is given to anything else: a client that asks for it finds it absent,
// not something else in its place. The list only grows.
var removedFeatures = []removedFeature{}

// featureTable is every feature, strongest promise first. An entry's surfaces
// are the doors that exist today for the feature's own verbs.
var featureTable = []feature{
	// Revision 2: `vocabulary.dialect`, the declaration format the server
	// reads (decision 0268). Revision 3: `grammar.actorTypes`, the types of
	// actor record a write links, in place of the actor spellings (decision
	// 0270). Revision 4: `registration.nameHost`, the host a short name is
	// completed under (decision 0288).
	{name: "discovery", stability: substrate.StabilityStable, revision: 4, surfaces: []string{surfaceREST}, routes: []string{
		"GET /.well-known/substrate/server.json",
		"GET /healthz",
	}},
	// Every list, the body-addressed create and the record path. The ranked
	// and the aggregate read of the list route are features of their own
	// below, each with its own label. Revision 3: a property's manager and
	// each alternative link the actor record of their hand (decision 0270).
	// Revision 4: a definition a kind's record serves names its properties'
	// writers as a type of actor or a list of them (decision 0290).
	// Revision 5: `marks` in place of `propertyMeta`, each property's
	// `pinned`, `setBy`, `onBehalfOf`, `derivedFrom` and `setAt`, its
	// alternatives keyed by source record, and the `pin` and `release`
	// actions (decision 0284).
	// Revision 6: the repository's own kinds read `~/…`, and its full name
	// is another spelling of `~` on input (decision 0259).
	{name: "records", stability: substrate.StabilityStable, revision: 6, surfaces: []string{surfaceREST}, routes: []string{
		recordsRead,
		"POST /api/v1/records",
		"GET /api/v1/{a1}/{a2}/{a3}/{a4}",
		"PUT /api/v1/{a1}/{a2}/{a3}/{a4}",
		"PATCH /api/v1/{a1}/{a2}/{a3}/{a4}",
		"DELETE /api/v1/{a1}/{a2}/{a3}/{a4}",
		"POST /api/v1/{a1}/{a2}/{a3}/{a4}/pin",
		"POST /api/v1/{a1}/{a2}/{a3}/{a4}/release",
	}},
	// The error envelope and its closed code set (errorCodes), which every
	// route under /api and the sign-in door answers with. It holds no route
	// because it is every route's refusal. Revision 3: the reason
	// `set_aside`, a repository the server's start set aside. Revision 4: the
	// reason `repository_required` and the payload's `repositories`, a
	// sign-in that must name one (decision 0274). Revision 5: the reason
	// `not_permitted`, a refusal by a rule of the person's roles (decision
	// 0276).
	{name: "errors", stability: substrate.StabilityStable, revision: 5, surfaces: []string{surfaceREST}},
	// The changefeed: paged backward (`?before=`), resumed forward (`?from=`)
	// and tailed (`?watch=1`, here and on the records route). Revision 2: a
	// change links the actor record that wrote it, the one it acted for
	// (`onBehalfOf`) and the token behind it (`token`), a run its token
	// (`token`, `tokenName`), and `actors=` takes actor record ids (decision
	// 0270). Revision 3: a change names the repository's own kinds `~/…`
	// (decision 0259).
	{name: "changefeed", stability: substrate.StabilityStable, revision: 3, surfaces: []string{surfaceREST}, routes: []string{
		"GET /api/v1/changes",
	}},
	{name: "blobs", stability: substrate.StabilityStable, revision: 1, surfaces: []string{surfaceREST}, routes: []string{
		"PUT /api/v1/blobs",
		"PUT /api/v1/blobs/{digest}",
		"GET /api/v1/blobs/{digest}",
	}},
	// Revision 2: a webhook address names the repository by its id, which
	// the trigger's `webhookPath` gives out, or by its name.
	{name: "triggers", stability: substrate.StabilityStable, revision: 2, surfaces: []string{surfaceREST}, routes: []string{
		"GET /api/v1/substrate.reamde.dev/core/trigger/status",
		"POST /api/v1/substrate.reamde.dev/core/trigger/{id}/replay",
		"POST /api/v1/substrate.reamde.dev/core/trigger/{id}/run",
		"POST /api/v1/substrate.reamde.dev/core/trigger/{id}/wake",
		"GET /api/v1/substrate.reamde.dev/core/trigger/{id}/parked",
		"POST /api/v1/substrate.reamde.dev/core/trigger/{id}/parked/{fid}/retry",
		"DELETE /api/v1/substrate.reamde.dev/core/trigger/{id}/parked/{fid}",
		"POST /webhooks/{repository}/{trigger}",
		"POST /webhooks/{repository}/{trigger}/{key}",
	}},
	{name: "functions", stability: substrate.StabilityStable, revision: 1, surfaces: []string{surfaceREST}, routes: []string{
		"POST /api/v1/substrate.reamde.dev/core/function/{name}/call",
	}},
	// The bundle entry stands for the status and bind verbs and the catalog
	// together: a client reads one entry and gets every route behind it. The
	// lifecycle transitions are a PATCH of the bundle record, on the record
	// path.
	// Revision 3: the upgrade preview names each kind whose history it would
	// keep for less time (retentionShortens, decision 0282).
	// Revision 4: a sample added lands at `~/<package>`, and a bundle's id
	// and status say `~` (decision 0259).
	{name: "bundles", stability: substrate.StabilityStable, revision: 4, surfaces: []string{surfaceREST}, routes: []string{
		"GET /api/v1/substrate.reamde.dev/core/bundle/status",
		"GET /api/v1/substrate.reamde.dev/core/bundle/{id}/status",
		"POST /api/v1/substrate.reamde.dev/core/bundle/{id}/bind",
		"GET /api/v1/catalog",
		"GET /api/v1/catalog/{id}",
		"POST /api/v1/catalog/{id}/add",
	}, removed: []string{
		// One add route took the place of the two doors (decision record 0258).
		"POST /api/v1/catalog/{id}/import",
		"POST /api/v1/catalog/{id}/install",
	}},
	// The sign-in door: registering, signing in, the credential changes,
	// renaming and deleting the repository, and the tokens. Revision 4:
	// `GET /identity` names the roles the person holds in each repository
	// they may open (decision 0275). Revision 5: `POST /repository/rename`
	// gives the repository a new name, its former one resolving for 90 days
	// (decision 0287).
	{
		name: "auth", stability: substrate.StabilityBeta, revision: 5, surfaces: []string{surfaceREST},
		routes: []string{
			"POST /register/enroll",
			"POST /register",
			"POST /login",
			"POST /password",
			"POST /totp/enroll",
			"POST /totp",
			"POST /repository/delete",
			"POST /repository/rename",
			"POST /signout/everywhere",
			"POST /tokens",
			"GET /tokens",
			"GET /identity",
			"DELETE /tokens/{id}",
		},
	},
	// App authorization (decision 0194): an app that registered nowhere gets
	// a scoped token through /oauth/authorize and /oauth/token, the owner
	// approves it through /authorizations, and the app asks whether it is
	// still set up. The takeover is the owner's way out of a package an app
	// keeps (decision 0205). Revision 2: a package row says who keeps it
	// (`keptBy`, decision 0258). Revision 3: the approval page's app says the
	// name its changes appear as (`appearsAs`), in place of an actor spelling
	// (decision 0270). Revision 4: an app's scopes over the repository's own
	// kinds read `~/…`, and the takeover names a package `~/<package>`
	// (decision 0259).
	{name: "apps", stability: substrate.StabilityBeta, revision: 4, surfaces: []string{surfaceREST}, routes: []string{
		"GET /oauth/authorize",
		"POST /oauth/token",
		"POST /oauth/revoke",
		"GET /.well-known/oauth-authorization-server",
		"GET /.well-known/oauth-protected-resource",
		"GET /api/v1/authorizations/{request}",
		"POST /api/v1/authorizations/{request}",
		"DELETE /api/v1/authorizations/{request}",
		"POST /api/v1/authorizations/check",
		"POST /api/v1/vocabulary/takeover",
	}},
	// Revision 2: a definition's `writer:` takes a type of actor or a list of
	// them, and the three words before refuse (decision 0290). Revision 3: a
	// kind's info says whether it is a control kind (`control`, decision
	// 0276).
	// Revision 4: a new package of the repository's own is declared under
	// `~`, and the repository's full name is folded into it (decision 0259).
	{name: "vocabulary", stability: substrate.StabilityBeta, revision: 4, surfaces: []string{surfaceREST}, routes: []string{
		"POST /api/v1/vocabulary/apply",
		"POST /api/v1/vocabulary/plan",
		"GET /api/v1/vocabulary/upgrade",
		"GET /api/v1/substrate.reamde.dev/core/trait/{id}/implementors",
	}},
	// How long each kind's history is kept: what its declaration states, the
	// repository's override and the one that applies (decision 0282). Acted
	// on by nothing yet, so experimental.
	{name: "retention", stability: substrate.StabilityExperimental, revision: 1, surfaces: []string{surfaceREST}, routes: []string{
		"GET /api/v1/retention",
	}},
	{name: "merge", stability: substrate.StabilityBeta, revision: 1, surfaces: []string{surfaceREST}, routes: []string{
		"POST /api/v1/merge",
		"POST /api/v1/split",
	}},
	// The host OAuth facility that connects an account record to its
	// provider: the start an owner calls and the callback the provider
	// redirects to.
	{name: "connect", stability: substrate.StabilityBeta, revision: 1, surfaces: []string{surfaceREST}, routes: []string{
		"POST /api/v1/oauth/start",
		"GET /api/v1/oauth/callback",
	}},
	{name: "sync", stability: substrate.StabilityBeta, revision: 1, surfaces: []string{surfaceREST}, routes: []string{
		"GET /api/v1/sync/status",
	}},
	{name: "domain", stability: substrate.StabilityBeta, revision: 1, surfaces: []string{surfaceREST}, routes: []string{
		"POST /api/v1/repository/domain",
		"POST /api/v1/repository/domain/verify",
	}},
	// The recovery export streams the repository's archive as one tar
	// (decision 0278); revision 2 is that layout.
	{name: "export", stability: substrate.StabilityBeta, revision: 2, surfaces: []string{surfaceREST}, routes: []string{
		"GET /api/v1/export",
	}},
	// Search is the list route's `q=`: beta while the hit shape (the per-arm
	// scores beside the page) is young.
	{name: "search", stability: substrate.StabilityBeta, revision: 1, surfaces: []string{surfaceREST}, modeOf: recordsRead},
	// The two a deployment turns on (SUBSTRATE_METRICS, SUBSTRATE_SITE_DIR).
	// Their entries are listed everywhere: the list says what this binary
	// serves and what it promises about it, and whether a deployment has a
	// site is the document's own `site` object.
	{name: "metrics", stability: substrate.StabilityBeta, revision: 1, surfaces: []string{surfaceREST}, routes: []string{
		"GET /metrics",
	}},
	{name: "site", stability: substrate.StabilityBeta, revision: 1, surfaces: []string{surfaceREST}, routes: []string{
		"* /site/*",
		"GET /favicon.ico",
	}},
	// The aggregate read is the list route's `aggregate=` (decision 0161):
	// experimental while the observations it reduces are a first release.
	{name: "aggregate", stability: substrate.StabilityExperimental, revision: 1, surfaces: []string{surfaceREST}, modeOf: recordsRead},
	// Embeddings reach a caller as the semantic arm of the list route's
	// `q=`, and the vector width is a constant in the engine that no
	// declaration can move. The provider is a repository's own llm/provider
	// row, not a host setting an operator can omit, so the feature is served
	// wherever the substrate is; a repository that declares no row is told
	// so by its first query, naming the property.
	{name: "embeddings", stability: substrate.StabilityExperimental, revision: 1, surfaces: []string{surfaceREST}, modeOf: recordsRead},
	// The repository's unit graph and one conversion through it (decision
	// 0186).
	{name: "units", stability: substrate.StabilityExperimental, revision: 1, surfaces: []string{surfaceREST}, routes: []string{
		"GET /api/v1/units",
		"GET /api/v1/units/convert",
	}},
	// The agent call and chat routes and the two that reach a running turn.
	// Experimental with the agent kind and the llm package they run on
	// (substrate/agent.go).
	{name: "agents", stability: substrate.StabilityExperimental, revision: 2, surfaces: []string{surfaceREST}, routes: []string{
		"POST /api/v1/substrate.reamde.dev/core/agent/{name}/chat",
		"POST /api/v1/substrate.reamde.dev/core/agent/{name}/call",
		"GET /api/v1/substrate.reamde.dev/llm/thread/{id}/events",
		"POST /api/v1/substrate.reamde.dev/llm/thread/{id}/stop",
	}},
	// The MCP door (decision 0230): an assistant's tools over the REST verbs,
	// experimental while the protocol's stateless revision and the tool set
	// settle. Revision 3: describe_kind's definition names its properties'
	// writers as a type of actor or a list of them (decision 0290). Revision
	// 4: the overview names the repository's own namespace, `ownNamespace`,
	// and every own kind it lists is under it (decision 0259).
	{name: "mcp", stability: substrate.StabilityExperimental, revision: 4, surfaces: []string{surfaceMCP}, routes: []string{
		"* /mcp",
		"GET /.well-known/oauth-protected-resource/mcp",
	}},
	// Applets (docs/plans/applets.md): records of the seeded applets package
	// rendered as A2UI v1.0 screens, and the image proxy that draws the
	// images they name. Experimental: the record's format, its steps and its
	// data model grow with the first applets people keep.
	{name: "applets", stability: substrate.StabilityExperimental, revision: 1, surfaces: []string{surfaceREST, surfaceMCP}, routes: []string{
		"POST /api/v1/substrate.reamde.dev/applets/applet/{id}/render",
		"POST /api/v1/substrate.reamde.dev/applets/applet/{id}/act",
		"POST /api/v1/substrate.reamde.dev/applets/applet/{id}/preview",
		"GET /api/v1/images",
	}},
	// The read is served everywhere, and the entry is listed only where the
	// server booted with an operator defaults file: it is what tells a
	// client the operator pays for anything (decision 0235). Experimental
	// while the plan's later phases add OAuth clients and function secrets
	// to it.
	{
		name: "operatorDefaults", stability: substrate.StabilityExperimental, revision: 1, surfaces: []string{surfaceREST},
		routes: []string{"GET /api/v1/operator/defaults"},
		listed: func(h *handler) bool { return h.operatorDefaults },
	},
}

// features is the feature list this server announces: the table's entries,
// in the table's order, less the ones this deployment does not list.
func (h *handler) features() []featureInfo {
	out := make([]featureInfo, 0, len(featureTable))
	for _, f := range featureTable {
		if f.listed == nil || f.listed(h) {
			out = append(out, f.info())
		}
	}
	return out
}
