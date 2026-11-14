package api

// APP SET UP (docs/plans/app-setup.md): an app's authorization request may
// name, beside its scopes, what approving sets up (`authorization_details`,
// internal/authorize). internal/appsetup plans it and runs it; this file is
// the HTTP side: the run detached from the approval that started it, what
// the approval page and the token response say of it, and the app's check.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/appsetup"
	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// runSetup runs an approval's set up, detached from the request that
// started it, and answers the request: the code once every row landed, with
// the page's whole view of the request as it then stands, or the request
// pending again with why it stopped. approved is the rows the approval was
// checked against.
func (h *handler) runSetup(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, req authorize.Request, approved []substrate.AuthorizationSetup,
	opts appsetup.Options, run appsetup.RunOptions, approve func(authorize.Request) (authorize.Approval, error),
) {
	if h.setUpEnded != nil {
		defer h.setUpEnded(req.ID)
	}
	// The request's own lapse bounds the run: past it nobody can answer
	// or read the request, so nothing more may land for it.
	ctx, cancel := context.WithTimeout(ctx, req.Expires.Sub(h.now()))
	defer cancel()
	owner := ds.Repository().ID
	stop := func(failure *substrate.AuthorizationFailure, moved bool) {
		end := h.authz.Fail
		if moved {
			end = h.authz.Replan
		}
		if err := end(req.ID, owner, failure); err != nil {
			slog.Warn("authorize: a set up outlived its request", "request", req.ID, "error", err)
		}
	}
	// Detached from the HTTP request, a panic here is outside chi's
	// recoverer and would take the server down with every repository on it.
	defer func() {
		if p := recover(); p != nil {
			slog.Error("authorize: a set up panicked and was contained",
				"request", req.ID, "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
			stop(&substrate.AuthorizationFailure{Message: "The set up failed on the server. Try again, and if it fails again the server log says why."}, false)
		}
	}()
	out := h.setups.Run(ctx, ds, actor, req.Details, approved, opts, run, func(progress []string) bool {
		if err := h.authz.Progress(req.ID, owner, progress); err != nil {
			slog.Warn("authorize: a set up outlived its request", "request", req.ID, "error", err)
			return false
		}
		return true
	})
	switch {
	case out.Gone:
		return
	case out.Failure != nil:
		stop(out.Failure, out.Moved)
		return
	}
	// What the approved request answers is read before the code exists: its
	// hand-over reads nothing, so no read that fails then can keep the code
	// from the page.
	reads, err := readForPage(ctx, ds, req)
	if err != nil {
		stop(&substrate.AuthorizationFailure{Message: appsetup.FailureWords("Everything is set up, and approving", err)}, false)
		return
	}
	// A set up may run for minutes: the publisher the page showed is checked
	// again before the code exists.
	if why := h.publisherMoved(ctx, req); why != "" {
		stop(&substrate.AuthorizationFailure{Message: "Everything is set up, and who publishes this app changed since the page loaded: " + why + ". Start again from the app, which reads who publishes it afresh."}, false)
		return
	}
	_, _, err = h.authz.Finish(req.ID, owner, approve, func(req authorize.Request, code string) substrate.AuthorizationRequest {
		view := reads.view(req, *req.Document, ds.Repository().Authority, req.Setup, req.PlanHash)
		view.Next = withParams(req.RedirectURI, url.Values{"code": {code}}, req)
		return view
	})
	switch {
	case errors.Is(err, authorize.ErrNotPending):
		slog.Warn("authorize: a set up finished after its request lapsed", "request", req.ID)
	case errors.Is(err, authorize.ErrFull):
		stop(&substrate.AuthorizationFailure{
			Message: "Everything is set up, and the server is holding too many authorization codes to issue one. Try again shortly.",
		}, false)
	case err != nil:
		stop(&substrate.AuthorizationFailure{Message: appsetup.FailureWords("Everything is set up, and approving", err)}, false)
	}
}

// --- the approval page's view and the token response ---

// scopeKinds lists the kinds one resolved read or write scope reaches: those
// the repository holds today, then those the rows would add that it reaches
// and it does not hold yet, a requirement's among them.
func scopeKinds(s scope.Scope, kinds []substrate.KindInfo, rows []substrate.AuthorizationSetup) []substrate.AuthorizationKind {
	out := reachedKinds(s, kinds)
	g, err := scope.NewGrant([]string{s.String()})
	if err != nil {
		return out
	}
	seen := map[string]bool{}
	for _, k := range out {
		seen[k.Identity] = true
	}
	var added []substrate.AuthorizationKind
	for _, row := range rows {
		if row.Package == nil || row.Action == substrate.SetupNone {
			continue
		}
		all := slices.Clone(row.Package.Kinds)
		for _, step := range row.Package.Requires {
			all = append(all, step.Kinds...)
		}
		for _, k := range all {
			// A kind an approval installs is listed by its name alone: the
			// kinds a trait scope reaches are listed from the registry once
			// they are installed (reachedKinds).
			kind := scope.Kind{Ref: k.Identity, Internal: k.Purpose == vocabulary.PurposeInternal}
			ok := g.Reads(kind)
			if s.Verb == scope.Write {
				ok = g.Writes(kind)
			}
			if ok && !seen[k.Identity] {
				seen[k.Identity] = true
				added = append(added, k)
			}
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].Identity < added[j].Identity })
	return append(out, added...)
}

