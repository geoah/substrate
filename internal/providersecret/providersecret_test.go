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

// A quote cut short must not keep part of what Scrub removes: Scrub matches
// only the whole key and the whole starred token, so a fragment left at the
// cut would reach the sink. Every cut position across each shape is tried.
func TestTruncateNeverSplitsWhatScrubRemoves(t *testing.T) {
	t.Parallel()
	const key = "sk-proj-notarealkey000000000000000000000000000cdef"
	for name, quoted := range map[string]string{
		"quoted whole":         key,
		"quoted inside a JSON": `{"key":"` + key + `"}`,
		"self-masked":          key[:8] + strings.Repeat("*", 32) + key[len(key)-4:],
	} {
		body := "Incorrect API key provided: " + quoted + " and then some more words"
		start := strings.Index(body, quoted)
		for limit := start; limit <= start+len(quoted); limit++ {
			cut := Truncate(key, []byte(body), limit)
			if len(cut) > limit {
				t.Fatalf("%s at %d: %d bytes kept", name, limit, len(cut))
			}
			got := Scrub(key, string(cut))
			if strings.Contains(got, "sk-") || strings.Contains(got, "cdef") {
				t.Fatalf("%s at %d: a fragment of the key survived: %q", name, limit, got)
			}
			if !strings.HasPrefix(got, "Incorrect API key provided: ") {
				t.Fatalf("%s at %d: the words before the key were dropped: %q", name, limit, got)
			}
		}
	}
}

func TestTruncateCuts(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		key, s string
		limit  int
		want   string
	}{
		"short enough":             {"", "aaa bbb", 7, "aaa bbb"},
		"a cut at a space":         {"", "aaa bbb ccc", 7, "aaa bbb"},
		"a cut inside a token":     {"", "aaa bbb ccc", 6, "aaa "},
		"no whitespace at all":     {"", "aaaaaaaaaa", 4, ""},
		"a key holding whitespace": {"sk-a b", "x sk-a b y", 7, "x "},
		"overlapping keys":         {"a a", "x a a a y", 6, "x "},
	} {
		if got := string(Truncate(tc.key, []byte(tc.s), tc.limit)); got != tc.want {
			t.Errorf("%s: Truncate(%q, %q, %d) = %q, want %q", name, tc.key, tc.s, tc.limit, got, tc.want)
		}
	}
}
