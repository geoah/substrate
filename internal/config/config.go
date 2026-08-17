// Package config loads the substrate service configuration from the
// environment; there is no settings surface by design.
package config

import "github.com/kelseyhightower/envconfig"

// Config is the full service configuration.
type Config struct {
	Port     string `envconfig:"PORT" default:"8080"`
	LogLevel string `envconfig:"LOG_LEVEL" default:"info"`
	// WebDir is the built SPA served at /; empty disables static serving
	// (dev mode, where Vite proxies the API).
	WebDir string `envconfig:"WEB_DIR" default:""`

	// DatabaseURL is the one Postgres holding every repository, in one
	// shared schema.
	DatabaseURL string `envconfig:"DATABASE_URL" required:"true"`
	// Blobs says where blob bytes live. Its own type, because the operator
	// hat loads it without the rest (LoadBlobs).
	Blobs Blobs
	// InviteCode is the ONE door into a fresh substrate: registering with it
	// creates a user and their one repository. UNSET TURNS REGISTRATION OFF —
	// /register answers `unsupported` — which is the right default for a
	// substrate that already has its user.
	InviteCode string `envconfig:"SUBSTRATE_INVITE_CODE" default:""`

	// InsecureDisableTOTP takes the SECOND FACTOR OFF the whole door: login,
	// registration and the credential changes ask for a username and a
	// password and nothing else. It exists for a local substrate you wipe
	// every day, where enrolling an authenticator to reach a throwaway
	// repository is friction with nothing behind it. NEVER set it on a
	// deployment anybody can reach: a leaked password is then the account.
	// The seed is still minted and still stored, so turning it back off
	// restores the factor the user enrolled.
	InsecureDisableTOTP bool `envconfig:"SUBSTRATE_INSECURE_DISABLE_TOTP" default:"false"`

	// The host OAuth facility (bundles declare auth, the host runs it).
	// StateKey signs flow state; CallbackURL is the one redirect URI every
	// provider app registers. Both unset disables the facility.
	OAuthStateKey    string `envconfig:"SUBSTRATE_OAUTH_STATE_KEY" default:""`
	OAuthCallbackURL string `envconfig:"SUBSTRATE_OAUTH_CALLBACK_URL" default:""`
	// CredentialKey seals the credential store (AES-256-GCM, derived from
	// any string) and every repository's changelog signing seed. Changelog
	// signing is MANDATORY and its seed may never sit unsealed beside the
	// signatures it mints, so a host without this key refuses to boot. There
	// is no exception.
	CredentialKey string `envconfig:"SUBSTRATE_CREDENTIAL_KEY" default:""`
	// ConsoleURL is the console origin the OAuth callback return-page posts its
	// completion message to and falls back to redirecting into. The scheme+host
	// is the postMessage targetOrigin; the full base is the fallback redirect
	// target. Empty (local dev) uses targetOrigin "*" and renders no redirect.
	ConsoleURL string `envconfig:"SUBSTRATE_CONSOLE_URL" default:""`

	// There is NO LLM configuration here. Completions and embeddings alike are
	// bought through a repository's own llmprovider records, which carry the
	// wire, the endpoint, the key and (for embeddings) the model, so the
	// process holds no bearer that could reach a repository-chosen endpoint.
}

// Load reads the configuration from the environment.
func Load() (Config, error) {
	var c Config
	err := envconfig.Process("", &c)
	return c, err
}
