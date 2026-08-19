// Package oauthflow is the substrate host's OAuth mechanics, ported from the
// (now retired) connectors service's OAuth engine:
// HMAC-signed state, the authorization-code exchange, refresh, and
// best-effort revocation — against endpoints DECLARED ON DATA (a bundle's
// oauth2-trait configuration record), never a compiled-in provider table.
// Bundles declare auth; this package and the engine's storage run it.
package oauthflow

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/geoah/substrate/internal/egress"
)

// StateTTL bounds how long a signed state is accepted.
const StateTTL = 15 * time.Minute

// ErrBadState is returned for a forged, malformed, or expired state.
var ErrBadState = errors.New("oauthflow: invalid oauth state")

// Endpoints is one bundle's OAuth client configuration, read off its
// configuration record's oauth2-trait properties per flow — never cached, so
// a config edit takes effect on the next start.
type Endpoints struct {
	AuthURL       string
	TokenURL      string
	RevocationURL string // optional, best-effort revocation on teardown
	ClientID      string
	ClientSecret  string
	Scopes        []string
}

// State identifies the account record a flow is connecting: the repository's
// owner and the record id ride the signed state through the provider
// redirect. Nonce is the flow's one-time handle: the engine persists its hash
// when the flow starts and consumes it atomically at the callback, so a signed
// state — the callback's sole authentication — authorizes exactly one
// completion inside its TTL, never a replay.
type State struct {
	// Username names the repository by its OWNER, not by the opaque internal
	// id an engine.Scope carries: the callback arrives unauthenticated and
	// resolves the repository with the ordinary by-username lookup, which is
	// the only lookup a maintenance-pool read offers. Spelling this field
	// `Repository` invited exactly one wrong fix — handing it a scope id —
	// which resolves to nothing and breaks every consent.
	Username string `json:"repository"`
	Record   string `json:"record"`
	Nonce    string `json:"nonce,omitempty"`
	Exp      int64  `json:"exp"`
}

// Client runs flows. CallbackURL is the one redirect URI every bundle's
// provider app registers; HTTP is the seam tests point at a fake provider.
type Client struct {
	StateKey    []byte
	CallbackURL string
	HTTP        *http.Client
	Now         func() time.Time
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// defaultClient is the client for every credential-bearing call this package
// makes when a caller supplied no seam. It holds two guards at once.
//
// CheckRedirect: `http.DefaultClient` FOLLOWS redirects, and a POST body is
// replayed to the redirect target on a 307/308 — which for Revoke below means
// the refresh token itself would be re-sent to whatever host the provider
// named. A credential call has no legitimate reason to follow a hop: refuse,
// and let the caller see the redirect as the error it is.
//
// Transport: the endpoints are DECLARED ON DATA (a repository-applied bundle's
// oauth2-trait config), and the bundle loader admits a loopback-`http`
// tokenEndpoint, so the exchange is a repository-chosen server-side dial —
// issue #241's SSRF read primitive, since the oauth2 library quotes a token
// endpoint's response into its error. egress.Transport() confines every dial to
// public destinations at CONNECT time, on the resolved address (DNS-rebinding
// safe), with the operator's SUBSTRATE_EGRESS_ALLOW escape for a local test
// provider. Built per call so the allowlist is read fresh, not frozen at
// package init before a test's t.Setenv.
func defaultClient() *http.Client {
	return &http.Client{
		Transport: egress.Transport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("oauthflow: refusing to follow a redirect to %s on a credential-bearing request", req.URL.Host)
		},
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return defaultClient()
}

// config renders the oauth2 library's shape. The auth style is declared, not
// probed: a probed style consumes the single-use code on providers that
// reject the first guess (the connectors engine's slack lesson).
func (c *Client) config(ep Endpoints) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     ep.ClientID,
		ClientSecret: ep.ClientSecret,
		RedirectURL:  c.CallbackURL,
		Scopes:       ep.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:   ep.AuthURL,
			TokenURL:  ep.TokenURL,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
}

// AuthCodeURL builds the provider consent URL for one account record. The
// state is HMAC-signed: an unsigned state would let anyone bind their
// provider account to someone else's record. A non-empty verifier adds the
// PKCE S256 challenge, binding the returned code to the started flow.
func (c *Client) AuthCodeURL(ep Endpoints, st State, verifier string) (string, error) {
	if c.CallbackURL == "" {
		return "", errors.New("oauthflow: no callback URL configured")
	}
	st.Exp = c.now().Add(StateTTL).Unix()
	signed, err := c.signState(st)
	if err != nil {
		return "", err
	}
	opts := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	}
	if verifier != "" {
		opts = append(opts, oauth2.S256ChallengeOption(verifier))
	}
	return c.config(ep).AuthCodeURL(signed, opts...), nil
}

// NewVerifier mints a PKCE code verifier for one flow.
func NewVerifier() string { return oauth2.GenerateVerifier() }

func (c *Client) signState(st State) (string, error) {
	// An empty key would make every "signature" forgeable: refuse to mint
	// rather than hand out worthless states.
	if len(c.StateKey) == 0 {
		return "", errors.New("oauthflow: no state key configured")
	}
	payload, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, c.StateKey)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// VerifyState checks the signature and the expiry and returns the identity
