package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// testNow is the fixed clock every golden table renders against.
var testNow = time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

// fakeSubstrate implements the REST subset the CLI talks to (the contract
// "REST wire contract"): the kind registry, one vocabulary collection,
// tokens, changes.
type fakeSubstrate struct {
	mu      sync.Mutex
	records map[string]*substrate.Record

	// formerIDs is the `former_ids` trail a merge left behind: former id → the
	// id the record now lives under. Flattened, so one hop always suffices. A
	// read addressed at a key in here returns the canonical record with
	// `canonicalId` set — the only time that field is on the wire at all.
	formerIDs map[string]string

	// propertyMeta is the managed-property block per record, served beside the
	// record's own fields on a single-record GET and NEVER on a list — the
	// asymmetry is the contract, so the fake keeps it.
	propertyMeta map[string]map[string]statusProperty

	// incoming is the reverse-edge block per record, served on a
	// single-record GET and NEVER on a list, exactly like propertyMeta.
	incoming map[string][]substrate.IncomingEdge

	// extraTypes are registry rows appended to fakeRegistry on the types read.
	// Empty by default so the golden `types` table is unaffected; the
	// shipped-example apply test seeds the `trigger` type here so the real
	// resolver can route triggers.yaml.
	extraTypes []map[string]any

	// authStatus, when non-zero, makes the door (register, login, the
	// credential changes, mint) fail with it.
	authStatus int
	// retryAfter is the Retry-After a 429 carries; empty means 5 seconds.
	retryAfter string
	// paceRegisterOnce refuses the FIRST registration commit with a paced 429,
	// the way the door's rate limiter does when the enrollment before it was a
	// second ago.
	paceRegisterOnce bool
	// revokeStatus, when non-zero, makes DELETE /tokens/{id} fail with it.
	revokeStatus int
	// totpDisabled makes GET /.well-known/substrate/server.json answer
	// `registration.totpRequired: false` — the local substrate that verifies
	// no second factor.
	totpDisabled bool
	// changes is the ndjson watch payload (one substrate.Change per row).
	changes []substrate.Change

	requests []string
	lastBody map[string]json.RawMessage
	lastAuth string
}

func newFake(t *testing.T) (*fakeSubstrate, *httptest.Server) {
	t.Helper()
	f := &fakeSubstrate{
		records:      map[string]*substrate.Record{},
		formerIDs:    map[string]string{},
		propertyMeta: map[string]map[string]statusProperty{},
		incoming:     map[string][]substrate.IncomingEdge{},
	}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeSubstrate) seed(e *substrate.Record) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[e.ID] = e
}

// seedMeta records the propertyMeta a single-record GET of id will carry.
func (f *fakeSubstrate) seedMeta(id string, meta map[string]statusProperty) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.propertyMeta[id] = meta
}

// seedIncoming records a legacy incoming block on a single-record GET.
func (f *fakeSubstrate) seedIncoming(id string, in []substrate.IncomingEdge) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.incoming[id] = in
}

func (f *fakeSubstrate) record(id string) *substrate.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.records[id]
}

func (f *fakeSubstrate) noteRequest(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.lastAuth = r.Header.Get("Authorization")
	f.lastBody = nil
	if r.Body != nil {
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) == nil {
			f.lastBody = body
		}
	}
}

