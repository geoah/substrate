package engine

// THE MARKER (decision 0270). The retired actor header let a person's
// request claim any label the header did not refuse, and the engine stored
// the request's actor as text wherever it named the hand. Here one label is
// put, as the base wrote it, into every value the engine stored from the
// request's actor, and converted. Afterwards it stands nowhere but in the
// person's own values, and every place it stood holds the person.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const marker = "zz-marker"

// markedPosition is one value the base stored a request's actor text in.
type markedPosition struct {
	name string
	// line is an old line holding hand at the position, behind principal.
	line func(hand, principal string) changelogfile.Entry
	// at reads what the converted line holds where hand stood.
	at func(t *testing.T, e changelogfile.Entry, payload map[string]any) any
	// of is what the position holds when it names hand.
	of func(hand substrate.Actor) any
}

func idOf(hand substrate.Actor) any  { return vocabulary.ActorID(hand) }
func refOf(hand substrate.Actor) any { return vocabulary.ActorRef(vocabulary.ActorID(hand)) }

const markedTask = "ada.example.com/tasks/task"

func effectSet(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	effect, _ := asItems(payload[foldPayloadKey])[0].(map[string]any)
	delta, _ := effect["delta"].(map[string]any)
	set, _ := delta["set"].(map[string]any)
	if set == nil {
		t.Fatalf("no record effect in %v", payload)
	}
	return set
}

// contentOf is the JSON a converted message's content holds.
func contentOf(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	var content map[string]any
	if err := json.Unmarshal([]byte(effectSet(t, payload)["content"].(string)), &content); err != nil {
		t.Fatal(err)
	}
	return content
}

// The host functions behind the read and the wait tools, as the base
// spelled a tool row's callable.
const (
	baseQueryCallable = "function:substrate.reamde.dev:core:query"
	baseWaitCallable  = "function:substrate.reamde.dev:core:wait"
)

// toolResultLine is an agent's tool result row as the base wrote it (the
// loop's tool row: role, content, `ok`, and the callable that answered), the
// line the agent's turn wrote.
func toolResultLine(principal, callable string, ok bool, content map[string]any) changelogfile.Entry {
	raw, _ := json.Marshal(content)
	row, _ := json.Marshal(map[string]any{
		"role": "tool", "content": string(raw), "toolCallId": "call_0", "name": "t", "ok": ok, "callable": callable,
	})
	return recordLine(string(substrate.AgentActor("ada.example.com", "tasks", "triage")), principal, typeMessage, string(row))
}

func recordLine(actor, principal, kind, set string) changelogfile.Entry {
	return oldLine(actor, principal, kind,
		`{"fold":[{"kind":"record","ref":"`+kind+`","id":"r1","delta":{"set":`+set+`}}]}`)
}

