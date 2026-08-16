package engine

// THE CHANGE VERBS, HELD APART. Four declared properties name the same act in
// three vocabularies, and each one's words live in Go somewhere else: the
// policy selector matches the write verb the agent called, the request says
// what accepting will do, a trigger says what happened to the record, and an
// agent turn's `changes` carry the changelog op verbatim. Nothing type-checks
// a declaration against the constants the engine branches on, so this does.
//
// docs/changelog.md#change-verbs is the same table in prose, and a failure
// here means that page is wrong too.

import (
	"slices"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

// seedRegistry is the embedded tree, parsed once for every pin below.
var seedRegistry = sync.OnceValues(func() (*vocabulary.Registry, error) {
	return vocabulary.LoadFS(kinds.Seed())
})

// seedKind resolves one shipped core declaration, straight from the embedded
// tree: no database, so the pin runs in the short suite.
func seedKind(t *testing.T, ref string) *vocabulary.Kind {
	t.Helper()
	registry, err := seedRegistry()
	if err != nil {
		t.Fatalf("load the seed vocabulary: %v", err)
	}
	ty, err := registry.Resolve(ref)
	if err != nil {
		t.Fatalf("resolve %s: %v", ref, err)
	}
	return ty
}

func assertValues(t *testing.T, where string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s declares %v, want %v", where, got, want)
	}
}

// fieldValues reads a nested object field's declared enum values.
func fieldValues(t *testing.T, ty *vocabulary.Kind, path ...string) []string {
	t.Helper()
	p := ty.Props[path[0]]
	if p == nil {
		t.Fatalf("%s declares no property %q", ty.Identity, path[0])
	}
	for _, name := range path[1:] {
		f := p.Fields[name]
		if f == nil {
			t.Fatalf("%s: %q declares no field %q", ty.Identity, p.Name, name)
		}
		p = f
	}
	return p.ValueStrings()
}

func TestPolicySelectorOpsAreTheDoorsOwnVerbs(t *testing.T) {
	t.Parallel()
	// An op outside this set matches nothing, so declaring it as an enum is
	// what turns a selector written in the request's or the trigger's words
	// into a refusal instead of a rule that never fires.
	assertValues(t, "recordpatchpolicy selector.ops",
		fieldValues(t, seedKind(t, vocabulary.KindRecordPatchPolicy), "selector", "ops"),
		[]string{policyOpPut, policyOpPatch, policyOpDelete})
}

func TestRequestOpIsWhatAcceptingDoes(t *testing.T) {
	t.Parallel()
	assertValues(t, "recordpatchrequest op",
		fieldValues(t, seedKind(t, vocabulary.KindRecordPatchRequest), "op"),
		[]string{opCreate, opPatch, opDelete})
}

func TestTriggerOpsAreChangeClasses(t *testing.T) {
	t.Parallel()
	// runner.OpOf folds the changelog onto these three; parseTrigger refuses
	// anything else.
	assertValues(t, "trigger source.record.ops",
		fieldValues(t, seedKind(t, typeTrigger), "source", "record", "ops"),
		[]string{vocabulary.FunctionOpCreate, vocabulary.FunctionOpUpdate, vocabulary.FunctionOpDelete})
}

func TestMessageChangeOpsAreTheChangelogsOwn(t *testing.T) {
	t.Parallel()
	// Every op the effect applier can stamp on a turn's `changes`. `gc` is
	// absent on purpose: it is the collector's own pass, not a dispatch's
	// write.
	assertValues(t, "llmmessage changes.op",
		fieldValues(t, seedKind(t, typeMessage), "changes", "op"),
		[]string{
			string(substrate.OpPut), string(substrate.OpPatch), string(substrate.OpDelete),
			string(substrate.OpLink), string(substrate.OpUnlink),
			string(substrate.OpMerge), string(substrate.OpSplit),
		})
}