// doorRequests is the recorded traffic MINUS the discovery probe: the door
// reads GET /.well-known/substrate/server.json before it asks a person for a
// code, and a test about the registration gesture is not about that read.
// The probe itself is asserted where it decides something.
func (f *fakeSubstrate) doorRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.requests))
	for _, r := range f.requests {
		if r != "GET /.well-known/substrate/server.json" {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeSubstrate) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/substrate/server.json", f.handleDiscovery)
	mux.HandleFunc("GET "+typesPath, f.handleTypes)
	mux.HandleFunc("POST /api/v1/vocabulary/apply", f.handleVocabularyApply)
	mux.HandleFunc("GET /api/v1/changes", f.handleChanges)
	// The door, BESIDE the versioned API and outside every prefix: no
	// repository segment anywhere, because registration has none yet and
	// everything after it takes one from the token.
	mux.HandleFunc("POST /register/enroll", f.handleRegisterEnroll)
	mux.HandleFunc("POST /register", f.handleRegister)
	mux.HandleFunc("POST /login", f.handleLogin)
	mux.HandleFunc("POST /password", f.handlePasswordChange)
	mux.HandleFunc("POST /totp/enroll", f.handleTOTPEnroll)
	mux.HandleFunc("POST /totp", f.handleTOTPReenroll)
	mux.HandleFunc("POST /tokens", f.handleMint)
	mux.HandleFunc("GET /tokens", f.handleTokens)
	mux.HandleFunc("DELETE /tokens/{id}", f.handleRevoke)
	mux.HandleFunc("GET "+tasksPath, f.handleList)
	mux.HandleFunc("POST "+tasksPath, f.handlePost)
	mux.HandleFunc("GET "+tasksPath+"/{id}", f.handleGet)
	mux.HandleFunc("PUT "+tasksPath+"/{id}", f.handlePut)
	mux.HandleFunc("PATCH "+tasksPath+"/{id}", f.handlePatch)
	mux.HandleFunc("DELETE "+tasksPath+"/{id}", f.handleDelete)
	// The shipped-example apply test writes triggers as ordinary data records
	// in core.substrate.reamde.dev; a generic collection store answers those puts.
	mux.HandleFunc("GET "+triggerColPath+"/{id}", f.handleTriggerGet)
	mux.HandleFunc("PUT "+triggerColPath+"/{id}", f.handleTriggerPut)
	// The trigger DELIVERY verbs, at the resource: they hang off
	// core.substrate.reamde.dev and NOWHERE else here, so a client still riding the
	// retired automation.substrate.reamde.dev spelling falls through to the 404 catch-all
	// rather than passing quietly.
	mux.HandleFunc("GET "+triggerColPath+"/status", f.handleTriggerStatus)
	mux.HandleFunc("POST "+triggerColPath+"/{id}/run", f.handleTriggerRun)
	mux.HandleFunc("POST "+triggerColPath+"/{id}/wake", f.handleTriggerWake)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.noteRequest(r)
		writeError(w, http.StatusNotFound, "not_found", "no such route: "+r.URL.Path, nil)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string, problems []string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"code": code, "message": msg, "problems": problems,
	}})
}

// The two paths the tests spell out: the registry is `kinds` (naming
// scheme R4 — `type` is a column on every record), and the one data collection
// lives in `tasks.substrate.reamde.dev`, since the shipped vocabulary is split by subject
// domain rather than gathered under a single `vocab` authority.
// The collection segment is the kind NAME (decision 0033): the registry is the
// `kind` kind, the one data collection is `task`, and the trigger records and
// their delivery verbs hang off `trigger`.
const (
	typesPath      = "/api/v1/core.substrate.reamde.dev/kind"
	tasksPath      = "/api/v1/tasks.substrate.reamde.dev/task"
	triggerColPath = "/api/v1/core.substrate.reamde.dev/trigger"
)

func typeRecord(name, authority, plural, source string, definition map[string]any) map[string]any {
	properties := map[string]any{
		"name": name, "authority": authority, "plural": plural,
		"version": 1, "source": source,
	}
	if definition != nil {
		properties["definition"] = definition
	}
	return map[string]any{
		"id":         authority + "/" + name,
		"kind":       "core.substrate.reamde.dev/kind",
		"properties": properties,
	}
}

func builtin(name, authority, plural string) map[string]any {
	return typeRecord(name, authority, plural, "builtin", nil)
}

func installed(name, authority, plural string) map[string]any {
	return typeRecord(name, authority, plural, "installed", nil)
}

// taskDefinition is the one declaration the fake registry carries, because the
// STATE column has no other source: a state is a property, so `lifecycle: open`
// in a record's properties looks exactly like `detail: rack layout` until the
// declaration says which is which. The name is deliberately NOT the shipped
// `status` — the column reads the declaration, not a word the CLI knows.
var taskDefinition = map[string]any{
	"properties": map[string]any{
		"detail": map[string]any{"type": "string"},
		"lifecycle": map[string]any{
			"type":   "state",
			"states": []string{"open", "done"},
		},
	},
}

