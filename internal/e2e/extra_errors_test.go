package e2e

// The 700 block: the error shapes over the live door. ERR-02 holds the
// wrong-shape routes, ERR-03 an unknown kind and the body caps, and ERR-04
// every code internal/api publishes, each produced by one live request.
//
// Everything ERR-04 writes lives under its own authority, so no case here
// moves a story fixture. It leaves one disabled trigger behind with one
// parked delivery, which is what a reviewer reads to see the `parked` code's
// row; nothing fires it again.

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	xeAuthority = "errors.e2e.example"
	xePackage   = "errors"
	xePkg       = xeAuthority + "/" + xePackage

	xeNoteKind = xePkg + "/note"
	xeNotePath = "/api/v1/" + xeNoteKind

	// xeFaulting is a function whose body raises unconditionally: the call is
	// the 500 `function_failed`, and the trigger over notes that calls it is
	// what parks a delivery for the 409 `parked`.
	xeFaulting = xePkg + "/faulting"
	xeTrigger  = "xe-on-note"

	// maxJSONBody is the cap internal/api holds every JSON request body to,
	// and maxWebhookInline the webhook door's cap on a non-multipart body.
	// Both are 1 MiB; the cases send one byte past them.
	maxJSONBody      = 1 << 20
	maxWebhookInline = 1 << 20
	// maxBlobBody is the blob door's cap on one uploaded blob.
	maxBlobBody = 64 << 20
	// maxConcurrentWebhooks is how many deliveries the webhook door holds at
	// once before it answers 503.
	maxConcurrentWebhooks = 8
)

func init() {
	registerCase(700, "ERR-02", "A wrong method at a route answers 405, a retired route 404",
		"A POST at a record path and a PUT, PATCH or DELETE at the records route answer 405 `bad_request` "+
			"naming the method and the path, and write nothing; the three-segment kind path, which no route "+
			"binds since the collection route went, answers 404 `not_found` for every method.",
		xeCaseWrongShape)
	registerCase(710, "ERR-03", "An unknown kind is a 404 naming it, and the body caps refuse",
		"A read, a write and a list of a kind nobody declared answer 404 `not_found` naming the kind; a "+
			"record body past 1 MiB is refused 400 `bad_request` and writes nothing, and the webhook door past "+
			"1 MiB and the blob door past 64 MiB answer 413.",
		xeCaseUnknownKindAndCaps)
	registerCase(720, "ERR-04", "Every published error code is reachable over the live door",
		"Each code internal/api declares is produced by one live request and answered with its status and "+
			"code; a code with neither a request nor a written reason fails the case.",
		xeCaseEveryCode)
}

// --- the helpers ---------------------------------------------------------

// xeError is the wire error envelope, narrowed to what these cases read.
type xeError struct {
	Error struct {
		Code     string   `json:"code"`
		Message  string   `json:"message"`
		Problems []string `json:"problems"`
	} `json:"error"`
}

// xeRequireError holds one answer to its status and code and returns the
// envelope. A body that is not the envelope is a failure, not a skip.
func xeRequireError(c *C, what string, status int, raw []byte, wantStatus int, wantCode string) xeError {
	c.t.Helper()
	var e xeError
	c.requiref(json.Unmarshal(raw, &e) == nil, "%s answered %d with a body that is not the error envelope: %s", what, status, raw)
	c.requiref(status == wantStatus && e.Error.Code == wantCode,
		"%s answered %d %q, want %d %q: %s", what, status, e.Error.Code, wantStatus, wantCode, raw)
	c.requiref(e.Error.Message != "", "%s answered %d %s with no message", what, status, wantCode)
	return e
}

