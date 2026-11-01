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

	"github.com/geoah/substrate/internal/appsetup"
	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
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

// lifetimeDays are the lifetimes an owner may give an app's token, 0 for
// until the owner revokes it. There is no refresh token, so a lifetime is
// how long an app keeps access without the owner seeing the approval page
// again.
var lifetimeDays = []int{7, 30, 90, 0}

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
	// AuthorizationDetailsTypesSupported (RFC 9396) is how an app knows to
	// send its set up: a server that does not list them ignores the
	// parameter and would approve the scopes alone.
	AuthorizationDetailsTypesSupported []string `json:"authorization_details_types_supported"`
}

type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ScopesSupported        []string `json:"scopes_supported"`
	ResourceName           string   `json:"resource_name"`
}

// scopesSupported names the widest scope of each verb: every other scope is
// a narrower pattern of one of them, which no list could enumerate.
var scopesSupported = []string{"read:*", "write:*", "agents:*", "decide:*"}

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
		AuthorizationDetailsTypesSupported:     []string{substrate.DetailPackage, substrate.DetailAccount, substrate.DetailWebhook},
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
	if err := authorize.RedirectAllowed(client, redirect); err != nil {
		writeAuthorizePage(w, http.StatusBadRequest, err.Error())
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
	details, err := authorize.ParseDetails(q.Get("authorization_details"))
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
		Issuer: iss, Client: client, RedirectURI: redirect, AppState: state,
		Challenge: q.Get("code_challenge"), Scopes: scopes, Details: details, Source: source,
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

// getAuthorization answers the approval page. A pending request with no
// set up behind it is planned at every read, so it shows the repository as it
// is; once an approval checked its rows, the page reads those, each with its
// progress, while the set up runs and after it stopped. An approved request
// is answered once, as its set up left it, reading nothing: that read hands
// the page where to go, and the request is gone after it.
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
	if req.State == substrate.AuthorizationApproved {
		handed, err := h.authz.HandOver(id, authority)
		if err != nil {
			writeAuthorizationGone(w)
			return
		}
		writeJSON(w, http.StatusOK, handed.Answer)
		return
	}
	// What the page shows is what an approval or a denial is checked
	// against: the first snapshot, never a later fetch, so a request read
	// twice shows the same document both times.
	var doc authorize.Document
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
	// The claim above came first: the plan reads this repository, and
	// another repository's read of the request learns nothing of it.
	rows, hash := req.Setup, req.PlanHash
	if rows == nil {
		if rows, hash, err = appsetup.Plan(ctx, ds, h.catalog, req.Details, appsetup.Options{}); err != nil {
			writeSubstrateError(w, r, err)
			return
		}
	}
	reads, err := readForPage(ctx, ds, req)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reads.view(req, doc, authority, rows, hash))
}

// pageReads is what the approval page's view of a request reads from the
// repository: the registry, the app's previous token, and the agent rows
// where a scope names agents.
type pageReads struct {
	kinds    []substrate.KindInfo
	previous *substrate.AuthorizationPrevious
	agents   []*substrate.Record
}

// readForPage reads what the approval page's view of req needs of ds.
func readForPage(ctx context.Context, ds substrate.Dataset, req authorize.Request) (pageReads, error) {
	var out pageReads
	var err error
	if out.kinds, err = ds.Kinds(ctx); err != nil {
		return out, err
	}
	if out.previous, err = previousToken(ctx, ds, req.Client); err != nil {
		return out, err
	}
	if slices.ContainsFunc(req.Scopes, scope.Scope.OverAgents) {
		if out.agents, err = agentRows(ctx, ds); err != nil {
			return out, err
		}
	}
	return out, nil
}

