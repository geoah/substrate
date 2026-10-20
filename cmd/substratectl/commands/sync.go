package commands

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

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
the account; to stop one, patch syncPaused.

TOKEN is the account's OAuth sign-in: connected, or erroring with the age
of the current reason and the reason itself. erroring means the last
refresh failed. A refusal (a 4xx such as invalid_grant) needs a reconnect
(substratectl bundle connect), and asking for another run does not help. A
5xx or a timeout clears on the next good refresh. The cell is empty on a
provider that signs in with a pasted key, and empty or pending on an
account that has not finished its first consent.`,
	}
	cmd.AddCommand(a.syncStatusCommand())
	return cmd
}

func (a *app) syncStatusCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Per-account sync state, health, whether its sign-in works, message, last run, request, streams, parked and lagging triggers, and the newest parked run's reason",
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
			return printList(a, output, res.Items, func() error {
				tw := newTable(a.out)
				fmt.Fprintln(tw, "KIND\tID\tSTATE\tHEALTH\tTOKEN\tPAUSED\tLAST\tREQUESTED\tSTREAMS\tPARKED\tLAG\tMESSAGE\tLAST PARKED")
				for _, s := range res.Items {
					last := ""
					if s.LastSyncedAt != nil {
						last = humanAge(a.now(), *s.LastSyncedAt)
					}
					// PARKED is the parked deliveries of this account's sync; LAG is the
					// kind's triggers' backlog summed, which no record owns alone.
					var lag int64
					for _, tr := range s.Triggers {
						lag += tr.Lag
					}
					message := s.Message
					if s.State == substrate.SyncStateErroring && s.Error != "" {
						message = s.Error
					}
					// One line per account: a message carrying a newline would
					// break the table.
					message, _, _ = strings.Cut(message, "\n")
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%t\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
						s.Kind, s.ID, s.State, a.health(s.Health, s.FailingSince), a.signIn(s), s.Paused, last, syncRequest(s), syncStreams(s), s.Parked, lag, truncate(message, 60), a.lastParked(s))
				}
				return tw.Flush()
			})
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: table|json|yaml")
	return cmd
}

// health renders a status's health as one cell: `ok`, or `failing` and how
// long it has failed, from the failing alert's firstSeenAt. A server that
// reports no health leaves the cell empty.
func (a *app) health(health string, since *time.Time) string {
	if health != substrate.HealthFailing || since == nil {
		return health
	}
	return health + " " + humanAge(a.now(), *since)
}

// signIn renders the account's OAuth sign-in as one cell: the facility's
// tokenStatus as is, and on `erroring` the age of the current reason (the
// facility restamps tokenErrorAt only when the reason changes) and the
// reason's first line, the LAST PARKED cell's shape. The reason drops the
// flow's `oauthflow: refresh: ` prefix and is cut later than the other
// cells: its tail carries the provider's error code, which tells a refused
// grant from an outage. A kind that carries no sign-in leaves the cell empty.
func (a *app) signIn(s substrate.SyncStatus) string {
	if s.TokenStatus != "erroring" {
		return s.TokenStatus
	}
	cell := s.TokenStatus
	if s.TokenErrorAt != nil {
		cell += " " + humanAge(a.now(), *s.TokenErrorAt)
	}
	reason, _, _ := strings.Cut(s.TokenError, "\n")
	reason = strings.TrimPrefix(reason, "oauthflow: refresh: ")
	reason = strings.TrimPrefix(reason, "oauthflow: ")
	if reason != "" {
		cell += ": " + truncate(reason, signInReasonMax)
	}
	return cell
}

// signInReasonMax bounds the TOKEN cell's reason. It holds a whole
// `provider answered 400, error code "invalid_request"` with room for a
// count and a reconnect ask around it.
const signInReasonMax = 140

// lastParked renders the newest parked delivery as its age and its reason:
// a park older than the account's last completed run leaves the account's
// own state standing, so MESSAGE alone does not say why PARKED is not zero.
func (a *app) lastParked(s substrate.SyncStatus) string {
	if s.Parked == 0 || s.LastParkedError == "" {
		return ""
	}
	reason, _, _ := strings.Cut(s.LastParkedError, "\n")
	if s.LastParkedAt == nil {
		return truncate(reason, 60)
	}
	return humanAge(a.now(), *s.LastParkedAt) + ": " + truncate(reason, 60)
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
