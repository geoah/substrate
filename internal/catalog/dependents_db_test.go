package catalog_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

// WHAT HAPPENS TO A PACKAGE WHOSE REQUIREMENT CHANGES OR GOES (F11, "Not
// verified", in pull request 1313). `publisher.example/journal` requires
// `publisher.example/base` and references its `item` kind. This test records
// what the server does today, and it is the defect the finding suspected:
// neither a version of `base` that drops `item` nor uninstalling `base` is
// checked against `journal`. Both are admitted, and `journal` is quarantined
// at the next open, its records unreadable until the requirement is back.
// Decision record 0269 names it; the fix belongs to a later step. When a door
// refuses either change, this test fails and its assertions turn around.
func TestADependentPackageIsNotCheckedWhenItsRequirementChanges(t *testing.T) {
	const base, journal = "publisher.example/base", "publisher.example/journal"
	setup := func(t *testing.T) (*reopenableDataset, *catalog.Catalog, func(int, bool)) {
		t.Helper()
		r := newReopenableDataset(t)
		dir := t.TempDir()
		docPath := filepath.Join(dir, "authority.json")
		write := func(name, body string) authority.AuthorityPackageVersion {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte(body))
			return authority.AuthorityPackageVersion{Closure: name, SHA256: hex.EncodeToString(sum[:])}
		}
		journalV1 := write("journal-1.yaml", fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %[1]s\ndata:\n  authority: publisher.example\n  package: journal\n  version: 1\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %[1]s\ndata:\n  authority: publisher.example\n  package: journal\n  description: Journal.\n  requires: [%[2]s]\n  installs: [%[1]s/entry]\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: %[1]s/entry\ndata:\n  authority: publisher.example\n  package: journal\n  names:\n    singular: entry\n  properties:\n    name:\n      type: string\n    item:\n      type: reference\n      kind: %[2]s/item\n      description: the item the entry is about\n", journal, base))
		journalV1.Version = 1
		var baseVersions []authority.AuthorityPackageVersion
		// release lists base at version v, with `item` or without it.
		release := func(v int, withItem bool) {
			t.Helper()
			var kinds, installs string
			if withItem {
				kinds = fmt.Sprintf("---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: %[1]s/item\ndata:\n  authority: publisher.example\n  package: base\n  names:\n    singular: item\n  properties:\n    name:\n      type: string\n", base)
				installs = "[" + base + "/item]"
			} else {
				kinds = fmt.Sprintf("---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: %[1]s/other\ndata:\n  authority: publisher.example\n  package: base\n  names:\n    singular: other\n  properties:\n    name:\n      type: string\n", base)
				installs = "[" + base + "/other]"
			}
			listed := write(fmt.Sprintf("base-%d.yaml", v), fmt.Sprintf("kind: substrate.reamde.dev/core/package\nmetadata:\n  id: %[1]s\ndata:\n  authority: publisher.example\n  package: base\n  version: %[2]d\n---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: %[1]s\ndata:\n  authority: publisher.example\n  package: base\n  description: Base.\n  installs: %[3]s\n%[4]s", base, v, installs, kinds))
			listed.Version = v
			baseVersions = append(baseVersions, listed)
			raw, _ := json.Marshal(authority.AuthorityDocument{Authority: "publisher.example", Packages: []authority.AuthorityPackage{
				{Package: "base", Versions: baseVersions},
				{Package: "journal", Versions: []authority.AuthorityPackageVersion{journalV1}},
			}})
			if err := os.WriteFile(docPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		release(1, true)
		c, err := catalog.LoadWithReader(authority.NewReader(authority.Options{LocalDocuments: map[string]string{"publisher.example": docPath}}), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.Install(context.Background(), substrate.ActorAPI, journal, r.ds); err != nil {
			t.Fatalf("install journal, and base with it: %v", err)
		}
		return r, c, release
	}
	// quarantined reports whether journal's package row carries the
	// quarantine, and logs why.
	quarantined := func(t *testing.T, ds substrate.Dataset) bool {
		t.Helper()
		row, err := ds.Get(context.Background(), "substrate.reamde.dev/core/package", journal)
		if err != nil {
			t.Fatalf("journal's package row: %v", err)
		}
		t.Logf("journal: quarantined=%v reason=%v", row.Properties["quarantined"], row.Properties["quarantineReason"])
		return row.Properties["quarantined"] == true
	}

	t.Run("a version of the requirement that drops a kind the dependent references", func(t *testing.T) {
		r, c, release := setup(t)
		if quarantined(t, r.ds) {
			t.Fatal("journal is quarantined before anything changed")
		}
		release(2, false)
		if _, _, err := c.Install(context.Background(), substrate.ActorAPI, base, r.ds); err != nil {
			t.Fatalf("today the change is admitted; a refusal here is the fix landing: %v", err)
		}
		if !quarantined(t, r.reopen(t)) {
			t.Fatal("journal was not quarantined at the next open; the defect this test records is gone, so turn it around")
		}
	})

	t.Run("uninstalling the requirement", func(t *testing.T) {
		r, _, _ := setup(t)
		if quarantined(t, r.ds) {
			t.Fatal("journal is quarantined before anything changed")
		}
		if err := r.ds.(bundleLifecycler).UninstallBundle(context.Background(), base); err != nil {
			t.Fatalf("today the uninstall is admitted; a refusal here is the fix landing: %v", err)
		}
		if !quarantined(t, r.reopen(t)) {
			t.Fatal("journal was not quarantined at the next open; the defect this test records is gone, so turn it around")
		}
	})
}
