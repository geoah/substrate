package commands

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

// bundlesPath is a bundle verb, behind the reserved segment: an empty id
// addresses the collection (/api/v1/{core}/bundle/-/status), an id addresses one
// bundle (/api/v1/{core}/bundle/{id}/-/enable). The segment is what keeps a
// bundle whose id is `status` addressable.
func bundlesPath(id string, verb ...string) string {
	if id == "" {
		return collectionPath(coreAuthority, "bundle") + "/" + verbSegment + "/" + strings.Join(verb, "/")
	}
	return recordVerbPath(coreAuthority, "bundle", id, verb...)
}

func (a *app) bundleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "bundle",
		Short:   "Inspect and drive installed bundles",
		Aliases: []string{"bundles"},
		Long: `Bundles are the install unit: one atomic apply of a whole closure (types,
traits, mappings, functions, agents) under an owned authority. Lifecycle is
three verbs, walked in order — disable stops execution reversibly and keeps
the data and schema; purge is the explicit, separately confirmed deletion of
the authority's data through the finalizer flow (refused while running, so
disable first); uninstall tears down the schema and callables (refused while
data lives, so purge first). Install and upgrade are ` + "`substratectl apply`" + ` of the
closure. connect starts the host OAuth flow for an account record.`,
	}
	cmd.AddCommand(
		a.bundleListCommand(),
		a.bundleStatusCommand(),
		a.bundleVerbCommand("disable", "Stop a bundle's execution (reversible): triggers pause, functions refuse, accounts freeze"),
		a.bundleVerbCommand("enable", "Reverse a disable; backlogged triggers resume from their cursors"),
		a.bundleUninstallCommand(),
		a.bundlePurgeCommand(),
		a.bundleConnectCommand(),
	)
	return cmd
}

func (a *app) bundleListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Every installed bundle's lifecycle, configuration state and counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			var res struct {
				Bundles []substrate.BundleStatus `json:"bundles"`
			}
			if err := cl.do(cmd.Context(), http.MethodGet, bundlesPath("", "status"), nil, nil, &res); err != nil {
				return err
			}
			tw := newTable(a.out)
			fmt.Fprintln(tw, "ID\tINSTALLED\tENABLED\tSETUP\tACCOUNTS\tFUNCTIONS\tKINDS\tRECORDS")
			for _, b := range res.Bundles {
				fmt.Fprintf(tw, "%s\t%t\t%t\t%s\t%d\t%d\t%d\t%d\n",
					b.ID, b.Installed, b.Enabled, setupSummary(b), b.Accounts, b.Functions, b.Kinds, b.LiveRecords)
			}
			return tw.Flush()
		},
	}
}

func (a *app) bundleStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <id>",
		Short: "One bundle's computed runtime state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			var st substrate.BundleStatus
			if err := cl.do(cmd.Context(), http.MethodGet, bundlesPath(args[0], "status"), nil, nil, &st); err != nil {
				return err
			}
			printBundleStatus(a, st)
			return nil
		},
	}
}

func printBundleStatus(a *app, st substrate.BundleStatus) {
	fmt.Fprintf(a.out, "bundle:      %s\n", st.ID)
	fmt.Fprintf(a.out, "installed:   %t\n", st.Installed)
	fmt.Fprintf(a.out, "enabled:     %t\n", st.Enabled)
	if st.Quarantined {
		fmt.Fprintf(a.out, "quarantined: %s — re-install the bundle to clear it\n", st.QuarantineReason)
	}
	for _, in := range st.Inputs {
		if in.Record != "" {
			fmt.Fprintf(a.out, "input:       %s → %s/%s (%s)\n", in.Name, in.Kind, in.Record, in.Via)
		} else {
			fmt.Fprintf(a.out, "input:       %s (%s) unresolved\n", in.Name, in.Kind)
		}
	}
	for _, s := range st.Setup {
		fmt.Fprintf(a.out, "setup:       [%s] %s\n", s.Code, s.Message)
	}
	fmt.Fprintf(a.out, "accounts:    %d\n", st.Accounts)
	fmt.Fprintf(a.out, "functions:   %d\n", st.Functions)
	fmt.Fprintf(a.out, "kinds:       %d\n", st.Kinds)
	fmt.Fprintf(a.out, "records:    %d\n", st.LiveRecords)
}

// setupSummary compresses a status's setup list for the table: "ready", or
// the item count.
func setupSummary(st substrate.BundleStatus) string {
	if len(st.Setup) == 0 {
		return "ready"
	}
	return fmt.Sprintf("%d steps", len(st.Setup))
}

func (a *app) bundleVerbCommand(verb, short string) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			var st substrate.BundleStatus
			if err := cl.do(cmd.Context(), http.MethodPost, bundlesPath(args[0], verb), nil, map[string]any{}, &st); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "bundle %s: %s applied\n", args[0], verb)
			printBundleStatus(a, st)
			return nil
		},
	}
}

// bundleUninstallCommand is not a bundleVerbCommand: uninstall tears the
// bundle row down, so there is no status to decode — the server acks.
func (a *app) bundleUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall <id>",
		Short: "Tear down the schema, callables and runtime registration; refused while live data remains (purge first)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			var res struct {
				Uninstalled bool `json:"uninstalled"`
			}
			if err := cl.do(cmd.Context(), http.MethodPost, bundlesPath(args[0], "uninstall"), nil, map[string]any{}, &res); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "bundle %s: uninstalled\n", args[0])
			return nil
		},
	}
}

func (a *app) bundlePurgeCommand() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "purge <id>",
		Short: "DELETE the bundle's authority data (soft delete + finalizers + GC); requires --yes",
		Long: `Purge tombstones every live data record in the bundle's owned authority. It runs
BEFORE uninstall — uninstall is refused while data lives — and is separately
confirmed with --yes, refused while the bundle is still running (disable it
first). Provider tokens are revoked through the finalizer flow before GC
collects.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("purge deletes data — confirm with --yes")
			}
			cl, err := a.client()
			if err != nil {
				return err
			}
			var res struct {
				Purged int `json:"purged"`
			}
			if err := cl.do(cmd.Context(), http.MethodPost, bundlesPath(args[0], "purge"), nil, map[string]any{}, &res); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "bundle %s: %d records tombstoned (finalizers and GC take it from here)\n", args[0], res.Purged)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the deletion")
	return cmd
}

func (a *app) bundleConnectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "connect <account-record-id>",
		Short: "Start the host OAuth flow for an account record; prints the consent URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			var res struct {
				URL string `json:"url"`
			}
			body := map[string]any{"record": args[0]}
			if err := cl.do(cmd.Context(), http.MethodPost, verbPath("oauth", "start"), nil, body, &res); err != nil {
				return err
			}
			fmt.Fprintln(a.out, res.URL)
			return nil
		},
	}
}
