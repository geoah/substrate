package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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
	parkAt int
	// pendingPolls is how many status reads see a delivery still pending.
	pendingPolls int
	// drop closes every webhook connection without an answer.
	drop     bool
	posts    int
	webhooks []*http.Request
	records  map[string]map[string]any
	// packages are the core package rows by id, kinds the declared kinds,
	// and accounts each kind's record ids, as the guard before a sync reads
	// them.
	packages map[string]map[string]any
	kinds    map[string]bool
	accounts map[string][]string
	// configs is each config kind's records, id to clientId.
	configs map[string]map[string]string
}

func newFakeSubstrate(t *testing.T) *fakeSubstrate {
	t.Helper()
	f := &fakeSubstrate{
		t: t, installed: map[string]bool{}, parked: map[string]int64{}, records: map[string]map[string]any{},
		packages: map[string]map[string]any{}, kinds: map[string]bool{}, accounts: map[string][]string{},
		configs: map[string]map[string]string{},
		triggers: map[string]map[string]any{
			"pushdemo-webhook": {"enabled": false, "source": map[string]any{"webhook": map[string]any{"headers": []any{"session-id"}}}},
		},
	}
	f.ts = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakeSubstrate) client() *client {
	return &client{server: f.ts.URL, token: testToken, authority: testAuthority, hc: f.ts.Client()}
}

