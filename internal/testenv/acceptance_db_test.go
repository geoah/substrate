package testenv_test

// The release acceptance drill (#462, tracker #360): one repository holding
// every state the release protects, stopped and snapshotted, restored into an
// empty database on a host with another credential key through the recovery
// key, and compared. Each stage is a subtest and none skips: a stage that
// cannot run fails with its reason, and a state the restore does not
// reproduce is a failing assertion, never a weakened one.
//
// The doors are the real ones. Everything a client would do arrives over HTTP
// with a token (records, vocabulary, blobs, merges, webhooks, function calls,
// search, the change feed); what the operator does runs through the engine
// the way substratectl and the substrated loops do (the trigger dispatcher
// pass, the embed drain, the GC sweep, `repository snapshot`, `repository
// rewrap`, `repository verify`). docs/operations.md is the procedure followed:
// "Backups" for the snapshot, "Restore without the credential key" for the
// rewrap and the boot that imports, "Restore" for the verify afterwards.
//
// The blob store is the fs backend throughout; the s3 half of the procedure
// (copying the listed objects back into the bucket) is not exercised here.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/kinds"
)

const (
	drillUser      = "drill"
	drillPassword  = "correct-horse-battery-staple"
	drillAuthority = "drill.example.com"

	legacyUser      = "legacy"
	legacyPassword  = "another-correct-horse-battery"
	legacyAuthority = "legacy.example.com"

	corePkg      = "substrate.reamde.dev/core"
	embedAPIKey  = "sk-drill-embed-key"
	embedModel   = "text-embedding-3-small"
	fileBytesTxt = "the report the drill attaches: bytes that must read back after the restore"

	// coreKindsDir is the shipped core package, relative to this package,
	// the tree the boot upgrade is patched against.
	coreKindsDir = "../../kinds/substrate.reamde.dev/core"
)

// The drill's kinds: the rehomed samples and the packages it declares itself.
var (
	taskKind    = drillAuthority + "/tasks/task"
	projectKind = drillAuthority + "/tasks/project"
	personKind  = drillAuthority + "/people/person"
	widgetKind  = drillAuthority + "/auto/widget"
	gadgetKind  = drillAuthority + "/auto/gadget"
	flagKind    = drillAuthority + "/auto/flag"
	echoKind    = drillAuthority + "/auto/echo"
	subjectKind = drillAuthority + "/crm/subject"
	contactKind = drillAuthority + "/dira/contact"
	memberKind  = drillAuthority + "/dirb/member"
	fileKind    = drillAuthority + "/attach/file"
	oauthKind   = drillAuthority + "/creds/oauthclient"
)

// The vocabulary the drill declares over /vocabulary/apply. Every function is
// python, so an apply that answers 2xx has already run each body through the
// runner.
const autoVocabulary = `
kind: substrate.reamde.dev/core/package
metadata:
  id: drill.example.com/auto
data:
  authority: drill.example.com
  package: auto
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: drill.example.com/auto/widget
data:
  authority: drill.example.com
  package: auto
  names:
    singular: widget
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: what the widget is called
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: drill.example.com/auto/gadget
data:
  authority: drill.example.com
  package: auto
  names:
    singular: gadget
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: what the gadget is called
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: drill.example.com/auto/flag
data:
  authority: drill.example.com
  package: auto
  names:
    singular: flag
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: a gate a function body waits on
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: drill.example.com/auto/echo
data:
  authority: drill.example.com
  package: auto
  names:
    singular: echo
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      description: the request body the fire saw
    want:
      type: string
      fts: false
      description: the x-github-event header the fire saw
    fire:
      type: string
      fts: false
      description: the fire id the fire ran under
---
kind: substrate.reamde.dev/core/function
metadata:
  id: drill.example.com/auto/mirror
data:
  authority: drill.example.com
  package: auto
  description: mirrors a widget into a task
  runtime: python
  permissions:
    writes: [drill.example.com/tasks/task]
  source: |
    def main(input, host):
        env = input["envelope"]
        return {"effects": [{"action": "put", "kind": "drill.example.com/tasks/task",
                             "id": "t-" + env["change"]["id"],
                             "properties": {"name": "mirror of " + env["record"]["properties"]["name"]}}]}
---
kind: substrate.reamde.dev/core/function
metadata:
  id: drill.example.com/auto/page
data:
  authority: drill.example.com
  package: auto
  description: a paged backfill that cannot pass page two until a gate opens
  runtime: python
  permissions:
    reads:
      kinds: [drill.example.com/auto/flag]
    writes: [drill.example.com/tasks/task]
  source: |
    FLAG = "drill.example.com/auto/flag"
    TASK = "drill.example.com/tasks/task"

    def main(input, host):
        page = input.get("resume") or 0
        if page == 2 and host.records.get(FLAG, "page-gate") is None:
            raise RuntimeError("page gate closed")
        effects = [{"action": "put", "kind": TASK, "id": "p-%d" % page,
                    "properties": {"name": "page %d" % page}}]
        if page < 4:
            return {"effects": effects, "more": {"cursor": page + 1}}
        return {"effects": effects}
---
kind: substrate.reamde.dev/core/function
metadata:
  id: drill.example.com/auto/hook
data:
  authority: drill.example.com
  package: auto
  description: echoes a webhook request once its gate opens, and fails until then
  runtime: python
  permissions:
    reads:
      kinds: [drill.example.com/auto/flag]
    writes: [drill.example.com/auto/echo]
  source: |
    FLAG = "drill.example.com/auto/flag"
    ECHO = "drill.example.com/auto/echo"

    def main(input, host):
        if host.records.get(FLAG, "hook-gate") is None:
            raise RuntimeError("hook gate closed")
        env = input.get("envelope") or {}
        req = env.get("request") or {}
        headers = req.get("headers") or {}
        body = (req.get("body") or {}).get("text") or ""
        host.effects.put(ECHO, "hook-echo", properties={
            "name": body,
            "want": headers.get("x-github-event") or "",
            "fire": (env.get("fire") or {}).get("id") or "",
        })
        return {"output": {}}
---
kind: substrate.reamde.dev/core/function
metadata:
  id: drill.example.com/auto/hold
data:
  authority: drill.example.com
  package: auto
  description: echoes a webhook request once a release flag exists, sleeping until then
  runtime: python
  timeout: PT50S
  permissions:
    reads:
      kinds: [drill.example.com/auto/flag]
      budgets:
        calls: 400
        rows: 400
    writes: [drill.example.com/auto/echo]
  source: |
    import time

    FLAG = "drill.example.com/auto/flag"
    ECHO = "drill.example.com/auto/echo"

    def main(input, host):
        for _ in range(160):
            if host.records.get(FLAG, "release") is not None:
                break
            time.sleep(0.25)
        else:
            raise RuntimeError("release never came")
        env = input.get("envelope") or {}
        req = env.get("request") or {}
        headers = req.get("headers") or {}
        body = (req.get("body") or {}).get("text") or ""
        host.effects.put(ECHO, "hold-echo", properties={
            "name": body,
            "want": headers.get("x-github-event") or "",
            "fire": (env.get("fire") or {}).get("id") or "",
        })
        return {"output": {}}
`

// crmVocabulary is the mapping target: the package that owns `subject`
// declares the mappings from the two mirror packages onto it (decision 0049).
// It is applied twice: the kind alone, before the mirrors that reference it,
// then with the mappings once the source kinds exist.
const crmKind = `
kind: substrate.reamde.dev/core/package
metadata:
  id: drill.example.com/crm
data:
  authority: drill.example.com
  package: crm
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: drill.example.com/crm/subject
data:
  authority: drill.example.com
  package: crm
  names:
    singular: subject
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      description: the full name, one string
    emails:
      type: email
      repeated: true
      description: every address seen for this subject
    note:
      type: string
      description: a hand-written note
`

const crmMappings = `
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: drill.example.com/crm/contactsubject
data:
  authority: drill.example.com
  package: crm
  description: a directory A contact converges onto its subject, matched on the address
  from: drill.example.com/dira/contact
  to: drill.example.com/crm/subject
  property: subject
  match:
    - from: email
      to: emails
  map:
    name:
      path: name
    emails:
      path: email
      merge: union
---
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: drill.example.com/crm/membersubject
data:
  authority: drill.example.com
  package: crm
  description: a directory B member converges onto its subject, matched on the address
  from: drill.example.com/dirb/member
  to: drill.example.com/crm/subject
  property: subject
  match:
    - from: email
      to: emails
  map:
    name:
      path: name
    emails:
      path: email
      merge: union
`

// mirrorVocabulary is one provider-shaped package: a mirror kind whose
// `subject` reference is the mapping's subject slot, and a sync function that
// writes the mirror under its own actor, the way a provider's sync does.
func mirrorVocabulary(pkg, kind, fn string) string {
	return strings.NewReplacer("PKG", pkg, "KIND", kind, "FN", fn).Replace(`
kind: substrate.reamde.dev/core/package
metadata:
  id: drill.example.com/PKG
data:
  authority: drill.example.com
  package: PKG
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: drill.example.com/PKG/KIND
data:
  authority: drill.example.com
  package: PKG
  names:
    singular: KIND
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      description: the name the directory holds
    email:
      type: email
      description: the address the directory holds
    subject:
      type: reference
      kind: drill.example.com/crm/subject
      required: true
      mustExist: true
      subject: true
      description: the subject this mirror describes
---
kind: substrate.reamde.dev/core/function
metadata:
  id: drill.example.com/PKG/FN
data:
  authority: drill.example.com
  package: PKG
  description: one sync of one directory entry
  runtime: python
  permissions:
    writes: [drill.example.com/PKG/KIND]
  source: |
    def main(input, host):
        args = input["args"]
        host.effects.put("drill.example.com/PKG/KIND", args["id"],
                         properties={"name": args["name"], "email": args["email"]})
        return {"output": {"ok": True}}
`)
}

// attachVocabulary declares the kind that carries the attachment; the second
// form adds an optional property, the user-declared edit the drill records.
func attachVocabulary(withNotes bool) string {
	notes := ""
	if withNotes {
		notes = `
    notes:
      type: string
      description: what the file is about`
	}
	return `
kind: substrate.reamde.dev/core/package
metadata:
  id: drill.example.com/attach
data:
  authority: drill.example.com
  package: attach
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: drill.example.com/attach/file
data:
  authority: drill.example.com
  package: attach
  names:
    singular: file
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: the file's display name
    data:
      type: blobref
      description: the bytes` + notes + `
`
}

// credsVocabulary is an OAuth-shaped credential: a client id beside three
// sealed values, the shape a provider's config and account carry.
const credsVocabulary = `
kind: substrate.reamde.dev/core/package
metadata:
  id: drill.example.com/creds
data:
  authority: drill.example.com
  package: creds
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: drill.example.com/creds/oauthclient
data:
  authority: drill.example.com
  package: creds
  names:
    singular: oauthclient
  displayTemplate: "{clientId}"
  properties:
    clientId:
      type: string
      required: true
      description: the OAuth app's client id
    clientSecret:
      type: secret
      description: the OAuth app's client secret
    accessToken:
      type: secret
      description: the bearer the provider issued
    refreshToken:
      type: secret
      description: the refresh token the provider issued
    expiresAt:
      type: datetime
      description: when the access token expires
`

// drillClock is the TOTP clock both substrates verify against: the wall clock
// plus what the drill advanced, so a login on the restored host does not
// replay the registration's code within one 30 second window.
type drillClock struct {
	mu     sync.Mutex
	offset time.Duration
}

func (c *drillClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset).UTC()
}

func (c *drillClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset += d
}

