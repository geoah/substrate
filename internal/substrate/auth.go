package substrate

import (
	"context"
	"time"
)

// The PRINCIPAL is the token id the door resolved from the bearer secret —
// what the substrate verified, beside the actor the caller merely asserted.
// It rides the context because a write path already carries one everywhere
// and because a caller must not be able to name it: the only writer is
// `withRequestAuth` (internal/api), from the TokenInfo `Authenticate`
// returned, and the only reader is the engine's transaction, which stamps it
// on the changelog entry and on every manager row the write lands.
type principalKey struct{}

// WithPrincipal binds the authenticated token id to ctx.
func WithPrincipal(ctx context.Context, tokenID string) context.Context {
	return context.WithValue(ctx, principalKey{}, tokenID)
}

// PrincipalFrom returns the authenticated token id, empty when no token stands
// behind the write: the seed, the boot upgrade, a background worker, and the
// unauthenticated doors (register, login) write without one.
func PrincipalFrom(ctx context.Context) string {
	id, _ := ctx.Value(principalKey{}).(string)
	return id
}

// The SCOPES of an app's token ride the context beside the principal, for the
// same reason: the engine's owner-only gates (revoking a token, creating or
// deleting an account, starting a provider connect) must refuse a request
// whose token an app holds, and the actor alone cannot tell, because every
// HTTP actor resolves to the owner tier. The only writer is `withRequestAuth`.
type scopesKey struct{}

// WithScopes marks ctx as serving a scoped token (decision 0194). A nil
// scopes leaves ctx unmarked: a full-access token is the owner's own hand.
func WithScopes(ctx context.Context, scopes []string) context.Context {
	if scopes == nil {
		return ctx
	}
	return context.WithValue(ctx, scopesKey{}, scopes)
}

// ScopesFrom returns the scopes of the token behind ctx and whether there is
// one: false means no scoped token stands behind the request.
func ScopesFrom(ctx context.Context) ([]string, bool) {
	s, ok := ctx.Value(scopesKey{}).([]string)
	return s, ok
}

// WithoutScopes clears the scoped-token mark: the work ctx goes on to is not
// the app's own request. A chat turn an app opens runs the agent under the
// agent's permissions (decision 0195), and the agent's writes must meet the
// gates an owner's chat meets, not the ones the app's token does. The
// principal stays, so the changelog still names the token behind the turn.
func WithoutScopes(ctx context.Context) context.Context {
	if _, ok := ScopesFrom(ctx); !ok {
		return ctx
	}
	return context.WithValue(ctx, scopesKey{}, nil)
}

// A DECISION mark names the one change request an app's token may decide
// under a `decide:` scope (decision 0195). The API sets it after it has held
// the request to the grant, and the engine's owner-only gates on accepting a
// change request open for that record alone: everything else the write
// touches is still held to the scoped mark.
type decisionKey struct{}

// WithDecision marks ctx as deciding the change request at path,
// `<kind>/<id>`.
func WithDecision(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, decisionKey{}, path)
}

// DecisionFrom returns the record path WithDecision named, "" when none.
func DecisionFrom(ctx context.Context) string {
	p, _ := ctx.Value(decisionKey{}).(string)
	return p
}

// The CLIENT ID of an app's token rides the context beside its scopes: the
// full client id (its client id metadata document URL) the owner approved.
// The actor is `app:<authority>` for a verified publisher, `app:local` for
// a loopback approval, and `app:<host>` of the client id otherwise. Apps may
// share an actor; the client id tells them apart in the label and annotation
// namespace an app keeps. The only writer is `withRequestAuth`, and only for
// a scoped token: a full-access app token writes as a door.
type clientIDKey struct{}

// WithClientID marks ctx as serving the app whose client id is id. An empty
// id leaves ctx unmarked.
func WithClientID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, clientIDKey{}, id)
}

// ClientIDFrom returns the client id of the app token behind ctx, "" when
// none stands behind the request.
func ClientIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(clientIDKey{}).(string)
	return id
}

// A WEBHOOK GRANT stands in for one webhook trigger's key
// (docs/plans/app-setup.md). The webhook door sets it once it has
// authenticated the bearer as an app's token that opens the repository the
// path names and holds `webhook:` for the trigger the path names; the
// engine's door then skips the key comparison for that trigger alone, and
// every other check, a declared signature included, still runs.
type webhookGrantKey struct{}

type webhookGrant struct{ authority, trigger string }

// WithWebhookGrant marks ctx as delivering to trigger in the repository whose
// authority is authority, under a token that holds `webhook:` for it.
func WithWebhookGrant(ctx context.Context, authority, trigger string) context.Context {
	return context.WithValue(ctx, webhookGrantKey{}, webhookGrant{authority: authority, trigger: trigger})
}