// scopeInternal reports whether a resolved scope names one kind whose
// purpose is internal, which a glob never reaches. A kind the registry does
// not hold yet is read as internal, so only its exact scope covers it.
func scopeInternal(s scope.Scope, registry map[string]substrate.KindInfo) bool {
	if s.OverAgents() || s.Verb == scope.Webhook || s.Verb == scope.Call || vocabulary.IsTypeGlob(s.Pattern) {
		return false
	}
	if _, ok := s.Trait(); ok {
		return false
	}
	k, held := registry[s.Pattern]
	return !held || internalKind(k)
}

// previousToken is the newest token the repository holds for the request's
// client id. A local app's client id is a loopback address anybody on the
// owner's computer may name, so it is never matched: its page is always a
// first approval.
func previousToken(ctx context.Context, ds substrate.Dataset, client authorize.Client) (*substrate.AuthorizationPrevious, error) {
	if client.Local {
		return nil, nil
	}
	tokens, err := ds.Tokens(ctx)
	if err != nil {
		return nil, err
	}
	var newest *substrate.TokenInfo
	for i := range tokens {
		if !tokens[i].Retired && tokens[i].ClientID == client.ID && (newest == nil || tokens[i].Created.After(newest.Created)) {
			newest = &tokens[i]
		}
	}
	if newest == nil {
		return nil, nil
	}
	scopes := slices.Clone(newest.Scopes)
	if scopes == nil {
		scopes = []string{}
	}
	return &substrate.AuthorizationPrevious{ApprovedAt: newest.Created, ExpiresAt: newest.ExpiresAt, Scopes: scopes}, nil
}

// grantedDetails is what the token response lists as set up: each package
// with the version now held, each account, and each webhook with where to
// deliver.
func grantedDetails(ctx context.Context, ds substrate.Dataset, issuer, authority string, details []substrate.AuthorizationDetail) []substrate.AuthorizationDetail {
	if len(details) == 0 {
		return nil
	}
	// A read that fails costs the response its versions, never the token.
	held := map[string]int64{}
	statuses, err := ds.BundleStatusesUncounted(ctx)
	if err != nil {
		slog.Warn("authorize: the held versions could not be read for a token response", "error", err)
	}
	for i := range statuses {
		held[statuses[i].ID] = appsetup.HeldRelease(&statuses[i])
	}
	out := make([]substrate.AuthorizationDetail, 0, len(details))
	for _, d := range details {
		switch d.Type {
		case substrate.DetailPackage:
			out = append(out, substrate.AuthorizationDetail{Type: d.Type, Package: d.Package, Version: held[d.Package]})
		case substrate.DetailAccount:
			out = append(out, substrate.AuthorizationDetail{Type: d.Type, Kind: d.Kind, ID: d.ID})
		case substrate.DetailWebhook:
			out = append(out, substrate.AuthorizationDetail{
				Type: d.Type, Trigger: d.Trigger,
				URL: issuer + "/webhooks/" + url.PathEscape(ds.Repository().ID) + "/" + url.PathEscape(d.Trigger),
			})
		}
	}
	return out
}