// fakeEmbed is an OpenAI-wire embeddings endpoint whose vectors are an
// L2-normalised bag-of-words hash, so two drains over the same text buy the
// same vectors and a semantic ranking compares equal across the restore. It
// records every Authorization header: the restored drain sending the
// repository's apiKey is the proof the sealed value opened under the new
// host key.
type fakeEmbed struct {
	srv   *httptest.Server
	mu    sync.Mutex
	auths []string
	texts int
}

func newFakeEmbed(t *testing.T) *fakeEmbed {
	t.Helper()
	f := &fakeEmbed{}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeEmbed) handle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Input []string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.auths = append(f.auths, r.Header.Get("Authorization"))
	f.texts += len(req.Input)
	f.mu.Unlock()
	type datum struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}
	out := struct {
		Data []datum `json:"data"`
	}{}
	for i, s := range req.Input {
		out.Data = append(out.Data, datum{Index: i, Embedding: bagOfWords(s, 1536)})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (f *fakeEmbed) seen() (auths []string, texts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auths...), f.texts
}

func (f *fakeEmbed) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auths, f.texts = nil, 0
}

func bagOfWords(s string, width int) []float32 {
	vec := make([]float32, width)
	for _, word := range strings.Fields(strings.ToLower(s)) {
		h := 0
		for _, r := range word {
			h = (h*31 + int(r)) % width
		}
		vec[h]++
	}
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		vec[0] = 1
		return vec
	}
	norm := float32(math.Sqrt(sum))
	for i := range vec {
		vec[i] /= norm
	}
	return vec
}

// The operator seams off substrate.Service, asserted here the way the CLI
// asserts them.
type (
	snapshotter interface {
		SnapshotRepository(ctx context.Context, username, destRoot string) (engine.SnapshotReport, error)
	}
	verifier interface {
		VerifyRepository(ctx context.Context, username string) (engine.VerifyReport, error)
	}
	folded interface {
		FoldSnapshot(ctx context.Context) ([]byte, error)
	}
)

// searchAnswer is one search's outcome: the hit ids in rank order and the
// backlog, or the refusal's code and message.
type searchAnswer struct {
	IDs     []string
	Pending int
	ErrCode string
	ErrMsg  string
}

// state is the observable state of one substrate, captured through the API
// and the operator seams: what stage 1 records and stage 4 compares.
type state struct {
	// records is every record the collections below hold, keyed
	// "<kind>/<id>", as a single-record GET returns it (propertyMeta
	// included), decoded so the comparison is structural.
	records map[string]any
	// deleted is each collection's tombstone list, keyed by kind.
	deleted map[string]any
	// kindVersions is each installed kind's declaration version.
	kindVersions map[string]int64
	// statuses is each trigger's status: enabled, parked, pending.
	statuses map[string]substrate.TriggerStatus
	// parked is each trigger's parked list, and pagedCursors the resume
	// cursors the delivery ledger holds, keyed by chain.
	parked       map[string][]substrate.TriggerFailure
	pagedCursors map[string]float64
	deliveries   int
	// fold is the engine's fold snapshot: records, refs, provenance, the
	// delivery ledger's tables.
	fold []byte
	// blobs is each attachment's bytes as GET /blobs served them.
	blobs map[string][]byte
	// lexical and semantic are the fixed queries' answers.
	lexical  map[string]searchAnswer
	semantic map[string]searchAnswer
	head     int64
	gen      string
}

// drill is the state the stages hand each other. root is the parent test:
// everything a later stage reads (schemas, roots, running substrates) is
// created against it, so a stage's end drops nothing the next one needs.
type drill struct {
	root  *testing.T
	clock *drillClock
	embed *fakeEmbed

	identity, legacyIdentity *age.X25519Identity
	keyA, keyB               string
	rootA, dsnA              string

	// The source substrate, its token and the recorded state.
	envA   *testenv.Env
	legacy *testenv.Env
	source *state

	// What stage 1 has to hand later stages by name.
	blobDigest     string
	subjectID      string
	mergeWinner    string
	splitMergeID   string
	pagedFailureID int64
	pagedChain     string
	hookFailureID  int64
	hookFireID     string
	holdFireID     string
	// holdInvoked is signaled as the runner starts the held fire's body.
	holdInvoked chan struct{}
	runVersion  int64

	// Stage 2: the snapshot roots and the recorded points.
	snapRoot   string
	drillDir   string
	legacyDir  string
	snapHead   int64
	legacyHead int64
	legacyFold []byte

	// Stage 3: the rewrapped directory as it stood before the restore booted
	// (pristine, copied for stages 5 and 6), the restored substrate and what
	// it reads.
	pristine string
	envB     *testenv.Env
	restored *state

	done map[string]bool
}

func TestReleaseAcceptanceDrill(t *testing.T) {
	if testing.Short() {
		t.Skip("the acceptance drill wants a database")
	}
	// The embeddings endpoint is an httptest server on loopback, which the
	// server dial refuses unless the operator allows it (issue #241's
	// documented escape for a local provider).
	if os.Getenv("SUBSTRATE_EGRESS_ALLOW") == "" {
		t.Setenv("SUBSTRATE_EGRESS_ALLOW", "127.0.0.0/8,::1/128")
	}
	d := &drill{root: t, clock: &drillClock{}, done: map[string]bool{}}
	stages := []struct {
		name string
		run  func(t *testing.T)
		// needs names the stages this one reads the outputs of.
		needs []string
	}{
		{"01 build the source through the real doors", d.buildSource, nil},
		{"02 stop and capture the committed recovery point", d.stopAndSnapshot, []string{"01"}},
		{"03 restore under another credential key with the recovery key", d.restore, []string{"02"}},
		{"04 compare the restored substrate and resume dispatch", d.compareAndResume, []string{"03"}},
		{"05 interrupt the import at batch boundaries and resume", d.interruptedImport, []string{"03"}},
		{"06 restore the oldest accepted format and refuse a newer one", d.formatTransition, []string{"02", "03"}},
		{"07 a cursor from the replaced history is reset", d.replacedHistoryCursor, []string{"03"}},
	}
	for _, s := range stages {
		t.Run(s.name, func(t *testing.T) {
			for _, need := range s.needs {
				if !d.done[need] {
					t.Fatalf("stage %s did not complete, so this stage has nothing to run against", need)
				}
			}
			s.run(t)
			d.done[s.name[:2]] = !t.Failed()
		})
	}
}

// --- stage 1 --------------------------------------------------------------------

