package catalog_test

// The upgrade preview against a REAL engine: a shipped closure that moved is
// reported (which declarations, which versions), one that did not is not, and
// a closure the install door would refuse carries the door's own guard lines
// as blockers: the preview and the refusal can never disagree because they
// run the same staging and the same counts (engine PlanBundleUpgrade).

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

const (
	tasksBundleID = "samples.substrate.reamde.dev/tasks"
	// tasksPackage is the package's own word, which is also its directory in
	// the samples root.
	tasksPackage = "tasks"
)

// tasksRequires is what the tasks closure declares against: its assignee edge
// lands on person and its task/tasklog bind the scheduling traits, so people
// and scheduling are imported first, exactly as the console would have to.
var tasksRequires = []string{
	"samples.substrate.reamde.dev/people",
	"samples.substrate.reamde.dev/scheduling",
}

// movedTasksCatalog loads a catalog whose tasks closure is the shipped one
// with each named file rewritten: binary N+1's tree, against a repository
// binary N installed into. Passing no mutation loads the shipped closure
// unchanged, which is binary N's own tree.
//
// The copy is loaded as a PROVIDER root: the provider preview is what these
// tests hold, while the closure it is made of stays the tasks closure the
// tree ships. movedTasksSampleCatalog loads the same copy as the sample it
// is.
func movedTasksCatalog(t *testing.T, rewrite map[string]func(string) string) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load(catalog.ProviderRoot(os.DirFS(movedTasksRoot(t, rewrite))))
	if err != nil {
		t.Fatalf("load moved catalog: %v", err)
	}
	return c
}

// movedTasksSampleCatalog is movedTasksCatalog under the SAMPLE tier: binary
// N+1's samples tree, which is what a copy imported from binary N is
// previewed against (decision record 0070).
func movedTasksSampleCatalog(t *testing.T, rewrite map[string]func(string) string) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load(catalog.SampleRoot(os.DirFS(movedTasksRoot(t, rewrite))))
	if err != nil {
		t.Fatalf("load moved sample catalog: %v", err)
	}
	return c
}

// movedTasksRoot writes the copied samples tree the two loaders read.
func movedTasksRoot(t *testing.T, rewrite map[string]func(string) string) string {
	t.Helper()
	// The copy mirrors the samples root: the authority manifest at the root,
	// the tasks package directory beside it.
	samplesRoot := filepath.Join("..", "..", "samples")
	root := t.TempDir()
	dst := filepath.Join(root, tasksPackage)
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	copyManifests := func(src, dstDir string) {
		t.Helper()
		entries, err := os.ReadDir(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", e.Name(), err)
			}
			doc := string(raw)
			if m, ok := rewrite[e.Name()]; ok {
				doc = m(doc)
			}
			if err := os.WriteFile(filepath.Join(dstDir, e.Name()), []byte(doc), 0o600); err != nil {
				t.Fatalf("write %s: %v", e.Name(), err)
			}
		}
	}
	copyManifests(samplesRoot, root)
	copyManifests(filepath.Join(samplesRoot, tasksPackage), dst)
	return root
}

// mustReplace is strings.Replace that fails the test when the needle is
// gone: a shipped file that drifted must fail loudly, not silently test
// nothing.
func mustReplace(t *testing.T, doc, from, to string) string {
	t.Helper()
	if !strings.Contains(doc, from) {
		t.Fatalf("shipped tasks file no longer contains %q", from)
	}
	return strings.Replace(doc, from, to, 1)
}

// shippedVersion is the tasks package's version in the tree right now: the
// tests bump PAST whatever it stands at, so a future bump of the shipped
// closure never turns these into a puzzle.
func shippedVersion(t *testing.T, c *catalog.Catalog) int64 {
	t.Helper()
	b, ok := c.ByID(tasksBundleID)
	if !ok {
		t.Fatal("the shipped catalog no longer carries the tasks bundle")
	}
	if b.Version == 0 {
		t.Fatal("the shipped tasks package declares no version")
	}
	return b.Version
}

// movedVersion is a version that outranks every spelling the tree could hold:
// binary N+1, whatever N was.
const movedVersion = int64(99)

