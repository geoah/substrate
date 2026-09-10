package commands

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/changelogfile"
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
// repositories, because users cannot see each other. `rewrap` is the one that
// takes no database at all: it acts on a copied directory before any server
// has imported it.

func (a *app) repositoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "repository",
		Short:   "Operator: inspect and rebuild repositories (direct database, no HTTP; rewrap reads a directory and no database)",
		Aliases: []string{"repositories", "repo"},
	}
	cmd.AddCommand(a.repositoryListCommand(), a.repositoryInspectCommand(),
		a.repositoryRebuildCommand(), a.repositoryVerifyCommand(),
		a.repositorySnapshotCommand(), a.repositoryRewrapCommand(),
		a.repositoryRotateGenerationCommand())
	return cmd
}

func (a *app) repositorySnapshotCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "snapshot <repository> <destination root>",
		Short: "Write a verified copy of a repository's directory that records the point it holds",
		Long: `Copy one repository's directory into a destination root, verified before and
after, with snapshot.json recording the committed point the copy holds.

The copy lands at <destination root>/repositories/<authority>/, the layout a
data root has, so a restore copies it straight under SUBSTRATE_DATA_ROOT. It
holds the manifest, every changelog segment and sidecar, every committed
sealed file and, under the fs blob store, the bytes of every stored blob,
each hashed against its digest on the way. Under the s3 blob store the bytes
stay in the bucket: snapshot.json lists the objects the copy needs and where
they are, and the restore copies them. snapshot.json is written last and
names the head seq, its checksum and when the copy was taken; 'repository
verify' on the restored repository prints it and holds the files to it.

Before anything is copied the repository is verified whole: the changelog in
both places, every stored blob's bytes, every live secret reference and, under
SUBSTRATE_CREDENTIAL_KEY, every sealed file opened. A finding refuses the
snapshot and is printed. The key is required for that reason. A destination
that already holds a directory for the repository is refused: a snapshot is
a fresh copy, never a merge over an older one. The copy is built beside the
destination and renamed into place once snapshot.json is on disk, so a failed
snapshot leaves nothing there and the same destination takes the retry. The
copy is read back before that: every segment's checksums, every sealed file
opened under the key, every blob hashed.

STOP THE SERVER FIRST. The snapshot opens the repository as its changelog
writer so that no write can land while it copies, and a running server holds
that lock; the command refuses rather than copy beside it, and a server that
opens the repository first while the snapshot runs meets the same lock. Run it
with the binary the server runs, as with 'rebuild': the open stamps the
repository with this binary's dialects, which an older server then refuses.

  SUBSTRATE_CREDENTIAL_KEY=… substratectl repository snapshot ada /srv/substrate-backup/2026-09-08`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if output != "" && output != "text" && output != "json" {
				return fmt.Errorf("unknown output format %q: use text or json", output)
			}
			// The key before the DSN: the refusal names what is missing
			// before anything is opened.
			if os.Getenv(credentialKeyEnv) == "" {
				return fmt.Errorf("refusing to snapshot: set %s to the key the server runs with; the snapshot proves every sealed file opens under it before it copies", credentialKeyEnv)
			}
			dest, err := filepath.Abs(args[1])
			if err != nil {
				return err
			}
			svc, err := a.openEngineWrite(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			sn, ok := svc.(engine.Snapshotter)
			if !ok {
				return seamMissing("SnapshotRepository")
			}
			report, err := sn.SnapshotRepository(cmd.Context(), args[0], dest)
			if err != nil {
				return lockHint(err)
			}
			if output == "json" {
				return printJSON(a.out, report)
			}
			fmt.Fprintf(a.out, "repository %s snapshot written\n", report.Repository)
			fmt.Fprintf(a.out, "  directory: %s\n", report.Directory)
			fmt.Fprintf(a.out, "  point:     seq %d, checksum %s\n", report.Head, report.HeadHash)
			fmt.Fprintf(a.out, "  changelog: %d segment(s)\n", report.Segments)
			fmt.Fprintf(a.out, "  sealed:    %d file(s), every one opened under %s\n", report.SealedFiles, credentialKeyEnv)
			if report.BlobLocation != "" {
				fmt.Fprintf(a.out, "  blobs:     %d object(s) listed in %s under %s; copy them with the directory\n",
					report.Blobs, changelogfile.SnapshotName, report.BlobLocation)
			} else {
				fmt.Fprintf(a.out, "  blobs:     %d copied (%d bytes), each hashed against its digest\n", report.Blobs, report.BlobBytes)
			}
			fmt.Fprintf(a.out, "  took:      %s\n", report.Took.Round(time.Millisecond))
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: text|json")
	return cmd
}

