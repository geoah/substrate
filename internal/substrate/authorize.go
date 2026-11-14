package substrate

import "time"

// The approval page's wire shapes (decision 0194). An app sends the owner to
// GET /oauth/authorize; the server parks the request and the console's
// /authorize/{request} page reads it with GET /api/v1/authorizations/{id},
// then approves with POST or denies with DELETE on the same path. The OAuth
// endpoints themselves (/oauth/token, /oauth/revoke, the two metadata
// documents) answer the shapes their RFCs fix, which are not pinned here.

// AuthorizationRequest is one parked request as the approval page shows it:
// the app as its client id URL and its own document describe it, where the
// code goes, the repository the token would open, the scopes it asks for and
// the lifetimes the owner may pick.
type AuthorizationRequest struct {
	ID         string                  `json:"id"`
	App        AuthorizationApp        `json:"app"`
	Redirect   AuthorizationRedirectTo `json:"redirect"`
	Repository string                  `json:"repository"`
	Scopes     []AuthorizationScope    `json:"scopes"`
	// LifetimeDays are the lifetimes the owner may pick, in days, and
	// DefaultLifetimeDays the one the page selects first. 0 is "until the
	// owner revokes it": a token with no expiry.
	LifetimeDays        []int `json:"lifetimeDays"`
	DefaultLifetimeDays int   `json:"defaultLifetimeDays"`
	// ExpiresAt is when the parked request lapses unanswered.
	ExpiresAt time.Time `json:"expiresAt"`

	// Details are the app's `authorization_details` as it wrote them
	// (RFC 9396), for the technical view to show verbatim; empty when it
	// asked for scopes alone.
	Details []AuthorizationDetail `json:"details"`
	// Setup is what approving would set up, one row per detail in the
	// request's order: planned at every read until an approval is checked
	// against them, then those rows, each carrying its Progress, while the
	// set up runs, after it stopped and once it finished.
	Setup []AuthorizationSetup `json:"setup"`
	// PlanHash digests Setup's rows. An approval of a request with setup
	// names it, and the server refuses when its own plan has moved since:
	// what runs is what the page showed. Empty when there is no setup.
	PlanHash string `json:"planHash,omitempty"`
	// Previous is the newest token this repository holds for the same
	// client id, so the page can say which scopes are new.
	Previous *AuthorizationPrevious `json:"previous,omitempty"`
	// State is `pending`, `setting-up` or `approved`.
	State string `json:"state"`
	// Failure is why the last set up stopped; the request is pending again.
	Failure *AuthorizationFailure `json:"failure,omitempty"`
	// Next is where the page goes once a set up finished and the code was
	// issued: the app's redirect URI carrying it. An approved request is
	// answered once, as it stood when the set up finished, with nothing read
	// again, and is gone after.
	Next string `json:"next,omitempty"`
}

// The states a parked request moves through.
const (
	AuthorizationPending   = "pending"
	AuthorizationSettingUp = "setting-up"
	AuthorizationApproved  = "approved"
)

// The detail types an authorization request may carry (RFC 9396), what the
// authorization server metadata lists as
// `authorization_details_types_supported`.
const (
	DetailPackage = "package"
	DetailAccount = "account"
	DetailWebhook = "webhook"
)

// The actions a setup row may need: what approving would do to it. SetupNone
// is in place already; SetupUnknown answers a check for a row the token
// cannot see, which an app treats as nothing to ask about. A row that cannot
// run keeps its action and carries Blocked.
const (
	SetupInstall = "install"
	SetupUpgrade = "upgrade"
	SetupCreate  = "create"
	SetupEnable  = "enable"
	SetupNone    = "none"
	SetupUnknown = "unknown"
	// SetupWait is an approved add-on whose provider is not installed: it
	// installs when the owner installs that provider (decision 0216).
	SetupWait = "wait"
)

