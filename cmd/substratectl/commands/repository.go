package commands

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A repository is the store: one changelog, its fold and its side stores, one per
// user. Its id is its authority (`ada.example.com`, decision 0046): the row's
// primary key, the scope, the directory under the data root. These commands
// are the only place the control plane is visible.
//
// They run ON THE BOX against the database (operator.go): there is no
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
		a.repositoryVerifyCommand(),
		a.repositoryRotateGenerationCommand(), a.repositoryReindexCommand())
	return cmd
}

func (a *app) repositoryReembedCommand() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "reembed <repository>",
		Short: "Queue a repository's vectors for re-embedding through its current provider",
		Long: `Enqueue every embeddable property whose stored vectors did not come from the
repository's current embeddings provider and model.

A repository buys its vectors from the one llm/provider row that declares
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
root and applies no migration, and the queue rows it writes are not changelog
entries.`,
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
		Long: `List the control-plane table: one row per repository, and the whole of it.

The authority is the repository's name, what its user signs in with and
every other command here takes. The id is its key: the directory under
SUBSTRATE_DATA_ROOT is named by it, and so is the webhook address the server
gives out. created_at is the admission record, since the invite code is the
only door and there is nothing else to record. ARCHIVED is the mark the
server's last archive pass left: the seq its archive holds every line up to,
and how long ago it was written ("never" before the first pass, or with the
archive off). How far that is behind the server's own head is the
substrate_archive_* metrics.`,
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
					entry := map[string]any{
						"authority":      r.Authority,
						"id":             r.ID,
						"createdAt":      r.CreatedAt.Format(time.RFC3339),
						"dekKeyId":       r.DEKKeyID,
						"archivedSeq":    r.ArchivedSeq,
						"archiveMissing": r.ArchiveMissing,
						"archiveStore":   r.ArchiveStore,
					}
					if !r.ArchivedAt.IsZero() {
						entry["archivedAt"] = r.ArchivedAt.Format(time.RFC3339)
					}
					out = append(out, entry)
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
			fmt.Fprintln(tw, "AUTHORITY\tID\tCREATED\tAGE\tARCHIVED")
			for _, r := range rows {
				archived := "never"
				if !r.ArchivedAt.IsZero() {
					archived = fmt.Sprintf("seq %d, %s ago", r.ArchivedSeq, humanAge(a.now(), r.ArchivedAt))
					if r.ArchiveMissing > 0 {
						archived += fmt.Sprintf(", %d blob(s) missing", r.ArchiveMissing)
					}
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					r.Authority, r.ID, r.CreatedAt.Format(time.RFC3339), humanAge(a.now(), r.CreatedAt), archived)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: table|json")
	return cmd
}

func (a *app) repositoryInspectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <repository>",
		Short: "Show one repository: authority, changelog head, archive mark, records, vocabulary versions",
		Long: `Describe a repository from the outside.

The changelog's head is its length — seq is per-repository, gapless and assigned at
commit — and the records count is the fold's size. The changelog is the table;
its copy is the archive, written behind the commits, and the archive line is
the mark the server's last pass left: the seq the archive holds every line up
to. The vocabulary section is what this repository's OWN changelog says its kinds are,
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
			repo, err := repositoryRowByID(cmd.Context(), db, args[0])
			if err != nil {
				return err
			}
			scoped, err := a.openScoped(cmd.Context(), repo.ID)
			if err != nil {
				return err
			}
			defer func() { _ = scoped.Close() }()

			fmt.Fprintf(a.out, "repository %s\n", repo.Authority)
			fmt.Fprintf(a.out, "  id:        %s\n", repo.ID)
			fmt.Fprintf(a.out, "  created:   %s (%s)\n",
				repo.CreatedAt.Format(time.RFC3339), humanAge(a.now(), repo.CreatedAt))
			fmt.Fprintf(a.out, "  dek:       %s\n", describeKeys(repo))

			head, entries, err := changelogHead(cmd.Context(), scoped)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "  changelog head:  %d (%d entries)\n", head, entries)
			if repo.ArchivedAt.IsZero() {
				fmt.Fprintln(a.out, "  archive:   nothing archived yet")
			} else {
				// The database's record of the last pass, not a read of the
				// store: `repository verify` reads the store.
				fmt.Fprintf(a.out, "  archive:   as last published: seq %d, %d behind the head, %d blob(s) missing, written %s ago to %s (the database's record; 'repository verify' reads the store)\n",
					repo.ArchivedSeq, max(head-repo.ArchivedSeq, 0), repo.ArchiveMissing, humanAge(a.now(), repo.ArchivedAt), repo.ArchiveStore)
			}
			if err := describeArchiveRecord(cmd.Context(), a.out, scoped); err != nil {
				return err
			}
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
		Use:   "rebuild <repository>",
		Short: "Replay a repository's changelog into a fresh fold",
		Long: `Clear a repository's fold and replay its whole changelog into it.