func markedPositions() []markedPosition {
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	managerRow := func(hand, principal string) string {
		return `{"kind":"manager","ref":"` + markedTask + `","id":"r1","property":"name","actor":` + q(hand) +
			`,"tier":"owner","principal":` + q(principal) + `}`
	}
	movedRow := func(hand, principal string) string {
		return `{"property":"name","actor":` + q(hand) + `,"tier":"owner","principal":` + q(principal) + `}`
	}
	return []markedPosition{
		{
			name: "the line's actor",
			line: func(hand, principal string) changelogfile.Entry {
				return recordLine(hand, principal, markedTask, `{"name":"a task"}`)
			},
			at: func(_ *testing.T, e changelogfile.Entry, _ map[string]any) any { return e.Actor },
			of: idOf,
		},
		{
			name: "a manager row",
			line: func(hand, principal string) changelogfile.Entry {
				return oldLine(hand, principal, markedTask, `{"fold":[`+managerRow(hand, principal)+`]}`)
			},
			at: func(_ *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return asItems(payload[foldPayloadKey])[0].(map[string]any)["actor"]
			},
			of: idOf,
		},
		{
			name: "a resync's manager row",
			line: func(hand, principal string) changelogfile.Entry {
				return oldLine("substrate", principal, kindRecordMerge,
					`{"fold":[{"kind":"resync","scope":[{"kind":"`+markedTask+`","id":"w"}],"rows":{"managers":[`+
						`{"kind":"`+markedTask+`","id":"w","property":"name","actor":`+q(hand)+`,"tier":"owner","principal":`+q(principal)+`}]}}]}`)
			},
			at: func(_ *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				rows := asItems(payload[foldPayloadKey])[0].(map[string]any)["rows"].(map[string]any)
				return asItems(rows["managers"])[0].(map[string]any)["actor"]
			},
			of: idOf,
		},
		{
			name: "a merge's moved manager",
			line: func(hand, principal string) changelogfile.Entry {
				return oldLine(hand, principal, kindRecordMerge, `{"fold":[],"moved":{"managers":[`+movedRow(hand, principal)+`]}}`)
			},
			at: func(_ *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return asItems(payload["moved"].(map[string]any)["managers"])[0].(map[string]any)["actor"]
			},
			of: idOf,
		},
		{
			name: "a merge record's moved manager",
			line: func(hand, principal string) changelogfile.Entry {
				return recordLine(hand, principal, kindRecordMerge, `{"moved":{"managers":[`+movedRow(hand, principal)+`]}}`)
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return asItems(effectSet(t, payload)["moved"].(map[string]any)["managers"])[0].(map[string]any)["actor"]
			},
			of: idOf,
		},
		{
			name: "a split's kept result",
			line: func(hand, principal string) changelogfile.Entry {
				return recordLine(hand, principal, kindRecordSplit, `{"result":{"fold":[`+managerRow(hand, principal)+`]}}`)
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				result := effectSet(t, payload)["result"].(map[string]any)
				return asItems(result[foldPayloadKey])[0].(map[string]any)["actor"]
			},
			of: idOf,
		},
		{
			name: "a recompute's managers",
			line: func(hand, principal string) changelogfile.Entry {
				return oldLine("substrate", principal, markedTask,
					`{"fold":[],"managers":{"name":`+q(hand)+`},"mechanism":"mapping","triggeredBy":"substrate"}`)
			},
			at: func(_ *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return payload["managers"].(map[string]any)["name"]
			},
			of: idOf,
		},
		{
			name: "a recompute's triggeredBy",
			line: func(hand, principal string) changelogfile.Entry {
				return oldLine("substrate", principal, markedTask,
					`{"fold":[],"mechanism":"mapping","triggeredBy":`+q(hand)+`}`)
			},
			at: func(_ *testing.T, _ changelogfile.Entry, payload map[string]any) any { return payload["triggeredBy"] },
			of: idOf,
		},
		{
			name: "a blob's createdBy",
			line: func(hand, principal string) changelogfile.Entry {
				return recordLine(hand, principal, kindBlob, `{"createdBy":`+q(hand)+`,"name":"scan.pdf"}`)
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return effectSet(t, payload)["createdBy"]
			},
			of: refOf,
		},
		{
			name: "a call run's caller",
			line: func(hand, principal string) changelogfile.Entry {
				return recordLine("substrate", principal, typeTriggerRun,
					`{"caller":`+q(hand)+`,"principal":`+q(principal)+`}`)
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return effectSet(t, payload)["caller"]
			},
			of: refOf,
		},
		{
			name: "a package's declaredBy",
			line: func(hand, principal string) changelogfile.Entry {
				return recordLine(hand, principal, kindPackage, `{"declaredBy":`+q(hand)+`,"source":"installed"}`)
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return effectSet(t, payload)[substrate.PackageDeclaredBy]
			},
			of: refOf,
		},
		{
			name: "a conflict's refusal, in its payload",
			line: func(hand, principal string) changelogfile.Entry {
				return oldLine(hand, principal, markedTask,
					`{"fold":[],"conflict":{"reason":`+q(`actor "`+hand+`" may not write key "k"`)+`,"at":"2026-11-01T00:00:00Z"}}`)
			},
			at: func(_ *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return payload["conflict"].(map[string]any)["reason"]
			},
			of: func(hand substrate.Actor) any { return handWords(hand) + ` may not write key "k"` },
		},
		{
			name: "a conflict's refusal, in its annotation",
			line: func(hand, principal string) changelogfile.Entry {
				return oldLine(hand, principal, markedTask,
					`{"fold":[{"kind":"annotation","ref":"`+markedTask+`","id":"r1","key":"`+annApplyConflict+
						`","value":{"reason":`+q(markedTask+`.done is written by the "connector" role, not `+hand)+`}}]}`)
			},
			at: func(_ *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return asItems(payload[foldPayloadKey])[0].(map[string]any)["value"].(map[string]any)["reason"]
			},
			of: func(hand substrate.Actor) any {
				return markedTask + `.done is written by the "connector" role, not ` + handWords(hand)
			},
		},
		{
			name: "a conflict's refusal, told to the proposing thread",
			line: func(hand, principal string) changelogfile.Entry {
				event, _ := json.Marshal(map[string]any{"event": "proposalConflicted", "reason": hand + " may not accept a change request — no effective emit ceiling was carried into acceptance"})
				return recordLine(hand, principal, typeMessage, `{"role":"system","content":`+q(string(event))+`}`)
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return contentOf(t, payload)["reason"]
			},
			of: func(hand substrate.Actor) any {
				return handWords(hand) + " may not accept a change request — no effective emit ceiling was carried into acceptance"
			},
		},
		{
			name: "a triggered agent's envelope",
			line: func(hand, principal string) changelogfile.Entry {
				envelope, _ := json.Marshal(map[string]any{
					"change":     map[string]any{"seq": 9, "op": "patch", "kind": markedTask, "id": "r1", "actor": hand},
					"record":     map[string]any{"id": "r1", "kind": markedTask, "version": 2, "properties": map[string]any{"name": "a task"}},
					"repository": map[string]any{"authority": "ada.example.com"},
				})
				return recordLine(string(substrate.AgentActor("ada.example.com", "tasks", "triage")), principal, typeMessage,
					`{"role":"user","content":`+q(string(envelope))+`}`)
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return contentOf(t, payload)["change"].(map[string]any)["actor"]
			},
			of: idOf,
		},
		{
			name: "a tool's echo of a record's manager",
			line: func(hand, principal string) changelogfile.Entry {
				return toolResultLine(principal, baseQueryCallable, true, map[string]any{"record": map[string]any{
					"id": "r1", "kind": markedTask, "properties": map[string]any{"name": "a task"},
					"propertyMeta": map[string]any{"name": map[string]any{"manager": hand, "tier": "owner"}},
				}})
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				record := contentOf(t, payload)["record"].(map[string]any)
				return record["propertyMeta"].(map[string]any)["name"].(map[string]any)["manager"]
			},
			of: idOf,
		},
		{
			name: "a tool's echo of an alternative",
			line: func(hand, principal string) changelogfile.Entry {
				return toolResultLine(principal, baseQueryCallable, true, map[string]any{"records": []any{map[string]any{
					"id": "r1", "kind": markedTask, "properties": map[string]any{"name": "a task"},
					"propertyMeta": map[string]any{"name": map[string]any{"manager": "substrate", "alternatives": []any{
						map[string]any{"actor": hand, "value": "another task"},
					}}},
				}}})
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				record := asItems(contentOf(t, payload)["records"])[0].(map[string]any)
				alt := asItems(record["propertyMeta"].(map[string]any)["name"].(map[string]any)["alternatives"])[0]
				return alt.(map[string]any)["actor"]
			},
			of: idOf,
		},
		{
			name: "a tool's echo of a blob",
			line: func(hand, principal string) changelogfile.Entry {
				return toolResultLine(principal, baseQueryCallable, true, map[string]any{"record": map[string]any{
					"id": "b1", "kind": kindBlob, "properties": map[string]any{"createdBy": hand, "name": "scan.pdf"},
				}})
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return contentOf(t, payload)["record"].(map[string]any)["properties"].(map[string]any)["createdBy"]
			},
			of: refOf,
		},
		{
			name: "a wait's echo of a stopped job",
			line: func(hand, principal string) changelogfile.Entry {
				return toolResultLine(principal, baseWaitCallable, true, map[string]any{
					"completed": []any{map[string]any{"job": "call_0", "ok": false, "state": "canceled", "output": toolError(stoppedBy + hand)}},
					"working":   []any{},
				})
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return asItems(contentOf(t, payload)["completed"])[0].(map[string]any)["output"]
			},
			of: func(hand substrate.Actor) any { return toolError(reasonStoppedBy(hand)) },
		},
		{
			name: "a thread's stop reason",
			line: func(hand, principal string) changelogfile.Entry {
				return recordLine(hand, principal, typeThread, `{"status":"stopped","reason":`+q(stoppedBy+hand)+`}`)
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return effectSet(t, payload)["reason"]
			},
			of: func(hand substrate.Actor) any { return reasonStoppedBy(hand) },
		},
		{
			name: "a stopped job's output",
			line: func(hand, principal string) changelogfile.Entry {
				event, _ := json.Marshal(map[string]any{"event": "toolCompleted", "ok": false, "output": toolError(stoppedBy + hand)})
				return recordLine("substrate", principal, typeMessage, `{"role":"system","content":`+q(string(event))+`}`)
			},
			at: func(t *testing.T, _ changelogfile.Entry, payload map[string]any) any {
				return contentOf(t, payload)["output"]
			},
			of: func(hand substrate.Actor) any { return toolError(reasonStoppedBy(hand)) },
		},
	}
}