// view is the approval page's view of req in repository authority: the app as
// doc describes it, rows as what approving sets up under their digest hash,
// and each scope with what it reaches and whether the previous token holds
// it. It reads nothing, so a set up that finished builds its request's last
// view under the store's lock.
func (p pageReads) view(req authorize.Request, doc authorize.Document, authority string, rows []substrate.AuthorizationSetup, hash string) substrate.AuthorizationRequest {
	view := substrate.AuthorizationRequest{
		ID: req.ID,
		App: substrate.AuthorizationApp{
			ClientID: req.Client.ID, Host: req.Client.Host, Local: req.Client.Local,
			Name: doc.Name, URI: doc.URI, Actor: string(substrate.AppActor(req.Client.Host)),
			URIHostDiffers: doc.URI != "" && hostname(doc.URI) != hostname(req.Client.ID),
		},
		Redirect: substrate.AuthorizationRedirectTo{
			URI: req.RedirectURI, Host: authorize.HostOf(req.RedirectURI),
			Loopback: authorize.LocalRedirect(req.RedirectURI),
		},
		Repository:          authority,
		LifetimeDays:        lifetimeDays,
		DefaultLifetimeDays: defaultLifetimeDays,
		ExpiresAt:           req.Expires,
		Details:             req.Details,
		Setup:               slices.Clone(rows),
		PlanHash:            hash,
		Previous:            p.previous,
		State:               req.State,
		Failure:             req.Failure,
	}
	if view.Details == nil {
		view.Details = []substrate.AuthorizationDetail{}
	}
	if view.Setup == nil {
		view.Setup = []substrate.AuthorizationSetup{}
	}
	var before scope.Grant
	if view.Previous != nil {
		// A grant that does not parse covers nothing, so its scopes read as
		// new.
		before, _ = scope.NewGrant(view.Previous.Scopes)
	}
	// The webhook a row turns on is the one scope a detail adds, so whether
	// the previous token holds it is said on the row. The plan is copied:
	// its pointer is shared with the rows the store holds.
	for i, row := range view.Setup {
		if row.Webhook != nil {
			plan := *row.Webhook
			plan.Allowed = before.Covers(scope.WebhookScope(plan.Trigger), false)
			view.Setup[i].Webhook = &plan
		}
	}
	registry := make(map[string]substrate.KindInfo, len(p.kinds))
	for _, k := range p.kinds {
		registry[k.Identity] = k
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
		row := substrate.AuthorizationScope{
			Scope: resolved.String(), Requested: s.String(), Verb: string(resolved.Verb),
			Write: resolved.Verb == scope.Write, Pattern: resolved.Pattern,
			Glob: strings.HasSuffix(resolved.Pattern, "*"), Kinds: []substrate.AuthorizationKind{},
			Agents:  []*substrate.Record{},
			Allowed: before.Covers(resolved, scopeInternal(resolved, registry)),
		}
		if resolved.OverAgents() {
			row.Agents = matchedAgents(resolved, p.agents)
		} else {
			row.Kinds = scopeKinds(resolved, p.kinds, view.Setup)
		}
		view.Scopes = append(view.Scopes, row)
	}
	return view
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
		if s.Verb == scope.Write {
			ok = g.Writes(k.Identity, internalKind(k))
		}
		if ok {
			out = append(out, substrate.AuthorizationKind{Identity: k.Identity, Label: k.Label, Purpose: kindPurpose(k)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity < out[j].Identity })
	return out
}

// kindPurpose is a registry kind's declared purpose where it is not the
// primary one an absent purpose reads as.
func kindPurpose(k substrate.KindInfo) string {
	p, _ := k.Definition["purpose"].(string)
	if p == vocabulary.PurposePrimary {
		return ""
	}
	return p
}

// agentRows is every agent row of the repository, sorted by id.
func agentRows(ctx context.Context, ds substrate.Dataset) ([]*substrate.Record, error) {
	var out []*substrate.Record
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{kindAgent}}, First: agentListPage}
	for {
		page, err := ds.List(ctx, q)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Records...)
		if page.Cursor == "" || len(page.Records) == 0 {
			break
		}
		q.After = page.Cursor
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// matchedAgents lists the agents one resolved agents or decide scope matches
// today.
func matchedAgents(s scope.Scope, agents []*substrate.Record) []*substrate.Record {
	out := []*substrate.Record{}
	g, err := scope.NewGrant([]string{s.String()})
	if err != nil {
		return out
	}
	for _, a := range agents {
		if g.TalksTo(a.ID) || g.Decides(a.ID) {
			out = append(out, a)
		}
	}
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

// postAuthorization answers a request with an approval. The approval
// reserves the request, so nothing else answers it meanwhile, and plans what
// it sets up once, under the reservation: a request that needs nothing set
// up is approved there and answered with the redirect; otherwise the plan
// must still be the one the page showed, and the approval answers 202 while
// the set up runs (appsetup.go), the page reading the request until it
// carries the redirect. Every refusal hands the request back.
func (h *handler) postAuthorization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in substrate.AuthorizationApproval
	if !readJSON(w, r, &in, jsonBody) {
		return
	}
	ds := DatasetFrom(ctx)
	authority := ds.Repository().Authority
	id := pathParam(r, "request")
	actor := ActorFrom(ctx)
	// Checked again under the store's lock by whichever of Reserve, Approve
	// and Finish answers the request.
	approve := func(req authorize.Request) (authorize.Approval, error) {
		// First, so a door that may not approve this request reserves and
		// plans nothing: what a request with details sets up, or finds in
		// place, is approved as the owner, through a human door.
		if len(req.Details) > 0 && !substrate.HumanActors[actor] {
			return authorize.Approval{}, notTheOwnersDoor{actor: actor}
		}
		doc, err := shownApp(req)
		if err != nil {
			return authorize.Approval{}, err
		}
		scopes, err := approvedScopes(req, authority, in.Scopes)
		if err != nil {
			return authorize.Approval{}, err
		}
		if in.LifetimeDays == nil {
			return authorize.Approval{}, validation("an approval names its lifetimeDays: 7, 30 or 90, or 0 for until revoked")
		}
		if !slices.Contains(lifetimeDays, *in.LifetimeDays) {
			return authorize.Approval{}, validation("lifetimeDays must be one of 7, 30 or 90, or 0 for until revoked")
		}
		// Only an approved webhook detail puts `webhook:` on a token.
		for _, d := range req.Details {
			if d.Type == substrate.DetailWebhook {
				scopes = append(scopes, scope.WebhookScope(d.Trigger).String())
			}
		}
		return authorize.Approval{
			Repository: authority, Dataset: ds, Scopes: scopes,
			Lifetime: time.Duration(*in.LifetimeDays) * 24 * time.Hour, ClientName: doc.Name,
			Details: req.Details,
		}, nil
	}
	req, err := h.authz.Reserve(id, authority, func(req authorize.Request) error {
		_, err := approve(req)
		return err
	})
	if err != nil {
		writeAuthorizationAnswerError(w, r, err)
		return
	}
	// refuse hands the request back; moved also drops the rows the page
	// showed, so its next read plans what approving would do now.
	refuse := func(moved bool, answer func()) {
		end := h.authz.Fail
		if moved {
			end = h.authz.Replan
		}
		if err := end(req.ID, authority, nil); err != nil {
			slog.Warn("authorize: a refused approval could not return its request", "request", req.ID, "error", err)
		}
		answer()
	}
	conflict := func() {
		writeError(w, http.StatusConflict, codeConflict, "what approving sets up changed since the page read it: read the request again")
	}
	rows, hash, err := appsetup.Plan(ctx, ds, h.catalog, req.Details, appsetup.Options{})
	if err != nil {
		refuse(false, func() { writeSubstrateError(w, r, err) })
		return
	}
	// What runs is what the page showed: the plan it read, or, after a set
	// up stopped, the rows that set up was checked against, of which the
	// plan now may run less.
	approved, approvedHash := rows, hash
	if req.Setup != nil {
		if in.PlanHash != "" && in.PlanHash != req.PlanHash {
			refuse(false, conflict)
			return
		}
		approved, approvedHash = withoutProgress(req.Setup), req.PlanHash
		if appsetup.Moved(approved, rows) >= 0 {
			refuse(true, conflict)
			return
		}
	} else if in.PlanHash != "" && in.PlanHash != hash {
		refuse(false, conflict)
		return
	}
	if i := appsetup.FirstBlocked(rows); i >= 0 {
		refuse(req.Setup != nil, func() {
			writeError(w, http.StatusUnprocessableEntity, codeValidation, "this request cannot be set up: "+rows[i].Blocked.Problem)
		})
		return
	}
	if !appsetup.NeedsAction(rows) {
		code, req, err := h.authz.Approve(id, authority, approve)
		if err != nil {
			refuse(false, func() { writeAuthorizationAnswerError(w, r, err) })
			return
		}
		writeJSON(w, http.StatusOK, substrate.AuthorizationAnswer{
			Redirect: withParams(req.RedirectURI, url.Values{"code": {code}}, req),
		})
		return
	}
	if in.PlanHash == "" {
		refuse(false, func() {
			writeError(w, http.StatusUnprocessableEntity, codeValidation, "an approval that sets something up names the planHash the page read")
		})
		return
	}
	started := appsetup.Started(approved, rows)
	if err := h.authz.Begin(id, authority, started, approvedHash); err != nil {
		writeAuthorizationAnswerError(w, r, err)
		return
	}
	// Detached: a closed tab does not stop a set up that may be part way
	// through an install, and the page that comes back reads where it is.
	go h.runSetup(context.WithoutCancel(ctx), ds, actor, req, approved, approve)
	writeJSON(w, http.StatusAccepted, substrate.AuthorizationAnswer{State: substrate.AuthorizationSettingUp, Setup: started})
}

