package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

func (a *app) triggerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "trigger",
		Short:   "Inspect and drive substrate triggers",
		Aliases: []string{"triggers"},
		Long: `Triggers are data records (core.substrate.reamde.dev) binding one source —
a record subscription, an RRULE schedule, or a webhook wake — to one
callable. Each record-sourced trigger owns a changelog cursor; status
shows where every trigger sits, replay rewinds a cursor, run synthesizes
a single delivery, wake scans a trigger immediately, and parked lists the
deliveries a trigger gave up on. The trigger rows themselves are ordinary
records: get/apply/delete them like any other.`,
	}
	cmd.AddCommand(
		a.triggerStatusCommand(),
		a.triggerReplayCommand(),
		a.triggerRunCommand(),
		a.triggerWakeCommand(),
		a.triggerParkedCommand(),
		a.triggerRetryCommand(),
	)
	return cmd
}

func (a *app) triggerStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Per-trigger kind, callable, cursor, lag, last fire and parked count",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			var res struct {
				Triggers []substrate.TriggerStatus `json:"triggers"`
			}
			if err := cl.do(cmd.Context(), http.MethodGet, triggersPath("", "status"), nil, nil, &res); err != nil {
				return err
			}
			tw := newTable(a.out)
			fmt.Fprintln(tw, "ID\tKIND\tCALLABLE\tENABLED\tCURSOR\tHEAD\tLAG\tLASTFIRE\tPARKED\tERROR")
			for _, t := range res.Triggers {
				lastFire := ""
				if t.LastFire != nil {
					lastFire = humanAge(a.now(), *t.LastFire)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%t\t%d\t%d\t%d\t%s\t%d\t%s\n",
					t.ID, t.Kind, t.Callable, t.Enabled, t.Cursor, t.Head, t.Lag, lastFire, t.Parked, truncate(t.Error, 60))
			}
			return tw.Flush()
		},
	}
}

