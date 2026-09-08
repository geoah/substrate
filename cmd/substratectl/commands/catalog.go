package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

// catalogPath is /api/v1/catalog/{id}/{verb}: the shipped closures, at the
// version root because the endpoint names no kind (decision 0033). The bundle
// id is a package reference and carries a `/`, so it is escaped as one
// segment.
func catalogPath(id, verb string) string {
	return apiPrefix + "/catalog/" + url.PathEscape(id) + "/" + verb
}

// takeBundle runs one of the two catalog doors and prints what landed. The
// server answers with the LANDED bundle's status, which for an import is the
// sample's package under this repository's own authority rather than the id
// typed. With allowDataLoss the door is asked for its preview first, and a
// lossy plan is confirmed by the hash and changelog head that preview carried
// (confirmedUpgrade): the consent covers what was shown and nothing else.
func (a *app) takeBundle(cmd *cobra.Command, id, verb, past string, allowDataLoss bool) error {
	cl, err := a.client()
	if err != nil {
		return err
	}
	var body any
	if allowDataLoss {
		confirm, err := a.confirmedUpgrade(cmd.Context(), cl, id)
		if err != nil {
			return err
		}
		if confirm != nil {
			body = map[string]any{"confirm": confirm}
		}
	}
	var taken bundleTaken
	if err := cl.do(cmd.Context(), http.MethodPost, catalogPath(id, verb), nil, body, &taken); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s %s\n", taken.ID, past)
	printBundleStatus(a, taken.BundleStatus)
	printSuggestedMappings(a, id, taken.SuggestedMappings)
	return nil
}

// confirmedUpgrade reads the catalog's preview of one bundle and, where its
// conversion plan is lossy, prints the steps that remove values and answers
// the confirmation bound to that preview (decision 0067). A bundle with no
// preview, or a lossless one, answers nil: there is nothing to consent to, and
// the door runs the plan as it stands.
func (a *app) confirmedUpgrade(ctx context.Context, cl *client, id string) (*substrate.ConversionConfirm, error) {
	var cat substrate.OperationalList[substrate.CatalogItem]
	if err := cl.do(ctx, http.MethodGet, apiPrefix+"/catalog", nil, nil, &cat); err != nil {
		return nil, err
	}
	for _, e := range cat.Items {
		if e.ID != id {
			continue
		}
		if e.Upgrade == nil || !e.Upgrade.Lossy || e.Upgrade.PlanHash == "" {
			fmt.Fprintf(a.out, "%s: the upgrade removes no values, so --allow-data-loss confirms nothing\n", id)
			return nil, nil
		}
		fmt.Fprintf(a.out, "%s: confirming plan %s at changelog seq %d, which removes values:\n", id, e.Upgrade.PlanHash, e.Upgrade.ChangelogSeq)
		for _, s := range e.Upgrade.Steps {
			if s.Lossy {
				fmt.Fprintf(a.out, "  %s\n", stepLine(s))
			}
		}
		return &substrate.ConversionConfirm{PlanHash: e.Upgrade.PlanHash, ChangelogSeq: e.Upgrade.ChangelogSeq}, nil
	}
	return nil, nil
}

// stepLine renders one conversion step the way the console does: what moves,
// on which kind, and how many live records it rewrites, with the loss named.
func stepLine(s substrate.ConversionStep) string {
	n := fmt.Sprintf("%d live record", s.Records)
	if s.Records != 1 {
		n += "s"
	}
	switch s.Step {
	case substrate.StepRename:
		return fmt.Sprintf("renames %s to %s on %s: %s rewritten", s.From, s.To, s.Kind, n)
	case substrate.StepBackfill:
		return fmt.Sprintf("backfills %s with its default on %s: %s rewritten", s.Property, s.Kind, n)
	case substrate.StepRemap:
		line := fmt.Sprintf("rewrites %s %s to %s on %s: %s rewritten", s.Property, s.From, s.To, s.Kind, n)
		if s.Lossy {
			line += " (lossy: the records holding either value become one set)"
		}
		return line
	default:
		return fmt.Sprintf("drops %s on %s: its value leaves %s (lossy: the values stay in the changelog only)", s.Property, s.Kind, n)
	}
}

