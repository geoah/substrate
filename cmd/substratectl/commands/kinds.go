package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// kindsCommand is the registry table: every kind this repository declares,
// read from the one canonical collection (`/api/v1/core.substrate.reamde.dev/kind`).
// `substratectl get kinds` reads the same collection as ordinary records; this
// command is the registry VIEW of it, with the declaration's own columns.
func (a *app) kindsCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:     "kinds",
		Short:   "List declared kinds",
		Aliases: []string{"kind"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kinds, err := a.types(cmd.Context())
			if err != nil {
				return err
			}
			switch output {
			case "", "table":
				return printKindsTable(a.out, kinds)
			case "json":
				return printJSON(a.out, kinds)
			}
			return fmt.Errorf("unknown output format %q: use table or json", output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: table|json")
	return cmd
}
