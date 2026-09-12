package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

func (a *app) getCommand() *cobra.Command {
	var (
		pkg         string
		output      string
		filter      string
		selector    []string
		watch       bool
		from        int64
		generation  string
		limit       int
		orderBy     string
		after       string
		expand      []string
		referencing string
	)
	cmd := &cobra.Command{
		Use:   "get <kind> [id]",
		Short: "List or read records",
		Long: `Read the records of one kind, or one record.

The kind may be qualified ("samples.substrate.reamde.dev/people/person") — which
is resolved without a round trip — or bare ("person"), which is resolved against
the kind registry and errors when several packages declare it. The shipped
vocabulary is split across several packages (people, messaging, calendar,
tasks), so a bare name is only unambiguous while one package declares it:
"task" is tasks' alone and resolves, but every provider installs a "config", so
"config" always needs qualifying (or --package to name the package it lives
in).

-o yaml writes each record as a manifest — kind, metadata, data and the
server-set status — ---separated, and -o json writes the same shape. status is
ignored on input, so the output applies back unchanged.

--expand names reference properties whose referents come back with the page,
one hop: -o yaml prints them as further documents after the page, -o json puts
the page under "records" and the referents under "included", keyed by
<kind>/<id>. The table formats print the page alone. --referencing <kind>/<id>
is the reverse read: only the records of this kind that point at that one.

-w streams this kind's changes instead of listing it; --from and --generation
resume the stream the way "substratectl watch" does.

Everything authored is in data.properties: title, body and the temporal
properties beside the declared ones, states included — so "status" sits beside
"description", and the STATE column names the ones the kind declares as
states.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			col, err := a.resolveCollection(ctx, args[0], pkg)
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
			if referencing != "" {
				if !strings.Contains(referencing, "/") {
					return fmt.Errorf("--referencing takes a record path, <kind>/<id>, got %q", referencing)
				}
				err := editFilter(q, func(f *substrate.Filter) {
					f.Referencing = &substrate.Referencing{Ref: referencing}
				})
				if err != nil {
					return err
				}
			}
			if len(expand) > 0 {
				q.Set("expand", strings.Join(expand, ","))
			}
			if watch {
				// The tail is the records route in its watch mode: the kind
				// rides in filter.kinds, and the cursor is the changelog's.
				q.Set("watch", "1")
				if err := setFilterKinds(q, col.pkg()+"/"+col.Name); err != nil {
					return err
				}
				if from > 0 {
					q.Set("from", strconv.FormatInt(from, 10))
				}
				if generation != "" {
					q.Set("generation", generation)
				}
				resp, err := cl.send(ctx, http.MethodGet, pathRecords, q, nil)
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				return streamChanges(a.out, resp.Body)
			}
			page, err := cl.list(ctx, col.pkg(), col.Name, q)
			if err != nil {
				return err
			}
			return a.printRecords(ctx, col, page, output, len(expand) > 0)
		},
	}
	f := cmd.Flags()
	f.StringVar(&pkg, "package", "", "the package (<authority>/<package>) a bare kind name resolves in")
	f.StringVarP(&output, "output", "o", "", "output format: table|wide|yaml|json (default table for lists, yaml for a single record)")
	f.StringVar(&filter, "filter", "", `filter as JSON (substrate.Filter), e.g. '{"properties":{"prominence":{"eq":"known"}}}'`)
	f.StringArrayVarP(&selector, "selector", "l", nil, "label selector, key=value (repeatable); bare key means present")
	f.BoolVarP(&watch, "watch", "w", false, "stream this kind's changes instead of listing")
	f.Int64Var(&from, "from", 0, "with -w: resume after this changelog sequence")
	f.StringVar(&generation, "generation", "", "with -w: the history generation --from was read under")
	f.IntVar(&limit, "limit", 0, "maximum records to return")
	f.StringVar(&orderBy, "order-by", "", `order, e.g. "at:desc,createdAt"`)
	f.StringVar(&after, "after", "", "opaque keyset cursor from a previous page's \"next cursor\" line; resent verbatim")
	f.StringSliceVar(&expand, "expand", nil, "reference properties whose referents ride along (comma-separated), printed after the page in -o yaml/json")
	f.StringVar(&referencing, "referencing", "", "only records pointing at this one, as <kind>/<id>")
	return cmd
}

func (a *app) getOne(ctx context.Context, cl *client, col collection, id, output string) error {
	e, meta, err := cl.get(ctx, col.pkg(), col.Name, id)
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
//
// expanded says --expand was asked, and it decides the JSON SHAPE rather than
// the page's `included` block doing so: a page whose expansions all dangled
// carries none, and the flag must not change the output's shape run to run.
func (a *app) printRecords(ctx context.Context, col collection, page *substrate.Page, output string, expanded bool) error {
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
		// The referents follow the page as further documents, sorted by their
		// record path so the stream is stable.
		if err := printDocuments(a.out, append(page.Records, includedRecords(page.Included)...)); err != nil {
			return err
		}
	case "json":
		// The JSON of a --- separated stream is the array of its manifests;
		// the cursor still goes to stderr, as it does for yaml. With --expand
		// the array becomes the `records` of an object whose `included` is
		// keyed the way the wire keys it, by record path.
		docs := make([]any, 0, len(page.Records))
		for _, e := range page.Records {
			docs = append(docs, documentOf(e, nil))
		}
		var body any = docs
		if expanded {
			included := make(map[string]any, len(page.Included))
			for path, e := range page.Included {
				included[path] = documentOf(e, nil)
			}
			body = map[string]any{"records": docs, "included": included}
		}
		if err := printJSON(a.out, body); err != nil {
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

// includedRecords is a page's `included` block as a list, in record-path
// order, so what was a map on the wire prints the same way twice.
func includedRecords(included map[string]*substrate.Record) []*substrate.Record {
	paths := make([]string, 0, len(included))
	for path := range included {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]*substrate.Record, 0, len(paths))
	for _, path := range paths {
		out = append(out, included[path])
	}
	return out
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
		len(f.Properties) == 0 && len(f.Labels) == 0 && f.Deleted == nil && f.Referencing == nil
}