// EVERY VALUE THE ENGINE STORED FROM A REQUEST'S ACTOR converts by the rule
// and the evidence the line's own actor does. Behind a token, a label the
// header did not refuse is the person whatever it looks like; behind none, a
// label is the person unless it is one of the engine's own forms.
func TestTheMarkerLeavesNoTraceInAnyValueTheEngineStored(t *testing.T) {
	for _, c := range []struct {
		hand, principal string
	}{
		{marker, "tok1"},
		{"id:" + marker, "tok1"},
		{"policy:" + marker, "tok1"},
		{"mapping:" + marker + "/notes/link", "tok1"},
		{marker, ""},
		{"id:" + marker, ""},
	} {
		behind := "behind a token"
		if c.principal == "" {
			behind = "behind no token"
		}
		for _, p := range markedPositions() {
			t.Run(fmt.Sprintf("%s %s in %s", c.hand, behind, p.name), func(t *testing.T) {
				e, payload := converted(t, p.line(c.hand, c.principal))
				out, _, err := changelogfile.Encode(e)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(out, []byte(marker)) {
					t.Errorf("the marker survived the conversion: %s", out)
				}
				if got, want := p.at(t, e, payload), p.of(substrate.ActorPerson); !reflect.DeepEqual(got, want) {
					t.Errorf("holds %v, want the person's %v", got, want)
				}
			})
		}
	}
}

