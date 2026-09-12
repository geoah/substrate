package testenv_test

// The release acceptance drill (#462, tracker #360): one repository holding
// every state the release protects, stopped and snapshotted, restored into an
// empty database on a host with another credential key through the recovery
// key, and compared. The drill as a whole skips under -short and without a
// database, like every database test; once it runs, no stage skips: a stage
// that cannot run fails with its reason, and a state the restore does not
// reproduce is a failing assertion, never a weakened one.
//
// The doors are the real ones. Everything a client would do arrives over HTTP
// with a token (records, vocabulary, the provider install, blobs, merges,
// webhooks, function calls, search, the change feed); what the operator does
// runs through the engine the way substratectl and the substrated loops do
// (the trigger dispatcher pass, the embed drain, the GC sweep, `repository
// snapshot`, `repository rewrap`, `repository verify`). docs/operations.md is
// the procedure followed: "Backups" for the snapshot, "Restore without the
// credential key" for the rewrap and the boot that imports, "Restore" for the
// verify afterwards.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
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
	drillAuthority = "drill.example.com"
	drillPassword  = "correct-horse-battery-staple"

	secondAuthority = "second.example.com"
	secondPassword  = "another-correct-horse-battery"

	corePkg        = "substrate.reamde.dev/core"
	llmPkg         = "substrate.reamde.dev/llm"
	googlePkg      = "providers.substrate.reamde.dev/google"
	embedAPIKey    = "sk-drill-embed-key"
	embedModel     = "text-embedding-3-small"
	fileBytes      = "the report the drill attaches: bytes that must read back after the restore"
	mergeWinner    = "m-a"
	mergeLoser     = "m-b"
	subjectHold    = "Alex"
	hookBody       = `{"say":"call the dentist"}`
	holdBody       = `{"say":"hold the line"}`
	pollInterval   = 10 * time.Millisecond
	settleDeadline = 20 * time.Second

	// seedAuthorityDir is the shipped seed authority, relative to this
	// package: the tree kinds.Seed() embeds, which the boot upgrade is patched
	// against.
	seedAuthorityDir = "../../kinds/substrate.reamde.dev"

	// sealedValues is how many sealed files the source repository holds: the
	// login credential's two (password hash, TOTP seed), the embeddings
	// provider's apiKey, the OAuth-shaped kind's three, the google provider
	// input's clientSecret. Each is one secret reference on a live record.
	sealedValues = 7
)

