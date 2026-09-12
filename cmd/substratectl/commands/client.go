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

// The core package and the kinds in it the CLI addresses by name. Every
// other kind is resolved from the registry, so these are the only literal
// names the client carries.
const (
	// corePackage is the package identity publishing the substrate's own
	// machinery kinds — the trigger/run vocabulary among them, so the trigger
	// records AND (a resource's operational verbs live at the resource, ruling
	// A8) the trigger delivery verbs hang off it. It is two segments, an
	// authority and a package, because a kind reference is three.
	corePackage = "substrate.reamde.dev/core"

	// nameKind is the registry kind's own name: the meta-kind is
	// substrate.reamde.dev/core/kind, self-describing the way a CRD is, and
	// the registry is the list of its records.
	nameKind = "kind"
)

// The repository-wide endpoints that name no kind sit at the version root, out
// of the kind namespace (decision 0033).
const (
	// pathRecords is the one records route: every list, ranked read and
	// kind-scoped tail is a GET here with the kind named INSIDE `filter`, and
	// a create under a server-assigned id is a POST here with the kind in the
	// body. A chosen id is a PUT at the record path instead.
	pathRecords    = apiPrefix + "/records"
	pathChanges    = apiPrefix + "/changes"
	pathVocabulary = apiPrefix + "/vocabulary/apply"
	// pathVocabularyPlan is the apply's preview: the same documents, nothing
	// written, the conversion plan answered (decision 0067).
	pathVocabularyPlan = apiPrefix + "/vocabulary/plan"
	pathOAuthStart     = apiPrefix + "/oauth/start"
	// pathExport is the owner's recovery export, a tar of the repository
	// directory (decision 0069).
	pathExport = apiPrefix + "/export"
)

