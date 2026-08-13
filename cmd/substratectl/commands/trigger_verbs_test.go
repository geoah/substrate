package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A trigger run delivers ONE RECORD, and a record is addressed by (type, id):
// an id alone names no record, since two types may share one. The CLI used to
// send `{"id": …}` alone, which every real substrate answered with 400 — a
// drift no test caught, because the fake accepted any body at all.
func TestTriggerRunSendsTypeAndID(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()

	out, _ := h.mustRun("trigger", "run", "classify-page", "task", "t9")
	if !strings.Contains(out, "effects applied") {
		t.Fatalf("trigger run output = %q, want the applied-effects line", out)
	}
	body := h.fake.lastBody
	var gotType, gotID string
	if raw, ok := body["kind"]; ok {
		_ = json.Unmarshal(raw, &gotType)
	}
	if raw, ok := body["id"]; ok {
		_ = json.Unmarshal(raw, &gotID)
	}
	// The bare `task` resolves against the registry to the full identity, which
	// is what the wire names.
	if gotType != "tasks.substrate.reamde.dev/task" {
		t.Errorf("body kind = %q, want the resolved identity", gotType)
	}
	if gotID != "t9" {
		t.Errorf("body id = %q, want t9", gotID)
	}
}

// A qualified type travels verbatim — the server resolves identities too, and
// its error names the type better than a client-side guess would.
func TestTriggerRunAcceptsAQualifiedType(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.mustRun("trigger", "run", "classify-page", "tasks.substrate.reamde.dev/task", "t9")
	var gotType string
	_ = json.Unmarshal(h.fake.lastBody["kind"], &gotType)
	if gotType != "tasks.substrate.reamde.dev/task" {
		t.Fatalf("body kind = %q, want it passed through", gotType)
	}
}

// Every trigger verb rides the ONE path. Trigger records are core.substrate.reamde.dev's
// and a resource's operational verbs live at the resource, so the
// verbs hang off core; the retired automation.substrate.reamde.dev spelling is gone, not
// deprecated. The fake serves the core paths only, so a verb still riding the
// old authority 404s here.
func TestTriggerVerbsRideTheCorePath(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()

	h.mustRun("trigger", "status")
	h.mustRun("trigger", "run", "classify-page", "task", "t9")
	h.mustRun("trigger", "wake", "classify-page")

	for _, want := range []string{
		"GET " + triggerColPath + "/status",
		"POST " + triggerColPath + "/classify-page/run",
		"POST " + triggerColPath + "/classify-page/wake",
	} {
		found := false
		for _, got := range h.fake.requests {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no request %q; got %v", want, h.fake.requests)
		}
	}
	for _, got := range h.fake.requests {
		if strings.Contains(got, "/automation.substrate.reamde.dev/triggers") {
			t.Errorf("request %q rides the retired automation.substrate.reamde.dev trigger path", got)
		}
	}
}

// --- help examples name edges that exist -----------------------------------

// The examples in `link`/`unlink --help` are the first thing a person copies,
// so every relation they name MUST be one the shipped vocabulary declares.
// `assignee` and `employer` were neither, and a copied example failed with an
// undeclared-edge error against a clean install.
func TestEdgeExamplesNameDeclaredEdges(t *testing.T) {
	declared := shippedTypes(t)
	a := &app{}
	for _, cmd := range []string{"link", "unlink"} {
		var example string
		switch cmd {
		case "link":
			example = a.linkCommand().Example
		case "unlink":
			example = a.unlinkCommand().Example
		}
		if strings.TrimSpace(example) == "" {
			t.Fatalf("%s has no examples", cmd)
		}
		for _, line := range strings.Split(example, "\n") {
			// "substratectl link <plural> <id> <rel> <target>"
			fields := strings.Fields(line)
			if len(fields) < 6 {
				continue
			}
			plural, rel := fields[2], fields[4]
			ty, ok := declared[plural]
			if !ok {
				t.Errorf("%s example names collection %q, which the shipped schema does not declare", cmd, plural)
				continue
			}
			if !ty.edges[rel] {
				t.Errorf("%s example names edge %q on %q; declared edges are %v",
					cmd, rel, plural, sortedSet(ty.edges))
			}
		}
	}
}

// The `patch --help` examples are copied just as often, so the properties they
// name must exist too — on a clean install, `--prop detail=…` on a task wrote a
// property the shipped type never declared.
func TestPatchExamplesNameDeclaredProperties(t *testing.T) {
	declared := shippedTypes(t)
	example := (&app{}).patchCommand().Example
	if strings.TrimSpace(example) == "" {
		t.Fatal("patch has no examples")
	}
	for _, line := range strings.Split(example, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		plural := fields[2]
		ty, ok := declared[plural]
		if !ok {
			t.Errorf("patch example names collection %q, which the shipped schema does not declare", plural)
			continue
		}
		// The flags that name a PROPERTY. --label is a free key space and -p
		// carries raw JSON, so neither is checked here.
		for i := 3; i+1 < len(fields); i++ {
			if fields[i] != "--prop" && fields[i] != "--state" {
				continue
			}
			key, _, _ := strings.Cut(fields[i+1], "=")
			if !ty.properties[key] {
				t.Errorf("patch example writes %q on %q; declared properties are %v",
					key, plural, sortedSet(ty.properties))
			}
		}
	}
}

// shippedType is one declaration's surface: the edge relations and the
// property names it declares, plus the properties every record carries.
type shippedType struct {
	edges      map[string]bool
	properties map[string]bool
}

// shippedTypes reads the shipped declarations: plural → what it declares. The
// schema on disk is the only authority for what exists.
func shippedTypes(t *testing.T) map[string]shippedType {
	t.Helper()
	// The shipped vocabulary lives in TWO places since the seed shrank to
	// core: the seeded tree, and the VOCABULARY bundles a repository imports
	// (people, tasks, media, messaging, calendar). Help examples name kinds
	// from both, so both are read.
	base := filepath.Join("..", "..", "..")
	roots := []string{filepath.Join(base, "kinds", "core.substrate.reamde.dev")}
	for _, a := range []string{"calendar", "media", "messaging", "people", "tasks"} {
		roots = append(roots, filepath.Join(base, "kinds", a+".substrate.reamde.dev"))
	}
	out := map[string]shippedType{}
	var dirs []string
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read the shipped schema: %v", err)
		}
		named := false
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, filepath.Join(root, e.Name()))
				named = true
			}
		}
		if !named {
			dirs = append(dirs, root)
		}
	}
	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".yaml") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, file.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", file.Name(), err)
			}
			var doc struct {
				Data struct {
					Names struct {
						Plural string `yaml:"plural"`
					} `yaml:"names"`
					Edges      map[string]any `yaml:"edges"`
					Properties map[string]any `yaml:"properties"`
				} `yaml:"data"`
			}
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				continue // not a type declaration (an authority manifest, a trait)
			}
			if doc.Data.Names.Plural == "" {
				continue
			}
			ty, ok := out[doc.Data.Names.Plural]
			if !ok {
				ty = shippedType{edges: map[string]bool{}, properties: map[string]bool{}}
				// Every record carries these whatever it declares (FORMAT.md
				// §3): they are properties like any other.
				for _, name := range []string{"title", "body", "at", "endsAt", "dueAt"} {
					ty.properties[name] = true
				}
				out[doc.Data.Names.Plural] = ty
			}
			for rel := range doc.Data.Edges {
				ty.edges[rel] = true
			}
			for name := range doc.Data.Properties {
				ty.properties[name] = true
			}
		}
	}
	return out
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