// The drill's kinds: the rehomed samples, the installed provider and the
// packages the drill declares itself.
var (
	taskKind     = drillAuthority + "/tasks/task"
	projectKind  = drillAuthority + "/tasks/project"
	personKind   = drillAuthority + "/people/person"
	widgetKind   = drillAuthority + "/auto/widget"
	gadgetKind   = drillAuthority + "/auto/gadget"
	flagKind     = drillAuthority + "/auto/flag"
	echoKind     = drillAuthority + "/auto/echo"
	subjectKind  = drillAuthority + "/crm/subject"
	contactKind  = drillAuthority + "/dira/contact"
	memberKind   = drillAuthority + "/dirb/member"
	fileKind     = drillAuthority + "/attach/file"
	oauthKind    = drillAuthority + "/creds/oauthclient"
	googleConfig = googlePkg + "/config"
	secondTask   = secondAuthority + "/tasks/task"

	// drillCollections is every collection the drill compares record by
	// record.
	drillCollections = []string{
		taskKind, projectKind, personKind, widgetKind, gadgetKind, flagKind, echoKind,
		subjectKind, contactKind, memberKind, fileKind, oauthKind, googleConfig, googlePkg + "/account",
		llmPkg + "/provider", corePkg + "/trigger", corePkg + "/recordmerge", corePkg + "/recordsplit",
		corePkg + "/blob", corePkg + "/token", corePkg + "/credential", corePkg + "/recoverykey",
		corePkg + "/repository", corePkg + "/triggerrun", corePkg + "/kind", corePkg + "/function",
		corePkg + "/recordmapping", corePkg + "/package", corePkg + "/bundle",
	}
	// drillTriggers are the triggers the drill writes; the provider install
	// brings its own beside them.
	drillTriggers   = []string{"on-mirror", "on-page", "on-hook", "on-hold"}
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
  description: echoes a webhook request once a release flag exists, waiting until then
  runtime: python
  # The body waits for the flag with no give-up of its own: the drill releases
  # it, and this bound is the one clock that ends a fire nothing releases.
  timeout: PT50S
  permissions:
    reads:
      kinds: [drill.example.com/auto/flag]
      budgets:
        calls: 1000
        rows: 1000
    writes: [drill.example.com/auto/echo]
  source: |
    import time

    FLAG = "drill.example.com/auto/flag"
    ECHO = "drill.example.com/auto/echo"

    def main(input, host):
        while host.records.get(FLAG, "release") is None:
            time.sleep(0.25)
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

// crmKind is the mapping target: the package that owns `subject` declares the
// mappings from the two mirror packages onto it (decision 0049). It is
// applied twice: the kind alone, before the mirrors that reference it, then
// with the mappings once the source kinds exist.
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

// credsVocabulary is an OAuth-shaped credential the user declares: a client
// id beside three sealed values, the shape a provider's input record and its
// `accountconfig` account carry. The installed google provider is the
// provider-tier counterpart.
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

func newFakeEmbed(t testing.TB) *fakeEmbed {
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
	return slices.Clone(f.auths), f.texts
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

// folded is the fold snapshot the rebuild tests compare, off the dataset.
type folded interface {
	FoldSnapshot(ctx context.Context) ([]byte, error)
}

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
	// records is every record the collections hold, keyed "<kind>/<id>", as
	// a single-record GET returns it (propertyMeta included), decoded so the
	// comparison is structural.
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
	// blobs is each stored blob's bytes as GET /blobs served them.
	blobs map[string][]byte
	// lexical and semantic are the fixed queries' answers.
	lexical  map[string]searchAnswer
	semantic map[string]searchAnswer
	head     int64
	gen      string
}

// stageTB is a stage's own t with the parent's lifetime: what a stage hands
// to Start, NewSchema, TempDir and the embed server, so a substrate or a
// schema outlives the stage that made it while every failure inside them is
// the stage's. A Fatalf on the parent from a subtest's goroutine is not a
// failure the runner can attribute.
type stageTB struct {
	testing.TB
	parent *testing.T
}

func (s stageTB) Cleanup(f func())          { s.parent.Cleanup(f) }
func (s stageTB) TempDir() string           { return s.parent.TempDir() }
func (d *drill) tb(t *testing.T) testing.TB { return stageTB{TB: t, parent: d.root} }

// drill is the state the stages hand each other.
type drill struct {
	root  *testing.T
	clock *engine.TestClock
	embed *fakeEmbed

	identity, secondIdentity *age.X25519Identity
	keyA, keyB               string
	rootA, dsnA              string
	dbs                      map[string]*sql.DB

	// The source substrate, its second user and the recorded state.
	envA   *testenv.Env
	second *testenv.Env
	source *state

	// What stage 1 hands later stages by name.
	blobDigest     string
	subjectID      string
	splitMergeID   string
	pagedFailureID int64
	pagedChain     string
	hookFailureID  int64
	hookFireID     string
	holdFireID     string
	// holdInvoked is signaled as the runner starts the held fire's body.
	holdInvoked chan struct{}
	runVersion  int64

	// Stage 2: the snapshot root and the recorded points.
	snapRoot   string
	snapHead   int64
	secondHead int64
	secondFold []byte

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
	d := &drill{root: t, clock: &engine.TestClock{}, dbs: map[string]*sql.DB{}, done: map[string]bool{}}
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
		{"06 restore the second repository under both keys and refuse a newer manifest", d.secondRestore, []string{"02", "03"}},
		{"07 a cursor from the replaced history is reset", d.replacedHistoryCursor, []string{"03", "04"}},
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
	e := d.seedSource(t)
	d.seedVocabulary(t, e)
	d.writeRecordsAndMerges(t, e)
	d.writeSecretsAndAttachment(t, e)
	d.writeMirrors(t, e)
	ds := d.parkAutomations(t, e)
	// The drain buys the source's vectors, so the fixed queries are recorded
	// over a full index.
	drainEmbeds(t, ds)
	d.writeSecondRepository(t, e)
	d.holdWebhook(t, e)

	d.source = captureState(t, e, ds, d.scoped(t, d.dsnA, drillAuthority))
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
	if !bytes.Equal(d.source.blobs[d.blobDigest], []byte(fileBytes)) {
		t.Errorf("the attachment did not read back on the source")
	}
}

// seedSource registers the source under a copy of the seed tree where `run`
// is one version behind, then restarts it under the shipped tree, so the
// repository's history holds a shipped kind upgraded at boot.
func (d *drill) seedSource(t *testing.T) *testenv.Env {
	tb := d.tb(t)
	var err error
	if d.identity, err = age.GenerateX25519Identity(); err != nil {
		t.Fatal(err)
	}
	if d.secondIdentity, err = age.GenerateX25519Identity(); err != nil {
		t.Fatal(err)
	}
	d.keyA, d.keyB = testenv.MintCredentialKey(), testenv.MintCredentialKey()
	if d.keyA == d.keyB {
		t.Fatal("the two host keys are one key")
	}
	d.rootA = tb.TempDir()
	d.dsnA = testdb.NewSchema(tb)
	d.embed = newFakeEmbed(tb)

	patched, shippedRun := patchedSeedTree(tb, "triggerrun.yaml")
	d.runVersion = shippedRun
	first := testenv.Start(tb,
		testenv.WithUser(drillAuthority, drillPassword),
		testenv.WithRecoveryPublicKey(d.identity.Recipient().String()),
		testenv.WithDSN(d.dsnA), testenv.WithDataRoot(d.rootA), testenv.WithCredentialKey(d.keyA),
		testenv.WithClock(d.clock.Now), testenv.WithKindsDir(patched))
	if first.Repository != drillAuthority {
		t.Fatalf("registered repository %q, want %q", first.Repository, drillAuthority)
	}
	if got := kindVersions(t, first)[corePkg+"/triggerrun"]; got != shippedRun-1 {
		t.Fatalf("triggerrun declared at %d under the patched tree, want %d", got, shippedRun-1)
	}
	first.Stop()

	d.holdInvoked = make(chan struct{}, 16)
	d.envA = testenv.Start(tb,
		testenv.WithUser(drillAuthority, drillPassword), testenv.WithoutRegistration(),
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
	// The restarted substrate speaks as the user registration created: the
	// token and the seed carry over the restart, as they would for a client.
	d.envA.Session = first.Session
	e := d.envA.For(t)
	if got := kindVersions(t, e)[corePkg+"/triggerrun"]; got != shippedRun {
		t.Errorf("the boot upgrade did not land: run declared at %d, want the shipped %d", got, shippedRun)
	}
	return e
}

// seedVocabulary imports the samples (rehomed onto the repository's
// authority), installs the google provider under its publisher's authority,
// and declares the drill's own packages, editing one kind afterwards.
func (d *drill) seedVocabulary(t *testing.T, e *testenv.Env) {
	for _, id := range sampleImports {
		e.MustJSON(http.MethodPost, "/api/v1/catalog/"+url.PathEscape(id)+"/import", nil, nil)
	}
	e.MustJSON(http.MethodPost, "/api/v1/catalog/"+url.PathEscape(googlePkg)+"/install", nil, nil)
	e.ApplyVocabularyYAML(autoVocabulary)
	e.ApplyVocabularyYAML(crmKind)
	e.ApplyVocabularyYAML(mirrorVocabulary("dira", "contact", "synca"))
	e.ApplyVocabularyYAML(mirrorVocabulary("dirb", "member", "syncb"))
	e.ApplyVocabularyYAML(crmKind + "---" + crmMappings)
	e.ApplyVocabularyYAML(attachVocabulary(false))
	e.ApplyVocabularyYAML(attachVocabulary(true))
	e.ApplyVocabularyYAML(credsVocabulary)
	versions := kindVersions(t, e)
	if got := versions[fileKind]; got != 2 {
		t.Errorf("file declared at version %d after its edit, want 2", got)
	}
	if _, ok := versions[googleConfig]; !ok {
		t.Errorf("the google provider install left no %s declaration", googleConfig)
	}
}

// writeRecordsAndMerges writes the tasks: a reference, labels, a transition,
// the last label cleared to the empty map (#362), a delete and the put that
// restores it, a merge that stands and one that is split.
func (d *drill) writeRecordsAndMerges(t *testing.T, e *testenv.Env) {
	putRecord(t, e, projectKind, "release", map[string]any{"properties": map[string]any{"name": "The release"}})
	due := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	putRecord(t, e, taskKind, "fold", map[string]any{
		"properties": map[string]any{
			"name": "Ship the fold", "description": "carry the values, not just the names",
			"dueAt": due, "url": "https://example.com/1", "project": "release",
		},
		"labels": map[string]any{"owner/pinned": true},
	})
	putRecord(t, e, taskKind, "rebuild", map[string]any{"properties": map[string]any{
		"name": "Rebuild the repository", "description": "replay every entry of the ledger", "project": "release",
	}})
	putRecord(t, e, taskKind, "collect", map[string]any{"properties": map[string]any{"name": "Collect me", "description": "and then go"}})
	patchRecord(t, e, taskKind, "fold", map[string]any{
		"properties":  map[string]any{"description": "values, replayable", "url": nil},
		"labels":      map[string]any{"owner/pinned": nil, "owner/urgent": "yes"},
		"annotations": map[string]any{"owner/note": map[string]any{"why": "the payload"}},
	})
	patchRecord(t, e, taskKind, "fold", map[string]any{"properties": map[string]any{"status": "done"}})
	cleared := patchRecord(t, e, taskKind, "fold", map[string]any{"labels": map[string]any{"owner/urgent": nil}})
	if labels := labelsOf(t, cleared, "fold"); len(labels) != 0 {
		t.Fatalf("the last label did not clear: %v", labels)
	}
	// The tombstone a later put revives has to fold back as live with the
	// new properties and labels, not as the record it was.
	deleteRecord(t, e, taskKind, "collect")
	putRecord(t, e, taskKind, "collect", map[string]any{
		"properties": map[string]any{"name": "Collect me", "description": "restored"},
		"labels":     map[string]any{"owner/kept": true},
	})

	putRecord(t, e, taskKind, mergeWinner, map[string]any{"properties": map[string]any{"name": "Winner", "description": "the record that stays"}})
	putRecord(t, e, taskKind, mergeLoser, map[string]any{"properties": map[string]any{"name": "Loser", "description": "the record that folds in"}})
	e.MustJSON(http.MethodPost, "/api/v1/merge", substrate.MergeInput{Kind: taskKind, Winner: mergeWinner, Loser: mergeLoser}, nil)
	putRecord(t, e, taskKind, "s-a", map[string]any{"properties": map[string]any{"name": "Split winner"}})
	putRecord(t, e, taskKind, "s-b", map[string]any{"properties": map[string]any{"name": "Split loser"}})
	var mergeRecord map[string]any
	e.MustJSON(http.MethodPost, "/api/v1/merge", substrate.MergeInput{Kind: taskKind, Winner: "s-a", Loser: "s-b"}, &mergeRecord)
	d.splitMergeID, _ = mergeRecord["id"].(string)
	if mergeRecord["kind"] != corePkg+"/recordmerge" || d.splitMergeID == "" {
		t.Fatalf("merge answered with %v, want the recordmerge record", mergeRecord)
	}
	e.MustJSON(http.MethodPost, "/api/v1/split", substrate.SplitInput{Merge: d.splitMergeID}, nil)
}

// writeSecretsAndAttachment stores the attachment's bytes and the record
// naming them, the embeddings provider whose apiKey is sealed, the
// user-declared OAuth-shaped credential, and the installed provider's input
// record with its sealed client secret.
func (d *drill) writeSecretsAndAttachment(t *testing.T, e *testenv.Env) {
	status, raw, _ := e.DoRaw(http.MethodPut, "/api/v1/blobs?name=report.txt", []byte(fileBytes),
		map[string]string{"Content-Type": "text/plain"})
	if status != http.StatusCreated {
		t.Fatalf("put blob: %d %s", status, raw)
	}
	var blob substrate.BlobInfo
	if err := json.Unmarshal(raw, &blob); err != nil || blob.Digest == "" {
		t.Fatalf("blob upload answered %s: %v", raw, err)
	}
	d.blobDigest = blob.Digest
	putRecord(t, e, fileKind, "report", map[string]any{"properties": map[string]any{
		"name": "report.txt", "data": blob.Digest, "notes": "attached before the restore",
	}})
	putRecord(t, e, llmPkg+"/provider", "vectors", map[string]any{"properties": map[string]any{
		"label": "vectors", "wire": "openai", "baseURL": d.embed.srv.URL,
		"apiKey": embedAPIKey, "embedModel": embedModel,
	}})
	putRecord(t, e, oauthKind, "github-app", map[string]any{"properties": map[string]any{
		"clientId": "Iv1.drill", "clientSecret": "gho_client_secret_value",
		"accessToken": "gho_access_token_value", "refreshToken": "ghr_refresh_token_value",
		"expiresAt": time.Now().UTC().Add(time.Hour).Truncate(time.Second).Format(time.RFC3339),
	}})
	putRecord(t, e, googleConfig, "default", map[string]any{"properties": map[string]any{
		"clientId": "drill-google-client", "clientSecret": "google-client-secret-value",
	}})
}

// writeMirrors writes conflicting provider values: two mirror packages sync
// one subject with two names, then the owner's hand takes the property, so
// both offers stand as alternatives beside a held value.
func (d *drill) writeMirrors(t *testing.T, e *testenv.Env) {
	e.MustCallFunction("synca", map[string]any{"id": "a-1", "name": "Alexandra Papas", "email": "alex@example.com"})
	e.MustCallFunction("syncb", map[string]any{"id": "b-1", "name": "Alex P", "email": "alex@example.com"})
	contact := getRecord(t, e, contactKind, "a-1")
	d.subjectID = refID(propOf(t, contact, "a-1", "subject"))
	if d.subjectID == "" {
		t.Fatalf("the contact names no subject: %v", contact["properties"])
	}
	if got := propOf(t, getRecord(t, e, subjectKind, d.subjectID), d.subjectID, "name"); got != "Alex P" {
		t.Errorf("subject name after two syncs = %v, want the later source's \"Alex P\"", got)
	}
	patchRecord(t, e, subjectKind, d.subjectID, map[string]any{"properties": map[string]any{"name": subjectHold, "note": "held by hand"}})
	meta := metaOf(t, getRecord(t, e, subjectKind, d.subjectID), d.subjectID, "name")
	if alts, _ := meta["alternatives"].([]any); meta["manager"] != "api" || len(alts) != 2 {
		t.Errorf("subject name provenance = %v, want manager api with two alternatives", meta)
	}
}

// parkAutomations writes the four triggers and drives them to the states the
// release names: a settled delivery, a pending one, a paged drain parked at
// cursor 2, a webhook parked with its request (#437), plus the purge and the
// tombstone the sweep has not reached. The dispatcher pass is the engine's,
// as substrated's loop runs it.
func (d *drill) parkAutomations(t *testing.T, e *testenv.Env) substrate.Dataset {
	ctx := context.Background()
	ds, err := e.Service.Dataset(ctx, drillAuthority)
	if err != nil {
		t.Fatalf("open the source dataset: %v", err)
	}
	dispatcher := ds
	callable := func(name string) string { return corePkg + "/function/" + drillAuthority + "/auto/" + name }
	putRecord(t, e, corePkg+"/trigger", "on-mirror", map[string]any{"properties": map[string]any{
		"enabled": true, "source": map[string]any{"record": map[string]any{"kinds": []any{widgetKind}}}, "callable": callable("mirror"),
	}})
	putRecord(t, e, corePkg+"/trigger", "on-page", map[string]any{"properties": map[string]any{
		"enabled": true, "source": map[string]any{"record": map[string]any{"kinds": []any{gadgetKind}}}, "callable": callable("page"),
	}})
	putRecord(t, e, corePkg+"/trigger", "on-hook", map[string]any{"properties": map[string]any{
		"enabled": true, "source": map[string]any{"webhook": map[string]any{}}, "callable": callable("hook"),
	}})
	putRecord(t, e, corePkg+"/trigger", "on-hold", map[string]any{"properties": map[string]any{
		"enabled": true, "source": map[string]any{"webhook": map[string]any{}}, "callable": callable("hold"),
	}})
	putRecord(t, e, widgetKind, "w1", map[string]any{"properties": map[string]any{"name": "one"}})
	putRecord(t, e, gadgetKind, "g1", map[string]any{"properties": map[string]any{"name": "big"}})
	if _, err := dispatcher.ProcessTriggers(ctx); err != nil {
		t.Fatalf("dispatcher pass: %v", err)
	}
	if got := propOf(t, getRecord(t, e, taskKind, "t-w1"), "t-w1", "name"); got != "mirror of one" {
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
	cursors := pagedCursors(t, d.scoped(t, d.dsnA, drillAuthority))
	if len(cursors) != 1 {
		t.Fatalf("paged_cursors = %v, want the one chain", cursors)
	}
	for chain, cur := range cursors {
		d.pagedChain = chain
		if cur != 2 {
			t.Fatalf("paged cursor = %v, want 2 after pages 0 and 1", cur)
		}
	}
	// Written after the pass: undelivered at the stop.
	putRecord(t, e, widgetKind, "w2", map[string]any{"properties": map[string]any{"name": "two"}})

	putRecord(t, e, taskKind, "purge-me", map[string]any{"properties": map[string]any{"name": "Purge me"}})
	deleteRecord(t, e, taskKind, "purge-me")
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if status, _ := e.Do(http.MethodGet, recordPath(taskKind, "purge-me"), nil); status != http.StatusNotFound {
		t.Fatalf("the purged record still reads: %d", status)
	}
	if tombs := listRecords(t, e, taskKind, true); containsID(tombs, "purge-me") {
		t.Fatalf("the purged record is still a tombstone: %v", tombs)
	}
	// Deleted after the sweep, so it is a tombstone at the stop.
	putRecord(t, e, taskKind, "stays-deleted", map[string]any{"properties": map[string]any{"name": "Stays deleted", "description": "a tombstone the sweep has not reached"}})
	deleteRecord(t, e, taskKind, "stays-deleted")

	d.hookFireID = postWebhook(t, e, "on-hook", hookBody, "parked")
	waitFor(t, "the webhook fire to park", func() bool {
		parked = parkedOf(t, e, "on-hook")
		return len(parked) == 1 && !strings.Contains(parked[0].LastError, "has not settled")
	})
	d.hookFailureID = parked[0].ID
	if parked[0].FireID != d.hookFireID || !strings.Contains(parked[0].LastError, "hook gate closed") {
		t.Fatalf("the parked webhook = %+v, want fire %s parked at its gate", parked[0], d.hookFireID)
	}
	return ds
}

// writeSecondRepository registers a second user on the same substrate and
// writes tasks, a cleared label, a sealed value and a blob. Stage 6 restores
// its snapshot on two hosts.
func (d *drill) writeSecondRepository(t *testing.T, e *testenv.Env) {
	ctx := context.Background()
	d.second = e.RegisterUser(secondAuthority, secondPassword, d.secondIdentity.Recipient().String())
	l := d.second.For(t)
	for _, id := range sampleImports {
		l.MustJSON(http.MethodPost, "/api/v1/catalog/"+url.PathEscape(id)+"/import", nil, nil)
	}
	for i, name := range []string{"first", "second", "third"} {
		putRecord(t, l, secondTask, fmt.Sprintf("l-%d", i), map[string]any{
			"properties": map[string]any{"name": name, "description": "second " + name},
			"labels":     map[string]any{"owner/second": true},
		})
	}
	patchRecord(t, l, secondTask, "l-0", map[string]any{"labels": map[string]any{"owner/second": nil}})
	putRecord(t, l, llmPkg+"/provider", "second-llm", map[string]any{"properties": map[string]any{
		"label": "second", "wire": "openai", "baseURL": "https://llm.example.com/v1", "apiKey": "sk-second-value",
	}})
	if status, raw, _ := l.DoRaw(http.MethodPut, "/api/v1/blobs?name=second.txt", []byte("second bytes"),
		map[string]string{"Content-Type": "text/plain"}); status != http.StatusCreated {
		t.Fatalf("the second repository's blob: %d %s", status, raw)
	}
	lds, err := e.Service.Dataset(ctx, secondAuthority)
	if err != nil {
		t.Fatalf("open the second dataset: %v", err)
	}
	if d.secondFold, err = lds.(folded).FoldSnapshot(ctx); err != nil {
		t.Fatalf("the second repository's fold: %v", err)
	}
}

// holdWebhook posts the webhook whose fire waits for a `release` flag, and
// waits for the runner to start its body, so the stop in stage 2 cancels a
// fire that is running, not one still queued. It is the last write of the
// source; the entry stays pending for the restored dispatcher.
func (d *drill) holdWebhook(t *testing.T, e *testenv.Env) {
	d.holdFireID = postWebhook(t, e, "on-hold", holdBody, "held")
	select {
	case <-d.holdInvoked:
	case <-time.After(settleDeadline):
		t.Fatal("the held fire's body did not start within 20s of the 202")
	}
	if status, _ := e.Do(http.MethodGet, recordPath(echoKind, "hold-echo"), nil); status != http.StatusNotFound {
		t.Fatalf("the held fire settled before the stop: hold-echo reads %d", status)
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
	d.snapRoot = d.tb(t).TempDir()
	report, err := operator.(engine.Snapshotter).SnapshotRepository(ctx, drillAuthority, d.snapRoot)
	if err != nil {
		t.Fatalf("snapshot %s: %v", drillAuthority, err)
	}
	d.snapHead = report.Head
	if report.Head != d.source.head || report.Repository != drillAuthority || report.BlobStore != "fs" || report.Blobs < 1 || report.SealedFiles != sealedValues {
		t.Errorf("snapshot report = %+v, want head %d for %s under fs with the attachment and %d sealed files", report, d.source.head, drillAuthority, sealedValues)
	}
	snap, err := changelogfile.ReadSnapshot(report.Directory)
	if err != nil {
		t.Fatalf("the copy carries no readable snapshot.json: %v", err)
	}
	if snap.Head != d.source.head || hex.EncodeToString(snap.HeadHash[:]) != report.HeadHash || snap.BlobStore != "fs" {
		t.Errorf("recorded point = seq %d %x laid out for %q, want seq %d %s under fs in the directory", snap.Head, snap.HeadHash, snap.BlobStore, d.source.head, report.HeadHash)
	}
	if !slices.Contains(snap.Blobs, d.blobDigest) {
		t.Errorf("snapshot.json lists %v, which lacks the attachment %s", snap.Blobs, d.blobDigest)
	}
	if _, err := os.Stat(filepath.Join(changelogfile.BlobsDir(report.Directory), d.blobDigest)); err != nil {
		t.Errorf("the copy holds no bytes for %s: %v", d.blobDigest, err)
	}

	secondReport, err := operator.(engine.Snapshotter).SnapshotRepository(ctx, secondAuthority, d.snapRoot)
	if err != nil {
		t.Fatalf("snapshot %s: %v", secondAuthority, err)
	}
	d.secondHead = secondReport.Head
	if secondReport.Repository != secondAuthority {
		t.Errorf("the second repository's snapshot report = %+v", secondReport)
	}
	t.Logf("snapshot: %s at seq %d (%d segments, %d sealed files, %d blobs); %s at seq %d",
		drillAuthority, report.Head, report.Segments, report.SealedFiles, report.Blobs, secondAuthority, secondReport.Head)
}

// --- stage 3 --------------------------------------------------------------------

// restore follows docs/operations.md "Restore without the credential key":
// the copy is refused on a host whose key did not write it, `repository
// rewrap` opens the DEK with the recovery key and rewrites the manifest under
// the new host's key, the boot into an empty database imports the directory,
// and `repository verify` opens every sealed file and hashes every blob.
func (d *drill) restore(t *testing.T) {
	ctx := context.Background()
	tb := d.tb(t)
	dsnB := testdb.NewSchema(tb)
	restoreRoot := copyRoot(tb, d.snapRoot, drillAuthority)

	// Without the rewrap the copy is inert on this host, and the refusal
	// leaves no row behind on the very schema the restore then boots.
	if svc, err := engine.Open(ctx, dsnB, engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(restoreRoot), engine.WithCredentialKey(d.keyB)); err == nil {
		_ = svc.Close()
		t.Fatal("the directory imported under a key it was not written under")
	} else if !strings.Contains(err.Error(), "SUBSTRATE_CREDENTIAL_KEY") {
		t.Errorf("the refusal does not name the variable: %v", err)
	}
	if n := d.countRows(t, dsnB, "repositories"); n != 0 {
		t.Errorf("the refused import left %d repositories row(s)", n)
	}

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
	if report.Repository != drillAuthority || report.SealedFiles != sealedValues {
		t.Errorf("rewrap report = %+v, want %s/%s with %d sealed files opened", report, drillAuthority, drillAuthority, sealedValues)
	}
	d.pristine = copyRoot(tb, restoreRoot, drillAuthority)

	// The boot that imports: the same empty schema, migrated from nothing, no
	// registration. The restored substrate speaks with the source's token,
	// which is a record the import brought back.
	d.envB = testenv.Start(tb,
		testenv.WithUser(drillAuthority, drillPassword), testenv.WithoutRegistration(),
		testenv.WithDSN(dsnB), testenv.WithDataRoot(restoreRoot), testenv.WithCredentialKey(d.keyB),
		testenv.WithClock(d.clock.Now))
	session := *d.envA.Session
	d.envB.Session = &session
	e := d.envB.For(t)
	repos, err := e.Service.Repositories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].ID != drillAuthority {
		t.Fatalf("the restored database holds %+v, want the one imported repository", repos)
	}
	verified, err := e.Service.(engine.Verifier).VerifyRepository(ctx, drillAuthority)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !verified.OK || verified.Head != d.snapHead || verified.FileHead != d.snapHead || len(verified.Findings) != 0 {
		t.Errorf("the restored repository does not verify: %+v", verified)
	}
	if verified.Snapshot == nil || verified.Snapshot.Head != d.snapHead || verified.Snapshot.HeadHash != verified.HeadHash {
		t.Errorf("verify did not report the recorded point: %+v (head %s)", verified.Snapshot, verified.HeadHash)
	}
	if verified.SealedFiles != sealedValues || verified.SealedOpened != sealedValues || verified.SecretRefs != sealedValues {
		t.Errorf("verify: %d sealed files, %d opened under the new key, %d secret refs; want %d of each",
			verified.SealedFiles, verified.SealedOpened, verified.SecretRefs, sealedValues)
	}
	if verified.Blobs < 1 {
		t.Errorf("verify hashed %d blobs, want the attachment", verified.Blobs)
	}
	t.Logf("restore: imported %s at seq %d into an empty schema under a new key; verify opened %d sealed files and hashed %d blobs",
		drillAuthority, verified.Head, verified.SealedOpened, verified.Blobs)
}

// --- stage 4 --------------------------------------------------------------------

func (d *drill) compareAndResume(t *testing.T) {
	e := d.envB.For(t)
	ds, err := e.Service.Dataset(context.Background(), drillAuthority)
	if err != nil {
		t.Fatalf("open the restored dataset: %v", err)
	}
	d.compareRestored(t, e, ds)
	d.resumeDispatch(t, e, ds)
	d.retryParked(t, e)
}

// compareRestored holds the restored substrate to the recorded state before
// anything resumes: records, provenance, the delivery ledger, auth, the
// attachment, search before and after the embedding drain.
func (d *drill) compareRestored(t *testing.T, e *testenv.Env, ds substrate.Dataset) {
	// Before the drain: semantic search refuses with the backlog, which is
	// not "no matches"; hybrid answers its lexical arm and reports it. The
	// vectors and the queue are derived tables the directory does not carry,
	// so this behavior, not a row count, is what proves the import queued
	// them.
	before := searchRecords(t, e, "carry the values", "semantic", []string{taskKind})
	if before.ErrCode != "unavailable" || !strings.Contains(before.ErrMsg, "pending") {
		t.Errorf("semantic search before the drain = %+v, want the unavailable code naming the pending count", before)
	}
	if hybrid := searchRecords(t, e, "ledger", "hybrid", []string{taskKind}); hybrid.ErrCode != "" || hybrid.Pending == 0 || len(hybrid.IDs) == 0 {
		t.Errorf("hybrid search before the drain = %+v, want lexical hits beside a non-zero backlog", hybrid)
	}

	d.embed.reset()
	d.restored = captureState(t, e, ds, d.scoped(t, e.DSN, drillAuthority))
	// The comparison is only as strong as what the source recorded, so the
	// record set is held to a floor before it is compared.
	if n := len(d.source.records); n < 60 {
		t.Errorf("the source recorded %d records, too few for the states the drill writes", n)
	}
	if n := len(d.source.blobs); n != 3 {
		t.Errorf("the source recorded %d blobs, want the attachment and the two spooled webhook bodies", n)
	}
	for _, id := range drillTriggers {
		if _, ok := d.source.statuses[id]; !ok {
			t.Errorf("the source recorded no status for trigger %s", id)
		}
	}
	if tombs, _ := d.source.deleted[taskKind].([]any); !containsID(tombs, "stays-deleted") {
		t.Errorf("the source's task tombstones lack stays-deleted: %v", tombs)
	}
	for _, finding := range diffStates(d.source, d.restored) {
		t.Errorf("restored: %s", finding)
	}
	t.Logf("compared %d records, %d collections' tombstones, %d kind versions, %d triggers, %d blobs, %d lexical and %d semantic queries; head %d",
		len(d.source.records), len(d.source.deleted), len(d.source.kindVersions), len(d.source.statuses),
		len(d.source.blobs), len(d.source.lexical), len(d.source.semantic), d.source.head)

	// The comparator's negative proof: a copy of the restored state with one
	// property flipped and the head moved must be reported, by path.
	t.Run("comparator reports a tampered copy", func(t *testing.T) {
		tampered := *d.restored
		tampered.records = maps.Clone(d.restored.records)
		key := taskKind + "/fold"
		var copyOf map[string]any
		raw, _ := json.Marshal(d.restored.records[key])
		if err := json.Unmarshal(raw, &copyOf); err != nil {
			t.Fatal(err)
		}
		copyOf["properties"].(map[string]any)["name"] = "tampered"
		tampered.records[key] = copyOf
		tampered.head++
		findings := diffStates(d.source, &tampered)
		joined := strings.Join(findings, "\n")
		if !strings.Contains(joined, key) || !strings.Contains(joined, "$.properties.name") || !strings.Contains(joined, "head") {
			t.Errorf("the comparator missed the tamper; it reported:\n%s", joined)
		}
	})

	// The delivery ledger before dispatch resumes: the parked webhook under
	// its retry id, the paged drain at its cursor, the held fire pending and
	// unsettled, the pending mirror undelivered.
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
	for _, id := range []string{"hold-echo", "hook-echo"} {
		if status, _ := e.Do(http.MethodGet, recordPath(echoKind, id), nil); status != http.StatusNotFound {
			t.Errorf("%s exists before dispatch resumed: %d", id, status)
		}
	}
	if status, _ := e.Do(http.MethodGet, recordPath(taskKind, "t-w2"), nil); status != http.StatusNotFound {
		t.Errorf("the pending delivery landed before dispatch resumed: t-w2 reads %d", status)
	}

	// The states the release names, read back by name.
	fold := getRecord(t, e, taskKind, "fold")
	if labels := labelsOf(t, fold, "fold"); len(labels) != 0 {
		t.Errorf("the cleared label came back: labels = %v (#362)", labels)
	}
	if winner := getRecord(t, e, taskKind, mergeWinner); !reflect.DeepEqual(winner["formerIds"], []any{mergeLoser}) {
		t.Errorf("the merge winner's former ids = %v, want [%s]", winner["formerIds"], mergeLoser)
	}
	if loser := getRecord(t, e, taskKind, mergeLoser); loser["canonicalId"] != mergeWinner {
		t.Errorf("the merged-away id resolves to %v, want %s", loser["canonicalId"], mergeWinner)
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
	meta := metaOf(t, getRecord(t, e, subjectKind, d.subjectID), d.subjectID, "name")
	if alts, _ := meta["alternatives"].([]any); meta["manager"] != "api" || len(alts) != 2 {
		t.Errorf("subject name provenance after the restore = %v, want the owner's hold over two offers", meta)
	}
	file := getRecord(t, e, fileKind, "report")
	if data, _ := propOf(t, file, "report", "data").(map[string]any); data["digest"] != d.blobDigest || data["status"] != "stored" {
		t.Errorf("the attachment reference resolves to %v, want the stored manifest %s", file["properties"], d.blobDigest)
	}
	if got := d.restored.blobs[d.blobDigest]; !bytes.Equal(got, []byte(fileBytes)) {
		t.Errorf("attachment bytes after the restore = %q", got)
	}
	sum := sha256.Sum256([]byte(fileBytes))
	if want := "blob-sha256-" + hex.EncodeToString(sum[:]); d.blobDigest != want {
		t.Errorf("attachment digest %s is not the bytes' %s", d.blobDigest, want)
	}

	// Authenticate with the original password and TOTP. The wrong password
	// goes first, so its refusal is the password check's and not the login
	// limiter's; the clock then moves one TOTP step, past the limiter's
	// allowance too (it reads the same clock), so the registration's code is
	// not replayed and the right password is admitted. The source's token
	// authenticated every read above.
	d.clock.Advance(engine.TOTPPeriod)
	status, raw := e.Login(drillAuthority, "not-the-password", e.TOTPCode())
	var refusal substrate.ErrorEnvelope
	if err := json.Unmarshal(raw, &refusal); status != http.StatusUnauthorized || err != nil || refusal.Error.Code != "auth" {
		t.Errorf("a wrong password on the restored host: %d %s, want 401 auth", status, raw)
	}
	d.clock.Advance(engine.TOTPPeriod)
	if status, raw := e.Login(drillAuthority, drillPassword, e.TOTPCode()); status != http.StatusCreated {
		t.Errorf("login on the restored host: %d %s", status, raw)
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
		if got := searchRecords(t, e, q, "semantic", []string{taskKind}); !reflect.DeepEqual(got, want) {
			t.Errorf("semantic %q after the drain = %+v, want the source's %+v", q, got, want)
		}
	}
}

// resumeDispatch releases the held fire and runs one dispatcher pass: the
// fire completes under its fire id, the pending mirror delivers once, the
// settled one is not delivered again. The resumed fire runs detached from
// the pass, so its effects are awaited.
func (d *drill) resumeDispatch(t *testing.T, e *testenv.Env, ds substrate.Dataset) {
	putRecord(t, e, flagKind, "release", map[string]any{"properties": map[string]any{"name": "release"}})
	if _, err := ds.ProcessTriggers(context.Background()); err != nil {
		t.Fatalf("dispatcher pass after the restore: %v", err)
	}
	var echo map[string]any
	waitFor(t, "the held fire to settle", func() bool {
		status, raw := e.Do(http.MethodGet, recordPath(echoKind, "hold-echo"), nil)
		if status != http.StatusOK {
			return false
		}
		echo = decodeAny(t, raw).(map[string]any)
		return statusOf(t, e, "on-hold").Pending == 0
	})
	if propOf(t, echo, "hold-echo", "fire") != d.holdFireID || propOf(t, echo, "hold-echo", "name") != holdBody {
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
}

// retryParked retries the two parked deliveries by hand under their restored
// ids once their gates open: the webhook echoes its recorded request, and
// the paged drain continues from page 2 with pages 0 and 1 erased first, so
// a restart from zero would have revived them.
func (d *drill) retryParked(t *testing.T, e *testenv.Env) {
	putRecord(t, e, flagKind, "hook-gate", map[string]any{"properties": map[string]any{"name": "hook-gate"}})
	e.MustJSON(http.MethodPost, fmt.Sprintf("/api/v1/%s/trigger/on-hook/parked/%d/retry", corePkg, d.hookFailureID), nil, nil)
	echo := getRecord(t, e, echoKind, "hook-echo")
	if propOf(t, echo, "hook-echo", "fire") != d.hookFireID || propOf(t, echo, "hook-echo", "want") != "parked" || propOf(t, echo, "hook-echo", "name") != hookBody {
		t.Errorf("the retried webhook's echo = %v, want fire %s with its header and body", echo["properties"], d.hookFireID)
	}
	if got := parkedOf(t, e, "on-hook"); len(got) != 0 {
		t.Errorf("on-hook still parks %+v after the retry", got)
	}

	for _, id := range []string{"p-0", "p-1"} {
		deleteRecord(t, e, taskKind, id)
	}
	putRecord(t, e, flagKind, "page-gate", map[string]any{"properties": map[string]any{"name": "page-gate"}})
	e.MustJSON(http.MethodPost, fmt.Sprintf("/api/v1/%s/trigger/on-page/parked/%d/retry", corePkg, d.pagedFailureID), nil, nil)
	for _, id := range []string{"p-2", "p-3", "p-4"} {
		if rec := getRecord(t, e, taskKind, id); rec["deletedAt"] != nil {
			t.Errorf("resumed page %s is a tombstone", id)
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
	if cursors := pagedCursors(t, d.scoped(t, e.DSN, drillAuthority)); len(cursors) != 0 {
		t.Errorf("paged_cursors after the drain finished = %v, want none", cursors)
	}
}

// --- stage 5 --------------------------------------------------------------------

// interruptedImport boots the rewrapped copy into a fresh schema and kills
// the boot at the import's durable steps (the progress-marker hook, #365):
// after the first batch, after a later batch of the resumed boot, then after
// the first fold pass; the fourth boot finishes. Each kill leaves exactly the
// rows its batches committed, the unfinished import is refused to a reader
// and reported by verify, and the boot that finishes reproduces the fold.
// internal/engine/repodir_db_test.go TestAnInterruptedImportResumesAtTheNextBoot
// owns the crash schedule; this stage runs it over the drill's history.
func (d *drill) interruptedImport(t *testing.T) {
	ctx := context.Background()
	// Five batches or so, so three kills land on three distinct boundaries.
	batch := int((d.snapHead + 4) / 5)
	batches := int((d.snapHead + int64(batch) - 1) / int64(batch))
	if batches < 4 {
		t.Fatalf("head %d spans %d batches of %d; the schedule needs at least 4", d.snapHead, batches, batch)
	}
	errKilled := errors.New("the process died here")
	// Each kill names the stage, its occurrence within that boot, and the
	// changelog rows it must leave: the resumed boot's nth batch lands after
	// the first boot's one.
	crashes := []struct {
		stage string
		nth   int
		rows  int64
	}{
		{engine.ImportAfterBatch, 1, int64(batch)},
		{engine.ImportAfterBatch, batches - 2, int64((batches - 1) * batch)},
		{engine.ImportAfterFirstFold, 1, d.snapHead},
	}
	if !(crashes[0].rows < crashes[1].rows && crashes[1].rows < crashes[2].rows) {
		t.Fatalf("the kills do not land on three distinct boundaries: %d, %d, %d rows", crashes[0].rows, crashes[1].rows, crashes[2].rows)
	}
	root := copyRoot(t, d.pristine, drillAuthority)
	dsn := testdb.NewSchema(t)
	open := func(opts ...engine.Option) (substrate.Service, error) {
		return engine.Open(ctx, dsn, append([]engine.Option{
			engine.WithKindsFS(kinds.Seed()), engine.WithDataRoot(root),
			engine.WithCredentialKey(d.keyB), engine.WithTestTOTPClock(d.clock.Now),
		}, opts...)...)
	}
	for i, c := range crashes {
		seen := 0
		svc, err := open(engine.WithTestImportFault(batch, func(stage string) error {
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
		if n := d.countRows(t, dsn, "changelog"); int64(n) != c.rows {
			t.Errorf("boot %d died %s and left %d changelog rows, want %d", i+1, c.stage, n, c.rows)
		}
		if i > 0 {
			continue
		}
		// What the crash left is not served: a read-only process refuses the
		// repository until the boot finishes the import, and verify names
		// the unfinished import while the files and the rows still agree.
		ro, err := open(engine.WithDirectoryReadOnly())
		if err != nil {
			t.Fatalf("read-only open after boot %d: %v", i+1, err)
		}
		if _, err := ro.Dataset(ctx, drillAuthority); !errors.Is(err, engine.ErrImportIncomplete) {
			t.Errorf("a read-only open after boot %d = %v, want ErrImportIncomplete", i+1, err)
		}
		report, err := ro.(engine.Verifier).VerifyRepository(ctx, drillAuthority)
		if err != nil {
			t.Fatalf("verify after boot %d: %v", i+1, err)
		}
		if report.OK || !slices.ContainsFunc(report.Findings, func(f string) bool {
			return strings.Contains(f, "import of the repository directory has not completed")
		}) {
			t.Errorf("verify after boot %d does not name the unfinished import: %+v", i+1, report)
		}
		_ = ro.Close()
	}

	svc, err := open()
	if err != nil {
		t.Fatalf("the boot after the crashes: %v", err)
	}
	defer func() { _ = svc.Close() }()
	ds, err := svc.Dataset(ctx, drillAuthority)
	if err != nil {
		t.Fatalf("open the repository after the import resumed: %v", err)
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
	report, err := svc.(engine.Verifier).VerifyRepository(ctx, drillAuthority)
	if err != nil || !report.OK || report.Head != d.snapHead || report.SealedOpened != sealedValues || len(report.Findings) != 0 {
		t.Errorf("the resumed repository does not verify: %+v %v", report, err)
	}
	d.clock.Advance(engine.TOTPPeriod)
	if _, _, err := svc.Login(ctx, substrate.LoginInput{Repository: drillAuthority, Password: drillPassword, TOTPCode: d.envA.For(t).TOTPCode(), Label: "resumed"}); err != nil {
		t.Errorf("login after the resumed import: %v", err)
	}
	t.Logf("interrupted import: %d batches of %d, killed at %d, %d and %d rows; the fourth boot finished it",
		batches, batch, crashes[0].rows, crashes[1].rows, crashes[2].rows)
}

// --- stage 6 --------------------------------------------------------------------

// secondRestore takes the second repository's snapshot back twice: once on a
// host that still has the key it was written under, once through the rewrap
// onto another host's key. Then a manifest above the binary's changelog
// dialect is refused by name before any row lands.
func (d *drill) secondRestore(t *testing.T) {
	ctx := context.Background()

	// The same-key restore: the boot imports the directory as it stands.
	root := copyRoot(t, d.snapRoot, secondAuthority)
	dir, err := changelogfile.RepoDir(root, secondAuthority)
	if err != nil {
		t.Fatal(err)
	}
	dsn := testdb.NewSchema(t)
	e := testenv.Start(t,
		testenv.WithUser(secondAuthority, secondPassword), testenv.WithoutRegistration(),
		testenv.WithDSN(dsn), testenv.WithDataRoot(root), testenv.WithCredentialKey(d.keyA),
		testenv.WithClock(d.clock.Now))
	session := *d.second.Session
	e.Session = &session
	ds, err := e.Service.Dataset(ctx, secondAuthority)
	if err != nil {
		t.Fatalf("open the repository the boot imported from format 1: %v", err)
	}
	if imported, err := changelogfile.ReadManifest(dir); err != nil || imported.Format != changelogfile.ManifestFormat ||
		imported.ChangelogDialect != engine.MaxChangelogDialect() {
		t.Errorf("manifest after the import = %+v (%v), want format %d at changelog dialect %d",
			imported, err, changelogfile.ManifestFormat, engine.MaxChangelogDialect())
	}
	fold, err := ds.(folded).FoldSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fold, d.secondFold) {
		t.Errorf("the restored fold is not the source's\n%s", firstDifference(d.secondFold, fold))
	}
	head, err := ds.Head(ctx)
	if err != nil || head.Seq != d.secondHead {
		t.Errorf("head after the import = %d (%v), want %d", head.Seq, err, d.secondHead)
	}
	scoped := d.scoped(t, dsn, secondAuthority)
	var stamped int
	if err := scoped.QueryRowContext(ctx, `SELECT dialect FROM changelog_dialect`).Scan(&stamped); err != nil || stamped != engine.MaxChangelogDialect() {
		t.Errorf("changelog dialect after the import = %d (%v), want the manifest's %d", stamped, err, engine.MaxChangelogDialect())
	}
	d.clock.Advance(engine.TOTPPeriod)
	if status, raw := e.Login(secondAuthority, secondPassword, e.TOTPCode()); status != http.StatusCreated {
		t.Errorf("login on the restore: %d %s", status, raw)
	}
	if labels := labelsOf(t, getRecord(t, e, secondTask, "l-0"), "l-0"); len(labels) != 0 {
		t.Errorf("the cleared label came back through the restore: %v", labels)
	}
	if labels := labelsOf(t, getRecord(t, e, secondTask, "l-1"), "l-1"); labels["owner/second"] != true {
		t.Errorf("l-1 lost its label: %v", labels)
	}
	// The restored repository takes writes.
	putRecord(t, e, secondTask, "l-after", map[string]any{"properties": map[string]any{"name": "after"}})
	if err := scoped.QueryRowContext(ctx, `SELECT dialect FROM changelog_dialect`).Scan(&stamped); err != nil || stamped != engine.MaxChangelogDialect() {
		t.Errorf("changelog dialect after the first write = %d (%v), want %d", stamped, err, engine.MaxChangelogDialect())
	}

	// The other-key restore: `repository rewrap` reads the manifest, opens the
	// DEK with the recovery key and rewrites the manifest under the new host's
	// key; the boot then imports it. A second copy, because the imported one
	// above now holds a row wrapped under the first key.
	rewrapRoot := copyRoot(t, d.snapRoot, secondAuthority)
	rewrapDir, err := changelogfile.RepoDir(rewrapRoot, secondAuthority)
	if err != nil {
		t.Fatal(err)
	}
	report, err := engine.RewrapRepositoryDir(rewrapDir, d.secondIdentity.String(), d.keyB)
	if err != nil {
		t.Fatalf("rewrap the directory: %v", err)
	}
	if report.Repository != secondAuthority {
		t.Errorf("rewrap report = %+v", report)
	}
	if rewrapped, err := changelogfile.ReadManifest(rewrapDir); err != nil || rewrapped.Format != changelogfile.ManifestFormat ||
		rewrapped.ChangelogDialect != engine.MaxChangelogDialect() {
		t.Errorf("the rewrapped manifest = %+v (%v), want format %d at changelog dialect %d",
			rewrapped, err, changelogfile.ManifestFormat, engine.MaxChangelogDialect())
	}
	rewrapped, err := engine.Open(ctx, testdb.NewSchema(t), engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(rewrapRoot), engine.WithCredentialKey(d.keyB), engine.WithTestTOTPClock(d.clock.Now))
	if err != nil {
		t.Fatalf("boot on the rewrapped directory: %v", err)
	}
	defer func() { _ = rewrapped.Close() }()
	rds, err := rewrapped.Dataset(ctx, secondAuthority)
	if err != nil {
		t.Fatalf("open the rewrapped repository: %v", err)
	}
	if rfold, err := rds.(folded).FoldSnapshot(ctx); err != nil || !bytes.Equal(rfold, d.secondFold) {
		t.Errorf("the fold restored from the rewrapped directory is not the source's (%v)\n%s", err, firstDifference(d.secondFold, rfold))
	}
	d.clock.Advance(engine.TOTPPeriod)
	if _, _, err := rewrapped.Login(ctx, substrate.LoginInput{Repository: secondAuthority, Password: secondPassword, TOTPCode: d.second.For(t).TOTPCode(), Label: "rewrapped"}); err != nil {
		t.Errorf("login on the rewrapped restore: %v", err)
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
	for _, table := range []string{"repositories", "changelog", "records"} {
		if n := d.countRows(t, newerDSN, table); n != 0 {
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
	if d.restored.gen == d.source.gen {
		t.Errorf("the restore kept the source's history generation %q", d.source.gen)
	}
	// The head the reset names is the restored history's head NOW, after
	// stage 4's writes, not the head at the import.
	var current substrate.ChangePage
	e.MustJSON(http.MethodGet, "/api/v1/changes?first=1", nil, &current)
	if current.Generation != d.restored.gen || current.Head < d.restored.head {
		t.Errorf("history page = head %d under %q, want at least %d under %q", current.Head, current.Generation, d.restored.head, d.restored.gen)
	}
	saved := fmt.Sprintf("from=%d&generation=%s", d.source.head-1, url.QueryEscape(d.source.gen))
	for _, path := range []string{"/api/v1/changes?" + saved, "/api/v1/changes?watch=1&" + saved, listPath(taskKind, nil) + "&watch=1&" + saved} {
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
	var page substrate.Page
	e.MustJSON(http.MethodGet, listPath(taskKind, nil)+"&first=500", nil, &page)
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

func captureState(t *testing.T, e *testenv.Env, ds substrate.Dataset, scoped *sql.DB) *state {
	t.Helper()
	ctx := context.Background()
	s := &state{
		records: map[string]any{}, deleted: map[string]any{}, statuses: map[string]substrate.TriggerStatus{},
		parked: map[string][]substrate.TriggerFailure{}, blobs: map[string][]byte{},
		lexical: map[string]searchAnswer{}, semantic: map[string]searchAnswer{},
	}
	for _, kind := range drillCollections {
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
		digest, _ := m["id"].(string)
		if propOf(t, m, digest, "status") != "stored" {
			continue
		}
		status, raw, _ := e.DoRaw(http.MethodGet, "/api/v1/blobs/"+digest, nil, nil)
		if status != http.StatusOK {
			t.Errorf("GET blob %s: %d %s", digest, status, raw)
			continue
		}
		s.blobs[digest] = raw
	}
	for _, q := range lexicalQueries {
		s.lexical[q] = searchRecords(t, e, q, "lexical", []string{taskKind})
	}
	for _, q := range semanticQueries {
		s.semantic[q] = searchRecords(t, e, q, "semantic", []string{taskKind})
	}
	head, err := ds.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.head, s.gen = head.Seq, head.Generation
	return s
}

// diffStates holds `got` to `want` field by field and returns every
// difference as its own finding, so one run lists them all.
func diffStates(want, got *state) []string {
	var out []string
	report := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	for _, k := range slices.Sorted(maps.Keys(want.records)) {
		g, ok := got.records[k]
		if !ok {
			report("record %s is missing", k)
			continue
		}
		if diff := firstJSONDifference(want.records[k], g); diff != "" {
			report("record %s differs: %s", k, diff)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(got.records)) {
		if _, ok := want.records[k]; !ok {
			report("record %s exists and the source had none", k)
		}
	}
	for _, kind := range drillCollections {
		if diff := firstJSONDifference(want.deleted[kind], got.deleted[kind]); diff != "" {
			report("the tombstones of %s differ: %s", kind, diff)
		}
	}
	if !reflect.DeepEqual(want.kindVersions, got.kindVersions) {
		report("kind versions differ:\n%v\n%v", want.kindVersions, got.kindVersions)
	}
	for _, id := range slices.Sorted(maps.Keys(want.statuses)) {
		w := want.statuses[id]
		g, ok := got.statuses[id]
		if !ok {
			report("trigger %s has no status", id)
			continue
		}
		// The cursor and the lag are scan positions: a restore comes back at
		// the last acknowledged delivery, never past it (decision 0064).
		if g.Enabled != w.Enabled || g.Kind != w.Kind || g.Parked != w.Parked || g.Pending != w.Pending || g.Error != w.Error {
			report("trigger %s status = %+v, want %+v", id, g, w)
		}
		if g.Cursor > w.Cursor {
			report("trigger %s came back at cursor %d, past the source's %d", id, g.Cursor, w.Cursor)
		}
		if diff := firstJSONDifference(want.parked[id], got.parked[id]); diff != "" {
			report("trigger %s parked list differs: %s", id, diff)
		}
	}
	if !reflect.DeepEqual(want.pagedCursors, got.pagedCursors) {
		report("paged cursors = %v, want %v", got.pagedCursors, want.pagedCursors)
	}
	if want.deliveries != got.deliveries {
		report("%d delivery entries, want %d", got.deliveries, want.deliveries)
	}
	if !bytes.Equal(want.fold, got.fold) {
		report("the fold differs from the source's\n%s", firstDifference(want.fold, got.fold))
	}
	for _, digest := range slices.Sorted(maps.Keys(want.blobs)) {
		if g, ok := got.blobs[digest]; !ok || !bytes.Equal(g, want.blobs[digest]) {
			report("blob %s bytes differ (present %v)", digest, ok)
		}
	}
	for _, q := range slices.Sorted(maps.Keys(want.lexical)) {
		if g := got.lexical[q]; !reflect.DeepEqual(g, want.lexical[q]) {
			report("lexical %q = %+v, want %+v", q, g, want.lexical[q])
		}
	}
	if want.head != got.head {
		report("head %d, want %d", got.head, want.head)
	}
	return out
}

// --- the record doors ----------------------------------------------------------------

func recordPath(kind, id string) string {
	return "/api/v1/" + kind + "/" + url.PathEscape(id)
}

// listPath is the records route narrowed to one kind: every list, tail and
// ranked read is GET /records, the kind under `filter`, and `extra` is any
// further filter arm beside it.
func listPath(kind string, extra map[string]any) string {
	f := map[string]any{"kinds": []string{kind}}
	for k, v := range extra {
		f[k] = v
	}
	raw, err := json.Marshal(f)
	if err != nil {
		panic(err)
	}
	return "/api/v1/records?filter=" + url.QueryEscape(string(raw))
}

// putRecord, patchRecord and deleteRecord are the three writes every
// repository in the drill goes through, so the second repository is written
// by the same path as the source. Each returns the record the door answered.
func putRecord(t *testing.T, e *testenv.Env, kind, id string, body map[string]any) map[string]any {
	t.Helper()
	var out map[string]any
	e.MustJSON(http.MethodPut, recordPath(kind, id), body, &out)
	return out
}

func patchRecord(t *testing.T, e *testenv.Env, kind, id string, body map[string]any) map[string]any {
	t.Helper()
	var out map[string]any
	e.MustJSON(http.MethodPatch, recordPath(kind, id), body, &out)
	return out
}

func deleteRecord(t *testing.T, e *testenv.Env, kind, id string) {
	t.Helper()
	e.MustJSON(http.MethodDelete, recordPath(kind, id), nil, nil)
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
	var out map[string]any
	e.MustJSON(http.MethodGet, recordPath(kind, id), nil, &out)
	return out
}

// propOf, labelsOf and metaOf read the optional parts of a record as the wire
// carries them, failing the stage by record and field where the shape is not
// the one expected: a `null` or an absent map is a finding here, never a
// panic that ends the drill.
func propOf(t *testing.T, rec map[string]any, id, name string) any {
	t.Helper()
	props, ok := rec["properties"].(map[string]any)
	if !ok {
		t.Fatalf("record %s carries no properties map: %v", id, rec["properties"])
	}
	return props[name]
}

func labelsOf(t *testing.T, rec map[string]any, id string) map[string]any {
	t.Helper()
	labels, ok := rec["labels"].(map[string]any)
	if !ok {
		t.Fatalf("record %s carries no labels map (a cleared record carries the empty map, #362): %v", id, rec["labels"])
	}
	return labels
}

func metaOf(t *testing.T, rec map[string]any, id, prop string) map[string]any {
	t.Helper()
	all, ok := rec["propertyMeta"].(map[string]any)
	if !ok {
		t.Fatalf("record %s carries no propertyMeta: %v", id, rec["propertyMeta"])
	}
	meta, ok := all[prop].(map[string]any)
	if !ok {
		t.Fatalf("record %s has no provenance for %s: %v", id, prop, all)
	}
	return meta
}

// listRecords is one kind's records, live or tombstoned, in the list's own
// order.
func listRecords(t *testing.T, e *testenv.Env, kind string, deleted bool) []any {
	t.Helper()
	var extra map[string]any
	if deleted {
		extra = map[string]any{"deleted": true}
	}
	path := listPath(kind, extra) + "&first=500"
	var page map[string]any
	e.MustJSON(http.MethodGet, path, nil, &page)
	if page["cursor"] != nil && page["cursor"] != "" {
		t.Fatalf("list %s spans more than one page of 500", kind)
	}
	records, _ := page["records"].([]any)
	return records
}

func containsID(records []any, id string) bool {
	return slices.ContainsFunc(records, func(r any) bool {
		m, ok := r.(map[string]any)
		return ok && m["id"] == id
	})
}

// kindVersions is every installed kind's declaration version.
func kindVersions(t *testing.T, e *testenv.Env) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, rec := range listRecords(t, e, corePkg+"/kind", false) {
		m := rec.(map[string]any)
		id, _ := m["id"].(string)
		v, _ := propOf(t, m, id, "version").(float64)
		out[id] = int64(v)
	}
	return out
}

func statuses(t *testing.T, e *testenv.Env) []substrate.TriggerStatus {
	t.Helper()
	var out substrate.OperationalList[substrate.TriggerStatus]
	e.MustJSON(http.MethodGet, "/api/v1/"+corePkg+"/trigger/status", nil, &out)
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
	var out substrate.OperationalList[substrate.TriggerFailure]
	e.MustJSON(http.MethodGet, "/api/v1/"+corePkg+"/trigger/"+id+"/parked", nil, &out)
	return out.Items
}

// searchRecords runs one ranked read through the records route, the one
// door search has: `q` on the collection, the kinds under `filter`.
func searchRecords(t *testing.T, e *testenv.Env, q, mode string, kinds []string) searchAnswer {
	t.Helper()
	filter, err := json.Marshal(map[string]any{"kinds": kinds})
	if err != nil {
		t.Fatal(err)
	}
	params := url.Values{"q": {q}, "mode": {mode}, "first": {"10"}, "filter": {string(filter)}}
	status, raw := e.Do(http.MethodGet, "/api/v1/records?"+params.Encode(), nil)
	var a searchAnswer
	if status/100 != 2 {
		var envelope substrate.ErrorEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Error.Code == "" {
			t.Fatalf("search answered %d without an error envelope: %s", status, raw)
		}
		a.ErrCode, a.ErrMsg = envelope.Error.Code, envelope.Error.Message
		return a
	}
	var out substrate.RankedPage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode the ranked page: %v (%s)", err, raw)
	}
	a.IDs = []string{}
	for _, r := range out.Records {
		a.IDs = append(a.IDs, r.ID)
	}
	a.Pending = out.Pending
	return a
}

// postWebhook posts one JSON request to the public webhook door and returns
// the fire id. The door authenticates by path and key alone, so the request
// carries no bearer, and the request as sent is held to that: a drill that
// sent a token would pass against a door that started demanding one.
func postWebhook(t *testing.T, e *testenv.Env, trigger, body, event string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), settleDeadline)
	defer cancel()
	resp := e.Request(ctx, http.MethodPost, "/webhooks/"+drillAuthority+"/"+trigger, []byte(body),
		map[string]string{"Content-Type": "application/json", "X-GitHub-Event": event, "Authorization": ""})
	defer func() { _ = resp.Body.Close() }()
	if _, sent := resp.Request.Header["Authorization"]; sent {
		t.Fatalf("the webhook request to %s carried an Authorization header", trigger)
	}
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

// waitFor polls cond until it holds or the settle deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(settleDeadline)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited %s for %s", settleDeadline, what)
		}
		time.Sleep(pollInterval)
	}
}

// firstStreamLine opens a watch and returns its first frame.
func firstStreamLine(t *testing.T, e *testenv.Env, path string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), settleDeadline)
	defer cancel()
	resp := e.Request(ctx, http.MethodGet, path, nil, nil)
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

// scoped is the repository's own pool on one schema, opened once per DSN
// and authority for the drill's lifetime.
func (d *drill) scoped(t *testing.T, dsn, authority string) *sql.DB {
	t.Helper()
	key := "scoped|" + authority + "|" + dsn
	if db, ok := d.dbs[key]; ok {
		return db
	}
	db, err := engine.OpenScopedDB(dsn, authority, engine.RoleApp)
	if err != nil {
		t.Fatalf("open the repository's pool: %v", err)
	}
	d.root.Cleanup(func() { _ = db.Close() })
	d.dbs[key] = db
	return db
}

// countRows counts a table through the schema's own connection, opened once
// per DSN.
func (d *drill) countRows(t *testing.T, dsn, table string) int {
	t.Helper()
	db, ok := d.dbs[dsn]
	if !ok {
		var err error
		if db, err = sql.Open("pgx", dsn); err != nil {
			t.Fatalf("open raw db: %v", err)
		}
		d.root.Cleanup(func() { _ = db.Close() })
		d.dbs[dsn] = db
	}
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
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
func copyRoot(t testing.TB, srcRoot, authority string) string {
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

// patchedSeedTree copies the shipped seed authority whole (its authority
// document and its core package, the tree kinds.Seed() embeds) and pins one
// core kind one version behind, so a repository seeded from it is one a
// substrated-seeded repository could be, and the next boot under the shipped
// tree upgrades that kind alone. It returns the copy and the shipped version.
func patchedSeedTree(t testing.TB, file string) (string, int64) {
	t.Helper()
	root := t.TempDir()
	dst := filepath.Join(root, filepath.Base(seedAuthorityDir))
	if err := os.CopyFS(dst, os.DirFS(seedAuthorityDir)); err != nil {
		t.Fatalf("copy the shipped seed: %v", err)
	}
	path := filepath.Join(dst, "core", file)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := reDeclaredVersion.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("%s pins no version of its own", file)
	}
	var shipped int64
	if _, err := fmt.Sscan(string(m[1]), &shipped); err != nil {
		t.Fatal(err)
	}
	raw = reDeclaredVersion.ReplaceAll(raw, []byte(fmt.Sprintf("\n  version: %d\n", shipped-1)))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, shipped
}

// reDeclaredVersion is a declaration's own version line, at the `data:`
// block's indentation, so a property named `version` is never the match.
var reDeclaredVersion = regexp.MustCompile(`\n  version: (\d+)\n`)

// --- diffing ---------------------------------------------------------------------------

// firstJSONDifference names the first path at which two decoded JSON values
// part, with both values there; "" when they are equal.
func firstJSONDifference(a, b any) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	return jsonDiff("$", a, b)
}

func jsonDiff(path string, a, b any) string {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: %s vs %s", path, short(a), short(b))
		}
		keys := slices.Sorted(maps.Keys(x))
		for k := range y {
			if _, ok := x[k]; !ok {
				keys = append(keys, k)
			}
		}
		for _, k := range keys {
			av, aok := x[k]
			bv, bok := y[k]
			if aok != bok {
				return fmt.Sprintf("%s.%s: %s vs %s", path, k, short(av), short(bv))
			}
			if d := jsonDiff(path+"."+k, av, bv); d != "" {
				return d
			}
		}
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
	default:
		if !reflect.DeepEqual(a, b) {
			return fmt.Sprintf("%s: %s vs %s", path, short(a), short(b))
		}
	}
	return ""
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
		names := slices.Sorted(maps.Keys(sa))
		for n := range sb {
			if _, ok := sa[n]; !ok {
				names = append(names, n)
			}
		}
		for _, n := range names {
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
