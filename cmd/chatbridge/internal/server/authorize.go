package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Metadata is the part of the authorization server metadata (RFC 8414) the
// bridge reads.
type Metadata struct {
	Issuer                    string   `json:"issuer"`
	AuthorizationEndpoint     string   `json:"authorization_endpoint"`
	TokenEndpoint             string   `json:"token_endpoint"`
	AuthorizationDetailsTypes []string `json:"authorization_details_types_supported"`
	CodeChallengeMethods      []string `json:"code_challenge_methods_supported"`
}

// ClientID is the bridge's client id: a loopback one with no document, as
// a program on the owner's own computer has (the approval page says so).
const ClientID = "http://127.0.0.1/chatbridge"

// Scopes are what the package's function and the bridge reach: every kind
// of the package, and the instance kind by name, since a glob never reaches
// an internal kind.
var Scopes = []string{
	"write:" + Package + "/*",
	"write:" + KindInstance,
}

// Detail is one authorization detail (RFC 9396) as the server takes it.
type Detail struct {
	Type    string `json:"type"`
	Package string `json:"package,omitempty"`
	Version int    `json:"version,omitempty"`
	Trigger string `json:"trigger,omitempty"`
	URL     string `json:"url,omitempty"`
}

// Details are what the bridge's approval sets up: the package installed,
// and its webhook keyed and turned on.
func Details() []Detail {
	return []Detail{
		{Type: "package", Package: Package, Version: PackageVersion},
		{Type: "webhook", Trigger: Trigger},
	}
}