// --- the app's check ---

// checkBody is the check's body as it arrives: its details stay raw for the
// one reader of details, which /oauth/authorize reads its parameter with,
// and its software version for the one reader of that.
type checkBody struct {
	Scope           string          `json:"scope"`
	Details         json.RawMessage `json:"details"`
	SoftwareVersion string          `json:"softwareVersion"`
}

// packageRefresher brings one document-published package the repository
// holds up to the newest version its publisher lists, where the upgrade is
// one the catalog applies on its own (decision 0213). The catalog is one
// once it reads authority documents; a test supplies its own.
type packageRefresher interface {
	RefreshPackage(ctx context.Context, ds substrate.Dataset, id string) (catalog.RefreshResult, error)
}

// postAuthorizationCheck answers an app, with its own token, whether it is
// still set up: the scopes it would ask for now that its token does not
// cover, each detail's action where the token may see it, and when the token
// ends. A row is `unknown` unless the token reaches what it names: a package
// one of its read or write scopes names a kind of, an account whose kind it
// reads, a webhook it delivers to. The owner's own token sees every row. An
// app asks at every launch, so the last answer a token got for a body is
// answered again for checkMemoTTL rather than planned again, and a plan
// waits its turn (checkTurns) rather than being refused. Every check records
// the build that asked on the calling token first (decision 0213).
func (h *handler) postAuthorizationCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw, ok := readBody(w, r, jsonBody)
	if !ok {
		return
	}
	var in checkBody
	if err := decodeStrictBytes(raw, &in, false); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	details, err := authorize.ParseDetails(string(in.Details))
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	var requested []scope.Scope
	if strings.TrimSpace(in.Scope) != "" {
		if requested, err = scope.ParseRequest(in.Scope); err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
			return
		}
	}
	softwareVersion, err := authorize.ParseSoftwareVersion(in.SoftwareVersion)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	app, err := checkedByAnApp(ctx)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	if app {
		if err := DatasetFrom(ctx).RecordAppBuild(ctx, authorize.PackageFloors(details), softwareVersion); err != nil {
			writeSubstrateError(w, r, err)
			return
		}
	}
	principal := substrate.PrincipalFrom(ctx)
	if out, ok := h.checks.answer(principal, raw, h.now()); ok {
		writeJSON(w, http.StatusOK, out)
		return
	}
	release, err := h.checkTurns.wait(ctx, principal)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	defer release()
	// The plan this one waited for may have been of the same body.
	if out, ok := h.checks.answer(principal, raw, h.now()); ok {
		writeJSON(w, http.StatusOK, out)
		return
	}
	out, err := h.check(ctx, requested, details)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	h.checks.keep(principal, raw, h.now(), out)
	writeJSON(w, http.StatusOK, out)
}

// checkedByAnApp reports whether the request's token is an app's, the one
// kind of token with a build for a check to record. A scoped app's token
// carries its client id in the request, a full-access app's only on its
// record. An owner's own token asks too and sees every row (docs/apps.md),
// and has no build: recording one is refused for a token with no client id.
// A token missing from the list was revoked after the request signed in, and
// is refused as the build's record refuses it: it is nobody's to answer.
func checkedByAnApp(ctx context.Context) (bool, error) {
	if substrate.ClientIDFrom(ctx) != "" {
		return true, nil
	}
	if _, scoped := substrate.ScopesFrom(ctx); scoped {
		return false, nil
	}
	tokens, err := DatasetFrom(ctx).Tokens(ctx)
	if err != nil {
		return false, err
	}
	id := substrate.PrincipalFrom(ctx)
	for _, t := range tokens {
		if t.ID == id {
			return t.ClientID != "", nil
		}
	}
	return false, fmt.Errorf("%w: a live token is required", substrate.ErrForbidden)
}