// Behind no token a `policy:` or a `mapping:` spelling is the engine's own
// form, so it converts to the hand it names, and the marker inside it goes
// with it all the same.
func TestTheEnginesOwnFormsBehindNoTokenAreTheirHands(t *testing.T) {
	for hand, want := range map[string]substrate.Actor{
		"policy:" + marker:                      substrate.ActorSystem,
		"mapping:notes.example/notes/" + marker: substrate.BundleActor("notes.example", "notes"),
	} {
		for _, p := range markedPositions() {
			e, payload := converted(t, p.line(hand, ""))
			out, _, err := changelogfile.Encode(e)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(out, []byte(marker)) {
				t.Errorf("%s in %s survived: %s", hand, p.name, out)
			}
			if got := p.at(t, e, payload); !reflect.DeepEqual(got, p.of(want)) {
				t.Errorf("%s in %s, behind no token, holds %v, want %v", hand, p.name, got, p.of(want))
			}
		}
	}
}

// A person's own values keep the marker: the conversion rewrites only what
// the engine stored about a hand, never what the person wrote, and a thread's
// reason that names no stop is not a stop.
func TestThePersonsOwnValuesKeepTheMarker(t *testing.T) {
	for name, e := range map[string]changelogfile.Entry{
		"a task's name":                   recordLine(marker, "tok1", markedTask, `{"name":"`+marker+`"}`),
		"the person's own message":        recordLine(marker, "tok1", typeMessage, `{"role":"user","content":"`+stoppedBy+marker+`"}`),
		"a thread reason that is no stop": recordLine(marker, "tok1", typeThread, `{"reason":"`+marker+`"}`),
	} {
		got, payload := converted(t, e)
		if got.Actor != vocabulary.ActorID(substrate.ActorPerson) {
			t.Errorf("%s: the line's actor %q, want the person", name, got.Actor)
		}
		raw, _ := json.Marshal(effectSet(t, payload))
		if !strings.Contains(string(raw), marker) {
			t.Errorf("%s lost the person's words: %s", name, raw)
		}
	}
	// A label the person wrote under the namespace its label had to name: the
	// key is the person's own text, and readers read it by name.
	e, _ := converted(t, oldLine(marker, "tok1", markedTask,
		`{"fold":[{"kind":"record","ref":"`+markedTask+`","id":"r1","delta":{"labels":{"`+marker+`/starred":true}}}]}`))
	if !bytes.Contains(e.Payload, []byte(`"`+marker+`/starred"`)) {
		t.Errorf("the person's label key moved: %s", e.Payload)
	}
}

