package engine_test

// Issue 118: core's `trigger` titles itself `{label|callable}`. Several
// triggers may call one function, and under `{callable}` alone every one of
// them listed as the same function reference. A trigger with a label lists
// under it; one without keeps the callable, so no stored row changes meaning.

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// mailSyncFn is the one function every trigger here calls.
const mailSyncFn = fnPackage + "/mailsync"

// mailSyncDoc declares mailSyncFn: a body that does nothing, since no test
// here fires a trigger.
func mailSyncDoc() map[string]any {
	return pyFn("mailsync", map[string]any{}, []any{widgetType}, "def main(input, host):\n    return {}\n")
}

// triggerTitles lists the live triggers by id and title, through the same
// records read every list surface uses.
func triggerTitles(t *testing.T, ds substrate.Dataset) map[string]string {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{corePackage + "/trigger"}},
		First:  100,
	})
	if err != nil {
		t.Fatalf("list triggers: %v", err)
	}
	out := map[string]string{}
	for _, r := range page.Records {
		out[r.ID] = r.Title
	}
	return out
}

func TestATriggerTitlesItselfFromItsLabel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newFnDataset(t, nil, mailSyncDoc())
	callable := vocabulary.RecordPath("substrate.reamde.dev/core/function", mailSyncFn)
	source := map[string]any{"webhook": map[string]any{}}
	trigger := func(id string, extra map[string]any) *substrate.Record {
		props := map[string]any{"enabled": false, "source": source, "callable": callable}
		for k, v := range extra {
			props[k] = v
		}
		return mustPut(t, ds, owner, substrate.PutInput{Kind: corePackage + "/trigger", ID: id, Properties: props})
	}

	if got := trigger("nightly", map[string]any{"label": "Nightly mail sync"}).Title; got != "Nightly mail sync" {
		t.Fatalf("a labeled trigger is titled %q, want its label", got)
	}
	// The function's own title is its reference, which is what `{callable}`
	// rendered before the label existed.
	if got := trigger("unlabeled", nil).Title; got != mailSyncFn {
		t.Fatalf("a trigger without a label is titled %q, want the callable %q", got, mailSyncFn)
	}
	// An empty or whitespace-only label is no label: the title falls back to
	// the callable rather than going blank.
	if got := trigger("blank", map[string]any{"label": ""}).Title; got != mailSyncFn {
		t.Fatalf("a trigger with an empty label is titled %q, want the callable %q", got, mailSyncFn)
	}
	if got := trigger("spaces", map[string]any{"label": "   "}).Title; got != mailSyncFn {
		t.Fatalf("a trigger with a whitespace label is titled %q, want the callable %q", got, mailSyncFn)
	}
	want := map[string]string{
		"nightly": "Nightly mail sync", "unlabeled": mailSyncFn, "blank": mailSyncFn, "spaces": mailSyncFn,
	}
	got := triggerTitles(t, ds)
	if len(got) != len(want) {
		t.Fatalf("the trigger list reads %v, want %v", got, want)
	}
	for id, title := range want {
		if got[id] != title {
			t.Fatalf("the trigger list reads %v, want %v", got, want)
		}
	}

	// A label written later titles the row from that write on, and clearing it
	// gives the callable back.
	patched := mustPatch(t, ds, owner, corePackage+"/trigger", "unlabeled", substrate.PatchInput{
		Properties: map[string]any{"label": "Hourly mail sync"},
	})
	if patched.Title != "Hourly mail sync" {
		t.Fatalf("a label patched onto a trigger titles it %q, want the label", patched.Title)
	}
	cleared := mustPatch(t, ds, owner, corePackage+"/trigger", "nightly", substrate.PatchInput{
		Properties: map[string]any{"label": nil},
	})
	if cleared.Title != mailSyncFn {
		t.Fatalf("a trigger whose label was cleared is titled %q, want the callable %q", cleared.Title, mailSyncFn)
	}
	if _, has := cleared.Properties["label"]; has {
		t.Fatalf("the cleared label is still stored: %v", cleared.Properties)
	}
	stored, err := ds.Get(ctx, corePackage+"/trigger", "unlabeled")
	if err != nil {
		t.Fatalf("get the patched trigger: %v", err)
	}
	if stored.Title != "Hourly mail sync" {
		t.Fatalf("the stored title reads %q, want the label", stored.Title)
	}
}