// check plans a check's answer for the request's token.
func (h *handler) check(ctx context.Context, requested []scope.Scope, details []substrate.AuthorizationDetail) (substrate.AuthorizationCheckResult, error) {
	ds := DatasetFrom(ctx)
	names := ds.Repository().LocalNames()
	// The plan reads the repository whole; what the token may learn of it is
	// cut below, row by row.
	owner, grant, full := ds, scope.Grant{}, true
	if sd, ok := ds.(*scopedDataset); ok {
		// A person's view holds no app's grant: it is checked as one that
		// grants nothing.
		owner, full = sd.inner, false
		if app, isApp := sd.grant.(appReach); isApp {
			grant = app.Grant
		}
	}
	var registry map[string]substrate.KindInfo
	if !full {
		kinds, err := owner.Kinds(ctx)
		if err != nil {
			return substrate.AuthorizationCheckResult{}, err
		}
		registry = make(map[string]substrate.KindInfo, len(kinds))
		for _, k := range kinds {
			registry[k.Identity] = k
		}
	}
	missing := []string{}
	for _, s := range requested {
		resolved := s.Localize(names...)
		if !full && !grant.Covers(resolved, scopeInternal(resolved, registry)) && !slices.Contains(missing, resolved.String()) {
			missing = append(missing, resolved.String())
		}
	}
	// The code the packages install is held to what the token itself may
	// do (decision 0215). A package row the token will see as `unknown` is
	// planned no further than its action, which the account and webhook
	// rows it admits read.
	opts := appsetup.Options{Approved: scope.FullApproval()}
	if !full {
		opts.Approved = scope.ApprovalFrom(grant)
		opts.ActionOnly = map[string]bool{}
		for _, d := range details {
			if d.Type == substrate.DetailPackage && !grant.NamesPackage(d.Package) {
				opts.ActionOnly[d.Package] = true
			}
		}
	}
	h.refreshBelowFloor(ctx, owner, details, opts.ActionOnly)
	packages := h.packages
	if packages != nil && len(opts.ActionOnly) > 0 {
		packages = shippedOnly{Catalog: packages, shipped: h.catalog, withheld: opts.ActionOnly}
	}
	rows, _, err := appsetup.Plan(ctx, owner, packages, details, opts)
	if err != nil {
		return substrate.AuthorizationCheckResult{}, err
	}
	out := substrate.AuthorizationCheckResult{Scopes: substrate.AuthorizationCheckScopes{Missing: missing}, Setup: make([]substrate.AuthorizationSetup, len(rows))}
	for i, row := range rows {
		visible := full
		if !full {
			switch row.Type {
			case substrate.DetailPackage:
				visible = grant.NamesPackage(row.Package.ID)
			case substrate.DetailAccount:
				// A kind the registry does not hold yet is read as internal,
				// which a glob never reaches.
				visible = grant.Reads(scopeKind(row.Account.Kind, registry))
			case substrate.DetailWebhook:
				visible = grant.Delivers(row.Webhook.Trigger)
			}
		}
		out.Setup[i] = checkRow(row, visible)
	}
	if lease := leaseFrom(ctx); lease != nil {
		out.ExpiresAt = lease.expires
	} else if full {
		tokens, err := owner.Tokens(ctx)
		if err != nil {
			return substrate.AuthorizationCheckResult{}, err
		}
		principal := substrate.PrincipalFrom(ctx)
		for _, t := range tokens {
			if t.ID == principal {
				out.ExpiresAt = t.ExpiresAt
			}
		}
	}
	return out, nil
}

