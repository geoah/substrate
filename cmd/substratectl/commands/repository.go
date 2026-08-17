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
		a.repositoryResealCommand(), a.repositoryVerifyCommand())
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
	var forceUnverified bool
	cmd := &cobra.Command{
		Use:     "rebuild <username>",
		Short:   "Replay a repository's changelog into a fresh fold",
		Aliases: []string{"rebuild-repository"},
		Long: `Clear a repository's fold and replay its whole changelog into it.

The changelog is the truth and the records table is a fold of it, so this is the
containment test made runnable. It holds the repository's write lock for the
duration and runs as ONE transaction: a rebuild either replaces the fold or
leaves it exactly as it was.

The chain is verified FIRST: a changelog whose hashes (or signatures) do not
check out refuses to become the live fold. --force-unverified rebuilds anyway
and says so in the report; run 'repository verify' first to see what it would
be installing.

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
			var report engine.RebuildReport
			if forceUnverified {
				r, ok := svc.(engine.ForceRebuilder)
				if !ok {
					return seamMissing("RebuildRepositoryUnverified")
				}
				report, err = r.RebuildRepositoryUnverified(cmd.Context(), args[0])
			} else {
				r, ok := svc.(engine.Rebuilder)
				if !ok {
					return seamMissing("RebuildRepository")
				}
				report, err = r.RebuildRepository(cmd.Context(), args[0])
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "repository %s rebuilt\n", report.Username)
			fmt.Fprintf(a.out, "  id:       %s\n", report.Repository)
			fmt.Fprintf(a.out, "  replayed: %d entries to head %d\n", report.Entries, report.Head)
			fmt.Fprintf(a.out, "  records:  %d\n", report.Records)
			fmt.Fprintf(a.out, "  took:     %s\n", report.Took.Round(time.Millisecond))
			if report.Unverified {
				fmt.Fprintf(a.out, "  WARNING:  the fold is built from UNVERIFIED history (--force-unverified)\n")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&forceUnverified, "force-unverified", false,
		"rebuild even when the chain does not verify: the fold is then built from unverified history")
	return cmd
}

func (a *app) repositoryVerifyCommand() *cobra.Command {
	var output, expectKey, expectHead string
	var expectSignedFrom int64
	cmd := &cobra.Command{
		Use:   "verify <username>",
		Short: "Walk a repository's changelog chain and check every hash and signature",
		Long: `Recompute every changelog entry's chain hash from the stored bytes and check
every signature the signing state requires, in one read-only snapshot. It
never backfills, repairs or touches the repository it judges (opening the
engine still applies any pending schema migration, as every operator command
does).

Everything in the database can be rewritten by whoever holds the database, so
a verify that trusts only the database proves internal consistency. The
--expect flags are the difference: pass the (public key, signed-from) pair
logged at activation and a previously reported head as seq:hash, and the
comparison the scheme rests on becomes enforced instead of eyeballed. A
pinned head is also the ONLY way to catch a truncated tail.

Chain epochs (backfill, reseal, signing activation) are listed and checked so
a head that legitimately moved is explained; run with a stale --expect-head
after a reseal and the finding says to re-pin rather than crying tamper.

Exits nonzero when anything does not verify.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pins := engine.VerifyPins{PublicKey: expectKey, SignedFrom: expectSignedFrom}
			if expectHead != "" {
				seqStr, hash, ok := strings.Cut(expectHead, ":")
				if !ok {
					return fmt.Errorf("--expect-head wants seq:hash, got %q", expectHead)
				}
				seq, err := strconv.ParseInt(seqStr, 10, 64)
				if err != nil || seq <= 0 || hash == "" {
					return fmt.Errorf("--expect-head wants a positive seq and a hex hash, got %q", expectHead)
				}
				pins.HeadSeq, pins.HeadHash = seq, hash
			}
			svc, err := a.openEngineRead(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			v, ok := svc.(engine.Verifier)
			if !ok {
				return seamMissing("VerifyRepositoryPinned")
			}
			report, err := v.VerifyRepositoryPinned(cmd.Context(), args[0], pins)
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
					fmt.Fprintf(a.out, "  head hash: %s\n", report.HeadHash)
				}
				if report.SignedFrom > 0 {
					fmt.Fprintf(a.out, "  signed:   from seq %d, public key %s\n", report.SignedFrom, report.PublicKey)
				} else {
					fmt.Fprintln(a.out, "  signed:   no (signing has not been activated)")
				}
				if report.UnsignedEntries > 0 {
					fmt.Fprintf(a.out, "  unsigned: %d entries below the activation seq — history from before signing, permanently unattested\n", report.UnsignedEntries)
				}
				for _, ep := range report.Epochs {
					line := fmt.Sprintf("  epoch:    %s at %s, from seq %d", ep.Reason, ep.At.Format(time.RFC3339), ep.FromSeq)
					if ep.Signed {
						switch {
						case ep.SigOK == nil:
							line += " (signed)"
						case *ep.SigOK:
							line += " (signed, verifies)"
						default:
							line += " (signed, DOES NOT VERIFY)"
						}
					}
					fmt.Fprintln(a.out, line)
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
	cmd.Flags().StringVar(&expectKey, "expect-public-key", "", "the hex public key logged at activation; a mismatch is a finding")
	cmd.Flags().Int64Var(&expectSignedFrom, "expect-signed-from", 0, "the activation seq logged beside the key; a mismatch is a finding")
	cmd.Flags().StringVar(&expectHead, "expect-head", "", "a previously reported head as seq:hash; a mismatch no epoch explains is a finding")
	return cmd
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
			r, ok := svc.(engine.Resealer)
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
	sort.Slice(out, func(i, j int) bool { return out[i].authority < out[j].authority })
	return out
}