// baseRefusals are the refusals the base composed around a hand, built as its
// constructors built them at f94f243de (copies of dataset.go metaKeyAllowed,
// write.go checkPropertyOwnership, the acceptance's emit ceiling twice, the
// token revoke and the minted id an agent's tool chose).
func baseRefusals(hand string) []error {
	return []error{
		fmt.Errorf("%w: actor %q may not write key %q", substrate.ErrForbidden, hand, "k"),
		fmt.Errorf("%w: %s.%s is written by the %q role, not %s", substrate.ErrForbidden, markedTask, "done", "connector", hand),
		fmt.Errorf("%w: %s may not accept a change request — no effective emit ceiling was carried into acceptance", substrate.ErrForbidden, hand),
		fmt.Errorf("%w: %s may not materialize a %s through an accepted %s request — %s is not in its effective emit allowlist",
			substrate.ErrForbidden, hand, markedTask, "create", markedTask),
		fmt.Errorf("%w: only the owner may revoke a %s: tokens are the owner's alone, and %s writes at the %s tier",
			substrate.ErrForbidden, kindToken, hand, "bundle"),
		fmt.Errorf("%w: %q is not a minted id: function %s runs as %s's tool and writes as the agent, so its new %s takes a minted id. The function has to derive it with host.ids.minted(<key>), which names the same record for the same key",
			substrate.ErrValidation, "x", "providers.example.com/web/scrape", hand, markedTask),
	}
}

// currentRefusals are the same refusals as this tree's constructors build
// them, the hand in words.
func currentRefusals(words string) []error {
	return []error{
		fmt.Errorf("%w: %s may not write key %q", substrate.ErrForbidden, words, "k"),
		fmt.Errorf("%w: %s.%s is written by the %q role, not %s", substrate.ErrForbidden, markedTask, "done", "connector", words),
		fmt.Errorf("%w: %s may not accept a change request — no effective emit ceiling was carried into acceptance", substrate.ErrForbidden, words),
		fmt.Errorf("%w: %s may not materialize a %s through an accepted %s request — %s is not in its effective emit allowlist",
			substrate.ErrForbidden, words, markedTask, "create", markedTask),
		fmt.Errorf("%w: only the owner may revoke a %s: tokens are the owner's alone, and %s writes at the %s tier",
			substrate.ErrForbidden, kindToken, words, "bundle"),
		fmt.Errorf("%w: %q is not a minted id: function %s runs as %s's tool and writes as the agent, so its new %s takes a minted id. The function has to derive it with host.ids.minted(<key>), which names the same record for the same key",
			substrate.ErrValidation, "x", "providers.example.com/web/scrape", words, markedTask),
	}
}

// refusalWraps are the ways a refusal's error reached a stored field: whole
// (a tool's error, a park, a run's reason), stripped of its sentinels (a
// conflict's reason, refusalReason), and as an accepted change request's
// (substrate.AcceptConflictError, whole).
var refusalWraps = map[string]func(error) string{
	"whole":    func(err error) string { return err.Error() },
	"stripped": refusalReason,
	"accepted": func(err error) string { return (&substrate.AcceptConflictError{Reason: refusalReason(err)}).Error() },
}

// refusalPosition is one field the engine stored a refusal in.
type refusalPosition struct {
	line func(msg string) changelogfile.Entry
	at   func(t *testing.T, payload map[string]any) string
}