// taskPin is the task kind's own version pin: the one `version:` key at the
// data level of task.yaml.
var taskPin = regexp.MustCompile(`(?m)^  version: \d+$`)

// bumpTaskPin moves the task kind's own version pin to movedVersion. The kind
// pins its version (a one-kind change bumps the kind, not the closure), so a
// moved closure that edits the task declaration moves the pin with it, exactly
// as the tree would.
func bumpTaskPin(t *testing.T, doc string) string {
	t.Helper()
	if !taskPin.MatchString(doc) {
		t.Fatal("shipped task.yaml no longer pins its own version")
	}
	return taskPin.ReplaceAllString(doc, "  version: "+strconv.FormatInt(movedVersion, 10))
}

// bumpTasksPackage moves the version on the package document heading the
// tasks closure, which is the closure-wide version the preview reads.
func bumpTasksPackage(t *testing.T, from int64, doc string) string {
	t.Helper()
	return mustReplace(t, doc,
		"version: "+strconv.FormatInt(from, 10),
		"version: "+strconv.FormatInt(movedVersion, 10))
}

func TestUpgradePreview(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	current := movedTasksCatalog(t, nil)

	// Not installed: nothing to upgrade, only to install.
	up, err := current.Upgrade(ctx, tasksBundleID, ds, nil)
	if err != nil {
		t.Fatalf("preview before install: %v", err)
	}
	if up == nil || up.Available {
		t.Fatalf("an uninstalled bundle previews an upgrade: %+v", up)
	}

	importVocabulary(t, c, ds, append(tasksRequires, tasksBundleID)...)

	// Installed and current: the shipped closure moves nothing.
	up, err = current.Upgrade(ctx, tasksBundleID, ds, nil)
	if err != nil {
		t.Fatalf("preview after install: %v", err)
	}
	if up == nil || up.Available || len(up.Changes) != 0 {
		t.Fatalf("an up-to-date bundle previews an upgrade: %+v", up)
	}

	// A SAMPLE is previewed only through the copy's origin stamp (decision
	// record 0070): with no held copy to read one off, the catalog answers no
	// preview at all before the dataset is asked. The verbatim install above
	// stamped nothing, so a status for the shipped id is such a copy.
	sample, err := c.Upgrade(ctx, tasksBundleID, ds, &substrate.BundleStatus{ID: tasksBundleID, Installed: true})
	if err != nil {
		t.Fatalf("preview of a sample: %v", err)
	}
	if sample != nil {
		t.Errorf("a sample held without an origin stamp previews an upgrade: %+v", sample)
	}

	// Binary N+1 ships the package a version ahead with a new optional
	// property: an additive move, offered with no blockers.
	shipped := shippedVersion(t, current)
	moved := movedTasksCatalog(t, map[string]func(string) string{
		"bundle.yaml": func(doc string) string { return bumpTasksPackage(t, shipped, doc) },
		"task.yaml": func(doc string) string {
			// The kind pins its own version, so the package bump alone
			// would not move it: the pin moves with the change.
			doc = bumpTaskPin(t, doc)
			return mustReplace(t, doc, "    status:",
				"    upgradeProbe:\n      type: string\n      description: a property this binary added\n    status:")
		},
	})
	up, err = moved.Upgrade(ctx, tasksBundleID, ds, nil)
	if err != nil {
		t.Fatalf("preview of the moved closure: %v", err)
	}
	if up == nil || !up.Available {
		t.Fatalf("a moved closure previews no upgrade: %+v", up)
	}
	if up.From != shipped || up.To != movedVersion {
		t.Errorf("package motion reads %d -> %d, want %d -> %d", up.From, up.To, shipped, movedVersion)
	}
	if len(up.Blockers) != 0 {
		t.Errorf("an additive upgrade carries blockers: %v", up.Blockers)
	}
	movedKinds := map[string]bool{}
	for _, ch := range up.Changes {
		if ch.Kind == "kind" {
			movedKinds[ch.ID] = true
		}
	}
	if !movedKinds["samples.substrate.reamde.dev/tasks/task"] {
		t.Errorf("the changed kind is not among the moves: %+v", up.Changes)
	}

	// The offered upgrade is the install verb, unchanged, and afterwards the
	// preview reads current again.
	if _, _, err := moved.Install(ctx, substrate.ActorAPI, tasksBundleID, ds); err != nil {
		t.Fatalf("install the moved closure: %v", err)
	}
	up, err = moved.Upgrade(ctx, tasksBundleID, ds, nil)
	if err != nil {
		t.Fatalf("preview after upgrade: %v", err)
	}
	if up == nil || up.Available {
		t.Fatalf("an upgraded bundle still previews an upgrade: %+v", up)
	}
}