// WebhookGranted reports whether ctx carries the webhook grant for exactly
// this authority and trigger.
func WebhookGranted(ctx context.Context, authority, trigger string) bool {
	g, ok := ctx.Value(webhookGrantKey{}).(webhookGrant)
	return ok && authority != "" && trigger != "" && g == webhookGrant{authority: authority, trigger: trigger}
}

// TokenSecretPrefix begins every token's bearer secret. The webhook door
// reads a bearer carrying it as a token before it reads it as a trigger key.
const TokenSecretPrefix = "substrate_tok_"

// TokenInfo is a token RECORD's metadata — never the secret, which is shown
// exactly once at mint and stored only as a SHA-256. A token without Scopes
// has full access to its repository (decision 0143); a token an app received
// through the authorization flow carries the scopes the owner approved and
// the app it was minted for (decision 0194).
type TokenInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Created is when the token was minted; ExpiresAt, when set, is
	// SERVER-ENFORCED at Authenticate — a token past it fails authentication,
	// no revoke step needed. Nil = lives until the record is deleted.
	Created   time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// Scopes is nil for a full-access token and never empty otherwise: the
	// mint refuses an empty list, so absence stays the one spelling of full
	// access on the record and on the wire.
	Scopes []string `json:"scopes,omitempty"`
	// Retired grants remain listable and revocable but never authenticate.
	Retired bool `json:"retired,omitempty"`
	// ClientID is the app's client_id URL and ClientName the name it claimed
	// when the owner approved it; both are empty on a token nobody minted
	// for an app.
	ClientID   string `json:"clientId,omitempty"`
	ClientName string `json:"clientName,omitempty"`
	// AppAuthority is the authority the approval verified as the app's
	// publisher (decision 0209), and AppLocal is true when the approval sent
	// the access to a loopback redirect, a program on the owner's device.
	// Both are what the app's actor is derived from. AppAuthorityFromFile
	// is true when a file this server reads
	// (SUBSTRATE_INSECURE_AUTHORITY_DOCS), not the authority's DNS, vouched
	// for AppAuthority.
	AppAuthority         string `json:"appAuthority,omitempty"`
	AppAuthorityFromFile bool   `json:"appAuthorityFromFile,omitempty"`
	AppLocal             bool   `json:"appLocal,omitempty"`
	// AppPackages, AppSoftwareVersion and AppCheckedAt are what the app's
	// build said of itself (decision 0213): the floor each of its package
	// details asks for, by package id, its own version, and when it last
	// checked in. An upgrade that narrows a package is held while a live
	// token's floor is under the candidate.
	AppPackages        map[string]int64 `json:"appPackages,omitempty"`
	AppSoftwareVersion string           `json:"appSoftwareVersion,omitempty"`
	AppCheckedAt       *time.Time       `json:"appCheckedAt,omitempty"`
	// AppAddons are the add-on packages the owner approved while the
	// provider each extends was not installed (decision 0216): installing
	// that provider later installs them under this token's approval.
	AppAddons []AppAddon `json:"appAddons,omitempty"`
}

// Scoped reports whether the token reaches less than its whole repository.
func (t TokenInfo) Scoped() bool { return t.Scopes != nil }

// MintTokenInput is one token mint. Label is required; the rest is optional.
// Scopes, when non-nil, must be non-empty and already resolved: no `~` in an
// authority, which only the authorization flow knows how to complete.
type MintTokenInput struct {
	Label      string
	ExpiresAt  *time.Time
	Scopes     []string
	ClientID   string
	ClientName string
	// AppAuthority, AppAuthorityFromFile and AppLocal are TokenInfo's:
	// what the approval proved about the app.
	AppAuthority         string
	AppAuthorityFromFile bool
	AppLocal             bool
	// AppPackages and AppSoftwareVersion are TokenInfo's: what the app's
	// build asked for when the owner approved it.
	AppPackages        map[string]int64
	AppSoftwareVersion string
	// AppAddons is TokenInfo's: the add-ons waiting for their provider.
	AppAddons []AppAddon
}

// MintedToken is what a login, a registration and a `POST /tokens` answer
// with: the token record and its secret, shown this once.
type MintedToken struct {
	Token  TokenInfo `json:"token"`
	Secret string    `json:"secret"`
	// Repository is the repository the token opens, as the door resolved it:
	// login and registration echo it, so a client that sent a bare label
	// learns the authority it got. A `POST /tokens` mint omits it, because
	// the caller is already in the repository it mints for.
	Repository string `json:"repository,omitempty"`
}

