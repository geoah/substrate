package authorize

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const syncDetails = `[
  {"type": "package", "package": "providers.substrate.reamde.dev/apple", "version": 3},
  {"type": "package", "package": "providers.substrate.reamde.dev/applehealth", "version": 5},
  {"type": "account", "kind": "providers.substrate.reamde.dev/applehealth/account",
   "id": "substrate-sync-1a2b3c4d",
   "properties": {"displayName": "Ada's iPhone", "timezone": "Europe/London", "limit": 3}},
  {"type": "webhook", "trigger": "applehealth-webhook"}
]`

func TestAuthorizationDetailsAreReadAsTheAppWroteThem(t *testing.T) {
	got, err := ParseDetails(syncDetails)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d details, want 4", len(got))
	}
	if p := got[0]; p.Type != "package" || p.Package != "providers.substrate.reamde.dev/apple" || p.Version != 3 || p.Properties != nil {
		t.Errorf("package = %+v", got[0])
	}
	acct := got[2]
	if acct.Kind != "providers.substrate.reamde.dev/applehealth/account" || acct.ID != "substrate-sync-1a2b3c4d" ||
		acct.Properties["displayName"] != "Ada's iPhone" {
		t.Errorf("account = %+v", acct)
	}
	// A number stays the digits the app sent, as a record body's does.
	if n, ok := acct.Properties["limit"].(json.Number); !ok || n.String() != "3" {
		t.Errorf("a number in properties decoded as %T %v, want json.Number 3", acct.Properties["limit"], acct.Properties["limit"])
	}
	if w := got[3]; w.Type != "webhook" || w.Trigger != "applehealth-webhook" || w.Package != "" || w.Properties != nil {
		t.Errorf("webhook = %+v", got[3])
	}
	if d, err := ParseDetails(""); d != nil || err != nil {
		t.Errorf("no parameter = %v, %v; want no details", d, err)
	}
}

func TestAuthorizationDetailsAreReadStrictly(t *testing.T) {
	many := make([]string, 9)
	for i := range many {
		many[i] = `{"type":"webhook","trigger":"t` + string(rune('a'+i)) + `"}`
	}
	long := `[{"type":"account","kind":"example.com/notes/account","id":"a","properties":{"note":"` +
		strings.Repeat("x", 1100) + `"}}]`
	cases := map[string]string{
		"not an array":         `{"type":"webhook","trigger":"t"}`,
		"null":                 `null`,
		"not JSON":             `[{"type":`,
		"an entry not object":  `["package"]`,
		"no type":              `[{"package":"example.com/notes","version":1}]`,
		"unknown type":         `[{"type":"record","kind":"example.com/notes/note","id":"n"}]`,
		"unknown member":       `[{"type":"package","package":"example.com/notes","version":1,"min":1}]`,
		"another type's":       `[{"type":"package","package":"example.com/notes","version":1,"trigger":"t"}]`,
		"miscased member":      `[{"type":"package","Package":"example.com/notes","version":1}]`,
		"url in a request":     `[{"type":"webhook","trigger":"t","url":"https://example.com/webhooks/t"}]`,
		"a fractional version": `[{"type":"package","package":"example.com/notes","version":1.5}]`,
		"a version as text":    `[{"type":"package","package":"example.com/notes","version":"3"}]`,
		"no version":           `[{"type":"package","package":"example.com/notes"}]`,
		"version zero":         `[{"type":"package","package":"example.com/notes","version":0}]`,
		"a bare package":       `[{"type":"package","package":"notes","version":1}]`,
		"a kind as package":    `[{"type":"package","package":"example.com/notes/note","version":1}]`,
		"duplicate package":    `[{"type":"package","package":"example.com/notes","version":1},{"type":"package","package":"example.com/notes","version":2}]`,
		"a bare kind":          `[{"type":"account","kind":"account","id":"a"}]`,
		"core kind":            `[{"type":"account","kind":"substrate.reamde.dev/core/token","id":"a"}]`,
		"llm kind":             `[{"type":"account","kind":"substrate.reamde.dev/llm/provider","id":"openai"}]`,
		"console kind":         `[{"type":"account","kind":"substrate.reamde.dev/console/recordview","id":"a"}]`,
		"bad id":               `[{"type":"account","kind":"example.com/notes/account","id":"has space"}]`,
		"the default id":       `[{"type":"account","kind":"example.com/notes/account","id":"default"}]`,
		"properties a list":    `[{"type":"account","kind":"example.com/notes/account","id":"a","properties":[1]}]`,
		"properties null":      `[{"type":"account","kind":"example.com/notes/account","id":"a","properties":null}]`,
		"properties too long":  long,
		"duplicate account":    `[{"type":"account","kind":"example.com/notes/account","id":"a"},{"type":"account","kind":"example.com/notes/account","id":"a"}]`,
		"a trigger glob":       `[{"type":"webhook","trigger":"*"}]`,
		"a trigger path":       `[{"type":"webhook","trigger":"a/b"}]`,
		"no trigger":           `[{"type":"webhook"}]`,
		"duplicate webhook":    `[{"type":"webhook","trigger":"t"},{"type":"webhook","trigger":"t"}]`,
		"too many":             "[" + strings.Join(many, ",") + "]",
		"too long":             "[" + strings.Repeat(" ", maxDetailsBytes) + "]",
		// The agent detail of decision 0207 is retired (decision 0214).
		"an agent detail":    `[{"type":"agent","agent":"~/trainingcoach/workoutCoach","reads":["~/training/routine"],"writes":[],"spendCentsPerDay":100}]`,
		"an agent's members": `[{"type":"package","package":"example.com/notes","version":1,"agent":"~/notes/keeper"}]`,
	}
	for name, param := range cases {
		if d, err := ParseDetails(param); err == nil {
			t.Errorf("%s: ParseDetails admitted %+v, want refused", name, d)
		}
	}
	// The owner's own package and kind are named under `~`, as a scope names
	// them, since an app cannot know the repository's name before the owner
	// signs in (decision record 0259).
	for name, param := range map[string]string{
		"home package": `[{"type":"package","package":"~/notes","version":1}]`,
		"home kind":    `[{"type":"account","kind":"~/notes/account","id":"a"}]`,
	} {
		if _, err := ParseDetails(param); err != nil {
			t.Errorf("%s: ParseDetails refused it: %v", name, err)
		}
	}
	if _, err := ParseDetails(cases["an agent detail"]); err == nil || !strings.Contains(err.Error(), "retired") || !strings.Contains(err.Error(), "package detail") {
		t.Errorf("an agent detail = %v, want a refusal naming what replaced it", err)
	}
	// One account of a kind and another id, and the same package beside
	// it, are three entries.
	ok := `[{"type":"package","package":"example.com/notes","version":1},
	{"type":"account","kind":"example.com/notes/account","id":"a"},
	{"type":"account","kind":"example.com/notes/account","id":"b"}]`
	if _, err := ParseDetails(ok); err != nil {
		t.Errorf("distinct entries were refused: %v", err)
	}
}

