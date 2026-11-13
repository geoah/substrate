package engine

// An entry's effects are a closed key set, as the line around them is
// (changelogfile.Decode). A field the effect structs do not carry would be a
// value the fold drops without a word, so foldOpsOf refuses the entry instead
// (docs/stored-format.md, "Effects").
//
// Before the decode turned strict, every effect field a released server ever
// wrote was read back from each release's tag (v0.1.0 to v0.123.1). The only
// ones the structs no longer carry are the edge fields (`rel`, `src`,
// `srcKind`, `dst`, `dstKind`, `dstType`, `props`, `subject`, and a resync's
// `rows.edges`), written by v0.1.0 to v0.35.0 on the `edge`, `unedge` and
// `edge1` effects alone. Since v0.36.0 a history holding those refuses at
// open, and the fold refuses the effect kinds by name, so no directory this
// binary could read holds one.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func foldOfJSON(t *testing.T, effects string) ([]foldOp, error) {
	t.Helper()
	var fold []any
	if err := json.Unmarshal([]byte(effects), &fold); err != nil {
		t.Fatalf("the fixture is not JSON: %v", err)
	}
	return foldOpsOf(substrate.Change{Seq: 7, Payload: map[string]any{foldPayloadKey: fold}})
}

// Every field the effects carry today reads back: the strict decode refuses
// what is unknown and nothing else.
func TestEveryEffectFieldReadsBack(t *testing.T) {
	t.Parallel()
	ops, err := foldOfJSON(t, `[
		{"kind":"record","ref":"a.example.com/tasks/task","id":"t1","version":2E0,
		 "delta":{"created":true,"restored":true,"force":true,"set":{"name":"x"},"del":["url"],
		          "title":"x","body":"","at":"","endsAt":"","dueAt":"2026-08-13T09:00:00Z",
		          "states":{"status":"open"},"labels":{},"finalizers":[],"kindVersion":3E0}},
		{"kind":"tombstone","ref":"a.example.com/tasks/task","id":"t1","finalizer":"substrate.merge"},
		{"kind":"purge","ref":"a.example.com/tasks/task","id":"t1"},
		{"kind":"annotation","ref":"a.example.com/tasks/task","id":"t1","key":"owner/note","value":false},
		{"kind":"manager","ref":"a.example.com/tasks/task","id":"t1","property":"name","actor":"6nk6ekkczdgh399nt2pqpccjz0",
		 "tier":"owner","token":"tok1","updatedAt":"2026-08-11T09:00:00Z"},
		{"kind":"former","ref":"a.example.com/tasks/task","id":"t1","formerId":"t2"},
		{"kind":"resync","scope":[{"kind":"a.example.com/tasks/task","id":"t1"}],
		 "rows":{"annotations":[{"kind":"a.example.com/tasks/task","id":"t1","key":"k","value":1,"updatedAt":"2026-08-11T09:00:00Z"}],
		         "managers":[{"kind":"a.example.com/tasks/task","id":"t1","property":"name","actor":"6nk6ekkczdgh399nt2pqpccjz0","tier":"owner","token":"tok1","updatedAt":"2026-08-11T09:00:00Z"}],
		         "formerIds":[{"kind":"a.example.com/tasks/task","formerId":"t2","recordId":"t1","createdAt":"2026-08-11T09:00:00Z"}]}},
		{"kind":"cursor","ref":"substrate.reamde.dev/core/trigger","id":"on-task","seq":1.8E1},
		{"kind":"schedule","ref":"substrate.reamde.dev/core/trigger","id":"hourly","at":"2026-08-11T09:00:00Z"},
		{"kind":"park","ref":"substrate.reamde.dev/core/trigger","id":"on-task",
		 "failure":{"id":4E1,"seq":1.8E1,"fireId":"f1","recordId":"t1","attempts":3E0,"lastError":"boom",
		            "parkedAt":"2026-08-11T09:00:00Z","payload":{"envelope":{}}}},
		{"kind":"unpark","ref":"substrate.reamde.dev/core/trigger","id":"on-task","failure":{"id":4E1}},
		{"kind":"page","ref":"substrate.reamde.dev/core/trigger","id":"on-task",
		 "page":{"chain":"c1","cursor":{"page":2},"cursorSha256":"ab","cursorBytes":1E1,"version":2E0,
		         "pages":2E0,"effects":2E0,"bytes":1E2,"startedAt":"2026-08-11T09:00:00Z",
		         "kind":"a.example.com/tasks/task","identity":"t1"}},
		{"kind":"unpage","ref":"substrate.reamde.dev/core/trigger","id":"on-task","page":{"chain":"c1"}},
		{"kind":"forget","ref":"substrate.reamde.dev/core/trigger","id":"on-task"}
	]`)
	if err != nil {
		t.Fatalf("an entry spelling every effect field the structs carry was refused: %v", err)
	}
	if len(ops) != 14 {
		t.Fatalf("read %d effects, want 14", len(ops))
	}
}

// A field the structs do not carry is refused at every depth of an effect,
// naming the field, rather than dropped.
func TestAnUnknownEffectFieldIsRefused(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, effects, field string }{
		{"on the effect", `[{"kind":"record","ref":"a.example.com/tasks/task","id":"t1","delta":{},"weight":3}]`, "weight"},
		{"in a delta", `[{"kind":"record","ref":"a.example.com/tasks/task","id":"t1","delta":{"set":{},"pinned":true}}]`, "pinned"},
		{"in a resync row", `[{"kind":"resync","scope":[],"rows":{"managers":[{"kind":"k","id":"i","property":"p","actor":"a","tier":"owner","updatedAt":"2026-08-11T09:00:00Z","setBy":"x"}]}}]`, "setBy"},
		{"in a scope", `[{"kind":"resync","scope":[{"kind":"k","id":"i","repository":"r"}],"rows":{}}]`, "repository"},
		{"in a parked failure", `[{"kind":"park","ref":"r","id":"i","failure":{"id":1,"retryAt":"later"}}]`, "retryAt"},
		{"in a page", `[{"kind":"page","ref":"r","id":"i","page":{"chain":"c","cursorUrl":"x"}}]`, "cursorUrl"},
		// What v0.35.0 and earlier wrote on an edge effect, and v0.36.0 refuses at open.
		{"an edge field a release before v0.36.0 wrote", `[{"kind":"record","ref":"a.example.com/tasks/task","id":"t1","delta":{},"rel":"assignee"}]`, "rel"},
		{"a resync's edges", `[{"kind":"resync","scope":[],"rows":{"edges":[]}}]`, "edges"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := foldOfJSON(t, c.effects)
			if err == nil {
				t.Fatalf("an effect carrying %q was read and the field dropped", c.field)
			}
			if !strings.Contains(err.Error(), `unknown field "`+c.field+`"`) || !strings.Contains(err.Error(), "seq 7") {
				t.Fatalf("the refusal must name the field and the seq: %v", err)
			}
		})
	}
}