// xeRaw sends one request with a raw body and explicit headers and returns
// the answer's headers too: Retry-After is part of two codes' contract.
func xeRaw(c *C, hc *http.Client, token, method, path string, body io.Reader, contentLength int64, header map[string]string) (int, http.Header, []byte) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.r.base+path, body)
	c.requiref(err == nil, "building %s %s: %v", method, path, err)
	if contentLength >= 0 {
		req.ContentLength = contentLength
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := hc.Do(req)
	c.requiref(err == nil, "%s %s: %v", method, path, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	c.requiref(err == nil, "reading %s %s: %v", method, path, err)
	c.stepf("`%s %s` answered %d", method, path, resp.StatusCode)
	return resp.StatusCode, resp.Header, raw
}

// xeRequireRetryAfter holds a header to a whole number of seconds, which is
// what a client that obeys it needs.
func xeRequireRetryAfter(c *C, what string, header http.Header) int {
	c.t.Helper()
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	c.requiref(err == nil && seconds >= 1, "%s carries Retry-After %q, want a whole number of seconds", what, header.Get("Retry-After"))
	return seconds
}

// xeNoteDoc declares the kind the record codes write. Each property is
// load-bearing: `subject` is required (the 422), `occurredAt` is what a
// lossy redeclaration drops (the 403 lossy), and `phase` is a state a put
// may not move (the 403 guard).
func xeNoteDoc(withOccurredAt bool) map[string]any {
	props := map[string]any{
		"subject": map[string]any{"type": "string", "required": true, "description": "what the note is about"},
		"phase": map[string]any{
			"type": "state", "states": []string{"draft", "filed"}, "initial": "draft",
			"transitions": []map[string]any{{"from": "draft", "to": "filed"}},
			"description": "where the note sits in its life",
		},
	}
	if withOccurredAt {
		props["occurredAt"] = map[string]any{"type": "datetime", "description": "when the thing the note records happened"}
	}
	return xfDoc("substrate.reamde.dev/core/kind", xeNoteKind, map[string]any{
		"authority":       xeAuthority,
		"package":         xePackage,
		"names":           map[string]any{"singular": "note"},
		"description":     "A note the error-code case writes to reach the record codes.",
		"displayTemplate": "{subject}",
		"properties":      props,
	})
}

func xeApply(c *C, docs ...map[string]any) (int, []byte) {
	c.t.Helper()
	return c.do(http.MethodPost, "/api/v1/vocabulary/apply", map[string]any{"documents": docs}, nil)
}

// xeCountTasks counts the live tasks, so a refused write can be shown to
// have written nothing.
func xeCountTasks(c *C) int {
	c.t.Helper()
	n := c.quietCount(tasksCollection)
	c.requiref(n >= 0, "counting the tasks failed")
	return n
}

// --- ERR-02 --------------------------------------------------------------

func xeCaseWrongShape(c *C) {
	before := xeCountTasks(c)

	// A POST at a record path is not an upsert and not a create: the id is
	// in the URL, so the write is PUT there or POST at the records route.
	path := tasksCollection + "/xe-post-at-id"
	status, raw := c.do(http.MethodPost, path, map[string]any{"properties": map[string]any{"name": "A POST at an id"}}, nil)
	e := xeRequireError(c, "POST at a record path", status, raw, http.StatusMethodNotAllowed, "bad_request")
	c.requiref(e.Error.Message == "POST is not supported at "+path,
		"the 405 says %q, want it to name the method and the path", e.Error.Message)
	status, raw = c.do(http.MethodGet, path, nil, nil)
	xeRequireError(c, "GET after the refused POST", status, raw, http.StatusNotFound, "not_found")
	c.stepf("POST at `%s` answered 405 `bad_request` %q and wrote nothing", path, e.Error.Message)

	// The records route lists and creates; it addresses no record, so no
	// verb that needs one is served there.
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		status, raw = c.do(method, recordsRoute, map[string]any{"kind": taskKind, "properties": map[string]any{"name": "A write at the list route"}}, nil)
		e = xeRequireError(c, method+" at the records route", status, raw, http.StatusMethodNotAllowed, "bad_request")
		c.requiref(e.Error.Message == method+" is not supported at "+recordsRoute,
			"the 405 says %q, want it to name %s and %s", e.Error.Message, method, recordsRoute)
	}
	c.stepf("PUT, PATCH and DELETE at `%s` each answered 405 `bad_request` naming the method and the route", recordsRoute)

	// The three-segment kind path was the collection route. Decision record
	// 0079 folded every list into the records route, so nothing binds the
	// path: every method is the router's 404, never a 405 that would imply
	// some other method works there.
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		status, raw = c.do(method, tasksCollection, map[string]any{"properties": map[string]any{"name": "A write at the kind path"}}, nil)
		e = xeRequireError(c, method+" at the kind path", status, raw, http.StatusNotFound, "not_found")
		c.requiref(e.Error.Message == "no such API path: "+tasksCollection,
			"the 404 says %q, want it to name the path", e.Error.Message)
	}
	c.stepf("GET, POST and PUT at the kind path `%s` each answered 404 `not_found` %q", tasksCollection, "no such API path")

	c.requiref(xeCountTasks(c) == before, "the refused writes changed the task count from %d", before)
	c.stepf("the task list still holds %d records: no refused shape wrote anything", before)
}