func (d *drill) buildSource(t *testing.T) {
	ctx := context.Background()
	var err error
	if d.identity, err = age.GenerateX25519Identity(); err != nil {
		t.Fatal(err)
	}
	if d.legacyIdentity, err = age.GenerateX25519Identity(); err != nil {
		t.Fatal(err)
	}
	d.keyA, d.keyB = testenv.MintCredentialKey(), testenv.MintCredentialKey()
	d.rootA = d.root.TempDir()
	d.dsnA = testdb.NewSchema(d.root)
	d.embed = newFakeEmbed(d.root)

	// A shipped kind upgraded: the first boot loads a copy of core where
	// `run` is one version behind, registration seeds it at that version,
	// and the second boot under the shipped tree appends the upgrade.
	patched, shippedRun := patchedCoreTree(t, "run.yaml")
	d.runVersion = shippedRun
	first := testenv.Start(d.root,
		testenv.WithUser(drillUser, drillPassword),
		testenv.WithAuthority(drillAuthority),
		testenv.WithRecoveryPublicKey(d.identity.Recipient().String()),
		testenv.WithDSN(d.dsnA), testenv.WithDataRoot(d.rootA), testenv.WithCredentialKey(d.keyA),
		testenv.WithClock(d.clock.Now), testenv.WithKindsDir(patched)).For(t)
	if first.Authority != drillAuthority {
		t.Fatalf("registered authority %q, want %q", first.Authority, drillAuthority)
	}
	if got := kindVersions(t, first)[corePkg+"/run"]; got != shippedRun-1 {
		t.Fatalf("run declared at %d under the patched tree, want %d", got, shippedRun-1)
	}
	token, totp := first.Token, first.TOTPSecret
	first.Stop()

	d.holdInvoked = make(chan struct{}, 16)
	d.envA = testenv.Start(d.root,
		testenv.WithUser(drillUser, drillPassword), testenv.WithoutRegistration(),
		testenv.WithDSN(d.dsnA), testenv.WithDataRoot(d.rootA), testenv.WithCredentialKey(d.keyA),
		testenv.WithClock(d.clock.Now),
		testenv.WithEngineOptions(engine.WithTestInvokeHook(func(function string) {
			if strings.HasSuffix(function, "/auto/hold") {
				select {
				case d.holdInvoked <- struct{}{}:
				default:
				}
			}
		})))
	d.envA.Token, d.envA.TOTPSecret, d.envA.Authority = token, totp, drillAuthority
	e := d.envA.For(t)
	if got := kindVersions(t, e)[corePkg+"/run"]; got != shippedRun {
		t.Errorf("the boot upgrade did not land: run declared at %d, want the shipped %d", got, shippedRun)
	}

	// The samples, rehomed onto the repository's authority; then the drill's
	// own packages, and the kind edit that bumps `file`.
	for _, id := range sampleImports {
		if status, body := e.Do(http.MethodPost, "/api/v1/catalog/"+url.PathEscape(id)+"/import", nil); status/100 != 2 {
			t.Fatalf("import %s: %d %s", id, status, body)
		}
	}
	e.ApplyVocabularyYAML(autoVocabulary)
	e.ApplyVocabularyYAML(crmKind)
	e.ApplyVocabularyYAML(mirrorVocabulary("dira", "contact", "synca"))
	e.ApplyVocabularyYAML(mirrorVocabulary("dirb", "member", "syncb"))
	e.ApplyVocabularyYAML(crmKind + "---" + crmMappings)
	e.ApplyVocabularyYAML(attachVocabulary(false))
	e.ApplyVocabularyYAML(attachVocabulary(true))
	e.ApplyVocabularyYAML(credsVocabulary)
	if got := kindVersions(t, e)[fileKind]; got != 2 {
		t.Errorf("file declared at version %d after its edit, want 2", got)
	}

	// Records: a project, tasks with a reference, labels, a transition, and
	// the last label cleared to the empty map (#362).
	put := func(kind, id string, body map[string]any) map[string]any {
		t.Helper()
		status, raw := e.Do(http.MethodPut, recordPath(kind, id), body)
		if status/100 != 2 {
			t.Fatalf("put %s/%s: %d %s", kind, id, status, raw)
		}
		return decodeAny(t, raw).(map[string]any)
	}
	patch := func(kind, id string, body map[string]any) map[string]any {
		t.Helper()
		status, raw := e.Do(http.MethodPatch, recordPath(kind, id), body)
		if status/100 != 2 {
			t.Fatalf("patch %s/%s: %d %s", kind, id, status, raw)
		}
		return decodeAny(t, raw).(map[string]any)
	}
	del := func(kind, id string) {
		t.Helper()
		if status, raw := e.Do(http.MethodDelete, recordPath(kind, id), nil); status/100 != 2 {
			t.Fatalf("delete %s/%s: %d %s", kind, id, status, raw)
		}
	}
	put(projectKind, "release", map[string]any{"properties": map[string]any{"name": "The release"}})
	due := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	put(taskKind, "fold", map[string]any{
		"properties": map[string]any{
			"name": "Ship the fold", "description": "carry the values, not just the names",
			"dueAt": due, "url": "https://example.com/1", "project": "release",
		},
		"labels": map[string]any{"owner/pinned": true},
	})
	put(taskKind, "rebuild", map[string]any{"properties": map[string]any{
		"name": "Rebuild the repository", "description": "replay every entry of the ledger", "project": "release",
	}})
	put(taskKind, "collect", map[string]any{"properties": map[string]any{"name": "Collect me", "description": "and then go"}})
	patch(taskKind, "fold", map[string]any{
		"properties":  map[string]any{"description": "values, replayable", "url": nil},
		"labels":      map[string]any{"owner/pinned": nil, "owner/urgent": "yes"},
		"annotations": map[string]any{"owner/note": map[string]any{"why": "the payload"}},
	})
	patch(taskKind, "fold", map[string]any{"properties": map[string]any{"status": "done"}})
	cleared := patch(taskKind, "fold", map[string]any{"labels": map[string]any{"owner/urgent": nil}})
	if labels, _ := cleared["labels"].(map[string]any); len(labels) != 0 {
		t.Fatalf("the last label did not clear: %v", cleared["labels"])
	}
	// A delete and the put that restores it.
	del(taskKind, "collect")
	put(taskKind, "collect", map[string]any{
		"properties": map[string]any{"name": "Collect me", "description": "restored"},
		"labels":     map[string]any{"owner/kept": true},
	})

	// Merges: one pair stays merged, one is merged and split.
	put(taskKind, "m-a", map[string]any{"properties": map[string]any{"name": "Winner", "description": "the record that stays"}})
	put(taskKind, "m-b", map[string]any{"properties": map[string]any{"name": "Loser", "description": "the record that folds in"}})
	status, raw := e.Do(http.MethodPost, "/api/v1/merge", map[string]any{"kind": taskKind, "winner": "m-a", "loser": "m-b"})
	if status/100 != 2 {
		t.Fatalf("merge: %d %s", status, raw)
	}
	d.mergeWinner = "m-a"
	put(taskKind, "s-a", map[string]any{"properties": map[string]any{"name": "Split winner"}})
	put(taskKind, "s-b", map[string]any{"properties": map[string]any{"name": "Split loser"}})
	status, raw = e.Do(http.MethodPost, "/api/v1/merge", map[string]any{"kind": taskKind, "winner": "s-a", "loser": "s-b"})
	if status/100 != 2 {
		t.Fatalf("merge for the split: %d %s", status, raw)
	}
	mergeRecord := decodeAny(t, raw).(map[string]any)
	d.splitMergeID, _ = mergeRecord["id"].(string)
	if mergeRecord["kind"] != corePkg+"/recordmerge" || d.splitMergeID == "" {
		t.Fatalf("merge answered with %v, want the recordmerge record", mergeRecord)
	}
	if status, raw := e.Do(http.MethodPost, "/api/v1/split", map[string]any{"merge": d.splitMergeID}); status/100 != 2 {
		t.Fatalf("split: %d %s", status, raw)
	}

	// An attachment: bytes into the blob store, then a record naming them.
	status, raw, _ = e.DoRaw(http.MethodPut, "/api/v1/blobs?name=report.txt", []byte(fileBytesTxt),
		map[string]string{"Content-Type": "text/plain"})
	if status != http.StatusCreated {
		t.Fatalf("put blob: %d %s", status, raw)
	}
	var blob substrate.BlobInfo
	if err := json.Unmarshal(raw, &blob); err != nil || blob.Digest == "" {
		t.Fatalf("blob upload answered %s: %v", raw, err)
	}
	d.blobDigest = blob.Digest
	put(fileKind, "report", map[string]any{"properties": map[string]any{
		"name": "report.txt", "data": blob.Digest, "notes": "attached before the restore",
	}})

	// Secrets: the embeddings provider's apiKey, and an OAuth-shaped set.
	put(corePkg+"/llmprovider", "vectors", map[string]any{"properties": map[string]any{
		"label": "vectors", "wire": "openai", "baseURL": d.embed.srv.URL,
		"apiKey": embedAPIKey, "embedModel": embedModel,
	}})
	put(oauthKind, "github-app", map[string]any{"properties": map[string]any{
		"clientId": "Iv1.drill", "clientSecret": "gho_client_secret_value",
		"accessToken": "gho_access_token_value", "refreshToken": "ghr_refresh_token_value",
		"expiresAt": time.Now().UTC().Add(time.Hour).Truncate(time.Second).Format(time.RFC3339),
	}})

	// Conflicting integration values: two mirrors, one subject, two names;
	// then the owner's hand on the same property, so both offers are
	// alternatives beside a held value.
	e.MustCallFunction("synca", map[string]any{"id": "a-1", "name": "Alexandra Papas", "email": "alex@example.com"})
	e.MustCallFunction("syncb", map[string]any{"id": "b-1", "name": "Alex P", "email": "alex@example.com"})
	contact := getRecord(t, e, contactKind, "a-1")
	d.subjectID = refID(contact["properties"].(map[string]any)["subject"])
	if d.subjectID == "" {
		t.Fatalf("the contact names no subject: %v", contact["properties"])
	}
	subject := getRecord(t, e, subjectKind, d.subjectID)
	if got := subject["properties"].(map[string]any)["name"]; got != "Alex P" {
		t.Errorf("subject name after two syncs = %v, want the later source's \"Alex P\"", got)
	}
	patch(subjectKind, d.subjectID, map[string]any{"properties": map[string]any{"name": "Alex", "note": "held by hand"}})
	subject = getRecord(t, e, subjectKind, d.subjectID)
	meta := subject["propertyMeta"].(map[string]any)["name"].(map[string]any)
	alts, _ := meta["alternatives"].([]any)
	if meta["manager"] != "api" || len(alts) != 2 {
		t.Errorf("subject name provenance = %v, want manager api with two alternatives", meta)
	}

	// Automations. The triggers are records; the dispatcher pass is the
	// engine's, as substrated's loop runs it.
	ds, err := e.Service.Dataset(ctx, drillUser)
	if err != nil {
		t.Fatalf("open the source dataset: %v", err)
	}
	dispatcher := ds.(substrate.TriggerDispatcher)
	callable := func(name string) string { return corePkg + "/function/" + drillAuthority + "/auto/" + name }
	put(corePkg+"/trigger", "on-mirror", map[string]any{"properties": map[string]any{
		"enabled": true, "source": map[string]any{"record": map[string]any{"kinds": []any{widgetKind}}}, "callable": callable("mirror"),
	}})
	put(corePkg+"/trigger", "on-page", map[string]any{"properties": map[string]any{
		"enabled": true, "source": map[string]any{"record": map[string]any{"kinds": []any{gadgetKind}}}, "callable": callable("page"),
	}})
	put(corePkg+"/trigger", "on-hook", map[string]any{"properties": map[string]any{
		"enabled": true, "source": map[string]any{"webhook": map[string]any{}}, "callable": callable("hook"),
	}})
	put(corePkg+"/trigger", "on-hold", map[string]any{"properties": map[string]any{
		"enabled": true, "source": map[string]any{"webhook": map[string]any{}}, "callable": callable("hold"),
	}})
	// Completed: a widget mirrored into a task.
	put(widgetKind, "w1", map[string]any{"properties": map[string]any{"name": "one"}})
	// Parked mid-cursor: the paged drain commits pages 0 and 1 and cannot
	// pass page 2 (#383, #426).
	put(gadgetKind, "g1", map[string]any{"properties": map[string]any{"name": "big"}})
	if _, err := dispatcher.ProcessTriggers(ctx); err != nil {
		t.Fatalf("dispatcher pass: %v", err)
	}
	if got := getRecord(t, e, taskKind, "t-w1")["properties"].(map[string]any)["name"]; got != "mirror of one" {
		t.Fatalf("the mirror did not deliver: %v", got)
	}
	for _, id := range []string{"p-0", "p-1"} {
		getRecord(t, e, taskKind, id)
	}
	if status, _ := e.Do(http.MethodGet, recordPath(taskKind, "p-2"), nil); status != http.StatusNotFound {
		t.Fatalf("page 2 ran past its gate: %d", status)
	}
	parked := parkedOf(t, e, "on-page")
	if len(parked) != 1 {
		t.Fatalf("paged failures = %+v, want the one park", parked)
	}
	d.pagedFailureID = parked[0].ID
	scopedA := openScoped(t, d.dsnA, drillAuthority)
	cursors := pagedCursors(t, scopedA)
	if len(cursors) != 1 {
		t.Fatalf("paged_cursors = %v, want the one chain", cursors)
	}
	for chain, cur := range cursors {
		d.pagedChain = chain
		if cur != 2 {
			t.Fatalf("paged cursor = %v, want 2 after pages 0 and 1", cur)
		}
	}
	// Pending: a widget written after the pass, undelivered at the stop.
	put(widgetKind, "w2", map[string]any{"properties": map[string]any{"name": "two"}})

	// A purge: a tombstone the sweep collects. Then a tombstone that stays.
	put(taskKind, "purge-me", map[string]any{"properties": map[string]any{"name": "Purge me"}})
	del(taskKind, "purge-me")
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if status, _ := e.Do(http.MethodGet, recordPath(taskKind, "purge-me"), nil); status != http.StatusNotFound {
		t.Fatalf("the purged record still reads: %d", status)
	}
	if tombs := listRecords(t, e, taskKind, true); containsID(tombs, "purge-me") {
		t.Fatalf("the purged record is still a tombstone: %v", tombs)
	}
	put(taskKind, "stays-deleted", map[string]any{"properties": map[string]any{"name": "Stays deleted", "description": "a tombstone the sweep has not reached"}})
	del(taskKind, "stays-deleted")

	// The parked webhook (#437): the door records the request and answers
	// 202; the fire fails at its gate and parks under a retry id.
	d.hookFireID = postWebhook(t, e, "on-hook", `{"say":"call the dentist"}`, "parked")
	deadline := time.Now().Add(20 * time.Second)
	for {
		parked = parkedOf(t, e, "on-hook")
		if len(parked) == 1 && !strings.Contains(parked[0].LastError, "has not settled") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the webhook fire did not park: %+v", parked)
		}
		time.Sleep(100 * time.Millisecond)
	}
	d.hookFailureID = parked[0].ID
	if parked[0].FireID != d.hookFireID || !strings.Contains(parked[0].LastError, "hook gate closed") {
		t.Fatalf("the parked webhook = %+v, want fire %s parked at its gate", parked[0], d.hookFireID)
	}

	// Search on the source: the drain buys the vectors, then the fixed
	// queries are recorded.
	drainEmbeds(t, ds)

	// A second repository on the same substrate, the one stage 6 takes
	// through the oldest accepted format: plain writes a v0.47 binary could
	// have made, a cleared label, a sealed value and a blob.
	d.legacy = e.RegisterUser(legacyUser, legacyPassword,
		testenv.WithAuthority(legacyAuthority),
		testenv.WithRecoveryPublicKey(d.legacyIdentity.Recipient().String()))
	l := d.legacy.For(t)
	for _, id := range sampleImports {
		if status, body := l.Do(http.MethodPost, "/api/v1/catalog/"+url.PathEscape(id)+"/import", nil); status/100 != 2 {
			t.Fatalf("legacy import %s: %d %s", id, status, body)
		}
	}
	legacyTask := legacyAuthority + "/tasks/task"
	for i, name := range []string{"first", "second", "third"} {
		if status, raw := l.Do(http.MethodPut, recordPath(legacyTask, fmt.Sprintf("l-%d", i)), map[string]any{
			"properties": map[string]any{"name": name, "description": "legacy " + name},
			"labels":     map[string]any{"owner/legacy": true},
		}); status/100 != 2 {
			t.Fatalf("legacy put: %d %s", status, raw)
		}
	}
	if status, raw := l.Do(http.MethodPatch, recordPath(legacyTask, "l-0"), map[string]any{
		"labels": map[string]any{"owner/legacy": nil},
	}); status/100 != 2 {
		t.Fatalf("legacy label clear: %d %s", status, raw)
	}
	if status, raw := l.Do(http.MethodPut, recordPath(corePkg+"/llmprovider", "legacy-llm"), map[string]any{"properties": map[string]any{
		"label": "legacy", "wire": "openai", "baseURL": "https://llm.example.com/v1", "apiKey": "sk-legacy-value",
	}}); status/100 != 2 {
		t.Fatalf("legacy provider: %d %s", status, raw)
	}
	if status, raw, _ := l.DoRaw(http.MethodPut, "/api/v1/blobs?name=legacy.txt", []byte("legacy bytes"),
		map[string]string{"Content-Type": "text/plain"}); status != http.StatusCreated {
		t.Fatalf("legacy blob: %d %s", status, raw)
	}
	lds, err := e.Service.Dataset(ctx, legacyUser)
	if err != nil {
		t.Fatalf("open the legacy dataset: %v", err)
	}
	if d.legacyFold, err = lds.(folded).FoldSnapshot(ctx); err != nil {
		t.Fatalf("legacy fold: %v", err)
	}

	// The held webhook LAST: its fire sleeps until a `release` flag exists,
	// the stop cancels it, and the entry stays pending for the restored
	// dispatcher. The runner's invoke hook says when the body has started,
	// so the stop below cancels a fire that is running, not one still queued.
	// Everything after this point must finish inside the body's 40 second
	// wait.
	d.holdFireID = postWebhook(t, e, "on-hold", `{"say":"hold the line"}`, "held")
	select {
	case <-d.holdInvoked:
	case <-time.After(20 * time.Second):
		t.Fatal("the held fire's body did not start within 20s of the 202")
	}
	if status, _ := e.Do(http.MethodGet, recordPath(echoKind, "hold-echo"), nil); status != http.StatusNotFound {
		t.Fatalf("the held fire settled before the stop: hold-echo reads %d", status)
	}

	d.source = captureState(t, e, ds, scopedA)
	if st := d.source.statuses["on-hold"]; st.Pending != 1 {
		t.Errorf("on-hold status = %+v, want one pending fire", st)
	}
	if st := d.source.statuses["on-mirror"]; st.Lag < 1 {
		t.Errorf("on-mirror status = %+v, want a pending delivery (lag) at the stop", st)
	}
	if got := d.source.lexical["ledger"]; len(got.IDs) == 0 {
		t.Errorf("lexical search found nothing for the fixed query: %+v", got)
	}
	if got := d.source.semantic["carry the values"]; len(got.IDs) == 0 || got.Pending != 0 {
		t.Errorf("semantic search on the source = %+v, want hits over a drained index", got)
	}
	if !bytes.Equal(d.source.blobs[d.blobDigest], []byte(fileBytesTxt)) {
		t.Errorf("the attachment did not read back on the source")
	}
}

