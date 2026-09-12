package commands

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

// searchCommand is the ranked read: the records route with `q`, answering
// records in rank order with each one's per-arm scores beside it. There is no
// cursor and no page to resume; a ranking opens no snapshot.
func (a *app) searchCommand() *cobra.Command {
	var (
		output string
		kinds  []string
		mode   string
		limit  int
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Rank records against a query",
		Long: `Rank the repository's records against a query.

--mode picks the arm: lexical (full text), semantic (embeddings) or hybrid,
the default, which fuses both. --kinds narrows the ranking to the kinds named,
each qualified or bare and resolved the way "get" resolves one; without it
every kind is a candidate. --limit caps the hits.

The table prints one hit per line with its raw per-arm scores: ts_rank for
the lexical arm, cosine similarity for the semantic one, "-" where an arm
did not rank it. -o yaml and -o json print the ranking as the server answers
it — the records as manifests under "records", their scores under "scores"
keyed by <kind>/<id>, and "pending", the number of values the semantic index
is still embedding: non-zero means the ranking covers a partial index.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := a.client()
			if err != nil {
				return err
			}
			q := url.Values{}
			q.Set("q", args[0])
			if mode != "" {
				switch substrate.SearchMode(strings.ToLower(mode)) {
				case substrate.SearchLexical, substrate.SearchSemantic, substrate.SearchHybrid:
				default:
					return fmt.Errorf("unknown --mode %q: use hybrid, lexical or semantic", mode)
				}
				q.Set("mode", strings.ToLower(mode))
			}
			if limit > 0 {
				q.Set("first", strconv.Itoa(limit))
			}
			if len(kinds) > 0 {
				refs := make([]string, 0, len(kinds))
				for _, k := range kinds {
					col, err := a.resolveCollection(ctx, k, "")
					if err != nil {
						return err
					}
					refs = append(refs, col.pkg()+"/"+col.Name)
				}
				if err := setFilterKinds(q, refs...); err != nil {
					return err
				}
			}
			page, err := cl.search(ctx, q)
			if err != nil {
				return err
			}
			switch output {
			case "", "table":
				return printRankedTable(a.out, page)
			case "yaml":
				b, err := marshalDocument(rankedDocument(page))
				if err != nil {
					return err
				}
				_, err = a.out.Write(b)
				return err
			case "json":
				return printJSON(a.out, rankedDocument(page))
			}
			return fmt.Errorf("unknown output format %q: use table, yaml or json", output)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&output, "output", "o", "", "output format: table|yaml|json")
	f.StringSliceVar(&kinds, "kinds", nil, "only these kinds (comma-separated; qualified or bare)")
	f.StringVar(&mode, "mode", "", "hybrid (default), lexical or semantic")
	f.IntVar(&limit, "limit", 0, "maximum hits to return")
	return cmd
}

// rankedDocument is the ranked page with its records rendered as manifests,
// so -o yaml/json print the shape the wire has and each hit applies back the
// way a `get` document does.
func rankedDocument(page *substrate.RankedPage) map[string]any {
	docs := make([]any, 0, len(page.Records))
	for _, e := range page.Records {
		docs = append(docs, documentOf(e, nil))
	}
	scores := page.Scores
	if scores == nil {
		scores = map[string]substrate.Scores{}
	}
	return map[string]any{"records": docs, "scores": scores, "pending": page.Pending}
}

func printRankedTable(w io.Writer, page *substrate.RankedPage) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "KIND\tID\tTITLE\tLEXICAL\tSEMANTIC")
	for _, e := range page.Records {
		s := page.Scores[e.Kind+"/"+e.ID]
		title := dash(truncate(strings.ReplaceAll(recordTitle(e), "\n", " "), 60))
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.Kind, e.ID, title, score(s.Lexical), score(s.Semantic))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if page.Pending > 0 {
		fmt.Fprintf(w, "# %d values still embedding; the semantic arm ranked a partial index\n", page.Pending)
	}
	return nil
}

// score renders one arm's raw score, "-" where the arm did not rank the hit.
func score(v float64) string {
	if v == 0 {
		return "-"
	}
	return strconv.FormatFloat(v, 'f', 3, 64)
}
