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
	// any string). Unset stores provider tokens unsealed, with a boot warning.
	CredentialKey string `envconfig:"SUBSTRATE_CREDENTIAL_KEY" default:""`
	// InsecureAllowInvalidSignatures lets a substrate run without the
	// signature guarantee. Changelog signing is MANDATORY: every repository
	// activates per-repository Ed25519 at its next open, and the signing
	// seed seals under the credential key, so a keyless host refuses to
	// boot. This switch is the local-testing exception: a keyless host runs,
	// never activates signing, stamps the all-zero placeholder signature on
	// every entry, and `repository verify` names that state as a finding.
	// Verification still runs everywhere and still complains; this only
	// stops the complaints from refusing. It does NOT weaken an activated
	// repository: activation is one-way, and an activated repository whose
	// key cannot open refuses to append no matter what this is set to.
	// Placeholder attestations are pre-v1 scaffolding (issue #175).
	InsecureAllowInvalidSignatures bool `envconfig:"SUBSTRATE_INSECURE_ALLOW_INVALID_SIGNATURES" default:"false"`
	// ConsoleURL is the console origin the OAuth callback return-page posts its
	// completion message to and falls back to redirecting into. The scheme+host
	// is the postMessage targetOrigin; the full base is the fallback redirect
	// target. Empty (local dev) uses targetOrigin "*" and renders no redirect.
	ConsoleURL string `envconfig:"SUBSTRATE_CONSOLE_URL" default:""`

	// The host's own LLM gateway: an OpenAI-wire endpoint. It backs the embed
	// queue — without a base URL and key the embedder is nil and the queue
	// simply does not drain — and it is what an openai-dialect llmprovider row
	// with no baseURL of its own falls back to. The key travels ONLY to this
	// URL. The embed model is a bare model id; a gateway that wants an alias
	// (`openai/…`) is naming its own configuration, so it belongs in the env.
	LLMBaseURL    string `envconfig:"SUBSTRATE_LLM_BASE_URL" default:""`
	LLMAPIKey     string `envconfig:"SUBSTRATE_LLM_API_KEY"`
	LLMEmbedModel string `envconfig:"SUBSTRATE_LLM_EMBED_MODEL" default:"text-embedding-3-small"`
}

// Load reads the configuration from the environment.
func Load() (Config, error) {
	var c Config
	err := envconfig.Process("", &c)
	return c, err
}
