package engine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// oldLine is a history line as changelog dialect 1 wrote it: the actor
// spelled, the token under `principal`.
func oldLine(actor, principal, kind, payload string) changelogfile.Entry {
	return changelogfile.Entry{
		Seq: 7, TS: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC), Txn: 7,
		Actor: actor, Principal: principal, HasPrincipal: true,
		Op: "put", RecordID: "r1", Kind: kind, Payload: json.RawMessage(payload),
	}
}

// converted is one old line through the conversion, and its payload decoded.
func converted(t *testing.T, e changelogfile.Entry) (changelogfile.Entry, map[string]any) {
	t.Helper()
	if err := linkActorsLine(&e); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	return e, payload
}

func setOf(t *testing.T, payload map[string]any, i int) map[string]any {
	t.Helper()
	effect := payload[foldPayloadKey].([]any)[i].(map[string]any)
	return effect["delta"].(map[string]any)["set"].(map[string]any)
}

// WHAT EACH OLD LINE BECOMES (decision 0270): the actor's spelling becomes
// the id of its record, the token moves from `principal` to `token`, an
// app's write acts for the person, and every attribution the line's effects
// store is a link.
func TestLinkActorsLineTurnsEverySpellingIntoALink(t *testing.T) {
	person := vocabulary.ActorID(substrate.ActorPerson)
	app := vocabulary.ActorID(substrate.AppActor("chat.example.com"))
	sync := vocabulary.ActorID(substrate.FunctionActor("providers.substrate.reamde.dev", "google", "synccontacts"))
	link := func(id string) map[string]any { return vocabulary.ActorRef(id) }

	t.Run("a door's write is the person's, its token kept", func(t *testing.T) {
		// The retired header took any label, one shaped like an id included:
		// the line's own key says it is unconverted, so the label is the
		// person's like any other.
		idShaped := vocabulary.DerivedMintedID("label", "a script's")
		if !vocabulary.MintedID(idShaped) {
			t.Fatalf("%q is not shaped like an id, so the case proves nothing", idShaped)
		}
		for _, door := range []string{"api", "console", "substratectl", "a-label-a-script-claimed", idShaped} {
			e, _ := converted(t, oldLine(door, "tok1", "ada.example.com/tasks/task", `{"created":true}`))
			if e.Actor != person || e.OnBehalfOf != "" || e.Token != "tok1" || e.HasPrincipal {
				t.Errorf("%s: actor %q on behalf of %q token %q principal %v", door, e.Actor, e.OnBehalfOf, e.Token, e.HasPrincipal)
			}
		}
	})

	t.Run("an id-shaped label inside an unconverted payload is the person's too", func(t *testing.T) {
		idShaped := vocabulary.DerivedMintedID("label", "a script's")
		_, payload := converted(t, oldLine(idShaped, "tok1", "ada.example.com/tasks/task",
			`{"fold":[{"kind":"manager","ref":"ada.example.com/tasks/task","id":"r1","property":"name","actor":"`+idShaped+`","tier":"owner","principal":"tok1"}]}`))
		effect := payload[foldPayloadKey].([]any)[0].(map[string]any)
		if effect["actor"] != person {
			t.Errorf("manager actor = %v, want the person", effect["actor"])
		}
	})

	t.Run("a label the header took is the person's, whatever form it wears", func(t *testing.T) {
		// The base's header refused only the reserved forms, so behind a token
		// an `id:` wrapper (around another hand's id even), a judge's or a
		// mapping's spelling were each a label a person's request claimed.
		for _, label := range []string{"id:hello", "id:" + sync, "policy:keep-tidy", "mapping:ada.example.com/people/link"} {
			e, payload := converted(t, oldLine(label, "tok1", "ada.example.com/tasks/task",
				`{"fold":[{"kind":"manager","ref":"ada.example.com/tasks/task","id":"r1","property":"name","actor":"`+label+`","tier":"owner","principal":"tok1"}],
				  "moved":{"managers":[{"property":"url","actor":"`+label+`","tier":"owner","principal":"tok1"}]}}`))
			if e.Actor != person {
				t.Errorf("%s: actor %q, want the person", label, e.Actor)
			}
			if effect := payload[foldPayloadKey].([]any)[0].(map[string]any); effect["actor"] != person {
				t.Errorf("%s: manager actor %v, want the person", label, effect["actor"])
			}
			if moved := payload["moved"].(map[string]any)["managers"].([]any)[0].(map[string]any); moved["actor"] != person {
				t.Errorf("%s: moved manager actor %v, want the person", label, moved["actor"])
			}
		}
		// Behind no token an `id:` text is still no form the engine wrote.
		if e, _ := converted(t, oldLine("id:"+sync, "", "ada.example.com/tasks/task", `{}`)); e.Actor != person {
			t.Errorf("an id: label behind no token links %q, want the person", e.Actor)
		}
	})

	t.Run("an app's write acts for the person", func(t *testing.T) {
		e, _ := converted(t, oldLine("app:chat.example.com", "tok2", "ada.example.com/tasks/task", `{}`))
		if e.Actor != app || e.OnBehalfOf != person || e.Token != "tok2" {
			t.Errorf("actor %q on behalf of %q token %q", e.Actor, e.OnBehalfOf, e.Token)
		}
	})

	t.Run("the engine's hands are the system, the seed is core's package", func(t *testing.T) {
		for spelled, want := range map[string]substrate.Actor{
			"substrate": substrate.ActorSystem, "substrate.oauth": substrate.ActorSystem,
			"substrate.webhook": substrate.ActorSystem, "policy:keep-tidy": substrate.ActorSystem,
			"bundle:core":                         substrate.BundleActor("substrate.reamde.dev", "core"),
			"mapping:ada.example.com/people/link": substrate.BundleActor("ada.example.com", "people"),
		} {
			e, _ := converted(t, oldLine(spelled, "", "ada.example.com/tasks/task", `{}`))
			if e.Actor != vocabulary.ActorID(want) || e.OnBehalfOf != "" || e.Token != "" {
				t.Errorf("%s: actor %q, want %s's", spelled, e.Actor, want)
			}
		}
	})

	t.Run("a manager effect, the managers, triggeredBy and a merge's moved rows", func(t *testing.T) {
		_, payload := converted(t, oldLine("substrate", "", "ada.example.com/people/person", `{
			"fold":[{"kind":"manager","ref":"ada.example.com/people/person","id":"p1","property":"name",
			         "actor":"function:providers.substrate.reamde.dev:google:synccontacts","tier":"machine","principal":"tok3"},
			        {"kind":"resync","ref":"ada.example.com/people/person","id":"p1","rows":{"managers":[
			         {"kind":"ada.example.com/people/person","id":"p1","property":"name","actor":"console","tier":"owner","principal":"tok1","updatedAt":"2026-11-13T09:00:00Z"}]}}],
			"managers":{"name":"function:providers.substrate.reamde.dev:google:synccontacts"},
			"mechanism":"mapping","triggeredBy":"function:providers.substrate.reamde.dev:google:synccontacts",
			"moved":{"managers":[{"property":"email","actor":"api","tier":"owner","principal":"tok1"}]}}`))
		manager := payload[foldPayloadKey].([]any)[0].(map[string]any)
		if manager["actor"] != sync || manager["token"] != "tok3" || manager["principal"] != nil {
			t.Errorf("manager effect = %v", manager)
		}
		resync := payload[foldPayloadKey].([]any)[1].(map[string]any)["rows"].(map[string]any)["managers"].([]any)[0].(map[string]any)
		if resync["actor"] != person || resync["token"] != "tok1" || resync["principal"] != nil {
			t.Errorf("resync manager row = %v", resync)
		}
		if payload["managers"].(map[string]any)["name"] != sync || payload["triggeredBy"] != sync {
			t.Errorf("managers %v, triggeredBy %v", payload["managers"], payload["triggeredBy"])
		}
		moved := payload["moved"].(map[string]any)["managers"].([]any)[0].(map[string]any)
		if moved["actor"] != person || moved["token"] != "tok1" || moved["principal"] != nil {
			t.Errorf("moved manager = %v", moved)
		}
	})

	t.Run("the attributions a record stores", func(t *testing.T) {
		record := func(kind, set string) string {
			return `{"fold":[{"kind":"record","ref":"` + kind + `","id":"r1","version":1,"delta":{"set":` + set + `}}]}`
		}
		_, payload := converted(t, oldLine("bundle:core", "", kindPackage, record(kindPackage,
			`{"declaredBy":"app:chat.example.com","takenOverFrom":"app:chat.example.com","actors":["api","console"]}`)))
		set := setOf(t, payload, 0)
		if !jsonEqual(set["declaredBy"], link(app)) || set["takenOverFrom"] != "chat.example.com" || set["actors"] != nil {
			t.Errorf("package = %v", set)
		}
		_, payload = converted(t, oldLine("console", "tok1", kindBlob, record(kindBlob, `{"createdBy":"console"}`)))
		if set := setOf(t, payload, 0); !jsonEqual(set["createdBy"], link(person)) {
			t.Errorf("blob = %v", set)
		}
		_, payload = converted(t, oldLine("substrate", "", typeTriggerRun, record(typeTriggerRun, `{"caller":"api","principal":"tok1"}`)))
		if set := setOf(t, payload, 0); !jsonEqual(set["caller"], link(person)) || set["token"] != "tok1" || set["principal"] != nil {
			t.Errorf("call run = %v", set)
		}
		_, payload = converted(t, oldLine("agent:ada.example.com:llm:helper", "", typeMessage, record(typeMessage,
			`{"callable":"function:ada.example.com:notes:save","toolCalls":[{"id":"c1","callable":"agent:ada.example.com:llm:scout"}]}`)))
		set = setOf(t, payload, 0)
		calls := set["toolCalls"].([]any)[0].(map[string]any)
		if set["callable"] != "substrate.reamde.dev/core/function/ada.example.com/notes/save" ||
			calls["callable"] != "substrate.reamde.dev/core/agent/ada.example.com/llm/scout" {
			t.Errorf("message = %v", set)
		}
		// The engine's system row of a stopped job's end: its event names the
		// callable and the hand that stopped it.
		event := `{"callable":"function:ada.example.com:notes:save","event":"toolCompleted","name":"save","ok":false,"output":"{\"error\":\"stopped by console\"}","toolCallId":"c1"}`
		raw, _ := json.Marshal(map[string]any{"role": "system", "content": event})
		_, payload = converted(t, oldLine("agent:ada.example.com:llm:helper", "", typeMessage, record(typeMessage, string(raw))))
		want := `{"callable":"substrate.reamde.dev/core/function/ada.example.com/notes/save","event":"toolCompleted","name":"save","ok":false,"output":"{\"error\":\"stopped by its owner\"}","toolCallId":"c1"}`
		if set := setOf(t, payload, 0); set["content"] != want {
			t.Errorf("system row content = %v, want %s", set["content"], want)
		}
		// A person's own words that say the same are theirs, untouched: as
		// prose, and as the engine's very event.
		for _, said := range []string{"the job was stopped by console", "stopped by api", `{"error":"stopped by console"}`, event} {
			raw, _ := json.Marshal(map[string]any{"role": "user", "content": said})
			_, payload = converted(t, oldLine("api", "", typeMessage, record(typeMessage, string(raw))))
			if set := setOf(t, payload, 0); set["content"] != said {
				t.Errorf("a person's message %q became %v", said, set["content"])
			}
			raw, _ = json.Marshal(map[string]any{"subject": said})
			_, payload = converted(t, oldLine("api", "", typeThread, record(typeThread, string(raw))))
			if set := setOf(t, payload, 0); set["subject"] != said {
				t.Errorf("a thread's subject %q became %v", said, set["subject"])
			}
		}
		_, payload = converted(t, oldLine("agent:ada.example.com:llm:helper", "", typeThread, record(typeThread, `{"reason":"stopped by app:local"}`)))
		if set := setOf(t, payload, 0); set["reason"] != "stopped by an app" {
			t.Errorf("thread = %v", set)
		}
		_, payload = converted(t, oldLine("bundle:providers.substrate.reamde.dev:google", "", typeTrigger, record(typeTrigger,
			`{"when":"!change.actor.startsWith(\"function:providers.substrate.reamde.dev:google:\") && change.actor != \"console\""}`)))
		want = `!("actorPackage" in change && change.actorPackage == "providers.substrate.reamde.dev/google" && change.actorType == "function") && change.actor != "` + person + `"`
		if set := setOf(t, payload, 0); set["when"] != want {
			t.Errorf("trigger when = %v, want %s", set["when"], want)
		}
	})
}