// --- stage 2 --------------------------------------------------------------------

// stopAndSnapshot stops the server the way a substrated exit does and takes
// `repository snapshot` for both repositories with an operator's process, as
// docs/operations.md "Backups" describes: the copy records its head.
func (d *drill) stopAndSnapshot(t *testing.T) {
	ctx := context.Background()
	d.envA.Stop()

	operator, err := engine.Open(ctx, d.dsnA, engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(d.rootA), engine.WithCredentialKey(d.keyA), engine.WithTestTOTPClock(d.clock.Now))
	if err != nil {
		t.Fatalf("open the operator's process: %v", err)
	}
	defer func() { _ = operator.Close() }()
	d.snapRoot = d.root.TempDir()
	report, err := operator.(snapshotter).SnapshotRepository(ctx, drillUser, d.snapRoot)
	if err != nil {
		t.Fatalf("snapshot %s: %v", drillUser, err)
	}
	d.drillDir, d.snapHead = report.Directory, report.Head
	if report.Head != d.source.head || report.Repository != drillAuthority || report.BlobStore != "fs" || report.Blobs < 1 {
		t.Errorf("snapshot report = %+v, want head %d for %s under fs with the attachment", report, d.source.head, drillAuthority)
	}
	snap, err := changelogfile.ReadSnapshot(report.Directory)
	if err != nil {
		t.Fatalf("the copy carries no readable snapshot.json: %v", err)
	}
	if snap.Head != d.source.head || hex.EncodeToString(snap.HeadHash[:]) != report.HeadHash || snap.BlobLocation != "" {
		t.Errorf("recorded point = seq %d %x at %q, want seq %d %s in the directory", snap.Head, snap.HeadHash, snap.BlobLocation, d.source.head, report.HeadHash)
	}
	if !slicesContain(snap.Blobs, d.blobDigest) {
		t.Errorf("snapshot.json lists %v, which lacks the attachment %s", snap.Blobs, d.blobDigest)
	}
	if _, err := os.Stat(filepath.Join(changelogfile.BlobsDir(report.Directory), d.blobDigest)); err != nil {
		t.Errorf("the copy holds no bytes for %s: %v", d.blobDigest, err)
	}

	legacyReport, err := operator.(snapshotter).SnapshotRepository(ctx, legacyUser, d.snapRoot)
	if err != nil {
		t.Fatalf("snapshot %s: %v", legacyUser, err)
	}
	d.legacyDir, d.legacyHead = legacyReport.Directory, legacyReport.Head
	if legacyReport.Repository != legacyAuthority {
		t.Errorf("legacy snapshot report = %+v", legacyReport)
	}
	t.Logf("snapshot: %s at seq %d (%d segments, %d sealed files, %d blobs); %s at seq %d",
		drillAuthority, report.Head, report.Segments, report.SealedFiles, report.Blobs, legacyAuthority, legacyReport.Head)
}

// --- stage 3 --------------------------------------------------------------------

// restore follows docs/operations.md "Restore without the credential key":
// the copy is refused on a host whose key did not write it, `repository
// rewrap` opens the DEK with the recovery key and rewrites the manifest under
// the new host's key, the boot into an empty database imports the directory,
// and `repository verify` opens every sealed file and hashes every blob.
func (d *drill) restore(t *testing.T) {
	ctx := context.Background()
	if d.keyA == d.keyB {
		t.Fatal("the two host keys are one key")
	}
	// Without the rewrap the copy is inert on this host, and the refusal
	// leaves no row behind.
	refusedDSN := testdb.NewSchema(t)
	refusedRoot := copyRoot(t, d.snapRoot, drillAuthority)
	if svc, err := engine.Open(ctx, refusedDSN, engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(refusedRoot), engine.WithCredentialKey(d.keyB)); err == nil {
		_ = svc.Close()
		t.Fatal("the directory imported under a key it was not written under")
	} else if !strings.Contains(err.Error(), "SUBSTRATE_CREDENTIAL_KEY") {
		t.Errorf("the refusal does not name the variable: %v", err)
	}
	if n := countRows(t, refusedDSN, "repositories"); n != 0 {
		t.Errorf("the refused import left %d repositories row(s)", n)
	}

	// The rewrap, in the restore location, with the new host's key.
	restoreRoot := copyRoot(d.root, d.snapRoot, drillAuthority)
	dir, err := changelogfile.RepoDir(restoreRoot, drillAuthority)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RewrapRepositoryDir(dir, wrong.String(), d.keyB); err == nil {
		t.Error("a wrong identity rewrapped the directory")
	}
	report, err := engine.RewrapRepositoryDir(dir, d.identity.String(), d.keyB)
	if err != nil {
		t.Fatalf("rewrap: %v", err)
	}
	if report.Repository != drillAuthority || report.Username != drillUser || report.SealedFiles < 4 {
		t.Errorf("rewrap report = %+v, want %s/%s with the credential, the provider key and the three oauth values", report, drillAuthority, drillUser)
	}
	d.pristine = copyRoot(d.root, restoreRoot, drillAuthority)

	// The boot that imports: an empty schema, migrated from nothing, no
	// registration.
	dsnB := testdb.NewSchema(d.root)
	d.envB = testenv.Start(d.root,
		testenv.WithUser(drillUser, drillPassword), testenv.WithoutRegistration(),
		testenv.WithDSN(dsnB), testenv.WithDataRoot(restoreRoot), testenv.WithCredentialKey(d.keyB),
		testenv.WithClock(d.clock.Now))
	d.envB.TOTPSecret, d.envB.Authority = d.envA.TOTPSecret, drillAuthority
	e := d.envB.For(t)
	repos, err := e.Service.Repositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].ID != drillAuthority || repos[0].Name != drillUser {
		t.Fatalf("the restored database holds %+v, want the one imported repository", repos)
	}
	if incomplete, err := engine.ImportIncomplete(ctx, rawDB(t, dsnB)); err != nil || incomplete {
		t.Fatalf("import marker after the boot = %v, %v; want cleared", incomplete, err)
	}
	// No auxiliary table came along: the schema was empty and the import
	// wrote the repository's rows alone; its idempotency keys and its embed
	// vectors are not in the directory.
	for _, table := range []string{"idempotency_keys", "embeddings"} {
		if n := countRows(t, dsnB, table); n != 0 {
			t.Errorf("the restored database holds %d %s row(s) the directory could not have carried", n, table)
		}
	}
	verified, err := e.Service.(verifier).VerifyRepository(ctx, drillUser)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !verified.OK || verified.Head != d.snapHead || verified.FileHead != d.snapHead {
		t.Errorf("the restored repository does not verify: %+v", verified)
	}
	if verified.Snapshot == nil || verified.Snapshot.Head != d.snapHead || verified.Snapshot.HeadHash != verified.HeadHash {
		t.Errorf("verify did not report the recorded point: %+v (head %s)", verified.Snapshot, verified.HeadHash)
	}
	if verified.SealedOpened != verified.SealedFiles || verified.SealedOpened == 0 {
		t.Errorf("verify opened %d of %d sealed files under the new key", verified.SealedOpened, verified.SealedFiles)
	}
	if verified.Blobs < 1 || verified.SecretRefs < 4 {
		t.Errorf("verify hashed %d blobs and held %d secret refs, want the attachment and the four sealed values", verified.Blobs, verified.SecretRefs)
	}
	t.Logf("restore: imported %s at seq %d into an empty schema under a new key; verify opened %d sealed files and hashed %d blobs",
		drillAuthority, verified.Head, verified.SealedOpened, verified.Blobs)
}

// --- stage 4 --------------------------------------------------------------------

