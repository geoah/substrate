package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

func (a *app) patchCommand() *cobra.Command {
	var (
		authority  string
		states     []string
		labels     []string
		properties []string
		raw        string
	)
	cmd := &cobra.Command{
		Use:   "patch <plural> <id>",
		Short: "Patch a record — state transitions, labels, properties",
		Long: `Patch a record in place. State transitions only travel this way
("complete a task" is --state status=done; apply/put refuse to move a state).

A state is a property, so --state and --prop write to the same block; --state is
how you say that the value you are writing is a declared move, and the engine
checks the transition against the state machine either way. Maps merge
key-wise; a raw -p patch may use null values to delete keys.`,
		Example: `  substratectl patch tasks t9 --state status=done
  substratectl patch tasks t9 --prop description="rack layout"
  substratectl patch people 9f2k --label owner/pinned=true
  substratectl patch tasks t9 -p '{"properties":{"description":null}}'`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			col, err := a.resolveCollection(ctx, args[0], authority)
			if err != nil {
				return err
			}
			var in substrate.PatchInput
			if raw != "" {
				if err := json.Unmarshal([]byte(raw), &in); err != nil {
					return fmt.Errorf("parse -p patch: %w", err)
				}
			}
			// A transition is written into the properties map like any other
			// value — as a string, always: a state is named, never typed.
			for _, kv := range states {
				k, v, err := splitKV(kv)
				if err != nil {
					return fmt.Errorf("--state: %w", err)
				}
				if in.Properties == nil {
					in.Properties = map[string]any{}
				}
				in.Properties[k] = v
			}
			if len(labels) > 0 {
				if in.Labels == nil {
					in.Labels = map[string]any{}
				}
				for _, kv := range labels {
					k, v, err := splitKV(kv)
					if err != nil {
						return fmt.Errorf("--label: %w", err)
					}
					in.Labels[k] = scalarValue(v)
				}
			}
			for _, kv := range properties {
				k, v, err := splitKV(kv)
				if err != nil {
					return fmt.Errorf("--prop: %w", err)
				}
				if in.Properties == nil {
					in.Properties = map[string]any{}
				}
				in.Properties[k] = scalarValue(v)
			}
			if in.Properties == nil && in.Labels == nil && in.Annotations == nil &&
				len(in.AddFinalizers) == 0 && len(in.RemoveFinalizers) == 0 {
				return fmt.Errorf("nothing to patch: pass --state, --label, --prop, or -p")
			}
			cl, err := a.client()
			if err != nil {
				return err
			}
			e, err := cl.patch(ctx, col.Authority, col.Name, args[1], in)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "%s patched (version %d)\n", col.ref(e.ID), e.Version)
			// Where the record stands afterwards, which is the answer a transition
			// was asking for. Which properties are states is the declaration's to
			// say, so a registry the CLI cannot reach costs the line, not the patch.
			if names := a.statesFor(ctx, col); len(names) > 0 {
				if line := joinStates(e.Properties, names); line != "-" {
					fmt.Fprintf(a.out, "  states: %s\n", strings.ReplaceAll(line, ",", " "))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&authority, "authority", "g", "", "kind authority for a bare plural")
	cmd.Flags().StringArrayVar(&states, "state", nil, "state transition name=state (repeatable)")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label key=value (repeatable)")
	cmd.Flags().StringArrayVar(&properties, "prop", nil, "property key=value (repeatable)")
	cmd.Flags().StringVarP(&raw, "patch", "p", "", "raw JSON PatchInput, merged under the flags")
	return cmd
}

func splitKV(kv string) (string, string, error) {
	k, v, ok := strings.Cut(kv, "=")
	if !ok || k == "" {
		return "", "", fmt.Errorf("want key=value, got %q", kv)
	}
	return k, v, nil
}
