package commands

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

// syncPath is the synchronization read: cross-kind, so at the version root
// beside /records and /catalog, not under any one kind.
const syncPath = "/api/v1/sync/status"

func (a *app) syncCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Inspect the synchronization of every connected account",
		Long: `A provider's account kind binds the core sync trait, and its sync
function reports through the trait's properties: the state, the last run,
the request that asks for the next one, and the progress per stream. The
trigger dispatcher stamps a record-sourced delivery's start, finish and
park onto the same record. status reads every such record joined with the
record triggers on its kind. To ask for a run, patch syncRequestedAt on
the account; to stop one, patch syncPaused.`,
	}
	cmd.AddCommand(a.syncStatusCommand())
	return cmd
}

func (a *app) syncStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Per-account sync state, message, last run, request, streams, parked and lagging triggers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			var res substrate.OperationalList[substrate.SyncStatus]
			if err := cl.do(cmd.Context(), http.MethodGet, syncPath, nil, nil, &res); err != nil {
				return err
			}
			tw := newTable(a.out)
			fmt.Fprintln(tw, "KIND\tID\tSTATE\tPAUSED\tLAST\tREQUESTED\tSTREAMS\tPARKED\tLAG\tMESSAGE")
			for _, s := range res.Items {
				last := ""
				if s.LastSyncedAt != nil {
					last = humanAge(a.now(), *s.LastSyncedAt)
				}
				var parked, lag int64
				for _, tr := range s.Triggers {
					parked += tr.Parked
					lag += tr.Lag
				}
				message := s.Message
				if s.State == substrate.SyncStateErroring && s.Error != "" {
					message = s.Error
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%t\t%s\t%s\t%s\t%d\t%d\t%s\n",
					s.Kind, s.ID, s.State, s.Paused, last, syncRequest(s), syncStreams(s), parked, lag, truncate(message, 60))
			}
			return tw.Flush()
		},
	}
}

// syncRequest renders the request pair as one word: nothing when nothing was
// asked, `pending` while the sync has not acknowledged the ask, `served`
// once it has.
func syncRequest(s substrate.SyncStatus) string {
	switch {
	case s.RequestedAt == nil:
		return ""
	case s.RequestedAck != nil && !s.RequestedAck.Before(*s.RequestedAt):
		return "served"
	default:
		return "pending"
	}
}

// syncStreams renders the per-stream slice as `name:pending`, sorted, so a
// multi-stream account reads as one cell.
func syncStreams(s substrate.SyncStatus) string {
	if len(s.Streams) == 0 {
		return ""
	}
	names := make([]string, 0, len(s.Streams))
	for name := range s.Streams {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		st := s.Streams[name]
		part := name
		if st.State != "" {
			part += "=" + st.State
		}
		if st.Pending > 0 {
			part += fmt.Sprintf("(%d pending)", st.Pending)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}
