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

	// The host OAuth facility (bundles declare auth, the host runs it).
	// StateKey signs flow state; CallbackURL is the one redirect URI every
	// provider app registers. Both unset disables the facility.
	OAuthStateKey    string `envconfig:"SUBSTRATE_OAUTH_STATE_KEY" default:""`
	OAuthCallbackURL string `envconfig:"SUBSTRATE_OAUTH_CALLBACK_URL" default:""`
	// CredentialKey seals the credential store (AES-256-GCM, derived from
	// any string). Unset stores provider tokens unsealed, with a boot warning.
	CredentialKey string `envconfig:"SUBSTRATE_CREDENTIAL_KEY" default:""`
	// ConsoleURL is the console origin the OAuth callback return-page posts its
	// completion message to and falls back to redirecting into. The scheme+host
	// is the postMessage targetOrigin; the full base is the fallback redirect
	// target. Empty (local dev) uses targetOrigin "*" and renders no redirect.
	ConsoleURL string `envconfig:"SUBSTRATE_CONSOLE_URL" default:""`

	// LiteLLM backs the embed queue. Without an API key the embedder is
	// nil and the queue simply does not drain.
	LiteLLMBaseURL string `envconfig:"LITELLM_BASE_URL" default:""`
	LiteLLMAPIKey  string `envconfig:"LITELLM_API_KEY"`
	LiteLLMMaster  string `envconfig:"LITELLM_MASTER_KEY"`
	LiteLLMModel   string `envconfig:"LITELLM_EMBED_MODEL" default:"openai/text-embedding-3-small"`
}

// Load reads the configuration from the environment.
func Load() (Config, error) {
	var c Config
	err := envconfig.Process("", &c)
	if c.LiteLLMAPIKey == "" {
		c.LiteLLMAPIKey = c.LiteLLMMaster
	}
	return c, err
}
