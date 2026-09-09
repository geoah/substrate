package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/engine"
)

func (a *app) repositoryRotateGenerationCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate-generation <repository>",
		Short: "Mint a new history generation, so every saved change cursor resets once",
		Long: `Mint a new history generation for a repository and store it on its row.

A change cursor is a seq under a history generation: a client resumes a watch
or a forward read with the pair, and the server refuses a cursor from a
generation it does not hold instead of skipping writes. Importing a repository
directory into a database that has no row for it mints a generation by itself.
Restoring a DATABASE DUMP does not: the row comes back with the generation the
dump held, while the changelog may be shorter than the one clients saved
cursors against. Run this once per repository after restoring a dump, with or
without its directory, and every client re-lists at its next resume.

STOP THE SERVER FIRST. A running server holds the repository's directory lock
and has the old generation cached; the command refuses rather than leave the
two disagreeing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineExclusive(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			r, ok := svc.(engine.GenerationRotator)
			if !ok {
				return seamMissing("RotateHistoryGeneration")
			}
			report, err := r.RotateHistoryGeneration(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "repository %s: history generation %s -> %s; every saved change cursor now re-lists once\n",
				report.Repository, report.Previous, report.Generation)
			return nil
		},
	}
}