func (a *app) repositoryRewrapCommand() *cobra.Command {
	var (
		identityFile  string
		identityStdin bool
		output        string
	)
	cmd := &cobra.Command{
		Use:   "rewrap <repository directory>",
		Short: "Operator: open a copied repository directory with its recovery key for a new credential key (no database)",
		Long: `Rewrite a repository directory's manifest so this host's SUBSTRATE_CREDENTIAL_KEY
opens it, using the user's recovery key.

A repository directory copied to a host without the credential key it was
written under refuses to import: repository.json carries the repository's
data-encryption key wrapped under that key. The same key sits in the changelog,
wrapped to the user's age recipient in the recoverykey record, and this command
opens that wrap with the recovery key (the AGE-SECRET-KEY-1... line the user
kept), proves it opens every file under sealed/, wraps it under
SUBSTRATE_CREDENTIAL_KEY and rewrites repository.json. Booting the server with
the directory under its data root then imports it as usual.

It is offline: no database, no server, no HTTP. It takes the directory itself,
which must be named by the repository's authority, as a copy of
<root>/repositories/<authority> is. The recovery key is read from --identity-file
or from stdin, never from an argument, and neither it nor the data-encryption
key is printed or logged. A directory with no manifest, or whose changelog
holds no recoverykey record, is refused: nothing is synthesized, and a refused
rewrap leaves the directory as it was. The manifest is written under the
changelog writer lock, so a server that has opened the repository is refused;
stop the server either way, because one that has not opened it yet is not.

The destination database must hold no row for the repository: the boot
imports a directory that has no row, and a directory that has one is
reconciled FROM the row, which writes the row's wrap back over the manifest.
Rewrap the copy in a restore location, move it under the data root of a
server whose database has never held this repository, then boot.

  SUBSTRATE_CREDENTIAL_KEY=… substratectl repository rewrap /srv/restore/repositories/ada.example.com
  SUBSTRATE_CREDENTIAL_KEY=… substratectl repository rewrap ./ada.example.com --identity-file ./recovery.key`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Every refusal that needs no secret comes first: a bad flag must
			// not be discovered after the manifest is rewritten, and nobody
			// should paste a recovery key to learn the key is missing. The
			// key check keeps the order `user reset` keeps.
			if output != "" && output != "text" && output != "json" {
				return fmt.Errorf("unknown output format %q: use text or json", output)
			}
			if identityFile != "" && identityStdin {
				return errors.New("--identity-file and --identity-stdin name two sources for one recovery key: pass one")
			}
			credKey := os.Getenv(credentialKeyEnv)
			if credKey == "" {
				return fmt.Errorf("refusing to rewrap: set %s to the key this host's server runs with; the manifest is rewritten so that key opens the repository", credentialKeyEnv)
			}
			if err := config.ValidateCredentialKey(credKey); err != nil {
				return err
			}
			identity, err := a.recoveryIdentity(identityFile, identityStdin)
			if err != nil {
				return err
			}
			report, err := engine.RewrapRepositoryDir(args[0], identity, credKey)
			if err != nil {
				return lockHint(err)
			}
			if output == "json" {
				return printJSON(a.out, report)
			}
			fmt.Fprintf(a.out, "repository %s rewrapped\n", report.Repository)
			fmt.Fprintf(a.out, "  recovery key: recoverykey record at seq %d opened\n", report.RecoveryKeySeq)
			fmt.Fprintf(a.out, "  sealed:      %d file(s) open under the recovered key\n", report.SealedFiles)
			fmt.Fprintf(a.out, "  manifest:    %s rewritten under %s\n", changelogfile.ManifestName, credentialKeyEnv)
			fmt.Fprintln(a.out, "boot the server with the directory under SUBSTRATE_DATA_ROOT to import it, then `repository verify`")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&identityFile, "identity-file", "", "read the recovery key from this file (one line)")
	f.BoolVar(&identityStdin, "identity-stdin", false, "read the recovery key from stdin (one line)")
	f.StringVarP(&output, "output", "o", "", "output format: text|json")
	return cmd
}

// recoveryIdentity resolves the recovery key for a rewrap: the named file,
// else stdin, else a prompt that does not echo. Never an argument, where it
// would land in the shell history and the process table. The file is read as
// age reads an identity file (`age-keygen -o` writes comment lines before the
// key), and stdin is one line.
func (a *app) recoveryIdentity(file string, fromStdin bool) (string, error) {
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return "", fmt.Errorf("read the recovery key: %w", err)
		}
		defer func() { _ = f.Close() }()
		return parseRecoveryIdentity(f)
	}
	line, err := a.secret(fromStdin, "Recovery key: ")
	if err != nil {
		return "", err
	}
	if line == "" {
		return "", errors.New("a recovery key is required: the AGE-SECRET-KEY-1... line kept at registration")
	}
	return parseRecoveryIdentity(strings.NewReader(line))
}

// parseRecoveryIdentity reads exactly one age X25519 identity out of r. The
// refusals carry no part of what was read: a mis-pasted line is still a
// secret.
func parseRecoveryIdentity(r io.Reader) (string, error) {
	ids, err := age.ParseIdentities(r)
	if err != nil {
		return "", errors.New("the recovery key is not an age identity file: expected one AGE-SECRET-KEY-1... line, comments allowed")
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("the recovery key must be exactly one age identity, got %d", len(ids))
	}
	id, ok := ids[0].(*age.X25519Identity)
	if !ok {
		return "", errors.New("the recovery key is not an X25519 age identity (AGE-SECRET-KEY-1...)")
	}
	return id.String(), nil
}

