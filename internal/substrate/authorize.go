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
	// or `decide` over agents (decision 0195). Write is Verb == `write`.
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

// AuthorizationKind is one kind a scope reaches today.
type AuthorizationKind struct {
	Identity string     `json:"identity"`
	Label    *KindLabel `json:"label,omitempty"`
}

// AuthorizationApproval is the approval: the subset of the request's
// resolved scopes the owner checked, at least one, and the lifetime picked,
// one of the request's LifetimeDays (0 for until revoked).
type AuthorizationApproval struct {
	Scopes []string `json:"scopes"`
	// LifetimeDays is required: a pointer, so an approval that names none
	// is refused rather than read as 0, which is until revoked.
	LifetimeDays *int `json:"lifetimeDays"`
}

// AuthorizationAnswer is an approval's or a denial's answer: the URL the
// page navigates to, the app's redirect URI carrying the code or the
// refusal, the request's state and the issuer.
type AuthorizationAnswer struct {
	Redirect string `json:"redirect"`
}
