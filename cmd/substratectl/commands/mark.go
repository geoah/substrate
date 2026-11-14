package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// pinCommand and releaseCommand are the two actions on a property's mark
// (decision 0284): pin keeps the value against sync, release hands it back.
func (a *app) pinCommand() *cobra.Command {
	return a.markCommand("pin", "Keep properties' values against sync",
		`Pin properties of a record: sync leaves their values alone, and what each
source record would write instead shows beside them as an alternative
(get -o yaml, status.properties). A write by a person, an app, a function or
an agent pins what it writes already; pin keeps a value sync wrote. It works
on a required property, and it is recorded in the record's history.`)
}

func (a *app) releaseCommand() *cobra.Command {
	return a.markCommand("release", "Hand properties back to sync",
		`Release pinned properties of a record: sync picks each value again among
what its source records offer, at once. It works on a required property,
which keeps its value where no source offers one, and it is recorded in the
record's history.

To write the next release of a package this repository publishes, use
substratectl publish <authority>/<package> --release.`)
}

func (a *app) markCommand(action, short, long string) *cobra.Command {
	return &cobra.Command{
		Use:   action + " <kind> <id> <property>...",
		Short: short,
		Long:  long,
		Args:  cobra.MinimumNArgs(3),
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
			e, marks, err := cl.mark(ctx, action, col.pkg(), col.Name, args[1], args[2:])
			if err != nil {
				return err
			}
			id := args[1]
			if e.ID != "" {
				id = e.ID
			}
			var held []string
			for _, name := range args[2:] {
				state := "released"
				if marks[name].Pinned {
					state = "pinned"
				}
				held = append(held, name+" "+state)
			}
			fmt.Fprintf(a.out, "%s: %s\n", col.ref(id), strings.Join(held, ", "))
			return nil
		},
	}
}
