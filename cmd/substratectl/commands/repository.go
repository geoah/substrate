package commands

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

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
		a.repositoryRebuildCommand(), a.repositoryResealCommand())
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
						"id": r.ID, "username": r.Username,
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
			fmt.Fprintln(tw, "ID\tUSERNAME\tCREATED\tAGE")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
					r.ID, r.Username, r.CreatedAt.Format(time.RFC3339), humanAge(a.now(), r.CreatedAt))
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
			fmt.Fprintln(tw, "    AUTHORITY\tKINDS\tVERSIONS")
			for _, g := range groupByAuthority(kinds) {
				fmt.Fprintf(tw, "    %s\t%d\t%s\n", g.authority, g.count, strings.Join(g.versions, ","))
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
leaves it exactly as it was.

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
			r, ok := svc.(rebuilder)
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

func (a *app) repositoryResealCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reseal <username>",
		Short: "Move a repository's legacy secret values into the sealed store",
		Long: `Move every legacy secret value into the sealed store and re-point the
records fold and the changelog at the refs.

Secret-typed properties store a ref now; the material lives encrypted in the
sealed store. Values written by earlier releases sit in the database as
plaintext (or as the retired inline-sealed form), and because the changelog
is immutable no ordinary write can ever remove them. This command is the one
sanctioned rewrite of history, and it is values-only: no entry is added,
removed or reordered, no seq moves, and every historical value of a secret
property becomes the record's current ref, so a rebuild afterwards folds the
changelog to byte-identical rows. It also upgrades sealed-store payloads
written while the server ran without a credential key.

It needs the SAME key the server runs with (SUBSTRATE_CREDENTIAL_KEY), holds
the repository's write lock for the duration, and runs as ONE transaction.
It refuses until the repository has been opened once under the upgraded
server (the boot-time vocabulary upgrade must land first), and it is
idempotent. Kinds uninstalled before the migration keep their old bytes: no
declaration survives to say which properties were secret, and the change
feed redacts those kinds' payloads wholesale instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineRead(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			r, ok := svc.(resealer)
			if !ok {
				return seamMissing("ResealRepository")
			}
			report, err := r.ResealRepository(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "repository %s resealed\n", report.Username)
			fmt.Fprintf(a.out, "  id:        %s\n", report.Repository)
			fmt.Fprintf(a.out, "  records:   %d rows migrated into the store\n", report.Records)
			fmt.Fprintf(a.out, "  changelog: %d payloads rewritten\n", report.Entries)
			fmt.Fprintf(a.out, "  sealed:    %d payloads upgraded\n", report.SealedRows)
			fmt.Fprintf(a.out, "  took:      %s\n", report.Took.Round(time.Millisecond))
			return nil
		},
	}
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
const kindKind = coreAuthority + "/kind"

// localAuthority labels the repository-local kinds in a report. Bare names
// have no authority by design, so the label
// is the report's word and never a reference.
const localAuthority = "(repository-local)"

type authorityGroup struct {
	authority string
	count     int
	versions  []string
}

// groupByAuthority folds the declarations into one row per authority: how many
// kinds it publishes here, and which declaration versions are live. More than
// one version under an authority means a partial upgrade — exactly the thing an
// operator opens this command to see.
func groupByAuthority(kinds []declaredKind) []authorityGroup {
	byAuthority := map[string]*authorityGroup{}
	seen := map[string]map[string]bool{}
	for _, k := range kinds {
		authority := vocabulary.KindAuthority(k.ref)
		if authority == "" {
			authority = localAuthority
		}
		g, ok := byAuthority[authority]
		if !ok {
			g = &authorityGroup{authority: authority}
			byAuthority[authority] = g
			seen[authority] = map[string]bool{}
		}
		g.count++
		version := k.version
		if version == "" {
			version = "(unversioned)"
		}
		if !seen[authority][version] {
			seen[authority][version] = true
			g.versions = append(g.versions, version)
		}
	}
	out := make([]authorityGroup, 0, len(byAuthority))
	for _, g := range byAuthority {
		sort.Strings(g.versions)
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].authority < out[j].authority })
	return out
}
