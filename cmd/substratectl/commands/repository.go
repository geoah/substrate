package commands

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A repository is the store: one changelog, its fold and its side stores, one per
// user. Its id is opaque and internal — "it appears in `substratectl`
// output and nowhere else" — which is why these commands
// exist at all: they are the only place the control plane is visible.
//
// All three run ON THE BOX against the database (operator.go): there is no
// repository segment in any URL and no HTTP surface that lists other people's
// repositories, because users cannot see each other.

func (a *app) repositoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "repository",
		Short:   "Operator: inspect and rebuild repositories (direct database, no HTTP)",
		Aliases: []string{"repositories", "repo"},
	}
	cmd.AddCommand(a.repositoryListCommand(), a.repositoryInspectCommand(),
		a.repositoryRebuildCommand(), a.repositoryReembedCommand(),
		a.repositoryVerifyCommand())
	return cmd
}

func (a *app) repositoryReembedCommand() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "reembed <username>",
		Short: "Queue a repository's vectors for re-embedding through its current provider",
		Long: `Enqueue every embeddable property whose stored vectors did not come from the
repository's current embeddings provider and model.

A repository buys its vectors from the one llmprovider row that declares
'embedModel', and every stored vector names the row and the model that produced
it. Change either and the old vectors are from a different model: cosine
distance between two models' vectors is not a distance, so search would go
wrong with no error anywhere. Semantic search scores only the current pair's
vectors, so until this runs the older ones are simply invisible.

This buys nothing. It writes queue rows, and the server's drain loop pays for
them a batch at a time, which is why an interrupted re-embed resumes by itself
and why a large repository catches up over minutes rather than in one call.

--all ignores the stored provenance and queues everything. It is the answer to
a gateway swapped behind an unchanged provider row and model name, which
nothing stored can tell apart.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineRead(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			ds, err := svc.Dataset(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			report, err := ds.Reembed(cmd.Context(), all)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "repository %s queued for re-embedding\n", args[0])
			fmt.Fprintf(a.out, "  provider: %s\n", report.Provider)
			fmt.Fprintf(a.out, "  model:    %s\n", report.Model)
			fmt.Fprintf(a.out, "  queued:   %d properties\n", report.Enqueued)
			if report.All {
				fmt.Fprintf(a.out, "  scope:    every embeddable property (--all)\n")
			}
			fmt.Fprintf(a.out, "the server's drain loop buys the vectors; nothing was bought here\n")
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false,
		"queue every embeddable property, not only the ones another provider or model embedded")
	return cmd
}

func (a *app) repositoryListCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every repository on this substrate",
		Long: `List the control-plane table: one row per user, and the whole of it.

The id is opaque and internal; created_at is the admission record, since the
invite code is the only door and there is nothing else to record.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := a.controlPlane()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			rows, err := listRepositoryRows(cmd.Context(), db)
			if err != nil {
				return err
			}
			if output == "json" {
				out := make([]map[string]any, 0, len(rows))
				for _, r := range rows {
					out = append(out, map[string]any{
						"id": r.ID, "username": r.Username, "authority": r.Authority,
						"createdAt": r.CreatedAt.Format(time.RFC3339),
					})
				}
				return printJSON(a.out, out)
			}
			if output != "" && output != "table" {
				return fmt.Errorf("unknown output format %q: use table or json", output)
			}
			if len(rows) == 0 {
				fmt.Fprintln(a.out, "no repositories: nobody has registered yet")
				return nil
			}
			tw := newTable(a.out)
			fmt.Fprintln(tw, "ID\tUSERNAME\tAUTHORITY\tCREATED\tAGE")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					r.ID, r.Username, r.Authority, r.CreatedAt.Format(time.RFC3339), humanAge(a.now(), r.CreatedAt))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: table|json")
	return cmd
}