// shippedOnly is the approval's catalog with each package in withheld, one
// the token's scopes do not name, resolved through the binary's closures
// alone. The check answers that row `unknown`, and resolving it through its
// publisher's document would let an app with any access make the server fetch
// a document for a package it does not reach. A package nothing resolves is
// planned no further than its action.
type shippedOnly struct {
	appsetup.Catalog
	shipped  *catalog.Catalog
	withheld map[string]bool
}

func (c shippedOnly) ResolveFor(ctx context.Context, id string, atLeast int64, ds substrate.Dataset) (*catalog.Bundle, error) {
	if c.withheld[id] && !c.ships(id) {
		return nil, fmt.Errorf("%w: %s is not resolved for a token whose scopes do not name it", catalog.ErrNotPublished, id)
	}
	return c.Catalog.ResolveFor(ctx, id, atLeast, ds)
}

func (c shippedOnly) ships(id string) bool {
	if c.shipped == nil {
		return false
	}
	_, ok := c.shipped.ByID(id)
	return ok
}

// checkRow cuts a planned row to what a check answers. A row the token may
// see keeps its action, what blocks it, a package's versions and the package
// a webhook delivers to; one it may not see is `unknown` and carries only
// what the app sent.
func checkRow(row substrate.AuthorizationSetup, visible bool) substrate.AuthorizationSetup {
	out := substrate.AuthorizationSetup{Type: row.Type, Action: substrate.SetupUnknown}
	if visible {
		out.Action = row.Action
		if row.Blocked != nil {
			b := *row.Blocked
			out.Blocked = &b
		}
	}
	switch row.Type {
	case substrate.DetailPackage:
		out.Package = &substrate.AuthorizationPackage{ID: row.Package.ID, Required: row.Package.Required}
		if visible {
			out.Package.Held, out.Package.Shipped = row.Package.Held, row.Package.Shipped
		}
	case substrate.DetailAccount:
		out.Account = &substrate.AuthorizationAccount{Kind: row.Account.Kind, ID: row.Account.ID}
	case substrate.DetailWebhook:
		out.Webhook = &substrate.AuthorizationWebhookPlan{Trigger: row.Webhook.Trigger}
		if visible {
			out.Webhook.Package = row.Webhook.Package
		}
	}
	return out
}

// refreshBelowFloor brings each document-published package the repository
// holds below the floor the app's build asks for up to the newest version
// its publisher lists, where the catalog applies that on its own (decision
// 0213): a kinds-only, additive upgrade lands now and the row answers
// `none`. One the catalog cannot refresh is left for the plan to say what
// it needs, with the reason in the log. A package in withheld, one the
// token's scopes do not name, is not refreshed: an app may not make the
// server fetch a publisher's document and upgrade a package it has nothing
// to do with by naming it in a check.
func (h *handler) refreshBelowFloor(ctx context.Context, ds substrate.Dataset, details []substrate.AuthorizationDetail, withheld map[string]bool) {
	floors := authorize.PackageFloors(details)
	if h.refresher == nil || h.packages == nil || len(floors) == 0 {
		return
	}
	statuses, err := ds.BundleStatusesUncounted(ctx)
	if err != nil {
		slog.Warn("authorize: the held versions could not be read for a check's refresh", "error", err)
		return
	}
	held := map[string]int64{}
	for i := range statuses {
		held[statuses[i].ID] = appsetup.HeldRelease(&statuses[i])
	}
	for _, id := range slices.Sorted(maps.Keys(floors)) {
		if withheld[id] || held[id] == 0 || held[id] >= floors[id] {
			continue
		}
		b, err := h.packages.ResolveFor(ctx, id, floors[id], ds)
		if err != nil || b.Origin() == nil {
			continue
		}
		res, err := h.refresher.RefreshPackage(ctx, ds, id)
		if err != nil {
			slog.Warn("authorize: a package below its app's floor could not be refreshed", "package", logLine(id), "error", logLine(err.Error()))
			continue
		}
		slog.Info("authorize: a check refreshed a package below its app's floor", "package", logLine(id), "outcome", res.Outcome, "reason", logLine(res.Reason))
	}
}

