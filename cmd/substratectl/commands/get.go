package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

func (a *app) getCommand() *cobra.Command {
	var (
		authority string
		output    string
		filter    string
		selector  []string
		watch     bool
		limit     int
		orderBy   string
		after     string
	)
	cmd := &cobra.Command{
		Use:   "get <plural> [id]",
		Short: "List or read records",
		Long: `Read records from a collection.

The plural may be qualified ("people.substrate.reamde.dev/people") — which is resolved
without a round trip — or bare ("people"), which is resolved against the kind
registry and errors when several authorities declare it. The shipped vocabulary is
split across several authorities (people, messaging, calendar, tasks), so a
bare plural is only unambiguous while one authority declares it: "tasks" is
tasks' alone and resolves, but every bundle installs a
"config", so "configs" always needs qualifying (or -g to name the authority).

-o yaml writes each record as a manifest — authority, kind, metadata, data and the
server-set status — ---separated, and -o json writes the same shape. status is
ignored on input, so the output applies back unchanged.

Everything authored is in data.properties: title, body and the temporal
properties beside the declared ones, states included — so "status" sits beside
"description", and the STATE column names the ones the kind declares as
states.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			col, err := a.resolveCollection(ctx, args[0], authority)
			if err != nil {
				return err
			}
			cl, err := a.client()
			if err != nil {
				return err
			}
			if len(args) == 2 {
				return a.getOne(ctx, cl, col, args[1], output)
			}
			q, err := listQuery(filter, selector, limit, orderBy, after)
			if err != nil {
				return err
			}
			if watch {
				q.Set("watch", "1")
				resp, err := cl.send(ctx, http.MethodGet, collectionPath(col.Authority, col.Plural), q, nil)
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				return streamChanges(a.out, resp.Body)
			}
			page, err := cl.list(ctx, col.Authority, col.Plural, q)
			if err != nil {
				return err
			}
			return a.printRecords(ctx, col, page, output)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&authority, "authority", "g", "", "kind authority for a bare plural")
	f.StringVarP(&output, "output", "o", "", "output format: table|wide|yaml|json (default table for lists, yaml for a single record)")
	f.StringVar(&filter, "filter", "", `filter as JSON (substrate.Filter), e.g. '{"properties":{"prominence":{"eq":"known"}}}'`)
	f.StringArrayVarP(&selector, "selector", "l", nil, "label selector, key=value (repeatable); bare key means present")
	f.BoolVarP(&watch, "watch", "w", false, "stream changes for this collection instead of listing")
	f.IntVar(&limit, "limit", 0, "maximum records to return")
	f.StringVar(&orderBy, "order-by", "", `order, e.g. "at:desc,createdAt"`)
	f.StringVar(&after, "after", "", "opaque keyset cursor from a previous page's \"next cursor\" line; resent verbatim")
	return cmd
}

func (a *app) getOne(ctx context.Context, cl *client, col collection, id, output string) error {
	e, meta, err := cl.get(ctx, col.Authority, col.Plural, id)
	if err != nil {
		return err
	}
	// The canonical-id contract: a read by a former id returns the canonical
	// record *and says so*. `CanonicalID` is set only on such a
	// read, so its presence is the whole signal.
	//
	// The note goes to stderr, so every output format keeps its shape — a yaml
	// document still applies back unchanged (`documentOf` renders the
	// envelope's fields, and this is not one of them), a table is still a
	// table, and `substratectl get … | …` is not what changed. It is a note and
	// not an error: the read succeeded, and the record below is the right one.
	if e.CanonicalID != "" && e.CanonicalID != id {
		fmt.Fprintf(a.errOut, "resolved via former id; canonical: %s\n", e.CanonicalID)
	}
	switch output {
	case "", "yaml":
		// A single read is the one place `propertyMeta` is on the wire, so it
		// is the one place `status.properties` is in the document.
		b, err := marshalDocument(documentOf(e, meta))
		if err != nil {
			return err
		}
		_, err = a.out.Write(b)
		return err
	case "json":
		return printJSON(a.out, documentOf(e, meta))
	case "table":
		return printRecordTable(a.out, []*substrate.Record{e}, false, a.now(), a.statesFor(ctx, col))
	case "wide":
		return printRecordTable(a.out, []*substrate.Record{e}, true, a.now(), a.statesFor(ctx, col))
	}
	return fmt.Errorf("unknown output format %q: use table, wide, yaml or json", output)
}

// printRecords writes a page. The STATE column is the only thing here that
// needs the declaration — a state is an ordinary property, and nothing on the
// record says which one it is — so the registry is consulted for the table
// formats and for nothing else.
func (a *app) printRecords(ctx context.Context, col collection, page *recordPage, output string) error {
	switch output {
	case "", "table":
		if err := printRecordTable(a.out, page.Records, false, a.now(), a.statesFor(ctx, col)); err != nil {
			return err
		}
	case "wide":
		if err := printRecordTable(a.out, page.Records, true, a.now(), a.statesFor(ctx, col)); err != nil {
			return err
		}
	case "yaml":
		if err := printDocuments(a.out, page.Records); err != nil {
			return err
		}
	case "json":
		// The JSON of a --- separated stream is the array of its manifests;
		// the cursor still goes to stderr, as it does for yaml.
		docs := make([]any, 0, len(page.Records))
		for _, e := range page.Records {
			docs = append(docs, documentOf(e, nil))
		}
		if err := printJSON(a.out, docs); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown output format %q: use table, wide, yaml or json", output)
	}
	if page.Cursor != "" {
		fmt.Fprintf(a.errOut, "more results available; next cursor: %s\n", page.Cursor)
	}
	return nil
}

// listQuery builds the list parameters: --filter merged with -l selectors,
// plus the opaque keyset cursor (--after), which pagination resends verbatim.
func listQuery(filter string, selectors []string, limit int, orderBy, after string) (url.Values, error) {
	q := url.Values{}
	var f substrate.Filter
	if strings.TrimSpace(filter) != "" {
		if err := json.Unmarshal([]byte(filter), &f); err != nil {
			return nil, fmt.Errorf("parse --filter as JSON: %w", err)
		}
	}
	for _, sel := range selectors {
		key, cond, err := parseSelector(sel)
		if err != nil {
			return nil, err
		}
		if f.Labels == nil {
			f.Labels = map[string]substrate.Cond{}
		}
		f.Labels[key] = cond
	}
	if !filterIsZero(f) {
		b, err := json.Marshal(f)
		if err != nil {
			return nil, fmt.Errorf("encode filter: %w", err)
		}
		q.Set("filter", string(b))
	}
	if limit > 0 {
		q.Set("first", strconv.Itoa(limit))
	}
	if orderBy != "" {
		q.Set("orderBy", orderBy)
	}
	if after != "" {
		// The server's own opaque keyset token — stored and resent unchanged
		//: no offset, no page-jump, just the next seek.
		q.Set("after", after)
	}
	return q, nil
}

func parseSelector(sel string) (string, substrate.Cond, error) {
	key, value, ok := strings.Cut(sel, "=")
	key = strings.TrimSpace(key)
	if key == "" {
		return "", substrate.Cond{}, fmt.Errorf("invalid selector %q: expected key=value", sel)
	}
	if !ok {
		yes := true
		return key, substrate.Cond{Exists: &yes}, nil
	}
	return key, substrate.Cond{Eq: scalarValue(value)}, nil
}

// scalarValue types a selector value the way the filter grammar expects.
func scalarValue(s string) any {
	switch s {
	case "true":
		return true
	case "false":
		return false
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

func filterIsZero(f substrate.Filter) bool {
	return len(f.Kinds) == 0 && f.Implements == "" && len(f.IDs) == 0 &&
		len(f.Properties) == 0 && len(f.Labels) == 0 && f.Edge == nil &&
		f.Deleted == nil
}
