package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/synth"
)

const (
	testToken     = "synth-test-token"
	testAuthority = "ada.example.com"
	testKey       = "synth-test-webhook-key-0123"
)

// fakeSubstrate answers the routes the loader calls: the catalog, the
// trigger records and their verbs, record writes, and the webhook door.
type fakeSubstrate struct {
	t  *testing.T
	ts *httptest.Server

	mu        sync.Mutex
	requests  []string
	installed map[string]bool
	triggers  map[string]map[string]any
	parked    map[string]int64
	// busy is how many webhook posts the door answers 503 before it takes one.
	busy int
	// parkAt is the webhook post (1-based) whose delivery parks, 0 for none.
	parkAt   int
	posts    int
	webhooks []*http.Request
	records  map[string]map[string]any
}

func newFakeSubstrate(t *testing.T) *fakeSubstrate {
	t.Helper()
	f := &fakeSubstrate{
		t: t, installed: map[string]bool{}, parked: map[string]int64{}, records: map[string]map[string]any{},
		triggers: map[string]map[string]any{
			"applehealth-webhook": {"enabled": false, "source": map[string]any{"webhook": map[string]any{"headers": []any{"session-id"}}}},
			"garmin-webhook":      {"enabled": false, "source": map[string]any{"webhook": map[string]any{}}},
		},
	}
	f.ts = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakeSubstrate) client() *client {
	return &client{server: f.ts.URL, token: testToken, authority: testAuthority, hc: f.ts.Client()}
}

func (f *fakeSubstrate) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.requests...)
}

