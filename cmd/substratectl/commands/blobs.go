package commands

// THE BLOB BYTES, moved from one store to another. Operator hat: it opens the
// database over the DSN and the target store over the same environment the
// server reads, and it never speaks HTTP.
//
// It does not open the engine. Nothing here writes a record: the blob manifest
// stays exactly where it is, in Postgres, and only the bytes move. That is
// also why the server's backend-switch refusal cannot lock this command out —
// there is no boot to refuse.

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/config"
)

func (a *app) blobsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "blobs",
		Short:   "Operator: move blob bytes between stores (direct database, no HTTP)",
		Aliases: []string{"blob"},
	}
	cmd.AddCommand(a.blobsMigrateCommand())
	return cmd
}

func (a *app) blobsMigrateCommand() *cobra.Command {
	var from, to string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "migrate [username]",
		Short: "Move blob bytes from one store to another, repository by repository",
		Long: `Move every blob's bytes from one store to another, one repository at a
time, and delete each object from the source only once the target holds it.

The manifest does not move: it is a record in Postgres and it is the truth
whichever store the bytes sit in. Nothing is written to the changelog, no seq
moves, and a rebuild afterwards folds to the same rows.

--from defaults to postgres and --to defaults to SUBSTRATE_BLOB_STORE, so the
ordinary upgrade is to set the new store's variables, run this once, and start
the server. Both stores are configured from the same environment: the fs and
s3 variables describe whichever side names that backend.

It is resumable and re-runnable. An object the target already holds is not
copied again, and one the source no longer holds was already moved, so a run
interrupted anywhere continues by being run again. Stop the server first: a
blob uploaded while this runs may be left in the source store, where the next
run picks it up.

With no username it moves every repository; with one, only that user's.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			blobs, err := config.LoadBlobs()
			if err != nil {
				return err
			}
			if to == "" {
				to = blobs.Store
			}
			if from == to {
				return fmt.Errorf("--from and --to are both %q: there is nothing to move", from)
			}
			source, err := namedBackend(blobs, from)
			if err != nil {
				return fmt.Errorf("--from: %w", err)
			}
			target, err := namedBackend(blobs, to)
			if err != nil {
				return fmt.Errorf("--to: %w", err)
			}

			db, err := a.controlPlane()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			rows, err := listRepositoryRows(cmd.Context(), db)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				row, err := repositoryRowByUsername(cmd.Context(), db, args[0])
				if err != nil {
					return err
				}
				rows = []repositoryRow{row}
			}

			started := time.Now()
			total := 0
			for _, row := range rows {
				moved, err := a.migrateRepositoryBlobs(cmd.Context(), row, source, target, dryRun)
				if err != nil {
					return fmt.Errorf("repository %s: %w", row.Username, err)
				}
				total += moved
				fmt.Fprintf(a.out, "%s: %d blobs\n", row.Username, moved)
			}
			verb := "moved"
			if dryRun {
				verb = "would move"
			}
			fmt.Fprintf(a.out, "%s %d blobs from %s to %s in %s\n",
				verb, total, from, to, time.Since(started).Round(time.Millisecond))
			if !dryRun && total > 0 {
				fmt.Fprintf(a.out, "set SUBSTRATE_BLOB_STORE=%s before starting the server\n", to)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", blobbytes.BackendPostgres, "the store the bytes are in now (postgres, fs, s3)")
	cmd.Flags().StringVar(&to, "to", "", "the store to move them into (default $SUBSTRATE_BLOB_STORE)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "count what would move and touch nothing")
	return cmd
}

// namedBackend builds one backend by name out of the environment's blob
// configuration, so `--from fs --to s3` reads both halves from the same place
// the server reads them.
func namedBackend(blobs config.Blobs, name string) (blobbytes.Backend, error) {
	blobs.Store = name
	return blobs.Backend()
}

// migrateRepositoryBlobs moves one repository's bytes. The order per object is
// copy, verify, then delete the source: an interruption leaves the object in
// both stores, never in neither.
func (a *app) migrateRepositoryBlobs(ctx context.Context, row repositoryRow, source, target blobbytes.Backend, dryRun bool) (int, error) {
	// The repository-scoped pool, which is what row level security binds. The
	// postgres side of the move reads and writes `blobs` through it, so this
	// command cannot touch another repository's rows even by mistake.
	scoped, err := a.openScoped(ctx, row.ID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = scoped.Close() }()

	src, err := source.Repository(row.ID, scoped)
	if err != nil {
		return 0, err
	}
	dst, err := target.Repository(row.ID, scoped)
	if err != nil {
		return 0, err
	}

	if dryRun {
		return blobbytes.Count(ctx, src)
	}
	return blobbytes.Move(ctx, src, dst)
}
