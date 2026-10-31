package api

// APP AUTHORIZATION (decision 0194): an app that registered nowhere sends the
// owner to GET /oauth/authorize, the owner approves on the console's
// /authorize/{request} page, and the app exchanges the code at
// POST /oauth/token for a token that reaches only what the owner approved.
// The protocol pieces (client ids, redirects, the document fetch, PKCE, the
// request and code lifecycle) are internal/authorize; the scope grammar is
// internal/scope. The outbound provider flow at /api/v1/oauth/* shares
// nothing with this one.

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

const (
	asMetadataPath = "/.well-known/oauth-authorization-server"
	prMetadataPath = "/.well-known/oauth-protected-resource"
	// approvalPage is the console route a parked request is answered with.
	approvalPage = "/authorize/"
	// maxOAuthForm bounds a token or revocation body: a handful of short
	// parameters.
	maxOAuthForm = 8 << 10
	// maxState bounds the state an app round-trips through the server.
	maxState = 1024
	// defaultLifetimeDays is the lifetime the approval page selects first.
	defaultLifetimeDays = 30
)

// lifetimeDays are the lifetimes an owner may give an app's token. There is
// no refresh token, so the longest is the longest an app keeps access
// without the owner seeing the approval page again.
var lifetimeDays = []int{7, 30, 90}

// issuerOf is the issuer a request reached. A configured public URL
// (SUBSTRATE_PUBLIC_URL) is the issuer whatever host the request named.
// Without one it is the origin of the request's Host: http on a loopback
// host (a dev box speaks plain HTTP and nothing else can reach it), https on
// any other, since the server never terminates TLS and a deployment anybody
// else reaches sits behind it. The metadata, the `iss` parameter and the 401
// challenge all derive it here, and a flow is bound to the issuer it began
// on.
func (h *handler) issuerOf(r *http.Request) string {
	if h.publicURL != "" {
		return h.publicURL
	}
	host := r.Host
	name := host
	if hn, _, err := net.SplitHostPort(host); err == nil {
		name = hn
	}
	name = strings.Trim(name, "[]")
	if ip := net.ParseIP(name); strings.EqualFold(name, "localhost") || (ip != nil && ip.IsLoopback()) {
		return "http://" + host
	}
	return "https://" + host
}

// sameResource reports whether an RFC 8707 `resource` names this server: one
// server is one protected resource, identified by its issuer.
func sameResource(resource, issuer string) bool {
	return strings.TrimSuffix(resource, "/") == issuer
}

// validation is a refusal the approval page shows the owner as it is.
func validation(msg string) error {
	return fmt.Errorf("%w: %s", substrate.ErrValidation, msg)
}

// clientDocuments reads an app's metadata document. The server's is
// authorize.Fetcher; a test substitutes one that serves documents from
// memory, since the real one never dials a loopback address.
type clientDocuments interface {
	Fetch(ctx context.Context, c authorize.Client) (authorize.Document, error)
}

// --- metadata ---

type authorizationServerMetadata struct {
	Issuer                                 string   `json:"issuer"`
	AuthorizationEndpoint                  string   `json:"authorization_endpoint"`
	TokenEndpoint                          string   `json:"token_endpoint"`
	RevocationEndpoint                     string   `json:"revocation_endpoint"`
	ResponseTypesSupported                 []string `json:"response_types_supported"`
	ResponseModesSupported                 []string `json:"response_modes_supported"`
	GrantTypesSupported                    []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported          []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported      []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported []string `json:"revocation_endpoint_auth_methods_supported"`
	ScopesSupported                        []string `json:"scopes_supported"`
	AuthorizationResponseISSSupported      bool     `json:"authorization_response_iss_parameter_supported"`
	ClientIDMetadataDocumentSupported      bool     `json:"client_id_metadata_document_supported"`
}

type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ScopesSupported        []string `json:"scopes_supported"`
	ResourceName           string   `json:"resource_name"`
}

// scopesSupported names the two widest scopes: every other scope is a
// narrower pattern of one of them, which no list could enumerate.
var scopesSupported = []string{"read:*", "write:*"}