func (f *fakeSubstrate) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	if rest, ok := strings.CutPrefix(r.URL.Path, "/webhooks/"); ok {
		f.webhooks = append(f.webhooks, r)
		if f.busy > 0 {
			f.busy--
			w.Header().Set("Retry-After", "1")
			reply(http.StatusServiceUnavailable, map[string]any{"error": "webhook door is busy, retry"})
			return
		}
		parts := strings.Split(rest, "/")
		f.posts++
		if f.posts == f.parkAt {
			f.parked[parts[1]]++
		}
		reply(http.StatusAccepted, map[string]any{"fire": "f1"})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		reply(http.StatusUnauthorized, map[string]any{"error": "no token"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	switch {
	case strings.HasPrefix(path, "catalog/"):
		id, verb, _ := strings.Cut(strings.TrimPrefix(r.URL.EscapedPath(), "/api/v1/catalog/"), "/")
		id, _ = url.PathUnescape(id)
		switch verb {
		case "":
			reply(http.StatusOK, map[string]any{"id": id, "installed": f.installed[id]})
		case "import", "install":
			f.installed[id] = true
			reply(http.StatusOK, map[string]any{"id": id, "installed": true})
		}
	case path == "substrate.reamde.dev/core/trigger/status":
		var items []map[string]any
		for id := range f.triggers {
			items = append(items, map[string]any{"id": id, "kind": "webhook", "parked": f.parked[id], "pending": 0, "inFlight": 0})
		}
		reply(http.StatusOK, map[string]any{"items": items})
	case strings.HasPrefix(path, "substrate.reamde.dev/core/trigger/"):
		id, verb, _ := strings.Cut(strings.TrimPrefix(path, "substrate.reamde.dev/core/trigger/"), "/")
		props, ok := f.triggers[id]
		switch {
		case !ok:
			reply(http.StatusNotFound, map[string]any{"error": "no trigger"})
		case verb != "":
			reply(http.StatusOK, map[string]any{})
		case r.Method == http.MethodGet:
			reply(http.StatusOK, map[string]any{"id": id, "properties": props})
		case r.Method == http.MethodPatch:
			var in struct {
				Properties map[string]any `json:"properties"`
			}
			_ = json.Unmarshal(body, &in)
			for k, v := range in.Properties {
				props[k] = v
			}
			reply(http.StatusOK, map[string]any{"id": id, "properties": props})
		}
	case r.Method == http.MethodPut:
		var in struct {
			Properties map[string]any `json:"properties"`
		}
		_ = json.Unmarshal(body, &in)
		f.records[path] = in.Properties
		reply(http.StatusOK, map[string]any{})
	default:
		reply(http.StatusNotFound, map[string]any{"error": "no route " + r.URL.Path})
	}
}

// generated writes the named emitters' output for two days under a temporary
// directory, as `synth generate` does.
func generated(t *testing.T, authority string, ems ...synth.Emitter) string {
	t.Helper()
	p, err := synth.NewPersona(synth.Options{Start: synth.NewDate(2026, time.September, 12), End: synth.NewDate(2026, time.September, 13)})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, e := range ems {
		if m, ok := e.(synth.Manual); ok {
			m.Authority = authority
			e = m
		}
		out, err := e.Emit(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := synth.WriteOutput(dir, e, out); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runLoad runs `synth load` against the fake with the arguments given.
func (f *fakeSubstrate) runLoad(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("SUBSTRATE_TOKEN", testToken)
	var out bytes.Buffer
	err := run(append([]string{"load", "-server", f.ts.URL, "-authority", testAuthority}, args...), &out, &out)
	return out.String(), err
}

func indexOf(list []string, want string) int {
	for i, s := range list {
		if s == want {
			return i
		}
	}
	return -1
}

// The hand-entered records need the observations and health samples, so a
// load of `manual` alone imports them before its first write.
func TestLoadImportsTheSamplesBeforeTheManualRecords(t *testing.T) {
	f := newFakeSubstrate(t)
	dir := generated(t, testAuthority, synth.Manual{})
	if out, err := f.runLoad(t, "-dir", dir, "-providers", "manual"); err != nil {
		t.Fatalf("load: %v\n%s", err, out)
	}
	log := f.log()
	firstPut := -1
	for i, r := range log {
		if strings.HasPrefix(r, "PUT ") {
			firstPut = i
			break
		}
	}
	for _, s := range []string{"samples.substrate.reamde.dev/observations", "samples.substrate.reamde.dev/health"} {
		i := indexOf(log, "POST /api/v1/catalog/"+s+"/import")
		if i < 0 || firstPut < 0 || i > firstPut {
			t.Errorf("%s imported at request %d, the first record written at %d", s, i, firstPut)
		}
	}
}

// labs.yaml names the authority generate was given; a load into another
// repository is refused before it writes anything, and says how to fix it.
func TestLoadRefusesRecordsGeneratedForAnotherAuthority(t *testing.T) {
	f := newFakeSubstrate(t)
	dir := generated(t, "other.example.com", synth.Manual{})
	out, err := f.runLoad(t, "-dir", dir, "-providers", "manual")
	if err == nil || !strings.Contains(err.Error(), "other.example.com") || !strings.Contains(err.Error(), "-authority "+testAuthority) {
		t.Fatalf("load = %v, want a refusal naming both authorities\n%s", err, out)
	}
	for _, r := range f.log() {
		if strings.HasPrefix(r, "PUT ") {
			t.Fatalf("the refused load wrote %s", r)
		}
	}
}

// A re-import answers 200 like a first one, so the loader reads whether the
// repository holds a sample before it imports, and answers only the samples
// it imported: those are the ones replayed over rows that landed first.
func TestImportSamplesAnswersOnlyWhatItImported(t *testing.T) {
	f := newFakeSubstrate(t)
	const adapter = "samples.substrate.reamde.dev/healthwhoop"
	for _, s := range []string{"samples.substrate.reamde.dev/observations", "samples.substrate.reamde.dev/health", adapter} {
		f.installed[s] = true
	}
	imported, err := f.client().importSamples(t.Context(), []string{adapter}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 0 {
		t.Errorf("a repository holding every sample imported %v", imported)
	}
	for _, r := range f.log() {
		if strings.HasSuffix(r, "/import") {
			t.Errorf("a held sample was imported again: %s", r)
		}
	}

	f = newFakeSubstrate(t)
	imported, err = f.client().importSamples(t.Context(), []string{adapter}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(imported, " ") != "samples.substrate.reamde.dev/observations samples.substrate.reamde.dev/health "+adapter {
		t.Errorf("a fresh repository imported %v", imported)
	}
}

// The webhook key is the owner's: it comes from -key or SYNTH_WEBHOOK_KEY,
// never from the source, so a load with neither posts nothing.
func TestLoadTakesTheWebhookKeyFromTheOwner(t *testing.T) {
	f := newFakeSubstrate(t)
	dir := generated(t, testAuthority, synth.AppleHealth{})
	t.Setenv("SYNTH_WEBHOOK_KEY", "")
	if out, err := f.runLoad(t, "-dir", dir, "-providers", "applehealth"); err == nil ||
		!strings.Contains(err.Error(), "-key") || !strings.Contains(err.Error(), "SYNTH_WEBHOOK_KEY") {
		t.Fatalf("a load with no key = %v, want a refusal naming -key and SYNTH_WEBHOOK_KEY\n%s", err, out)
	}
	if log := f.log(); len(log) != 0 {
		t.Fatalf("a load with no key made %d requests: %v", len(log), log)
	}

	t.Setenv("SYNTH_WEBHOOK_KEY", testKey)
	if out, err := f.runLoad(t, "-dir", dir, "-providers", "applehealth"); err != nil {
		t.Fatalf("load: %v\n%s", err, out)
	}
	webhook := f.triggers["applehealth-webhook"]["source"].(map[string]any)["webhook"].(map[string]any)
	if webhook["key"] != testKey {
		t.Errorf("the trigger was armed with %v, want the environment's key", webhook["key"])
	}
	if len(f.webhooks) != 2 {
		t.Errorf("%d deliveries posted, want 2", len(f.webhooks))
	}
}

// A key the trigger already has is the owner's: the loader posts with it
// when -key names it, refuses another unless -replace-key says so, and
// leaves a trigger it found disabled disabled.
func TestLoadKeepsAKeyTheTriggerHas(t *testing.T) {
	const owners = "the-owners-own-key-0123456789"
	f := newFakeSubstrate(t)
	f.triggers["applehealth-webhook"]["source"] = map[string]any{"webhook": map[string]any{"key": owners}}
	dir := generated(t, testAuthority, synth.AppleHealth{})
	t.Setenv("SYNTH_WEBHOOK_KEY", "")

	out, err := f.runLoad(t, "-dir", dir, "-providers", "applehealth", "-key", testKey)
	if err == nil || !strings.Contains(err.Error(), "-replace-key") {
		t.Fatalf("a load with another key = %v, want a refusal naming -replace-key\n%s", err, out)
	}
	if key := f.triggers["applehealth-webhook"]["source"].(map[string]any)["webhook"].(map[string]any)["key"]; key != owners || len(f.webhooks) != 0 {
		t.Fatalf("the refused load left key %v and posted %d deliveries", key, len(f.webhooks))
	}

	if out, err := f.runLoad(t, "-dir", dir, "-providers", "applehealth", "-key", owners); err != nil {
		t.Fatalf("a load with the trigger's own key: %v\n%s", err, out)
	}
	if out, err := f.runLoad(t, "-dir", dir, "-providers", "applehealth", "-key", testKey, "-replace-key"); err != nil {
		t.Fatalf("a load asked to replace the key: %v\n%s", err, out)
	}
	trig := f.triggers["applehealth-webhook"]
	if key := trig["source"].(map[string]any)["webhook"].(map[string]any)["key"]; key != testKey {
		t.Errorf("-replace-key left key %v", key)
	}
	if trig["enabled"] != false {
		t.Errorf("the trigger was found disabled and left enabled %v", trig["enabled"])
	}
}

// The repository token is for the API. A webhook post carries the key the
// trigger was armed with and nothing else: a Garmin push in its path, an
// Apple Health delivery as its own bearer.
func TestWebhookPostsCarryNoRepositoryToken(t *testing.T) {
	f := newFakeSubstrate(t)
	dir := generated(t, testAuthority, synth.AppleHealth{})
	t.Setenv("SYNTH_WEBHOOK_KEY", testKey)
	if out, err := f.runLoad(t, "-dir", dir, "-providers", "applehealth"); err != nil {
		t.Fatalf("load: %v\n%s", err, out)
	}
	if len(f.webhooks) == 0 {
		t.Fatal("nothing was posted")
	}
	for _, r := range f.webhooks {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
			t.Errorf("%s carried Authorization %q, want the webhook key alone", r.URL.Path, got)
		}
	}
	c := f.client()
	if _, _, err := c.do(t.Context(), http.MethodPost, "/webhooks/"+testAuthority+"/garmin-webhook/"+testKey, []byte("{}"), nil); err != nil {
		t.Fatal(err)
	}
	last := f.webhooks[len(f.webhooks)-1]
	if got := last.Header.Get("Authorization"); got != "" {
		t.Errorf("a post with its key in the path carried Authorization %q", got)
	}
}
