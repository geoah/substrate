package commands

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

// pathVocabularyTakeover is the owner's takeover of a package whose row still
// names the app that declared it under an earlier server's `declare:` scope
// (decision 0205, retired by 0214).
const pathVocabularyTakeover = apiPrefix + "/vocabulary/takeover"

func (a *app) takeoverCommand() *cobra.Command {
	var confirm string
	cmd := &cobra.Command{
		Use:   "takeover <authority>/<package> --confirm <authority>/<package>",
		Short: "Make a package an app declared yours by name; requires --confirm",
		Long: `A package an app declared under an earlier server's declare: scope still names
that app on its package row. Its declarations are already yours to change, as
every package of your own is; takeover rewrites the row so it names you as its
declarer and the app it was taken from, and nothing else moves: no kind, no
record, no token. It cannot be handed back. --confirm repeats the package
identity.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pkg := args[0]
			if confirm != pkg {
				return fmt.Errorf("takeover makes %s yours for good — confirm with --confirm %s", pkg, pkg)
			}
			cl, err := a.client()
			if err != nil {
				return err
			}
			var row substrate.Record
			body := map[string]any{"package": pkg, "confirm": confirm}
			if err := cl.do(cmd.Context(), http.MethodPost, pathVocabularyTakeover, nil, body, &row); err != nil {
				return err
			}
			from := "an app"
			if f, _ := row.Properties[substrate.PackageTakenOverFrom].(string); f != "" {
				from = f
			}
			fmt.Fprintf(a.out, "package %s: taken over from %s, declared by %v\n", pkg, from, row.Properties[substrate.PackageDeclaredBy])
			return nil
		},
	}
	cmd.Flags().StringVar(&confirm, "confirm", "", "the package identity again, to confirm the takeover")
	return cmd
}
