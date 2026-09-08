package substrate

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The console mirrors these shapes BY HAND. web/console/src/lib/api/types.ts is
// several hundred lines of TypeScript interfaces written to match the structs
// in this package, and nothing generates it: there is no OpenAPI document, no
// SDL export and no code generator anywhere in this tree, which is a deliberate
// simplification and also the one place the two halves can silently disagree.
//
// A Go field renamed, added or retagged used to be invisible to the console
// until something failed in a browser. This is the guard: the field names Go
// serializes, and whether each is always present, are written to a golden
// file, this test fails when they move, and a vitest beside the golden fails
// when the TypeScript does not match it. The golden is the contract the two
// sides meet at, and it is reviewed as a diff: one line per field, `true` for
// a field the server always writes and `false` for one it may omit.
//
// Adding a shape here is deliberate — it commits the console to tracking it.

var updateGolden = flag.Bool("update", false, "rewrite the wire golden file")

// goldenPath is relative because the console consumes the same file: the test
// writes it into web/console so a vitest can import it without reaching across
// the repository at runtime.
const goldenPath = "../../web/console/src/lib/api/wire.golden.json"

// wireTypes are the shapes types.ts mirrors. The KEY is the TypeScript name,
// which is the console's word for it — `Record` is `SubstrateRecord` there
// because TypeScript already owns `Record<K, V>`.
//
// Every interface a module under web/console/src/lib/api exports is here, and
// the vitest beside the golden refuses an export that is not (a client-only
// shape is named there, with the reason). A response an API handler builds as
// a bare map cannot be pinned, so a handler names its reply as a struct here
// first.
var wireTypes = map[string]any{
	// The error envelope: the one body every refused request answers with,
	// and the refusal it wraps.
	"ErrorEnvelope": ErrorEnvelope{},
	"ErrorPayload":  ErrorPayload{},
	"ProblemDetail": ProblemDetail{},

	"SubstrateRecord":     Record{},
	"IncomingReference":   IncomingReference{},
	"IncomingSource":      IncomingSource{},
	"IncomingPage":        IncomingPage{},
	"PropertyMeta":        PropertyMeta{},
	"PropertyAlternative": PropertyAlternative{},
	"PutInput":            PutInput{},
	// The patch body. The console calls it RecordPatch (records.ts).
	"RecordPatch": PatchInput{},
	// The filter grammar. The console calls the Filter `RecordFilter`.
	"Cond":         Cond{},
	"RecordFilter": Filter{},
	"KindInfo":     KindInfo{},

	// The change feed: the entry, the trigger stance decorating a row, the
	// row, and the history page. The row and the page are what /changes and
	// the watch stream serialize; a field that moves here moves the console's
	// feed and its list-to-watch handoff.
	"Change":        Change{},
	"ChangeTrigger": ChangeTrigger{},
	"ChangeRow":     ChangeRow{},
	"ChangePage":    ChangePage{},
	// The public change event nested in a Change (decision 0061): the console
	// renders it in place of the replay effects it used to decode.
	"AffectedRecord": AffectedRecord{},
	// The list envelope: the console hands its `head` and `generation` to the
	// watch, so a field that moves here moves that handoff.
	"Page":              Page{},
	"Occurrence":        Occurrence{},
	"OccurrenceLog":     OccurrenceLog{},
	"OccurrenceProblem": OccurrenceProblem{},
	"OccurrenceList":    OccurrenceList{},
	// The operational-list envelope. Element type does not change the field
	// names, so any instantiation pins items/cursor.
	"OperationalList": OperationalList[TokenInfo]{},

	// Auth: the token record, the mint that shows its secret once, and the
	// TOTP enrollment a registration hands back.
	"TokenInfo":      TokenInfo{},
	"MintedToken":    MintedToken{},
	"TOTPEnrollment": TOTPEnrollment{},
	// Registration: the request the door decodes and the answer it writes.
	// The console's RegisterInput and RegisterResult (auth.ts) mirror them.
	"RegisterInput":  RegisterRequest{},
	"RegisterResult": Registered{},
	// A credential change's answer: the username the factors proved.
	"SessionUser": SessionUser{},

	// The agent chat stream (agents.ts): one ndjson event, and the settled
	// result the done event carries.
	"AgentResult": AgentResult{},
	"AgentEvent":  AgentEvent{},

	// The catalog entry and the shapes nested in it. The console's Registry
	// reads them on every visit and its two sections key on `tier`, so a
	// field that moves here has to move there (decision record 0048).
	// internal/catalog embeds CatalogBundle rather than declaring its own,
	// which is what puts the shape in reach of this reflection.
	//
	// The KEYS are the TypeScript names, as every key here is: the console
	// calls these `BundleClosure` and `ShippedRecord`, while the Go types
	// carry the `Catalog` prefix that keeps them apart in this package.
	"CatalogBundle": CatalogBundle{},
	// The entry as the API serves it: the bundle's fields promoted, then
	// `installed` and `upgrade`. The console's CatalogItem extends its
	// CatalogBundle, so its key set is this flattened list.
	"CatalogItem":   CatalogItem{},
	"CatalogInput":  CatalogInput{},
	"BundleClosure": CatalogClosure{},
	"ShippedRecord": CatalogShippedRecord{},
	// One suggested mapping and its state. The Registry renders these on both
	// sections: a sample's card lists what it would project, a provider's
	// lists the samples waiting on it (decision record 0049).
	"SuggestedMapping": SuggestedMapping{},
	// The upgrade preview: a catalog entry carries one for an installed
	// provider. The Registry renders the motion and the blockers.
	"BundleUpgrade":       BundleUpgrade{},
	"BundleUpgradeChange": BundleUpgradeChange{},
	// `GET /api/v1/vocabulary/upgrade` carries one upgrade preview per shipped
	// package. The Registry renders the motion and the blockers.
	"ShippedUpgrade": ShippedUpgrade{},

	// The installed bundle's computed status and the lifecycle replies.
	"BundleStatus":      BundleStatus{},
	"InputStatus":       InputStatus{},
	"SetupItem":         SetupItem{},
	"BundleUninstalled": BundleUninstalled{},
	"BundlePurged":      BundlePurged{},
	"OAuthStarted":      OAuthStarted{},

	// The automation replies and the webhook door's 202.
	"TriggerReplayed": TriggerReplayed{},
	"TriggerRan":      TriggerRan{},
	"FunctionCalled":  FunctionCalled{},
	"WebhookAccepted": WebhookAccepted{},
}

