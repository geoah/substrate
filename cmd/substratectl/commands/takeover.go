package commands

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

// pathVocabularyTakeover is the owner's takeover of a package an app keeps
// under its `declare:` scope (decision 0205).
const pathVocabularyTakeover = apiPrefix + "/vocabulary/takeover"

func (a *app) takeoverCommand() *cobra.Command {
	var confirm string
	cmd := &cobra.Command{
		Use:   "takeover <authority>/<package> --confirm <authority>/<package>",
		Short: "Make a package an app manages yours, so you can edit its kinds; requires --confirm",
		Long: `A package an app declared under its declare: scope is the app's: its package,
bundle and kind documents change only through that app, and every other apply,
put, patch, delete or uninstall of them is refused. Its records stay yours.

takeover makes the package yours. Nothing else moves: no kind, no record, no
token. The app keeps its token and its scope, and its next change to the package
is refused, so take a package over only when you mean to manage its kinds
yourself from now on. It cannot be handed back. --confirm repeats the package
identity. The answer, and the package row from then on, name the app it was
taken from.`,
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