// SessionCredential is what a credential change answers (a password change, a
// TOTP re-enrollment, a sign out everywhere): the repository the factors
// proved, and, where the request signed the person out everywhere, how many
// tokens that ended. No token is minted, so the caller signs in again with
// the new material.
type SessionCredential struct {
	Repository string `json:"repository"`
	SignedOut  *int   `json:"signedOut,omitempty"`
}

// LoginRequest is a login as the HTTP door decodes it: the repository, the two
// factors and the label of the token the login mints. Repository is the name
// the user registered — its authority, or the bare label the door completes
// under its own host.
type LoginRequest struct {
	Repository string `json:"repository"`
	Password   string `json:"password"`
	TOTPCode   string `json:"totpCode"`
	Label      string `json:"label,omitempty"`
}

// RegisterRequest is the registration commit as the HTTP door decodes it:
// the invite code, the repository to create, the password, and the enrollment
// the caller was issued plus one code from it, or SkipTOTP in their place. The
// door builds the engine's RegisterInput from it.
type RegisterRequest struct {
	InviteCode string `json:"inviteCode"`
	// Repository is the name of the repository to create, which BECOMES its
	// authority: the home of every kind its user declares. A name carrying a
	// dot is the authority itself (`ada.example.com`); a bare label is
	// completed under the host the request reached (`ada` ->
	// `ada.example.com`). It is also the login name, and it is permanent.
	Repository string `json:"repository"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totpSecret"`
	TOTPCode   string `json:"totpCode"`
	// SkipTOTP registers without a second factor, which the owner may enroll
	// later with the same two calls that replace one. It is explicit, so a
	// request that merely lost its enrollment is refused.
	SkipTOTP bool `json:"skipTotp,omitempty"`
	// Label names the token registration mints; absent, the door's default.
	Label string `json:"label,omitempty"`
}

// Registered is what a registration answers: the mint, whose `repository` is
// the one that was created.
type Registered struct {
	MintedToken
}

// TOTPEnrollment is a candidate second factor: the base32 seed and the
// otpauth:// URI a password manager imports. Issuing one creates NOTHING
// durable — the caller proves possession by returning the seed with one code,
// and only that call writes.
type TOTPEnrollment struct {
	Secret string `json:"totpSecret"`
	URI    string `json:"otpauthUri"`
}

// RegisterInput is the registration commit: the repository to create, the
// chosen password, and the enrollment the caller was issued plus one code
// from it. The invite code is checked by the HTTP layer, which is where the
// configuration lives; the engine never sees it.
type RegisterInput struct {
	// Repository is the repository's authority (`ada.example.com`), its name
	// and its user's login name; the id that keys it is minted at creation. The HTTP layer resolves a bare label under
	// the host the request reached; the engine requires a concrete
	// authority, validates the grammar and refuses a taken one.
	Repository string
	Password   string
	TOTPSecret string
	TOTPCode   string
	// SkipTOTP commits the credential with no second factor; TOTPSecret and
	// TOTPCode are then refused.
	SkipTOTP bool
	// Label names the token registration mints, exactly as login's does.
	Label string
}

// RegisterResult is what a registration hands back: the first token and its
// secret, shown once.
type RegisterResult struct {
	Token  TokenInfo
	Secret string
	// Repository is the repository that was created, echoed so a client that
	// sent a bare label learns the authority it got.
	Repository string
}

// LoginInput is BOTH factors presented directly: it authenticates /login and
// it is the password-factor rule's evidence on every endpoint
// that changes auth material. A bearer token is never a substitute.
type LoginInput struct {
	// Repository is the repository the factors admit to: its authority, which
	// the HTTP layer has already resolved from whatever the caller typed.
	Repository string
	Password   string
	TOTPCode   string
	// Label names the token a login mints; ignored by the credential-changing
	// paths, which mint nothing.
	Label string
}

// AppAddon is one add-on an approval left waiting for its provider: the
// package, the floor the app asked for, and the provider it extends.
type AppAddon struct {
	Package  string `json:"package"`
	Version  int64  `json:"version"`
	AddonFor string `json:"addonFor"`
}

// The actor a request acts FOR rides the context beside the principal: a
// write a person's request starts in installed code (an agent the person
// runs, a function the person calls) is the code's, on behalf of the person
// (decision 0270). The only writer is `withRequestAuth`; a dispatch the
// trigger pass fires later runs on the server's own context and acts for no
// one.
type onBehalfOfKey struct{}

// WithOnBehalfOf binds the actor a request acts for to ctx.
func WithOnBehalfOf(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, onBehalfOfKey{}, actor)
}

// OnBehalfOfFrom returns the actor the request acts for, "" when none.
func OnBehalfOfFrom(ctx context.Context) Actor {
	a, _ := ctx.Value(onBehalfOfKey{}).(Actor)
	return a
}