func TestUpgradePreviewReportsBlockers(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	importVocabulary(t, c, ds, append(tasksRequires, tasksBundleID)...)

	// A live row holding the very property binary N+1 drops.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/tasks/task", ID: "guarded",
		Properties: map[string]any{"url": "https://example.com/issue/1"},
	}); err != nil {
		t.Fatalf("put the live task row: %v", err)
	}

	shipped := shippedVersion(t, c)
	const urlDecl = "    url:\n      type: url\n      description: where this task lives outside the substrate\n"
	const nameDecl = "    name:\n      type: string\n      description: the task's heading, one line\n"
	// Binary N+1 drops `url` and makes `name` required: the drop is a lossy
	// null step over the live row (decision 0067), the requirement a
	// narrowing no step covers, so the preview reports both the blocker and
	// the plan.
	blocked := movedTasksCatalog(t, map[string]func(string) string{
		"bundle.yaml": func(doc string) string { return bumpTasksPackage(t, shipped, doc) },
		"task.yaml": func(doc string) string {
			doc = bumpTaskPin(t, doc)
			doc = mustReplace(t, doc, urlDecl, "")
			return mustReplace(t, doc, nameDecl,
				"    name:\n      type: string\n      required: true\n      description: the task's heading, one line\n")
		},
	})
	up, err := blocked.Upgrade(ctx, tasksBundleID, ds, nil)
	if err != nil {
		t.Fatalf("preview of the narrowing closure: %v", err)
	}
	if up == nil || !up.Available {
		t.Fatalf("a moved closure previews no upgrade: %+v", up)
	}
	if len(up.Blockers) != 1 || !strings.Contains(up.Blockers[0], `"name"`) || !strings.Contains(up.Blockers[0], "1 live record") {
		t.Fatalf("the narrowing is not the one blocker naming the property and the count: %v", up.Blockers)
	}
	if !up.Lossy || len(up.Steps) != 1 || up.Steps[0].Step != substrate.StepNull || up.Steps[0].Property != "url" || up.Steps[0].Records != 1 {
		t.Fatalf("the preview does not plan the null step beside the blocker: %+v", up.ConversionPlan)
	}
	// The preview's blocker IS the install door's refusal.
	if _, _, err := blocked.Install(ctx, substrate.ActorAPI, tasksBundleID, ds); err == nil {
		t.Fatal("the install door admitted the closure the preview reported blocked")
	} else if !strings.Contains(err.Error(), `"name"`) {
		t.Errorf("the refusal does not name the property the preview named: %v", err)
	}

	// Dropping `url` alone blocks nothing: the plan is lossy, the door
	// refuses it without the previewed pair, naming the property, and lands
	// it with them. Nothing is renamed, so the legacy list stays empty.
	lossy := movedTasksCatalog(t, map[string]func(string) string{
		"bundle.yaml": func(doc string) string { return bumpTasksPackage(t, shipped, doc) },
		"task.yaml": func(doc string) string {
			return mustReplace(t, bumpTaskPin(t, doc), urlDecl, "")
		},
	})
	if up, err = lossy.Upgrade(ctx, tasksBundleID, ds, nil); err != nil {
		t.Fatalf("preview of the lossy closure: %v", err)
	}
	if up == nil || !up.Available || len(up.Blockers) != 0 || !up.Lossy || up.Work != 1 || up.PlanHash == "" {
		t.Fatalf("a lossy drop previews as %+v", up)
	}
	if _, _, err := lossy.Install(ctx, substrate.ActorAPI, tasksBundleID, ds); err == nil {
		t.Fatal("the install door admitted a lossy plan without a confirmation")
	} else if !errors.Is(err, substrate.ErrLossyConversion) || !strings.Contains(err.Error(), `"url"`) {
		t.Errorf("the refusal is not the lossy one naming the property: %v", err)
	}
	confirm := &substrate.ConversionConfirm{PlanHash: up.PlanHash, ChangelogSeq: up.ChangelogSeq}
	if _, _, err := lossy.InstallConfirmed(ctx, substrate.ActorAPI, tasksBundleID, ds, confirm); err != nil {
		t.Fatalf("the confirmed install must land: %v", err)
	}
	got, err := ds.Get(ctx, "samples.substrate.reamde.dev/tasks/task", "guarded")
	if err != nil {
		t.Fatalf("get the converted row: %v", err)
	}
	if got.Properties["url"] != nil {
		t.Fatalf("the null step did not remove the value: %v", got.Properties)
	}
}