// the flow was started for. It does NOT make the state one-time — the caller
// owns consuming the nonce.
func (c *Client) VerifyState(signed string) (State, error) {
	// The empty-key twin of signState's refusal: verification against an
	// empty key would accept anyone's HMAC arithmetic as authentication.
	if len(c.StateKey) == 0 {
		return State{}, ErrBadState
	}
	body, sig, ok := strings.Cut(signed, ".")
	if !ok {
		return State{}, ErrBadState
	}
	mac := hmac.New(sha256.New, c.StateKey)
	mac.Write([]byte(body))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return State{}, ErrBadState
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return State{}, ErrBadState
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, ErrBadState
	}
	if st.Username == "" || st.Record == "" || c.now().After(time.Unix(st.Exp, 0)) {
		return State{}, ErrBadState
	}
	return st, nil
}

// Exchange trades the consent code for a token at the declared endpoint,
// binding the redeemed code to the started flow with the PKCE verifier when
// one was issued at start.
func (c *Client) Exchange(ctx context.Context, ep Endpoints, code, verifier string) (*oauth2.Token, error) {
	var opts []oauth2.AuthCodeOption
	if verifier != "" {
		opts = append(opts, oauth2.VerifierOption(verifier))
	}
	tok, err := c.config(ep).Exchange(c.withHTTP(ctx), code, opts...)
	if err != nil {
		return nil, fmt.Errorf("oauthflow: exchange: %w", sanitizeTokenError(err))
	}
	return tok, nil
}

// Refresh trades a refresh token for a fresh access token. A response that
// omits the refresh token keeps the old one — providers rotate at will.
func (c *Client) Refresh(ctx context.Context, ep Endpoints, tok *oauth2.Token) (*oauth2.Token, error) {
	if tok == nil || tok.RefreshToken == "" {
		return nil, errors.New("oauthflow: no refresh token")
	}
	fresh, err := c.config(ep).TokenSource(c.withHTTP(ctx), &oauth2.Token{
		RefreshToken: tok.RefreshToken,
	}).Token()
	if err != nil {
		return nil, fmt.Errorf("oauthflow: refresh: %w", sanitizeTokenError(err))
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = tok.RefreshToken
	}
	return fresh, nil
}

// sanitizeTokenError bounds a token-endpoint failure to what is safe to
// carry: the HTTP status and the RFC 6749 error code, itself reduced to a
// short token-safe alphabet. The library's own error text includes the
// provider's `error_description` — and, for non-JSON failures, the raw
// response body — either of which can reflect request diagnostics (client
// credentials among them), so neither ever leaves this package.
func sanitizeTokenError(err error) error {
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) {
		// Transport errors (DNS, TLS, context) carry no provider body.
		return err
	}
	status := 0
	if re.Response != nil {
		status = re.Response.StatusCode
	}
	if code := sanitizeErrorCode(re.ErrorCode); code != "" {
		return fmt.Errorf("provider answered %d, error code %q", status, code)
	}
	return fmt.Errorf("provider answered %d", status)
}

// sanitizeErrorCode keeps only [a-z0-9_-] (case-folded), bounded to 40
// characters — enough for every RFC 6749 code, too little for a reflection.
func sanitizeErrorCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(code) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
		if b.Len() >= 40 {
			break
		}
	}
	return b.String()
}

// Revoke posts the token to the declared revocation endpoint. Best effort by
// design — a failed revoke must not strand a deletion (the connectors
// engine's google lesson); a missing endpoint is nothing to do.
func (c *Client) Revoke(ctx context.Context, revocationURL string, tok *oauth2.Token) error {
	if revocationURL == "" || tok == nil {
		return nil
	}
	token := tok.RefreshToken
	if token == "" {
		token = tok.AccessToken
	}
	if token == "" {
		return nil
	}
	body := strings.NewReader(url.Values{"token": {token}}.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, revocationURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	// Only a documented 2xx is a revocation. Anything else is reported — as a
	// BOUNDED error, status line only, never the body — so the best-effort
	// policy is observable in the caller's changelog instead of silently absorbing
	// a 401/500 as success.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("oauthflow: revoke: provider answered %d", resp.StatusCode)
	}
	return nil
}

// AccountEmail GETs the provider's account-info endpoint with a fresh access
// token and returns the connected account's email — the address the OAuth
// facility writes onto the account so the user never types it. Two response
// shapes are accepted: OIDC/userinfo `{"email": …}` and Google People
// `{"emailAddresses":[{"value":…,"metadata":{"primary":true}}]}` (a primary
// wins, else the first). Best-effort by contract: a non-2xx, an undecodable
// body, or a missing address is an error the caller logs and tolerates —
// connecting the account never fails because its email could not be derived.
func (c *Client) AccountEmail(ctx context.Context, endpoint, accessToken string) (string, error) {
	if endpoint == "" || accessToken == "" {
		return "", errors.New("oauthflow: account email: missing endpoint or token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("oauthflow: account email: provider answered %d", resp.StatusCode)
	}
	var body struct {
		Email          string `json:"email"`
		EmailAddresses []struct {
			Value    string `json:"value"`
			Metadata struct {
				Primary bool `json:"primary"`
			} `json:"metadata"`
		} `json:"emailAddresses"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body); err != nil {
		return "", fmt.Errorf("oauthflow: account email: decode: %w", err)
	}
	if body.Email != "" {
		return body.Email, nil
	}
	first := ""
	for _, e := range body.EmailAddresses {
		if e.Value == "" {
			continue
		}
		if e.Metadata.Primary {
			return e.Value, nil
		}
		if first == "" {
			first = e.Value
		}
	}
	if first != "" {
		return first, nil
	}
	return "", errors.New("oauthflow: account email: provider returned no address")
}

// withHTTP threads the client's HTTP seam into the oauth2 library.
func (c *Client) withHTTP(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, c.httpClient())
}
