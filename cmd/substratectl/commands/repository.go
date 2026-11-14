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

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/config"
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
		a.repositoryRebuildCommand(), a.repositoryCompactCommand(), a.repositoryHistoryCommand(), a.repositoryReembedCommand(),
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
entries. A repository the running server set aside at its start is refused,
read from the writer leases as 'repository list' reads them, and gets no
queue row.`,
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

The authority is the repository's name, which every other command here takes.
The id is its key: the directory under SUBSTRATE_DATA_ROOT is named by it, and
so is the webhook address the server gives out. SIGN-INS are the names of the
people who may open it (decision 0274); a repository nobody can sign in to
shows none, and ` + "`user reset`" + ` gives it one. created_at is the admission
record, since the invite code is the only door and there is nothing else to
record. ARCHIVED is the mark the server's last archive pass left: the seq its
archive holds every line up to, and how long ago it was written ("never"
before the first pass, or with the archive off). How far that is behind the
server's own head is the substrate_archive_* metrics.

The state is read from the writer leases in pg_locks, beside a running
server: serving when a server holds the repository's lease; set aside when
a server holds others and not this one, because its start found it damaged
(the server's log says why, and a restart after the repair serves it again),
or because its start-up check has not reached it yet; stopped when no server
holds any. With SUBSTRATE_DATA_ROOT set, a 0.x directory there with no row,
and a repository the archive (SUBSTRATE_ARCHIVE, as the server runs with
it) holds and the database has no row of, are listed too, with no creation
time: set aside while a server runs, since its start imports or restores
every other one, and not imported while none does.`,
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
			people, err := repositorySignIns(cmd.Context(), db)
			if err != nil {
				return err
			}
			root := ""
			var store archive.Store
			if os.Getenv(dataRootEnv) != "" {
				data, err := config.LoadData()
				if err != nil {
					return err
				}
				root = data.Root
				archived, err := config.LoadArchive()
				if err != nil {
					return err
				}
				if store, err = archived.Store(data.Root); err != nil {
					return err
				}
			}
			states, err := engine.RepositoryStates(cmd.Context(), db, root, store)
			if errors.Is(err, engine.ErrArchiveUnlisted) {
				// The database's rows are listed all the same.
				fmt.Fprintf(a.errOut, "the archive did not answer, so a repository it alone holds is not listed: %v\n", err)
				err = nil
			}
			if err != nil {
				return err
			}
			state := make(map[string]string, len(states))
			var dirs []engine.RepositoryState
			for _, st := range states {
				if st.Directory {
					dirs = append(dirs, st)
					continue
				}
				state[st.ID] = st.State
			}
			if output == "json" {
				out := make([]map[string]any, 0, len(rows)+len(dirs))
				for _, r := range rows {
					entry := map[string]any{
						"authority":      r.Authority,
						"id":             r.ID,
						"identities":     append([]string{}, people[r.ID]...),
						"createdAt":      r.CreatedAt.Format(time.RFC3339),
						"dekKeyId":       r.DEKKeyID,
						"archivedSeq":    r.ArchivedSeq,
						"archiveMissing": r.ArchiveMissing,
						"archiveStore":   r.ArchiveStore,
						"state":          state[r.ID],
					}
					if !r.ArchivedAt.IsZero() {
						entry["archivedAt"] = r.ArchivedAt.Format(time.RFC3339)
					}
					out = append(out, entry)
				}
				for _, d := range dirs {
					out = append(out, map[string]any{"authority": d.Authority, "id": d.ID, "state": d.State})
				}
				return printJSON(a.out, out)
			}
			if output != "" && output != "table" {
				return fmt.Errorf("unknown output format %q: use table or json", output)
			}
			if len(rows) == 0 && len(dirs) == 0 {
				fmt.Fprintln(a.out, "no repositories: nobody has registered yet")
				return nil
			}
			tw := newTable(a.out)
			fmt.Fprintln(tw, "AUTHORITY\tID\tSIGN-INS\tCREATED\tAGE\tARCHIVED\tSTATE")
			for _, r := range rows {
				signIns := strings.Join(people[r.ID], ",")
				if signIns == "" {
					signIns = "-"
				}
				archived := "never"
				if !r.ArchivedAt.IsZero() {
					archived = fmt.Sprintf("seq %d, %s ago", r.ArchivedSeq, humanAge(a.now(), r.ArchivedAt))
					if r.ArchiveMissing > 0 {
						archived += fmt.Sprintf(", %d blob(s) missing", r.ArchiveMissing)
					}
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					r.Authority, r.ID, signIns, r.CreatedAt.Format(time.RFC3339), humanAge(a.now(), r.CreatedAt), archived, state[r.ID])
			}
			for _, d := range dirs {
				name := d.Authority
				if name == "" {
					name = "-"
				}
				fmt.Fprintf(tw, "%s\t%s\t-\t-\t-\t-\t%s\n", name, d.ID, d.State)
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

func (a *app) repositoryHistoryCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "history <repository>",
		Short: "Measure a repository's history by kind: entries and bytes, largest first",
		Long: `Measure a repository's history by kind: how many entries are addressed to each
kind's records, how many bytes their payloads hold as stored, and when the
oldest of them was written. Postgres sums it, so the command reads no line and
runs beside a live server. It is what a kind's retention is chosen from. It
also says where the compaction pass stands: the repository's horizon, the
record trigger that holds forgetting deletions back, and how far each kind is
compacted.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineReadOnly(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			kinds, err := svc.HistoryByKind(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			status, err := svc.CompactionStatus(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if output == "json" {
				return printJSON(a.out, map[string]any{"kinds": kinds, "compaction": status})
			}
			var entries, bytes int64
			for _, k := range kinds {
				entries, bytes = entries+k.Entries, bytes+k.Bytes
			}
			fmt.Fprintf(a.out, "%-60s %10s %12s %6s  %s\n", "KIND", "ENTRIES", "BYTES", "SHARE", "OLDEST")
			for _, k := range kinds {
				share := 0.0
				if bytes > 0 {
					share = 100 * float64(k.Bytes) / float64(bytes)
				}
				fmt.Fprintf(a.out, "%-60s %10d %12d %5.1f%%  %s\n", k.Kind, k.Entries, k.Bytes, share, k.Oldest.Format(time.RFC3339))
			}
			fmt.Fprintf(a.out, "%-60s %10d %12d\n", "total", entries, bytes)
			fmt.Fprintf(a.out, "\nhorizon: seq %d (a resume from below it is told compacted); deletions looked at up to seq %d\n", status.Horizon, status.ForgotUpTo)
			if p := status.LastPass; p != nil {
				if status.LastPassEnded {
					fmt.Fprintf(a.out, "the last pass ran out of its budget at %s: the next goes on from where it stopped\n", p.Format(time.RFC3339))
				} else {
					fmt.Fprintf(a.out, "the last pass finished at %s\n", p.Format(time.RFC3339))
				}
			}
			if len(status.Damaged) > 0 {
				fmt.Fprintf(a.out, "seq %v found not to verify: nothing at or above seq %d is compacted or erased until its row is written back from a whole copy; the next pass then goes on by itself\n", status.Damaged, status.Damaged[0])
			}
			for _, e := range status.Erasures {
				what := "its history"
				if e.Live {
					what = "its history before its current state"
				}
				fmt.Fprintf(a.out, "the erasure of %s/%s (%s, up to seq %d) waits since %s, %s ago: the pass finishes it, and a forget with finish finishes it now\n",
					e.Kind, e.ID, what, e.UpTo, e.Since.Format(time.RFC3339), time.Since(e.Since).Round(time.Second))
			}
			if h := status.HeldBy; h != nil {
				fmt.Fprintf(a.out, "held back by trigger %s, its cursor at seq %d since %s: no deletion past it is forgotten\n", h.Trigger, h.Seq, h.Since.Format(time.RFC3339))
			}
			for _, k := range status.Kinds {
				fmt.Fprintf(a.out, "compacted %s up to seq %d: %d records, %d entries removed, last at %s\n", k.Kind, k.UpTo, k.Records, k.Removed, k.At.Format(time.RFC3339))
				if k.Held > 0 {
					fmt.Fprintf(a.out, "  %d of its records wait on a parked failure, a drain or the webhook spool for the rest of their compaction; a round of their retries takes %d passes\n", k.Held, k.HeldRound)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: json")
	return cmd
}

func (a *app) repositoryCompactCommand() *cobra.Command {
	var upTo int64
	cmd := &cobra.Command{
		Use:   "compact <repository>",
		Short: "Replace each record's history up to a seq with one state entry",
		Long: `Compact a repository's history: replace every record's entries up to a seq
with one state entry, the record's whole state as of the newest entry it
replaces, and a trigger's delivery entries with one ledger entry the same way.

The records, versions and times a rebuild replays from the compacted history
are the ones it replayed before. The history is smaller: an entry left with no
effect is removed, and its seq is not used again, so the history has gaps. What
a compaction cannot keep is the values a record held before its state: the
activity view reads them as not kept, and a rebuild can no longer bring a
state from before the compaction back. A copy taken before it still holds them.

With no --up-to, every record is compacted up to its newest entry. The
archive's next pass writes its pieces again from the lowest entry the
compaction changed.

STOP THE SERVER FIRST. Like a rebuild, the compaction opens the repository as
its writer and refuses beside a running server.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineExclusive(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			report, err := svc.CompactRepository(cmd.Context(), args[0], upTo)
			if err != nil {
				return lockHint(err)
			}
			fmt.Fprintf(a.out, "repository %s compacted up to seq %d\n", report.Repository, report.UpTo)
			fmt.Fprintf(a.out, "  states:    %d records, %d triggers\n", report.Records, report.Triggers)
			fmt.Fprintf(a.out, "  replaced:  %d effects\n", report.Effects)
			fmt.Fprintf(a.out, "  entries:   %d removed, %d rewritten\n", report.Removed, report.Rewritten)
			if report.Damaged > 0 {
				fmt.Fprintf(a.out, "  stopped:   seq %d does not verify, so it compacted below it alone and did not look above it; write the row back from a whole copy and compact again ('repository verify --recanonicalize' names every such seq)\n", report.Damaged)
			}
			fmt.Fprintf(a.out, "  took:      %s\n", report.Took.Round(time.Millisecond))
			return nil
		},
	}
	cmd.Flags().Int64Var(&upTo, "up-to", 0, "the seq to compact up to (default: the head)")
	return cmd
}

func (a *app) repositoryVerifyCommand() *cobra.Command {
	var output string
	var recanonicalize bool
	cmd := &cobra.Command{
		Use:   "verify <repository>",
		Short: "Walk a repository's changelog table and archive and check every checksum",
		Long: `Walk a repository's changelog table and its archive and hold them to each other.

The table is walked from seq 1 to the head in one read-only snapshot: every
transaction must be whole, every entry stamped with a checksum, and every seq
must hold a line, but at or below the highest line a compaction's state entry
stands at: a seq absent there is one a compaction may have taken, and it is
counted, not a finding. Above it, a gap is damage, as in a history never
compacted. With --recanonicalize every entry's checksum is also recomputed
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
				if report.CompactedUpTo > 0 {
					fmt.Fprintf(a.out, "  table:    compacted: %d seqs at or below seq %d hold no line, which a compaction may have taken\n",
						report.Absent, report.CompactedUpTo)
				}
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
