package catalog_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

// A requirement whose document gives a closure written against a function
// interface this server does not run (decision record 0265) is refused for
// that reason wherever the requiring package meets it: the install refuses
// before anything lands, saying which requirement and why; the upgrade
// preview names the same reason among its blockers; and the walk hands the
// app set-up planner the reason with the requirement.
func TestARequirementOnAnInterfaceTheServerLacksIsRefusedWithThatReason(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	const (
		base = "publisher.example/base"
		app  = "publisher.example/app"
		why  = "function interface 2"
	)
	dir := t.TempDir()
	closure := func(name, header, bundle string) authority.AuthorityPackage {
		t.Helper()
		id := "publisher.example/" + name
		body := fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %[1]s\ndata:\n  authority: publisher.example\n  package: %[2]s\n  version: 1\n%[3]s---\n"+
			"kind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %[1]s\ndata:\n  authority: publisher.example\n  package: %[2]s\n  description: The %[2]s package.\n  installs: [%[1]s/note]\n%[4]s---\n"+
			"kind: substrate.reamde.dev/core/kind\nmetadata:\n  id: %[1]s/note\ndata:\n  authority: publisher.example\n  package: %[2]s\n  names:\n    singular: note\n  properties:\n    name:\n      type: string\n",
			id, name, header, bundle)
		file := name + ".yaml"
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		return authority.AuthorityPackage{Package: name, Versions: []authority.AuthorityPackageVersion{{Version: 1, Closure: file, SHA256: hex.EncodeToString(sum[:])}}}
	}
	doc := authority.AuthorityDocument{Authority: "publisher.example", Packages: []authority.AuthorityPackage{
		closure("base", "  interface: 2\n", ""),
		closure("app", "", "  requires: ["+base+"]\n"),
	}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	docPath := filepath.Join(dir, "authority.json")
	if err := os.WriteFile(docPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"publisher.example": docPath}}), nil)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = c.Install(ctx, substrate.ActorAPI, app, ds)
	if err == nil {
		t.Fatal("an install whose requirement's code is written against an interface this server lacks landed")
	}
	for _, want := range []string{app + " requires " + base + ", which this server cannot install", why} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
	// The requiring package is published and found; what it needs cannot be
	// installed, which is a validation refusal, not a 404.
	if !errors.Is(err, substrate.ErrValidation) || errors.Is(err, catalog.ErrNotPublished) {
		t.Errorf("the refusal is %v, want a validation refusal that does not read as the package unpublished", err)
	}
	for _, id := range []string{app, base} {
		if _, err := ds.Get(ctx, "substrate.reamde.dev/core/package", id); !errors.Is(err, substrate.ErrNotFound) {
			t.Errorf("the refused install left %s: %v", id, err)
		}
	}

	plan, err := c.Upgrade(ctx, app, ds, nil)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !strings.Contains(strings.Join(plan.Blockers, "\n"), why) {
		t.Errorf("the preview's blockers do not name the interface: %q", plan.Blockers)
	}

	_, unmet, err := c.Requirements(ctx, app, ds)
	if err != nil {
		t.Fatal(err)
	}
	if len(unmet) != 1 || unmet[0].ID != base || unmet[0].Err == nil || !strings.Contains(unmet[0].Err.Error(), why) {
		t.Fatalf("the walk passed over %+v, want %s with the interface as its reason", unmet, base)
	}
}