The changelog table is the truth and the records table is a fold of it, so
this is the containment test made runnable: a rebuild that reproduces the fold
proves the history alone can bring the repository back. It holds the
repository's write lock for the duration and runs as ONE transaction: a rebuild
either replaces the fold or leaves it exactly as it was. Run 'repository
verify' first to see whether the changelog it would replay is intact.

STOP THE SERVER FIRST. The rebuild opens the repository as its writer, and a
running server holds its writer lease; the command refuses rather than write
beside it. 'repository inspect' and 'repository verify' run beside a live
server; this does not.

Blobs and sealed material are SIDE STORES: their bytes were never in the changelog
and are kept as they are, not regenerated. The archive holds all three, which
is why it is the backup.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineExclusive(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			report, err := svc.RebuildRepository(cmd.Context(), args[0])
			if err != nil {
				return lockHint(err)
			}
			fmt.Fprintf(a.out, "repository %s rebuilt\n", report.Repository)
			fmt.Fprintf(a.out, "  replayed: %d entries to head %d\n", report.Entries, report.Head)
			fmt.Fprintf(a.out, "  records:  %d\n", report.Records)
			fmt.Fprintf(a.out, "  took:     %s\n", report.Took.Round(time.Millisecond))
			return nil
		},
	}
}

func (a *app) repositoryVerifyCommand() *cobra.Command {
	var output string
	var recanonicalize bool
	cmd := &cobra.Command{
		Use:   "verify <repository>",
		Short: "Walk a repository's changelog table and archive and check every checksum",
		Long: `Walk a repository's changelog table and its archive and hold them to each other.

The table is walked from seq 1 to the head in one read-only snapshot: the
sequence must be gapless, every transaction whole and every entry stamped with
a checksum. With --recanonicalize every entry's checksum is also recomputed
from the stored row and must equal the stamped one; that reads and
canonicalizes every payload the table holds, which on a long history takes
hours, and it is the only check that finds a row edited in place with its
checksum left alone.

The archive (SUBSTRATE_ARCHIVE) is read through its manifest: the head it names
must be one the table holds, and every line of the pieces up to it must carry
the table's stamped checksum. An archive behind the table is not a finding,
because it is copied after the commits ('repository list' shows how far
behind); one ahead of the table, or one the store cannot be read for, is.

The side stores are held to the fold: every blob whose manifest says stored is
read out of the configured blob store and hashed against its digest, and every
secret reference a live record holds must have its sealed row. With
SUBSTRATE_CREDENTIAL_KEY set, every sealed row is opened under the
repository's key; without it nothing is opened.

It never repairs or touches the repository it judges, and it runs beside a live
server: the engine opens read-only, so an incomplete final transaction is
reported as a finding, never cut. The read-only open applies no migration either: a database
missing one this binary carries, or holding one it does not carry, is refused
by name, so run the substratectl of the release the server runs. Against a
server that is mid-write a finding about the heads, or about a blob the sweep
collected a moment ago, can be a write in flight; run it again before
believing it.

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
			report, err := svc.VerifyRepositoryWith(cmd.Context(), args[0], engine.VerifyOptions{Recanonicalize: recanonicalize})
			if err != nil {
				return err
			}
			if output == "json" {
				if err := printJSON(a.out, report); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(a.out, "repository %s\n", report.Repository)
				fmt.Fprintf(a.out, "  table:    %d entries, head %d\n", report.Entries, report.Head)
				if report.Recanonicalized {
					fmt.Fprintln(a.out, "  table:    every checksum recomputed from its stored row")
				}
				switch {
				case report.Archive == "":
					fmt.Fprintln(a.out, "  archive:  none (SUBSTRATE_ARCHIVE=off)")
				case report.ArchiveUnverified != "":
					fmt.Fprintf(a.out, "  archive:  NOT VERIFIED in %s: %s\n", report.Archive, report.ArchiveUnverified)
				default:
					fmt.Fprintf(a.out, "  archive:  head %d in %d piece(s), every line held to the table, in %s\n", report.ArchiveHead, report.ArchivePieces, report.Archive)
				}
				if report.ArchiveID != "" {
					unaccounted := strconv.Itoa(report.ArchiveUnaccounted)
					if report.ArchiveUnaccounted < 0 {
						unaccounted = "an unknown number of"
					}
					fmt.Fprintf(a.out, "  archive:  archive %s, %s object(s) the database has no record of (nothing reads them); read now, and no proof the store hides no other manifest\n",
						report.ArchiveID, unaccounted)
				}
				fmt.Fprintf(a.out, "  sealed:   %d rows, %d held by no record\n", report.SealedRows, report.SealedOrphans)
				if report.SealedOpened > 0 || os.Getenv(credentialKeyEnv) != "" {
					fmt.Fprintf(a.out, "  sealed:   %d row(s) opened under %s\n", report.SealedOpened, credentialKeyEnv)
				} else {
					fmt.Fprintf(a.out, "  sealed:   not opened (set %s to prove every row opens)\n", credentialKeyEnv)
				}
				fmt.Fprintf(a.out, "  secrets:  %d live reference(s) held to the sealed rows\n", report.SecretRefs)
				fmt.Fprintf(a.out, "  blobs:    %d stored, %d bytes hashed\n", report.Blobs, report.BlobBytes)
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
				return fmt.Errorf("repository %s does not verify: %d finding(s)", report.Repository, len(report.Findings))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: text|json")
	cmd.Flags().BoolVar(&recanonicalize, "recanonicalize", false, "also recompute every table row's checksum from its stored columns (reads every payload; hours on a long history)")
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
// collection — and the record's id is the kind reference. Every declaration
// carries its version property; the coalesce keeps one malformed row from
// failing the whole read.
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
		if !seen[pkg][version] {
			seen[pkg][version] = true
			g.versions = append(g.versions, version)
		}
	}
	out := make([]packageGroup, 0, len(byPackage))
	for _, g := range byPackage {
		// Versions are incremental integers, so the order is numeric.
		sort.Slice(g.versions, func(i, j int) bool {
			vi, _ := strconv.ParseInt(g.versions[i], 10, 64)
			vj, _ := strconv.ParseInt(g.versions[j], 10, 64)
			return vi < vj
		})
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].pkg < out[j].pkg })
	return out
}

// describeArchiveRecord prints what the database records of the
// repository's archive (decision 0279): the archive it writes, a publication
// about to be made, and why the passes stopped, if they did.
func describeArchiveRecord(ctx context.Context, w io.Writer, scoped *sql.DB) error {
	var archive string
	var pending []byte
	var stopped, take sql.NullString
	err := scoped.QueryRowContext(ctx, `SELECT archive, pending, stopped, take FROM archive_records`).Scan(&archive, &pending, &stopped, &take)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "  archive id: %s\n", archive)
	if pending != nil {
		fmt.Fprintln(w, "  archive:   one manifest about to be published; the next pass puts it again")
	}
	if stopped.Valid {
		fmt.Fprintf(w, "  archive:   STOPPED, writing nothing until resolved ('archive report', 'archive resolve'): %s\n", stopped.String)
	}
	if take.Valid {
		fmt.Fprintf(w, "  archive:   the next boot replaces the repository by %s, as its operator chose\n", take.String)
	}
	return nil
}