func (a *app) repositoryInspectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <username>",
		Short: "Show one repository: id, changelog head, records, vocabulary versions",
		Long: `Describe a repository from the outside.

The changelog's head is its length — seq is per-repository, gapless and assigned at
commit — and the records count is the fold's size. The vocabulary section is
what this repository's OWN changelog says its kinds are, which is the only authority
on the question: the embedded tree is a seed, not a source of truth.

This command only reads.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := a.controlPlane()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			repo, err := repositoryRowByUsername(cmd.Context(), db, args[0])
			if err != nil {
				return err
			}
			scoped, err := a.openScoped(cmd.Context(), repo.ID)
			if err != nil {
				return err
			}
			defer func() { _ = scoped.Close() }()

			fmt.Fprintf(a.out, "repository %s\n", repo.ID)
			fmt.Fprintf(a.out, "  username:  %s\n", repo.Username)
			fmt.Fprintf(a.out, "  created:   %s (%s)\n",
				repo.CreatedAt.Format(time.RFC3339), humanAge(a.now(), repo.CreatedAt))

			head, entries, err := changelogHead(cmd.Context(), scoped)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "  changelog head:  %d (%d entries)\n", head, entries)
			records, deleted, err := recordCounts(cmd.Context(), scoped)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "  records:   %d live, %d tombstoned\n", records, deleted)

			kinds, err := declaredKinds(cmd.Context(), scoped)
			if err != nil {
				return err
			}
			if len(kinds) == 0 {
				fmt.Fprintln(a.out, "  vocabulary: none declared")
				return nil
			}
			fmt.Fprintf(a.out, "  vocabulary: %d kinds\n", len(kinds))
			tw := newTable(a.out)
			fmt.Fprintln(tw, "    PACKAGE\tKINDS\tVERSIONS")
			for _, g := range groupByPackage(kinds) {
				fmt.Fprintf(tw, "    %s\t%d\t%s\n", g.pkg, g.count, strings.Join(g.versions, ","))
			}
			return tw.Flush()
		},
	}
}

func (a *app) repositoryRebuildCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "rebuild <username>",
		Short:   "Replay a repository's changelog into a fresh fold",
		Aliases: []string{"rebuild-repository"},
		Long: `Clear a repository's fold and replay its whole changelog into it.

The changelog is the truth and the records table is a fold of it, so this is the
containment test made runnable. It holds the repository's write lock for the
duration and runs as ONE transaction: a rebuild either replaces the fold or
leaves it exactly as it was. Run 'repository verify' first to see whether the
changelog it would replay is intact.

Blobs and sealed material are SIDE STORES: their bytes were never in the changelog
and are re-linked, not regenerated. A backup is changelog + blobs + sealed as one
unit.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineRead(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			r, ok := svc.(engine.Rebuilder)
			if !ok {
				return seamMissing("RebuildRepository")
			}
			report, err := r.RebuildRepository(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "repository %s rebuilt\n", report.Username)
			fmt.Fprintf(a.out, "  id:       %s\n", report.Repository)
			fmt.Fprintf(a.out, "  replayed: %d entries to head %d\n", report.Entries, report.Head)
			fmt.Fprintf(a.out, "  records:  %d\n", report.Records)
			fmt.Fprintf(a.out, "  took:     %s\n", report.Took.Round(time.Millisecond))
			return nil
		},
	}
}