// unlabeledTriggerTree is the shipped tree as the binary before issue 118
// shipped it: no `label` property, the template `{callable}`, and the kind one
// version below the shipped pin, so the boot upgrade lands the label.
func unlabeledTriggerTree(t *testing.T) (string, int64) {
	t.Helper()
	tree := shippedTree(t)
	var old int64
	patchShipped(t, coreKind(tree, "trigger.yaml"), func(doc string) string {
		const label = "    label:\n      type: string\n"
		at := strings.Index(doc, label)
		if at < 0 {
			t.Fatal("the shipped trigger no longer declares `label` as a string")
		}
		end := strings.Index(doc[at+len(label):], "\n    enabled:\n")
		if end < 0 {
			t.Fatal("the shipped trigger's `label` is no longer followed by `enabled`")
		}
		doc = doc[:at] + doc[at+len(label)+end+1:]
		const tmpl = `displayTemplate: "{label|callable}"`
		if !strings.Contains(doc, tmpl) {
			t.Fatalf("the shipped trigger no longer carries %s", tmpl)
		}
		doc = strings.Replace(doc, tmpl, `displayTemplate: "{callable}"`, 1)
		m := reDeclaredVersion.FindString(doc)
		shipped, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m), "version:")), 10, 64)
		if err != nil {
			t.Fatalf("the shipped trigger pins no integer version: %q", m)
		}
		old = shipped - 1
		return pinVersion(t, doc, strconv.FormatInt(old, 10))
	})
	return tree, old
}

// A shipped template change does not re-derive any stored title: a title is
// derived at write, so the boot upgrade that lands `{label|callable}` leaves
// every trigger row as it was, and none of them could hold a label before the
// property existed. The row reads the new template at its next write.
func TestTheLabelUpgradeKeepsStoredTriggerTitles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	tree, oldVersion := unlabeledTriggerTree(t)

	old := openTree(t, dsn, tree)
	if _, err := old.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := old.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := enginetest.Install(ctx, ds, owner, fnConnector(nil, mailSyncDoc())); err != nil {
		t.Fatalf("install the callable: %v", err)
	}
	callable := vocabulary.RecordPath("substrate.reamde.dev/core/function", mailSyncFn)
	props := map[string]any{"enabled": false, "source": map[string]any{"webhook": map[string]any{}}, "callable": callable}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: corePackage + "/trigger", ID: "early",
		Properties: map[string]any{"enabled": false, "source": props["source"], "callable": callable, "label": "Too early"},
	}); err == nil {
		t.Fatal("the old tree admitted a label; it is not the old tree")
	}
	before := mustPut(t, ds, owner, substrate.PutInput{Kind: corePackage + "/trigger", ID: "legacy", Properties: props})
	if before.Title != mailSyncFn {
		t.Fatalf("the old template titled the trigger %q, want %q", before.Title, mailSyncFn)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	next := openTree(t, dsn, shippedTree(t))
	t.Cleanup(func() { _ = next.Close() })
	ds, err = next.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if v := kindVersion(t, ds, corePackage+"/trigger"); v <= oldVersion {
		t.Fatalf("trigger is at version %d after the upgrade, want above %d", v, oldVersion)
	}
	after, err := ds.Get(ctx, corePackage+"/trigger", "legacy")
	if err != nil {
		t.Fatalf("get the stored trigger: %v", err)
	}
	if after.Title != before.Title || after.Version != before.Version {
		t.Fatalf("the upgrade rewrote the stored trigger: title %q version %d, was %q version %d",
			after.Title, after.Version, before.Title, before.Version)
	}
	labeled := mustPatch(t, ds, owner, corePackage+"/trigger", "legacy", substrate.PatchInput{
		Properties: map[string]any{"label": "Nightly mail sync"},
	})
	if labeled.Title != "Nightly mail sync" {
		t.Fatalf("a label written after the upgrade titles the trigger %q, want the label", labeled.Title)
	}
}
