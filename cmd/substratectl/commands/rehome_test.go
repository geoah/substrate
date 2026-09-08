package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A sample file applied with --as lands under the authority named, not the
// placeholder it is authored under: the client-side half of a sample import
// (decision record 0048). The rehome is the server's own walk, so it reaches
// the id, the declared authority, the reference pin, `installs` and the
// authority a data record names inside its properties.
func TestApplyAsRehomesTheInput(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	dir := t.TempDir()
	file := filepath.Join(dir, "tasks.yaml")
	doc := `kind: substrate.reamde.dev/core/kind
metadata:
  id: samples.substrate.reamde.dev/tasks/task
data:
  authority: samples.substrate.reamde.dev
  package: tasks
  version: 1
  names:
    singular: task
  properties:
    assignee:
      type: reference
      kind: samples.substrate.reamde.dev/people/person
---
kind: substrate.reamde.dev/core/bundle
metadata:
  id: samples.substrate.reamde.dev/tasks
data:
  authority: samples.substrate.reamde.dev
  package: tasks
  installs:
    - samples.substrate.reamde.dev/tasks/task
`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := h.run("apply", "-f", file, "--as", "ada.example.com"); err != nil {
		t.Fatalf("apply --as: %v %s", err, stderr)
	}
	var sent []map[string]any
	if err := json.Unmarshal(h.fake.lastBody["documents"], &sent); err != nil {
		t.Fatalf("decode the applied documents: %v", err)
	}
	raw, err := json.Marshal(sent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "samples.substrate.reamde.dev") {
		t.Errorf("the applied documents still name the placeholder: %s", raw)
	}
	if !strings.Contains(string(raw), "ada.example.com/tasks/task") {
		t.Errorf("the applied documents do not carry the new authority: %s", raw)
	}
	// The core meta-kind reference is NOT the authority being rehomed: rewriting
	// it would declare into a meta-kind that does not exist.
	if !strings.Contains(string(raw), "substrate.reamde.dev/core/kind") {
		t.Errorf("the core reference was rewritten too: %s", raw)
	}
}