// The reasons a setup row cannot run, which a client words and acts on
// without reading the server's sentence.
const (
	// BlockServerUpdate: this server's catalog ships the package below the
	// version the app needs; only a newer server can.
	BlockServerUpdate = "server-update"
	// BlockNotShipped: this server's catalog does not ship the package.
	BlockNotShipped = "not-shipped"
	// BlockSample: the package is a sample, which this door does not take;
	// the console's own door imports it with its requirements.
	BlockSample = "sample"
	// BlockQuarantined: the repository's copy of the package is quarantined.
	BlockQuarantined = "quarantined"
	// BlockDeclaredByOwner: the owner declared the package by hand below the
	// version needed; installing the shipped one would take it from them.
	BlockDeclaredByOwner = "declared-by-owner"
	// BlockUpgradeNeedsReview: the upgrade changes or drops what the
	// repository holds, which the provider page asks the owner to confirm.
	BlockUpgradeNeedsReview = "upgrade-needs-review"
	// BlockNotRequested: an account or a webhook of a package the request
	// does not name.
	BlockNotRequested = "not-requested"
	// BlockAccountDeleted: the account was deleted and its deletion is not
	// collected yet, so it cannot be created again.
	BlockAccountDeleted = "account-deleted"
	// BlockNotPublished: no authority document lists the package at the
	// version the app needs, or the package's authority publishes no
	// document this server can read (decision 0212).
	BlockNotPublished = "not-published"
	// BlockReachesPastScopes: the package's code, or a requirement's,
	// reads or writes a kind the scopes left checked do not cover, so
	// approving would install code reaching past the approval (decision
	// 0215). The problem names each uncovered reach and the scope that
	// would cover it.
	BlockReachesPastScopes = "reaches-past-scopes"
	// BlockUnsupported: what the row names is not something this door sets
	// up: a kind that is no account, a property it may not set, a trigger
	// that takes no webhook, or one its provider signs.
	BlockUnsupported = "unsupported"
)

// The progress of one setup row while a set up runs, and after one stopped.
const (
	ProgressWaiting = "waiting"
	ProgressRunning = "running"
	ProgressDone    = "done"
	ProgressFailed  = "failed"
)

