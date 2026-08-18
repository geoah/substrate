package vocabulary_test

// Issue #251: a `secret` property may not declare a `default`. A default writes
// the credential into the declaration file and prefills it into the console's
// create form, so the value a secret exists to hide would live in a committed
// document. parseDefault refuses it (a sensitive value is never written into a
// declaration); these cases hold that the refusal names the offending property
// and that a non-sensitive property still carries its default.

import "testing"

func TestSecretPropertyRefusesDefault(t *testing.T) {
	// The refusal states the rule and names the offending property, so the
	// author's fix is where they are already looking.
	loadThingErr(t, `  properties:
    label: {type: string}
    apiKey: {type: secret, default: not-a-real-key}
`, "never written into a declaration")
	loadThingErr(t, `  properties:
    label: {type: string}
    apiKey: {type: secret, default: not-a-real-key}
`, "apiKey")
}

func TestDigestPropertyRefusesDefault(t *testing.T) {
	// `digest` is the other sensitive datatype, so the refusal covers it too.
	loadThingErr(t, `  properties:
    fingerprint: {type: digest, default: abc123}
`, "never written into a declaration")
}

func TestNonSensitivePropertyKeepsDefault(t *testing.T) {
	ty := loadThing(t, `  properties:
    kind: {type: string, default: note}
`)
	if got := ty.Props["kind"].Default; got != "note" {
		t.Fatalf("default on a non-sensitive property was dropped: %v", got)
	}
}