// bundleTaken is the two doors' response: the landed bundle's computed status,
// plus what each SUGGESTED MAPPING the closure carries did.
type bundleTaken struct {
	substrate.BundleStatus
	SuggestedMappings []substrate.SuggestedMapping `json:"suggestedMappings,omitempty"`
}

// printSuggestedMappings says what the closure's suggested mappings did
// (decision record 0049). A sample ships one per provider it knows, onto a
// kind of its own, and the door admits only the ones that resolve here, so a
// reader who installs Linear tomorrow has to be told BOTH that importing this
// sample again is what lands the rest and what that costs: a re-import
// replaces the package (record 0048), so a kind or a property they added
// since goes with it.
//
// The ids and targets are the ones this repository holds, rehomed by the door,
// so a line names the person or task kind the reader can actually go and read.
func printSuggestedMappings(a *app, sample string, mappings []substrate.SuggestedMapping) {
	for _, m := range mappings {
		fmt.Fprintf(a.out, "mapping:     %s -> %s: %s\n", m.From, m.To, suggestedMappingLine(m, sample))
		for _, p := range m.Problems {
			fmt.Fprintf(a.out, "             %s\n", p)
		}
	}
}

// suggestedMappingLine is one mapping's state and what to do about it.
func suggestedMappingLine(m substrate.SuggestedMapping, sample string) string {
	again := fmt.Sprintf("import %s again. Re-importing replaces that package and may remove your changes.", sample)
	switch m.State {
	case substrate.SuggestedMappingLanded:
		return "landed"
	case substrate.SuggestedMappingReady:
		return "ready; " + again
	case substrate.SuggestedMappingBlocked:
		return fmt.Sprintf("blocked; upgrade %s first, then %s", m.Package, again)
	default:
		return fmt.Sprintf("waiting; install %s, then %s", m.Package, again)
	}
}

// importCommand is the SAMPLE door (decision record 0048). A sample is
// vocabulary to copy: the server rehomes the closure onto this repository's
// own authority and admits it there, so `samples.substrate.reamde.dev/tasks`
// lands as `<your authority>/tasks` and is yours to edit afterwards.
func (a *app) importCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "import <sample>",
		Short: "Copy a shipped sample under your own authority",
		Long: `Import one of the shipped SAMPLE packages into this repository.

A sample is vocabulary to copy, not vocabulary to depend on. The server
rewrites the closure's placeholder authority to the one this repository owns
before admitting it, so

  substratectl import samples.substrate.reamde.dev/tasks

lands ` + "`<your authority>/tasks/task`" + `: your kind, writable through the
API and never offered an upgrade. A sample that declares against another is
refused until that one is imported, naming what to import first.

Providers take the other door, ` + "`substratectl install`" + `, and land under
the authority that publishes them. Importing a provider is refused, naming it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.takeBundle(cmd, args[0], "import", "imported", false)
		},
	}
}

// installCommand is the PROVIDER door: the closure lands verbatim, under the
// authority that publishes it, and the publisher's next version bump is what
// the console offers as an upgrade.
func (a *app) installCommand() *cobra.Command {
	var allowDataLoss bool
	cmd := &cobra.Command{
		Use:   "install <provider>",
		Short: "Install a shipped provider under the authority that publishes it",
		Long: `Install one of the shipped PROVIDER packages into this repository.

A provider is a package its publisher owns, so it lands exactly as published:

  substratectl install providers.substrate.reamde.dev/google

Its declarations are the publisher's afterwards (` + "`source: published`" + `),
so this repository's token may not rewrite them, and each change the publisher
ships arrives as an upgrade. Re-running this command is that upgrade.

An upgrade that removes values from your records (a property the new closure
drops while records carry it, an enum value renamed onto one it keeps) is
refused until it is confirmed. ` + "`substratectl catalog`" + ` shows the plan;
--allow-data-loss reads it again, prints the steps that remove values with the
records each touches, and confirms exactly that plan: a write that lands in
between, or a plan that reads differently, is refused again. The removed
values stay in the changelog.