func (a *app) repositoryVerifyCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "verify <username>",
		Short: "Walk a repository's changelog and check every entry's checksum",
		Long: `Walk a repository's changelog from seq 1 to the head in one read-only
snapshot: the sequence must be gapless, and every entry's checksum, recomputed
from the stored row, must equal the one stamped when the entry was written. It
never repairs or touches the repository it judges (opening the engine still
applies any pending schema migration, as every operator command does).

The checksum catches corruption, not tampering: whoever holds the database can
rewrite a row and its checksum together. The segment files under the data
root are the copy to compare against, and this command will walk those once
the boot check lands.

Exits nonzero when anything does not verify.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineRead(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			v, ok := svc.(engine.Verifier)
			if !ok {
				return seamMissing("VerifyRepository")
			}
			report, err := v.VerifyRepository(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if output == "json" {
				if err := printJSON(a.out, report); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(a.out, "repository %s\n", report.Username)
				fmt.Fprintf(a.out, "  id:       %s\n", report.Repository)
				fmt.Fprintf(a.out, "  entries:  %d, head %d\n", report.Entries, report.Head)
				if report.HeadHash != "" {
					fmt.Fprintf(a.out, "  head checksum: %s\n", report.HeadHash)
				}
				for _, f := range report.Findings {
					fmt.Fprintf(a.out, "  FINDING:  %s\n", f)
				}
				if report.Truncated {
					fmt.Fprintln(a.out, "  ... more findings truncated")
				}
				if report.OK {
					fmt.Fprintf(a.out, "  verified in %s\n", report.Took.Round(time.Millisecond))
				}
			}
			if !report.OK {
				return fmt.Errorf("repository %s does not verify: %d finding(s)", report.Username, len(report.Findings))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: text|json")
	return cmd
}

// changelogHead reads the repository's chronology: the head seq and how many entries
// stand behind it. They match on a healthy repository — seq is gapless — so
// printing both is the cheapest gap check there is.
func changelogHead(ctx context.Context, db *sql.DB) (head, entries int64, err error) {
	err = db.QueryRowContext(ctx,
		`SELECT COALESCE(max(seq), 0), count(*) FROM changelog`).Scan(&head, &entries)
	return head, entries, err
}

func recordCounts(ctx context.Context, db *sql.DB) (live, tombstoned int64, err error) {
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE deleted_at IS NULL),
		       count(*) FILTER (WHERE deleted_at IS NOT NULL)
		FROM records`).Scan(&live, &tombstoned)
	return live, tombstoned, err
}

// declaredKind is one kind declaration as the repository's own rows hold it.
type declaredKind struct {
	ref     string
	version string
}

// declaredKinds reads the kind declarations out of the fold. A declaration IS
// a record, so this is an ordinary read of an ordinary
// collection — and the record's id is the kind reference.
func declaredKinds(ctx context.Context, db *sql.DB) ([]declaredKind, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(props->>'version', '')
		FROM records
		WHERE kind = $1 AND deleted_at IS NULL
		ORDER BY id`, kindKind)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []declaredKind
	for rows.Next() {
		var d declaredKind
		if err := rows.Scan(&d.ref, &d.version); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// kindKind is the meta-kind every kind declaration is a record of.
const kindKind = corePackage + "/kind"

// localPackage labels a declaration whose reference names no package in a
// report. Every stored kind carries one, so this is the report's word for a row
// that should not exist, never a reference.
const localPackage = "(no package)"

type packageGroup struct {
	pkg      string
	count    int
	versions []string
}

// groupByPackage folds the declarations into one row per PACKAGE: how many
// kinds it declares here, and which declaration versions are live. The package
// is the version unit (decision record 0047), so more than one version under
// one package means a partial upgrade — exactly the thing an operator opens
// this command to see.
func groupByPackage(kinds []declaredKind) []packageGroup {
	byPackage := map[string]*packageGroup{}
	seen := map[string]map[string]bool{}
	for _, k := range kinds {
		pkg := vocabulary.KindPackage(k.ref)
		if pkg == "" {
			pkg = localPackage
		}
		g, ok := byPackage[pkg]
		if !ok {
			g = &packageGroup{pkg: pkg}
			byPackage[pkg] = g
			seen[pkg] = map[string]bool{}
		}
		g.count++
		version := k.version
		if version == "" {
			version = "(unversioned)"
		}
		if !seen[pkg][version] {
			seen[pkg][version] = true
			g.versions = append(g.versions, version)
		}
	}
	out := make([]packageGroup, 0, len(byPackage))
	for _, g := range byPackage {
		// Versions are incremental integers, so the honest order is numeric;
		// the non-numeric labels (a legacy spelling, "(unversioned)") sort
		// after the numbers, lexically among themselves.
		sort.Slice(g.versions, func(i, j int) bool {
			vi, ei := strconv.ParseInt(g.versions[i], 10, 64)
			vj, ej := strconv.ParseInt(g.versions[j], 10, 64)
			switch {
			case ei == nil && ej == nil:
				return vi < vj
			case ei == nil:
				return true
			case ej == nil:
				return false
			default:
				return g.versions[i] < g.versions[j]
			}
		})
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pkg < out[j].pkg })
	return out
}