func TestADecodedBodyIsHeldToTheSameRules(t *testing.T) {
	for name, d := range map[string]substrate.AuthorizationDetail{
		"url":           {Type: "webhook", Trigger: "t", URL: "https://example.com/webhooks/t"},
		"cross members": {Type: "webhook", Trigger: "t", Package: "example.com/notes"},
		"empty props":   {Type: "package", Package: "example.com/notes", Version: 1, Properties: map[string]any{}},
		"no type":       {Package: "example.com/notes", Version: 1},
	} {
		if err := checkDetails([]substrate.AuthorizationDetail{d}); err == nil {
			t.Errorf("%s: checkDetails admitted %+v", name, d)
		}
	}
}

// A BUILD SAYS ITS VERSION (decision 0213): at most 64 printable characters,
// read once for /oauth/authorize and the set up check alike; nothing is
// nothing to record.
func TestASoftwareVersionIsBoundedAndPrintable(t *testing.T) {
	for raw, want := range map[string]string{
		"":                      "",
		"3.2.1 (418)":           "3.2.1 (418)",
		"v2024.11.03-β":         "v2024.11.03-β",
		strings.Repeat("x", 64): strings.Repeat("x", 64),
		strings.Repeat("é", 64): strings.Repeat("é", 64),
	} {
		if got, err := ParseSoftwareVersion(raw); err != nil || got != want {
			t.Errorf("ParseSoftwareVersion(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for name, raw := range map[string]string{
		"65 characters":  strings.Repeat("x", 65),
		"a newline":      "1.0\n",
		"a control byte": "1.0\x01",
		"a tab":          "1\t0",
		"invalid UTF-8":  "1.0\xff",
	} {
		if got, err := ParseSoftwareVersion(raw); err == nil {
			t.Errorf("%s: ParseSoftwareVersion admitted %q", name, got)
		}
	}
}

// THE FLOORS A TOKEN RECORDS are the package details' versions by id; a
// request naming no package records none.
func TestPackageFloorsAreThePackageDetailsVersions(t *testing.T) {
	details, err := ParseDetails(syncDetails)
	if err != nil {
		t.Fatal(err)
	}
	floors := PackageFloors(details)
	if len(floors) != 2 || floors["providers.substrate.reamde.dev/apple"] != 3 || floors["providers.substrate.reamde.dev/applehealth"] != 5 {
		t.Fatalf("floors = %v", floors)
	}
	if got := PackageFloors(details[2:]); got != nil {
		t.Fatalf("floors of no package detail = %v, want nil", got)
	}
}
