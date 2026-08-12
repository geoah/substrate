package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// parseEdgeTarget parses an edge target argument into an EdgeRef. A bare `id`
// is the shorthand the server accepts where the edge declaration pins a single
// target type; the qualified form `type.authority:id` names the full identity for
// a `to: any` edge (or to disambiguate). The id is everything after the first
// colon, so an id that itself contains colons survives.
func parseEdgeTarget(arg string) (substrate.EdgeRef, error) {
	typeref, id, ok := strings.Cut(arg, ":")
	if !ok {
		if arg == "" {
			return substrate.EdgeRef{}, fmt.Errorf("empty edge target")
		}
		return substrate.EdgeRef{ID: arg}, nil
	}
	if id == "" {
		return substrate.EdgeRef{}, fmt.Errorf("edge target %q has no id after the colon", arg)
	}
	if !vocabulary.Qualified(typeref) {
		return substrate.EdgeRef{}, fmt.Errorf("edge target kind %q must be a kind reference, <authority>/<name>", typeref)
	}
	return substrate.EdgeRef{Kind: typeref, ID: id}, nil
}

func (a *app) linkCommand() *cobra.Command {
	var (
		authority  string
		properties []string
	)
	cmd := &cobra.Command{
		Use:   "link <plural> <id> <rel> <target>",
		Short: "Add an outgoing edge",
		Long: `Add an outgoing edge {rel} from a source record to a target.

The target is a bare id where the edge declaration pins a single target kind,
or the qualified form kind.authority:id for a to:any edge. Edge properties are set
with --prop key=value.`,
		Example: `  substratectl link tasks t9 project pr3
  substratectl link tasks t9 source messaging.substrate.reamde.dev/conversationmessage:m7
  substratectl link people 9f2k memberOf c3`,
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			col, err := a.resolveCollection(ctx, args[0], authority)
			if err != nil {
				return err
			}
			to, err := parseEdgeTarget(args[3])
			if err != nil {
				return err
			}
			var props map[string]any
			for _, kv := range properties {
				k, v, err := splitKV(kv)
				if err != nil {
					return fmt.Errorf("--prop: %w", err)
				}
				if props == nil {
					props = map[string]any{}
				}
				props[k] = scalarValue(v)
			}
			cl, err := a.client()
			if err != nil {
				return err
			}
			e, err := cl.link(ctx, col.Authority, col.Plural, args[1], args[2], to, props)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "%s linked %s -> %s (version %d)\n", col.ref(e.ID), args[2], edgeTargetLabel(to), e.Version)
			return nil
		},
	}
	cmd.Flags().StringVarP(&authority, "authority", "g", "", "type authority for a bare plural")
	cmd.Flags().StringArrayVar(&properties, "prop", nil, "edge property key=value (repeatable)")
	return cmd
}

func (a *app) unlinkCommand() *cobra.Command {
	var authority string
	cmd := &cobra.Command{
		Use:   "unlink <plural> <id> <rel> <target>",
		Short: "Remove an outgoing edge",
		Long: `Remove an outgoing edge {rel} from a source record to a target.

The target syntax matches ` + "`link`" + `: a bare id, or type.authority:id.`,
		Example: `  substratectl unlink tasks t9 project pr3
  substratectl unlink tasks t9 source messaging.substrate.reamde.dev/conversationmessage:m7`,
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			col, err := a.resolveCollection(ctx, args[0], authority)
			if err != nil {
				return err
			}
			to, err := parseEdgeTarget(args[3])
			if err != nil {
				return err
			}
			cl, err := a.client()
			if err != nil {
				return err
			}
			e, err := cl.unlink(ctx, col.Authority, col.Plural, args[1], args[2], to)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "%s unlinked %s -> %s (version %d)\n", col.ref(e.ID), args[2], edgeTargetLabel(to), e.Version)
			return nil
		},
	}
	cmd.Flags().StringVarP(&authority, "authority", "g", "", "type authority for a bare plural")
	return cmd
}

func edgeTargetLabel(to substrate.EdgeRef) string {
	if id := to.Identity(); id != "" {
		return id + "/" + to.ID
	}
	return to.ID
}