// --- ERR-03 --------------------------------------------------------------

func xeCaseUnknownKindAndCaps(c *C) {
	const ghost = "ghosts.e2e.example/ghosts/ghost"
	ghostPath := "/api/v1/" + ghost + "/g1"
	want := "unknown kind " + ghost
	for _, probe := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, ghostPath, nil},
		{http.MethodPut, ghostPath, map[string]any{"properties": map[string]any{"name": "a ghost"}}},
		{http.MethodPatch, ghostPath, map[string]any{"properties": map[string]any{"name": "a ghost"}}},
		{http.MethodGet, listWhere(map[string]any{"kinds": []string{ghost}}), nil},
		{http.MethodPost, recordsRoute, map[string]any{"kind": ghost, "properties": map[string]any{"name": "a ghost"}}},
	} {
		status, raw := c.do(probe.method, probe.path, probe.body, nil)
		e := xeRequireError(c, probe.method+" of an unknown kind", status, raw, http.StatusNotFound, "not_found")
		c.requiref(strings.Contains(e.Error.Message, want), "the 404 says %q, want it to name %q", e.Error.Message, want)
	}
	c.stepf("a GET, PUT and PATCH of a `%s` record, a list narrowed to it and a POST creating one each answered 404 `not_found` %q", ghost, want)

	// The JSON body cap. The refusal is `bad_request` with a 400: the cap
	// reaches the handler as a read error, and every JSON door maps a read
	// error to 400. The blob and webhook doors below say 413.
	oversized := tasksCollection + "/xe-oversized"
	name := strings.Repeat("x", maxJSONBody)
	status, raw := c.do(http.MethodPut, oversized, map[string]any{"properties": map[string]any{"name": name}}, nil)
	e := xeRequireError(c, "a record body past 1 MiB", status, raw, http.StatusBadRequest, "bad_request")
	c.requiref(strings.Contains(e.Error.Message, "request body too large"), "the refusal says %q, want it to name the size", e.Error.Message)
	status, raw = c.do(http.MethodGet, oversized, nil, nil)
	xeRequireError(c, "GET after the refused oversized PUT", status, raw, http.StatusNotFound, "not_found")
	c.stepf("a PUT whose body is past the 1 MiB JSON cap answered 400 `bad_request` %q and wrote nothing", e.Error.Message)

	// The webhook door reads the body before it looks the trigger up, so the
	// cap answers whether or not the trigger exists.
	hook := "/webhooks/" + url.PathEscape(c.r.authority) + "/xe-no-such-trigger"
	body := strings.NewReader(`{"pad":"` + strings.Repeat("x", maxWebhookInline) + `"}`)
	status, _, raw = xeRaw(c, c.r.hc, "", http.MethodPost, hook, body, -1, map[string]string{"Content-Type": "application/json"})
	e = xeRequireError(c, "a webhook body past 1 MiB", status, raw, http.StatusRequestEntityTooLarge, "bad_request")
	c.requiref(strings.Contains(e.Error.Message, "exceeds 1 MiB"), "the 413 says %q, want it to name the cap", e.Error.Message)
	c.stepf("a webhook delivery past 1 MiB answered 413 `bad_request` %q", e.Error.Message)

	// The blob door streams to disk under its own cap, one byte past it.
	blob := io.LimitReader(xeZeros{}, maxBlobBody+1)
	status, _, raw = xeRaw(c, c.r.hc, c.r.token, http.MethodPut, "/api/v1/blobs", blob, maxBlobBody+1, map[string]string{"Content-Type": "application/octet-stream"})
	e = xeRequireError(c, "a blob past 64 MiB", status, raw, http.StatusRequestEntityTooLarge, "bad_request")
	c.requiref(strings.Contains(e.Error.Message, "too large"), "the 413 says %q, want it to name the size", e.Error.Message)
	c.stepf("a blob upload past 64 MiB answered 413 `bad_request` %q", e.Error.Message)
}

