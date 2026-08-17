package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func newTable(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)
}

// humanAge renders a duration the way kubectl does: one unit, no decimals.
func humanAge(now, then time.Time) string {
	if then.IsZero() {
		return "<unknown>"
	}
	d := now.Sub(then)
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/24/365))
}

// joinStates renders a record's states as "name=state", in the order the
// caller resolved them (`stateProperties`, sorted).
//
// The names come from the type's declaration rather than from the record: a
// state is an ordinary property carrying a declared value, so nothing about
// `status: open` sitting in a properties map says it is a state. A type that
// declares none, or a registry that could not be reached, renders the same
// empty column — which is honest either way.
func joinStates(properties map[string]any, names []string) string {
	parts := make([]string, 0, len(names))
	for _, name := range names {
		if s, ok := properties[name].(string); ok && s != "" {
			parts = append(parts, name+"="+s)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// recordTitle reads the title out of the property map, which is where
// everything authored lives (FORMAT.md §3). The TITLE column is the one place
// the CLI names a property rather than printing the document.
func recordTitle(e *substrate.Record) string {
	s, _ := e.Properties["title"].(string)
	return s
}

func printRecordTable(w io.Writer, records []*substrate.Record, wide bool, now time.Time, states []string) error {
	tw := newTable(w)
	if wide {
		fmt.Fprintln(tw, "ID\tTYPE\tTITLE\tSTATE\tVERSION\tUPDATED")
	} else {
		fmt.Fprintln(tw, "ID\tTITLE\tSTATE\tUPDATED")
	}
	for _, e := range records {
		title := dash(truncate(strings.ReplaceAll(recordTitle(e), "\n", " "), 60))
		state := joinStates(e.Properties, states)
		if wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n",
				e.ID, e.Kind, title, state, e.Version, humanAge(now, e.UpdatedAt))
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.ID, title, state, humanAge(now, e.UpdatedAt))
	}
	return tw.Flush()
}

func printKindsTable(w io.Writer, types []substrate.KindInfo) error {
	tw := newTable(w)
	// COLLECTION is the segment a command takes, which is the kind's name: a
	// reader copies it straight into `substratectl get <collection>` and into a
	// URL, because those are now the same string (decision 0028).
	fmt.Fprintln(tw, "NAME\tAUTHORITY\tCOLLECTION\tVERSION\tSOURCE")
	for _, ti := range types {
		version := "-"
		if ti.Version > 0 {
			version = strconv.FormatInt(ti.Version, 10)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			dash(ti.Name), dash(ti.Authority), dash(collectionOf(ti)), version, dash(ti.Source))
	}
	return tw.Flush()
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// printDocuments writes records as apply-able YAML documents.
func printDocuments(w io.Writer, records []*substrate.Record) error {
	for i, e := range records {
		if i > 0 {
			fmt.Fprintln(w, "---")
		}
		// Lists never carry propertyMeta, so there is no status.properties here.
		b, err := marshalDocument(documentOf(e, nil))
		if err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	return nil
}

const changeHeader = "SEQ      OP        TYPE                                 RECORD           ACTOR"

func formatChange(c substrate.Change) string {
	return fmt.Sprintf("%-8d %-9s %-36s %-16s %s", c.Seq, c.Op, dash(c.Kind), dash(c.RecordID), dash(string(c.Actor)))
}
