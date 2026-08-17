package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"text/template"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/geoah/substrate/internal/substrate"
)

// bundlesFrom resolves the request's dataset to the bundle-lifecycle seam; a
// dataset without it has no bundle verbs.
func bundlesFrom(ctx context.Context) (substrate.BundleOps, bool) {
	ops, ok := DatasetFrom(ctx).(substrate.BundleOps)
	return ops, ok
}

func writeNoBundles(w http.ResponseWriter) {
	writeUnsupported(w, "this substrate runs no bundles")
}

// getBundleStatuses lists every installed bundle's computed runtime state.
func (h *handler) getBundleStatuses(w http.ResponseWriter, r *http.Request) {
	ops, ok := bundlesFrom(r.Context())
	if !ok {
		writeNoBundles(w)
		return
	}
	statuses, err := ops.BundleStatuses(r.Context())
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bundles": statuses})
}

// getBundleStatus is one bundle's computed state — lifecycle, input
// resolution, setup steps, account and record counts.
func (h *handler) getBundleStatus(w http.ResponseWriter, r *http.Request) {
	ops, ok := bundlesFrom(r.Context())
	if !ok {
		writeNoBundles(w)
		return
	}
	st, err := ops.BundleStatus(r.Context(), pathParam(r, "id"))
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// bundleLifecycleGate resolves the bundle's owning authority for a lifecycle
// verb. It no longer AUTHORIZES anything: the token that reached here holds
// the whole repository, and the bundle's own lifecycle rules —
// not a capability list — decide what the verb may do. On failure it has
// already written the response.
func (h *handler) bundleLifecycleGate(w http.ResponseWriter, r *http.Request, ops substrate.BundleOps) (string, bool) {
	authority, err := ops.BundleAuthority(r.Context(), pathParam(r, "id"))
	if err != nil {
		writeSubstrateError(w, err)
		return "", false
	}
	return authority, true
}

// bundleStateProps are the bundle's runtime-state properties, the transitions a
// PATCH of the bundle record carries. They are managed properties the engine
// writes, never authored, so a PATCH names exactly one and the handler runs the
// matching lifecycle op behind the exclusive fence.
const (
	propBundleDisabled    = "disabled"
	propBundleUninstalled = "uninstalled"
	propBundlePurging     = "purging"
)

// patchBundleLifecycle runs one bundle lifecycle transition, expressed as a
// PATCH of the bundle record's runtime state (decision 0019). It reads exactly
// one state property and dispatches:
//
//	disabled: true    disable — execution stopped, reversible
//	disabled: false   enable — reverses a disable
//	uninstalled: true uninstall — runtime registration removed, data stays
//	purging: true     purge — the owned authority's data tombstoned
//
// Disable, enable and bind answer with the refreshed status; uninstall removes
// the row (no status to reload) and acks {"uninstalled": true}; purge answers
// with the tombstoned count. The guards live in the engine ops, unchanged.
func (h *handler) patchBundleLifecycle(w http.ResponseWriter, r *http.Request, id string) {
	ops, ok := bundlesFrom(r.Context())
	if !ok {
		writeNoBundles(w)
		return
	}
	var in substrate.PatchInput
	if err := decodeBody(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	// A bundle PATCH carries a state transition and nothing else: the authored
	// properties are the declaration, maintained by an apply, never patched.
	if len(in.Labels) > 0 || len(in.Annotations) > 0 || len(in.AddFinalizers) > 0 || len(in.RemoveFinalizers) > 0 {
		writeError(w, http.StatusBadRequest, codeBadRequest,
			"a bundle PATCH carries one lifecycle state — "+propBundleDisabled+", "+
				propBundleUninstalled+" or "+propBundlePurging)
		return
	}
	if len(in.Properties) != 1 {
		writeError(w, http.StatusBadRequest, codeBadRequest,
			"a bundle PATCH sets exactly one lifecycle state — "+propBundleDisabled+", "+
				propBundleUninstalled+" or "+propBundlePurging)
		return
	}
	if _, ok := h.bundleLifecycleGate(w, r, ops); !ok {
		return
	}
	ctx := r.Context()
	switch {
	case has(in.Properties, propBundleDisabled):
		var disable bool
		switch v := in.Properties[propBundleDisabled].(type) {
		case bool:
			disable = v
		case nil:
			// Clearing the flag enables, the PATCH-null-deletes convention.
			disable = false
		default:
			writeError(w, http.StatusBadRequest, codeBadRequest, propBundleDisabled+" is a bool")
			return
		}
		var err error
		if disable {
			err = ops.DisableBundle(ctx, id)
		} else {
			err = ops.EnableBundle(ctx, id)
		}
		if err != nil {
			writeSubstrateError(w, err)
			return
		}
		st, err := ops.BundleStatus(ctx, id)
		if err != nil {
			writeSubstrateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	case has(in.Properties, propBundleUninstalled):
		if v, ok := in.Properties[propBundleUninstalled].(bool); !ok || !v {
			writeError(w, http.StatusBadRequest, codeBadRequest, propBundleUninstalled+" transitions only to true")
			return
		}
		if err := ops.UninstallBundle(ctx, id); err != nil {
			writeSubstrateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"uninstalled": true})
	case has(in.Properties, propBundlePurging):
		if v, ok := in.Properties[propBundlePurging].(bool); !ok || !v {
			writeError(w, http.StatusBadRequest, codeBadRequest, propBundlePurging+" transitions only to true")
			return
		}
		purged, err := ops.PurgeBundle(ctx, id)
		if err != nil {
			writeSubstrateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"purged": purged})
	default:
		writeError(w, http.StatusBadRequest, codeBadRequest,
			"unknown bundle lifecycle state — set "+propBundleDisabled+", "+
				propBundleUninstalled+" or "+propBundlePurging)
	}
}

// has reports whether a key is present, so a `null` value (enable spelled
// `disabled: null`) is told from an absent key.
func has(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

// postBundleBind points one input at a chosen record, or clears the choice
// (record "" or absent), then answers with the refreshed status so the
// caller sees the resolution it just changed.
func (h *handler) postBundleBind(w http.ResponseWriter, r *http.Request) {
	ops, ok := bundlesFrom(r.Context())
	if !ok {
		writeNoBundles(w)
		return
	}
	if _, ok := h.bundleLifecycleGate(w, r, ops); !ok {
		return
	}
	var body struct {
		Input  string `json:"input"`
		Record string `json:"record"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if body.Input == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "input is required — the declared input name to bind")
		return
	}
	id := pathParam(r, "id")
	if err := ops.BindBundleInput(r.Context(), id, body.Input, body.Record); err != nil {
		writeSubstrateError(w, err)
		return
	}
	st, err := ops.BundleStatus(r.Context(), id)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// getTraitImplementors lists the record types implementing a trait — the
// trait-as-interface query.
func (h *handler) getTraitImplementors(w http.ResponseWriter, r *http.Request) {
	ops, ok := bundlesFrom(r.Context())
	if !ok {
		writeNoBundles(w)
		return
	}
	types, err := ops.TypesImplementing(r.Context(), pathParam(r, "id"))
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kinds": types})
}

// getTraitRecords pages the records of every type implementing a trait —
// the read the console's "account configs" view is built on.
func (h *handler) getTraitRecords(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	first := 0
	if v := r.URL.Query().Get("first"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, codeBadRequest, "first must be a positive integer")
			return
		}
		first = n
	}
	// The trait selector passes through WHOLE: a full identity matches
	// resolved bindings exactly, and a bare name resolves only when unique —
	// cutting an identity down to its local name would let a bundle-local
	// look-alike answer for a core trait.
	page, err := DatasetFrom(ctx).List(ctx, substrate.Query{
		Filter: substrate.Filter{Implements: pathParam(r, "id")},
		First:  first,
		After:  r.URL.Query().Get("after"),
	})
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

type oauthStartRequest struct {
	Record string `json:"record"`
}

// postOAuthStart begins the host connect flow for an account record: the
// response carries the provider consent URL the browser should visit.
func (h *handler) postOAuthStart(w http.ResponseWriter, r *http.Request) {
	ops, ok := bundlesFrom(r.Context())
	if !ok {
		writeNoBundles(w)
		return
	}
	var req oauthStartRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if req.Record == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "an account record id is required")
		return
	}
	url, err := ops.StartOAuth(r.Context(), ActorFrom(r.Context()), req.Record)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url})
}

// getOAuthCallback completes a consent. Unauthenticated: the provider
// redirects the browser here, and the HMAC-signed one-time state is what
// names the repository and the record. It answers with a small self-contained
// return-page, not JSON: the console opens the consent in a tab keeping
// window.opener, and the page postMessages the outcome back and closes,
// falling back to a redirect into the console.
//
// Failures reflect NO provider detail to the browser — only a correlation id
// the operator joins against the server log, where the fixed message + the
// engine's real error are recorded.
func (h *handler) getOAuthCallback(w http.ResponseWriter, r *http.Request) {
	oc, ok := h.svc.(substrate.OAuthCompleter)
	if !ok {
		writeNoBundles(w)
		return
	}
	q := r.URL.Query()
	record, err := oc.CompleteOAuth(r.Context(), q.Get("state"), q.Get("code"))
	if err != nil {
		corr := middleware.GetReqID(r.Context())
		slog.Warn("oauth callback failed", "correlation", corr, "error", err)
		h.writeOAuthReturnPage(w, oauthOutcome{correlation: corr})
		return
	}
	h.writeOAuthReturnPage(w, oauthOutcome{ok: true, record: record})
}

// oauthOutcome is what the return-page reports: success carries the connected
// record id; failure carries only the correlation id.
type oauthOutcome struct {
	ok          bool
	record      string
	correlation string
}

// writeOAuthReturnPage renders the self-contained consent return-page. The
// contract the (deployed) console listens for:
//
//	success: window.opener.postMessage({source:"substrate-oauth", ok:true,
//	         record:"<id>"}, origin); window.close(); fallback
//	         window.location.replace(base+"/registry?connected=<id>")
//	failure: postMessage({source:"substrate-oauth", ok:false,
//	         correlation:"<id>"}, origin); fallback
//	         window.location.replace(base+"/registry?error=<id>")
//
// origin is the console scheme+host (targetOrigin); base is its full URL. With
// no configured console (local dev) origin is "*" and no redirect is rendered
// — the page just says the tab can be closed.
func (h *handler) writeOAuthReturnPage(w http.ResponseWriter, o oauthOutcome) {
	origin := "*"
	base := ""
	if h.consoleURL != "" {
		if u, err := url.Parse(h.consoleURL); err == nil && u.Scheme != "" && u.Host != "" {
			origin = u.Scheme + "://" + u.Host
			base = h.consoleURL
		}
	}

	msg := map[string]any{"source": "substrate-oauth", "ok": o.ok}
	var redirect, heading string
	if o.ok {
		msg["record"] = o.record
		heading = "Connected — you can close this tab."
		if base != "" {
			redirect = base + "/registry?connected=" + url.QueryEscape(o.record)
		}
	} else {
		msg["correlation"] = o.correlation
		heading = "Could not complete the connection (correlation " + o.correlation + ")."
		if base != "" {
			redirect = base + "/registry?error=" + url.QueryEscape(o.correlation)
		}
	}

	// json.Marshal produces safe JS string/object literals — the record and
	// correlation id are interpolated ONLY through it, never raw.
	msgJSON, _ := json.Marshal(msg)
	originJSON, _ := json.Marshal(origin)
	redirectJS := ""
	if redirect != "" {
		rj, _ := json.Marshal(redirect)
		redirectJS = "if(!window.closed){window.location.replace(" + string(rj) + ");}"
	}

	var link string
	if base != "" {
		link = `<p><a href="` + template.HTMLEscapeString(base) + `">Return to the console</a></p>`
	}

	page := `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Substrate — connection</title></head>
<body>
<p>` + template.HTMLEscapeString(heading) + `</p>` + link + `
<script>
(function(){
  var msg = ` + string(msgJSON) + `;
  try { if (window.opener) { window.opener.postMessage(msg, ` + string(originJSON) + `); } } catch (e) {}
  try { window.close(); } catch (e) {}
  setTimeout(function(){ ` + redirectJS + ` }, 150);
})();
</script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(page))
}