func (d *drill) compareAndResume(t *testing.T) {
	ctx := context.Background()
	e := d.envB.For(t)
	// The original token authenticates: tokens are records.
	e.Token = d.envA.Token
	ds, err := e.Service.Dataset(ctx, drillUser)
	if err != nil {
		t.Fatalf("open the restored dataset: %v", err)
	}
	scopedB := openScoped(t, e.DSN, drillAuthority)

	// Before the drain: semantic search refuses with the backlog, which is
	// not "no matches"; hybrid answers its lexical arm and reports it.
	before := gqlSearch(t, e, "carry the values", "semantic", []string{taskKind})
	if before.ErrCode != "unavailable" || !strings.Contains(before.ErrMsg, "pending") {
		t.Errorf("semantic search before the drain = %+v, want the unavailable code naming the pending count", before)
	}
	if hybrid := gqlSearch(t, e, "ledger", "hybrid", []string{taskKind}); hybrid.ErrCode != "" || hybrid.Pending == 0 || len(hybrid.IDs) == 0 {
		t.Errorf("hybrid search before the drain = %+v, want lexical hits beside a non-zero backlog", hybrid)
	}

	d.embed.reset()
	d.restored = captureState(t, e, ds, scopedB)
	// The comparison is only as strong as what the source recorded, so the
	// record set is held to a floor before it is compared.
	if n := len(d.source.records); n < 60 {
		t.Errorf("the source recorded %d records, too few for the states the drill writes", n)
	}
	if n := len(d.source.blobs); n != 3 {
		t.Errorf("the source recorded %d blobs, want the attachment and the two spooled webhook bodies", n)
	}
	if n := len(d.source.statuses); n != 4 {
		t.Errorf("the source recorded %d trigger statuses, want 4", n)
	}
	if tombs, _ := d.source.deleted[taskKind].([]any); !containsID(tombs, "stays-deleted") {
		t.Errorf("the source's task tombstones lack stays-deleted: %v", tombs)
	}
	compareStates(t, "restored", d.source, d.restored)
	t.Logf("compared %d records, %d collections' tombstones, %d kind versions, %d triggers, %d blobs, %d lexical and %d semantic queries; head %d",
		len(d.source.records), len(d.source.deleted), len(d.source.kindVersions), len(d.source.statuses),
		len(d.source.blobs), len(d.source.lexical), len(d.source.semantic), d.source.head)

	// The states the release names, read back by name.
	fold := getRecord(t, e, taskKind, "fold")
	if labels, ok := fold["labels"].(map[string]any); !ok || len(labels) != 0 {
		t.Errorf("the cleared label came back: labels = %v (#362)", fold["labels"])
	}
	if fold["version"] != d.source.records[taskKind+"/fold"].(map[string]any)["version"] {
		t.Errorf("fold's version = %v, want the source's %v", fold["version"], d.source.records[taskKind+"/fold"].(map[string]any)["version"])
	}
	if winner := getRecord(t, e, taskKind, d.mergeWinner); !reflect.DeepEqual(winner["formerIds"], []any{"m-b"}) {
		t.Errorf("the merge winner's former ids = %v, want [m-b]", winner["formerIds"])
	}
	if loser := getRecord(t, e, taskKind, "m-b"); loser["canonicalId"] != d.mergeWinner {
		t.Errorf("the merged-away id resolves to %v, want %s", loser["canonicalId"], d.mergeWinner)
	}
	if split := getRecord(t, e, taskKind, "s-b"); split["canonicalId"] != nil || split["deletedAt"] != nil {
		t.Errorf("the split loser is not its own live record again: %v", split)
	}
	if status, _ := e.Do(http.MethodGet, recordPath(taskKind, "purge-me"), nil); status != http.StatusNotFound {
		t.Errorf("the purged record came back: %d", status)
	}
	if tombs := listRecords(t, e, taskKind, true); containsID(tombs, "purge-me") || !containsID(tombs, "stays-deleted") {
		t.Errorf("task tombstones after the restore = %v", tombs)
	}
	subject := getRecord(t, e, subjectKind, d.subjectID)
	meta := subject["propertyMeta"].(map[string]any)["name"].(map[string]any)
	if alts, _ := meta["alternatives"].([]any); meta["manager"] != "api" || len(alts) != 2 {
		t.Errorf("subject name provenance after the restore = %v, want the owner's hold over two offers", meta)
	}
	if got := d.restored.kindVersions[corePkg+"/run"]; got != d.runVersion {
		t.Errorf("run declared at %d after the restore, want the shipped %d", got, d.runVersion)
	}
	if got := d.restored.kindVersions[fileKind]; got != 2 {
		t.Errorf("file declared at %d after the restore, want 2", got)
	}
	file := getRecord(t, e, fileKind, "report")
	if data, _ := file["properties"].(map[string]any)["data"].(map[string]any); data["digest"] != d.blobDigest || data["status"] != "stored" {
		t.Errorf("the attachment reference resolves to %v, want the stored manifest %s", file["properties"].(map[string]any)["data"], d.blobDigest)
	}

	// The delivery ledger before dispatch resumes: the parked webhook under
	// its retry id, the paged drain at its cursor, the held fire pending.
	if got := d.restored.parked["on-hook"]; len(got) != 1 || got[0].ID != d.hookFailureID || got[0].FireID != d.hookFireID {
		t.Errorf("restored on-hook parked = %+v, want failure %d for fire %s", got, d.hookFailureID, d.hookFireID)
	}
	if got := d.restored.parked["on-page"]; len(got) != 1 || got[0].ID != d.pagedFailureID {
		t.Errorf("restored on-page parked = %+v, want failure %d", got, d.pagedFailureID)
	}
	if got, ok := d.restored.pagedCursors[d.pagedChain]; !ok || got != 2 {
		t.Errorf("restored paged cursor for %s = %v (%v), want 2", d.pagedChain, got, ok)
	}
	if st := d.restored.statuses["on-hold"]; st.Pending != 1 {
		t.Errorf("restored on-hold status = %+v, want the held fire pending", st)
	}
	if status, _ := e.Do(http.MethodGet, recordPath(echoKind, "hold-echo"), nil); status != http.StatusNotFound {
		t.Errorf("the held fire's echo exists before dispatch resumed: %d", status)
	}
	if status, _ := e.Do(http.MethodGet, recordPath(taskKind, "t-w2"), nil); status != http.StatusNotFound {
		t.Errorf("the pending delivery landed before dispatch resumed: t-w2 reads %d", status)
	}

	// Authenticate with the original password and TOTP, one step later on
	// the shared clock so the registration's code is not replayed.
	d.clock.Advance(engine.TOTPPeriod)
	status, raw := e.Login(drillUser, drillPassword, e.TOTPCode())
	if status != http.StatusCreated {
		t.Errorf("login on the restored host: %d %s", status, raw)
	}
	if status, raw := e.Login(drillUser, "not-the-password", e.TOTPCode()); status/100 == 2 {
		t.Errorf("a wrong password logged in on the restored host: %d %s", status, raw)
	}

	// The attachment's bytes.
	if got := d.restored.blobs[d.blobDigest]; !bytes.Equal(got, []byte(fileBytesTxt)) {
		t.Errorf("attachment bytes after the restore = %q", got)
	}
	sum := sha256.Sum256([]byte(fileBytesTxt))
	if want := "blob-sha256-" + hex.EncodeToString(sum[:]); d.blobDigest != want {
		t.Errorf("attachment digest %s is not the bytes' %s", d.blobDigest, want)
	}

	// The embedding drain: the restored repository buys its vectors with the
	// sealed apiKey, and the semantic ranking converges on the source's.
	drainEmbeds(t, ds)
	auths, texts := d.embed.seen()
	if texts == 0 {
		t.Errorf("the restored drain embedded nothing")
	}
	for _, a := range auths {
		if a != "Bearer "+embedAPIKey {
			t.Errorf("the restored drain sent %q, want the sealed apiKey", a)
		}
	}
	for q, want := range d.source.semantic {
		got := gqlSearch(t, e, q, "semantic", []string{taskKind})
		if !reflect.DeepEqual(got, want) {
			t.Errorf("semantic %q after the drain = %+v, want the source's %+v", q, got, want)
		}
	}

	// Dispatch resumes: the held fire completes under its fire id, the
	// pending mirror delivers once, the settled one is not delivered again.
	if status, raw := e.Do(http.MethodPut, recordPath(flagKind, "release"), map[string]any{"properties": map[string]any{"name": "release"}}); status/100 != 2 {
		t.Fatalf("put release: %d %s", status, raw)
	}
	if _, err := ds.(substrate.TriggerDispatcher).ProcessTriggers(ctx); err != nil {
		t.Fatalf("dispatcher pass after the restore: %v", err)
	}
	if echo := getRecord(t, e, echoKind, "hold-echo"); echo["properties"].(map[string]any)["fire"] != d.holdFireID ||
		echo["properties"].(map[string]any)["name"] != `{"say":"hold the line"}` {
		t.Errorf("the held fire's echo = %v, want fire %s with its body", echo["properties"], d.holdFireID)
	}
	if st := statusOf(t, e, "on-hold"); st.Pending != 0 || st.Parked != 0 {
		t.Errorf("on-hold after the pass = %+v, want nothing pending", st)
	}
	if w2 := getRecord(t, e, taskKind, "t-w2"); w2["version"] != float64(1) {
		t.Errorf("the pending delivery landed at version %v, want one delivery", w2["version"])
	}
	if w1 := getRecord(t, e, taskKind, "t-w1"); w1["version"] != float64(1) {
		t.Errorf("the settled delivery was delivered again: version %v", w1["version"])
	}

	// The parked webhook retries by hand under its restored id once its
	// gate opens.
	if status, raw := e.Do(http.MethodPut, recordPath(flagKind, "hook-gate"), map[string]any{"properties": map[string]any{"name": "hook-gate"}}); status/100 != 2 {
		t.Fatalf("put hook-gate: %d %s", status, raw)
	}
	status, raw = e.Do(http.MethodPost, fmt.Sprintf("/api/v1/%s/trigger/on-hook/parked/%d/retry", corePkg, d.hookFailureID), nil)
	if status/100 != 2 {
		t.Errorf("retry the parked webhook %d: %d %s", d.hookFailureID, status, raw)
	}
	if echo := getRecord(t, e, echoKind, "hook-echo"); echo["properties"].(map[string]any)["fire"] != d.hookFireID ||
		echo["properties"].(map[string]any)["want"] != "parked" || echo["properties"].(map[string]any)["name"] != `{"say":"call the dentist"}` {
		t.Errorf("the retried webhook's echo = %v, want fire %s with its header and body", echo["properties"], d.hookFireID)
	}
	if got := parkedOf(t, e, "on-hook"); len(got) != 0 {
		t.Errorf("on-hook still parks %+v after the retry", got)
	}

	// The paged import continues from its cursor: pages 0 and 1 are erased
	// first, so a restart from zero would recreate them.
	for _, id := range []string{"p-0", "p-1"} {
		if status, raw := e.Do(http.MethodDelete, recordPath(taskKind, id), nil); status/100 != 2 {
			t.Fatalf("delete %s: %d %s", id, status, raw)
		}
	}
	if status, raw := e.Do(http.MethodPut, recordPath(flagKind, "page-gate"), map[string]any{"properties": map[string]any{"name": "page-gate"}}); status/100 != 2 {
		t.Fatalf("put page-gate: %d %s", status, raw)
	}
	status, raw = e.Do(http.MethodPost, fmt.Sprintf("/api/v1/%s/trigger/on-page/parked/%d/retry", corePkg, d.pagedFailureID), nil)
	if status/100 != 2 {
		t.Errorf("retry the parked drain %d: %d %s", d.pagedFailureID, status, raw)
	}
	for _, id := range []string{"p-2", "p-3", "p-4"} {
		if status, _ := e.Do(http.MethodGet, recordPath(taskKind, id), nil); status != http.StatusOK {
			t.Errorf("resumed page %s reads %d, want 200", id, status)
		}
	}
	for _, id := range []string{"p-0", "p-1"} {
		// A tombstone still reads, with deletedAt set; a re-run would have
		// restored it.
		if rec := getRecord(t, e, taskKind, id); rec["deletedAt"] == nil {
			t.Errorf("page %s was re-run: the retry restarted from zero (version %v)", id, rec["version"])
		}
	}
	if got := parkedOf(t, e, "on-page"); len(got) != 0 {
		t.Errorf("on-page still parks %+v after the resumed drain", got)
	}
	if cursors := pagedCursors(t, scopedB); len(cursors) != 0 {
		t.Errorf("paged_cursors after the drain finished = %v, want none", cursors)
	}
}

// --- stage 5 --------------------------------------------------------------------

