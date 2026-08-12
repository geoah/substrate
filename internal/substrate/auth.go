package substrate

import "time"

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
	// LastUsedAt is COARSE: authentication stamps it at most
	// once a minute, so a busy token does not write a changelog entry per request.
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// TOTPEnrollment is a candidate second factor: the base32 seed and the
// otpauth:// URI a password manager imports. Issuing one creates NOTHING
// durable — the caller proves possession by returning the seed with one code,
// and only that call writes.
type TOTPEnrollment struct {
	Secret string `json:"totpSecret"`
	URI    string `json:"otpauthUri"`
}

// RegisterInput is the registration commit: the username, the chosen
// password, and the enrollment the caller was issued plus one code from it.
// The invite code is checked by the HTTP layer, which is where the
// configuration lives; the engine never sees it.
type RegisterInput struct {
	Username   string
	Password   string
	TOTPSecret string
	TOTPCode   string
	// Label names the token registration mints, exactly as login's does.
	Label string
}

// LoginInput is BOTH factors presented directly: it authenticates /login and
// it is the password-factor rule's evidence on every endpoint
// that changes auth material. A bearer token is never a substitute.
type LoginInput struct {
	Username string
	Password string
	TOTPCode string
	// Label names the token a login mints; ignored by the credential-changing
	// paths, which mint nothing.
	Label string
}