// xeZeros is an endless reader of zero bytes, so a 64 MiB body is streamed
// rather than held in memory.
type xeZeros struct{}

func (xeZeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// --- ERR-04 --------------------------------------------------------------

// xeUnreachable names the published codes no request can produce, with the
// reason. It is the same excuse the in-process conformance suite writes
// (internal/testenv/conformance_db_test.go), and a code a request below
// DOES produce must leave it.
var xeUnreachable = map[string]string{
	"internal": "500 is the unexpected fault, so producing one means breaking the server rather than sending a request",
}

// xeCodeCase is one request that must answer one published code.
type xeCodeCase struct {
	code string
	name string
	run  func(c *C)
}

func xeCaseEveryCode(c *C) {
	published := xePublishedCodes(c)

	status, raw := xeApply(c,
		xfDoc("substrate.reamde.dev/core/package", xePkg, map[string]any{
			"authority": xeAuthority, "package": xePackage, "version": 1,
		}),
		xeNoteDoc(true),
		xfDoc("substrate.reamde.dev/core/function", xeFaulting, map[string]any{
			"authority": xeAuthority, "package": xePackage,
			"description": "A body that always raises, for the function_failed and parked codes.",
			"runtime":     "python", "timeout": "PT10S",
			"source": "def main(input, host):\n    raise RuntimeError(\"boom\")\n",
		}),
	)
	c.requiref(status == http.StatusOK, "applying the %s package answered %d: %s", xePkg, status, raw)
	c.stepf("applied `%s`: the `note` kind and the `faulting` function", xePkg)

	cases := xeCodeCases()
	covered := map[string]bool{}
	for _, cc := range cases {
		covered[cc.code] = true
		cc.run(c)
		c.stepf("`%s`: %s", cc.code, cc.name)
	}
	var missing, stale []string
	for code, ident := range published {
		switch {
		case covered[code] && xeUnreachable[code] != "":
			stale = append(stale, fmt.Sprintf("%s (%q)", ident, code))
		case !covered[code] && xeUnreachable[code] == "":
			missing = append(missing, fmt.Sprintf("%s (%q)", ident, code))
		}
	}
	for code := range xeUnreachable {
		_, ok := published[code]
		c.requiref(ok, "the excuse for %q names a code internal/api no longer publishes", code)
	}
	slices.Sort(missing)
	slices.Sort(stale)
	c.requiref(len(missing) == 0, "published codes with no live request and no reason: %s", strings.Join(missing, ", "))
	c.requiref(len(stale) == 0, "codes excused as unreachable that a request above produces: %s", strings.Join(stale, ", "))
	for code, why := range xeUnreachable {
		c.stepf("`%s` is not produced: %s", code, why)
	}
	c.stepf("every one of the %d codes internal/api publishes is produced over the live door or excused with its reason", len(published))
}

func xeCodeCases() []xeCodeCase {
	const lifecycle = xeNotePath + "/lifecycle"
	return []xeCodeCase{{
		code: "conflict",
		name: "a put carrying a stale ifVersion answers 409 naming both versions",
		run: func(c *C) {
			c.putRec(xeNotePath, "lifecycle", map[string]any{"subject": "the first write"})
			c.putRec(xeNotePath, "lifecycle", map[string]any{"subject": "the second write"})
			status, raw := c.do(http.MethodPut, lifecycle, map[string]any{
				"ifVersion": 1, "properties": map[string]any{"subject": "written against a moved version"},
			}, nil)
			xeRequireError(c, "a stale ifVersion", status, raw, http.StatusConflict, "conflict")
			c.requiref(strings.Contains(string(raw), "ifVersion 1, stored 2"), "the conflict does not name both versions: %s", raw)
			rec := c.getRec(xeNotePath, "lifecycle")
			c.requiref(rec.Version == 2 && rec.prop("subject") == "the second write", "the refused put changed the record: %+v", rec)
		},
	}, {
		code: "guard",
		name: "a put that moves a state answers 403, because a transition is a patch's job",
		run: func(c *C) {
			status, raw := c.do(http.MethodPut, lifecycle, map[string]any{
				"properties": map[string]any{"subject": "the second write", "phase": "filed"},
			}, nil)
			xeRequireError(c, "a put moving a state", status, raw, http.StatusForbidden, "guard")
		},
	}, {
		code: "lossy",
		name: "a redeclaration that drops a property with live values answers 403 without a confirmation",
		run: func(c *C) {
			c.putRec(xeNotePath, "dated", map[string]any{"subject": "dated", "occurredAt": "2026-09-08T00:00:00Z"})
			status, raw := xeApply(c, xeNoteDoc(false))
			xeRequireError(c, "a lossy redeclaration", status, raw, http.StatusForbidden, "lossy")
			rec := c.getRec(xeNotePath, "dated")
			c.requiref(rec.prop("occurredAt") != "", "the refused redeclaration dropped the value anyway")
		},
	}, {
		code: "validation",
		name: "a write missing a required property answers 422 naming it",
		run: func(c *C) {
			status, raw := c.do(http.MethodPut, xeNotePath+"/no-subject", map[string]any{
				"properties": map[string]any{"occurredAt": "2026-08-17T09:00:00Z"},
			}, nil)
			e := xeRequireError(c, "a missing required property", status, raw, http.StatusUnprocessableEntity, "validation")
			c.requiref(slices.ContainsFunc(e.Error.Problems, func(p string) bool { return strings.Contains(p, "props.subject") }),
				"the problems %v do not name props.subject", e.Error.Problems)
		},
	}, {
		code: "not_found",
		name: "a record nobody wrote answers 404",
		run: func(c *C) {
			status, raw := c.do(http.MethodGet, xeNotePath+"/never-written", nil, nil)
			xeRequireError(c, "an unknown record", status, raw, http.StatusNotFound, "not_found")
		},
	}, {
		code: "forbidden",
		name: "a generic write of a system kind (a token) answers 403",
		run: func(c *C) {
			status, raw := c.do(http.MethodPost, recordsRoute, map[string]any{
				"kind": "substrate.reamde.dev/core/token", "properties": map[string]any{"label": "forged"},
			}, nil)
			xeRequireError(c, "a forged token write", status, raw, http.StatusForbidden, "forbidden")
		},
	}, {
		code: "bad_request",
		name: "a miscased body key answers 400 naming it, because `ifversion` must not bind to `ifVersion`",
		run: func(c *C) {
			status, raw := c.do(http.MethodPut, xeNotePath+"/miscased", map[string]any{
				"ifversion": 1, "properties": map[string]any{"subject": "a precondition nothing reads"},
			}, nil)
			xeRequireError(c, "a miscased body key", status, raw, http.StatusBadRequest, "bad_request")
			c.requiref(strings.Contains(string(raw), "ifversion"), "the 400 does not name the key: %s", raw)
		},
	}, {
		code: "auth",
		name: "a bearer the substrate never minted answers 401",
		run: func(c *C) {
			status, raw := c.doAs("substrate_tok_never-minted", http.MethodGet, listOf(xeNotePath), nil, nil)
			xeRequireError(c, "an unminted bearer", status, raw, http.StatusUnauthorized, "auth")
		},
	}, {
		code: "rate_limited",
		name: "a second login for one repository inside the door's interval answers 429 with Retry-After",
		run: func(c *C) {
			// A repository name of this case's own: its bucket is untouched,
			// so the first attempt spends it and the second is the 429.
			attempt, err := json.Marshal(map[string]any{"repository": xaName("xerate"), "password": "not-the-password"})
			c.requiref(err == nil, "marshal the attempt: %v", err)
			headers := map[string]string{"Content-Type": "application/json"}
			xeRaw(c, c.r.hc, "", http.MethodPost, "/login", strings.NewReader(string(attempt)), -1, headers)
			status, header, raw := xeRaw(c, c.r.hc, "", http.MethodPost, "/login", strings.NewReader(string(attempt)), -1, headers)
			xeRequireError(c, "a second login inside the interval", status, raw, http.StatusTooManyRequests, "rate_limited")
			xeRequireRetryAfter(c, "the 429", header)
		},
	}, {
		code: "compacted",
		name: "a change cursor below the retention horizon answers 410",
		run: func(c *C) {
			status, raw := c.do(http.MethodGet, "/api/v1/changes?from=-1", nil, nil)
			xeRequireError(c, "a cursor below the horizon", status, raw, http.StatusGone, "compacted")
		},
	}, {
		code: "function_failed",
		name: "a direct call of a raising body answers 500 carrying the exception",
		run: func(c *C) {
			status, raw := c.do(http.MethodPost, xfFunctionPath+url.PathEscape(xeFaulting)+"/call", map[string]any{"input": map[string]any{}}, nil)
			e := xeRequireError(c, "a raising body", status, raw, http.StatusInternalServerError, "function_failed")
			c.requiref(strings.Contains(e.Error.Message, "boom"), "the 500 does not carry the body's exception: %s", e.Error.Message)
		},
	}, {
		code: "parked",
		name: "a retry of a parked delivery that fails again answers 409 naming the error",
		run:  xeParked,
	}, {
		code: "unavailable",
		name: "a webhook delivery past the door's concurrency answers 503 with Retry-After",
		run:  xeWebhookBusy,
	}}
}

// xeParked parks one delivery of a trigger over notes whose callable always
// raises, retries it, and disables the trigger so nothing fires it again.
func xeParked(c *C) {
	c.putTrigger(xeTrigger, map[string]any{
		"enabled":  true,
		"source":   map[string]any{"record": map[string]any{"kinds": []string{xeNoteKind}}},
		"callable": "substrate.reamde.dev/core/function/" + xeFaulting,
	})
	defer func() {
		status, raw := c.do(http.MethodPatch, triggerCollection+"/"+xeTrigger, map[string]any{"properties": map[string]any{"enabled": false}}, nil)
		c.requiref(status == http.StatusOK, "disabling %s answered %d: %s", xeTrigger, status, raw)
	}()
	c.putRec(xeNotePath, "parks", map[string]any{"subject": "a note whose delivery parks"})

	var parked struct {
		Items []struct {
			ID        int64  `json:"id"`
			LastError string `json:"lastError"`
		} `json:"items"`
	}
	parkedPath := triggerCollection + "/" + xeTrigger + "/parked"
	c.waitFor("the note's delivery parked", func() bool {
		return c.r.fetch(parkedPath, &parked) == nil && len(parked.Items) > 0
	})
	status, raw := c.do(http.MethodPost, fmt.Sprintf("%s/%d/retry", parkedPath, parked.Items[0].ID), map[string]any{}, nil)
	e := xeRequireError(c, "a retry that fails again", status, raw, http.StatusConflict, "parked")
	c.requiref(strings.Contains(e.Error.Message, "boom"), "the 409 does not name the body's error: %s", e.Error.Message)
}

// xeWebhookBusy fills the webhook door with deliveries whose bodies never
// finish arriving, so the next one is turned away, then releases them and
// waits for the door to reopen: the case leaves no slot held. The held
// deliveries are raw connections, because net/http's client buffers a
// request until its body is whole, and a body that never completes would
// never leave the client.
func xeWebhookBusy(c *C) {
	hook := "/webhooks/" + url.PathEscape(c.r.authority) + "/xe-no-such-trigger"
	var held []net.Conn
	release := func() {
		for _, conn := range held {
			_ = conn.Close()
		}
		held = nil
	}
	defer release()
	for range maxConcurrentWebhooks {
		conn := xeDial(c)
		held = append(held, conn)
		// Declared longer than what is sent, so the handler holds its slot
		// reading a body that never completes.
		_, err := fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: 1024\r\n\r\n{", hook, xeHost(c))
		c.requiref(err == nil, "writing a held delivery: %v", err)
	}
	c.stepf("opened %d webhook deliveries whose bodies stop after one byte", maxConcurrentWebhooks)

	probe := func() (int, http.Header, []byte) {
		return xeRaw(c, c.r.hc, "", http.MethodPost, hook, strings.NewReader(`{}`), -1, map[string]string{"Content-Type": "application/json"})
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		status, header, raw := probe()
		if status == http.StatusServiceUnavailable {
			xeRequireError(c, "a delivery past the door's concurrency", status, raw, http.StatusServiceUnavailable, "unavailable")
			xeRequireRetryAfter(c, "the 503", header)
			break
		}
		c.requiref(time.Now().Before(deadline), "%d held deliveries never filled the webhook door: the probe still answers %d: %s", maxConcurrentWebhooks, status, raw)
		time.Sleep(100 * time.Millisecond)
	}

	release()
	deadline = time.Now().Add(15 * time.Second)
	for {
		status, _, raw := probe()
		if status != http.StatusServiceUnavailable {
			xeRequireError(c, "a delivery after the door reopened", status, raw, http.StatusNotFound, "not_found")
			return
		}
		c.requiref(time.Now().Before(deadline), "the webhook door stayed busy after the held deliveries were released")
		time.Sleep(100 * time.Millisecond)
	}
}

// xeHost is the server's host:port as the run's base URL names it, with the
// scheme's default port filled in.
func xeHost(c *C) string {
	c.t.Helper()
	u, err := url.Parse(c.r.base)
	c.requiref(err == nil && u.Host != "", "the server URL %q has no host: %v", c.r.base, err)
	if u.Port() != "" {
		return u.Host
	}
	if u.Scheme == "https" {
		return net.JoinHostPort(u.Hostname(), "443")
	}
	return net.JoinHostPort(u.Hostname(), "80")
}

// xeDial opens one connection to the server, over TLS when the run's URL is
// https.
func xeDial(c *C) net.Conn {
	c.t.Helper()
	host := xeHost(c)
	d := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if strings.HasPrefix(c.r.base, "https://") {
		name, _, _ := net.SplitHostPort(host)
		conn, err = tls.DialWithDialer(d, "tcp", host, &tls.Config{ServerName: name, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.Dial("tcp", host)
	}
	c.requiref(err == nil, "dialing %s: %v", host, err)
	return conn
}

// xePublishedCodes reads the closed error-code set out of internal/api's
// source, as the in-process conformance suite does, so a code added there
// with no request here fails this case rather than going unasked. Every
// `code*` const or var in a non-test file counts, and one whose value is not
// a plain string literal fails the case: skipping it would be a silent miss.
func xePublishedCodes(c *C) map[string]string {
	c.t.Helper()
	dir := filepath.Join("..", "api")
	entries, err := os.ReadDir(dir)
	c.requiref(err == nil, "reading %s, where the published codes are declared: %v", dir, err)
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, src, nil, 0)
		c.requiref(err == nil, "parsing %s: %v", src, err)
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range vs.Names {
					if !strings.HasPrefix(ident.Name, "code") {
						continue
					}
					c.requiref(i < len(vs.Values), "%s: %s carries no value this case can read", src, ident.Name)
					lit, ok := vs.Values[i].(*ast.BasicLit)
					c.requiref(ok && lit.Kind == token.STRING, "%s: %s is not a string literal", src, ident.Name)
					value, err := strconv.Unquote(lit.Value)
					c.requiref(err == nil, "%s: %s: %v", src, ident.Name, err)
					out[value] = ident.Name
				}
			}
		}
	}
	c.requiref(len(out) > 0, "%s declares no code* identifier: has the set moved?", dir)
	return out
}