// fakeRegistry is a slice of a real repository's registry under the pinned naming
// scheme: the substrate's own machinery in `core`, the shipped vocabulary
// split across `people`/`messaging`/`calendar`/`tasks`, one invented `library`
// vocabulary authority, and two installed connector authorities. Nothing
// shipped is an installed authority any more, so `installed` here means a
// connector, always.
//
// The two connector authorities are what make the ambiguity path real rather than
// hypothetical: every connector installs a type named exactly `syncrun` in its
// own authority, so `syncruns` can never resolve bare — while `people`, `tasks`,
// `calendarevents` and `books` each still belong to exactly one authority and must.
var fakeRegistry = []map[string]any{
	// The `connector`/`connectoraccount` core mirrors were removed at the v1
	// freeze — a connection is an accountconfig-trait
	// record now, so the fake registry no longer advertises those kinds.
	builtin("recordmerge", "core.substrate.reamde.dev", "recordmerges"),
	builtin("recordsplit", "core.substrate.reamde.dev", "recordsplits"),
	builtin("kind", "core.substrate.reamde.dev", "kinds"),
	builtin("token", "core.substrate.reamde.dev", "tokens"),
	builtin("person", "people.substrate.reamde.dev", "people"),
	builtin("organization", "people.substrate.reamde.dev", "organizations"),
	builtin("conversationmessage", "messaging.substrate.reamde.dev", "conversationmessages"),
	builtin("calendarevent", "calendar.substrate.reamde.dev", "calendarevents"),
	builtin("calendareventseries", "calendar.substrate.reamde.dev", "calendareventseries"),
	typeRecord("task", "tasks.substrate.reamde.dev", "tasks", "builtin", taskDefinition),
	// `library` is the fake's own vocabulary authority (nothing shipped by that
	// name); its five types cover both plural shapes — `books`/`movies`/
	// `podcasts`, whose plural is a word of its own, and `bookseries`/
	// `tvseries`, whose plural is its singular.
	builtin("book", "library.substrate.reamde.dev", "books"),
	builtin("bookseries", "library.substrate.reamde.dev", "bookseries"),
	builtin("movie", "library.substrate.reamde.dev", "movies"),
	builtin("podcast", "library.substrate.reamde.dev", "podcasts"),
	builtin("tvseries", "library.substrate.reamde.dev", "tvseries"),
	installed("syncrun", "google.connectors.substrate.reamde.dev", "syncruns"),
	installed("syncrun", "slack.connectors.substrate.reamde.dev", "syncruns"),
}

// fakeTypesPageSize and fakeTypesMaxPage mirror the engine's defaultPageSize
// and maxPageSize: the registry is an ordinary collection, so it pages like
// one, and a client that ignores the cursor sees only the first page.
const (
	fakeTypesPageSize = 50
	fakeTypesMaxPage  = 500
)

// handleTypes serves the registry the way the collection list does: newest
// first (the engine orders created_at DESC, and an installed bundle's types
// are the newest rows), `first` capped at maxPageSize, and an opaque cursor
// that is absent once the walk is exhausted.
func (f *fakeSubstrate) handleTypes(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	f.mu.Lock()
	registry := append(append([]map[string]any{}, f.extraTypes...), fakeRegistry...)
	f.mu.Unlock()

	first := fakeTypesPageSize
	if raw := r.URL.Query().Get("first"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "first must be a positive integer", nil)
			return
		}
		first = min(n, fakeTypesMaxPage)
	}
	start := 0
	if after := r.URL.Query().Get("after"); after != "" {
		n, err := strconv.Atoi(after)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "unknown cursor "+after, nil)
			return
		}
		start = min(n, len(registry))
	}
	end := min(start+first, len(registry))
	page := map[string]any{"records": registry[start:end], "head": 0}
	if end < len(registry) {
		page["cursor"] = strconv.Itoa(end)
	}
	writeJSON(w, http.StatusOK, page)
}