func (h *handler) getAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	iss := h.issuerOf(r)
	writeJSON(w, http.StatusOK, authorizationServerMetadata{
		Issuer:                                 iss,
		AuthorizationEndpoint:                  iss + "/oauth/authorize",
		TokenEndpoint:                          iss + "/oauth/token",
		RevocationEndpoint:                     iss + "/oauth/revoke",
		ResponseTypesSupported:                 []string{"code"},
		ResponseModesSupported:                 []string{"query"},
		GrantTypesSupported:                    []string{"authorization_code"},
		CodeChallengeMethodsSupported:          []string{"S256"},
		TokenEndpointAuthMethodsSupported:      []string{"none"},
		RevocationEndpointAuthMethodsSupported: []string{"none"},
		ScopesSupported:                        scopesSupported,
		AuthorizationResponseISSSupported:      true,
		ClientIDMetadataDocumentSupported:      true,
	})
}

func (h *handler) getProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	iss := h.issuerOf(r)
	writeJSON(w, http.StatusOK, protectedResourceMetadata{
		Resource:               iss,
		AuthorizationServers:   []string{iss},
		BearerMethodsSupported: []string{"header"},
		ScopesSupported:        scopesSupported,
		ResourceName:           "substrate",
	})
}

// --- the authorization endpoint ---

// getAuthorize checks an authorization request's shape and parks it. It
// fetches nothing and redirects nowhere but the console: until the owner is
// signed in and the app's document is read, the redirect_uri is unverified,
// so every refusal here is a page (OAuth 2.1 §4.1.2.1).
func (h *handler) getAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for k, v := range q {
		if len(v) > 1 {
			writeAuthorizePage(w, http.StatusBadRequest, "the parameter "+k+" appears more than once")
			return
		}
	}
	client, err := authorize.ParseClientID(q.Get("client_id"))
	if err != nil {
		writeAuthorizePage(w, http.StatusBadRequest, err.Error())
		return
	}
	redirect := q.Get("redirect_uri")
	if _, err := authorize.ParseRedirect(redirect); err != nil {
		writeAuthorizePage(w, http.StatusBadRequest, err.Error())
		return
	}
	if client.Local && !authorize.LocalRedirect(redirect) {
		writeAuthorizePage(w, http.StatusBadRequest, "a local app's redirect_uri is a loopback http URL")
		return
	}
	if rt := q.Get("response_type"); rt != "code" {
		writeAuthorizePage(w, http.StatusBadRequest, "response_type must be code")
		return
	}
	if q.Get("code_challenge_method") != "S256" {
		writeAuthorizePage(w, http.StatusBadRequest, "code_challenge_method must be S256: PKCE is required")
		return
	}
	if !authorize.ValidChallenge(q.Get("code_challenge")) {
		writeAuthorizePage(w, http.StatusBadRequest, "code_challenge must be an S256 challenge: 43 base64url characters")
		return
	}
	scopes, err := scope.ParseRequest(q.Get("scope"))
	if err != nil {
		writeAuthorizePage(w, http.StatusBadRequest, err.Error())
		return
	}
	iss := h.issuerOf(r)
	if res := q.Get("resource"); res != "" && !sameResource(res, iss) {
		writeAuthorizePage(w, http.StatusBadRequest, "resource must name this server, "+iss)
		return
	}
	state := q.Get("state")
	if len(state) > maxState {
		writeAuthorizePage(w, http.StatusBadRequest, "state is too long")
		return
	}
	source := sourceOf(r)
	if ok, _ := h.parkRate.allow(costPaired, source+"|authorize"); !ok {
		writeAuthorizePage(w, http.StatusTooManyRequests, "too many authorization requests from this address; wait a moment and try again")
		return
	}
	req, err := h.authz.Park(authorize.Request{
		Issuer: iss, Client: client, RedirectURI: redirect, State: state,
		Challenge: q.Get("code_challenge"), Scopes: scopes, Source: source,
	})
	if err != nil {
		writeAuthorizePage(w, http.StatusServiceUnavailable, "the server is holding too many authorization requests; try again shortly")
		return
	}
	http.Redirect(w, r, h.consoleURL+approvalPage+req.ID, http.StatusSeeOther)
}