// handApplyTasks applies the tasks closure the way `substratectl apply -f`
// of the shipped files would, each named file rewritten first: the hand door,
// which lands the package `installed` and lets the API move its versions.
// The suggested Linear mapping stays out, with its `installs:` entry, because
// Linear is not installed.
func handApplyTasks(t *testing.T, ds substrate.Dataset, rewrite map[string]func(string) string) {
	t.Helper()
	root := movedTasksRoot(t, rewrite)
	var docs []map[string]any
	for _, file := range []string{
		"authority.yaml",
		filepath.Join(tasksPackage, "bundle.yaml"),
		filepath.Join(tasksPackage, "project.yaml"),
		filepath.Join(tasksPackage, "task.yaml"),
		filepath.Join(tasksPackage, "tasklog.yaml"),
	} {
		raw, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		text := linearInstall.ReplaceAllString(string(raw), "")
		dec := yaml.NewDecoder(strings.NewReader(text))
		for {
			var d map[string]any
			if err := dec.Decode(&d); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("decode %s: %v", file, err)
			}
			if len(d) > 0 {
				docs = append(docs, d)
			}
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI, docs); err != nil {
		t.Fatalf("hand apply: %v", err)
	}
}

// linearInstall is the `installs:` entry of the suggested Linear mapping.
var linearInstall = regexp.MustCompile(`(?m)^ +- [^\n]*/linearissuetask\n`)

// setPackageVersion rewrites the version on the package document heading the
// tasks closure.
func setPackageVersion(t *testing.T, doc string, from, to int64) string {
	t.Helper()
	return mustReplace(t, doc, "version: "+strconv.FormatInt(from, 10), "version: "+strconv.FormatInt(to, 10))
}

