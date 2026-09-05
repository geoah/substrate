package commands

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/config"
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
nothing stored can tell apart.

It runs beside a live server: the engine opens read-only against the data
root, and the queue rows it writes are not changelog entries.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineReadOnly(cmd.Context())
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
		Short: "Show one repository: id, both changelog heads, records, vocabulary versions",
		Long: `Describe a repository from the outside.

The changelog's head is its length — seq is per-repository, gapless and assigned at
commit — and the records count is the fold's size. The changelog lives twice:
the table is the live index and the segment files under SUBSTRATE_DATA_ROOT
(<root>/repositories/<id>/changelog) are the copy a backup takes, so both heads
are printed and a healthy repository shows the same number twice. The
vocabulary section is what this repository's OWN changelog says its kinds are,
which is the only authority on the question: the embedded tree is a seed, not
a source of truth.

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
			fmt.Fprintf(a.out, "  changelog head:  %d (%d entries, table)\n", head, entries)
			printChangelogFiles(a.out, repo.ID)
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

// printChangelogFiles reports the repository directory's half of the
// changelog: the data root, the file head and the segment count. The walk is
// read-only (changelogfile.Verify), so an inspect never repairs a torn tail.
// A missing SUBSTRATE_DATA_ROOT is reported, not fatal: the table half of the
// report stands on its own.
func printChangelogFiles(out io.Writer, repoID string) {
	data, err := config.LoadData()
	if err != nil {
		fmt.Fprintf(out, "  data root: not set (%v)\n", err)
		return
	}
	fmt.Fprintf(out, "  data root: %s\n", data.Root)
	dir, err := changelogfile.RepoDir(data.Root, repoID)
	if err != nil {
		fmt.Fprintf(out, "  changelog files: %v\n", err)
		return
	}
	rep, err := changelogfile.Verify(changelogfile.ChangelogDir(dir))
	if err != nil {
		fmt.Fprintf(out, "  changelog files: head %d in %d segment(s); DAMAGED: %v\n", rep.Head, rep.Segments, err)
		return
	}
	fmt.Fprintf(out, "  changelog files: head %d (%d entries, %d segment(s))\n", rep.Head, rep.Entries, rep.Segments)
	if rep.TruncatedBytes > 0 {
		fmt.Fprintf(out, "  changelog files: the active segment ends in a torn line of %d bytes (the next open cuts it)\n", rep.TruncatedBytes)
	}
}

func (a *app) repositoryRebuildCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "rebuild <username>",
		Short:   "Replay a repository's changelog into a fresh fold",
		Aliases: []string{"rebuild-repository"},
		Long: `Clear a repository's fold and replay its whole changelog into it.

The changelog is the truth and the records table is a fold of it, so this is the
containment test made runnable. The replay reads the segment files under
SUBSTRATE_DATA_ROOT, not the table, so a rebuild that reproduces the fold proves
the repository directory alone can bring the repository back. Before it
replays, the files are held to the table (equal heads, agreeing checksums on
the common tail) and a disagreement refuses the rebuild. It holds the
repository's write lock for the duration and runs as ONE transaction: a rebuild
either replaces the fold or leaves it exactly as it was. Run 'repository
verify' first to see whether the changelog it would replay is intact.

STOP THE SERVER FIRST. The rebuild opens the repository as its changelog
writer, and a running server holds that lock; the command refuses rather than
write beside it. 'repository inspect' and 'repository verify' run beside a
live server; this does not.

Blobs and sealed material are SIDE STORES: their bytes were never in the changelog
and are re-linked, not regenerated. The repository directory holds all three,
which is why it is the backup unit.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineExclusive(cmd.Context())
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
				return lockHint(err)
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
		Short: "Walk a repository's changelog files and table and check every checksum",
		Long: `Walk a repository's changelog in both places and hold them to each other.

The segment files under SUBSTRATE_DATA_ROOT are walked whole: every line's
checksum, every finished segment's sidecar digest, the seq sequence. The table
is walked from seq 1 to the head in one read-only snapshot: the sequence must
be gapless, and every entry's checksum, recomputed from the stored row, must
equal the one stamped when the entry was written and the one the file's line
carries. Both heads must agree, and every sealed row must have its file and
every sealed file its row.

It never repairs or touches the repository it judges, and it runs beside a live
server: the engine opens read-only against the data root, so a torn tail or a
table ahead of its file is reported as a finding, never cut or caught up
(opening the engine still applies any pending schema migration, as every
operator command does). Against a server that is mid-write a finding about the
heads can be a transaction in flight; run it again before believing it.

The checksum catches corruption, not tampering: whoever holds the disk can
rewrite a line and its checksum together.

Exits nonzero when anything does not verify.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineReadOnly(cmd.Context())
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
				fmt.Fprintf(a.out, "  table:    %d entries, head %d\n", report.Entries, report.Head)
				fmt.Fprintf(a.out, "  files:    head %d in %d segment(s)\n", report.FileHead, report.Segments)
				fmt.Fprintf(a.out, "  sealed:   %d rows, %d files\n", report.SealedRows, report.SealedFiles)
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
