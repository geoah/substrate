package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
)

// A page cut by --limit says where the next one starts on stderr, so stdout
// under -o json is one array json.Unmarshal reads whole (#889).
func TestGetJSONKeepsTheCursorOnStderr(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedTask(h)
	h.fake.seed(&substrate.Record{
		ID: "t10", Kind: taskKind,
		Properties: map[string]any{"title": "Order rack rails", "lifecycle": "open"},
		Version:    1, CreatedAt: testNow.Add(-time.Hour), UpdatedAt: testNow.Add(-time.Hour),
	})

	out, errOut := h.mustRun("get", taskKind, "-o", "json", "--limit", "1")
	var docs []map[string]any
	if err := json.Unmarshal([]byte(out), &docs); err != nil {
		t.Fatalf("stdout is not one JSON array: %v\n%s", err, out)
	}
	if len(docs) != 1 {
		t.Errorf("stdout carries %d records, want the one --limit allows", len(docs))
	}
	if !strings.Contains(errOut, "more results available; next cursor: 1") {
		t.Errorf("stderr lacks the cursor line: %q", errOut)
	}
}

// seedListings gives the fake two parked deliveries of one trigger, one
// whose error is too long for its column and one whose short first line sits
// over a traceback, and two bundles, one of them quarantined.
func seedListings(h *harness) {
	h.fake.parked = []substrate.TriggerFailure{
		{
			ID: 7, Trigger: "classify-page", Seq: 40, RecordID: "t9", Attempts: 3,
			// One ASCII byte, then two-byte characters: the 80th byte falls
			// inside a character.
			LastError: "x" + strings.Repeat("é", 100),
			ParkedAt:  testNow.Add(-time.Hour),
		},
		{
			ID: 8, Trigger: "classify-page", Seq: 41, RecordID: "t10", Attempts: 3,
			LastError: "RuntimeError: HTTP 500\nTraceback (most recent call last):",
			ParkedAt:  testNow.Add(-time.Hour),
		},
	}
	h.fake.bundles = []substrate.BundleStatus{
		{
			ID: "providers.substrate.reamde.dev/google", Name: "google", Authority: "providers.substrate.reamde.dev",
			Package: "google", Installed: true, Enabled: true, Accounts: 1, Functions: 4, Kinds: 3, LiveRecords: 12,
		},
		{
			ID: "providers.substrate.reamde.dev/broken", Name: "broken", Authority: "providers.substrate.reamde.dev",
			Package: "broken", Quarantined: true, QuarantineReason: "kind note: unknown key shade",
		},
	}
}

// Every listing command prints its items as one bare JSON array, the shape
// `kinds`, `token list` and `catalog` print, and `bundle status`, which reads
// one bundle, prints one object.
func TestListingCommandsPrintJSON(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		check func(t *testing.T, out string)
	}{
		{[]string{"trigger", "status"}, func(t *testing.T, out string) {
			var got []substrate.TriggerStatus
			mustUnmarshal(t, out, &got)
			if len(got) != 1 || got[0].ID != "classify-page" || got[0].Cursor != 41 {
				t.Errorf("trigger status = %+v", got)
			}
		}},
		{[]string{"trigger", "parked", "classify-page"}, func(t *testing.T, out string) {
			var got []substrate.TriggerFailure
			mustUnmarshal(t, out, &got)
			if len(got) != 2 || got[0].ID != 7 || got[1].ID != 8 {
				t.Fatalf("trigger parked = %+v", got)
			}
			// The table cuts the error; the JSON carries it whole.
			if got[0].LastError != "x"+strings.Repeat("é", 100) ||
				got[1].LastError != "RuntimeError: HTTP 500\nTraceback (most recent call last):" {
				t.Errorf("lastError was cut: %q, %q", got[0].LastError, got[1].LastError)
			}
		}},
		{[]string{"sync", "status"}, func(t *testing.T, out string) {
			var got []substrate.SyncStatus
			mustUnmarshal(t, out, &got)
			if len(got) != 1 || got[0].ID != "george-work" || got[0].State != substrate.SyncStateErroring {
				t.Errorf("sync status = %+v", got)
			}
		}},
		{[]string{"bundle", "list"}, func(t *testing.T, out string) {
			var got []substrate.BundleStatus
			mustUnmarshal(t, out, &got)
			if len(got) != 2 || got[0].LiveRecords != 12 || !got[1].Quarantined {
				t.Errorf("bundle list = %+v", got)
			}
		}},
		{[]string{"bundle", "status", "providers.substrate.reamde.dev/google"}, func(t *testing.T, out string) {
			var got substrate.BundleStatus
			mustUnmarshal(t, out, &got)
			if got.ID != "providers.substrate.reamde.dev/google" || got.Functions != 4 {
				t.Errorf("bundle status = %+v", got)
			}
		}},
	} {
		t.Run(strings.Join(tc.args[:2], " "), func(t *testing.T) {
			h := newHarness(t)
			h.writeConfig()
			seedListings(h)
			out, _ := h.mustRun(append(tc.args, "-o", "json")...)
			tc.check(t, out)
		})
	}
}

func mustUnmarshal(t *testing.T, out string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(out), v); err != nil {
		t.Fatalf("stdout is not one JSON value of %T: %v\n%s", v, err, out)
	}
}

