package catalog

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/kinds"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The scopes the first-party apps need for the packages they ask to have
// set up (decision 0215), held to the closures: what each package and its
// requirements reach, every entry covered by the app's scopes, with an
// internal kind covered by its exact scope alone. A closure that comes to
// reach more fails here before it fails on the owner's approval page.
//
// Nothing here is a hand copy. What each app asks for is its
// apps/<app>/app.json, which a Swift test on each side holds equal to the
// app's manifest; the binary's packages come from the catalog it ships,
// and Workouts' published packages from the closures the app carries
// (Resources/Package/*.json, the documents the site publishes), parsed as
// the server parses a published closure.

// appRequest is what one app's app.json says it asks a substrate for, and
// the packages its own site publishes.
type appRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	Publishes []string `json:"publishes"`
	Asks      []struct {
		Package string `json:"package"`
		AtLeast int64  `json:"atLeast"`
	} `json:"asks"`
}

// firstPartyRequests reads every app's app.json and keeps the apps that ask
// for packages to be set up, by name: an app that asks for none has no
// closure to hold its scopes to. It answers too every package an app
// publishes, with the app's folder.
func firstPartyRequests(t *testing.T) (map[string]appRequest, map[string]string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "apps", "*", "app.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("find the apps' app.json: %v (%d found)", err, len(files))
	}
	out := map[string]appRequest{}
	published := map[string]string{}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read the app's request: %v", err)
		}
		var app appRequest
		if err := json.Unmarshal(raw, &app); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if app.Name == "" {
			t.Fatalf("%s names no app", file)
		}
		for _, id := range app.Publishes {
			published[id] = filepath.Base(filepath.Dir(file))
		}
		if len(app.Asks) > 0 {
			out[app.Name] = app
		}
	}
	for _, name := range []string{"Substrate Sync", "Substrate Workouts"} {
		if _, ok := out[name]; !ok {
			t.Fatalf("no app.json asks for %s's packages", name)
		}
	}
	return out, published
}

