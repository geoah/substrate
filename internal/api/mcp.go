package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/geoah/substrate/internal/mcp"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

// The MCP door (decision 0230): where an assistant such as Claude Code or
// Codex reaches the repository, with the `mcp` token it signed in for.
const (
	mcpPath = "/mcp"
	// mcpConsolePage is where a browser opening the door is sent: the
	// console's page that says how to connect an assistant.
	mcpConsolePage = "/settings/assistants"
	// maxMCPBody admits the largest request a tool forwards, a vocabulary
	// apply, plus its JSON-RPC envelope.
	maxMCPBody = maxVocabularyApplyBody + 64<<10
)

// mcpRoutes are the method and route patterns the door's tools request, as
// the router matched them: an assistant's token reaches these and nothing
// else, whatever path a tool built from what the model sent.
var mcpRoutes = func() map[string]bool {
	core, llm := "/api/"+APIVersion+"/"+corePackage, "/api/"+APIVersion+"/"+llmPackage
	v := "/api/" + APIVersion
	out := map[string]bool{}
	for _, route := range []string{
		"GET " + v + recordsRoute,
		"POST " + v + recordsRoute,
		"GET " + v + "/{a1}/{a2}/{a3}/{a4}",
		"PUT " + v + "/{a1}/{a2}/{a3}/{a4}",
		"PATCH " + v + "/{a1}/{a2}/{a3}/{a4}",
		"DELETE " + v + "/{a1}/{a2}/{a3}/{a4}",
		"GET " + v + "/changes",
		"POST " + v + "/vocabulary/apply",
		"POST " + v + "/vocabulary/plan",
		"GET " + v + "/vocabulary/upgrade",
		"GET " + v + "/catalog",
		"GET " + v + "/catalog/{id}",
		"POST " + v + "/catalog/{id}/install",
		"POST " + v + "/catalog/{id}/import",
		"GET " + core + "/bundle/status",
		"GET " + core + "/bundle/{id}/status",
		"POST " + core + "/bundle/{id}/bind",
		"POST " + core + "/function/{name}/call",
		"POST " + core + "/agent/{name}/chat",
		"POST " + llm + "/thread/{id}/stop",
		"GET " + core + "/trigger/status",
		"POST " + core + "/trigger/{id}/wake",
		"POST " + core + "/trigger/{id}/run",
		"POST " + core + "/trigger/{id}/replay",
		"GET " + core + "/trigger/{id}/parked",
		"POST " + core + "/trigger/{id}/parked/{fid}/retry",
		"DELETE " + core + "/trigger/{id}/parked/{fid}",
		"GET " + v + "/sync/status",
		"GET " + v + "/blobs/{digest}",
		"PUT " + v + "/blobs",
		"GET " + v + unitsRoute,
		"GET " + v + unitsConvertRoute,
		"POST " + v + "/merge",
		"POST " + v + "/split",
	} {
		out[route] = true
	}
	return out
}()

// mcpRouteAllowed reports whether the request matched one of mcpRoutes.
func mcpRouteAllowed(r *http.Request) bool {
	rc := chi.RouteContext(r.Context())
	return rc != nil && mcpRoutes[r.Method+" "+rc.RoutePattern()]
}

// mcpResource is the protected resource the door is: its own address, so an
// assistant's token is asked for and minted for it alone.
func (h *handler) mcpResource(r *http.Request) string { return h.issuerOf(r) + mcpPath }

// mcpChallenge is the challenge a refusal at the door carries: where to read
// the door's metadata, and the one scope that reaches it, so a client that
// knows no better asks for `mcp` alone (both clients seen ask for every
// scope the metadata lists when the challenge names none).
func (h *handler) mcpChallenge(r *http.Request, errCode string) string {
	c := "Bearer "
	if errCode != "" {
		c += `error="` + errCode + `", `
	}
	return c + `scope="` + string(scope.MCP) + `", resource_metadata="` + h.issuerOf(r) + prMetadataPath + mcpPath + `"`
}

// getMCPResourceMetadata is RFC 9728 for the door, at the path-suffixed
// address a client derives from `<issuer>/mcp`.
func (h *handler) getMCPResourceMetadata(w http.ResponseWriter, r *http.Request) {
	iss := h.issuerOf(r)
	writeJSON(w, http.StatusOK, protectedResourceMetadata{
		Resource:               h.mcpResource(r),
		AuthorizationServers:   []string{iss},
		BearerMethodsSupported: []string{"header"},
		ScopesSupported:        []string{string(scope.MCP)},
		ResourceName:           "substrate",
	})
}

