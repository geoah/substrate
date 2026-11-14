package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
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
so the next put at the id is a fresh record, and erases its history, and
that of what asks to change it or was merged into it, which nothing brings
back; what it owns (onDelete: cascade) is tombstoned for the sweep, its
history kept until old deletions are forgotten. A mapping source resolves its
subject again only when that put leaves the subject slot out; a put that
names the slot keeps that subject. A record a finalizer holds refuses --purge.`,
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
				printErasure(a, e.Erasure)
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

// printErasure says what a permanent delete purged with the record and,
// while its erasure waits, what it waits on.
func printErasure(a *app, e *substrate.Erasure) {
	if e == nil {
		return
	}
	for _, p := range e.Purged {
		fmt.Fprintf(a.out, "  purged with it: %s\n", p)
	}
	if e.Tombstoned > 0 {
		fmt.Fprintf(a.out, "  what it owned (%d records) is deleted with it, and keeps its history until old deletions are forgotten\n", e.Tombstoned)
	}
	if e.Pending && e.Held != "" {
		fmt.Fprintf(a.out, "  erasure waits: %s\n", e.Held)
	}
	for _, w := range e.Waits {
		fmt.Fprintf(a.out, "  erasure waits for trigger %s, at seq %d\n", w.Trigger, w.Cursor)
	}
}
