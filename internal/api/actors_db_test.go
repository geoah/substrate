package api

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// THE ACTORS A WRITE LINKS (decision 0270), on the real engine and over HTTP.
// One repository is written by every type of actor: the person through a
// sign-in, an app through its token, an agent the person's chat started, a
// sync a schedule fired, the engine merging what the sync offered, the
// package installs that laid the samples down and the creation itself.

// actorsWorld is a repository written by every type of actor, and the token
// ids and change seqs the tests read it back by.
type actorsWorld struct {
	*appAgentsEnv
	personToken, appToken string
	// person and app are the records each wrote; synced the sync's source
	// record and profile the target the engine merged it onto.
	person, app, synced, profile string
	agentThread                  string
}

// The sync package: a source kind a scheduled function writes, mapped onto a
// profile kind the engine merges it into.
func actorsSyncPackage(env *dbEnv) (pkg, source, profile string, docs []map[string]any) {
	pkg = env.repository + "/feed"
	source, profile = pkg+"/entry", pkg+"/profile"
	docs = []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "profile"}, map[string]any{
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"emails": map[string]any{"type": "email", "repeated": true},
			},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "entry"}, map[string]any{
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"email": map[string]any{"type": "email"},
				"profile": map[string]any{
					"type": "reference", "kind": profile, "required": true, "mustExist": true, "subject": true,
				},
			},
		}),
		vocabulary.MappingManifest(pkg, "entryprofile", map[string]any{
			"from": source, "to": profile, "property": "profile",
			"match": []any{map[string]any{"from": "email", "to": "emails"}},
			"map": map[string]any{
				"name":   map[string]any{"path": "name"},
				"emails": map[string]any{"path": "email", "merge": "union"},
			},
		}),
		vocabulary.FunctionManifest(pkg, "pull", map[string]any{
			"description": "pulls one entry on a schedule",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{source}},
			"source": `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "` + source + `", "id": "e-grace",
                         "properties": {"name": "Grace", "email": "grace@example.com"}}]}
`,
		}),
	}
	return pkg, source, profile, docs
}