func refusalPositions() map[string]refusalPosition {
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	errorOf := func(t *testing.T, s string) string {
		t.Helper()
		var out map[string]any
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			t.Fatal(err)
		}
		return out["error"].(string)
	}
	systemRow := func(event map[string]any) changelogfile.Entry {
		raw, _ := json.Marshal(event)
		return recordLine("substrate", "", typeMessage, `{"role":"system","content":`+q(string(raw))+`}`)
	}
	annotation := func(key, field, msg string) changelogfile.Entry {
		return oldLine("substrate", "", vocabulary.KindRecordPatchRequest,
			`{"fold":[{"kind":"annotation","ref":"`+vocabulary.KindRecordPatchRequest+`","id":"r1","key":"`+key+`","value":{"`+field+`":`+q(msg)+`}}]}`)
	}
	annotated := func(field string) func(*testing.T, map[string]any) string {
		return func(_ *testing.T, payload map[string]any) string {
			return asItems(payload[foldPayloadKey])[0].(map[string]any)["value"].(map[string]any)[field].(string)
		}
	}
	return map[string]refusalPosition{
		"a delivery's park": {
			line: func(msg string) changelogfile.Entry {
				return oldLine("substrate", "", typeTrigger,
					`{"fold":[{"kind":"park","ref":"`+typeTrigger+`","id":"r1","failure":{"id":3,"attempts":1,"lastError":`+q(msg)+`}}]}`)
			},
			at: func(_ *testing.T, payload map[string]any) string {
				return asItems(payload[foldPayloadKey])[0].(map[string]any)["failure"].(map[string]any)["lastError"].(string)
			},
		},
		"a failed tool call's error": {
			line: func(msg string) changelogfile.Entry {
				return toolResultLine("tok1", "function:providers.example.com:web:scrape", false, map[string]any{"error": msg})
			},
			at: func(t *testing.T, payload map[string]any) string { return contentOf(t, payload)["error"].(string) },
		},
		"a failed job's end": {
			line: func(msg string) changelogfile.Entry {
				return systemRow(map[string]any{"event": "toolCompleted", "toolCallId": "c1", "name": "t", "ok": false, "output": toolError(msg)})
			},
			at: func(t *testing.T, payload map[string]any) string {
				return errorOf(t, contentOf(t, payload)["output"].(string))
			},
		},
		"a failed job a wait answered": {
			line: func(msg string) changelogfile.Entry {
				return toolResultLine("tok1", baseWaitCallable, true, map[string]any{
					"completed": []any{map[string]any{"job": "c1", "ok": false, "state": "failed", "output": toolError(msg)}},
					"working":   []any{},
				})
			},
			at: func(t *testing.T, payload map[string]any) string {
				return errorOf(t, asItems(contentOf(t, payload)["completed"])[0].(map[string]any)["output"].(string))
			},
		},
		"an approval's report": {
			line: func(msg string) changelogfile.Entry {
				return systemRow(map[string]any{"event": "approvalDecided", "decision": "approved", "ok": false, "error": msg})
			},
			at: func(t *testing.T, payload map[string]any) string { return contentOf(t, payload)["error"].(string) },
		},
		"a conflict told to the proposing thread": {
			line: func(msg string) changelogfile.Entry {
				return systemRow(map[string]any{"event": "proposalConflicted", "op": "patch", "reason": msg})
			},
			at: func(t *testing.T, payload map[string]any) string { return contentOf(t, payload)["reason"].(string) },
		},
		"an approval's error": {
			line: func(msg string) changelogfile.Entry {
				return recordLine("agent:ada.example.com:mail:mailer", "", vocabulary.KindLLMApproval, `{"error":`+q(msg)+`}`)
			},
			at: func(t *testing.T, payload map[string]any) string { return effectSet(t, payload)["error"].(string) },
		},
		"a run's reason": {
			line: func(msg string) changelogfile.Entry {
				return recordLine("substrate", "", typeTriggerRun, `{"status":"failed","reason":`+q(msg)+`}`)
			},
			at: func(t *testing.T, payload map[string]any) string { return effectSet(t, payload)["reason"].(string) },
		},
		"a thread's reason": {
			line: func(msg string) changelogfile.Entry {
				return recordLine("agent:ada.example.com:tasks:triage", "", typeThread, `{"status":"error","reason":`+q(msg)+`}`)
			},
			at: func(t *testing.T, payload map[string]any) string { return effectSet(t, payload)["reason"].(string) },
		},
		"a conflict, in its payload": {
			line: func(msg string) changelogfile.Entry {
				return oldLine("substrate", "", markedTask, `{"fold":[],"conflict":{"reason":`+q(msg)+`}}`)
			},
			at: func(_ *testing.T, payload map[string]any) string {
				return payload["conflict"].(map[string]any)["reason"].(string)
			},
		},
		"a conflict, in its annotation": {
			line: func(msg string) changelogfile.Entry { return annotation(annApplyConflict, "reason", msg) },
			at:   annotated("reason"),
		},
		"a policy's verdict, in its payload": {
			line: func(msg string) changelogfile.Entry {
				return oldLine("substrate", "", vocabulary.KindRecordPatchRequest, `{"fold":[],"policyVerdict":{"outcome":"escalated","note":`+q(msg)+`}}`)
			},
			at: func(_ *testing.T, payload map[string]any) string {
				return payload["policyVerdict"].(map[string]any)["note"].(string)
			},
		},
		"a policy's verdict, in its annotation": {
			line: func(msg string) changelogfile.Entry { return annotation(annPolicyVerdict, "note", msg) },
			at:   annotated("note"),
		},
	}
}