// Auth is the bridge's standing with one server, kept in the bridge store.
type Auth struct {
	Server     string     `json:"server"`
	Issuer     string     `json:"issuer"`
	Repository string     `json:"repository"`
	Token      string     `json:"token"`
	TokenID    string     `json:"tokenId,omitempty"`
	Scope      string     `json:"scope"`
	WebhookURL string     `json:"webhookUrl"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

// tokenResponse is the token endpoint's answer.
type tokenResponse struct {
	AccessToken string   `json:"access_token"`
	TokenType   string   `json:"token_type"`
	Scope       string   `json:"scope"`
	ExpiresIn   int64    `json:"expires_in"`
	Repository  string   `json:"repository"`
	TokenID     string   `json:"token_id"`
	Details     []Detail `json:"authorization_details"`
	Error       string   `json:"error"`
	ErrorDesc   string   `json:"error_description"`
}

// AuthorizeOptions configure one authorization.
type AuthorizeOptions struct {
	Server string
	// Listen is the loopback address the callback arrives on; port 0 picks
	// one. A fixed port is what a forwarded port needs.
	Listen string
	// SoftwareVersion is the bridge's version, which the token records.
	SoftwareVersion string
	// Show is handed the URL the owner opens.
	Show func(authorizeURL string)
	// Wait bounds the whole flow: the approval may set up packages first.
	Wait   time.Duration
	Client *http.Client
}

// Authorize runs the authorization code flow with PKCE on a loopback
// redirect and returns the bridge's new standing.
func Authorize(ctx context.Context, opts AuthorizeOptions) (*Auth, error) {
	if opts.Wait <= 0 {
		opts.Wait = 30 * time.Minute
	}
	if opts.Listen == "" {
		opts.Listen = "127.0.0.1:0"
	}
	// The redirect always names the loopback address; binding every
	// address is for a container whose port is published to the host's
	// loopback. The code is useless without the verifier, which stays here.
	host, _, err := net.SplitHostPort(opts.Listen)
	if err != nil || (host != "127.0.0.1" && host != "::1" && host != "localhost" && host != "0.0.0.0" && host != "::" && host != "") {
		return nil, fmt.Errorf("the callback listens on a loopback address, or every address in a container, not %q", opts.Listen)
	}
	c, err := NewClient(opts.Server, func() string { return "" }, opts.Client)
	if err != nil {
		return nil, err
	}
	meta, err := c.Discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the server's authorization metadata: %w", err)
	}
	for _, t := range []string{"package", "webhook"} {
		if !slices.Contains(meta.AuthorizationDetailsTypes, t) {
			return nil, fmt.Errorf("the server cannot set up %q for the bridge: it needs an update", t)
		}
	}
	if meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
		return nil, errors.New("the server's metadata names no authorization or token endpoint")
	}

	ln, err := net.Listen("tcp", opts.Listen)
	if err != nil {
		return nil, fmt.Errorf("listening for the callback: %w", err)
	}
	defer ln.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	verifier := randomToken(32)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := randomToken(16)
	details, err := json.Marshal(Details())
	if err != nil {
		return nil, err
	}
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {ClientID},
		"redirect_uri":          {redirect},
		"scope":                 {strings.Join(Scopes, " ")},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"authorization_details": {string(details)},
	}
	if opts.SoftwareVersion != "" {
		q.Set("software_version", opts.SoftwareVersion)
	}
	authURL := meta.AuthorizationEndpoint
	if strings.Contains(authURL, "?") {
		authURL += "&" + q.Encode()
	} else {
		authURL += "?" + q.Encode()
	}

	got := make(chan callbackResult, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		cb := readCallback(r.URL.Query(), state, meta.Issuer)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if cb.err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "<!doctype html><title>Chat bridge</title><p>The bridge was not authorized: %s</p>", html.EscapeString(cb.err.Error()))
		} else {
			fmt.Fprint(w, "<!doctype html><title>Chat bridge</title><p>The bridge is authorized. You can close this window.</p>")
		}
		select {
		case got <- cb:
		default:
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	if opts.Show != nil {
		opts.Show(authURL)
	}
	waitCtx, cancel := context.WithTimeout(ctx, opts.Wait)
	defer cancel()
	var cb callbackResult
	select {
	case cb = <-got:
	case <-waitCtx.Done():
		return nil, errors.New("no approval arrived in time; run chatbridge authorize again")
	}
	if cb.err != nil {
		return nil, cb.err
	}

	tok, err := exchange(ctx, opts.Client, meta.TokenEndpoint, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {cb.code},
		"client_id":     {ClientID},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	})
	if err != nil {
		return nil, err
	}
	auth := &Auth{
		Server:     strings.TrimRight(opts.Server, "/"),
		Issuer:     meta.Issuer,
		Repository: tok.Repository,
		Token:      tok.AccessToken,
		TokenID:    tok.TokenID,
		Scope:      tok.Scope,
	}
	if tok.ExpiresIn > 0 {
		at := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
		auth.ExpiresAt = &at
	}
	for _, d := range tok.Details {
		if d.Type == "webhook" && d.Trigger == Trigger {
			auth.WebhookURL = d.URL
		}
	}
	if auth.WebhookURL == "" {
		if auth.Repository == "" {
			return nil, errors.New("the token response names neither the repository nor the webhook")
		}
		auth.WebhookURL = strings.TrimRight(meta.Issuer, "/") + "/webhooks/" + url.PathEscape(auth.Repository) + "/" + Trigger
	}
	return auth, nil
}

type callbackResult struct {
	code string
	err  error
}

func readCallback(q url.Values, state, issuer string) callbackResult {
	if q.Get("state") != state {
		return callbackResult{err: errors.New("the callback came back for another request")}
	}
	if iss := q.Get("iss"); iss != "" && issuer != "" && iss != issuer {
		return callbackResult{err: fmt.Errorf("the callback came from %s, not %s", iss, issuer)}
	}
	if e := q.Get("error"); e != "" {
		if e == "access_denied" {
			return callbackResult{err: errors.New("the owner denied the request")}
		}
		return callbackResult{err: fmt.Errorf("the server refused the request: %s", e)}
	}
	code := q.Get("code")
	if code == "" {
		return callbackResult{err: errors.New("the callback carried no code")}
	}
	return callbackResult{code: code}
}

func exchange(ctx context.Context, hc *http.Client, endpoint string, form url.Values) (*tokenResponse, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, redact(err)
	}
	defer resp.Body.Close()
	var tok tokenResponse
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("the token endpoint answered %d with no token", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || tok.AccessToken == "" {
		if tok.Error != "" {
			return nil, fmt.Errorf("the token endpoint refused the code: %s %s", tok.Error, tok.ErrorDesc)
		}
		return nil, fmt.Errorf("the token endpoint answered %d", resp.StatusCode)
	}
	return &tok, nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
