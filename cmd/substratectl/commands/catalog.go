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

// catalogAddPath is /api/v1/catalog/{id}/add: the shipped closures, at the
// version root because the endpoint names no kind (decision 0033). The bundle
// id is a package reference and carries a `/`, so it is escaped as one
// segment.
func catalogAddPath(id string) string {
	return apiPrefix + "/catalog/" + url.PathEscape(id) + "/add"
}

// addBundle adds one catalog package and prints what landed. The server
// answers with the LANDED bundle's status, which for a sample is its package
// under this repository's own authority rather than the id typed. With
// allowDataLoss the route is asked for its preview first, and a lossy plan is
// confirmed by the hash and changelog head that preview carried
// (confirmedUpgrade): the consent covers what was shown and nothing else.
func (a *app) addBundle(cmd *cobra.Command, id string, allowDataLoss bool) error {
	cl, err := a.client()
	if err != nil {
		return err
	}
	if err := a.noteOutsideCode(cmd.Context(), cl, id); err != nil {
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
	if err := cl.do(cmd.Context(), http.MethodPost, catalogAddPath(id), nil, body, &taken); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s added\n", taken.ID)
	printBundleStatus(a, taken.BundleStatus)
	printSuggestedMappings(a, id, taken.SuggestedMappings)
	return nil
}

// confirmedUpgrade reads the catalog's preview of one bundle and, where its
// conversion plan is lossy or the re-import would replace declarations edited
// since the copy was imported (decision record 0070), prints what is lost and
// answers the confirmation bound to that preview (decision 0067). A bundle
// with no preview, or one that loses nothing, answers nil: there is nothing
// to consent to, and the door runs the plan as it stands.
func (a *app) confirmedUpgrade(ctx context.Context, cl *client, id string) (*substrate.ConversionConfirm, error) {
	var cat substrate.OperationalList[substrate.CatalogItem]
	if err := cl.do(ctx, http.MethodGet, apiPrefix+"/catalog", nil, nil, &cat); err != nil {
		return nil, err
	}
	for _, e := range cat.Items {
		if e.ID != id {
			continue
		}
		up := e.Upgrade
		if up == nil || up.PlanHash == "" || (!up.Lossy && !up.DiscardsEdits) {
			fmt.Fprintf(a.out, "%s: the upgrade removes no values and replaces no edits, so --allow-data-loss confirms nothing\n", id)
			return nil, nil
		}
		fmt.Fprintf(a.out, "%s: confirming plan %s at changelog seq %d, which %s:\n", id, up.PlanHash, up.ChangelogSeq, lossWords(up))
		if up.DiscardsEdits {
			fmt.Fprintf(a.out, "  replaces your copy of %s whole: the declarations edited since it was imported go with it\n", id)
		}
		for _, s := range up.Steps {
			if s.Lossy {
				fmt.Fprintf(a.out, "  %s\n", stepLine(s))
			}
		}
		return &substrate.ConversionConfirm{PlanHash: up.PlanHash, ChangelogSeq: up.ChangelogSeq}, nil
	}
	return nil, nil
}

// lossWords names what a confirmation consents to: values, edits, or both.
func lossWords(up *substrate.BundleUpgrade) string {
	switch {
	case up.Lossy && up.DiscardsEdits:
		return "removes values and replaces edits"
	case up.DiscardsEdits:
		return "replaces edits"
	default:
		return "removes values"
	}
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
	case substrate.StepEnter:
		return fmt.Sprintf("enters %s at %s on %s: %s rewritten", s.Property, s.To, s.Kind, n)
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

// bundleTaken is the add route's response: the landed bundle's computed
// status, plus what each SUGGESTED MAPPING the closure carries did.
type bundleTaken struct {
	substrate.BundleStatus
	SuggestedMappings []substrate.SuggestedMapping `json:"suggestedMappings,omitempty"`
}

// printSuggestedMappings says what the closure's suggested mappings did
// (decision record 0049). A sample ships one per provider it knows, onto a
// kind of its own, and the door admits only the ones that resolve here, so a
// reader who adds Linear tomorrow has to be told BOTH that adding this sample
// again is what lands the rest and what that costs: adding it again replaces
// the copy (record 0048), so a kind or a property they added since goes with
// it.
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
	again := fmt.Sprintf("add %s again. Adding it again replaces your copy and may remove your changes.", sample)
	switch m.State {
	case substrate.SuggestedMappingLanded:
		return "landed"
	case substrate.SuggestedMappingReady:
		return "ready; " + again
	case substrate.SuggestedMappingBlocked:
		return fmt.Sprintf("blocked; upgrade %s first, then %s", m.Package, again)
	default:
		return fmt.Sprintf("waiting; add %s, then %s", m.Package, again)
	}
}

// addCommand takes one catalog package through the one add route. Who keeps
// the package decides where it lands (decision record 0258): a published
// package lands under the authority that publishes it, a sample is copied
// under this repository's own authority. `install` and `import`, the two verbs
// this replaced, stay as aliases, so a script that names either still runs.
func (a *app) addCommand() *cobra.Command {
	var allowDataLoss bool
	cmd := &cobra.Command{
		Use:     "add <package>",
		Aliases: []string{"install", "import"},
		Short:   "Add a catalog package: a published one under its publisher, a sample as your own copy",
		Long: `Add one of the catalog's packages to this repository. Who keeps the
package decides where it lands; ` + "`substratectl catalog`" + ` lists it under
KEPT BY.

A PUBLISHED package (a provider, or a catalogue such as substrate.reamde.dev/labs)
is kept by its publisher, so it lands exactly as published:

  substratectl add providers.substrate.reamde.dev/google
  substratectl add substrate.reamde.dev/labs

Its declarations are the publisher's afterwards (` + "`source: published`" + `),
so this repository's token may not rewrite them, and each change the publisher
ships arrives as an upgrade. Adding it again is that upgrade.

A SAMPLE is kept by your repository: it is vocabulary to copy, not vocabulary
to depend on. The server rewrites the closure's placeholder authority to the
one this repository owns before admitting it, so

  substratectl add samples.substrate.reamde.dev/tasks

lands ` + "`<your authority>/tasks/task`" + `: your kind, writable through the
API. A sample that declares against another is refused until that one is
added at the version it needs, naming what to add first. The copy records
where it came from, so when a later binary ships the sample at a newer
version ` + "`substratectl catalog`" + ` offers the upgrade, and adding it again
is how it lands. Adding it again REPLACES your copy.

An upgrade that removes values from your records (a property the new closure
drops while records carry it, an enum value renamed onto one it keeps), or
that replaces a copy you edited since you added it, is refused until it is
confirmed. ` + "`substratectl catalog`" + ` shows the plan; --allow-data-loss
reads it again, prints what goes, and confirms exactly that plan: a write in
between to a record the plan rewrites or a declaration it converts, or a plan
that reads differently, is refused again. The removed values stay in the
changelog.

` + "`install`" + ` and ` + "`import`" + ` are aliases of this command.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.addBundle(cmd, args[0], allowDataLoss)
		},
	}
	cmd.Flags().BoolVar(&allowDataLoss, "allow-data-loss", false, "confirm the previewed upgrade even where it removes values from records or replaces your edits")
	return cmd
}

// catalogRow is one line of `substratectl catalog`: a package the binary ships,
// from either read, in one shape.
type catalogRow struct {
	ID string `json:"id"`
	// KeptBy is who keeps the package: `binary` for one registration writes
	// and the binary upgrades at boot, else the catalog's `publisher` or
	// `repository`.
	KeptBy    string `json:"keptBy"`
	Installed bool   `json:"installed"`
	// Version is the version this binary ships the package at.
	Version int64 `json:"version,omitempty"`
	// Upgrade is the motion an upgrade would make here and the guard lines it
	// is refused on, when the server previewed one.
	Upgrade *substrate.BundleUpgrade `json:"upgrade,omitempty"`
	// Code is `experimental` where taking the package runs code a publisher
	// outside this server published (outsideCode), whatever the package's
	// own stability label says (docs/stability.md).
	Code string `json:"code,omitempty"`
}

// codeExperimental is the code's label where it comes from outside.
const codeExperimental = "experimental"

// outsideCodeWords is the qualification printed where a person takes such a
// package, the words the console shows beside the same label.
const outsideCodeWords = "Its code comes from a publisher outside this server, and such code is experimental: how an approval reviews it and how the server confines it may change in any release."

// outsideCode reports whether taking the catalog entry id runs code a
// publisher outside this server published: its own closure's functions, or
// those of a package the take brings first, one not held here or held below
// what the catalog lists. A publisher's document is what marks an entry as
// another's (its publisher), the fact the console's approval and package
// pages read too.
func outsideCode(items []substrate.CatalogItem, id string) bool {
	byID := make(map[string]substrate.CatalogItem, len(items))
	for _, e := range items {
		byID[e.ID] = e
	}
	seen := map[string]bool{}
	var runs func(id string, taken bool) bool
	runs = func(id string, taken bool) bool {
		e, ok := byID[id]
		if !ok || seen[id] {
			return false
		}
		seen[id] = true
		if !taken && e.Installed && (e.Upgrade == nil || !e.Upgrade.Available) {
			return false
		}
		if e.Publisher != nil && len(e.Closure.Functions) > 0 {
			return true
		}
		for _, req := range e.Requires {
			if runs(req, false) {
				return true
			}
		}
		return false
	}
	return runs(id, true)
}

// noteOutsideCode prints the code's label before a take of a package whose
// code comes from outside. It reads the package's own catalog entry, and the
// entries of what the take brings first, never the whole catalog: the take
// stays the bare POST, and a refusal is still the server's. An id the
// catalog does not list is left to the take's refusal.
func (a *app) noteOutsideCode(ctx context.Context, cl *client, id string) error {
	var entries []substrate.CatalogItem
	seen := map[string]bool{}
	next := []string{id}
	for len(next) > 0 {
		at := next[0]
		next = next[1:]
		if seen[at] {
			continue
		}
		seen[at] = true
		var e substrate.CatalogItem
		err := cl.do(ctx, http.MethodGet, apiPrefix+"/catalog/"+url.PathEscape(at), nil, nil, &e)
		var ae *apiError
		switch {
		case errors.As(err, &ae) && ae.Status == http.StatusNotFound:
			continue
		case err != nil:
			return err
		}
		entries = append(entries, e)
		if at == id || !e.Installed || (e.Upgrade != nil && e.Upgrade.Available) {
			next = append(next, e.Requires...)
		}
	}
	if outsideCode(entries, id) {
		fmt.Fprintf(a.out, "%s: code %s. %s\n", id, codeExperimental, outsideCodeWords)
	}
	return nil
}

// keptByBinary is the row word for the package the binary seeds and upgrades
// itself, which no catalog entry says: nothing adds core.
const keptByBinary = "binary"

// catalogCommand lists every package the binary ships and where this repository
// stands on each: the seeded core package from `GET /api/v1/vocabulary/upgrade`
// and the catalog's published packages and samples from `GET /api/v1/catalog`.
// It is the one place the CLI prints an upgrade: a refused core boot upgrade
// and a published package's blocked upgrade both reach a terminal here.
func (a *app) catalogCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Every shipped package: held here or not, and the upgrade this binary offers or refuses",
		Long: `List the packages this substrate's binary ships and where this repository
stands on each.

  PACKAGE    the package identity
  KEPT BY    binary (core, written at registration and upgraded at boot),
             publisher (a published package, added under the authority that
             publishes it) or repository (a sample, copied under your own
             authority)
  INSTALLED  whether this repository holds it
  VERSION    the version the binary ships it at
  UPGRADE    the motion an upgrade would make here ("16 -> 17"); "blocked"
             when the server refuses it; for core, "lands at restart" when
             it is admitted and waiting for the server to start again

A blocked upgrade prints its guard lines under the table. Each names a kind, a
property and the count of live records still holding the old shape; the
upgrade lands once those records are migrated or deleted. For core that is
the boot upgrade, which runs at the server's next start and not before, so
an admitted core upgrade reads "lands at restart" until then; for a published
package it is ` + "`substratectl add <package>`" + ` again, and for a sample
the same, which replaces your copy.

An upgrade that rewrites records prints its steps under the table too, each
with the live records it touches. A step marked lossy removes values from the
fold (they stay in the changelog); an upgrade with one runs only with
--allow-data-loss on ` + "`substratectl add`" + `, and the boot upgrade never
runs one. A sample copy you edited since adding it says so under the table as
well: adding it again replaces those edits, and takes the same flag.`,
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
			// server that does not serve /vocabulary/upgrade answers 404 and
			// has no core row, and any other refusal costs the core row
			// alone, because the catalog read still serves and is the reason
			// the command was run. A refusal that is not the expected 404 is
			// said once, on stderr.
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
					rows = append(rows, catalogRow{ID: s.Package, KeptBy: keptByBinary, Installed: up.From != 0, Version: up.To, Upgrade: &up})
				}
			case errors.As(err, &ae):
				if ae.Status != http.StatusNotFound {
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
				row := catalogRow{ID: e.ID, KeptBy: e.KeptBy, Installed: e.Installed, Version: e.Version, Upgrade: e.Upgrade}
				if outsideCode(cat.Items, e.ID) {
					row.Code = codeExperimental
				}
				rows = append(rows, row)
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
	fmt.Fprintln(tw, "PACKAGE\tKEPT BY\tINSTALLED\tVERSION\tUPGRADE\tCODE")
	for _, r := range rows {
		// A row whose code is not another's leaves the last cell out, so no
		// padding trails its upgrade cell.
		line := fmt.Sprintf("%s\t%s\t%t\t%d\t%s", r.ID, r.KeptBy, r.Installed, r.Version, upgradeCell(r.KeptBy, r.Upgrade))
		if r.Code != "" {
			line += "\t" + r.Code
		}
		fmt.Fprintln(tw, line)
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
	// read here before anybody confirms it (decision 0067). A sample copy
	// edited since it was added is said the same way (decision record 0070):
	// adding it again replaces the edits, under the same flag.
	for _, r := range rows {
		if r.Upgrade == nil {
			continue
		}
		if r.Upgrade.DiscardsEdits {
			fmt.Fprintf(w, "\n%s: your copy was edited since it was added; adding it again replaces those edits, confirm it with `substratectl add %s --allow-data-loss`\n", r.ID, r.ID)
		}
		if len(r.Upgrade.Steps) == 0 {
			continue
		}
		switch {
		case r.Upgrade.Lossy && r.KeptBy == keptByBinary:
			fmt.Fprintf(w, "\n%s: the upgrade rewrites %d live records and removes values; the boot upgrade never runs a lossy step, so rewrite the records it names first\n", r.ID, r.Upgrade.Work)
		case r.Upgrade.Lossy:
			fmt.Fprintf(w, "\n%s: the upgrade rewrites %d live records and removes values; confirm it with `substratectl add %s --allow-data-loss`\n", r.ID, r.Upgrade.Work, r.ID)
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
// a third state a catalog package does not: admitted with nothing to block it,
// which still lands only when the server starts again, so it says so rather
// than reading like a published package's one-command upgrade.
//
// The motion is printed only when the upgrade is AVAILABLE. A repository ahead
// of its binary (a rollback) previews core as not available with the stored
// version above the shipped one, and "18 -> 17" would read as a downgrade the
// boot never performs.
func upgradeCell(keptBy string, up *substrate.BundleUpgrade) string {
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
	case keptBy == keptByBinary && up.Available:
		return motion + ", lands at restart"
	case up.DiscardsEdits && motion == "":
		// A sample copy edited since it was added, with nothing newer
		// shipped: the preview is here for the confirmation adding it again needs, and
		// the column says why the row carries one.
		return "edited copy"
	case up.DiscardsEdits:
		return motion + ", edited copy"
	}
	return motion
}