func newActorsWorld(t *testing.T) *actorsWorld {
	t.Helper()
	ctx := context.Background()
	// The schedule's clock alone moves: past the next occurrence, so the
	// trigger pass fires it as a schedule does.
	clock := &engine.TestClock{}
	e := newAppAgentsEnv(t, false, withEngine(engine.WithTestScheduleClock(clock.Now)))
	appAgentsLLM(t, e.dbEnv)
	w := &actorsWorld{appAgentsEnv: e}

	// The person, through a sign-in of its own, sending the retired header an
	// older client still sends.
	personInfo, personSecret, err := e.ds.MintToken(ctx, substrate.MintTokenInput{Label: "console on a laptop"})
	if err != nil {
		t.Fatal(err)
	}
	w.personToken = personInfo.ID
	rec := e.do(t, http.MethodPost, recordsPath, personSecret,
		map[string]any{"kind": personKind, "properties": map[string]any{"name": "Ada"}}, retiredActorHeader, "console")
	wantStatus(t, rec, http.StatusCreated)
	w.person = decodeJSON[substrate.Record](t, rec).ID

	// An app the person approved, through its own token.
	appInfo, appSecret, err := e.ds.MintToken(ctx, substrate.MintTokenInput{
		Label: "chat", ClientID: "https://chat.example.com/client.json", ClientName: "Chat",
		Scopes: []string{"read:" + personKind, "write:" + personKind},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.appToken = appInfo.ID
	rec = e.do(t, http.MethodPost, recordsPath, appSecret,
		map[string]any{"kind": personKind, "properties": map[string]any{"name": "Grace"}})
	wantStatus(t, rec, http.StatusCreated)
	w.app = decodeJSON[substrate.Record](t, rec).ID

	// An agent the person started: a chat through the person's sign-in.
	chat := e.do(t, http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(e.talked)+"/chat",
		personSecret, map[string]any{"message": "hi"})
	wantStatus(t, chat, http.StatusOK)
	for _, line := range strings.Split(chat.Body.String(), "\n") {
		if i := strings.Index(line, `"thread":"`); i >= 0 && w.agentThread == "" {
			rest := line[i+len(`"thread":"`):]
			w.agentThread = rest[:strings.Index(rest, `"`)]
		}
	}
	if w.agentThread == "" {
		t.Fatalf("the chat named no thread: %s", chat.Body.String())
	}

	// A sync a schedule fires, and the engine merging what it offered.
	pkg, source, profile, docs := actorsSyncPackage(e.dbEnv)
	appAgentsApply(t, e.dbEnv, docs...)
	if _, err := e.ds.Put(ctx, substrate.ActorSystem, substrate.PutInput{
		Kind: corePackage + "/trigger", ID: "pull-hourly", Properties: map[string]any{
			"enabled": true,
			"source": map[string]any{"schedule": map[string]any{
				"recurrence": "FREQ=HOURLY", "timezone": "UTC",
			}},
			"callable": vocabulary.RecordPath(corePackage+"/function", pkg+"/pull"),
		},
	}); err != nil {
		t.Fatalf("write the schedule: %v", err)
	}
	clock.Advance(2 * time.Hour)
	if _, err := e.ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process triggers: %v", err)
	}
	w.synced = "e-grace"
	entry := e.get(t, source, w.synced)
	if entry == nil {
		t.Fatal("the scheduled sync wrote no entry")
	}
	ref, _ := entry.Properties["profile"].(map[string]any)
	path, _ := ref[vocabulary.ReferenceValueKey].(string)
	w.profile = path[strings.LastIndex(path, "/")+1:]
	if w.profile == "" || e.get(t, profile, w.profile) == nil {
		t.Fatalf("the engine merged the entry onto no profile: %v", entry.Properties["profile"])
	}
	return w
}

// actorChanges is the history page as the owner reads it, with the three
// links each change carries.
type actorChanges struct {
	Changes []substrate.Change `json:"changes"`
}

// changesOf reads, over HTTP as the owner, every change that addressed one
// record, oldest first.
func (w *actorsWorld) changesOf(t *testing.T, kind, id string) []substrate.Change {
	t.Helper()
	rec := w.do(t, http.MethodGet, "/api/v1/changes?first=200&recordKind="+url.QueryEscape(kind)+
		"&recordId="+url.QueryEscape(id), w.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[actorChanges](t, rec)
	slices.Reverse(page.Changes)
	if len(page.Changes) == 0 {
		t.Fatalf("no change addressed %s/%s", kind, id)
	}
	return page.Changes
}

// actorOf reads, over HTTP, the actor record a link names: its type and the
// link it carries.
func (w *actorsWorld) actorOf(t *testing.T, id string) (typ, link string) {
	t.Helper()
	rec := w.do(t, http.MethodGet, "/api/v1/"+corePackage+"/actor/"+url.PathEscape(id), w.token, nil)
	wantStatus(t, rec, http.StatusOK)
	actor := decodeJSON[substrate.Record](t, rec)
	typ, _ = actor.Properties["type"].(string)
	for _, name := range []string{"function", "agent", "package"} {
		if ref, ok := actor.Properties[name].(map[string]any); ok {
			link, _ = ref[vocabulary.ReferenceValueKey].(string)
		}
	}
	if p, ok := actor.Properties["publisher"].(string); ok {
		link = p
	}
	return typ, link
}

// The page's table of what a write records, one row each, read over HTTP:
// the actor, the one it acted for and the token behind it.
func TestEachWriteRecordsItsActorOnBehalfOfAndToken(t *testing.T) {
	w := newActorsWorld(t)
	person := vocabulary.ActorID(substrate.ActorPerson)
	type want struct {
		typ, link, onBehalfOf, token string
	}
	check := func(row string, c substrate.Change, w2 want) {
		t.Helper()
		typ, link := w.actorOf(t, c.Actor)
		if typ != w2.typ || (w2.link != "" && link != w2.link) || c.OnBehalfOf != w2.onBehalfOf || c.Token != w2.token {
			t.Errorf("%s: seq %d is by a %s actor (%s) on behalf of %q with token %q; want a %s actor (%s) on behalf of %q with token %q",
				row, c.Seq, typ, link, c.OnBehalfOf, c.Token, w2.typ, w2.link, w2.onBehalfOf, w2.token)
		}
	}

	// The person edits: its own sign-in, on behalf of no one.
	check("the person edits", w.changesOf(t, personKind, w.person)[0],
		want{typ: substrate.ActorTypePerson, token: w.personToken})
	// The person uses the app: the app's write, on behalf of the person,
	// with the app's token.
	check("the person uses an app", w.changesOf(t, personKind, w.app)[0],
		want{typ: substrate.ActorTypeApp, link: "chat.example.com", onBehalfOf: person, token: w.appToken})
	// An agent the person started: the agent, on behalf of the person, behind
	// no token. The first row of its thread is the person's own message; the
	// agent's reply is the agent's.
	var agentRow *substrate.Change
	for _, c := range w.changesOf(t, kindThread, w.agentThread) {
		if typ, _ := w.actorOf(t, c.Actor); typ == substrate.ActorTypeAgent {
			agentRow = &c
			break
		}
	}
	if agentRow == nil {
		t.Fatal("no change to the agent's thread is the agent's")
	}
	check("an agent the person started", *agentRow,
		want{typ: substrate.ActorTypeAgent, link: vocabulary.RecordPath(corePackage+"/agent", w.talked), onBehalfOf: person})
	// A sync on a schedule: the function, on behalf of no one, behind no token.
	_, source, profile, _ := actorsSyncPackage(w.dbEnv)
	pkg := strings.TrimSuffix(source, "/entry")
	check("a sync on a schedule", w.changesOf(t, source, w.synced)[0],
		want{typ: substrate.ActorTypeFunction, link: vocabulary.RecordPath(corePackage+"/function", vocabulary.LocalizeRef(pkg+"/pull", w.repository))})
	// The engine merging what the sync offered: the system, on behalf of no
	// one, behind no token.
	var merged *substrate.Change
	for _, c := range w.changesOf(t, profile, w.profile) {
		if typ, _ := w.actorOf(t, c.Actor); typ == substrate.ActorTypeSystem {
			merged = &c
		}
	}
	if merged == nil {
		t.Fatal("no change to the merged profile is the system's")
	}
	check("the engine merging synced values", *merged, want{typ: substrate.ActorTypeSystem})
}

// actorSpelling is a prefixed hand's retired spelling as a whole JSON
// string. A label or annotation key keeps its `<actor>/<name>` namespace and
// carries a slash, which this never matches.
var actorSpelling = regexp.MustCompile(`"(?:function|agent|bundle|app|mapping|policy|connector):[^"/]*"`)

// attribution is a payload key that names a hand, with the string it holds.
// A stored attribution is a reference, an object, so a string under
// `declaredBy`, `createdBy` or `caller` is the retired spelling.
var attribution = regexp.MustCompile(`"(actor|triggeredBy|caller|createdBy|declaredBy)":"([^"]*)"`)

// NO HISTORY LINE, CHANGE EVENT OR STORED VALUE HOLDS AN ACTOR AS TEXT: the
// repository's history as its export carries it, every change event the
// feed serves, and every attribution a record stores link actor records,
// and every link names one the repository holds.
func TestNoHistoryLineChangeEventOrStoredValueHoldsAnActorAsText(t *testing.T) {
	w := newActorsWorld(t)
	linked := map[string]bool{}
	link := func(where, id string) {
		t.Helper()
		if id == "" {
			return
		}
		if !vocabulary.MintedID(id) {
			t.Errorf("%s holds %q, which is no actor record's id", where, id)
			return
		}
		linked[id] = true
	}

	// History, as the export carries its segment files.
	rec := w.do(t, http.MethodGet, "/api/v1/export", w.token, nil)
	wantStatus(t, rec, http.StatusOK)
	lines := 0
	tr := tar.NewReader(bytes.NewReader(rec.Body.Bytes()))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(hdr.Name, "/changelog/") || !strings.HasSuffix(hdr.Name, ".ndjson") {
			continue
		}
		raw, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			e, _, err := changelogfile.Decode(line)
			if err != nil {
				t.Fatalf("%s: %v", hdr.Name, err)
			}
			lines++
			where := fmt.Sprintf("line %d", e.Seq)
			link(where+" actor", e.Actor)
			link(where+" onBehalfOf", e.OnBehalfOf)
			if e.HasPrincipal {
				t.Errorf("%s carries `principal`", where)
			}
			// A record of the actor kind holds its own name, not a link, and a
			// declaration's text is its own: a property may be named
			// `principal`.
			if e.Kind == corePackage+"/actor" || vocabulary.IsDeclarationRef(e.Kind) {
				continue
			}
			if m := actorSpelling.Find(e.Payload); m != nil {
				t.Errorf("%s (%s %s) holds the spelling %s: %s", where, e.Op, e.Kind, m, e.Payload)
			}
			for _, m := range attribution.FindAllSubmatch(e.Payload, -1) {
				if string(m[1]) == "declaredBy" || string(m[1]) == "createdBy" || string(m[1]) == "caller" {
					t.Errorf("%s (%s %s) names a hand by %s %q, not a link", where, e.Op, e.Kind, m[1], m[2])
					continue
				}
				link(where+" "+string(m[1]), string(m[2]))
			}
			var payload struct {
				Managers map[string]string `json:"managers"`
			}
			if err := json.Unmarshal(e.Payload, &payload); err == nil {
				for name, id := range payload.Managers {
					link(where+" manager of "+name, id)
				}
			}
			for _, key := range []string{`"principal"`} {
				if bytes.Contains(e.Payload, []byte(key)) {
					t.Errorf("%s carries %s in its payload: %s", where, key, e.Payload)
				}
			}
		}
	}
	if lines == 0 {
		t.Fatal("the export carried no history line")
	}

	// Every change event the feed serves.
	head := changesHead(t, w.dbEnv)
	rec = w.do(t, http.MethodGet, fmt.Sprintf("/api/v1/changes?first=%d", head.Seq), w.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[actorChanges](t, rec)
	if len(page.Changes) == 0 {
		t.Fatal("the feed served no change")
	}
	for _, c := range page.Changes {
		link(fmt.Sprintf("change %d actor", c.Seq), c.Actor)
		link(fmt.Sprintf("change %d onBehalfOf", c.Seq), c.OnBehalfOf)
	}

	// The stored attributions: each property's mark, a package's declarer,
	// a blob's creator.
	for _, read := range []struct{ kind, id string }{
		{personKind, w.person}, {personKind, w.app},
	} {
		got := w.get(t, read.kind, read.id)
		for name, m := range got.Marks {
			for what, l := range map[string]*substrate.RecordLink{"setBy": m.SetBy, "onBehalfOf": m.OnBehalfOf} {
				if l != nil {
					link(read.id+"."+name+" "+what, strings.TrimPrefix(l.Ref, corePackage+"/actor/"))
				}
			}
		}
	}
	packages, err := w.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{corePackage + "/package"}}, First: 200})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range packages.Records {
		if by, ok := p.Properties[substrate.PackageDeclaredBy]; ok {
			ref, isRef := by.(map[string]any)
			if !isRef {
				t.Errorf("package %s names its declarer as %v, not a link", p.ID, by)
				continue
			}
			path, _ := ref[vocabulary.ReferenceValueKey].(string)
			link("package "+p.ID+" declaredBy", strings.TrimPrefix(path, corePackage+"/actor/"))
		}
		if _, listed := p.Properties["actors"]; listed {
			t.Errorf("package %s still lists actors: %v", p.ID, p.Properties["actors"])
		}
	}

	// Every link names an actor record the repository holds.
	for id := range linked {
		if typ, _ := w.actorOf(t, id); typ == "" {
			t.Errorf("actor %s has a record of no type", id)
		}
	}
}