// handleVocabularyApply is the batch schema verb: it records the documents and
// answers one schema record per document, the way the engine does.
func (f *fakeSubstrate) handleVocabularyApply(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	var req struct {
		Documents []map[string]any `json:"documents"`
	}
	raw, ok := f.lastBody["documents"]
	if !ok || json.Unmarshal(raw, &req.Documents) != nil || len(req.Documents) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation", "no documents", nil)
		return
	}
	ents := make([]*substrate.Record, 0, len(req.Documents))
	for _, d := range req.Documents {
		meta, _ := d["metadata"].(map[string]any)
		id, _ := meta["id"].(string)
		kind, _ := d["kind"].(string)
		ents = append(ents, &substrate.Record{ID: id, Kind: kind, Version: 1})
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": ents})
}

// fakeSecret is what every mint hands back once. The format is the substrate's
// own: `substrate_tok_<hex>`, and nothing about it names a repository.
var fakeSecret = "substrate_tok_" + strings.Repeat("a", 40)

// fakeSigningPublicKey is the changelog-signing PIN registration hands back: 32
// bytes in hex, the shape `repository verify --expect-public-key` takes. Shape,
// not crypto. There is deliberately no fake SEED beside it — no response carries
// private key material any more (#217), and a fake that offered one would let
// the CLI start printing it again with nothing failing.
var fakeSigningPublicKey = strings.Repeat("cd", 32)

// authFails makes the door refuse: the shared 401 (or a 429 with Retry-After)
// every unauthenticated auth path answers with.
func (f *fakeSubstrate) authFails(w http.ResponseWriter) bool {
	switch f.authStatus {
	case 0:
		return false
	case http.StatusTooManyRequests:
		f.paced(w)
	default:
		writeError(w, f.authStatus, "auth", "invalid username, password or code", nil)
	}
	return true
}

// paced is the door's rate-limited refusal, Retry-After and all.
func (f *fakeSubstrate) paced(w http.ResponseWriter) {
	after := f.retryAfter
	if after == "" {
		after = "5"
	}
	w.Header().Set("Retry-After", after)
	writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests", nil)
}

// handleRegisterEnroll issues a TOTP enrollment and writes NOTHING, exactly as
// the substrate does: the caller holds the seed and hands it back with a code.
// handleDiscovery serves the slice of GET /.well-known/substrate/server.json
// the door reads: whether this deployment verifies a second factor at all.
func (f *fakeSubstrate) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"registration": map[string]any{"totpRequired": !f.totpDisabled},
	})
}

func (f *fakeSubstrate) handleRegisterEnroll(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	if f.authFails(w) {
		return
	}
	writeJSON(w, http.StatusOK, substrate.TOTPEnrollment{
		Secret: fakeTOTPSecret, URI: fakeOtpauthURI,
	})
}

func (f *fakeSubstrate) handleRegister(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	if f.paceRegisterOnce {
		f.paceRegisterOnce = false
		f.paced(w)
		return
	}
	if f.authFails(w) {
		return
	}
	var label string
	_ = json.Unmarshal(f.lastBody["label"], &label)
	// Echo the recovery half the way the server does: the client-generated
	// recipient comes straight back, and nothing mints server-side when one
	// was supplied.
	var recipient string
	_ = json.Unmarshal(f.lastBody["recoveryPublicKey"], &recipient)
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":             substrate.TokenInfo{ID: "tk01", Label: label, Created: testNow},
		"secret":            fakeSecret,
		"recoveryPublicKey": recipient,
		"signingPublicKey":  fakeSigningPublicKey,
	})
}

// handleLogin mints a token record and hands back its secret once. There is no
// session beside it — a login IS a mint.
func (f *fakeSubstrate) handleLogin(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	if f.authFails(w) {
		return
	}
	var label string
	_ = json.Unmarshal(f.lastBody["label"], &label)
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":  substrate.TokenInfo{ID: "tk01", Label: label, Created: testNow},
		"secret": fakeSecret,
	})
}