// A rehomed input that carries its package document names the package it was
// authored as, so the server stamps the landed copy with its origin exactly as
// an import would (decision record 0070). An input with no package document
// lands no package row to stamp, and claims nothing.
func TestApplyAsSendsTheOrigin(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	dir := t.TempDir()
	kindDoc := `kind: substrate.reamde.dev/core/kind
metadata:
  id: samples.substrate.reamde.dev/tasks/task
data:
  authority: samples.substrate.reamde.dev
  package: tasks
  names:
    singular: task
  properties:
    name:
      type: string
`
	packageDoc := `kind: substrate.reamde.dev/core/package
metadata:
  id: samples.substrate.reamde.dev/tasks
data:
  authority: samples.substrate.reamde.dev
  package: tasks
  version: 7
---
`
	closure := filepath.Join(dir, "closure.yaml")
	if err := os.WriteFile(closure, []byte(packageDoc+kindDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := h.run("apply", "-f", closure, "--as", "ada.example.com"); err != nil {
		t.Fatalf("apply --as: %v %s", err, stderr)
	}
	var origin string
	if err := json.Unmarshal(h.fake.lastBody["origin"], &origin); err != nil || origin != "samples.substrate.reamde.dev/tasks" {
		t.Errorf("the apply named origin %q (%v), want the package as authored", origin, err)
	}

	alone := filepath.Join(dir, "kind.yaml")
	if err := os.WriteFile(alone, []byte(kindDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := h.run("apply", "-f", alone, "--as", "ada.example.com"); err != nil {
		t.Fatalf("apply --as one kind: %v %s", err, stderr)
	}
	if _, claimed := h.fake.lastBody["origin"]; claimed {
		t.Errorf("an input with no package document claimed an origin: %s", h.fake.lastBody["origin"])
	}
}

// One request names one origin, so an input carrying several package documents
// is refused before anything is sent: letting it through would replace every
// copy unstamped and unconfirmed (decision record 0070).
func TestApplyAsRefusesSeveralPackages(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	packageDoc := func(word string) string {
		return `kind: substrate.reamde.dev/core/package
metadata:
  id: samples.substrate.reamde.dev/` + word + `
data:
  authority: samples.substrate.reamde.dev
  package: ` + word + `
  version: 2
`
	}
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.yaml"), filepath.Join(dir, "b.yaml")
	if err := os.WriteFile(a, []byte(packageDoc("tasks")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(packageDoc("people")), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := h.run("apply", "-f", a, "-f", b, "--as", "ada.example.com")
	if err == nil {
		t.Fatal("apply --as admitted two package documents in one run")
	}
	for _, want := range []string{"one package per run", "samples.substrate.reamde.dev/people and samples.substrate.reamde.dev/tasks"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %s", want, err)
		}
	}
	if contains(h.fake.doorRequests(), "POST /api/v1/vocabulary/apply") {
		t.Errorf("the refused apply still reached the server: %v", h.fake.doorRequests())
	}
}

// `substratectl import` calls the sample door and reports what LANDED: the
// package under this repository's own authority, not the shipped id typed.
func TestImportCallsTheSampleDoor(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	stdout, stderr, err := h.run("import", "samples.substrate.reamde.dev/tasks")
	if err != nil {
		t.Fatalf("import: %v %s", err, stderr)
	}
	// The id is a package reference and carries a `/`, so it rides as one
	// escaped segment; the recorded path is what the server decoded it back to.
	if want := "POST /api/v1/catalog/samples.substrate.reamde.dev/tasks/import"; !contains(h.fake.doorRequests(), want) {
		t.Errorf("import did not call %s: %v", want, h.fake.doorRequests())
	}
	if !strings.Contains(stdout, "geoah.example.com/tasks imported") {
		t.Errorf("import does not report where the sample landed: %s", stdout)
	}
	// AND what its suggested mappings did (decision record 0049): a reader who
	// installs Linear tomorrow has to be told that importing again is what
	// lands the rest.
	if !strings.Contains(stdout, "providers.substrate.reamde.dev/github/user -> geoah.example.com/tasks/person: landed") {
		t.Errorf("import does not report the mapping that landed, onto the kind that landed: %s", stdout)
	}
	want := "waiting; install providers.substrate.reamde.dev/linear, then import " +
		"samples.substrate.reamde.dev/tasks again. Re-importing replaces that " +
		"package and may remove your changes."
	if !strings.Contains(stdout, want) {
		t.Errorf("import does not say what the waiting mapping waits for and what a re-import costs: %s", stdout)
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// --as rehomes ONE authority. An input declaring under two is refused, naming
// both, rather than half-rewritten.
func TestApplyAsRefusesTwoAuthorities(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	file := filepath.Join(t.TempDir(), "two.yaml")
	doc := `kind: substrate.reamde.dev/core/kind
metadata:
  id: samples.substrate.reamde.dev/tasks/task
data:
  authority: samples.substrate.reamde.dev
  package: tasks
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: other.example.com/tasks/task
data:
  authority: other.example.com
  package: tasks
`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := h.run("apply", "-f", file, "--as", "ada.example.com")
	if err == nil {
		t.Fatal("apply --as rehomed an input declaring under two authorities")
	}
	for _, name := range []string{"other.example.com", "samples.substrate.reamde.dev"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the refusal does not name %s: %v", name, err)
		}
	}
}

// `--as` names where a closure lands, so it is held to what a REPOSITORY may
// own: the publisher's own name is where the shipped vocabulary lives, and the
// server refuses a repository claiming it. Saying so here names the flag that
// did it.
func TestApplyAsRefusesThePublisherAuthority(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	file := writeSampleFile(t)
	for _, authority := range []string{
		"substrate.reamde.dev",
		"samples.substrate.reamde.dev",
		"not an authority",
	} {
		_, _, err := h.run("apply", "-f", file, "--as", authority)
		if err == nil {
			t.Errorf("--as %s was accepted", authority)
			continue
		}
		if !strings.Contains(err.Error(), authority) {
			t.Errorf("the refusal of --as %s does not name it: %v", authority, err)
		}
	}
}

// `--as-mine` is the authority this context logged in with, so nobody retypes
// their own name to import a sample by hand.
func TestApplyAsMineUsesTheContextAuthority(t *testing.T) {
	h := newHarness(t)
	h.writeConfigAuthority("ada.example.com")
	file := writeSampleFile(t)
	if _, stderr, err := h.run("apply", "-f", file, "--as-mine"); err != nil {
		t.Fatalf("apply --as-mine: %v %s", err, stderr)
	}
	var sent []map[string]any
	if err := json.Unmarshal(h.fake.lastBody["documents"], &sent); err != nil {
		t.Fatalf("decode the applied documents: %v", err)
	}
	raw, err := json.Marshal(sent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "ada.example.com/tasks/task") {
		t.Errorf("--as-mine did not rehome onto the context authority: %s", raw)
	}
}

// A context written before the authority was stored has none, so the flag says
// what to pass instead of rehoming onto "".
func TestApplyAsMineRefusesAContextWithNoAuthority(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	file := writeSampleFile(t)
	_, _, err := h.run("apply", "-f", file, "--as-mine")
	if err == nil {
		t.Fatal("--as-mine rehomed with no authority to rehome onto")
	}
	if !strings.Contains(err.Error(), "--as") {
		t.Errorf("the refusal does not name what to pass instead: %v", err)
	}
}

// The rehome reaches a record document's LABELS and ANNOTATIONS, not its
// properties alone: a namespaced key names an actor, and an actor carries the
// authority. Asserted against the walk itself, since the point is what the
// rewrite touches rather than what the wire does with it.
func TestRehomeInputReachesLabelsAndAnnotations(t *testing.T) {
	declaration := map[string]any{
		"kind":     "substrate.reamde.dev/core/kind",
		"metadata": map[string]any{"id": "samples.substrate.reamde.dev/tasks/task"},
		"data":     map[string]any{"authority": "samples.substrate.reamde.dev", "package": "tasks"},
	}
	record := &document{
		Kind: "samples.substrate.reamde.dev/tasks/task",
		Metadata: documentMeta{
			ID:          "t9",
			Labels:      map[string]any{"bundle:samples.substrate.reamde.dev:tasks/source": "seed"},
			Annotations: map[string]any{"owner/note": "declared by samples.substrate.reamde.dev/tasks"},
		},
		Data: documentData{Properties: map[string]any{
			"source": "samples.substrate.reamde.dev/calendar/transcript/f81k",
		}},
	}
	docs := []*document{record}
	vocabularyDocs := []map[string]any{declaration}
	if _, err := rehomeInput(docs, vocabularyDocs, "ada.example.com"); err != nil {
		t.Fatalf("rehome: %v", err)
	}
	raw, err := json.Marshal([]any{vocabularyDocs, docs})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "samples.substrate.reamde.dev") {
		t.Errorf("the rehome left the placeholder behind: %s", raw)
	}
	if _, ok := record.Metadata.Labels["bundle:ada.example.com:tasks/source"]; !ok {
		t.Errorf("the label key was not rehomed: %v", record.Metadata.Labels)
	}
	note, _ := record.Metadata.Annotations["owner/note"].(string)
	if !strings.Contains(note, "ada.example.com/tasks") {
		t.Errorf("the annotation value was not rehomed: %q", note)
	}
	if record.Kind != "ada.example.com/tasks/task" {
		t.Errorf("the record kind was not rehomed: %q", record.Kind)
	}
}

// writeSampleFile is one shipped-sample-shaped closure on disk: enough for the
// rehome to have a single authored authority to find.
func writeSampleFile(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "tasks.yaml")
	doc := `kind: substrate.reamde.dev/core/kind
metadata:
  id: samples.substrate.reamde.dev/tasks/task
data:
  authority: samples.substrate.reamde.dev
  package: tasks
  version: 1
  names:
    singular: task
`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// writeConfigAuthority seeds a logged-in context that also recorded the
// repository's own authority, which is what `--as-mine` reads.
func (h *harness) writeConfigAuthority(authority string) {
	h.t.Helper()
	cfg := &Config{
		CurrentContext: "test",
		Contexts: []Context{{
			Name: "test", Server: h.server, Username: "geoah",
			Authority: authority,
			Token:     "substrate_tok_geoah_test", TokenID: "tk01",
		}},
	}
	if err := saveConfig(h.configPath, cfg); err != nil {
		h.t.Fatalf("seed config: %v", err)
	}
}
