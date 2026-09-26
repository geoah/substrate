package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (a *app) deleteCommand() *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:   "delete <kind> <id>",
		Short: "Soft-delete a record",
		Long: `Delete a record. Deletion is soft and cooperative: the record is
tombstoned and hard deletion waits for its finalizers to be released.

Until the garbage collector purges the tombstone, a put at the same id
restores the record with every property it held. --purge collects it now,
so the next put at the id is a fresh record (a mapping source resolves its
subject again). A record a finalizer holds refuses --purge.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			col, err := a.resolveCollection(ctx, args[0])
			if err != nil {
				return err
			}
			cl, err := a.client()
			if err != nil {
				return err
			}
			e, err := cl.delete(ctx, col.pkg(), col.Name, args[1], purge)
			if err != nil {
				return err
			}
			id := args[1]
			if e.ID != "" {
				id = e.ID
			}
			if purge {
				fmt.Fprintf(a.out, "%s purged\n", col.ref(id))
				return nil
			}
			fmt.Fprintf(a.out, "%s deleted\n", col.ref(id))
			if len(e.Finalizers) > 0 {
				fmt.Fprintf(a.out, "  waiting on finalizers: %v\n", e.Finalizers)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "collect the record now, so a later put at the id is a fresh record")
	return cmd
}