// seen is every request the fake answered, as `METHOD /path`, in order.
func (f *fakeSubstrate) seen() []string {
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
		if f.drop {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				_ = conn.Close()
			}
			return
		}
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
		pending := 0
		if f.pendingPolls > 0 {
			f.pendingPolls--
			pending = 1
		}
		var items []map[string]any
		for id := range f.triggers {
			items = append(items, map[string]any{"id": id, "kind": "webhook", "parked": f.parked[id], "pending": pending, "inFlight": 0})
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
	case r.Method == http.MethodGet && strings.HasPrefix(path, "substrate.reamde.dev/core/package/"):
		id := strings.TrimPrefix(path, "substrate.reamde.dev/core/package/")
		if props, ok := f.packages[id]; ok {
			reply(http.StatusOK, map[string]any{"id": id, "properties": props})
		} else {
			reply(http.StatusNotFound, map[string]any{"error": "no package"})
		}
	case r.Method == http.MethodGet && strings.HasPrefix(path, "substrate.reamde.dev/core/kind/"):
		id := strings.TrimPrefix(path, "substrate.reamde.dev/core/kind/")
		if f.kinds[id] {
			reply(http.StatusOK, map[string]any{"id": id})
		} else {
			reply(http.StatusNotFound, map[string]any{"error": "no kind"})
		}
	case r.Method == http.MethodGet && path == "records":
		var filter struct {
			Kinds []string `json:"kinds"`
		}
		_ = json.Unmarshal([]byte(r.URL.Query().Get("filter")), &filter)
		records := []map[string]any{}
		for _, k := range filter.Kinds {
			for _, id := range f.accounts[k] {
				records = append(records, map[string]any{"id": id, "kind": k})
			}
			for id, client := range f.configs[k] {
				records = append(records, map[string]any{"id": id, "kind": k, "properties": map[string]any{"clientId": client}})
			}
		}
		reply(http.StatusOK, map[string]any{"records": records})
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
	seen := f.seen()
	firstPut := -1
	for i, r := range seen {
		if strings.HasPrefix(r, "PUT ") {
			firstPut = i
			break
		}
	}
	for _, s := range []string{"samples.substrate.reamde.dev/observations", "samples.substrate.reamde.dev/health"} {
		i := indexOf(seen, "POST /api/v1/catalog/"+s+"/import")
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
	for _, r := range f.seen() {
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
	for _, r := range f.seen() {
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

// pushDemo stands in for a provider that receives pushes: one webhook
// request a day to its trigger, the delivery carrying the key as its bearer
// where the trigger is keyed. The tests below post its output through the
// loader's poster, so the push path is held without a shipped push provider.
type pushDemo struct{}

func (pushDemo) Name() string { return "pushdemo" }

func (pushDemo) Emit(p *synth.Persona) (*synth.Output, error) {
	out := &synth.Output{Setup: synth.Setup{Provider: "providers.substrate.reamde.dev/pushdemo"}}
	for _, d := range p.Days() {
		out.Deliveries = append(out.Deliveries, synth.Delivery{
			Name:    d.String(),
			Trigger: "pushdemo-webhook",
			Headers: map[string]string{"content-type": "application/json", "authorization": "Bearer <key>"},
			Body:    []byte(`{"day": "` + d.String() + `"}`),
		})
	}
	return out, nil
}

// pushed writes pushDemo's output for two days and answers its directory.
func pushed(t *testing.T) string {
	t.Helper()
	p, err := synth.NewPersona(synth.Options{Start: synth.NewDate(2026, time.September, 12), End: synth.NewDate(2026, time.September, 13)})
	if err != nil {
		t.Fatal(err)
	}
	out, err := pushDemo{}.Emit(p)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := synth.WriteOutput(dir, pushDemo{}, out); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "pushdemo")
}

// poster is a poster over the fake that does not sleep between retries.
func (f *fakeSubstrate) poster(key string, replace bool) *poster {
	c := f.client()
	c.pause = func(context.Context, time.Duration) error { return nil }
	return &poster{c: c, key: key, replaceKey: replace, settle: time.Minute, stdout: io.Discard}
}

// The poster arms the trigger with the owner's key, posts each delivery in
// order with that key as its bearer and never the repository token, waits
// for each run, and disables again a trigger it found disabled.
func TestPosterPostsEachDeliveryWithTheOwnersKey(t *testing.T) {
	f := newFakeSubstrate(t)
	if err := f.poster(testKey, false).deliveries(t.Context(), "pushdemo", pushed(t), 1); err != nil {
		t.Fatal(err)
	}
	if len(f.webhooks) != 2 {
		t.Fatalf("%d deliveries posted, want 2", len(f.webhooks))
	}
	for _, r := range f.webhooks {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
			t.Errorf("%s carried Authorization %q, want the webhook key alone", r.URL.Path, got)
		}
	}
	trig := f.triggers["pushdemo-webhook"]
	if key := trig["source"].(map[string]any)["webhook"].(map[string]any)["key"]; key != testKey {
		t.Errorf("the trigger was armed with %v, want the owner's key", key)
	}
	if trig["enabled"] != false {
		t.Errorf("the trigger was found disabled and left enabled %v", trig["enabled"])
	}
}

// A key the trigger already has is the owner's: the poster refuses another
// unless told to replace it, names where to resume, and posts nothing.
func TestPosterKeepsAKeyTheTriggerHas(t *testing.T) {
	const owners = "the-owners-own-key-0123456789"
	f := newFakeSubstrate(t)
	f.triggers["pushdemo-webhook"]["source"] = map[string]any{"webhook": map[string]any{"key": owners}}
	dir := pushed(t)
	err := f.poster(testKey, false).deliveries(t.Context(), "pushdemo", dir, 1)
	if err == nil || !strings.Contains(err.Error(), "-replace-key") || !strings.Contains(err.Error(), "-resume pushdemo/0001") {
		t.Fatalf("a post with another key = %v, want a refusal naming -replace-key and -resume pushdemo/0001", err)
	}
	if len(f.webhooks) != 0 {
		t.Fatalf("the refused load posted %d deliveries", len(f.webhooks))
	}
	if err := f.poster(testKey, true).deliveries(t.Context(), "pushdemo", dir, 1); err != nil {
		t.Fatalf("a post asked to replace the key: %v", err)
	}
	if key := f.triggers["pushdemo-webhook"]["source"].(map[string]any)["webhook"].(map[string]any)["key"]; key != testKey {
		t.Errorf("replacing the key left %v", key)
	}
}

// A busy door answers 503 with Retry-After; the poster posts the same
// delivery again instead of abandoning the load.
func TestPosterRetriesABusyDoor(t *testing.T) {
	f := newFakeSubstrate(t)
	f.busy = 1
	if err := f.poster(testKey, false).deliveries(t.Context(), "pushdemo", pushed(t), 1); err != nil {
		t.Fatal(err)
	}
	if f.posts != 2 || len(f.webhooks) != 3 {
		t.Errorf("%d deliveries taken over %d posts, want 2 over 3", f.posts, len(f.webhooks))
	}
}

// A delivery an earlier run parked is not this run's failure: the poster
// stops on a park its own post caused and names where to resume, and a
// resume posts from that delivery on.
func TestPosterStopsOnTheParkItCausedAndResumes(t *testing.T) {
	f := newFakeSubstrate(t)
	f.parked["pushdemo-webhook"] = 1
	dir := pushed(t)
	if err := f.poster(testKey, false).deliveries(t.Context(), "pushdemo", dir, 1); err != nil {
		t.Fatalf("a post beside an earlier park: %v", err)
	}
	f = newFakeSubstrate(t)
	f.parkAt = 1
	err := f.poster(testKey, false).deliveries(t.Context(), "pushdemo", dir, 1)
	if err == nil || !strings.Contains(err.Error(), "-resume pushdemo/0002") || f.posts != 1 {
		t.Fatalf("a load whose first delivery parked = %v after %d posts, want a stop naming -resume pushdemo/0002", err, f.posts)
	}
	f = newFakeSubstrate(t)
	if err := f.poster(testKey, false).deliveries(t.Context(), "pushdemo", dir, 2); err != nil {
		t.Fatal(err)
	}
	if len(f.webhooks) != 1 {
		t.Errorf("a resume at the second delivery posted %d", len(f.webhooks))
	}
	if provider, n, err := parseResume("pushdemo/0002"); err != nil || provider != "pushdemo" || n != 2 {
		t.Errorf("parseResume(pushdemo/0002) = %q, %d, %v", provider, n, err)
	}
}

// A post that fails in transport is reported with its delivery and where to
// resume, never with the key, the only credential on that door.
func TestAFailedPostNamesNoWebhookKey(t *testing.T) {
	f := newFakeSubstrate(t)
	f.drop = true
	err := f.poster(testKey, false).deliveries(t.Context(), "pushdemo", pushed(t), 1)
	if err == nil || !strings.Contains(err.Error(), "-resume pushdemo/0001") {
		t.Fatalf("a load whose posts all fail = %v, want a stop naming -resume pushdemo/0001", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("the error prints the webhook key: %v", err)
	}
	if len(f.webhooks) != postTries {
		t.Errorf("%d posts, want %d", len(f.webhooks), postTries)
	}
}

// A run that takes milliseconds is read again in milliseconds, not half a
// second, or 700 deliveries spend six minutes asleep.
func TestAwaitSettledPollsAgainSoon(t *testing.T) {
	f := newFakeSubstrate(t)
	f.pendingPolls = 2
	c := f.client()
	var waits []time.Duration
	c.pause = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	if _, err := c.awaitSettled(t.Context(), "pushdemo-webhook", time.Minute); err != nil {
		t.Fatal(err)
	}
	if want := []time.Duration{20 * time.Millisecond, 40 * time.Millisecond}; !slices.Equal(waits, want) {
		t.Errorf("settling after two pending reads waited %v, want %v", waits, want)
	}
}

// TestMain runs the command itself when a test re-executes this binary.
func TestMain(m *testing.M) {
	if os.Getenv("SYNTH_TEST_RUN_MAIN") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

func TestCtlPathIsAbsolute(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "substratectl"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	for _, ctl := range []string{"", "bin/substratectl", "./bin/substratectl"} {
		got, err := ctlPath(".", ctl)
		if err != nil {
			t.Fatalf("ctlPath(%q): %v", ctl, err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("ctlPath(%q) = %q, want an absolute path", ctl, got)
		}
	}
	if _, err := ctlPath(".", "bin/missing"); err == nil {
		t.Error("ctlPath accepted a path with no binary")
	}
}