var authorizePage = template.Must(template.New("authorize").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Authorization refused</title>
<meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body style="font-family: system-ui, sans-serif; max-width: 36rem; margin: 4rem auto; padding: 0 1rem; line-height: 1.5">
<h1 style="font-size: 1.25rem">This authorization request cannot be used</h1>
<p>{{.}}.</p>
<p>Nothing was sent to the app. Go back to the app and start again; if it keeps happening, the app is sending a request this server does not accept.</p>
</body></html>`))

// writeAuthorizePage answers a refused authorization request with a page
// for the person in the browser, never a redirect.
func writeAuthorizePage(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = authorizePage.Execute(w, msg)
}

// sourceOf is who a parked request is charged to: the peer's IPv4 address,
// or the /64 an IPv6 peer sits in, since one host holds a whole /64 and
// would otherwise be as many sources as it has addresses.
func sourceOf(r *http.Request) string {
	ip := peerIP(r)
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is6() {
		if p, err := addr.Prefix(64); err == nil {
			return p.String()
		}
	}
	return addr.String()
}

// --- the approval page's calls ---

// errAuthorizationGone answers a request that lapsed, was answered, or never
// existed, all alike.
func writeAuthorizationGone(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, codeNotFound,
		"this authorization request has expired or was already answered: start again from the app")
}

// verifiedApp reads a pending request's app: a local app as its URL alone, a
// web app through its document, whose redirect_uris must list the request's
// redirect. A refusal is the owner's to read and names no address.
func (h *handler) verifiedApp(ctx context.Context, req authorize.Request) (authorize.Document, error) {
	if req.Client.Local {
		return authorize.Document{ClientID: req.Client.ID}, nil
	}
	doc, err := h.fetcher.Fetch(ctx, req.Client)
	if err != nil {
		var fe *authorize.FetchError
		if errors.As(err, &fe) {
			return authorize.Document{}, validation(fe.Error())
		}
		return authorize.Document{}, err
	}
	if !authorize.RedirectListed(doc.RedirectURIs, req.RedirectURI) {
		return authorize.Document{}, validation(
			"the app's document does not list the redirect_uri this request names, " + req.RedirectURI)
	}
	return doc, nil
}

func (h *handler) getAuthorization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ds := DatasetFrom(ctx)
	authority := ds.Repository().Authority
	id := pathParam(r, "request")
	req, err := h.authz.Claim(id, authority)
	if err != nil {
		writeAuthorizationGone(w)
		return
	}
	// What the page shows is what an approval or a denial is checked
	// against: the first snapshot, never a later fetch, so a request read
	// twice shows the same document both times.
	doc := authorize.Document{}
	if req.Document != nil {
		doc = *req.Document
	} else {
		if doc, err = h.verifiedApp(ctx, req); err != nil {
			writeSubstrateError(w, r, err)
			return
		}
		if doc, err = h.authz.Snapshot(id, authority, doc); err != nil {
			writeAuthorizationGone(w)
			return
		}
	}
	kinds, err := ds.Kinds(ctx)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	view := substrate.AuthorizationRequest{
		ID: req.ID,
		App: substrate.AuthorizationApp{
			ClientID: req.Client.ID, Host: req.Client.Host, Local: req.Client.Local,
			Name: doc.Name, URI: doc.URI,
			URIHostDiffers: doc.URI != "" && hostname(doc.URI) != hostname(req.Client.ID),
		},
		Redirect: substrate.AuthorizationRedirectTo{
			URI: req.RedirectURI, Host: authorize.HostOf(req.RedirectURI),
			HostDiffers: hostname(req.RedirectURI) != hostname(req.Client.ID),
		},
		Repository:          authority,
		LifetimeDays:        lifetimeDays,
		DefaultLifetimeDays: defaultLifetimeDays,
		ExpiresAt:           req.Expires,
	}
	seen := map[string]bool{}
	for _, s := range req.Scopes {
		resolved := s.Resolve(authority)
		// `read:~/tasks/*` and `read:<authority>/tasks/*` are one scope once
		// resolved, and the page lists it once.
		if seen[resolved.String()] {
			continue
		}
		seen[resolved.String()] = true
		view.Scopes = append(view.Scopes, substrate.AuthorizationScope{
			Scope: resolved.String(), Requested: s.String(), Write: resolved.Write,
			Pattern: resolved.Pattern, Glob: strings.HasSuffix(resolved.Pattern, "*"),
			Kinds: reachedKinds(resolved, kinds),
		})
	}
	writeJSON(w, http.StatusOK, view)
}

// shownApp is the document the approval page showed for a request, which an
// approval and a denial are held to: a request the page never read cannot be
// answered, and its redirect must still be the one the snapshot lists.
func shownApp(req authorize.Request) (authorize.Document, error) {
	if req.Document == nil {
		return authorize.Document{}, validation("the approval page has not shown this request yet: load it, then answer")
	}
	if !req.Client.Local && !authorize.RedirectListed(req.Document.RedirectURIs, req.RedirectURI) {
		return authorize.Document{}, validation("the app's document does not list the redirect_uri this request names, " + req.RedirectURI)
	}
	return *req.Document, nil
}

// reachedKinds lists the kinds one resolved scope reaches today.
func reachedKinds(s scope.Scope, kinds []substrate.KindInfo) []substrate.AuthorizationKind {
	g, err := scope.NewGrant([]string{s.String()})
	out := []substrate.AuthorizationKind{}
	if err != nil {
		return out
	}
	for _, k := range kinds {
		ok := g.Reads(k.Identity, internalKind(k))
		if s.Write {
			ok = g.Writes(k.Identity, internalKind(k))
		}
		if ok {
			out = append(out, substrate.AuthorizationKind{Identity: k.Identity, Label: k.Label})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity < out[j].Identity })
	return out
}

// hostname is a URL's host without its port, "" when it does not parse.
func hostname(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func (h *handler) postAuthorization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in substrate.AuthorizationApproval
	if !readJSON(w, r, &in, jsonBody) {
		return
	}
	ds := DatasetFrom(ctx)
	authority := ds.Repository().Authority
	code, req, err := h.authz.Approve(pathParam(r, "request"), authority, func(req authorize.Request) (authorize.Approval, error) {
		doc, err := shownApp(req)
		if err != nil {
			return authorize.Approval{}, err
		}
		scopes, err := approvedScopes(req, authority, in.Scopes)
		if err != nil {
			return authorize.Approval{}, err
		}
		if !slices.Contains(lifetimeDays, in.LifetimeDays) {
			return authorize.Approval{}, validation("lifetimeDays must be one of 7, 30 or 90")
		}
		return authorize.Approval{
			Repository: authority, Dataset: ds, Scopes: scopes,
			Lifetime: time.Duration(in.LifetimeDays) * 24 * time.Hour, ClientName: doc.Name,
		}, nil
	})
	if errors.Is(err, authorize.ErrNotPending) {
		writeAuthorizationGone(w)
		return
	}
	if errors.Is(err, authorize.ErrFull) {
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, "the server is holding too many authorization codes; try again shortly")
		return
	}
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.AuthorizationAnswer{
		Redirect: withParams(req.RedirectURI, url.Values{"code": {code}}, req),
	})
}

// approvedScopes holds an approval to the request: a non-empty subset of
// the request's scopes, resolved against the approving repository, each
// once, in the request's order.
func approvedScopes(req authorize.Request, authority string, approved []string) ([]string, error) {
	if len(approved) == 0 {
		return nil, validation("an approval names at least one scope: to grant nothing, deny")
	}
	want := map[string]bool{}
	for _, s := range approved {
		want[s] = true
	}
	var out []string
	for _, s := range req.Scopes {
		resolved := s.Resolve(authority).String()
		if want[resolved] {
			out = append(out, resolved)
			delete(want, resolved)
		}
	}
	if len(want) > 0 {
		var extra []string
		for s := range want {
			extra = append(extra, s)
		}
		sort.Strings(extra)
		return nil, validation("the approval names scopes the app did not request: " + strings.Join(extra, ", "))
	}
	if err := scope.CheckStored(out); err != nil {
		return nil, validation(err.Error())
	}
	return out, nil
}

func (h *handler) deleteAuthorization(w http.ResponseWriter, r *http.Request) {
	authority := DatasetFrom(r.Context()).Repository().Authority
	// A denial navigates to the app too, so its redirect is held to the
	// snapshot the page showed, like an approval's.
	req, err := h.authz.Deny(pathParam(r, "request"), authority, func(req authorize.Request) error {
		_, err := shownApp(req)
		return err
	})
	if errors.Is(err, authorize.ErrNotPending) {
		writeAuthorizationGone(w)
		return
	}
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.AuthorizationAnswer{
		Redirect: withParams(req.RedirectURI, url.Values{
			"error":             {"access_denied"},
			"error_description": {"the owner denied the request"},
		}, req),
	})
}

// withParams appends the answer's parameters, the request's state and the
// issuer (RFC 9207) to the redirect URI's query, leaving what it already
// carries as it was.
func withParams(redirect string, v url.Values, req authorize.Request) string {
	if req.State != "" {
		v.Set("state", req.State)
	}
	v.Set("iss", req.Issuer)
	sep := "?"
	if strings.Contains(redirect, "?") {
		sep = "&"
		if strings.HasSuffix(redirect, "?") || strings.HasSuffix(redirect, "&") {
			sep = ""
		}
	}
	return redirect + sep + v.Encode()
}

// --- the token and revocation endpoints ---

// oauthError is RFC 6749 §5.2's error answer.
type oauthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, oauthError{Error: code, Description: desc})
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	ExpiresIn   int64  `json:"expires_in"`
	// Repository is the authority the token opens, as IndieAuth's `me`
	// tells an app whom it signed in as.
	Repository string `json:"repository"`
}

// readOAuthForm reads a bounded form body with each parameter once. It
// answers false after writing the error.
func readOAuthForm(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/x-www-form-urlencoded" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "the body must be application/x-www-form-urlencoded")
		return nil, false
	}
	if r.URL.RawQuery != "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "parameters go in the body, never the query")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxOAuthForm)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "the body is not a form this server reads")
		return nil, false
	}
	for k, v := range r.PostForm {
		if len(v) > 1 {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "the parameter "+k+" appears more than once")
			return nil, false
		}
	}
	return r.PostForm, true
}

// postOAuthToken exchanges a code for a token. It has no pace of its own: a
// code is 256 random bits and the verifier is the app's secret, so guessing
// gains nothing, and behind a proxy a pace keyed on the peer would let one
// client starve every app's exchange. A presentation that does not
// prove every binding and the verifier is refused and spends nothing; the
// first that does redeems the code; a later one that does is a replay of a
// code that worked, and revokes what the first exchange minted (OAuth 2.1
// §4.1.3). A mint that fails leaves the code spent: the app starts over.
func (h *handler) postOAuthToken(w http.ResponseWriter, r *http.Request) {
	form, ok := readOAuthForm(w, r)
	if !ok {
		return
	}
	if gt := form.Get("grant_type"); gt != "authorization_code" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
		return
	}
	code, clientID, redirect, verifier := form.Get("code"), form.Get("client_id"), form.Get("redirect_uri"), form.Get("code_verifier")
	if code == "" || clientID == "" || redirect == "" || verifier == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code, client_id, redirect_uri and code_verifier are required")
		return
	}
	if !authorize.ValidVerifier(verifier) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code_verifier is not 43 to 128 characters of the PKCE alphabet")
		return
	}
	iss := h.issuerOf(r)
	resource := form.Get("resource")
	if resource != "" && !sameResource(resource, iss) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource must name this server, "+iss)
		return
	}
	c, first, err := h.authz.Redeem(code, func(c authorize.Code) error {
		if c.Request.Issuer != iss || c.Request.Client.ID != clientID || c.Request.RedirectURI != redirect ||
			!authorize.VerifyS256(verifier, c.Request.Challenge) {
			return authorize.ErrBadCode
		}
		return nil
	})
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant",
			"the code is unknown, expired, or was not issued to this client, redirect_uri and code_verifier")
		return
	}
	ctx := r.Context()
	if !first {
		// Every binding and the verifier, a second time: the code worked
		// once and is being used again, so what it minted is revoked.
		if c.MintedTokenID != "" {
			h.revokeMinted(ctx, c.Approval.Dataset, c.MintedTokenID)
		}
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the code was already used")
		return
	}
	expires := h.now().Add(c.Approval.Lifetime).UTC().Truncate(time.Second)
	info, secret, err := c.Approval.Dataset.MintToken(ctx, substrate.MintTokenInput{
		Label: c.Request.Client.Host, ExpiresAt: &expires, Scopes: c.Approval.Scopes,
		ClientID: c.Request.Client.ID, ClientName: c.Approval.ClientName,
	})
	if err != nil {
		status, code := http.StatusInternalServerError, "server_error"
		if errors.Is(err, substrate.ErrUnavailable) {
			status, code = http.StatusServiceUnavailable, "temporarily_unavailable"
		}
		writeOAuthError(w, status, code, "the token could not be minted")
		return
	}
	if h.authz.Minted(code, info.ID) {
		h.revokeMinted(ctx, c.Approval.Dataset, info.ID)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the code was already used")
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken: secret, TokenType: "Bearer", Scope: strings.Join(c.Approval.Scopes, " "),
		ExpiresIn: int64(c.Approval.Lifetime / time.Second), Repository: c.Approval.Repository,
	})
}

// revokeBackoff is how long revokeMinted waits before each retry of a revoke
// that failed: a replay proved the code leaked, so the token must go, and a
// database that refused the delete once usually takes it seconds later.
var revokeBackoff = []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}

// revokeMinted deletes a token the flow minted, as the owner's own write: the
// request behind it carries no app's mark. The first attempt runs in the
// request; a failure is logged and retried in the background on the
// backoff above, outliving the request, until the delete lands or the token
// is gone. Every failure is logged, so a token a replay could not revoke is
// never silent.
func (h *handler) revokeMinted(ctx context.Context, ds substrate.Dataset, id string) {
	ctx = context.WithoutCancel(ctx)
	attempt := func() bool {
		_, err := ds.Delete(ctx, substrate.ActorAPI, tokenType, id, substrate.DeleteInput{})
		if err == nil || errors.Is(err, substrate.ErrNotFound) {
			return true
		}
		slog.Error("authorize: revoking the token a replayed code minted failed", "token", id, "error", err)
		return false
	}
	if attempt() {
		return
	}
	go func() {
		for _, wait := range revokeBackoff {
			time.Sleep(wait)
			if attempt() {
				slog.Info("authorize: revoked the token a replayed code minted", "token", id)
				return
			}
		}
		slog.Error("authorize: gave up revoking the token a replayed code minted; revoke it by hand", "token", id)
	}()
}

// postOAuthRevoke is RFC 7009: the token presented is the credential, and
// it is the only token revoked. Every answer to a well-formed request is
// 200, so the endpoint does not tell anybody whether a token existed.
func (h *handler) postOAuthRevoke(w http.ResponseWriter, r *http.Request) {
	form, ok := readOAuthForm(w, r)
	if !ok {
		return
	}
	token := form.Get("token")
	if token == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "token is required")
		return
	}
	ctx := r.Context()
	ds, info, err := h.svc.Authenticate(ctx, token)
	if err != nil {
		if errors.Is(err, substrate.ErrAuth) {
			w.WriteHeader(http.StatusOK)
			return
		}
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "the server could not look the token up")
		return
	}
	if cid := form.Get("client_id"); cid != "" && cid != info.ClientID {
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, tokenType, info.ID, substrate.DeleteInput{}); err != nil &&
		!errors.Is(err, substrate.ErrNotFound) {
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "the token could not be revoked")
		return
	}
	w.WriteHeader(http.StatusOK)
}
