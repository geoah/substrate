package commands

// THE BLOB BYTES, moved out of the Postgres `blobs` column into the store the
// server runs on. Operator hat: it opens the database over the DSN and the
// target store over the same environment the server reads, and it never
// speaks HTTP.
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
		Short:   "Operator: move blob bytes out of the Postgres column (direct database, no HTTP)",
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
		Short: "Move blob bytes out of the Postgres `blobs` column into the configured store, repository by repository",
		Long: `Move every blob's bytes out of the Postgres ` + "`blobs`" + ` column into the store the
server runs on, one repository at a time, and delete each row only once the
target holds the object.

The column is no longer a blob store: the server refuses to boot while it
holds a row, and this command is the one way out. --from accepts only
postgres. --to defaults to SUBSTRATE_BLOB_STORE (fs, under SUBSTRATE_DATA_ROOT,
or s3), so the ordinary upgrade is to set the data root, run this once, and
start the server. The target is configured from the same environment the
server reads.

The manifest does not move: it is a record in Postgres and it is the truth
whichever store the bytes sit in. Nothing is written to the changelog, no seq
moves, and a rebuild afterwards folds to the same rows.

It is resumable and re-runnable. An object the target already holds is not
copied again, and a row the column no longer holds was already moved, so a
run interrupted anywhere continues by being run again. Stop the server first.

With no username it moves every repository; with one, only that user's.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if from != blobbytes.BackendPostgres {
				return fmt.Errorf("--from %q: the only source is %s, the column this command empties", from, blobbytes.BackendPostgres)
			}
			blobs, err := config.LoadBlobs()
			if err != nil {
				return err
			}
			if to != "" {
				blobs.Store = to
			}
			data, err := config.LoadData()
			if err != nil {
				return err
			}
			// config.Blobs.Backend refuses postgres by name, so --to postgres
			// is refused there with the same message the server gives.
			target, err := blobs.Backend(data.Root)
			if err != nil {
				return fmt.Errorf("--to: %w", err)
			}
			source := blobbytes.NewPostgres()

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
				verb, total, from, target.Name(), time.Since(started).Round(time.Millisecond))
			if !dryRun && total > 0 {
				fmt.Fprintf(a.out, "set SUBSTRATE_BLOB_STORE=%s before starting the server\n", target.Name())
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", blobbytes.BackendPostgres, "the store the bytes are in now; only postgres, the column this command empties")
	cmd.Flags().StringVar(&to, "to", "", "the store to move them into: fs or s3 (default $SUBSTRATE_BLOB_STORE)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "count what would move and touch nothing")
	return cmd
}

// migrateRepositoryBlobs moves one repository's bytes. The order per object is
// copy, verify, then delete the source: an interruption leaves the object in
// both stores, never in neither.
func (a *app) migrateRepositoryBlobs(ctx context.Context, row repositoryRow, source, target blobbytes.Backend, dryRun bool) (int, error) {
	// The repository-scoped pool, which is what row level security binds. The
	// postgres source reads and deletes `blobs` rows through it, so this
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