Samples take the other door, ` + "`substratectl import`" + `, and land under
your own authority.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.takeBundle(cmd, args[0], "install", "installed", allowDataLoss)
		},
	}
	cmd.Flags().BoolVar(&allowDataLoss, "allow-data-loss", false, "confirm the previewed upgrade plan even where it removes values from records")
	return cmd
}

// catalogRow is one line of `substratectl catalog`: a package the binary ships,
// from either read, in one shape.
type catalogRow struct {
	ID string `json:"id"`
	// Tier is the door the package takes: `seed` for the package registration
	// writes and the binary upgrades at boot, `provider` and `sample` for the
	// two catalog doors.
	Tier      string `json:"tier"`
	Installed bool   `json:"installed"`
	// Version is the version this binary ships the package at.
	Version int64 `json:"version,omitempty"`
	// Upgrade is the motion an upgrade would make here and the guard lines it
	// is refused on, when the server previewed one.
	Upgrade *substrate.BundleUpgrade `json:"upgrade,omitempty"`
}

// tierSeed is the row word for the package the binary seeds and upgrades
// itself, which is not a catalog tier: nothing installs or imports core.
const tierSeed = "seed"

// catalogCommand lists every package the binary ships and where this repository
// stands on each: the seeded core package from `GET /api/v1/vocabulary/upgrade`
// and the catalog's providers and samples from `GET /api/v1/catalog`. It is the
// one place the CLI prints an upgrade: a refused core boot upgrade used to be a
// server log line and nothing else, and a provider's blocked upgrade was on the
// wire and in the console but never in a terminal.
func (a *app) catalogCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Every shipped package: held here or not, and the upgrade this binary offers or refuses",
		Long: `List the packages this substrate's binary ships and where this repository
stands on each.

  PACKAGE    the package identity
  TIER       seed (core, written at registration and upgraded at boot),
             provider (installed under the authority that publishes it) or
             sample (imported under your own authority)
  INSTALLED  whether this repository holds it
  VERSION    the version the binary ships it at
  UPGRADE    the motion an upgrade would make here ("16 -> 17"); "blocked"
             when the server refuses it; for core, "lands at restart" when
             it is admitted and waiting for the server to start again

A blocked upgrade prints its guard lines under the table. Each names a kind, a
property and the count of live records still holding the old shape; the
upgrade lands once those records are migrated or deleted. For core that is
the boot upgrade, which runs at the server's next start and not before, so
an admitted core upgrade reads "lands at restart" until then; for a provider
it is ` + "`substratectl install <provider>`" + ` again. A sample is never
offered an upgrade: what it landed is yours.

An upgrade that rewrites records prints its steps under the table too, each
with the live records it touches. A step marked lossy removes values from the
fold (they stay in the changelog); a provider upgrade with one runs only with
` + "`substratectl install <provider> --allow-data-loss`" + `, and the boot
upgrade never runs one.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			// Never nil: `-o json` prints `[]` for a server with nothing
			// shipped, not `null`.
			rows := []catalogRow{}
			// The seeded package first. The shipped read is optional: a
			// server that previews no boot upgrade (an older binary, a
			// dataset without the seam) answers 404 or 501 and has no core
			// row, and any other refusal costs the core row alone, because
			// the catalog read still serves and is the reason the command
			// was run. A refusal that is not one of the two expected ones
			// is said once, on stderr.
			var shipped substrate.OperationalList[substrate.ShippedUpgrade]
			err = cl.do(ctx, http.MethodGet, apiPrefix+"/vocabulary/upgrade", nil, nil, &shipped)
			var ae *apiError
			switch {
			case err == nil:
				for _, s := range shipped.Items {
					// Held here when a stored package row carries a version.
					// Core always does; a second shipped package a core guard
					// withholds is listed before it has ever landed.
					up := s.Upgrade
					rows = append(rows, catalogRow{ID: s.Package, Tier: tierSeed, Installed: up.From != 0, Version: up.To, Upgrade: &up})
				}
			case errors.As(err, &ae):
				if ae.Status != http.StatusNotFound && ae.Status != http.StatusNotImplemented {
					fmt.Fprintf(a.errOut, "note: the shipped upgrade preview answered %d (%s); core is not listed\n", ae.Status, ae.Error())
				}
			default:
				return err
			}
			var cat substrate.OperationalList[substrate.CatalogItem]
			if err := cl.do(ctx, http.MethodGet, apiPrefix+"/catalog", nil, nil, &cat); err != nil {
				return err
			}
			for _, e := range cat.Items {
				rows = append(rows, catalogRow{ID: e.ID, Tier: e.Tier, Installed: e.Installed, Version: e.Version, Upgrade: e.Upgrade})
			}
			switch output {
			case "", "table":
				return printCatalogTable(a.out, rows)
			case "json":
				return printJSON(a.out, rows)
			}
			return fmt.Errorf("unknown output format %q: use table or json", output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: table|json")
	return cmd
}

// printCatalogTable is the table, then every blocked upgrade's guard lines,
// verbatim: they are the migration instructions, and a table cell cannot hold
// them.
func printCatalogTable(w io.Writer, rows []catalogRow) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "PACKAGE\tTIER\tINSTALLED\tVERSION\tUPGRADE")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%t\t%d\t%s\n", r.ID, r.Tier, r.Installed, r.Version, upgradeCell(r.Tier, r.Upgrade))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, r := range rows {
		if r.Upgrade == nil || len(r.Upgrade.Blockers) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s: the upgrade is blocked\n", r.ID)
		for _, b := range r.Upgrade.Blockers {
			fmt.Fprintf(w, "  %s\n", b)
		}
	}
	// The conversion plan, step by step: what the upgrade rewrites and how
	// many live records each step touches, so the loss a lossy step means is
	// read here before anybody confirms it (decision 0067).
	for _, r := range rows {
		if r.Upgrade == nil || len(r.Upgrade.Steps) == 0 {
			continue
		}
		switch {
		case r.Upgrade.Lossy && r.Tier == tierSeed:
			fmt.Fprintf(w, "\n%s: the upgrade rewrites %d live records and removes values; the boot upgrade never runs a lossy step, so rewrite the records it names first\n", r.ID, r.Upgrade.Work)
		case r.Upgrade.Lossy:
			fmt.Fprintf(w, "\n%s: the upgrade rewrites %d live records and removes values; confirm it with `substratectl install %s --allow-data-loss`\n", r.ID, r.Upgrade.Work, r.ID)
		default:
			fmt.Fprintf(w, "\n%s: the upgrade rewrites %d live records\n", r.ID, r.Upgrade.Work)
		}
		for _, s := range r.Upgrade.Steps {
			fmt.Fprintf(w, "  %s\n", stepLine(s))
		}
	}
	return nil
}

// upgradeCell is the UPGRADE column: the motion when the server previewed
// one, and "blocked" when it refuses. A preview that could not run has no
// motion and one blocker, so it reads "blocked" alone. The seeded package has
// a third state a catalog tier does not: admitted with nothing to block it,
// which still lands only when the server starts again, so it says so rather
// than reading like a provider's one-command upgrade.
//
// The motion is printed only when the upgrade is AVAILABLE. A repository ahead
// of its binary (a rollback) previews core as not available with the stored
// version above the shipped one, and "18 -> 17" would read as a downgrade the
// boot never performs.
func upgradeCell(tier string, up *substrate.BundleUpgrade) string {
	if up == nil {
		return ""
	}
	var motion string
	if up.Available {
		switch {
		case up.From != 0 && up.To != 0 && up.From != up.To:
			motion = fmt.Sprintf("%d -> %d", up.From, up.To)
		case up.To != 0:
			motion = fmt.Sprintf("%d", up.To)
		}
	}
	switch {
	case len(up.Blockers) > 0 && motion == "":
		return "blocked"
	case len(up.Blockers) > 0:
		return motion + ", blocked"
	case tier == tierSeed && up.Available:
		return motion + ", lands at restart"
	}
	return motion
}