// handlePasswordChange enforces the password-factor rule the way the substrate
// does: a request without both current factors in the BODY is refused with 403,
// whatever bearer token it carries.
func (f *fakeSubstrate) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	if !f.factorsPresented(w) {
		return
	}
	if f.authFails(w) {
		return
	}
	var username string
	_ = json.Unmarshal(f.lastBody["username"], &username)
	writeJSON(w, http.StatusOK, map[string]string{"username": username})
}

func (f *fakeSubstrate) handleTOTPEnroll(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	if !f.factorsPresented(w) {
		return
	}
	if f.authFails(w) {
		return
	}
	writeJSON(w, http.StatusOK, substrate.TOTPEnrollment{
		Secret: fakeTOTPSecret, URI: fakeOtpauthURI,
	})
}

func (f *fakeSubstrate) handleTOTPReenroll(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	if !f.factorsPresented(w) {
		return
	}
	if f.authFails(w) {
		return
	}
	var username string
	_ = json.Unmarshal(f.lastBody["username"], &username)
	writeJSON(w, http.StatusOK, map[string]string{"username": username})
}

func (f *fakeSubstrate) factorsPresented(w http.ResponseWriter) bool {
	var password, code string
	_ = json.Unmarshal(f.lastBody["password"], &password)
	_ = json.Unmarshal(f.lastBody["totpCode"], &code)
	if password != "" && code != "" {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden",
		"changing auth material requires the current password and TOTP code in the request body — a bearer token is not accepted", nil)
	return false
}