// interruptedImport boots the rewrapped copy into fresh schemas and kills the
// boot at the import's durable steps (the progress-marker hook, #365): after
// the first batch, then after the first fold pass, then a clean boot. Each
// kill leaves the marker, and the boot that finishes reproduces the fold.
func (d *drill) interruptedImport(t *testing.T) {
	ctx := context.Background()
	const batch = 40
	batches := int((d.snapHead + batch - 1) / batch)
	if batches < 3 {
		t.Fatalf("head %d spans %d batches of %d; the schedule needs at least 3", d.snapHead, batches, batch)
	}
	errKilled := errors.New("the process died here")
	crashes := []struct {
		stage string
		nth   int
	}{
		{engine.ImportAfterBatch, 1},
		{engine.ImportAfterBatch, batches - 2},
		{engine.ImportAfterFirstFold, 1},
	}
	root := copyRoot(t, d.pristine, drillAuthority)
	dsn := testdb.NewSchema(t)
	for i, c := range crashes {
		seen := 0
		svc, err := engine.Open(ctx, dsn, engine.WithKindsFS(kinds.Seed()),
			engine.WithDataRoot(root), engine.WithCredentialKey(d.keyB),
			engine.WithTestImportFault(batch, func(stage string) error {
				if stage != c.stage {
					return nil
				}
				seen++
				if seen == c.nth {
					return errKilled
				}
				return nil
			}))
		if err == nil {
			_ = svc.Close()
			t.Fatalf("boot %d did not die %s (occurrence %d)", i+1, c.stage, c.nth)
		}
		if !errors.Is(err, errKilled) {
			t.Fatalf("boot %d failed elsewhere: %v", i+1, err)
		}
		incomplete, err := engine.ImportIncomplete(ctx, rawDB(t, dsn))
		if err != nil {
			t.Fatal(err)
		}
		if !incomplete {
			t.Errorf("boot %d died %s and left no import-progress marker", i+1, c.stage)
		}
		if n := countRows(t, dsn, "changelog"); int64(n) > d.snapHead {
			t.Errorf("boot %d left %d changelog rows, the history has %d", i+1, n, d.snapHead)
		}
		// What the crash left is not served: a read-only process refuses the
		// repository until the boot finishes the import.
		ro, err := engine.Open(ctx, dsn, engine.WithKindsFS(kinds.Seed()),
			engine.WithDataRoot(root), engine.WithCredentialKey(d.keyB), engine.WithDirectoryReadOnly())
		if err != nil {
			t.Fatalf("read-only open after boot %d: %v", i+1, err)
		}
		if _, err := ro.Dataset(ctx, drillUser); !errors.Is(err, engine.ErrImportIncomplete) {
			t.Errorf("a read-only open after boot %d = %v, want ErrImportIncomplete", i+1, err)
		}
		_ = ro.Close()
	}

	svc, err := engine.Open(ctx, dsn, engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(root), engine.WithCredentialKey(d.keyB), engine.WithTestTOTPClock(d.clock.Now))
	if err != nil {
		t.Fatalf("the boot after the crashes: %v", err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, drillUser)
	if err != nil {
		t.Fatalf("open the repository after the import resumed: %v", err)
	}
	if incomplete, err := engine.ImportIncomplete(ctx, rawDB(t, dsn)); err != nil || incomplete {
		t.Errorf("the import-progress marker outlived the import: %v %v", incomplete, err)
	}
	fold, err := ds.(folded).FoldSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fold, d.source.fold) {
		t.Errorf("the resumed import's fold is not the source's\n%s", firstDifference(d.source.fold, fold))
	}
	head, err := ds.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if head.Seq != d.snapHead {
		t.Errorf("head after the resumed import = %d, want %d", head.Seq, d.snapHead)
	}
	if head.Generation == d.source.gen {
		t.Errorf("the resumed import kept the source's history generation %q", head.Generation)
	}
	report, err := svc.(verifier).VerifyRepository(ctx, drillUser)
	if err != nil || !report.OK || report.Head != d.snapHead || report.SealedOpened != report.SealedFiles {
		t.Errorf("the resumed repository does not verify: %+v %v", report, err)
	}
	d.clock.Advance(engine.TOTPPeriod)
	code, err := engine.TOTPCode(d.envA.TOTPSecret, engine.TOTPStep(d.clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{Username: drillUser, Password: drillPassword, TOTPCode: code, Label: "resumed"}); err != nil {
		t.Errorf("login after the resumed import: %v", err)
	}
	t.Logf("interrupted import: %d batches of %d, killed after batch 1, after batch %d and after the first fold pass; the fourth boot finished it", batches, batch, batches-2)
}

// --- stage 6 --------------------------------------------------------------------

// formatTransition takes the legacy repository's snapshot through the oldest
// format the reader accepts, the v0.47.0 through v0.51.0 directory: format 1
// manifest stamped changelog dialect 2 with no vocabulary dialect, and lines
// without a transaction frame (#363). It is rewrapped under the new key and
// imported into an empty schema. Then a manifest above the binary's dialect
// is refused by name before any row lands.
func (d *drill) formatTransition(t *testing.T) {
	ctx := context.Background()

	// The boot reads format 1 itself: the same-key restore, a directory a
	// v0.51 binary wrote copied under a server holding the key it was written
	// under, imported and upgraded in place by the boot alone. No rewrap runs
	// first, so a reader that stopped accepting format 1 fails here.
	root := formatOneCopy(t, d.snapRoot)
	dir, err := changelogfile.RepoDir(root, legacyAuthority)
	if err != nil {
		t.Fatal(err)
	}
	dsn := testdb.NewSchema(t)
	e := testenv.Start(t,
		testenv.WithUser(legacyUser, legacyPassword), testenv.WithoutRegistration(),
		testenv.WithDSN(dsn), testenv.WithDataRoot(root), testenv.WithCredentialKey(d.keyA),
		testenv.WithClock(d.clock.Now))
	e.TOTPSecret, e.Authority = d.legacy.TOTPSecret, legacyAuthority
	ds, err := e.Service.Dataset(ctx, legacyUser)
	if err != nil {
		t.Fatalf("open the repository the boot imported from format 1: %v", err)
	}
	if upgraded, err := changelogfile.ReadManifest(dir); err != nil || upgraded.Format != changelogfile.ManifestFormat || upgraded.ChangelogDialect != 2 {
		t.Errorf("manifest after the format-1 import = %+v (%v), want format %d at the manifest's dialect 2", upgraded, err, changelogfile.ManifestFormat)
	}
	fold, err := ds.(folded).FoldSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fold, d.legacyFold) {
		t.Errorf("the fold restored from format 1 is not the source's\n%s", firstDifference(d.legacyFold, fold))
	}
	head, err := ds.Head(ctx)
	if err != nil || head.Seq != d.legacyHead {
		t.Errorf("head after the format-1 import = %d (%v), want %d", head.Seq, err, d.legacyHead)
	}
	scoped := openScoped(t, dsn, legacyAuthority)
	var stamped int
	if err := scoped.QueryRowContext(ctx, `SELECT dialect FROM changelog_dialect`).Scan(&stamped); err != nil || stamped != 2 {
		t.Errorf("changelog dialect after the import = %d (%v), want the manifest's 2", stamped, err)
	}
	d.clock.Advance(engine.TOTPPeriod)
	if status, raw := e.Login(legacyUser, legacyPassword, e.TOTPCode()); status != http.StatusCreated {
		t.Errorf("login on the format-1 restore: %d %s", status, raw)
	}
	legacyTask := legacyAuthority + "/tasks/task"
	l0 := getRecord(t, e, legacyTask, "l-0")
	if labels, _ := l0["labels"].(map[string]any); len(labels) != 0 {
		t.Errorf("the cleared label came back through format 1: %v", l0["labels"])
	}
	if l1 := getRecord(t, e, legacyTask, "l-1"); l1["labels"].(map[string]any)["owner/legacy"] != true {
		t.Errorf("l-1 lost its label: %v", l1["labels"])
	}
	// The first write moves the stamp and the manifest together.
	if status, raw := e.Do(http.MethodPut, recordPath(legacyTask, "l-after"), map[string]any{"properties": map[string]any{"name": "after"}}); status/100 != 2 {
		t.Fatalf("write after the format-1 import: %d %s", status, raw)
	}
	if err := scoped.QueryRowContext(ctx, `SELECT dialect FROM changelog_dialect`).Scan(&stamped); err != nil || stamped != engine.MaxChangelogDialect() {
		t.Errorf("changelog dialect after the first write = %d (%v), want %d", stamped, err, engine.MaxChangelogDialect())
	}
	if moved, err := changelogfile.ReadManifest(dir); err != nil || moved.ChangelogDialect != engine.MaxChangelogDialect() {
		t.Errorf("manifest after the first write = %+v (%v), want changelog dialect %d", moved, err, engine.MaxChangelogDialect())
	}

	// The other-key restore of the same format: `repository rewrap` reads the
	// format-1 manifest, opens the DEK with the recovery key and rewrites the
	// manifest under the new host's key; the boot then imports it.
	rewrapRoot := formatOneCopy(t, d.snapRoot)
	rewrapDir, err := changelogfile.RepoDir(rewrapRoot, legacyAuthority)
	if err != nil {
		t.Fatal(err)
	}
	report, err := engine.RewrapRepositoryDir(rewrapDir, d.legacyIdentity.String(), d.keyB)
	if err != nil {
		t.Fatalf("rewrap the format-1 directory: %v", err)
	}
	if report.Repository != legacyAuthority || report.Username != legacyUser {
		t.Errorf("rewrap report = %+v", report)
	}
	if rewrapped, err := changelogfile.ReadManifest(rewrapDir); err != nil || rewrapped.Format != changelogfile.ManifestFormat || rewrapped.ChangelogDialect != 2 {
		t.Errorf("the rewrapped manifest = %+v (%v), want format %d at changelog dialect 2", rewrapped, err, changelogfile.ManifestFormat)
	}
	rewrapDSN := testdb.NewSchema(t)
	rewrapped, err := engine.Open(ctx, rewrapDSN, engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(rewrapRoot), engine.WithCredentialKey(d.keyB), engine.WithTestTOTPClock(d.clock.Now))
	if err != nil {
		t.Fatalf("boot on the rewrapped format-1 directory: %v", err)
	}
	defer func() { _ = rewrapped.Close() }()
	rds, err := rewrapped.Dataset(ctx, legacyUser)
	if err != nil {
		t.Fatalf("open the rewrapped repository: %v", err)
	}
	if rfold, err := rds.(folded).FoldSnapshot(ctx); err != nil || !bytes.Equal(rfold, d.legacyFold) {
		t.Errorf("the fold restored from the rewrapped format-1 directory is not the source's (%v)\n%s", err, firstDifference(d.legacyFold, rfold))
	}
	d.clock.Advance(engine.TOTPPeriod)
	code, err := engine.TOTPCode(d.legacy.TOTPSecret, engine.TOTPStep(d.clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := rewrapped.Login(ctx, substrate.LoginInput{Username: legacyUser, Password: legacyPassword, TOTPCode: code, Label: "rewrapped"}); err != nil {
		t.Errorf("login on the rewrapped format-1 restore: %v", err)
	}

	// A directory a NEWER binary wrote refuses the boot by name, before any
	// row lands.
	newerRoot := copyRoot(t, d.pristine, drillAuthority)
	newerDir, err := changelogfile.RepoDir(newerRoot, drillAuthority)
	if err != nil {
		t.Fatal(err)
	}
	nm, err := changelogfile.ReadManifest(newerDir)
	if err != nil {
		t.Fatal(err)
	}
	nm.ChangelogDialect = engine.MaxChangelogDialect() + 1
	if err := changelogfile.WriteManifest(newerDir, nm); err != nil {
		t.Fatal(err)
	}
	newerDSN := testdb.NewSchema(t)
	svc, err := engine.Open(ctx, newerDSN, engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(newerRoot), engine.WithCredentialKey(d.keyB))
	if err == nil {
		_ = svc.Close()
		t.Fatal("a directory above the binary's changelog dialect imported")
	}
	if !errors.Is(err, engine.ErrChangelogDialectNewer) {
		t.Errorf("the refusal = %v, want ErrChangelogDialectNewer", err)
	}
	for _, table := range []string{"repositories", "changelog", "records", "import_progress"} {
		if n := countRows(t, newerDSN, table); n != 0 {
			t.Errorf("the refused import left %d %s row(s)", n, table)
		}
	}
}

// --- stage 7 --------------------------------------------------------------------

// replacedHistoryCursor is a client that saved a change cursor against the
// source and resumes against the restore (#374): the resume is refused with
// `compacted`, the new generation and the head, and a re-list carries the
// handoff that resumes cleanly.
func (d *drill) replacedHistoryCursor(t *testing.T) {
	e := d.envB.For(t)
	e.Token = d.envA.Token
	if d.restored.gen == d.source.gen {
		t.Errorf("the restore kept the source's history generation %q", d.source.gen)
	}
	// The head the reset names is the restored history's head NOW, after
	// stage 4's writes, not the head at the import.
	status, raw := e.Do(http.MethodGet, "/api/v1/changes?first=1", nil)
	if status != http.StatusOK {
		t.Fatalf("history page: %d %s", status, raw)
	}
	var current substrate.ChangePage
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	if current.Generation != d.restored.gen || current.Head < d.restored.head {
		t.Errorf("history page = head %d under %q, want at least %d under %q", current.Head, current.Generation, d.restored.head, d.restored.gen)
	}
	saved := fmt.Sprintf("from=%d&generation=%s", d.source.head-1, url.QueryEscape(d.source.gen))
	for _, path := range []string{"/api/v1/changes?" + saved, "/api/v1/changes?watch=1&" + saved, recordPath(taskKind, "") + "?watch=1&" + saved} {
		status, raw := e.Do(http.MethodGet, path, nil)
		if status != http.StatusGone {
			t.Errorf("GET %s: %d %s, want 410", path, status, raw)
			continue
		}
		var env substrate.ErrorEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Errorf("GET %s: %v (%s)", path, err, raw)
			continue
		}
		if env.Error.Code != "compacted" || env.Error.Generation != current.Generation || env.Error.Head == nil || *env.Error.Head != current.Head {
			var head int64 = -1
			if env.Error.Head != nil {
				head = *env.Error.Head
			}
			t.Errorf("GET %s: reset = code %q head %d generation %q (%s), want compacted at head %d under %q",
				path, env.Error.Code, head, env.Error.Generation, env.Error.Message, current.Head, current.Generation)
		}
	}
	// The re-list carries the handoff, and the resume under it streams a
	// bookmark at the head.
	status, raw = e.Do(http.MethodGet, recordPath(taskKind, "")+"?first=500", nil)
	if status != http.StatusOK {
		t.Fatalf("re-list: %d %s", status, raw)
	}
	var page substrate.Page
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	if page.Generation != d.restored.gen || page.Head < d.restored.head {
		t.Errorf("re-list handoff = head %d under %q, want at least %d under %q", page.Head, page.Generation, d.restored.head, d.restored.gen)
	}
	line := firstStreamLine(t, e, fmt.Sprintf("/api/v1/changes?watch=1&from=%d&generation=%s", page.Head, url.QueryEscape(page.Generation)))
	var bookmark struct {
		Bookmark   int64  `json:"bookmark"`
		Generation string `json:"generation"`
	}
	if err := json.Unmarshal(line, &bookmark); err != nil || bookmark.Bookmark != page.Head || bookmark.Generation != page.Generation {
		t.Errorf("watch under the handoff opened with %s (%v), want bookmark %d under %q", line, err, page.Head, page.Generation)
	}
}

// --- capturing and comparing state ------------------------------------------------

// drillCollections is every collection the drill compares record by record.
func drillCollections() []string {
	return []string{
		taskKind, projectKind, personKind, widgetKind, gadgetKind, flagKind, echoKind,
		subjectKind, contactKind, memberKind, fileKind, oauthKind,
		corePkg + "/llmprovider", corePkg + "/trigger", corePkg + "/recordmerge", corePkg + "/recordsplit",
		corePkg + "/blob", corePkg + "/token", corePkg + "/credential", corePkg + "/recoverykey",
		corePkg + "/repository", corePkg + "/run", corePkg + "/kind", corePkg + "/function",
		corePkg + "/recordmapping", corePkg + "/package", corePkg + "/bundle",
	}
}

var (
	lexicalQueries  = []string{"ledger", "fold", "restored"}
	semanticQueries = []string{"carry the values", "replay the ledger"}
	// sampleImports is the samples a repository imports before it holds a
	// task, in the order their `requires` demand.
	sampleImports = []string{
		"samples.substrate.reamde.dev/people",
		"samples.substrate.reamde.dev/scheduling",
		"samples.substrate.reamde.dev/tasks",
	}
)

func captureState(t *testing.T, e *testenv.Env, ds substrate.Dataset, scoped *sql.DB) *state {
	t.Helper()
	ctx := context.Background()
	s := &state{
		records: map[string]any{}, deleted: map[string]any{}, statuses: map[string]substrate.TriggerStatus{},
		parked: map[string][]substrate.TriggerFailure{}, blobs: map[string][]byte{},
		lexical: map[string]searchAnswer{}, semantic: map[string]searchAnswer{},
	}
	for _, kind := range drillCollections() {
		for _, rec := range listRecords(t, e, kind, false) {
			id, _ := rec.(map[string]any)["id"].(string)
			s.records[kind+"/"+id] = getRecord(t, e, kind, id)
		}
		s.deleted[kind] = listRecords(t, e, kind, true)
	}
	s.kindVersions = kindVersions(t, e)
	for _, st := range statuses(t, e) {
		s.statuses[st.ID] = st
		s.parked[st.ID] = parkedOf(t, e, st.ID)
	}
	s.pagedCursors = pagedCursors(t, scoped)
	if err := scoped.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE op = 'delivery'`).Scan(&s.deliveries); err != nil {
		t.Fatal(err)
	}
	var err error
	if s.fold, err = ds.(folded).FoldSnapshot(ctx); err != nil {
		t.Fatalf("fold snapshot: %v", err)
	}
	for _, rec := range listRecords(t, e, corePkg+"/blob", false) {
		m := rec.(map[string]any)
		if m["properties"].(map[string]any)["status"] != "stored" {
			continue
		}
		digest := m["id"].(string)
		status, raw, _ := e.DoRaw(http.MethodGet, "/api/v1/blobs/"+digest, nil, nil)
		if status != http.StatusOK {
			t.Errorf("GET blob %s: %d %s", digest, status, raw)
			continue
		}
		s.blobs[digest] = raw
	}
	for _, q := range lexicalQueries {
		s.lexical[q] = gqlSearch(t, e, q, "lexical", []string{taskKind})
	}
	for _, q := range semanticQueries {
		s.semantic[q] = gqlSearch(t, e, q, "semantic", []string{taskKind})
	}
	head, err := ds.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.head, s.gen = head.Seq, head.Generation
	return s
}

// compareStates holds `got` to `want` field by field. Every difference is
// its own failing assertion, so one run lists them all.
func compareStates(t *testing.T, label string, want, got *state) {
	t.Helper()
	keys := sortedKeys(want.records)
	for _, k := range keys {
		g, ok := got.records[k]
		if !ok {
			t.Errorf("%s: record %s is missing", label, k)
			continue
		}
		if diff := firstJSONDifference(want.records[k], g); diff != "" {
			t.Errorf("%s: record %s differs: %s", label, k, diff)
		}
	}
	for _, k := range sortedKeys(got.records) {
		if _, ok := want.records[k]; !ok {
			t.Errorf("%s: record %s exists and the source had none", label, k)
		}
	}
	for _, kind := range drillCollections() {
		if diff := firstJSONDifference(want.deleted[kind], got.deleted[kind]); diff != "" {
			t.Errorf("%s: the tombstones of %s differ: %s", label, kind, diff)
		}
	}
	if !reflect.DeepEqual(want.kindVersions, got.kindVersions) {
		t.Errorf("%s: kind versions differ:\n%v\n%v", label, want.kindVersions, got.kindVersions)
	}
	for id, w := range want.statuses {
		g, ok := got.statuses[id]
		if !ok {
			t.Errorf("%s: trigger %s has no status", label, id)
			continue
		}
		// The cursor and the lag are scan positions: a restore comes back at
		// the last acknowledged delivery, never past it (decision 0064).
		if g.Enabled != w.Enabled || g.Kind != w.Kind || g.Parked != w.Parked || g.Pending != w.Pending || g.Error != w.Error {
			t.Errorf("%s: trigger %s status = %+v, want %+v", label, id, g, w)
		}
		if g.Cursor > w.Cursor {
			t.Errorf("%s: trigger %s came back at cursor %d, past the source's %d", label, id, g.Cursor, w.Cursor)
		}
		if diff := firstJSONDifference(want.parked[id], got.parked[id]); diff != "" {
			t.Errorf("%s: trigger %s parked list differs: %s", label, id, diff)
		}
	}
	if !reflect.DeepEqual(want.pagedCursors, got.pagedCursors) {
		t.Errorf("%s: paged cursors = %v, want %v", label, got.pagedCursors, want.pagedCursors)
	}
	if want.deliveries != got.deliveries {
		t.Errorf("%s: %d delivery entries, want %d", label, got.deliveries, want.deliveries)
	}
	if !bytes.Equal(want.fold, got.fold) {
		t.Errorf("%s: the fold differs from the source's\n%s", label, firstDifference(want.fold, got.fold))
	}
	for digest, w := range want.blobs {
		if g, ok := got.blobs[digest]; !ok || !bytes.Equal(g, w) {
			t.Errorf("%s: blob %s bytes differ (present %v)", label, digest, ok)
		}
	}
	for q, w := range want.lexical {
		if g := got.lexical[q]; !reflect.DeepEqual(g, w) {
			t.Errorf("%s: lexical %q = %+v, want %+v", label, q, g, w)
		}
	}
	if want.head != got.head {
		t.Errorf("%s: head %d, want %d", label, got.head, want.head)
	}
}

// --- HTTP helpers ------------------------------------------------------------------

func recordPath(kind, id string) string {
	p := "/api/v1/" + kind
	if id != "" {
		p += "/" + url.PathEscape(id)
	}
	return p
}

func decodeAny(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return v
}

func getRecord(t *testing.T, e *testenv.Env, kind, id string) map[string]any {
	t.Helper()
	status, raw := e.Do(http.MethodGet, recordPath(kind, id), nil)
	if status != http.StatusOK {
		t.Fatalf("GET %s/%s: %d %s", kind, id, status, raw)
	}
	return decodeAny(t, raw).(map[string]any)
}

// listRecords is one collection's records, live or tombstoned, in the
// collection's own order.
func listRecords(t *testing.T, e *testenv.Env, kind string, deleted bool) []any {
	t.Helper()
	path := recordPath(kind, "") + "?first=500"
	if deleted {
		path += "&filter=" + url.QueryEscape(`{"deleted":true}`)
	}
	status, raw := e.Do(http.MethodGet, path, nil)
	if status != http.StatusOK {
		t.Fatalf("list %s (deleted=%v): %d %s", kind, deleted, status, raw)
	}
	page := decodeAny(t, raw).(map[string]any)
	if page["cursor"] != nil && page["cursor"] != "" {
		t.Fatalf("list %s spans more than one page of 500", kind)
	}
	records, _ := page["records"].([]any)
	return records
}

func containsID(records []any, id string) bool {
	for _, r := range records {
		if m, ok := r.(map[string]any); ok && m["id"] == id {
			return true
		}
	}
	return false
}

// kindVersions is every installed kind's declaration version.
func kindVersions(t *testing.T, e *testenv.Env) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, rec := range listRecords(t, e, corePkg+"/kind", false) {
		m := rec.(map[string]any)
		v, _ := m["properties"].(map[string]any)["version"].(float64)
		out[m["id"].(string)] = int64(v)
	}
	return out
}

func statuses(t *testing.T, e *testenv.Env) []substrate.TriggerStatus {
	t.Helper()
	status, raw := e.Do(http.MethodGet, "/api/v1/"+corePkg+"/trigger/status", nil)
	if status != http.StatusOK {
		t.Fatalf("trigger status: %d %s", status, raw)
	}
	var out substrate.OperationalList[substrate.TriggerStatus]
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode trigger status: %v (%s)", err, raw)
	}
	return out.Items
}

func statusOf(t *testing.T, e *testenv.Env, id string) substrate.TriggerStatus {
	t.Helper()
	for _, st := range statuses(t, e) {
		if st.ID == id {
			return st
		}
	}
	t.Fatalf("no status for trigger %s", id)
	return substrate.TriggerStatus{}
}

func parkedOf(t *testing.T, e *testenv.Env, id string) []substrate.TriggerFailure {
	t.Helper()
	status, raw := e.Do(http.MethodGet, "/api/v1/"+corePkg+"/trigger/"+id+"/parked", nil)
	if status != http.StatusOK {
		t.Fatalf("parked of %s: %d %s", id, status, raw)
	}
	var out substrate.OperationalList[substrate.TriggerFailure]
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode parked of %s: %v (%s)", id, err, raw)
	}
	return out.Items
}

// gqlSearch runs one search through GraphQL, the one door search has.
func gqlSearch(t *testing.T, e *testenv.Env, q, mode string, kinds []string) searchAnswer {
	t.Helper()
	status, raw := e.Do(http.MethodPost, "/api/v1/graphql", map[string]any{
		"query":     `query($q: String!, $mode: String, $kinds: [String!]) { search(q: $q, mode: $mode, kinds: $kinds, k: 10) { hits { record { id } } pending } }`,
		"variables": map[string]any{"q": q, "mode": mode, "kinds": kinds},
	})
	if status != http.StatusOK {
		t.Fatalf("graphql search: %d %s", status, raw)
	}
	var out struct {
		Data struct {
			Search *struct {
				Hits []struct {
					Record struct {
						ID string `json:"id"`
					} `json:"record"`
				} `json:"hits"`
				Pending int `json:"pending"`
			} `json:"search"`
		} `json:"data"`
		Errors []struct {
			Message    string         `json:"message"`
			Extensions map[string]any `json:"extensions"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode graphql search: %v (%s)", err, raw)
	}
	var a searchAnswer
	if len(out.Errors) > 0 {
		a.ErrMsg = out.Errors[0].Message
		a.ErrCode, _ = out.Errors[0].Extensions["code"].(string)
		return a
	}
	if out.Data.Search == nil {
		t.Fatalf("graphql search answered neither hits nor an error: %s", raw)
	}
	a.IDs = []string{}
	for _, h := range out.Data.Search.Hits {
		a.IDs = append(a.IDs, h.Record.ID)
	}
	a.Pending = out.Data.Search.Pending
	return a
}

