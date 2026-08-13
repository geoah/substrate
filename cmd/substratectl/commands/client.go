package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

const apiPrefix = "/api/v1"

// The core authority and the collections in it the CLI addresses by name. Every
// other collection is resolved from the registry, so these are the only
// literal names the client carries.
//
// The registry collection is `kinds`: the meta-kind is `core.substrate.reamde.dev/kind`,
// self-describing the way a CRD is, so its collection needs no prefix.
const (
	// coreAuthority publishes the substrate's own machinery kinds — the
	// trigger/run vocabulary among them, so the trigger records AND (a
	// resource's operational verbs live at the resource, ruling A8) the
	// trigger delivery verbs hang off it.
	coreAuthority = "core.substrate.reamde.dev"

	pluralKinds   = "kinds"
	pluralChanges = "changes"
)

// The door sits BESIDE the versioned API and outside every prefix:
// registration has no repository yet, and everything after it takes one from
// the token.
const (
	pathRegisterEnroll = "/register/enroll"
	pathRegister       = "/register"
	pathLogin          = "/login"
	pathPassword       = "/password"
	pathRecoveryEnroll = "/recovery/enroll"
	pathTOTPEnroll     = "/totp/enroll"
	pathTOTP           = "/totp"
	pathTokens         = "/tokens"
)

// client is the REST client for one substrate context.
type client struct {
	server string
	token  string
	actor  string
	hc     *http.Client
}

func newClient(server, token string, hc *http.Client) *client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &client{server: strings.TrimRight(server, "/"), token: token, hc: hc}
}

// collectionPath is the kind reference AS a path:
// /api/v1/{authority}/{plural} for a published kind, /api/v1/{plural} for a
// repository-local one — the same two forms the reference itself has. Every id
// segment is escaped, so a record id carrying a slash arrives as %2F rather
// than as another path segment.
func collectionPath(authority, plural string, id ...string) string {
	p := apiPrefix
	if authority != "" {
		p += "/" + authority
	}
	p += "/" + plural
	for _, seg := range id {
		p += "/" + url.PathEscape(seg)
	}
	return p
}

func (c *client) newRequest(ctx context.Context, method, path string, q url.Values, body any) (*http.Request, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	u := c.server + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.actor != "" {
		req.Header.Set("X-Substrate-Actor", c.actor)
	}
	return req, nil
}

// send performs the request and returns the live response for 2xx, or an
// *apiError parsed from the envelope.
func (c *client) send(ctx context.Context, method, path string, q url.Values, body any) (*http.Response, error) {
	req, err := c.newRequest(ctx, method, path, q, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	return nil, parseAPIError(resp, method, path)
}

func (c *client) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	resp, err := c.send(ctx, method, path, q, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	// UseNumber: a property map is `map[string]any`, and the default decode
	// turns every number in one into a float64 — which prints an int64 back out
	// as `5.9545831e+07` and loses digits outright past 2^53. The wire number is
	// kept verbatim here and typed once, at the render edge (normalizeNumbers).
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode response from %s %s: %w", method, path, err)
	}
	return nil
}

