package catalog_test

// The upgrade preview against a REAL engine: a shipped closure that moved is
// reported (which declarations, which versions), one that did not is not, and
// a closure the install door would refuse carries the door's own guard lines
// as blockers: the preview and the refusal can never disagree because they
// run the same staging and the same counts (engine PlanBundleUpgrade).

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

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
// binary N installed into.
func movedTasksCatalog(t *testing.T, mutate map[string]func(string) string) *catalog.Catalog {
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
			if m, ok := mutate[e.Name()]; ok {
				doc = m(doc)
			}
			if err := os.WriteFile(filepath.Join(dstDir, e.Name()), []byte(doc), 0o600); err != nil {
				t.Fatalf("write %s: %v", e.Name(), err)
			}
		}
	}
	copyManifests(samplesRoot, root)
	copyManifests(filepath.Join(samplesRoot, tasksPackage), dst)
	c, err := catalog.Load(os.DirFS(root))
	if err != nil {
		t.Fatalf("load moved catalog: %v", err)
	}
	return c
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

	// Not installed: nothing to upgrade, only to install.
	up, err := c.Upgrade(ctx, tasksBundleID, ds)
	if err != nil {
		t.Fatalf("preview before install: %v", err)
	}
	if up == nil || up.Available {
		t.Fatalf("an uninstalled bundle previews an upgrade: %+v", up)
	}

	importVocabulary(t, c, ds, append(tasksRequires, tasksBundleID)...)

	// Installed and current: the shipped closure moves nothing.
	up, err = c.Upgrade(ctx, tasksBundleID, ds)
	if err != nil {
		t.Fatalf("preview after install: %v", err)
	}
	if up == nil || up.Available || len(up.Changes) != 0 {
		t.Fatalf("an up-to-date bundle previews an upgrade: %+v", up)
	}

	// Binary N+1 ships the package a version ahead with a new optional
	// property: an additive move, offered with no blockers.
	shipped := shippedVersion(t, c)
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
	up, err = moved.Upgrade(ctx, tasksBundleID, ds)
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
	if _, err := moved.Install(ctx, substrate.ActorAPI, tasksBundleID, ds); err != nil {
		t.Fatalf("install the moved closure: %v", err)
	}
	up, err = moved.Upgrade(ctx, tasksBundleID, ds)
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
	moved := movedTasksCatalog(t, map[string]func(string) string{
		"bundle.yaml": func(doc string) string { return bumpTasksPackage(t, shipped, doc) },
		"task.yaml": func(doc string) string {
			doc = bumpTaskPin(t, doc)
			return mustReplace(t, doc,
				"    url:\n      type: url\n      description: where this task lives outside the substrate\n", "")
		},
	})
	up, err := moved.Upgrade(ctx, tasksBundleID, ds)
	if err != nil {
		t.Fatalf("preview of the narrowing closure: %v", err)
	}
	if up == nil || !up.Available {
		t.Fatalf("a moved closure previews no upgrade: %+v", up)
	}
	if len(up.Blockers) == 0 {
		t.Fatal("a narrowing upgrade previews no blockers")
	}
	if !strings.Contains(up.Blockers[0], `"url"`) || !strings.Contains(up.Blockers[0], "1 live record") {
		t.Errorf("the blocker does not name the property and the count: %v", up.Blockers)
	}

	// The preview's blocker IS the install door's refusal.
	if _, err := moved.Install(ctx, substrate.ActorAPI, tasksBundleID, ds); err == nil {
		t.Fatal("the install door admitted the closure the preview reported blocked")
	} else if !strings.Contains(err.Error(), `"url"`) {
		t.Errorf("the refusal does not name the property the preview named: %v", err)
	}
}
