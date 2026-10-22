package unitseed_test

// docs/units.md lists the shipped seed for a reader. TestUnitsPage writes the
// list from units.yaml and conversions.yaml into the region of the page
// between the two markers below, so the list cannot drift from the seed: a
// seed change fails the test until
//
//	go test ./internal/engine/unitseed/ -run TestUnitsPage -update
//
// rewrites the region. The prose around it is written by hand.

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/engine/unitseed"
	"github.com/geoah/substrate/internal/vocabulary"
)

var updatePage = flag.Bool("update", false, "rewrite the shipped units in docs/units.md from the seed")

const (
	unitsPage   = "../../../docs/units.md"
	pageBegin   = "<!-- BEGIN shipped units: written by TestUnitsPage in internal/engine/unitseed from units.yaml and conversions.yaml; rewrite with go test ./internal/engine/unitseed/ -run TestUnitsPage -update, never by hand. -->\n"
	pageEnd     = "<!-- END shipped units -->\n"
	ungroupedAs = "other"
)

// seedDoc is one record of a seed file and the group heading it sits under:
// the `# heading` comment on the line right above a record's `kind:` opens a
// group that lasts until the next one.
type seedDoc struct {
	group string
	id    string
	props map[string]any
}

func TestUnitsPage(t *testing.T) {
	page, err := os.ReadFile(unitsPage)
	if err != nil {
		t.Fatal(err)
	}
	head, rest, ok := bytes.Cut(page, []byte(pageBegin))
	if !ok {
		t.Fatalf("%s has no line %q", unitsPage, strings.TrimSpace(pageBegin))
	}
	_, tail, ok := bytes.Cut(rest, []byte(pageEnd))
	if !ok {
		t.Fatalf("%s has no line %q", unitsPage, strings.TrimSpace(pageEnd))
	}
	var want bytes.Buffer
	want.Write(head)
	want.WriteString(pageBegin)
	want.WriteString(renderSeed(t))
	want.WriteString(pageEnd)
	want.Write(tail)
	if *updatePage {
		if err := os.WriteFile(unitsPage, want.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if !bytes.Equal(page, want.Bytes()) {
		t.Fatalf("the shipped units in %s differ from the seed: rewrite them with -update, never by hand", unitsPage)
	}
}

func renderSeed(t *testing.T) string {
	t.Helper()
	g, problems, err := unitseed.Graph()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("the shipped seed: %v", p)
	}
	units := readSeedFile(t, unitseed.Units())
	conversions := readSeedFile(t, unitseed.Conversions())

	var groups []string
	unitsIn := map[string][]seedDoc{}
	conversionsIn := map[string][]seedDoc{}
	for _, d := range units {
		if _, ok := unitsIn[d.group]; !ok {
			groups = append(groups, d.group)
		}
		unitsIn[d.group] = append(unitsIn[d.group], d)
	}
	for _, d := range conversions {
		if _, ok := unitsIn[d.group]; !ok {
			if _, seen := conversionsIn[d.group]; !seen {
				groups = append(groups, d.group)
			}
		}
		conversionsIn[d.group] = append(conversionsIn[d.group], d)
	}

	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	w("\n%d units and %d conversions, in %d groups. A group is a heading in the seed\n", len(units), len(conversions), len(groups))
	w("files and nothing more: two units convert when conversions join them, whatever\n")
	w("group they sit in. In each unit's row, `root = value x factor + offset` is its\n")
	w("exact map to the root of its linear component.\n\n")
	for _, group := range groups {
		w("### %s\n\n", group)
		if us := unitsIn[group]; len(us) > 0 {
			w("| Id | Name | Symbol | Code | Aliases | Root | Factor | Offset | Notes |\n")
			w("| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
			for _, u := range us {
				rec, ok := g.Unit(u.id)
				if !ok {
					t.Fatalf("the graph holds no unit %s", u.id)
				}
				root, _ := g.Root(u.id)
				factor, offset := "", ""
				if f, o, ok := g.ToRoot(u.id); ok {
					factor = vocabulary.FormatRational(f)
					if o.Sign() != 0 {
						offset = vocabulary.FormatRational(o)
					}
				}
				aliases := make([]string, len(rec.Aliases))
				for i, a := range rec.Aliases {
					aliases[i] = code(a)
				}
				var notes []string
				if rec.Scale != "" {
					notes = append(notes, "scale "+code(string(rec.Scale)))
				}
				if rec.Turn != nil {
					notes = append(notes, "turn "+code(vocabulary.FormatRational(rec.Turn)))
				}
				w("| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
					code(rec.ID), cell(rec.Name), cell(rec.Symbol), code(rec.Code), strings.Join(aliases, ", "),
					code(root), factor, offset, strings.Join(notes, ", "))
			}
			w("\n")
		}
		if cs := conversionsIn[group]; len(cs) > 0 {
			w("| Conversion | From | To | Factor | Offset |\n")
			w("| --- | --- | --- | --- | --- |\n")
			for _, c := range cs {
				rec, err := vocabulary.CheckConversionRecord(c.id, c.props)
				if err != nil {
					t.Fatalf("conversion %s: %v", c.id, err)
				}
				factor, offset := "", ""
				if rec.Factor != nil {
					factor = vocabulary.FormatRational(rec.Factor)
				}
				if rec.Offset != nil && rec.Offset.Sign() != 0 {
					offset = vocabulary.FormatRational(rec.Offset)
				}
				if rec.Expr != "" {
					factor = code(rec.Expr)
				}
				w("| %s | %s | %s | %s | %s |\n", code(rec.ID), code(rec.From), code(rec.To), factor, offset)
			}
			w("\n")
		}
	}
	return b.String()
}

// readSeedFile reads one seed file's records in order, each with its group.
func readSeedFile(t *testing.T, file []byte) []seedDoc {
	t.Helper()
	// The groups, read off the raw text: yaml.v3 hands a comment to whichever
	// node it decides owns it, and a heading must belong to the record below.
	var groupOf []string
	group, prev := ungroupedAs, ""
	sc := bufio.NewScanner(bytes.NewReader(file))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "kind:") {
			if h, ok := strings.CutPrefix(prev, "# "); ok {
				group = strings.TrimSpace(h)
			}
			groupOf = append(groupOf, group)
		}
		prev = line
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	var out []seedDoc
	dec := yaml.NewDecoder(bytes.NewReader(file))
	for i := 0; ; i++ {
		var doc struct {
			Metadata struct {
				ID string `yaml:"id"`
			} `yaml:"metadata"`
			Data struct {
				Properties map[string]any `yaml:"properties"`
			} `yaml:"data"`
		}
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if i >= len(groupOf) {
			t.Fatalf("record %s: its `kind:` line does not open the line", doc.Metadata.ID)
		}
		out = append(out, seedDoc{group: groupOf[i], id: doc.Metadata.ID, props: doc.Data.Properties})
	}
	if len(out) != len(groupOf) {
		t.Fatalf("%d records and %d `kind:` lines", len(out), len(groupOf))
	}
	return out
}

// code backticks a spelling for a Markdown table cell, escaping the one
// character a cell cannot hold.
func code(s string) string {
	if s == "" {
		return ""
	}
	return "`" + strings.ReplaceAll(s, "|", `\|`) + "`"
}

// cell escapes plain text for a Markdown table cell.
func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
