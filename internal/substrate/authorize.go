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
	// request's order. A pending request's rows are planned at every read,
	// so after a set up stops part way a second read shows what landed;
	// while a set up runs, and after one stopped, they are the rows the
	// approval was checked against, each carrying its Progress.
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
	// answered once, with Next and nothing planned again, and is gone after.
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
// version), absent when it holds none; Shipped is what this server's catalog
// ships, absent when it ships none. A check answers ID, Required, Held and
// Shipped alone, and ID and Required alone for a row it cannot see. A
// client names a package from its ID; the catalog carries no other name.
type AuthorizationPackage struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Tier        string `json:"tier,omitempty"`
	Required    int64  `json:"required"`
	Held        int64  `json:"held,omitempty"`
	Shipped     int64  `json:"shipped,omitempty"`
	// Requires is the requirement chain an install brings first, each with
	// its own action and its own facts, in the order they land. Kinds,
	// Functions and Triggers are this package's own.
	Requires  []AuthorizationPackageStep `json:"requires,omitempty"`
	Kinds     []AuthorizationKind        `json:"kinds,omitempty"`
	Functions []AuthorizationFunction    `json:"functions,omitempty"`
	Triggers  []AuthorizationTrigger     `json:"triggers,omitempty"`
}

// AuthorizationPackageStep is one package a requested one requires, with
// what it brings of its own. Blocked is present when it cannot be installed
// or upgraded, which blocks the row that requires it.
type AuthorizationPackageStep struct {
	ID          string                  `json:"id"`
	Description string                  `json:"description,omitempty"`
	Action      string                  `json:"action"`
	Blocked     *AuthorizationBlock     `json:"blocked,omitempty"`
	Held        int64                   `json:"held,omitempty"`
	Shipped     int64                   `json:"shipped,omitempty"`
	Kinds       []AuthorizationKind     `json:"kinds,omitempty"`
	Functions   []AuthorizationFunction `json:"functions,omitempty"`
	Triggers    []AuthorizationTrigger  `json:"triggers,omitempty"`
}

// AuthorizationFunction is code a package ships: what runs on the owner's
// server once it is installed, and the kinds it may write.
type AuthorizationFunction struct {
	ID     string   `json:"id"`
	Writes []string `json:"writes,omitempty"`
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
// shared, since the app delivers with its own token. A check answers
// Trigger and Package alone.
type AuthorizationWebhookPlan struct {
	Trigger string `json:"trigger"`
	Package string `json:"package,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
	Keyed   bool   `json:"keyed,omitempty"`
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
// request an app would send now, its `scope` parameter and its
// `authorization_details`, held to the rules /oauth/authorize holds them to.
type AuthorizationCheck struct {
	Scope   string                `json:"scope"`
	Details []AuthorizationDetail `json:"details"`
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
	// Actor is what the app's changes are recorded as, `app:<host>`.
	Actor string `json:"actor"`
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

// AuthorizationScope is one requested scope, resolved against the
// repository. Scope is the canonical, resolved spelling the token would
// store and the approval names; Requested is how the app wrote it, which
// differs when it used `~` for the repository's authority.
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
	// Allowed is true when the previous token's scopes already cover this
	// one (write covers read; a wider pattern covers a narrower one), so the
	// page can fold it under what was allowed before.
	Allowed bool `json:"allowed,omitempty"`
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

// AuthorizationApproval is the approval: the subset of the request's
// resolved scopes the owner checked, at least one, and the lifetime picked,
// one of the request's LifetimeDays (0 for until revoked).
type AuthorizationApproval struct {
	Scopes []string `json:"scopes"`
	// LifetimeDays is required: a pointer, so an approval that names none
	// is refused rather than read as 0, which is until revoked.
	LifetimeDays *int `json:"lifetimeDays"`
	// PlanHash is the request's planHash as the page read it; required
	// when the request has setup to run.
	PlanHash string `json:"planHash,omitempty"`
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