// mustDecodeOneYAML decodes out into v and fails unless out holds exactly
// one YAML document: yaml.Unmarshal reads the first document and drops
// whatever follows it.
func mustDecodeOneYAML(t *testing.T, out string, v any) {
	t.Helper()
	dec := yaml.NewDecoder(strings.NewReader(out))
	if err := dec.Decode(v); err != nil {
		t.Fatalf("stdout is not a YAML document of %T: %v\n%s", v, err, out)
	}
	var rest any
	if err := dec.Decode(&rest); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout holds more than one YAML document (next decode: %v, %v)\n%s", err, rest, out)
	}
}

// -o yaml carries the wire's field names, not yaml.v3's lowercased Go names,
// and keeps numbers numbers and timestamps strings.
func TestListingYAMLUsesTheWireFieldNames(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedListings(h)

	out, _ := h.mustRun("bundle", "list", "-o", "yaml")
	var bundles []map[string]any
	mustDecodeOneYAML(t, out, &bundles)
	if len(bundles) != 2 || bundles[0]["liveRecords"] != 12 {
		t.Errorf("bundle list -o yaml = %v", bundles)
	}

	out, _ = h.mustRun("trigger", "parked", "classify-page", "-o", "yaml")
	var parked []map[string]any
	mustDecodeOneYAML(t, out, &parked)
	if len(parked) != 2 {
		t.Fatalf("trigger parked -o yaml = %v", parked)
	}
	if parked[0]["attempts"] != 3 || parked[0]["recordId"] != "t9" {
		t.Errorf("trigger parked -o yaml = %v", parked[0])
	}
	if _, ok := parked[0]["parkedAt"].(string); !ok {
		t.Errorf("parkedAt = %#v, want the wire's RFC 3339 string", parked[0]["parkedAt"])
	}
}

// An empty listing prints `[]` in both formats, including when the server
// answers `"items": null`, so a script reads it as a list.
func TestListingPrintsAnEmptyListAsAnArray(t *testing.T) {
	for _, output := range []string{"json", "yaml"} {
		t.Run(output, func(t *testing.T) {
			h := newHarness(t)
			h.writeConfig()
			// No bundles seeded: the fake answers `"items": null`.
			out, _ := h.mustRun("bundle", "list", "-o", output)
			if out != "[]\n" {
				t.Errorf("bundle list -o %s with no bundles = %q, want %q", output, out, "[]\n")
			}
		})
	}
}

// A string that would read as another YAML type comes back from -o yaml as
// the same string, and a multi-line string keeps its newlines.
func TestPrintYAMLRoundTripsStringsExactly(t *testing.T) {
	in := []string{
		"true", "41", "null", "1.5", "2026-08-02T12:00:00Z", "",
		"RuntimeError: HTTP 500\nTraceback (most recent call last):",
		"two lines\nand a trailing newline\n",
	}
	var buf bytes.Buffer
	if err := printYAML(&buf, in); err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	mustDecodeOneYAML(t, buf.String(), &doc)
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.SequenceNode {
		t.Fatalf("-o yaml is not one sequence:\n%s", buf.String())
	}
	got := make([]string, 0, len(in))
	for _, n := range doc.Content[0].Content {
		// The tag is what a reader resolves the scalar to: an unquoted
		// `true` reads as !!bool, an unquoted timestamp as !!timestamp.
		if n.ShortTag() != "!!str" {
			t.Errorf("%q reads back as %s:\n%s", n.Value, n.ShortTag(), buf.String())
		}
		got = append(got, n.Value)
	}
	if !slices.Equal(got, in) {
		t.Errorf("round trip = %q, want %q", got, in)
	}
}

// Under -o json the quarantine lines go to stderr beside the document, the
// way `catalog` writes its note; the table keeps them under it on stdout.
func TestBundleListQuarantineLinesLeaveTheJSONAlone(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedListings(h)
	const line = "quarantined: providers.substrate.reamde.dev/broken: kind note: unknown key shade"

	out, errOut := h.mustRun("bundle", "list", "-o", "json")
	var got []substrate.BundleStatus
	mustUnmarshal(t, out, &got)
	if !strings.Contains(errOut, line) {
		t.Errorf("stderr lacks the quarantine line: %q", errOut)
	}

	out, _ = h.mustRun("bundle", "list")
	if !strings.Contains(out, line) {
		t.Errorf("the table lacks the quarantine line:\n%s", out)
	}
}

// The parked table shows one line per delivery: the error's first line, cut
// by characters so a multi-byte character is never split.
func TestTriggerParkedTableCutsTheErrorAtItsFirstLine(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedListings(h)

	out, _ := h.mustRun("trigger", "parked", "classify-page")
	if !utf8.ValidString(out) {
		t.Fatalf("the table split a character:\n%q", out)
	}
	if !strings.Contains(out, "RuntimeError: HTTP 500") || strings.Contains(out, "Traceback") {
		t.Errorf("the table does not show the error's first line alone:\n%s", out)
	}
	if want := "x" + strings.Repeat("é", 79) + "…"; !strings.Contains(out, want) {
		t.Errorf("the error is not cut at 80 characters:\n%s", out)
	}
	if lines := strings.Count(strings.TrimSpace(out), "\n") + 1; lines != 3 {
		t.Errorf("table has %d lines, want the header and two deliveries:\n%s", lines, out)
	}
}

func TestTruncateCountsCharacters(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 3, "abc"},
		{"abcd", 3, "abc…"},
		{"abc", 0, "…"},
		{"", 0, ""},
		// Shorter than n in characters, longer in bytes: nothing is cut.
		{"日本", 3, "日本"},
		{"éé", 3, "éé"},
		{"日本語", 3, "日本語"},
		{"日本語です", 3, "日本語…"},
		{"héllo", 2, "hé…"},
	} {
		if got := truncate(tc.in, tc.n); got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}
