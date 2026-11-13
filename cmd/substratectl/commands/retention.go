package commands

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

// retentionCommand shows how long each kind's history is kept in the
// repository (decision 0282): what the declaration states, the repository's
// override and the one that applies. An override is a record of
// `substrate.reamde.dev/core/retention`, written with apply and removed with
// delete like any other.
func (a *app) retentionCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "retention [kind]",
		Short: "Show how long each kind's history is kept",
		Long: `Show how long each kind's history is kept past each record's latest state:
all, none, or an ISO 8601 duration such as P30D. DECLARED is what the kind's
declaration states, its own key else its package's (empty reads as all);
OVERRIDE is this repository's substrate.reamde.dev/core/retention record for
the kind, if one stands; EFFECTIVE is the one that applies.

Nothing drops an entry by it yet: the compaction that does is a later release.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			var list substrate.OperationalList[substrate.KindRetention]
			if err := cl.do(cmd.Context(), http.MethodGet, apiPrefix+"/retention", nil, nil, &list); err != nil {
				return err
			}
			rows := list.Items
			if len(args) == 1 {
				rows = nil
				for _, r := range list.Items {
					if r.Kind == args[0] {
						rows = append(rows, r)
					}
				}
				if len(rows) == 0 {
					return fmt.Errorf("no kind %q is declared here: `substratectl kinds` lists them", args[0])
				}
			}
			switch output {
			case "json":
				return printJSON(a.out, rows)
			case "", "table":
			default:
				return fmt.Errorf("unknown output format %q: use table or json", output)
			}
			tw := newTable(a.out)
			fmt.Fprintln(tw, "KIND\tDECLARED\tOVERRIDE\tEFFECTIVE")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Kind, dash(r.Declared), dash(r.Override), r.Effective)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: table|json")
	return cmd
}