func (a *app) repositoryListCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every repository on this substrate",
		Long: `List the control-plane table: one row per repository, and the whole of it.

The authority is the repository's id, and the directory under
SUBSTRATE_DATA_ROOT is named by it; created_at is the admission record, since
the invite code is the only door and there is nothing else to record.`,
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
						"authority": r.ID,
						"createdAt": r.CreatedAt.Format(time.RFC3339),
						"dekKeyId":  r.DEKKeyID,
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
			fmt.Fprintln(tw, "AUTHORITY\tCREATED\tAGE")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\n",
					r.ID, r.CreatedAt.Format(time.RFC3339), humanAge(a.now(), r.CreatedAt))
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
		Short: "Show one repository: authority, both changelog heads, records, vocabulary versions",
		Long: `Describe a repository from the outside.

The changelog's head is its length — seq is per-repository, gapless and assigned at
commit — and the records count is the fold's size. The changelog lives twice:
the table is the live index and the segment files under SUBSTRATE_DATA_ROOT
(<root>/repositories/<authority>/changelog) are the copy a backup takes, so both heads
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
			repo, err := repositoryRowByID(cmd.Context(), db, args[0])
			if err != nil {
				return err
			}
			scoped, err := a.openScoped(cmd.Context(), repo.ID)
			if err != nil {
				return err
			}
			defer func() { _ = scoped.Close() }()

			fmt.Fprintf(a.out, "repository %s\n", repo.ID)
			fmt.Fprintf(a.out, "  created:   %s (%s)\n",
				repo.CreatedAt.Format(time.RFC3339), humanAge(a.now(), repo.CreatedAt))
			fmt.Fprintf(a.out, "  dek:       %s\n", describeKeys(repo))

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
// read-only (changelogfile.Verify), so an inspect never repairs an incomplete tail.
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
		fmt.Fprintf(out, "  changelog files: the active segment ends in an incomplete transaction: %d bytes past the last complete one, %d complete line(s) among them (the next open cuts them)\n",
			rep.TruncatedBytes, rep.TruncatedEntries)
	}
}

func (a *app) repositoryRebuildCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rebuild <repository>",
		Short: "Replay a repository's changelog into a fresh fold",
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
	cmd := &cobra.Command{
		Use:   "verify <repository>",
		Short: "Walk a repository's changelog files and table and check every checksum",
		Long: `Walk a repository's changelog in both places and hold them to each other.

The segment files under SUBSTRATE_DATA_ROOT are walked whole: every line's
checksum, every finished segment's sidecar digest, the seq sequence. The table
is walked from seq 1 to the head in one read-only snapshot: the sequence must
be gapless, and every entry's checksum, recomputed from the stored row, must
equal the one stamped when the entry was written and the one the file's line
carries. Both heads must agree, and every sealed row must have its file and
every sealed file its row.

The side stores are held to the fold: every blob whose manifest says stored is
read out of the configured blob store and hashed against its digest, and every
secret reference a live record holds must have its sealed file. With
SUBSTRATE_CREDENTIAL_KEY set, every sealed file is opened under the
repository's key; without it the files are compared with the rows and nothing
is opened. A directory that is a snapshot, or was restored from one, carries
snapshot.json: its recorded point is printed and the entry it names must be in
the files with the recorded checksum.

It never repairs or touches the repository it judges, and it runs beside a live
server: the engine opens read-only against the data root, so an incomplete
final transaction or a table ahead of its file is reported as a finding, never
cut or caught up
(opening the engine still applies any pending schema migration, as every
operator command does). Against a server that is mid-write a finding about the
heads, or about a blob the sweep collected a moment ago, can be a write in
flight; run it again before believing it.

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
				fmt.Fprintf(a.out, "repository %s\n", report.Repository)
				fmt.Fprintf(a.out, "  table:    %d entries, head %d\n", report.Entries, report.Head)
				fmt.Fprintf(a.out, "  files:    head %d in %d segment(s)\n", report.FileHead, report.Segments)
				fmt.Fprintf(a.out, "  sealed:   %d rows, %d files\n", report.SealedRows, report.SealedFiles)
				if report.SealedOpened > 0 || os.Getenv(credentialKeyEnv) != "" {
					fmt.Fprintf(a.out, "  sealed:   %d file(s) opened under %s\n", report.SealedOpened, credentialKeyEnv)
				} else {
					fmt.Fprintf(a.out, "  sealed:   not opened (set %s to prove every file opens)\n", credentialKeyEnv)
				}
				fmt.Fprintf(a.out, "  secrets:  %d live reference(s) held to the sealed files\n", report.SecretRefs)
				fmt.Fprintf(a.out, "  blobs:    %d stored, %d bytes hashed\n", report.Blobs, report.BlobBytes)
				if report.HeadHash != "" {
					fmt.Fprintf(a.out, "  head checksum: %s\n", report.HeadHash)
				}
				if report.Snapshot != nil {
					fmt.Fprintf(a.out, "  recovery point: seq %d, checksum %s, taken %s\n",
						report.Snapshot.Head, report.Snapshot.HeadHash, report.Snapshot.TakenAt.Format(time.RFC3339))
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
