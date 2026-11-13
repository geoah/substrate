package vocabulary_test

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// retentionTree is one package whose header and two kinds state what the
// arguments say, "" leaving the key out.
func retentionTree(header, widget string) fstest.MapFS {
	line := func(v string) string {
		if v == "" {
			return ""
		}
		return "\n  retention: " + v
	}
	src := `kind: substrate.reamde.dev/core/package
metadata:
  id: r.example.com/r
data:
  authority: r.example.com
  package: r
  version: 1` + line(header) + `
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: r.example.com/r/widget
data:
  authority: r.example.com
  package: r
  names:
    singular: widget` + line(widget) + `
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: r.example.com/r/gadget
data:
  authority: r.example.com
  package: r
  names:
    singular: gadget
`
	return fstest.MapFS{"r.example.com/r/all.yaml": &fstest.MapFile{Data: []byte(src)}}
}

// THE THREE FORMS AND THE ABSENT KEY. A kind states `all`, `none` or a
// duration and keeps it as written; one that states none takes its package
// header's, and a header that states none reads as all.
func TestRetentionIsTheKindsElseThePackagesElseAll(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		header, widget         string
		wantWidget, wantGadget string
	}{
		{"", "", "", ""},
		{"", "all", "all", ""},
		{"", "none", "none", ""},
		{"", "P30D", "P30D", ""},
		{"P30D", "", "P30D", "P30D"},
		{"P30D", "P7D", "P7D", "P30D"},
		{"none", "all", "all", "none"},
	} {
		reg, err := vocabulary.LoadFS(retentionTree(tc.header, tc.widget))
		if err != nil {
			t.Fatalf("header %q, widget %q: %v", tc.header, tc.widget, err)
		}
		if got := reg.RetentionOf("r.example.com/r/widget"); got != tc.wantWidget {
			t.Errorf("header %q, widget %q: the widget keeps %q, want %q", tc.header, tc.widget, got, tc.wantWidget)
		}
		if got := reg.RetentionOf("r.example.com/r/gadget"); got != tc.wantGadget {
			t.Errorf("header %q, widget %q: the gadget keeps %q, want %q", tc.header, tc.widget, got, tc.wantGadget)
		}
		k, _ := reg.ByIdentity("r.example.com/r/widget")
		if v, stated := k.Definition["retention"]; (tc.widget == "") == stated || (stated && v != tc.widget) {
			t.Errorf("widget %q: the definition carries %v (%v), want exactly what was written", tc.widget, v, stated)
		}
	}
	if got := vocabulary.MustRetention(""); !got.KeepsAll() {
		t.Errorf("an absent retention reads as %+v, want all", got)
	}
	if got := vocabulary.MustRetention("P30D"); got.For != 30*24*time.Hour || got.KeepsAll() || got.KeepsNone() {
		t.Errorf("P30D reads as %+v", got)
	}
}

// A MALFORMED VALUE IS REFUSED WITH A REASON, on a kind and on a header
// alike: a value that is not a string, a word that is not one of the two, a
// duration of no time or less, a calendar duration, a time part with nothing
// in it.
func TestAMalformedRetentionIsRefusedWithAReason(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"30", "forever", "P0D", "-P1D", "P1Y", "PT", "P1DT", "P1WT", "p30d", "\"\"", "[all]"} {
		for _, where := range []string{"kind", "header"} {
			tree := retentionTree("", v)
			if where == "header" {
				tree = retentionTree(v, "")
			}
			_, err := vocabulary.LoadFS(tree)
			if err == nil {
				t.Errorf("%s retention %s was admitted", where, v)
				continue
			}
			if !strings.Contains(err.Error(), "data.retention") || !strings.Contains(err.Error(), "all") {
				t.Errorf("%s retention %s: the refusal does not name the key and the forms: %v", where, v, err)
			}
		}
	}
}

// Two retentions compare by how long each keeps past the latest state: all
// is the longest, none is nothing, and two spellings of one duration keep
// the same, so an upgrade that respells one is no change (decision 0282).
func TestARetentionComparesByWhatItKeeps(t *testing.T) {
	keeps := func(s string) time.Duration {
		t.Helper()
		r, err := vocabulary.ParseRetention(s)
		if err != nil {
			t.Fatal(err)
		}
		return r.Keeps()
	}
	if keeps("P30D") != keeps("PT720H") || keeps("P7D") != keeps("P1W") {
		t.Error("two spellings of one duration keep different times")
	}
	for _, longer := range [][2]string{{"all", "P3650D"}, {"P30D", "P7D"}, {"PT1H", "none"}} {
		if keeps(longer[0]) <= keeps(longer[1]) {
			t.Errorf("%s keeps no longer than %s", longer[0], longer[1])
		}
	}
}
