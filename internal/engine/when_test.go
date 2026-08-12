package engine

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// A `when` guard that reads a property not yet on the record — tokenStatus
// before OAuth lands — must be a normal NON-MATCH (skip), never a hard error
// that parks the delivery. Both the guarded `in` idiom and a bare read of an
// absent key evaluate to false against a pre-OAuth account; a present,
// satisfying account still matches. This is the engine half of the parked
// google-contacts-on-connect fix.
func TestWhenMissingKeyIsSkipNotPark(t *testing.T) {
	ctx := context.Background()

	// The google-contacts-on-connect guard, spelled with the `in` idiom.
	const guarded = `record != null && ` +
		`"tokenStatus" in record.properties && record.properties.tokenStatus == "connected" && ` +
		`"enabledContacts" in record.properties && record.properties.enabledContacts == true && ` +
		`!("contactsLastSyncedAt" in record.properties)`
	// The same intent written WITHOUT the guard — an unguarded read of an
	// optional property, the shape that used to park.
	const unguarded = `record != null && ` +
		`record.properties.tokenStatus == "connected" && ` +
		`record.properties.enabledContacts == true && ` +
		`!("contactsLastSyncedAt" in record.properties)`

	preOAuth := map[string]any{"record": map[string]any{
		// The delivery envelope carries the KIND REFERENCE (runner.Envelope).
		// Mirror that here, or the fixture teaches a shape no body ever sees.
		"kind":       "google.bundles.substrate.reamde.dev/account",
		"properties": map[string]any{}, // tokenStatus / enabledContacts not written yet
	}}
	connected := map[string]any{"record": map[string]any{
		"kind": "google.bundles.substrate.reamde.dev/account",
		"properties": map[string]any{
			"tokenStatus":     "connected",
			"enabledContacts": true,
		},
	}}

	for _, src := range []string{guarded, unguarded} {
		prog, err := vocabulary.CompileWhen(src)
		if err != nil {
			t.Fatalf("compile %q: %v", src, err)
		}
		// Pre-OAuth: the optional keys are absent. The old behavior raised
		// "no such key: tokenStatus" and parked; now it is a clean skip.
		ok, err := evalWhenProgram(ctx, prog, preOAuth)
		if err != nil {
			t.Fatalf("pre-OAuth guard errored (would park): %v", err)
		}
		if ok {
			t.Fatalf("pre-OAuth account must not match the on-connect guard")
		}
		// A connected, contacts-enabled, never-synced account still fires.
		ok, err = evalWhenProgram(ctx, prog, connected)
		if err != nil {
			t.Fatalf("connected guard errored: %v", err)
		}
		if !ok {
			t.Fatalf("connected account should match the on-connect guard")
		}
	}
}

// isMissingKeyErr keys off CEL's stable "no such key" message; a nil error and
// an unrelated error are not that miss.
func TestIsMissingKeyErr(t *testing.T) {
	if isMissingKeyErr(nil) {
		t.Fatal("nil is not a missing-key error")
	}
	if isMissingKeyErr(context.Canceled) {
		t.Fatal("an unrelated error is not a missing-key error")
	}
	prog, err := vocabulary.CompileWhen(`record.properties.absent == "x"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, _, evalErr := prog.ContextEval(context.Background(), map[string]any{
		"record": map[string]any{"properties": map[string]any{}},
	})
	if evalErr == nil || !isMissingKeyErr(evalErr) {
		t.Fatalf("indexing an absent key should be a missing-key error, got %v", evalErr)
	}
}
