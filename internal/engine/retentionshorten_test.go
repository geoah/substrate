package engine

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// shortenTree is one package whose header and widget state what the
// arguments say, "" leaving the key out, beside a gadget that states none.
func shortenTree(t *testing.T, header, widget string) *vocabulary.Registry {
	t.Helper()
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
	reg, err := vocabulary.LoadFS(fstest.MapFS{"r.example.com/r/all.yaml": &fstest.MapFile{Data: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// An upgrade shortens a kind's retention when the kind keeps less after it,
// by what it keeps and wherever that is stated: a header gaining or changing
// its default reaches every kind that states none, a kind's own key reaches
// that kind alone, a respelling and a lengthening are no shortening, and a
// kind the owner overrode keeps what the owner chose on both sides.
func TestAnUpgradeShortensWhatAKindKeepsByItsEffectiveRetention(t *testing.T) {
	t.Parallel()
	const widget, gadget = "r.example.com/r/widget", "r.example.com/r/gadget"
	for _, tc := range []struct {
		name                 string
		oldHeader, oldWidget string
		newHeader, newWidget string
		overrides            map[string]retentionOverride
		want                 []substrate.RetentionChange
	}{
		{name: "a header gains a default", newHeader: "none", want: []substrate.RetentionChange{
			{Kind: gadget, From: "all", To: "none"}, {Kind: widget, From: "all", To: "none"},
		}},
		{name: "a header default a kind states over", newHeader: "P30D", oldWidget: "all", newWidget: "all", want: []substrate.RetentionChange{
			{Kind: gadget, From: "all", To: "P30D"},
		}},
		{name: "a respelled default", oldHeader: "P30D", newHeader: "PT720H"},
		{name: "a header loses its default", oldHeader: "P7D"},
		{name: "a lengthened default", oldHeader: "P7D", newHeader: "P30D"},
		{name: "a kind gains a shorter key", oldHeader: "P30D", newHeader: "P30D", newWidget: "P7D", want: []substrate.RetentionChange{
			{Kind: widget, From: "P30D", To: "P7D"},
		}},
		{name: "a kind loses a longer key", oldHeader: "P30D", oldWidget: "all", newHeader: "P30D", want: []substrate.RetentionChange{
			{Kind: widget, From: "all", To: "P30D"},
		}},
		{
			name: "the owner overrode the kind", newHeader: "none",
			overrides: map[string]retentionOverride{widget: {id: "w", value: "all"}},
			want:      []substrate.RetentionChange{{Kind: gadget, From: "all", To: "none"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := retentionShortenings(shortenTree(t, tc.oldHeader, tc.oldWidget), shortenTree(t, tc.newHeader, tc.newWidget), tc.overrides)
			if !slices.Equal(got, tc.want) {
				t.Errorf("shortenings = %+v, want %+v", got, tc.want)
			}
		})
	}
}
