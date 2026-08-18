package providersecret

import (
	"strings"
	"testing"
)

// A provider's 401 sends the key in one of two shapes, and only one is
// obvious. A short key comes back whole; a real one comes back MASKED, because
// the provider stars out the middle itself and returns the leading and
// trailing fragments. An exact-match scrub removes nothing from the masked
// form, so both fragments would reach the sink.
func TestScrubTakesTheKeyBackOut(t *testing.T) {
	t.Parallel()
	// Written as fragments of one synthetic key so the assertions are about
	// that key, never a literal. No real key appears in this tree.
	const key = "sk-proj-notarealkey000000000000000000000000000cdef"
	prefix, suffix := key[:8], key[len(key)-4:]
	for name, quoted := range map[string]string{
		"quoted whole": key,
		"self-masked":  prefix + strings.Repeat("*", 32) + suffix,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := "Incorrect API key provided: " + quoted +
				". You can find your API key at https://example.com/account/api-keys."
			got := Scrub(key, body)
			if strings.Contains(got, key) {
				t.Fatalf("the scrub left the key whole: %v", got)
			}
			if strings.Contains(got, prefix) || strings.Contains(got, suffix) {
				t.Fatalf("the scrub left a fragment of the key: %v", got)
			}
			if !strings.Contains(got, "<redacted>") {
				t.Fatalf("nothing was redacted: %v", got)
			}
			if !strings.Contains(got, "Incorrect API key provided") {
				t.Fatalf("the scrub ate the endpoint's message: %v", got)
			}
		})
	}
}

// An empty key is the config-refused row: there is no exact match to make, and
// the masked pass still runs so a provider that starred a key out is caught
// even when the row carried none for this caller.
func TestScrubWithNoKeyStillCatchesTheMaskedForm(t *testing.T) {
	t.Parallel()
	got := Scrub("", "refused: sk-live-abcd****************wxyz here")
	if strings.Contains(got, "sk-live-abcd") || strings.Contains(got, "wxyz") {
		t.Fatalf("the masked pass missed a starred token: %v", got)
	}
}
