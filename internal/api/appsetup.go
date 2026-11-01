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
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// runSetup runs an approval's set up, detached from the request that
// started it, and answers the request: the code once every row landed, the
// request pending again with why it stopped otherwise. approved is the rows
// the approval was checked against.
func (h *handler) runSetup(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, req authorize.Request, approved []substrate.AuthorizationSetup,
	approve func(authorize.Request) (authorize.Approval, error),
) {
	// The request's own lapse bounds the run: past it nobody can answer
	// or read the request, so nothing more may land for it.
	ctx, cancel := context.WithTimeout(ctx, req.Expires.Sub(h.now()))
	defer cancel()
	authority := ds.Repository().Authority
	stop := func(failure *substrate.AuthorizationFailure, moved bool) {
		end := h.authz.Fail
		if moved {
			end = h.authz.Replan
		}
		if err := end(req.ID, authority, failure); err != nil {
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
	out := h.setups.Run(ctx, ds, actor, req.Details, approved, func(progress []string) bool {
		if err := h.authz.Progress(req.ID, authority, progress); err != nil {
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
	_, _, err := h.authz.Finish(req.ID, authority, approve, func(req authorize.Request, code string) string {
		return withParams(req.RedirectURI, url.Values{"code": {code}}, req)
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
			internal := k.Purpose == vocabulary.PurposeInternal
			ok := g.Reads(k.Identity, internal)
			if s.Verb == scope.Write {
				ok = g.Writes(k.Identity, internal)
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
	if s.OverAgents() || s.Verb == scope.Webhook || vocabulary.IsTypeGlob(s.Pattern) {
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
		if tokens[i].ClientID == client.ID && (newest == nil || tokens[i].Created.After(newest.Created)) {
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
				URL: issuer + "/webhooks/" + url.PathEscape(authority) + "/" + url.PathEscape(d.Trigger),
			})
		}
	}
	return out
}

// --- the app's check ---

// checkBody is the check's body as it arrives: its details stay raw for the
// one reader of details, which /oauth/authorize reads its parameter with.
type checkBody struct {
	Scope   string          `json:"scope"`
	Details json.RawMessage `json:"details"`
}

// postAuthorizationCheck answers an app, with its own token, whether it is
// still set up: the scopes it would ask for now that its token does not
// cover, each detail's action where the token may see it, and when the token
// ends. A row is `unknown` unless the token reaches what it names: a package
// one of its read or write scopes names a kind of, an account whose kind it
// reads, a webhook it delivers to. The owner's own token sees every row. An
// app asks at every launch, so the last answer a token got for a body is
// answered again for checkMemoTTL rather than planned again.
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
	principal := substrate.PrincipalFrom(ctx)
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

// check plans a check's answer for the request's token.
func (h *handler) check(ctx context.Context, requested []scope.Scope, details []substrate.AuthorizationDetail) (substrate.AuthorizationCheckResult, error) {
	ds := DatasetFrom(ctx)
	authority := ds.Repository().Authority
	// The plan reads the repository whole; what the token may learn of it is
	// cut below, row by row.
	owner, grant, full := ds, scope.Grant{}, true
	if sd, ok := ds.(*scopedDataset); ok {
		owner, grant, full = sd.inner, sd.grant, false
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
		resolved := s.Resolve(authority)
		if !full && !grant.Covers(resolved, scopeInternal(resolved, registry)) && !slices.Contains(missing, resolved.String()) {
			missing = append(missing, resolved.String())
		}
	}
	// A package row the token will see as `unknown` is planned no further
	// than its action, which the account and webhook rows it admits read.
	opts := appsetup.Options{}
	if !full {
		opts.ActionOnly = map[string]bool{}
		for _, d := range details {
			if d.Type == substrate.DetailPackage && !grant.NamesPackage(d.Package) {
				opts.ActionOnly[d.Package] = true
			}
		}
	}
	rows, _, err := appsetup.Plan(ctx, owner, h.catalog, details, opts)
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
				k, held := registry[row.Account.Kind]
				visible = grant.Reads(row.Account.Kind, !held || internalKind(k))
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

// checkMemoTTL is how long a token's last check answers the same body
// again: an app asks at every launch and on its own clock, and every answer
// plans against the owner's data, an upgrade preview included, where a status
// probe is never refused for asking.
const checkMemoTTL = 30 * time.Second

// maxCheckMemo bounds the answers kept: one per token that checked within
// checkMemoTTL.
const maxCheckMemo = 1024

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
