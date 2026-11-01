package catalog

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/substrate"
)

// requiringClosure is a package header at version and its bundle document,
// requiring each of requires at the floor beside it, in the order given.
func requiringClosure(pkg string, version int, requires ...any) string {
	authority, name, _ := strings.Cut(pkg, "/")
	var b strings.Builder
	fmt.Fprintf(&b, "kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %s\ndata:\n  authority: %s\n  package: %s\n  version: %d\n",
		pkg, authority, name, version)
	fmt.Fprintf(&b, "---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %s\ndata:\n  authority: %s\n  package: %s\n  description: The %s package.\n",
		pkg, authority, name, name)
	if len(requires) == 0 {
		return b.String()
	}
	b.WriteString("  requires:\n")
	for i := 0; i < len(requires); i += 2 {
		fmt.Fprintf(&b, "    - %s\n", requires[i])
	}
	b.WriteString("  requiresAtLeast:\n")
	for i := 0; i < len(requires); i += 2 {
		fmt.Fprintf(&b, "    %s: %d\n", requires[i], requires[i+1])
	}
	return b.String()
}

// heldBundles is a repository that holds the bundles it lists, at the stored
// versions it lists, and answers nothing else.
type heldBundles struct {
	substrate.Dataset
	held map[string]int64
}

func (d heldBundles) BundleStatusesUncounted(context.Context) ([]substrate.BundleStatus, error) {
	var out []substrate.BundleStatus
	for id, v := range d.held {
		out = append(out, substrate.BundleStatus{ID: id, Installed: true, Version: v})
	}
	return out, nil
}

// The one walk says what installing a bundle brings first and what it needs
// that the catalog cannot install: a package it does not ship and a sample,
// each once, held to the floor of the first bundle that required it. A
// requirement the repository holds at its floor is neither, whatever it is.
func TestRequirementsNamesWhatTheCatalogCannotInstall(t *testing.T) {
	const (
		top    = "example.com/top"
		mid    = "example.com/mid"
		held   = "example.com/held"
		gone   = "example.com/gone"
		mine   = "example.com/mine"
		notes  = "samples.example.com/notes"
		tasks  = "samples.example.com/tasks"
		absent = "example.com/absent"
	)
	providers := fstest.MapFS{
		"example.com/top/bundle.yaml":  {Data: []byte(requiringClosure(top, 1, gone, 1, mid, 1, mine, 1, notes, 1, tasks, 1))},
		"example.com/mid/bundle.yaml":  {Data: []byte(requiringClosure(mid, 1, gone, 2, held, 3, absent, 1))},
		"example.com/held/bundle.yaml": {Data: []byte(requiringClosure(held, 3))},
	}
	samples := fstest.MapFS{
		"samples.example.com/notes/bundle.yaml": {Data: []byte(requiringClosure(notes, 1))},
		"samples.example.com/tasks/bundle.yaml": {Data: []byte(requiringClosure(tasks, 1))},
	}
	c, err := Load(ProviderRoot(providers), SampleRoot(samples))
	if err != nil {
		t.Fatal(err)
	}
	if w := c.Warnings(); len(w) > 0 {
		t.Fatalf("the fixture catalog dropped %v", w)
	}
	ds := heldBundles{held: map[string]int64{held: 3, mine: 1, tasks: 1}}
	order, unmet, err := c.Requirements(context.Background(), top, ds)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 1 || order[0].ID != mid {
		t.Fatalf("installs %v first, want %s alone", order, mid)
	}
	var got []string
	for _, u := range unmet {
		shipped := "nothing"
		if u.Bundle != nil {
			shipped = u.Bundle.Tier
		}
		got = append(got, fmt.Sprintf("%s@%d:%s", u.ID, u.Floor, shipped))
	}
	want := gone + "@1:nothing," + absent + "@1:nothing," + notes + "@1:" + substrate.TierSample
	if strings.Join(got, ",") != want {
		t.Fatalf("unmet = %s, want %s", strings.Join(got, ","), want)
	}

	// Install runs the same order.
	if again, err := c.requirementsToInstall(context.Background(), c.byID[top], ds); err != nil || len(again) != 1 || again[0].ID != mid {
		t.Fatalf("the install's walk = %v, %v", again, err)
	}
	// Nothing to walk reads nothing and answers nothing.
	if order, unmet, err := c.Requirements(context.Background(), held, nil); err != nil || order != nil || unmet != nil {
		t.Fatalf("a bundle requiring nothing = %v, %v, %v", order, unmet, err)
	}
}