// A converted line comes back unchanged, so a history whose manifest names
// the old dialect over converted lines converts again to the same bytes.
func TestLinkActorsLineLeavesAConvertedLineAlone(t *testing.T) {
	e, _ := converted(t, oldLine("app:chat.example.com", "tok2", kindPackage,
		`{"fold":[{"kind":"manager","ref":"x.example.com/p/k","id":"r1","property":"name","actor":"console","tier":"owner","principal":"tok2"},
		          {"kind":"record","ref":"`+kindPackage+`","id":"r1","version":1,"delta":{"set":{"declaredBy":"app:chat.example.com"}}}],
		  "managers":{"name":"console"}}`))
	once, _, err := changelogfile.Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := converted(t, e)
	twice, _, err := changelogfile.Encode(again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(once, twice) {
		t.Fatalf("a second pass moved the line:\n  %s\n  %s", once, twice)
	}
}

// keptSpelling is an actor spelling as a whole JSON string, as a line of a
// kept directory holds it where an actor is named. A label or annotation key
// carries a slash, which this never matches.
var keptSpelling = regexp.MustCompile(`"(?:function|agent|bundle|app|mapping|policy|connector):[^"/]*"`)

// keptActorKey is an `actor` key inside a payload, a manager effect's or a
// manager row's, with what it names.
var keptActorKey = regexp.MustCompile(`"actor":"([^"]*)"`)

// THE KEPT DIRECTORY CONVERTS. Every line of the release's kept history,
// through the conversion, links its actors and carries no spelling. What it
// names (the differences from the directory as kept) is counted: each
// spelling an id, each `principal` a `token`, each app's line on behalf of
// the person. The four actor declarations core shipped keep their own
// lines, which repository migration 9 tombstones by name.
func TestTheKeptDirectoryLinksItsActors(t *testing.T) {
	segments, err := filepath.Glob("../testenv/testdata/kept/*/repositories/*/changelog/*.ndjson")
	if err != nil || len(segments) == 0 {
		t.Fatalf("no kept segment: %v", err)
	}
	spellings, tokens, onBehalf, lines := map[string]int{}, 0, 0, 0
	for _, path := range segments {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			e, _, err := changelogfile.Decode(line)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			lines++
			spellings[e.Actor]++
			if e.HasPrincipal && e.Principal != "" {
				tokens++
			}
			if err := linkActorsLine(&e); err != nil {
				t.Fatalf("seq %d: %v", e.Seq, err)
			}
			if e.OnBehalfOf != "" {
				onBehalf++
			}
			if !vocabulary.MintedID(e.Actor) || e.HasPrincipal {
				t.Errorf("seq %d converts to actor %q, principal %v", e.Seq, e.Actor, e.HasPrincipal)
			}
			out, _, err := changelogfile.Encode(e)
			if err != nil {
				t.Fatalf("seq %d: %v", e.Seq, err)
			}
			// The actor declarations keep their own lines, and a
			// declaration's text is its own: a property may be named
			// `principal`.
			if e.Kind == kindActor || vocabulary.IsDeclarationRef(e.Kind) {
				continue
			}
			if m := keptSpelling.Find(e.Payload); m != nil {
				t.Errorf("seq %d still names %s: %s", e.Seq, m, out)
			}
			if bytes.Contains(e.Payload, []byte(`"principal"`)) {
				t.Errorf("seq %d still carries principal: %s", e.Seq, out)
			}
			for _, m := range keptActorKey.FindAllSubmatch(e.Payload, -1) {
				if !vocabulary.MintedID(string(m[1])) {
					t.Errorf("seq %d still names the actor %q: %s", e.Seq, m[1], out)
				}
			}
		}
	}
	t.Logf("%d lines converted: actors spelled %v; %d tokens moved to `token`; %d lines gained onBehalfOf", lines, spellings, tokens, onBehalf)
}