func (f *fakeSubstrate) handleMint(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	if f.authStatus != 0 {
		writeError(w, f.authStatus, "auth", "invalid token", nil)
		return
	}
	var body struct {
		Label     string     `json:"label"`
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	_ = json.Unmarshal(f.lastBody["label"], &body.Label)
	_ = json.Unmarshal(f.lastBody["expiresAt"], &body.ExpiresAt)
	writeJSON(w, http.StatusCreated, map[string]any{
		"token": substrate.TokenInfo{
			ID: "tk02", Label: body.Label, Created: testNow, ExpiresAt: body.ExpiresAt,
		},
		"secret": fakeSecret,
	})
}

func (f *fakeSubstrate) handleTokens(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	expires := testNow.Add(720 * time.Hour)
	writeJSON(w, http.StatusOK, map[string]any{"items": []substrate.TokenInfo{
		{ID: "tk01", Label: "substratectl@laptop", Created: testNow.Add(-48 * time.Hour)},
		{ID: "tk02", Label: "backup-script", Created: testNow.Add(-time.Hour), ExpiresAt: &expires},
	}})
}

func (f *fakeSubstrate) handleRevoke(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	if f.revokeStatus != 0 {
		writeError(w, f.revokeStatus, "not_found", "no such token", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id")})
}

// fakeOtpauthURI and fakeTOTPSecret are the enrollment the door issues once.
const (
	fakeTOTPSecret = "JBSWY3DPEHPK3PXP"
	fakeOtpauthURI = "otpauth://totp/Substrate%20Substrate:geoah?secret=JBSWY3DPEHPK3PXP&issuer=Substrate+Substrate&algorithm=SHA1&digits=6&period=30"
)

// rejectUnknown answers 400 the way the real server's strict decode does when
// the recorded body carries a key outside `allowed`. It reports whether the
// handler may continue.
func (f *fakeSubstrate) rejectUnknown(w http.ResponseWriter, route string, allowed ...string) bool {
	ok := map[string]bool{}
	for _, k := range allowed {
		ok[k] = true
	}
	for k := range f.lastBody {
		if !ok[k] {
			writeError(w, http.StatusBadRequest, "bad_request",
				"unknown field "+strconv.Quote(k), nil)
			return false
		}
	}
	return true
}

func (f *fakeSubstrate) handleChanges(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]any{"bookmark": 41})
	for _, c := range f.changes {
		_ = enc.Encode(c)
	}
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
}

func (f *fakeSubstrate) handleList(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("watch") == "1" {
		f.handleChanges(w, r)
		return
	}
	f.noteRequest(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*substrate.Record, 0, len(f.records))
	for _, e := range f.records {
		out = append(out, e)
	}
	sortRecords(out)
	writeJSON(w, http.StatusOK, map[string]any{"records": out})
}

// mergeInto records that `former` was merged away into `canonical`: the trail
// that makes the id still resolve, and the winner's own side of it — the
// `formerIds` a read carries in status.
func (f *fakeSubstrate) mergeInto(former, canonical string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.formerIDs[former] = canonical
	if e := f.records[canonical]; e != nil {
		e.FormerIDs = append(e.FormerIDs, former)
	}
}

func (f *fakeSubstrate) formerID(id string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	canonical, ok := f.formerIDs[id]
	return canonical, ok
}

func (f *fakeSubstrate) handleGet(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	id := r.PathValue("id")
	canonical, merged := f.formerID(id)
	if merged {
		id = canonical
	}
	e := f.record(id)
	if e == nil {
		writeError(w, http.StatusNotFound, "not_found", "no record "+id, nil)
		return
	}
	f.mu.Lock()
	meta := f.propertyMeta[id]
	inc := f.incoming[id]
	f.mu.Unlock()
	if !merged && meta == nil && inc == nil {
		writeJSON(w, http.StatusOK, e)
		return
	}
	// Detail-only fields ride alongside the record's own fields in this fake.
	// Incoming is included only to exercise compatibility with an older server.
	body, _ := json.Marshal(e)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	if merged {
		out["canonicalId"] = canonical
	}
	if meta != nil {
		out["propertyMeta"] = meta
	}
	if inc != nil {
		out["incoming"] = inc
	}
	writeJSON(w, http.StatusOK, out)
}

// putTitle reads the title out of a write's property map, which is where
// everything authored lives: `title` is a property with a column behind it,
// not a field of its own.
func putTitle(in substrate.PutInput) (string, bool) {
	s, ok := in.Properties["title"].(string)
	return s, ok
}

// handlePut applies the write with v4-style no-op suppression: an identical
// title leaves the version and updatedAt untouched.
func (f *fakeSubstrate) handlePut(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	var in substrate.PutInput
	_ = json.Unmarshal(mustRaw(f.lastBody), &in)
	f.mu.Lock()
	defer f.mu.Unlock()
	id := r.PathValue("id")
	title, hasTitle := putTitle(in)
	e, ok := f.records[id]
	if !ok {
		e = &substrate.Record{
			ID: id, Kind: "tasks.substrate.reamde.dev/task", Properties: map[string]any{},
			Labels: map[string]any{}, Version: 1,
			CreatedAt: testNow.Add(-time.Hour), UpdatedAt: testNow.Add(-time.Hour),
		}
		if hasTitle {
			e.Properties["title"] = title
		}
		f.records[id] = e
		writeJSON(w, http.StatusCreated, e)
		return
	}
	if hasTitle && title != e.Properties["title"] {
		e.Properties["title"] = title
		e.Version++
		e.UpdatedAt = testNow
	}
	writeJSON(w, http.StatusOK, e)
}

// handlePatch merges the properties key-wise, which is the whole of what the
// CLI asks of it: a state transition is a property write like any other, and
// the guards that make it more than that live in the engine.
func (f *fakeSubstrate) handlePatch(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	var in substrate.PatchInput
	_ = json.Unmarshal(mustRaw(f.lastBody), &in)
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.records[r.PathValue("id")]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no record "+r.PathValue("id"), nil)
		return
	}
	if e.Properties == nil {
		e.Properties = map[string]any{}
	}
	for k, v := range in.Properties {
		if v == nil {
			delete(e.Properties, k)
			continue
		}
		e.Properties[k] = v
	}
	e.Version++
	e.UpdatedAt = testNow
	writeJSON(w, http.StatusOK, e)
}