// withoutProgress is rows as an approval names them, the progress of the
// last run cleared.
func withoutProgress(rows []substrate.AuthorizationSetup) []substrate.AuthorizationSetup {
	out := slices.Clone(rows)
	for i := range out {
		out[i].Progress = ""
	}
	return out
}

// notTheOwnersDoor refuses an approval of a request with details from a door
// that is not a human's.
type notTheOwnersDoor struct{ actor substrate.Actor }

func (e notTheOwnersDoor) Error() string {
	return "setting up is the owner's act, through the console, substratectl or the API, never as " + string(e.actor)
}

// writeAuthorizationAnswerError answers the store's refusals of an approval
// or a denial.
func writeAuthorizationAnswerError(w http.ResponseWriter, r *http.Request, err error) {
	var door notTheOwnersDoor
	switch {
	case errors.As(err, &door):
		writeError(w, http.StatusForbidden, codeForbidden, door.Error())
	case errors.Is(err, authorize.ErrNotPending):
		writeAuthorizationGone(w)
	case errors.Is(err, authorize.ErrSettingUp):
		writeError(w, http.StatusConflict, codeConflict,
			"this request is being set up: it is answered when the set up finishes, or pending again if it fails")
	case errors.Is(err, authorize.ErrNotShown):
		writeSubstrateError(w, r, validation("the approval page has not shown this request yet: load it, then answer"))
	case errors.Is(err, authorize.ErrFull):
		writeError(w, http.StatusServiceUnavailable, codeUnavailable, "the server is holding too many authorization codes; try again shortly")
	default:
		writeSubstrateError(w, r, err)
	}
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
	if err != nil {
		writeAuthorizationAnswerError(w, r, err)
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
	if req.AppState != "" {
		v.Set("state", req.AppState)
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
	// ExpiresIn is absent on a token the owner approved until revoked.
	ExpiresIn *int64 `json:"expires_in,omitempty"`
	// Repository is the authority the token opens, as IndieAuth's `me`
	// tells an app whom it signed in as.
	Repository string `json:"repository"`
	// AuthorizationDetails (RFC 9396 §7) is what the approval set up or
	// found in place: each package at the version now held, each account,
	// and each webhook with the URL it takes deliveries at.
	AuthorizationDetails []substrate.AuthorizationDetail `json:"authorization_details,omitempty"`
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
	var expiresAt *time.Time
	if c.Approval.Lifetime > 0 {
		at := h.now().Add(c.Approval.Lifetime).UTC().Truncate(time.Second)
		expiresAt = &at
	}
	info, secret, err := c.Approval.Dataset.MintToken(ctx, substrate.MintTokenInput{
		Label: c.Request.Client.Host, ExpiresAt: expiresAt, Scopes: c.Approval.Scopes,
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
	res := tokenResponse{
		AccessToken: secret, TokenType: "Bearer", Scope: strings.Join(c.Approval.Scopes, " "),
		Repository:           c.Approval.Repository,
		AuthorizationDetails: grantedDetails(ctx, c.Approval.Dataset, c.Request.Issuer, c.Approval.Repository, c.Approval.Details),
	}
	if c.Approval.Lifetime > 0 {
		secs := int64(c.Approval.Lifetime / time.Second)
		res.ExpiresIn = &secs
	}
	writeJSON(w, http.StatusOK, res)
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
		// Each attempt is bounded, so a database that hangs costs one
		// attempt and the next one still runs.
		actx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		_, err := ds.Delete(actx, substrate.ActorAPI, tokenType, id, substrate.DeleteInput{})
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
