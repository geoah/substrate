package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func (a *app) repositoryReindexCommand() *cobra.Command {
	var observed bool
	cmd := &cobra.Command{
		Use:   "reindex <repository> --observed",
		Short: "Queue the background re-derivation of a repository's observed index",
		Long: `Queue every record of every kind that binds the observed trait for the
background pass that derives the observed index again: its segments, its
rollup rows in every tier its metrics name, and its latest rows.

The index is derived from the records and never read by verify or the export,
so this changes no record and appends nothing to the changelog. It is the
repair for an index a fault left behind its records, short of a rebuild,
which replays the whole changelog. The server logs the pass's progress as it
does for a reshaped kind.

STOP THE SERVER FIRST. The command opens the repository as its changelog
writer, and a running server holds that lock; it refuses rather than queue
work that server would not see before its next restart. The pass starts here
and stops when the command exits; the server resumes it at its next start.

--observed names the index to re-derive, the one this command knows.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !observed {
				return errors.New("name the index to re-derive: --observed")
			}
			svc, err := a.openEngineExclusive(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			report, err := svc.ReindexObserved(cmd.Context(), args[0])
			if err != nil {
				return lockHint(err)
			}
			if len(report.Kinds) == 0 {
				fmt.Fprintf(a.out, "repository %s holds no observed record; nothing queued\n", report.Repository)
				return nil
			}
			fmt.Fprintf(a.out, "repository %s: observed index queued for re-derivation\n", report.Repository)
			fmt.Fprintf(a.out, "  kinds:   %s\n", strings.Join(report.Kinds, ", "))
			fmt.Fprintf(a.out, "  records: %d\n", report.Records)
			fmt.Fprintf(a.out, "the server re-derives them in the background at its next start\n")
			return nil
		},
	}
	cmd.Flags().BoolVar(&observed, "observed", false, "re-derive the observed index (segments, rollups and latest rows)")
	return cmd
}