// A REFUSAL NAMES NO HAND BY ITS SPELLING. In every field the engine stored a
// refusal in, each refusal the base composed, in each wrapping it was stored
// in, becomes the refusal this tree composes, wrapping and all, the hand in
// words. Prose of another shape keeps its words, the product's name included.
func TestARefusalNamesNoHandBySpelling(t *testing.T) {
	for _, hand := range []string{
		"function:providers.example.com:web:scrape", "agent:ada.example.com:tasks:worker", "api",
	} {
		words := handWords(spelledHand(hand, false))
		olds, news := baseRefusals(hand), currentRefusals(words)
		for wrapName, wrap := range refusalWraps {
			for i := range olds {
				for name, p := range refusalPositions() {
					e, payload := converted(t, p.line(wrap(olds[i])))
					out, _, err := changelogfile.Encode(e)
					if err != nil {
						t.Fatal(err)
					}
					if hand != "api" && bytes.Contains(out, []byte(hand)) {
						t.Errorf("%s, refusal %d %s, in %s keeps the spelling: %s", hand, i, wrapName, name, out)
					}
					if got, want := p.at(t, payload), wrap(news[i]); got != want {
						t.Errorf("%s, refusal %d %s, in %s:\n  got  %q\n  want %q", hand, i, wrapName, name, got, want)
					}
				}
			}
		}
	}
	prose := `"x" is not a minted id: a new task takes the id the substrate assigns. Leave the id out`
	for name, p := range refusalPositions() {
		if _, payload := converted(t, p.line(prose)); p.at(t, payload) != prose {
			t.Errorf("prose in %s moved: %q", name, p.at(t, payload))
		}
	}
}