// postWebhook posts one JSON request to the public webhook door and returns
// the fire id. The request is built here, not through the harness, so the
// stage can hold it to carrying no bearer: the door authenticates by path and
// key alone, and a drill that sent a token would pass against a door that
// started demanding one.
func postWebhook(t *testing.T, e *testenv.Env, trigger, body, event string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.URL+"/webhooks/"+drillAuthority+"/"+trigger, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", event)
	if _, has := req.Header["Authorization"]; has {
		t.Fatalf("the webhook request carries an Authorization header")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("webhook %s: %v", trigger, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("webhook %s without a bearer: %d %s, want 202", trigger, resp.StatusCode, raw)
	}
	var accepted substrate.WebhookAccepted
	if err := json.Unmarshal(raw, &accepted); err != nil || accepted.Fire == "" {
		t.Fatalf("webhook %s answered %s: %v", trigger, raw, err)
	}
	return accepted.Fire
}

// firstStreamLine opens a watch and returns its first frame.
func firstStreamLine(t *testing.T, e *testenv.Env, path string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("watch %s: %d %s", path, resp.StatusCode, body)
	}
	line, err := bufio.NewReader(resp.Body).ReadBytes('\n')
	if err != nil {
		t.Fatalf("watch: read the first frame: %v", err)
	}
	return bytes.TrimSpace(line)
}