// An install over a package whose stored version ran ahead of the shipped one
// (hand applies before the provider was installed) lands at stored+1, as
// every changed declaration does through the API. What it must not do is hide
// the next shipped versions: the install records the shipped version it took,
// and the preview measures the next closure from that, so shipped S+1 is
// offered even though the stored package already reads S+3 (issue #642).
func TestInstallOverAHigherStoredVersionIsOfferedTheNextShipped(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	importVocabulary(t, loadCatalog(t), ds, tasksRequires...)
	current := movedTasksCatalog(t, nil)
	shipped := shippedVersion(t, current)
	b, _ := current.ByID(tasksBundleID)

	// The hand applies: the header two versions past the shipped one, and the
	// bundle document edited, so the shipped closure differs from the rows.
	handApplyTasks(t, ds, map[string]func(string) string{
		"bundle.yaml": func(doc string) string {
			doc = setPackageVersion(t, doc, shipped, shipped+2)
			return mustReplace(t, doc, "Things to do and", "Things to do, edited by hand, and")
		},
	})
	if got := closureVersions(t, ds, b)["package "+tasksBundleID]; got != shipped+2 {
		t.Fatalf("the hand apply landed the package at %d, want %d", got, shipped+2)
	}

	// The install of binary N's closure: the bundle document changed back, so
	// the package moves past what the store holds, never back to shipped.
	if _, _, err := current.Install(ctx, substrate.ActorAPI, tasksBundleID, ds); err != nil {
		t.Fatalf("install over the hand-applied package: %v", err)
	}
	installed := closureVersions(t, ds, b)["package "+tasksBundleID]
	if installed != shipped+3 {
		t.Fatalf("the install landed the package at %d, want stored+1 = %d", installed, shipped+3)
	}
	up, err := current.Upgrade(ctx, tasksBundleID, ds, nil)
	if err != nil {
		t.Fatalf("preview after install: %v", err)
	}
	if up == nil || up.Available {
		t.Fatalf("the closure just installed previews an upgrade: %+v", up)
	}

	// Binary N+1 ships the package at shipped+1 with an additive property on
	// a kind that rides the package version: below what the store holds, and
	// still the next closure this install has not taken.
	next := movedTasksCatalog(t, map[string]func(string) string{
		"bundle.yaml": func(doc string) string { return setPackageVersion(t, doc, shipped, shipped+1) },
		"project.yaml": func(doc string) string {
			return mustReplace(t, doc, "  properties:\n",
				"  properties:\n    code:\n      type: string\n      description: a property the next binary added\n")
		},
	})
	up, err = next.Upgrade(ctx, tasksBundleID, ds, nil)
	if err != nil {
		t.Fatalf("preview of the next closure: %v", err)
	}
	if up == nil || !up.Available {
		t.Fatalf("the next shipped closure is not offered over a stored version ahead of it: %+v", up)
	}
	if up.From != shipped || up.To != shipped+1 {
		t.Errorf("package motion reads %d -> %d, want %d -> %d", up.From, up.To, shipped, shipped+1)
	}
	if len(up.Blockers) != 0 {
		t.Errorf("an additive upgrade carries blockers: %v", up.Blockers)
	}
	// The stored versions ran ahead, so the moves are read by content: the
	// changed kind is listed at the version the install lands it at.
	var projectChange *substrate.BundleUpgradeChange
	for i, ch := range up.Changes {
		if ch.Kind == "kind" && ch.ID == "samples.substrate.reamde.dev/tasks/project" {
			projectChange = &up.Changes[i]
		}
	}
	if projectChange == nil {
		t.Fatalf("the preview does not name the changed kind: %+v", up.Changes)
	}

	// Taking it lands the property, the changed kind's version still only
	// rises (stored+1), the package's never falls back, and the preview
	// reads current again.
	before := closureVersions(t, ds, b)
	if _, _, err := next.Install(ctx, substrate.ActorAPI, tasksBundleID, ds); err != nil {
		t.Fatalf("install the next closure: %v", err)
	}
	after := closureVersions(t, ds, b)
	const project = "kind samples.substrate.reamde.dev/tasks/project"
	if after[project] != before[project]+1 {
		t.Errorf("the upgrade landed the changed kind at %d, want stored+1 = %d", after[project], before[project]+1)
	}
	if projectChange.From != before[project] || projectChange.To != after[project] {
		t.Errorf("the preview read the kind's move as %d -> %d, the install moved it %d -> %d",
			projectChange.From, projectChange.To, before[project], after[project])
	}
	if got := after["package "+tasksBundleID]; got < installed {
		t.Errorf("the upgrade moved the package back to %d from %d", got, installed)
	}
	k, err := ds.KindByRef(ctx, "samples.substrate.reamde.dev/tasks/project")
	if err != nil {
		t.Fatalf("project kind: %v", err)
	}
	if props, _ := k.Definition["properties"].(map[string]any); props["code"] == nil {
		t.Errorf("the upgrade did not land the added property: %v", k.Definition["properties"])
	}
	if up, err = next.Upgrade(ctx, tasksBundleID, ds, nil); err != nil {
		t.Fatalf("preview after the upgrade: %v", err)
	}
	if up == nil || up.Available {
		t.Fatalf("an upgraded bundle still previews an upgrade: %+v", up)
	}
}