func (a *app) triggerReplayCommand() *cobra.Command {
	var from int64
	cmd := &cobra.Command{
		Use:   "replay <id>",
		Short: "Reset a record-sourced trigger's cursor; delivery does the rest",
		Long: `Set a trigger's cursor to --from (default 0: everything). Effects are
idempotent by construction and identical puts are no-ops, so a full replay
converges instead of duplicating.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			body := map[string]any{"from": from}
			if err := cl.do(cmd.Context(), http.MethodPost, triggersPath(args[0], "replay"), nil, body, nil); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "trigger %s cursor set to %d\n", args[0], from)
			return nil
		},
	}
	cmd.Flags().Int64Var(&from, "from", 0, "seq to resume from (0 = replay everything)")
	return cmd
}

func (a *app) triggerRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run <id> <kind> <record-id>",
		Short: "Deliver one record's current state through a trigger, cursor untouched",
		Long: `Synthesize one delivery of a record's current state through a trigger's
callable, without moving the cursor.

A record is addressed by (kind, id) — an id alone names no record, since two
kinds may share one — so the delivery takes BOTH. The kind may be the plural or
singular the registry knows ("tasks", "task") or the full reference
("tasks.substrate.reamde.dev/task").`,
		Example: `  substratectl trigger run classify-page task t9
  substratectl trigger run classify-page tasks.substrate.reamde.dev/task t9`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cl, err := a.client()
			if err != nil {
				return err
			}
			var res struct {
				Ran int `json:"ran"`
			}
			// The wire wants the kind reference. A plural or singular the registry
			// knows is resolved here; anything else travels verbatim, because the
			// server resolves references and bare names too and its error names the
			// kind better than a guess would.
			recordKind := args[1]
			if col, err := a.resolveCollection(ctx, args[1], ""); err == nil && col.Identity != "" {
				recordKind = col.Identity
			}
			// {"type", "id"}, both: the server refuses a body missing either
			//, so an id-only call was a 400 every single time.
			body := map[string]any{"kind": recordKind, "id": args[2]}
			if err := cl.do(ctx, http.MethodPost, triggersPath(args[0], "run"), nil, body, &res); err != nil {
				return err
			}
			if res.Ran == 0 {
				fmt.Fprintf(a.out, "trigger %s ran on %s/%s: no effects (guard false or empty result)\n", args[0], recordKind, args[2])
				return nil
			}
			fmt.Fprintf(a.out, "trigger %s ran on %s/%s: effects applied\n", args[0], recordKind, args[2])
			return nil
		},
	}
}

func (a *app) triggerWakeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "wake <id>",
		Short: "Scan a trigger now: webhook fires once, record drains, schedule checks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			var res struct {
				Ran int `json:"ran"`
			}
			if err := cl.do(cmd.Context(), http.MethodPost, triggersPath(args[0], "wake"), nil, map[string]any{}, &res); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "trigger %s woke: %d deliveries applied effects\n", args[0], res.Ran)
			return nil
		},
	}
}

func (a *app) triggerParkedCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "parked <id>",
		Short: "List a trigger's parked deliveries",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			parked, err := fetchParked(cmd.Context(), cl, args[0])
			if err != nil {
				return err
			}
			tw := newTable(a.out)
			fmt.Fprintln(tw, "ID\tSEQ\tFIRE\tRECORD\tATTEMPTS\tPARKED\tERROR")
			for _, f := range parked {
				fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%d\t%s\t%s\n",
					f.ID, f.Seq, f.FireID, f.RecordID, f.Attempts, humanAge(a.now(), f.ParkedAt), truncate(f.LastError, 80))
			}
			return tw.Flush()
		},
	}
}

func (a *app) triggerRetryCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "retry <id> <parked-id>",
		Short: "Re-run one parked delivery; success deletes the row",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("parked id %q is not a number (find it with `substratectl trigger parked %s`)", args[1], args[0])
			}
			if err := cl.do(cmd.Context(), http.MethodPost,
				triggersPath(args[0], "parked", args[1], "retry"), nil, map[string]any{}, nil); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "parked delivery %d of %s retried\n", id, args[0])
			return nil
		},
	}
}

func fetchParked(ctx context.Context, cl *client, id string) ([]substrate.TriggerFailure, error) {
	var res struct {
		Parked []substrate.TriggerFailure `json:"parked"`
	}
	if err := cl.do(ctx, http.MethodGet, triggersPath(id, "parked"), nil, nil, &res); err != nil {
		return nil, err
	}
	return res.Parked, nil
}

// triggersPath is /api/v1/core.substrate.reamde.dev/trigger/…/-/{verb}. A record's
// operational verbs live AT the resource and trigger records are
// core's — the substrate maintains its own delivery plumbing, so it publishes
// it — which makes this the one spelling.
func triggersPath(id string, verb ...string) string {
	if id == "" {
		return collectionPath(coreAuthority, "trigger") + "/" + verbSegment + "/" + strings.Join(verb, "/")
	}
	return recordVerbPath(coreAuthority, "trigger", id, verb...)
}

func (a *app) functionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "function",
		Short:   "Invoke substrate functions",
		Aliases: []string{"functions", "fn"},
		Long: `Functions are pure callables (function manifests): real code invoked by
triggers, by other functions, or directly. call invokes one with arbitrary
JSON input — validated against the manifest's input schema when one is
declared — applies its effects under the function's actor, and prints the
output. Delivery bookkeeping lives on triggers: see ` + "`substratectl trigger`" + `.`,
	}
	cmd.AddCommand(a.functionCallCommand())
	return cmd
}

func (a *app) functionCallCommand() *cobra.Command {
	var inputJSON string
	cmd := &cobra.Command{
		Use:   "call <name>",
		Short: "Invoke a function with arbitrary input (mode: call)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			var input any
			if inputJSON != "" {
				if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
					return fmt.Errorf("--input is not JSON: %w", err)
				}
			}
			var res struct {
				Output  any `json:"output"`
				Effects int `json:"effects"`
			}
			if err := cl.do(cmd.Context(), http.MethodPost,
				recordVerbPath(coreAuthority, "function", args[0], "call"), nil,
				map[string]any{"input": input}, &res); err != nil {
				return err
			}
			out, err := json.MarshalIndent(map[string]any{"output": res.Output, "effects": res.Effects}, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(a.out, string(out))
			return nil
		},
	}
	cmd.Flags().StringVar(&inputJSON, "input", "", "the call input, as JSON")
	return cmd
}