// AuthorizationDetail is one entry of an app's `authorization_details`: a
// package it needs at a version or later, an account it delivers under, or
// a webhook it delivers to. Which fields a type carries is fixed by the
// type; the token response answers the same shape, with Version the
// version now held and URL where a webhook takes deliveries.
type AuthorizationDetail struct {
	Type string `json:"type"`
	// Package and Version: a `package` detail.
	Package string `json:"package,omitempty"`
	Version int64  `json:"version,omitempty"`
	// Kind, ID and Properties: an `account` detail.
	Kind       string         `json:"kind,omitempty"`
	ID         string         `json:"id,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	// Trigger: a `webhook` detail; URL only in the token response.
	Trigger string `json:"trigger,omitempty"`
	URL     string `json:"url,omitempty"`
}

// AuthorizationSetup is one detail as approving would set it up: the action
// it needs and the facts behind it. Exactly one of Package, Account and
// Webhook is present, by Type.
type AuthorizationSetup struct {
	Type   string `json:"type"`
	Action string `json:"action"`
	// Blocked is present when the row cannot run, and then approving is
	// refused.
	Blocked *AuthorizationBlock `json:"blocked,omitempty"`
	// Progress is the row's ProgressWaiting, ProgressRunning, ProgressDone
	// or ProgressFailed while a set up runs and after one stopped; absent
	// otherwise.
	Progress string                    `json:"progress,omitempty"`
	Package  *AuthorizationPackage     `json:"package,omitempty"`
	Account  *AuthorizationAccount     `json:"account,omitempty"`
	Webhook  *AuthorizationWebhookPlan `json:"webhook,omitempty"`
}

// AuthorizationBlock is why a setup row cannot run: a Block* reason a client
// branches on, the server's one sentence for it in the owner's words, and
// Fix, a console path where the owner can resolve it, absent when only a
// newer server or the app can.
type AuthorizationBlock struct {
	Reason  string `json:"reason"`
	Problem string `json:"problem"`
	Fix     string `json:"fix,omitempty"`
}

// AuthorizationPackage is a package row's facts. Held is the catalog release
// the repository holds (its shippedVersion when published, else its stored
// version), absent when it holds none; Shipped is the version approving
// would install: what this server's catalog ships, or, for a package an
// authority document publishes, the newest version the document lists at
// the app's floor or later, absent when neither offers one. A check answers
// ID, Required, Held and Shipped alone, and ID and Required alone for a row
// it cannot see. A client names a package from its ID; the catalog carries
// no other name.
type AuthorizationPackage struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	KeptBy      string `json:"keptBy,omitempty"`
	Required    int64  `json:"required"`
	Held        int64  `json:"held,omitempty"`
	Shipped     int64  `json:"shipped,omitempty"`
	// AddonFor is the provider an optional add-on package extends (decision
	// 0216), from its publisher's document; the owner may leave it out.
	AddonFor string `json:"addonFor,omitempty"`
	// Settings are the empty setting and secret records the package ships,
	// which the owner fills on the page (decision 0218).
	Settings []AuthorizationSetting `json:"settings,omitempty"`
	// OAuthHosts are the hosts an outside package's oauth2 block names
	// (decision 0220).
	OAuthHosts []string `json:"oauthHosts,omitempty"`
	// Requires is the requirement chain an install brings first, each with
	// its own action and its own facts, in the order they land. Kinds,
	// Functions and Triggers are this package's own.
	Requires  []AuthorizationPackageStep `json:"requires,omitempty"`
	Kinds     []AuthorizationKind        `json:"kinds,omitempty"`
	Functions []AuthorizationFunction    `json:"functions,omitempty"`
	Triggers  []AuthorizationTrigger     `json:"triggers,omitempty"`
	// Reach is every kind this package's own code reads or writes, held to
	// the scopes left checked (decision 0215).
	Reach []AuthorizationReach `json:"reach,omitempty"`
	// Publisher is present for a package an authority document publishes
	// rather than this server's catalog ships (decision 0212).
	Publisher *AuthorizationPackagePublisher `json:"publisher,omitempty"`

	// ProviderInputs are the provider rows the owner chooses for this package and its requirements.
	ProviderInputs []AuthorizationProviderInput `json:"providerInputs,omitempty"`
}

// AuthorizationPackagePublisher is where a document-published package comes
// from: its publisher's authority, the document's own name for it, the
// document the package is listed in, and whether a file this server was
// configured to read (SUBSTRATE_INSECURE_AUTHORITY_DOCS) stood in for the
// authority's DNS record and document.
type AuthorizationPackagePublisher struct {
	Authority   string `json:"authority"`
	Name        string `json:"name,omitempty"`
	DocumentURL string `json:"documentUrl"`
	FromFile    bool   `json:"fromFile,omitempty"`
	// Digest is the SHA-256 of the closure the row would install, which
	// binds the approval to that closure (appsetup.Moved).
	Digest string `json:"digest,omitempty"`
	// MovedTo is the authority the publisher's document says it moved to
	// (decision record 0293): shown, never followed.
	MovedTo string `json:"movedTo,omitempty"`
}

// AuthorizationPackageStep is one package a requested one requires, with
// what it brings of its own. Blocked is present when it cannot be installed
// or upgraded, which blocks the row that requires it. Publisher is present
// for a requirement an authority document publishes, as on the row.
type AuthorizationPackageStep struct {
	ID          string                         `json:"id"`
	Description string                         `json:"description,omitempty"`
	Action      string                         `json:"action"`
	Blocked     *AuthorizationBlock            `json:"blocked,omitempty"`
	Held        int64                          `json:"held,omitempty"`
	Shipped     int64                          `json:"shipped,omitempty"`
	Kinds       []AuthorizationKind            `json:"kinds,omitempty"`
	Functions   []AuthorizationFunction        `json:"functions,omitempty"`
	Triggers    []AuthorizationTrigger         `json:"triggers,omitempty"`
	Publisher   *AuthorizationPackagePublisher `json:"publisher,omitempty"`
	// OAuthHosts are the hosts an outside requirement's oauth2 block names
	// (decision 0220).
	OAuthHosts []string             `json:"oauthHosts,omitempty"`
	Reach      []AuthorizationReach `json:"reach,omitempty"`
}

// AuthorizationFunction is code a package ships: what runs on the owner's
// server once it is installed, the kinds it may read and write, and the
// hosts its `permissions.network` declares, none for a body the sandbox
// denies every socket.
type AuthorizationFunction struct {
	ID      string   `json:"id"`
	Reads   []string `json:"reads,omitempty"`
	Writes  []string `json:"writes,omitempty"`
	Network []string `json:"network,omitempty"`
}

// AuthorizationReach is one kind, or one glob over kinds, a package's code
// reads or writes once installed (decision 0215): what reaches it, whether
// the scopes left checked cover it, and the scope that would.
type AuthorizationReach struct {
	Pattern string `json:"pattern"`
	Write   bool   `json:"write,omitempty"`
	// By is what reaches it: each a function, agent, trigger or mapping of
	// the package, in document order.
	By []AuthorizationReachBy `json:"by"`
	// Covered is whether a scope left checked covers it; Scope is the one
	// that would, `full` where no scope can.
	Covered bool   `json:"covered,omitempty"`
	Scope   string `json:"scope"`
}

// AuthorizationReachBy is one piece of code that reaches a kind: Type is
// `function`, `agent`, `trigger` or `recordmapping`, and ID its id.
type AuthorizationReachBy struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// AuthorizationTrigger is a trigger a package ships. Enabled is how it ships:
// an enabled record or schedule trigger runs on its own once installed.
type AuthorizationTrigger struct {
	ID       string `json:"id"`
	Callable string `json:"callable,omitempty"`
	Webhook  bool   `json:"webhook,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// AuthorizationAccount is an account row: the record approving creates once.
type AuthorizationAccount struct {
	Kind       string         `json:"kind"`
	Label      *KindLabel     `json:"label,omitempty"`
	ID         string         `json:"id"`
	Properties map[string]any `json:"properties,omitempty"`
}

// AuthorizationWebhookPlan is a webhook row: the trigger approving turns on.
// Package is the package its callable belongs to, the one deliveries reach.
// Keyed says whether it holds a key already; the key is never shown or
// shared, since the app delivers with its own token. Allowed is true when the
// previous token already delivers to the trigger, by the same rule as a
// scope row's Allowed. A check answers Trigger and Package alone.
type AuthorizationWebhookPlan struct {
	Trigger string `json:"trigger"`
	Package string `json:"package,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
	Keyed   bool   `json:"keyed,omitempty"`
	Allowed bool   `json:"allowed,omitempty"`
}

// AuthorizationPrevious is the newest token the repository holds for the
// requesting client id. Never present for a local app, whose client id
// nothing verifies.
type AuthorizationPrevious struct {
	ApprovedAt time.Time  `json:"approvedAt"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	Scopes     []string   `json:"scopes"`
}

// AuthorizationFailure is why a set up stopped, in the owner's words: Row
// is the row it stopped at, by its index in Setup, absent when it stopped
// before or after every row (waiting for the repository's turn, issuing the
// code).
type AuthorizationFailure struct {
	Row     *int   `json:"row,omitempty"`
	Message string `json:"message"`
}

// AuthorizationCheck is `POST /api/v1/authorizations/check`'s body: the
// request an app would send now, its `scope` parameter, its
// `authorization_details` and its `software_version`, held to the rules
// /oauth/authorize holds them to. The check records the build on the
// calling token (decision 0213): the floor of each package detail and
// SoftwareVersion.
type AuthorizationCheck struct {
	Scope           string                `json:"scope"`
	Details         []AuthorizationDetail `json:"details"`
	SoftwareVersion string                `json:"softwareVersion,omitempty"`
}

// AuthorizationCheckResult says whether a token still has what its app
// needs: the requested scopes it lacks, each detail's action (`unknown`
// where the token cannot see it) and when the token ends.
type AuthorizationCheckResult struct {
	Scopes    AuthorizationCheckScopes `json:"scopes"`
	Setup     []AuthorizationSetup     `json:"setup"`
	ExpiresAt *time.Time               `json:"expiresAt,omitempty"`
}

// AuthorizationCheckScopes lists the requested scopes, resolved, that the
// token does not hold.
type AuthorizationCheckScopes struct {
	Missing []string `json:"missing"`
}

// AuthorizationApp is the app asking. Host is the client id URL's host, the
// one fact about the app that its URL proves; Name and URI are what the
// app's own document claims, absent when it claims nothing, and always
// absent for a local app, whose document is never fetched.
type AuthorizationApp struct {
	ClientID string `json:"clientId"`
	Host     string `json:"host"`
	// Local is true for a client id on a loopback address: an app on the
	// owner's own computer, which nothing can verify.
	Local bool   `json:"local"`
	Name  string `json:"name,omitempty"`
	URI   string `json:"uri,omitempty"`
	// URIHostDiffers is true when the claimed URI's host is not Host, which
	// the page warns about: the app is naming a site its client id does not
	// prove it runs.
	URIHostDiffers bool `json:"uriHostDiffers,omitempty"`
	// AppearsAs is the name the app's changes read under, the publisher its
	// actor record names (decision 0270): its authority for a verified app,
	// `local` for one whose access goes to this device, its client id's host
	// otherwise (decision 0209).
	AppearsAs string `json:"appearsAs"`
	// Verified is true when the app's publisher is confirmed: its document
	// claims an authority, the authority's `_substrate` record names a
	// document that lists this client id, and the access goes to the
	// authority or a name under it.
	Verified  bool                   `json:"verified"`
	Publisher AuthorizationPublisher `json:"publisher"`
}

// AuthorizationPublisher is what the server read about who publishes an
// app. Authority is the verified one, or the one the app claims; Name is the
// authority document's name for the publisher and AppName its name for the
// app, both only when the document was read. Unconfirmed is why an app is
// not verified, absent when it is or when it claims no publisher. Record,
// DocumentURL and Listed are the technical details: the `_substrate` record
// as read, the document it names, and whether that document lists the
// client id. FromFile is true when a file this server was configured to
// read (SUBSTRATE_INSECURE_AUTHORITY_DOCS) stood in for the authority's DNS
// record and document, so the operator, not the authority, vouched for it.
type AuthorizationPublisher struct {
	Authority   string `json:"authority,omitempty"`
	Name        string `json:"name,omitempty"`
	AppName     string `json:"appName,omitempty"`
	Unconfirmed string `json:"unconfirmed,omitempty"`
	Record      string `json:"record,omitempty"`
	DocumentURL string `json:"documentUrl,omitempty"`
	Listed      bool   `json:"listed"`
	FromFile    bool   `json:"fromFile,omitempty"`
}

// AuthorizationRedirectTo is where the code goes on approval.
type AuthorizationRedirectTo struct {
	URI  string `json:"uri"`
	Host string `json:"host"`
	// Loopback is true when the access goes to a loopback address: a program
	// on the owner's own computer, which nothing verifies is the app. Any
	// other redirect is on the app's own origin, the one its client id
	// proves.
	Loopback bool `json:"loopback,omitempty"`
}

// AuthorizationScope is one requested scope, read against the repository.
// Scope is the canonical spelling the token would store and the approval
// names, the repository's own kinds under `~`; Requested is how the app
// wrote it, which differs when it spelled them with the repository's full
// name (decision record 0259).
type AuthorizationScope struct {
	Scope     string `json:"scope"`
	Requested string `json:"requested"`
	// Verb is what the scope does: `read` or `write` over kinds, `agents`
	// or `decide` over agents (decision 0195). Write is Verb == `write`,
	// kept on the wire for the approval pages written before Verb.
	Verb    string `json:"verb"`
	Write   bool   `json:"write"`
	Pattern string `json:"pattern"`
	// Glob is true when Pattern covers more than one kind or agent, and so
	// also covers the ones the repository gains later.
	Glob bool `json:"glob"`
	// Kinds are the kinds a read or write scope reaches in the repository
	// today, sorted by reference; empty when it reaches none yet, and always
	// empty for an agents or decide scope.
	Kinds []AuthorizationKind `json:"kinds"`
	// Agents are the agent rows an agents or decide scope matches today,
	// sorted by id, so the page names each and says what it may read and
	// change; empty when it matches none yet, and always empty for a read
	// or write scope.
	Agents []*Record `json:"agents"`
	// Functions are the functions a call scope matches today, sorted by id,
	// each with the reach a call of it is held to (decision 0219); empty
	// when it matches none yet, and always empty for every other verb.
	Functions []AuthorizationCallFunction `json:"functions"`
	// Allowed is true when the previous token's scopes already cover this
	// one (write covers read; a wider pattern covers a narrower one), so the
	// page can fold it under what was allowed before.
	Allowed bool `json:"allowed,omitempty"`
}

// AuthorizationCallFunction is one function a `call:` scope matches today
// (decision 0219): its id and description, the reach its own permissions
// and those of what it may call declare, and whether the request's read and
// write scopes cover that reach, which is what a call is held to.
type AuthorizationCallFunction struct {
	Identity    string `json:"identity"`
	Description string `json:"description,omitempty"`
	// Reads and Writes are the kinds, or globs over kinds, the function and
	// what it calls read and write, sorted.
	Reads  []string `json:"reads"`
	Writes []string `json:"writes"`
	// Covered is true when the request's read and write scopes cover every
	// entry of the reach; Uncovered names the entries they do not, each
	// `read <kind>` or `write <kind>`.
	Covered   bool     `json:"covered"`
	Uncovered []string `json:"uncovered,omitempty"`
}

// AuthorizationKind is one kind a scope reaches today, or one a package the
// request sets up would add (Added). Purpose is the kind's declared purpose
// where it is not `primary`.
type AuthorizationKind struct {
	Identity string     `json:"identity"`
	Label    *KindLabel `json:"label,omitempty"`
	Purpose  string     `json:"purpose,omitempty"`
	Added    bool       `json:"added,omitempty"`
}

// AuthorizationSetting describes an empty configuration record, never its value.
type AuthorizationSetting struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Secret      bool   `json:"secret"`
	Required    bool   `json:"required"`
}

