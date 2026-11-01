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
	// request's order, computed at every read: after a set up stops part
	// way, a second read shows what landed.
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
	// Progress is each setup row's state while a set up runs, and after
	// one stopped: `waiting`, `running`, `done` or `failed`, in Setup's
	// order.
	Progress []string `json:"progress,omitempty"`
	// Failure is why the last set up stopped; the request is pending again.
	Failure *AuthorizationFailure `json:"failure,omitempty"`
	// Next is where the page goes once a set up finished and the code was
	// issued: the app's redirect URI carrying it.
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

// The actions a setup row may need. SetupNone is in place already; SetupUnknown
// answers a check for a row the token cannot see, which an app treats as
// nothing to ask about.
const (
	SetupInstall = "install"
	SetupUpgrade = "upgrade"
	SetupCreate  = "create"
	SetupArm     = "arm"
	SetupNone    = "none"
	SetupBlocked = "blocked"
	SetupUnknown = "unknown"
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
	// Problem says why a blocked row is blocked, in the owner's words, and
	// Fix where to go about it: a console path, or empty when the server
	// itself needs an update.
	Problem string                    `json:"problem,omitempty"`
	Fix     string                    `json:"fix,omitempty"`
	Package *AuthorizationPackage     `json:"package,omitempty"`
	Account *AuthorizationAccount     `json:"account,omitempty"`
	Webhook *AuthorizationWebhookPlan `json:"webhook,omitempty"`
}

// AuthorizationPackage is a package row's facts. Held is the catalog release
// the repository holds (its shippedVersion when published, else its stored
// version), 0 when it holds none; Shipped is what this server's catalog
// ships. A check answers ID, Required, Held and Shipped alone.
type AuthorizationPackage struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Tier        string `json:"tier,omitempty"`
	Required    int64  `json:"required"`
	Held        int64  `json:"held"`
	Shipped     int64  `json:"shipped"`
	// Requires is the requirement chain an install brings first, each with
	// its own action, in the order they land.
	Requires  []AuthorizationPackageStep `json:"requires,omitempty"`
	Kinds     []AuthorizationKind        `json:"kinds,omitempty"`
	Functions []AuthorizationFunction    `json:"functions,omitempty"`
	Triggers  []AuthorizationTrigger     `json:"triggers,omitempty"`
}

// AuthorizationPackageStep is one package a requested one requires.
type AuthorizationPackageStep struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Action  string `json:"action"`
	Held    int64  `json:"held"`
	Shipped int64  `json:"shipped"`
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
// Keyed says whether it holds a key already; the key is never shown or
// shared, since the app delivers with its own token.
type AuthorizationWebhookPlan struct {
	Trigger string `json:"trigger"`
	Package string `json:"package,omitempty"`
	Enabled bool   `json:"enabled"`
	Keyed   bool   `json:"keyed"`
}

// AuthorizationPrevious is the newest token the repository holds for the
// requesting client id.
type AuthorizationPrevious struct {
	ApprovedAt time.Time  `json:"approvedAt"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	Scopes     []string   `json:"scopes"`
}

// AuthorizationFailure is why a set up stopped: the row, by its index in
// Setup, and the error in the owner's words.
type AuthorizationFailure struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

// AuthorizationCheck is `POST /api/v1/authorizations/check`'s body: the
// request an app would send now.
type AuthorizationCheck struct {
	Scope   string                `json:"scope"`
	Details []AuthorizationDetail `json:"authorization_details"`
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
// in the background answers no redirect and State `setting-up`: the page
// reads the request until it is approved and carries the redirect.
type AuthorizationAnswer struct {
	Redirect string `json:"redirect,omitempty"`
	State    string `json:"state,omitempty"`
}