// refID reads the id a stored reference names: the `{ref: "<kind>/<id>"}`
// object, or the bare path.
func refID(v any) string {
	var path string
	switch x := v.(type) {
	case string:
		path = x
	case map[string]any:
		path, _ = x["ref"].(string)
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return ""
}

// --- the engine and the files -------------------------------------------------------

func openScoped(t *testing.T, dsn, authority string) *sql.DB {
	t.Helper()
	db, err := engine.OpenScopedDB(dsn, authority, engine.RoleApp)
	if err != nil {
		t.Fatalf("open the repository's pool: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func rawDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func countRows(t *testing.T, dsn, table string) int {
	t.Helper()
	var n int
	if err := rawDB(t, dsn).QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// pagedCursors reads the delivery ledger's resume cursors, keyed by chain.
func pagedCursors(t *testing.T, db *sql.DB) map[string]float64 {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT chain, cursor FROM paged_cursors ORDER BY chain`)
	if err != nil {
		t.Fatalf("read paged_cursors: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]float64{}
	for rows.Next() {
		var chain string
		var raw []byte
		if err := rows.Scan(&chain, &raw); err != nil {
			t.Fatal(err)
		}
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("decode paged cursor %q: %v", raw, err)
		}
		out[chain] = v
	}
	return out
}

// drainEmbeds runs the embed drain until the queue is empty, as the
// substrated loop would over its passes.
func drainEmbeds(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	for range 50 {
		n, err := ds.ProcessEmbedQueue(ctx, 50)
		if err != nil {
			t.Fatalf("embed drain: %v", err)
		}
		if n == 0 {
			return
		}
	}
	t.Fatal("the embed queue did not drain in 50 passes")
}

// copyRoot copies one repository directory out of a data root into a fresh
// root laid out the same way.
func copyRoot(t *testing.T, srcRoot, authority string) string {
	t.Helper()
	src, err := changelogfile.RepoDir(srcRoot, authority)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dst, err := changelogfile.RepoDir(root, authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	return root
}

// patchedCoreTree copies the shipped core package and pins one kind one
// version behind, so a repository seeded from it is a repository the shipped
// tree upgrades at the next boot. It returns the copy and the shipped version.
func patchedCoreTree(t *testing.T, file string) (string, int64) {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir(coreKindsDir)
	if err != nil {
		t.Fatalf("read the shipped core: %v", err)
	}
	var shipped int64
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(coreKindsDir, ent.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if ent.Name() == file {
			m := reDeclaredVersion.FindSubmatch(raw)
			if m == nil {
				t.Fatalf("%s pins no version of its own", file)
			}
			if _, err := fmt.Sscan(string(m[1]), &shipped); err != nil {
				t.Fatal(err)
			}
			raw = reDeclaredVersion.ReplaceAll(raw, []byte(fmt.Sprintf("\n  version: %d\n", shipped-1)))
		}
		if err := os.WriteFile(filepath.Join(dir, ent.Name()), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if shipped == 0 {
		t.Fatalf("%s is not in the shipped core", file)
	}
	return dir, shipped
}

// reDeclaredVersion is a declaration's own version line, at the `data:`
// block's indentation, so a property named `version` is never the match.
var reDeclaredVersion = regexp.MustCompile(`\n  version: (\d+)\n`)

// unframeChangelog re-encodes a changelog directory without transaction
// frames, the lines v0.46.0 through v0.51.0 wrote, recomputing each
// checksum.
func unframeChangelog(t *testing.T, dir string) {
	t.Helper()
	log, err := changelogfile.OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	var entries []changelogfile.Entry
	if err := log.Walk(func(e changelogfile.Entry) error {
		e.Payload = append(json.RawMessage(nil), e.Payload...)
		e.Txn = 0
		entries = append(entries, e)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	w, err := changelogfile.OpenWriter(dir, changelogfile.WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(entries); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// formatOneCopy copies the legacy repository out of the snapshot root as a
// v0.47.0 through v0.51.0 binary would have written it: lines without a
// transaction frame, a format-1 manifest, no snapshot.json.
func formatOneCopy(t *testing.T, snapRoot string) string {
	t.Helper()
	root := copyRoot(t, snapRoot, legacyAuthority)
	dir, err := changelogfile.RepoDir(root, legacyAuthority)
	if err != nil {
		t.Fatal(err)
	}
	m, err := changelogfile.ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	unframeChangelog(t, changelogfile.ChangelogDir(dir))
	writeFormatOneManifest(t, dir, m)
	if err := os.Remove(filepath.Join(dir, changelogfile.SnapshotName)); err != nil {
		t.Fatal(err)
	}
	return root
}

// writeFormatOneManifest writes the manifest v0.47.0 through v0.51.0 wrote:
// format 1, changelog dialect 2, no vocabulary dialect, no key id, no marker.
func writeFormatOneManifest(t *testing.T, dir string, m changelogfile.Manifest) {
	t.Helper()
	raw, err := json.MarshalIndent(map[string]any{
		"format": 1, "username": m.Username, "authority": m.Authority,
		"createdAt": m.CreatedAt.Format(changelogfile.TSFormat), "changelogDialect": 2,
		"dek": base64.StdEncoding.EncodeToString(m.DEK),
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, changelogfile.ManifestName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := changelogfile.ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format != 1 || got.ChangelogDialect != 2 || got.VocabularyDialect != 0 {
		t.Fatalf("the fixture's manifest is not the format-1 shape: %+v", got)
	}
}

// --- diffing ---------------------------------------------------------------------------

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func slicesContain(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// firstJSONDifference names the first path at which two decoded JSON values
// part, with both values there; "" when they are equal.
func firstJSONDifference(a, b any) string {
	return jsonDiff("$", a, b)
}

func jsonDiff(path string, a, b any) string {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: %s vs %s", path, short(a), short(b))
		}
		keys := map[string]bool{}
		for k := range x {
			keys[k] = true
		}
		for k := range y {
			keys[k] = true
		}
		for _, k := range sortedKeys(keys) {
			av, aok := x[k]
			bv, bok := y[k]
			if aok != bok {
				return fmt.Sprintf("%s.%s: %s vs %s", path, k, short(av), short(bv))
			}
			if d := jsonDiff(path+"."+k, av, bv); d != "" {
				return d
			}
		}
		return ""
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return fmt.Sprintf("%s: %s vs %s", path, short(a), short(b))
		}
		for i := range x {
			if d := jsonDiff(fmt.Sprintf("%s[%d]", path, i), x[i], y[i]); d != "" {
				return d
			}
		}
		return ""
	default:
		if !reflect.DeepEqual(a, b) {
			return fmt.Sprintf("%s: %s vs %s", path, short(a), short(b))
		}
		return ""
	}
}

func short(v any) string {
	raw, _ := json.Marshal(v)
	if len(raw) > 300 {
		return string(raw[:300]) + "..."
	}
	return string(raw)
}

// firstDifference names the fold section and the bytes where two fold
// snapshots part company.
func firstDifference(a, b []byte) string {
	var sa, sb map[string]json.RawMessage
	if json.Unmarshal(a, &sa) == nil && json.Unmarshal(b, &sb) == nil {
		names := map[string]bool{}
		for n := range sa {
			names[n] = true
		}
		for n := range sb {
			names[n] = true
		}
		for _, n := range sortedKeys(names) {
			if string(sa[n]) != string(sb[n]) {
				return "section " + n + ":\n" + firstByteDifference(sa[n], sb[n])
			}
		}
	}
	return firstByteDifference(a, b)
}

func firstByteDifference(a, b []byte) string {
	as, bs := string(a), string(b)
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		lo := max(0, i-300)
		return "before: ..." + as[lo:min(len(as), i+300)] + "\n\nafter:  ..." + bs[lo:min(len(bs), i+300)]
	}
	if len(as) == len(bs) {
		return "(identical)"
	}
	return "the snapshots differ in length"
}