// AuthorizationSettingValue exists only in the owner's approval POST.
type AuthorizationSettingValue struct {
	ID     string `json:"id"`
	Secret bool   `json:"secret"`
	Value  string `json:"value"`
}

const BlockNeedsSetting = "needs-setting"

// AuthorizationApproval is the approval: the subset of the request's
// resolved scopes the owner checked, at least one, and the lifetime picked,
// one of the request's LifetimeDays (0 for until revoked).
type AuthorizationApproval struct {
	Addons   []string                    `json:"addons,omitempty"`
	Settings []AuthorizationSettingValue `json:"settings,omitempty"`
	Scopes   []string                    `json:"scopes"`
	// LifetimeDays is required: a pointer, so an approval that names none
	// is refused rather than read as 0, which is until revoked.
	LifetimeDays *int `json:"lifetimeDays"`
	// PlanHash is the request's planHash as the page read it; required
	// when the request has setup to run.
	PlanHash string `json:"planHash,omitempty"`

	// ProviderChoices are validated against the plan before the package inputs are bound.
	ProviderChoices []AuthorizationProviderChoice `json:"providerChoices,omitempty"`
}

// AuthorizationAnswer is an approval's or a denial's answer: the URL the
// page navigates to, the app's redirect URI carrying the code or the
// refusal, the request's state and the issuer. An approval whose set up runs
// in the background answers no redirect, State `setting-up` and Setup, the
// rows the set up runs with their first progress: the page shows those, then
// reads the request until it is approved and carries the redirect.
type AuthorizationAnswer struct {
	Redirect string               `json:"redirect,omitempty"`
	State    string               `json:"state,omitempty"`
	Setup    []AuthorizationSetup `json:"setup,omitempty"`
}