// wireFields maps the wire names a struct serializes to whether each is
// REQUIRED: always present in the JSON, `null` included. A field tagged
// `omitempty` or `omitzero` is optional, because the server drops it at its
// zero value and a client has to read it as possibly absent. A field tagged
// `json:"-"` is not on the wire and is not listed: Record.Title and Record.At
// are server-side projections of properties, and the console is right not to
// know them.
//
// An embedded struct with no tag is flattened, as encoding/json promotes its
// fields: ChangeRow is a Change plus `triggers`, CatalogItem a CatalogBundle
// plus `installed` and `upgrade`, and the console mirrors both with `extends`.
// An embedded POINTER is refused: encoding/json drops every promoted field
// when it is nil, which no per-field boolean can say. So is `omitempty` on a
// struct value (time.Time included): it never omits one, so the golden would
// call required a field it marked optional; `omitzero` is the tag that works.
func wireFields(t *testing.T, v any) map[string]bool {
	t.Helper()
	rt := reflect.TypeOf(v)
	if rt.Kind() != reflect.Struct {
		t.Fatalf("wire type %s is not a struct", rt)
	}
	out := map[string]bool{}
	collectWireFields(t, rt, out)
	return out
}

func collectWireFields(t *testing.T, rt reflect.Type, out map[string]bool) {
	t.Helper()
	for i := range rt.NumField() {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		tag, tagged := f.Tag.Lookup("json")
		if f.Anonymous && !tagged {
			if f.Type.Kind() == reflect.Pointer {
				t.Fatalf("%s embeds *%s: a nil pointer drops every promoted field, which the golden cannot record", rt.Name(), f.Type.Elem().Name())
			}
			if f.Type.Kind() != reflect.Struct {
				t.Fatalf("%s embeds %s, which is not a struct: the wire name would be the Go name by accident", rt.Name(), f.Type)
			}
			collectWireFields(t, f.Type, out)
			continue
		}
		if !tagged {
			t.Fatalf("%s.%s has no json tag: the wire name would be the Go name by accident", rt.Name(), f.Name)
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if name == "" {
			t.Fatalf("%s.%s has a json tag that names nothing", rt.Name(), f.Name)
		}
		if _, dup := out[name]; dup {
			t.Fatalf("%s serializes %q twice", rt.Name(), name)
		}
		omitempty, omitzero := false, false
		for _, o := range strings.Split(opts, ",") {
			switch o {
			case "omitempty":
				omitempty = true
			case "omitzero":
				omitzero = true
			}
		}
		if omitempty && !omitzero && f.Type.Kind() == reflect.Struct {
			t.Fatalf("%s.%s: omitempty never omits a struct value (%s), so the field is required on the wire; tag it omitzero", rt.Name(), f.Name, f.Type)
		}
		out[name] = !omitempty && !omitzero
	}
}

func TestWireGoldenMatchesStructs(t *testing.T) {
	got := make(map[string]map[string]bool, len(wireTypes))
	for name, v := range wireTypes {
		got[name] = wireFields(t, v)
	}

	encoded, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	encoded = append(encoded, '\n')

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(goldenPath, encoded, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (regenerate with `go test ./internal/substrate/ -run TestWireGolden -update`): %v", err)
	}
	if string(want) != string(encoded) {
		t.Errorf("the wire shapes have moved and %s is stale.\n\n"+
			"Regenerate it:\n"+
			"  go test ./internal/substrate/ -run TestWireGolden -update\n\n"+
			"then make web/console/src/lib/api/types.ts match — `pnpm test` in\n"+
			"web/console fails until it does.\n\ngot:\n%s\nwant:\n%s",
			goldenPath, encoded, want)
	}
}
