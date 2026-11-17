package kinds_test

import (
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

var updateRetention = flag.Bool("update", false, "rewrite retention.golden.json from the shipped tree")

const retentionGolden = "retention.golden.json"

// EVERY SHIPPED KIND'S RETENTION, HELD (decision records 0282, 0283, 0320 and
// 0333). The golden is each kind of both shipped trees with the retention its
// declaration states, its own key else its package header's, else all. A kind
// the golden does not list fails here until its author says which of the two
// classes it is in, and a change of one is a line the review reads:
//
//   - bookkeeping, what the substrate or a sync writes about its own runs,
//     states P7D;
//   - every other kind keeps all of its history and states nothing: what a
//     person writes, and a provider's mirror of an outside service, whose
//     earlier states nothing can fetch again.
func TestEveryShippedKindsRetentionIsHeld(t *testing.T) {
	merged := fstest.MapFS{}
	for name, src := range map[string]fs.FS{"kinds": kinds.All(), "samples": samples.Samples()} {
		err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			raw, err := fs.ReadFile(src, p)
			if err != nil {
				return err
			}
			merged[name+"/"+p] = &fstest.MapFile{Data: raw}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	reg, err := vocabulary.LoadFS(merged)
	if err != nil {
		t.Fatalf("load both shipped trees: %v", err)
	}
	got := map[string]string{}
	for _, k := range reg.Kinds() {
		got[k.Identity] = vocabulary.MustRetention(reg.RetentionOf(k.Identity)).Value
	}
	if *updateRetention {
		raw, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.FromSlash(retentionGolden), append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(filepath.FromSlash(retentionGolden))
	if err != nil {
		t.Fatalf("read the golden (go test ./kinds -run TestEveryShippedKindsRetentionIsHeld -update writes it): %v", err)
	}
	var want map[string]string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	const classes = "say which class it is in — bookkeeping states P7D, every other kind keeps all " +
		"(a person's own and a provider's mirror alike) — " +
		"then run go test ./kinds -run TestEveryShippedKindsRetentionIsHeld -update"
	for kind, value := range got {
		w, listed := want[kind]
		switch {
		case !listed:
			t.Errorf("%s keeps %s and the golden does not list it: %s", kind, value, classes)
		case w != value:
			t.Errorf("%s keeps %s, and the golden says %s: %s", kind, value, w, classes)
		}
	}
	for kind := range want {
		if _, ok := got[kind]; !ok {
			t.Errorf("the golden lists %s, which the shipped trees no longer declare: run with -update", kind)
		}
	}
	// The classes the records name, held where the golden could drift:
	// bookkeeping keeps seven days, and every other shipped kind, a mirror
	// of an outside service among them, keeps all.
	bookkeeping := map[string]bool{
		"substrate.reamde.dev/core/trigger":                     true,
		"substrate.reamde.dev/core/triggerrun":                  true,
		"substrate.reamde.dev/core/alert":                       true,
		"providers.substrate.reamde.dev/applehealth/export":     true,
		"providers.substrate.reamde.dev/beeper/chatsync":        true,
		"providers.substrate.reamde.dev/google/calendarsync":    true,
		"providers.substrate.reamde.dev/notion/pagesync":        true,
		"providers.substrate.reamde.dev/slack/conversationsync": true,
	}
	for kind := range bookkeeping {
		if got[kind] != "P7D" {
			t.Errorf("%s is bookkeeping and keeps %s, want P7D (decision records 0283 and 0320)", kind, got[kind])
		}
	}
	providers := 0
	for _, k := range reg.Kinds() {
		if bookkeeping[k.Identity] {
			continue
		}
		if authority, _, _ := vocabulary.SplitKindRef(k.Identity); authority == "providers.substrate.reamde.dev" {
			providers++
		}
		if got[k.Identity] != vocabulary.RetentionAll {
			t.Errorf("%s keeps %s, want all (decision record 0333): %s", k.Identity, got[k.Identity], classes)
		}
	}
	if providers == 0 {
		t.Fatal("no provider kind loaded, so the rule above held no mirror")
	}
}