// The door sits BESIDE the versioned API and outside every prefix:
// registration has no repository yet, and everything after it takes one from
// the token.
const (
	// pathDiscovery is GET /.well-known/substrate/server.json: unauthenticated,
	// repository-free, and the one thing the door reads before it asks a
	// person for anything.
	pathDiscovery      = "/.well-known/substrate/server.json"
	pathRegisterEnroll = "/register/enroll"
	pathRegister       = "/register"
	pathLogin          = "/login"
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

// recordPath is the kind reference AS a path, then the record id and any
// sub-resource after it: /api/v1/{authority}/{package}/{kind}/{id}[/…], where
// pkg is the package IDENTITY (`{authority}/{package}`, decision 0047) and so
// already carries its own separator. There is no shorter path: three segments
// name nothing on the server, every list goes through pathRecords. Every
// segment after the kind is escaped, so a record id carrying a slash (a
// declaration's id IS a kind reference) arrives percent-encoded rather than
// as more path segments.
func recordPath(pkg, kind string, id ...string) string {
	p := apiPrefix + "/" + pkg + "/" + kind
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
			Code       string   `json:"code"`
			Message    string   `json:"message"`
			Problems   []string `json:"problems"`
			Head       *int64   `json:"head"`
			Generation string   `json:"generation"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &env); err == nil && (env.Error.Code != "" || env.Error.Message != "") {
		ae.Code, ae.Message, ae.Problems = env.Error.Code, env.Error.Message, env.Error.Problems
		ae.Head, ae.Generation = env.Error.Head, env.Error.Generation
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

// list reads one page of a kind's records through the records route. The
// page is the server's whole shape, head and generation included, so a
// `get -w --from <head> --generation <generation>` after it resumes with
// neither a gap nor a double-see.
func (c *client) list(ctx context.Context, pkg, kind string, q url.Values) (*substrate.Page, error) {
	if err := setFilterKinds(q, pkg+"/"+kind); err != nil {
		return nil, err
	}
	var page substrate.Page
	if err := c.do(ctx, http.MethodGet, pathRecords, q, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// search is the ranked read: the query, its mode and the kinds it ranks over.
func (c *client) search(ctx context.Context, q url.Values) (*substrate.RankedPage, error) {
	var page substrate.RankedPage
	if err := c.do(ctx, http.MethodGet, pathRecords, q, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// setFilterKinds names the kinds a records read is scoped to, INSIDE the
// `filter` parameter: the route has no kind in its path, so the kind rides
// with the rest of the grammar. A filter the caller already built (--filter,
// -l) is kept and its `kinds` replaced, never appended to, because the read
// is one kind's by construction and a user-supplied kinds list would widen
// what `get <kind>` promised.
func setFilterKinds(q url.Values, kinds ...string) error {
	return editFilter(q, func(f *substrate.Filter) { f.Kinds = kinds })
}

// editFilter decodes the `filter` parameter q carries (none is the empty
// filter), lets edit change it and writes it back.
func editFilter(q url.Values, edit func(*substrate.Filter)) error {
	var f substrate.Filter
	if raw := q.Get("filter"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &f); err != nil {
			return fmt.Errorf("parse --filter as JSON: %w", err)
		}
	}
	edit(&f)
	b, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("encode filter: %w", err)
	}
	q.Set("filter", string(b))
	return nil
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

func (c *client) get(ctx context.Context, pkg, kind, id string) (*substrate.Record, map[string]statusProperty, error) {
	var e recordRead
	if err := c.do(ctx, http.MethodGet, recordPath(pkg, kind, id), nil, nil, &e); err != nil {
		return nil, nil, err
	}
	return &e.Record, e.PropertyMeta, nil
}

// put writes one record. With an id it is a PUT at the record path, whose
// URL fixes (kind, id) and the body needs neither; without one it is a POST
// at the records route, a create under a server-assigned id, so the body
// carries the kind reference and no id.
func (c *client) put(ctx context.Context, pkg, kind, id string, in substrate.PutInput) (*substrate.Record, error) {
	method, path := http.MethodPut, recordPath(pkg, kind, id)
	if id == "" {
		method, path = http.MethodPost, pathRecords
		in.Kind, in.ID = pkg+"/"+kind, ""
	}
	var e substrate.Record
	if err := c.do(ctx, method, path, nil, in, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (c *client) patch(ctx context.Context, pkg, kind, id string, in substrate.PatchInput) (*substrate.Record, error) {
	var e substrate.Record
	if err := c.do(ctx, http.MethodPatch, recordPath(pkg, kind, id), nil, in, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (c *client) delete(ctx context.Context, pkg, kind, id string) (*substrate.Record, error) {
	var e substrate.Record
	if err := c.do(ctx, http.MethodDelete, recordPath(pkg, kind, id), nil, nil, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// applyVocabulary sends a batch of schema documents to the one apply verb:
// every document admitted or none, one transaction, activation on commit. A
// confirmation, when given, is the consent to a lossy conversion plan the
// server previewed (planVocabulary), bound to that preview's hash and
// changelog head. An origin, when given, is the package a rehomed input was
// authored as, which the server stamps on the landed copy (decision record
// 0070).
func (c *client) applyVocabulary(ctx context.Context, docs []map[string]any, confirm *substrate.ConversionConfirm, origin string) ([]*substrate.Record, error) {
	var out struct {
		Records []*substrate.Record `json:"records"`
	}
	body := vocabularyBody(docs, origin)
	if confirm != nil {
		body["confirm"] = confirm
	}
	if err := c.do(ctx, http.MethodPost, pathVocabulary, nil, body, &out); err != nil {
		return nil, err
	}
	return out.Records, nil
}

// planVocabulary asks what applying the batch would refuse and rewrite,
// without applying it: the conversion steps with their counts, whether the
// plan is lossy or replaces an edited copy, and the hash and changelog head a
// confirmation names. The origin rides along so the preview hashes as the
// apply will.
func (c *client) planVocabulary(ctx context.Context, docs []map[string]any, origin string) (*substrate.VocabularyPlan, error) {
	var out substrate.VocabularyPlan
	if err := c.do(ctx, http.MethodPost, pathVocabularyPlan, nil, vocabularyBody(docs, origin), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// vocabularyBody is the two vocabulary verbs' shared request: the documents,
// and the origin only when there is one, so a plain apply is the body it
// always was.
func vocabularyBody(docs []map[string]any, origin string) map[string]any {
	body := map[string]any{"documents": docs}
	if origin != "" {
		body["origin"] = origin
	}
	return body
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
	Repository string `json:"repository"`
}

// registerRequest is the registration commit — the only call that creates
// anything.
type registerRequest struct {
	InviteCode string `json:"inviteCode"`
	// Repository is the name of the repository to create, which BECOMES its
	// authority: a bare label is completed under the substrate's own host.
	Repository string `json:"repository"`
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

// factors is both current factors presented directly, which is what a login
// authenticates with. A bearer token is never a substitute.
type factors struct {
	Repository string `json:"repository"`
	Password   string `json:"password"`
	TOTPCode   string `json:"totpCode"`
	Label      string `json:"label,omitempty"`
}

// tokenResult is what every mint answers with: the token record's metadata,
// and the secret shown exactly once.
type tokenResult struct {
	Token  substrate.TokenInfo `json:"token"`
	Secret string              `json:"secret"`
	// Repository is the repository the door resolved the request's name to.
	// Login and registration echo it; a `POST /tokens` mint does not.
	Repository string `json:"repository,omitempty"`
}

// discoveryDoc is the slice of GET /.well-known/substrate/server.json the
// door reads. The pointers are the point: a substrate that predates a field,
// and one that cannot be reached at all, must both read as "required" rather
// than as "no".
type discoveryDoc struct {
	Registration struct {
		InviteRequired *bool `json:"inviteRequired"`
		TOTPRequired   *bool `json:"totpRequired"`
	} `json:"registration"`
}

// doorPolicy is what the door asks a person for. Anything short of an
// explicit NO is a yes: prompting for a code the door wanted anyway costs a
// moment, while skipping one it wanted refuses the request. A local substrate
// is the only thing that answers no to either — no SUBSTRATE_INVITE_CODE for
// the invite, SUBSTRATE_INSECURE_DISABLE_TOTP for the second factor.
type doorPolicy struct {
	inviteRequired bool
	totpRequired   bool
}

// door reads the policy once; both answers ride one request.
func (c *client) door(ctx context.Context) doorPolicy {
	strict := doorPolicy{inviteRequired: true, totpRequired: true}
	var doc discoveryDoc
	if err := c.do(ctx, http.MethodGet, pathDiscovery, nil, nil, &doc); err != nil {
		return strict
	}
	reg := doc.Registration
	return doorPolicy{
		inviteRequired: reg.InviteRequired == nil || *reg.InviteRequired,
		totpRequired:   reg.TOTPRequired == nil || *reg.TOTPRequired,
	}
}

// totpRequired reports whether this deployment verifies a second factor.
func (c *client) totpRequired(ctx context.Context) bool {
	return c.door(ctx).totpRequired
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

func (c *client) login(ctx context.Context, in factors) (*tokenResult, error) {
	var out tokenResult
	if err := c.do(ctx, http.MethodPost, pathLogin, nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
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
	var out substrate.OperationalList[substrate.TokenInfo]
	if err := c.do(ctx, http.MethodGet, pathTokens, nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// revokeToken deletes the token record — which is the whole of revocation: no
// row means no access, and there is no revocation list to consult.
func (c *client) revokeToken(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, pathTokens+"/"+url.PathEscape(id), nil, nil, nil)
}