// workoutsClosure parses one of the closures Substrate Workouts carries as
// the server parses a published closure: the file's documents, in the order
// the site publishes them, as one YAML stream.
func workoutsClosure(t *testing.T, name, id string) *Bundle {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "apps", "workouts", "WorkoutsKit", "Sources", "WorkoutsCore", "Resources", "Package", name+".json"))
	if err != nil {
		t.Fatalf("read the %s closure: %v", id, err)
	}
	var file struct {
		Package   string            `json:"package"`
		Version   int64             `json:"version"`
		Documents []json.RawMessage `json:"documents"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("%s.json: %v", name, err)
	}
	if file.Package != id {
		t.Fatalf("%s.json is %s, not %s", name, file.Package, id)
	}
	var stream bytes.Buffer
	for i, doc := range file.Documents {
		if i > 0 {
			stream.WriteString("\n---\n")
		}
		stream.Write(doc)
	}
	b, err := ParseClosure(stream.Bytes(), id, file.Version)
	if err != nil {
		t.Fatalf("parse the %s closure as the server does: %v", id, err)
	}
	return b
}

// folderClosure parses the closure of a package an app publishes straight
// from its package/ folder, built as .mise/closures.py builds it for the
// site: the file declaring the package, then the file declaring each entry
// of its bundle's installs, in that order, as one YAML stream.
func folderClosure(t *testing.T, app, id string) *Bundle {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "apps", app, "package", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("find %s's package files: %v (%d found)", app, err, len(files))
	}
	raw := map[string][]byte{}
	declares := map[string]string{}
	var header string
	var version int64
	var installs []string
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		raw[file] = body
		docs, err := vocabulary.ParseStream(body)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, doc := range docs {
			switch {
			case doc.Kind == vocabulary.DocPackage && doc.ID == id:
				header = file
				v, ok := doc.Data["version"].(int)
				if !ok {
					t.Fatalf("%s: %s's version is not an integer", file, id)
				}
				version = int64(v)
			case doc.Kind == vocabulary.DocBundle && doc.ID == id:
				list, _ := doc.Data["installs"].([]any)
				for _, entry := range list {
					name, _ := entry.(string)
					installs = append(installs, name)
				}
			case doc.Kind != vocabulary.DocPackage && doc.Kind != vocabulary.DocBundle:
				declares[doc.ID] = file
			}
		}
	}
	if header == "" {
		t.Fatalf("no file under apps/%s/package declares %s", app, id)
	}
	stream := append([]byte{}, raw[header]...)
	for _, entry := range installs {
		file, ok := declares[entry]
		if !ok {
			t.Fatalf("%s's bundle installs %s, which no file declares", id, entry)
		}
		stream = append(stream, "\n---\n"...)
		stream = append(stream, raw[file]...)
	}
	b, err := ParseClosure(stream, id, version)
	if err != nil {
		t.Fatalf("parse the %s closure as the server does: %v", id, err)
	}
	return b
}

// shippedPurposes is the declared purpose of every kind the binary ships,
// the seeded packages included.
func shippedPurposes(t *testing.T, cat *Catalog) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, b := range cat.Bundles() {
		for _, k := range b.Kinds() {
			out[k.Identity] = k.Purpose
		}
	}
	err := fs.WalkDir(kinds.Seed(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return err
		}
		raw, err := fs.ReadFile(kinds.Seed(), path)
		if err != nil {
			return err
		}
		docs, err := vocabulary.ParseStream(raw)
		if err != nil {
			return err
		}
		for _, doc := range docs {
			if doc.Kind == vocabulary.DocKind {
				out[doc.ID], _ = doc.Data["purpose"].(string)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the seed: %v", err)
	}
	return out
}

// uncovered is every entry of reach the scopes do not cover.
func uncovered(t *testing.T, scopes []string, purposes map[string]string, reach []Reach) []string {
	t.Helper()
	approved, err := scope.ApprovalOf(scopes)
	if err != nil {
		t.Fatal(err)
	}
	var out, internals []string
	for ref, purpose := range purposes {
		if purpose == vocabulary.PurposeInternal {
			internals = append(internals, ref)
		}
	}
	for _, r := range reach {
		purpose, known := purposes[r.Pattern]
		internal := !vocabulary.IsTypeGlob(r.Pattern) && (!known || purpose == vocabulary.PurposeInternal)
		if !approved.Covers(r.Pattern, r.Write, internal, internals...) {
			out = append(out, scope.Covering(r.Pattern, r.Write))
		}
	}
	return out
}

func TestTheFirstPartyAppsScopesCoverThePackagesTheyAskFor(t *testing.T) {
	cat, err := Load(ProviderRoot(kinds.Bundles()))
	if err != nil {
		t.Fatal(err)
	}
	requests, publishes := firstPartyRequests(t)
	// Workouts' closures are the generated ones the app carries; every other
	// app's (Tasks', and each one `mise run apps:new` makes) is read from its
	// package/ folder as the site builds it.
	published := map[string]*Bundle{
		"substrate.reamde.dev/training":      workoutsClosure(t, "training", "substrate.reamde.dev/training"),
		"substrate.reamde.dev/trainingcoach": workoutsClosure(t, "coach", "substrate.reamde.dev/trainingcoach"),
	}
	for id, app := range publishes {
		if _, ok := published[id]; !ok {
			published[id] = folderClosure(t, app, id)
		}
	}
	if _, ok := published["substrate.reamde.dev/tasks"]; !ok {
		t.Fatal("no app.json publishes substrate.reamde.dev/tasks")
	}
	purposes := shippedPurposes(t, cat)
	for _, b := range published {
		for _, k := range b.Kinds() {
			purposes[k.Identity] = k.Purpose
		}
	}
	// The coach's reach is what the test is for: an agent with none would
	// hold the scopes to nothing.
	if coach := published["substrate.reamde.dev/trainingcoach"]; len(coach.Agents()) != 1 || len(coach.Reach()) == 0 {
		t.Fatalf("the trainingcoach closure ships %d agents reaching %d patterns", len(coach.Agents()), len(coach.Reach()))
	}
	var closure func(id string) []Reach
	closure = func(id string) []Reach {
		b, ok := published[id]
		if !ok {
			b, ok = cat.ByID(id)
		}
		if !ok {
			t.Fatalf("neither the catalog nor the app ships %s", id)
		}
		var out []Reach
		for _, req := range b.Requires {
			out = append(out, closure(req)...)
		}
		return append(out, b.Reach()...)
	}
	// The closures read here as an app's own are the ones the apps say
	// their sites publish, no more and no fewer.
	var carried, said []string
	for id := range published {
		carried = append(carried, id)
	}
	for id := range publishes {
		said = append(said, id)
	}
	slices.Sort(carried)
	slices.Sort(said)
	if !slices.Equal(carried, said) {
		t.Fatalf("the apps publish %v, and this test reads the closures of %v", said, carried)
	}
	for name, c := range requests {
		t.Run(name, func(t *testing.T) {
			var reach []Reach
			for _, ask := range c.Asks {
				b, ok := published[ask.Package]
				if !ok {
					b, ok = cat.ByID(ask.Package)
				}
				if !ok {
					t.Fatalf("neither the catalog nor the app ships %s", ask.Package)
				}
				// A floor past every version there is leaves the approval
				// nothing to install.
				if ask.AtLeast < 1 || ask.AtLeast > b.Version {
					t.Fatalf("%s asks for %s at %d or later, and the version there is %d", name, ask.Package, ask.AtLeast, b.Version)
				}
				reach = append(reach, closure(ask.Package)...)
				// An app writes the owner's kinds of a package it publishes
				// itself, code or none: Tasks' package is kinds alone. An
				// internal kind is the app's own machinery, held by the
				// app's own scopes where it writes one.
				if slices.Contains(c.Publishes, ask.Package) {
					for _, k := range b.Kinds() {
						if k.Purpose == vocabulary.PurposeInternal {
							continue
						}
						reach = append(reach, Reach{Pattern: k.Identity, Write: true})
					}
				}
			}
			if len(reach) == 0 {
				t.Fatalf("%s's packages reach nothing", name)
			}
			if missing := uncovered(t, c.Scopes, purposes, reach); len(missing) > 0 {
				t.Fatalf("%s's scopes leave uncovered: %s", name, strings.Join(missing, ", "))
			}
		})
	}
	// Each exact scope in Sync's list is there because a glob does not
	// reach the internal kind it names: without it, that kind alone is
	// uncovered, read and written.
	syncScopes := requests["Substrate Sync"].Scopes
	for _, kind := range []string{"providers.substrate.reamde.dev/applehealth/account", "providers.substrate.reamde.dev/applehealth/export"} {
		var fewer []string
		for _, held := range syncScopes {
			if held != "write:"+kind {
				fewer = append(fewer, held)
			}
		}
		if len(fewer) == len(syncScopes) {
			t.Fatalf("Sync's request no longer names write:%s", kind)
		}
		missing := uncovered(t, fewer, purposes, closure("providers.substrate.reamde.dev/applehealth"))
		if len(missing) == 0 {
			t.Fatalf("without write:%s, Sync covers everything, so the exact scope is not needed", kind)
		}
		for _, m := range missing {
			if !strings.HasSuffix(m, ":"+kind) {
				t.Fatalf("without write:%s, Sync leaves uncovered %v", kind, missing)
			}
		}
	}
	// Workouts' observations glob is what covers the coach's observation
	// reads: without it they are uncovered, so the test is live on that side.
	var fewer []string
	for _, held := range requests["Substrate Workouts"].Scopes {
		if held != "read:substrate.reamde.dev/observations/*" {
			fewer = append(fewer, held)
		}
	}
	missing := uncovered(t, fewer, purposes, closure("substrate.reamde.dev/trainingcoach"))
	if len(missing) == 0 {
		t.Fatal("without read:substrate.reamde.dev/observations/*, Workouts covers everything, so the coach reads no observation kind")
	}
	for _, m := range missing {
		if !strings.HasPrefix(m, "read:substrate.reamde.dev/observations/") {
			t.Fatalf("without the observations glob, Workouts leaves uncovered %v", missing)
		}
	}
}