func (f *fakeSubstrate) handlePost(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	var in substrate.PutInput
	_ = json.Unmarshal(mustRaw(f.lastBody), &in)
	f.mu.Lock()
	defer f.mu.Unlock()
	id := fmt.Sprintf("gen%02d", len(f.records)+1)
	e := &substrate.Record{
		ID: id, Kind: "tasks.substrate.reamde.dev/task", Properties: map[string]any{},
		Labels: map[string]any{}, Version: 1, CreatedAt: testNow, UpdatedAt: testNow,
	}
	if title, ok := putTitle(in); ok {
		e.Properties["title"] = title
	}
	f.records[id] = e
	writeJSON(w, http.StatusCreated, e)
}

// handleTriggerGet answers a single trigger read: 404 until one is applied,
// the stored record afterward — the read `substratectl apply` does to decide
// created vs updated.
func (f *fakeSubstrate) handleTriggerGet(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	e := f.record(r.PathValue("id"))
	if e == nil {
		writeError(w, http.StatusNotFound, "not_found", "no trigger "+r.PathValue("id"), nil)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// handleTriggerPut stores one trigger data record, mirroring the task put's
// create/update accounting so apply prints the right verb.
func (f *fakeSubstrate) handleTriggerPut(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	var in substrate.PutInput
	_ = json.Unmarshal(mustRaw(f.lastBody), &in)
	f.mu.Lock()
	defer f.mu.Unlock()
	id := r.PathValue("id")
	if e, ok := f.records[id]; ok {
		e.Properties = in.Properties
		e.Version++
		e.UpdatedAt = testNow
		writeJSON(w, http.StatusOK, e)
		return
	}
	e := &substrate.Record{
		ID: id, Kind: "core.substrate.reamde.dev/trigger", Properties: in.Properties,
		Labels: map[string]any{}, Version: 1, CreatedAt: testNow, UpdatedAt: testNow,
	}
	f.records[id] = e
	writeJSON(w, http.StatusCreated, e)
}

// handleTriggerStatus answers the computed per-trigger status table.
func (f *fakeSubstrate) handleTriggerStatus(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	writeJSON(w, http.StatusOK, map[string]any{"items": []substrate.TriggerStatus{{
		ID: "classify-page", Kind: substrate.TriggerKindRecord,
		Callable: "web.substrate.reamde.dev/classify", Enabled: true, Cursor: 41, Head: 41,
	}}})
}

// handleTriggerRun is the synthesized single delivery — STRICT, like the real
// server: a record is addressed by (kind, id), so a body missing either half
// is a bad_request there and must be one here. A permissive fake is how substratectl
// shipped an id-only body that 400d against every real substrate.
func (f *fakeSubstrate) handleTriggerRun(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	if !f.rejectUnknown(w, "trigger run", "kind", "id") {
		return
	}
	var req struct{ Kind, ID string }
	_ = json.Unmarshal(f.lastBody["kind"], &req.Kind)
	_ = json.Unmarshal(f.lastBody["id"], &req.ID)
	if req.Kind == "" || req.ID == "" {
		writeError(w, http.StatusBadRequest, "bad_request",
			"a kind and an id are required — records are addressed by (kind, id)", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ran": 1})
}

// handleTriggerWake is the immediate scan; its body is empty on purpose.
func (f *fakeSubstrate) handleTriggerWake(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	if !f.rejectUnknown(w, "trigger wake") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ran": 2})
}

func (f *fakeSubstrate) handleDelete(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	id := r.PathValue("id")
	e, ok := f.records[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no record "+id, nil)
		return
	}
	del := testNow
	e.DeletedAt = &del
	e.Finalizers = []string{"gmail.google.connectors.substrate.reamde.dev/unsend"}
	writeJSON(w, http.StatusOK, e)
}

// mustRaw re-marshals a recorded body map so it can be decoded into a typed
// input; the fake records bodies generically.
func mustRaw(body map[string]json.RawMessage) []byte {
	if body == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func sortRecords(es []*substrate.Record) {
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && es[j].ID < es[j-1].ID; j-- {
			es[j], es[j-1] = es[j-1], es[j]
		}
	}
}