// serveMCP is the door. Every method lands here, so a wrong one is an
// answer of the door's and never the console's shell: a bearer-less request
// is the 401 that starts a client's sign-in (Codex opens with a GET), a
// browser is sent to the console's page, and with a token only POST is
// served, since there is no server stream and no session to end.
func (h *handler) serveMCP(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, h.issuerOf(r)) {
		writeError(w, http.StatusForbidden, codeForbidden, "requests from another origin are refused here")
		return
	}
	if r.Method == http.MethodGet && bearerSecret(r) == "" && wantsHTML(r) {
		http.Redirect(w, r, h.consoleURL+mcpConsolePage, http.StatusSeeOther)
		return
	}
	ctx, status, code, msg, cause := h.authenticate(r.WithContext(withMCPDoor(r.Context())))
	if ctx == nil {
		switch status {
		case http.StatusServiceUnavailable:
			writeUnavailableCause(w, r, cause, substrate.ErrorPayload{Code: codeUnavailable, Message: msg})
		case http.StatusUnauthorized:
			errCode := ""
			if bearerSecret(r) != "" {
				errCode = "invalid_token"
			}
			w.Header().Set("WWW-Authenticate", h.mcpChallenge(r, errCode))
			writeError(w, status, code, msg)
		default:
			writeError(w, status, code, msg)
		}
		return
	}
	if _, assistant := DatasetFrom(ctx).(*assistantDataset); !assistant {
		w.Header().Set("WWW-Authenticate", h.mcpChallenge(r, "insufficient_scope"))
		writeError(w, http.StatusForbidden, codeForbidden,
			"this address takes an assistant's token: sign in again and approve the assistant's access")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, codeBadRequest,
			r.Method+" is not served at "+mcpPath+": it holds no session and opens no stream")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMCPBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, codeBadRequest, "the message is larger than this server accepts")
			return
		}
		writeError(w, http.StatusBadRequest, codeBadRequest, "the message could not be read")
		return
	}
	api := &doorAPI{h: h, outer: r}
	status, out := h.mcp.Handle(ctx, api, body, r.Header.Get("MCP-Protocol-Version"))
	if out == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, status, json.RawMessage(out))
}

// wantsHTML reports whether a request is a browser's navigation.
func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// sameOrigin reports whether an Origin header names the issuer's origin.
func sameOrigin(origin, issuer string) bool {
	o, err1 := url.Parse(origin)
	i, err2 := url.Parse(issuer)
	return err1 == nil && err2 == nil && strings.EqualFold(o.Scheme, i.Scheme) && strings.EqualFold(o.Host, i.Host)
}

// doorAPI answers a tool's REST request in process: through this server's
// own router, with the caller's own bearer, marked as the door's, so the
// request meets every check a REST request meets plus the door's route list.
type doorAPI struct {
	h     *handler
	outer *http.Request
}

func (d *doorAPI) Do(ctx context.Context, req mcp.Request) mcp.Response {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	target := "/api/" + APIVersion + req.Path
	if len(req.Query) > 0 {
		target += "?" + req.Query.Encode()
	}
	var body io.Reader
	ctype := ""
	switch {
	case req.Raw != nil:
		body, ctype = bytes.NewReader(req.Raw), req.ContentType
	case req.Body != nil:
		b, err := json.Marshal(req.Body)
		if err != nil {
			return failed(http.StatusBadRequest, codeBadRequest, "the request body is not JSON: "+err.Error())
		}
		body, ctype = bytes.NewReader(b), "application/json"
	}
	// The request is routed afresh: chi reads a route context already on the
	// context as a sub-router's and would route it on the door's own path.
	ctx = context.WithValue(withMCPDoor(ctx), chi.RouteCtxKey, (*chi.Context)(nil))
	in, err := http.NewRequestWithContext(ctx, req.Method, target, body)
	if err != nil {
		return failed(http.StatusBadRequest, codeBadRequest, "the request could not be made: "+err.Error())
	}
	in.Host, in.RemoteAddr = d.outer.Host, d.outer.RemoteAddr
	in.Header.Set("Authorization", d.outer.Header.Get("Authorization"))
	in.Header.Set("Accept", "application/json")
	if ctype != "" {
		in.Header.Set("Content-Type", ctype)
	}
	if req.IdempotencyKey != "" {
		in.Header.Set("Idempotency-Key", req.IdempotencyKey)
	}
	rec := &bufferedResponse{header: http.Header{}}
	d.h.router.ServeHTTP(rec, in)
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	return mcp.Response{Status: rec.status, Header: rec.header, Body: rec.body.Bytes()}
}

// failed is an answer the door makes itself, shaped as the API's errors.
func failed(status int, code, msg string) mcp.Response {
	b, _ := json.Marshal(substrate.ErrorEnvelope{Error: substrate.ErrorPayload{Code: code, Message: msg}})
	return mcp.Response{Status: status, Header: http.Header{}, Body: b}
}

// bufferedResponse holds an in-process answer. It flushes, as a streaming
// handler requires, by doing nothing: the stream is read once it ends.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

func (b *bufferedResponse) Flush() {}