func parseAPIError(resp *http.Response, method, path string) *apiError {
	ae := &apiError{
		Status:     resp.StatusCode,
		RetryAfter: resp.Header.Get("Retry-After"),
		Method:     method,
		Path:       path,
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var env struct {
		Error struct {
			Code     string   `json:"code"`
			Message  string   `json:"message"`
			Problems []string `json:"problems"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &env); err == nil && (env.Error.Code != "" || env.Error.Message != "") {
		ae.Code, ae.Message, ae.Problems = env.Error.Code, env.Error.Message, env.Error.Problems
		return ae
	}
	if msg := strings.TrimSpace(string(b)); msg != "" {
		ae.Message = truncate(msg, 400)
	}
	if ae.Code == "" {
		ae.Code = codeForStatus(resp.StatusCode)
	}
	return ae
}

func codeForStatus(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "auth"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnprocessableEntity:
		return "validation"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusGone:
		return "compacted"
	case http.StatusNotImplemented:
		return "unsupported"
	case http.StatusServiceUnavailable:
		return "unavailable"
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- typed calls ---

type recordPage struct {
	Records []*substrate.Record `json:"records"`
	Cursor  string              `json:"cursor,omitempty"`
}

func (c *client) list(ctx context.Context, authority, plural string, q url.Values) (*recordPage, error) {
	var page recordPage
	if err := c.do(ctx, http.MethodGet, collectionPath(authority, plural), q, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// recordRead is one GET's flat JSON: the record plus `propertyMeta`, the
// managed-property block a single-record read carries and a list never does.
// Decoded here beside the record rather than through substrate.Record, because
// the meta is a read-view fact the CLI renders into `status.properties` and
// nothing the write path ever sends.
type recordRead struct {
	substrate.Record
	PropertyMeta map[string]statusProperty `json:"propertyMeta"`
}

func (c *client) get(ctx context.Context, authority, plural, id string) (*substrate.Record, map[string]statusProperty, error) {
	var e recordRead
	if err := c.do(ctx, http.MethodGet, collectionPath(authority, plural, id), nil, nil, &e); err != nil {
		return nil, nil, err
	}
	return &e.Record, e.PropertyMeta, nil
}

func (c *client) put(ctx context.Context, authority, plural, id string, in substrate.PutInput) (*substrate.Record, error) {
	method, path := http.MethodPost, collectionPath(authority, plural)
	if id != "" {
		method, path = http.MethodPut, collectionPath(authority, plural, id)
	}
	var e substrate.Record
	if err := c.do(ctx, method, path, nil, in, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (c *client) patch(ctx context.Context, authority, plural, id string, in substrate.PatchInput) (*substrate.Record, error) {
	var e substrate.Record
	if err := c.do(ctx, http.MethodPatch, collectionPath(authority, plural, id), nil, in, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (c *client) delete(ctx context.Context, authority, plural, id string) (*substrate.Record, error) {
	var e substrate.Record
	if err := c.do(ctx, http.MethodDelete, collectionPath(authority, plural, id), nil, nil, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// link adds one outgoing edge to the source record via the resource's edge
// verb (POST …/{id}/edges/{rel}); the refreshed source record comes back.
func (c *client) link(ctx context.Context, authority, plural, id, rel string, to substrate.EdgeRef, props map[string]any) (*substrate.Record, error) {
	body := struct {
		substrate.EdgeRef
		Properties map[string]any `json:"properties,omitempty"`
	}{EdgeRef: to, Properties: props}
	var e substrate.Record
	if err := c.do(ctx, http.MethodPost, collectionPath(authority, plural, id, "edges", rel), nil, body, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// unlink removes one outgoing edge from the source record (DELETE
// …/{id}/edges/{rel}); the refreshed source record comes back.
func (c *client) unlink(ctx context.Context, authority, plural, id, rel string, to substrate.EdgeRef) (*substrate.Record, error) {
	var e substrate.Record
	if err := c.do(ctx, http.MethodDelete, collectionPath(authority, plural, id, "edges", rel), nil, to, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// applyVocabulary sends a batch of schema documents to the one apply verb:
// every document admitted or none, one transaction, activation on commit.
func (c *client) applyVocabulary(ctx context.Context, docs []map[string]any) ([]*substrate.Record, error) {
	var out struct {
		Records []*substrate.Record `json:"records"`
	}
	body := map[string]any{"documents": docs}
	if err := c.do(ctx, http.MethodPost, apiPrefix+"/"+coreAuthority+"/vocabulary/apply", nil, body, &out); err != nil {
		return nil, err
	}
	return out.Records, nil
}

// --- the door: register, login, the credential changes, tokens -------------
//
// These are the wire shapes of `api/auth_endpoints.go`, spelled here because
// they are a CONTRACT rather than a shared type: the CLI is a client of the
// HTTP surface, not of the server package.

// registerBeginRequest asks for a TOTP enrollment. It writes nothing on the
// server: the caller holds the seed and hands it back with one code.
type registerBeginRequest struct {
	InviteCode string `json:"inviteCode"`
	Username   string `json:"username"`
}

// registerRequest is the registration commit — the only call that creates
// anything.
type registerRequest struct {
	InviteCode string `json:"inviteCode"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totpSecret"`
	TOTPCode   string `json:"totpCode"`
	Label      string `json:"label,omitempty"`
	// RecoveryPublicKey is the age recipient generated CLIENT-SIDE, so the
	// matching identity never rides the wire.
	RecoveryPublicKey string `json:"recoveryPublicKey,omitempty"`
}

// registerResult is a tokenResult plus the recovery half: the enrolled
// recipient, and the identity ONLY when the server minted the pair.
type registerResult struct {
	tokenResult
	RecoveryKey       string `json:"recoveryKey,omitempty"`
	RecoveryPublicKey string `json:"recoveryPublicKey,omitempty"`
}

type recoveryEnrollRequest struct {
	Username          string `json:"username"`
	Password          string `json:"password"`
	TOTPCode          string `json:"totpCode"`
	RecoveryPublicKey string `json:"recoveryPublicKey,omitempty"`
}

type recoveryEnrollResult struct {
	RecoveryKey       string `json:"recoveryKey,omitempty"`
	RecoveryPublicKey string `json:"recoveryPublicKey"`
}

// factors is both current factors presented directly: it authenticates a
// login, and it is the password-factor rule's evidence on every endpoint that
// changes auth material. A bearer token is never a substitute.
type factors struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTPCode string `json:"totpCode"`
	Label    string `json:"label,omitempty"`
}

type passwordRequest struct {
	factors
	NewPassword string `json:"newPassword"`
}

type totpRequest struct {
	factors
	NewTOTPSecret string `json:"newTotpSecret"`
	NewTOTPCode   string `json:"newTotpCode"`
}

// tokenResult is what every mint answers with: the token record's metadata,
// and the secret shown exactly once.
type tokenResult struct {
	Token  substrate.TokenInfo `json:"token"`
	Secret string              `json:"secret"`
}

func (c *client) registerEnroll(ctx context.Context, in registerBeginRequest) (*substrate.TOTPEnrollment, error) {
	var out substrate.TOTPEnrollment
	if err := c.do(ctx, http.MethodPost, pathRegisterEnroll, nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) register(ctx context.Context, in registerRequest) (*registerResult, error) {
	var out registerResult
	if err := c.do(ctx, http.MethodPost, pathRegister, nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) recoveryEnroll(ctx context.Context, in recoveryEnrollRequest) (*recoveryEnrollResult, error) {
	var out recoveryEnrollResult
	if err := c.do(ctx, http.MethodPost, pathRecoveryEnroll, nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) login(ctx context.Context, in factors) (*tokenResult, error) {
	var out tokenResult
	if err := c.do(ctx, http.MethodPost, pathLogin, nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) changePassword(ctx context.Context, in passwordRequest) error {
	return c.do(ctx, http.MethodPost, pathPassword, nil, in, nil)
}

func (c *client) totpEnroll(ctx context.Context, in factors) (*substrate.TOTPEnrollment, error) {
	var out substrate.TOTPEnrollment
	if err := c.do(ctx, http.MethodPost, pathTOTPEnroll, nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) reenrollTOTP(ctx context.Context, in totpRequest) error {
	return c.do(ctx, http.MethodPost, pathTOTP, nil, in, nil)
}

// mintToken is login's authenticated twin: the same token record, the same
// secret shown once, a different door.
func (c *client) mintToken(ctx context.Context, label string, expiresAt *time.Time) (*tokenResult, error) {
	body := struct {
		Label     string     `json:"label"`
		ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	}{Label: label, ExpiresAt: expiresAt}
	var out tokenResult
	if err := c.do(ctx, http.MethodPost, pathTokens, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) tokens(ctx context.Context) ([]substrate.TokenInfo, error) {
	var out struct {
		Tokens []substrate.TokenInfo `json:"tokens"`
	}
	if err := c.do(ctx, http.MethodGet, pathTokens, nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Tokens, nil
}

// revokeToken deletes the token record — which is the whole of revocation: no
// row means no access, and there is no revocation list to consult.
func (c *client) revokeToken(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, pathTokens+"/"+url.PathEscape(id), nil, nil, nil)
}
