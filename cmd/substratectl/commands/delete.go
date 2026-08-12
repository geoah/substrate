package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (a *app) deleteCommand() *cobra.Command {
	var authority string
	cmd := &cobra.Command{
		Use:   "delete <plural> <id>",
		Short: "Soft-delete a record",
		Long: `Delete a record. Deletion is soft and cooperative: the record is
tombstoned and hard deletion waits for its finalizers to be released.`,
		Args: cobra.ExactArgs(2),
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
			e, err := cl.delete(ctx, col.Authority, col.Plural, args[1])
			if err != nil {
				return err
			}
			id := args[1]
			if e.ID != "" {
				id = e.ID
			}
			fmt.Fprintf(a.out, "%s deleted\n", col.ref(id))
			if len(e.Finalizers) > 0 {
				fmt.Fprintf(a.out, "  waiting on finalizers: %v\n", e.Finalizers)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&authority, "authority", "g", "", "type authority for a bare plural")
	return cmd
}