// checkMemoTTL is how long a token's last check answers the same body
// again: an app asks at every launch and on its own clock, and every answer
// plans against the owner's data, an upgrade preview included, where a status
// probe is never refused for asking.
const checkMemoTTL = 30 * time.Second

// maxCheckMemo bounds the answers kept: one per token that checked within
// checkMemoTTL.
const maxCheckMemo = 1024

// maxCheckPlans bounds the check plans running at once across the server.
const maxCheckPlans = 4

// checkTurns queues the check's plans: one at a time per token, and at most
// maxCheckPlans at once. A plan stages upgrade previews against the owner's
// data and a check is never refused for asking, so a token that changes its
// body to miss the memo, in a loop or in parallel, waits instead.
type checkTurns struct {
	plans  chan struct{}
	mu     sync.Mutex
	tokens map[string]*tokenTurn
}

// tokenTurn is one token's turn and how many of its checks hold or want it.
type tokenTurn struct {
	slot chan struct{}
	refs int
}

func newCheckTurns(plans int) *checkTurns {
	return &checkTurns{plans: make(chan struct{}, plans), tokens: map[string]*tokenTurn{}}
}

// wait takes principal's turn, then one of the server's plans, in that order,
// so a token's queued checks hold no more than one of them. release gives
// both back; an error is ctx's, the caller gone while it waited.
func (q *checkTurns) wait(ctx context.Context, principal string) (release func(), err error) {
	q.mu.Lock()
	t := q.tokens[principal]
	if t == nil {
		t = &tokenTurn{slot: make(chan struct{}, 1)}
		q.tokens[principal] = t
	}
	t.refs++
	q.mu.Unlock()
	leave := func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		if t.refs--; t.refs == 0 {
			delete(q.tokens, principal)
		}
	}
	select {
	case t.slot <- struct{}{}:
	case <-ctx.Done():
		leave()
		return nil, ctx.Err()
	}
	select {
	case q.plans <- struct{}{}:
	case <-ctx.Done():
		<-t.slot
		leave()
		return nil, ctx.Err()
	}
	return func() {
		<-q.plans
		<-t.slot
		leave()
	}, nil
}

// checkMemo is the last check answer each token got.
type checkMemo struct {
	mu   sync.Mutex
	last map[string]checkAnswer
}

type checkAnswer struct {
	body [sha256.Size]byte
	at   time.Time
	out  substrate.AuthorizationCheckResult
}

// answer is principal's last answer, when it was for body and is younger
// than checkMemoTTL.
func (m *checkMemo) answer(principal string, body []byte, now time.Time) (substrate.AuthorizationCheckResult, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.last[principal]
	if !ok || a.body != sha256.Sum256(body) || now.Sub(a.at) >= checkMemoTTL {
		return substrate.AuthorizationCheckResult{}, false
	}
	return a.out, true
}

// keep records principal's answer for body, dropping the answers too old to
// be served when the memo is full.
func (m *checkMemo) keep(principal string, body []byte, now time.Time, out substrate.AuthorizationCheckResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.last == nil {
		m.last = map[string]checkAnswer{}
	}
	if len(m.last) >= maxCheckMemo {
		for p, a := range m.last {
			if now.Sub(a.at) >= checkMemoTTL {
				delete(m.last, p)
			}
		}
		if len(m.last) >= maxCheckMemo {
			clear(m.last)
		}
	}
	m.last[principal] = checkAnswer{body: sha256.Sum256(body), at: now, out: out}
}

// logLine is a caller-supplied value as one log line: an app names package
// ids and a request id rides the URL, and neither may start a forged line.
func logLine(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", ""), "\r", "")
}