// A PERSON'S VALUE KEEPS EVERY BYTE. A refusal-shaped sentence, and a stop
// sentence, planted in each place a person's or a callable's value can sit,
// at every depth, comes back unchanged: the conversion reads only the
// engine's own fields.
func TestARefusalShapedValueOfAPersonIsLeftByteForByte(t *testing.T) {
	var said []string
	for _, hand := range []string{"api", "function:providers.example.com:web:scrape", marker} {
		for _, err := range baseRefusals(hand) {
			for _, wrap := range refusalWraps {
				said = append(said, wrap(err))
			}
		}
		said = append(said, stoppedBy+hand, toolError(stoppedBy+hand))
	}
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	nested := func(s string) map[string]any {
		return map[string]any{
			"note": s, "error": s, "reason": s, "lastError": s, "output": s,
			"details": map[string]any{"note": s, "error": s, "reason": s, "items": []any{s, map[string]any{"error": s}}},
		}
	}
	for _, s := range said {
		values := nested(s)
		valuesJSON, _ := json.Marshal(values)
		agent := string(substrate.AgentActor("ada.example.com", "tasks", "triage"))
		for name, e := range map[string]changelogfile.Entry{
			"a task's values": recordLine(marker, "tok1", markedTask, string(valuesJSON)),
			"the person's message": recordLine(marker, "tok1", typeMessage,
				`{"role":"user","content":`+q(s)+`}`),
			"a proposal's diff": recordLine("agent:ada.example.com:tasks:editor", "", vocabulary.KindRecordPatchRequest,
				`{"op":"patch","diff":`+string(valuesJSON)+`,"rationale":`+q(s)+`}`),
			"another annotation": oldLine(marker, "tok1", markedTask,
				`{"fold":[{"kind":"annotation","ref":"`+markedTask+`","id":"r1","key":"`+marker+`/review","value":`+string(valuesJSON)+`}]}`),
			"a judge's envelope": func() changelogfile.Entry {
				envelope, _ := json.Marshal(map[string]any{
					"event": "judgeRequest", "request": vocabulary.RecordPath(vocabulary.KindRecordPatchRequest, "r1"),
					"op": "patch", "diff": values, "rationale": s, "criteria": s,
					"targetKind": markedTask, "target": map[string]any{"id": "r1", "kind": markedTask, "properties": values},
					"referents": []any{map[string]any{"id": "r2", "kind": markedTask, "properties": values}},
					"thread":    []any{map[string]any{"role": "user", "content": s}, map[string]any{"role": "assistant", "content": s}},
					"proposer":  "substrate.reamde.dev/core/agent/ada.example.com/tasks/editor",
				})
				return recordLine(agent, "", typeMessage, `{"role":"user","content":`+q(string(envelope))+`}`)
			}(),
			"a sub-agent's input": recordLine(agent, "", typeMessage, `{"role":"user","content":`+q(string(valuesJSON))+`}`),
			"a trigger's record": func() changelogfile.Entry {
				envelope, _ := json.Marshal(map[string]any{
					"change":     map[string]any{"seq": 9, "op": "patch", "kind": markedTask, "id": "r1", "actor": "api"},
					"record":     map[string]any{"id": "r1", "kind": markedTask, "version": 2, "properties": values},
					"repository": map[string]any{"authority": "ada.example.com"},
				})
				return recordLine(agent, "", typeMessage, `{"role":"user","content":`+q(string(envelope))+`}`)
			}(),
			"a read of a task": toolResultLine("tok1", baseQueryCallable, true, map[string]any{
				"record":  map[string]any{"id": "r1", "kind": markedTask, "properties": values},
				"records": []any{map[string]any{"id": "r2", "kind": markedTask, "properties": values}},
			}),
			"a callable's result": toolResultLine("tok1", "function:providers.example.com:web:scrape", true, values),
			"a succeeded job a wait answered": toolResultLine("tok1", baseWaitCallable, true, map[string]any{
				"completed": []any{map[string]any{"job": "c1", "ok": true, "state": "completed", "output": s}},
			}),
			"a succeeded job's end": func() changelogfile.Entry {
				event, _ := json.Marshal(map[string]any{"event": "toolCompleted", "ok": true, "output": s, "reason": s, "error": s})
				return recordLine("substrate", "", typeMessage, `{"role":"system","content":`+q(string(event))+`}`)
			}(),
			"an approved call's output": func() changelogfile.Entry {
				event, _ := json.Marshal(map[string]any{"event": "approvalDecided", "ok": true, "output": values})
				return recordLine("substrate", "", typeMessage, `{"role":"system","content":`+q(string(event))+`}`)
			}(),
		} {
			before := setOrPayload(t, e)
			_, payload := converted(t, e)
			after := setOrPayloadOf(t, payload)
			if !bytes.Equal(before, after) && !onlyTheChangeMoved(t, name, before, after) {
				t.Errorf("%s with %q moved:\n  before %s\n  after  %s", name, s, before, after)
			}
		}
	}
}

// setOrPayload is what a line's record effect writes, or its whole payload
// where it has no record effect, as JSON.
func setOrPayload(t *testing.T, e changelogfile.Entry) []byte {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	return setOrPayloadOf(t, payload)
}

func setOrPayloadOf(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	var v any = payload[foldPayloadKey]
	if effect, ok := asItems(payload[foldPayloadKey])[0].(map[string]any); ok && effect["delta"] != nil {
		set := effect["delta"].(map[string]any)["set"].(map[string]any)
		v = set
		// A message's own fields are the engine's (its callable becomes a
		// record path); what it says is its content.
		if content, ok := set["content"]; ok && effect["ref"] == typeMessage {
			v = map[string]any{"content": content}
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// onlyTheChangeMoved reports whether the one difference is a trigger's
// envelope's change actor: the engine's field beside the person's record.
func onlyTheChangeMoved(t *testing.T, name string, before, after []byte) bool {
	t.Helper()
	if name != "a trigger's record" {
		return false
	}
	var b, a map[string]any
	if json.Unmarshal(before, &b) != nil || json.Unmarshal(after, &a) != nil {
		return false
	}
	var be, ae map[string]any
	if json.Unmarshal([]byte(b["content"].(string)), &be) != nil || json.Unmarshal([]byte(a["content"].(string)), &ae) != nil {
		return false
	}
	be["change"].(map[string]any)["actor"] = ae["change"].(map[string]any)["actor"]
	return reflect.DeepEqual(be, ae)
}
