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

// TokenInfo is a token RECORD's metadata — never the secret, which is shown
// exactly once at mint and stored only as a SHA-256. A token has full access
// to its repository: there are no scopes, no actors set and no roles on it, so
// this is the whole of what a token is.
type TokenInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Created is when the token was minted; ExpiresAt, when set, is
	// SERVER-ENFORCED at Authenticate — a token past it fails authentication,
	// no revoke step needed. Nil = lives until the record is deleted.
	Created   time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// MintedToken is what a login and a `POST /tokens` answer with: the token
// record and its secret, shown this once. Registration answers the same
// fields with the recovery material beside them.
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
// TOTP re-enrollment): the repository the factors proved, and nothing else.
// No token is minted, so the caller signs in again with the new material.
type SessionCredential struct {
	Repository string `json:"repository"`
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
// the caller was issued plus one code from it. The door builds the engine's
// RegisterInput from it.
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
	// Label names the token registration mints; absent, the door's default.
	Label string `json:"label,omitempty"`
	// RecoveryPublicKey is the client-generated age recipient; absent asks
	// the server to mint the pair and return the identity once.
	RecoveryPublicKey string `json:"recoveryPublicKey,omitempty"`
}

// Registered is what a registration answers: the mint, whose `repository` is
// the one that was created, and the recovery material. RecoveryKey is present
// only when the server generated the pair, shown this once like the token
// secret beside it.
type Registered struct {
	MintedToken
	RecoveryKey       string `json:"recoveryKey,omitempty"`
	RecoveryPublicKey string `json:"recoveryPublicKey,omitempty"`
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
	// Repository is the repository's authority (`ada.example.com`), its id
	// and its user's login name. The HTTP layer resolves a bare label under
	// the host the request reached; the engine requires a concrete
	// authority, validates the grammar and refuses a taken one.
	Repository string
	Password   string
	TOTPSecret string
	TOTPCode   string
	// Label names the token registration mints, exactly as login's does.
	Label string
	// RecoveryPublicKey is the age recipient the repository's data-encryption
	// key wraps to, generated CLIENT-SIDE so the matching identity never
	// touches the server. Empty asks the server to generate the pair and
	// return the identity once in RegisterResult.RecoveryKey.
	RecoveryPublicKey string
}

// RegisterResult is what a registration hands back: the first token, its
// secret shown once, and the recovery material.
type RegisterResult struct {
	Token  TokenInfo
	Secret string
	// Repository is the repository that was created, echoed so a client that
	// sent a bare label learns the authority it got.
	Repository string
	// RecoveryKey is the age identity that opens the repository's recovery
	// wrap, present ONLY when the server generated the pair (the input named
	// no recipient). Shown once, never stored.
	RecoveryKey string
	// RecoveryPublicKey is the enrolled age recipient, whichever side
	// generated it.
	RecoveryPublicKey string
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

// RecoveryEnroller is one-time recovery enrollment, an optional Service
// extension (see Service): it exists for repositories that predate recovery
// keys, and registration is the ordinary door.
type RecoveryEnroller interface {
	EnrollRecoveryKey(ctx context.Context, in LoginInput, publicKey string) (identity, recipient string, err error)
}
