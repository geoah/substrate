package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/config"
	"github.com/geoah/substrate/internal/engine"
)

// `repository blobs`: moving blob bytes between the data root and a bucket
// (internal/engine/blobmigrate.go). Both stores come from the server's own
// variables, SUBSTRATE_DATA_ROOT and SUBSTRATE_BLOB_S3_*, whichever of them
// SUBSTRATE_BLOB_STORE makes live, so the command and the server never
// disagree about where a bucket is.

func (a *app) repositoryBlobsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blobs",
		Short: "Operator: move a repository's blob bytes between the data root and a bucket",
		Long: `Move blob bytes between the two blob stores: fs, under SUBSTRATE_DATA_ROOT,
and s3, the bucket the SUBSTRATE_BLOB_S3_* variables name. Both are read from
the same variables the server reads, whichever SUBSTRATE_BLOB_STORE makes live.

Switching a substrate onto a bucket, with the server stopped throughout:

  substratectl repository blobs migrate --all --to s3
  substratectl repository blobs status  --all --to s3     # exits 0 once complete
  substratectl repository blobs prune   --all --from fs
  # then set SUBSTRATE_BLOB_STORE=s3 and start the server

A server on a bucket refuses to boot while any repository directory still
holds blob bytes, naming each one, so a restore from a snapshot onto such a
host is the same three commands. --to fs and --from s3 run it the other way.`,
	}
	cmd.AddCommand(a.repositoryBlobsStatusCommand(), a.repositoryBlobsMigrateCommand(), a.repositoryBlobsPruneCommand())
	return cmd
}

// blobStores opens the fs store over the data root and the bucket, each
// without opening the engine. The bucket is listed once, so a wrong
// endpoint or key refuses here, before any repository is touched.
func blobStores(ctx context.Context) (blobs config.Blobs, fs, s3 blobbytes.Backend, err error) {
	data, err := config.LoadData()
	if err != nil {
		return blobs, nil, nil, err
	}
	blobs, err = config.LoadBlobs()
	if err != nil {
		return blobs, nil, nil, err
	}
	fsStore, err := blobbytes.NewFS(data.Root)
	if err != nil {
		return blobs, nil, nil, err
	}
	bucket, err := blobs.S3()
	if err != nil {
		return blobs, nil, nil, fmt.Errorf("the bucket is one of the two stores: %w", err)
	}
	if err := bucket.Check(ctx); err != nil {
		return blobs, nil, nil, err
	}
	return blobs, fsStore, bucket, nil
}

// orderStores returns (from, to) for a direction named by the store it ends
// at.
func orderStores(to string, fs, s3 blobbytes.Backend) (blobbytes.Backend, blobbytes.Backend, error) {
	switch to {
	case blobbytes.BackendS3:
		return fs, s3, nil
	case blobbytes.BackendFS:
		return s3, fs, nil
	}
	return nil, nil, fmt.Errorf("unknown store %q: %s or %s", to, blobbytes.BackendFS, blobbytes.BackendS3)
}

// repositoryArgs is the repositories a blobs command walks: the one named,
// or with --all every row of the control plane.
func (a *app) repositoryArgs(ctx context.Context, args []string, all bool) ([]string, error) {
	switch {
	case all && len(args) > 0:
		return nil, errors.New("name a repository or pass --all, not both")
	case !all && len(args) != 1:
		return nil, errors.New("name a repository, or pass --all")
	case !all:
		return args, nil
	}
	db, err := a.controlPlane()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	rows, err := listRepositoryRows(ctx, db)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

func checkOutput(output string) error {
	if output != "" && output != "text" && output != "json" {
		return fmt.Errorf("unknown output format %q: use text or json", output)
	}
	return nil
}

func (a *app) repositoryBlobsStatusCommand() *cobra.Command {
	var (
		to, output string
		all, deep  bool
	)
	cmd := &cobra.Command{
		Use:   "status [<repository>] --to s3|fs",
		Short: "Compare a repository's blob bytes in the two stores; exits 0 only when the destination holds them all",
		Long: `Compare one repository's blob bytes (or every repository's, with --all) in
the store they would leave and the one --to names, and hold every live stored
blob manifest to the destination.

Per repository it prints how many objects each store holds, how many of the
source's are at the destination with the same size, missing, or there with
another size; and how many stored manifests name bytes the destination does
not hold. With --deep every object the destination holds is read and hashed,
so damage of the right length is found too.

It changes nothing and takes no lock, so it runs beside a server; what it
reports is then a moment of stores that may be moving. Exits nonzero unless
the destination is complete, which makes it the gate before a prune or a
switch of SUBSTRATE_BLOB_STORE.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOutput(output); err != nil {
				return err
			}
			ctx := cmd.Context()
			_, fs, s3, err := blobStores(ctx)
			if err != nil {
				return err
			}
			from, dest, err := orderStores(to, fs, s3)
			if err != nil {
				return err
			}
			ids, err := a.repositoryArgs(ctx, args, all)
			if err != nil {
				return err
			}
			svc, err := a.openEngineReadOnly(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			reports := make([]engine.BlobStatusReport, 0, len(ids))
			incomplete := 0
			for _, id := range ids {
				r, err := svc.BlobStatus(ctx, id, from, dest, deep)
				if err != nil {
					return fmt.Errorf("repository %s: %w", id, err)
				}
				if !r.Complete() {
					incomplete++
				}
				reports = append(reports, r)
			}
			if output == "json" {
				if err := printJSON(a.out, reports); err != nil {
					return err
				}
			} else {
				for _, r := range reports {
					printBlobStatus(a.out, r)
				}
			}
			if incomplete > 0 {
				return fmt.Errorf("%d of %d repositories are not complete at %s", incomplete, len(reports), to)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "the store to compare against: s3 or fs (required)")
	cmd.Flags().BoolVar(&all, "all", false, "every repository")
	cmd.Flags().BoolVar(&deep, "deep", false, "read and hash every object the destination holds")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: text|json")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func printBlobStatus(w io.Writer, r engine.BlobStatusReport) {
	fmt.Fprintf(w, "repository %s\n", r.Repository)
	fmt.Fprintf(w, "  from:        %s, %d object(s)\n", r.From, r.Source)
	fmt.Fprintf(w, "  to:          %s, %d object(s)\n", r.To, r.Destination)
	how := "by size"
	if r.Deep {
		how = "by size and hash"
	}
	fmt.Fprintf(w, "  matching:    %d (%s)\n", r.Matching, how)
	fmt.Fprintf(w, "  missing:     %d\n", r.Missing)
	fmt.Fprintf(w, "  mismatched:  %d\n", r.Mismatched)
	fmt.Fprintf(w, "  manifests:   %d stored, %d without their bytes at the destination\n", r.Manifests, r.ManifestsMissing)
	if r.Complete() {
		fmt.Fprintln(w, "  complete")
	}
}

func (a *app) repositoryBlobsMigrateCommand() *cobra.Command {
	var (
		to, output   string
		all          bool
		dryRun, deep bool
		concurrency  int
	)
	cmd := &cobra.Command{
		Use:   "migrate [<repository>] --to s3|fs",
		Short: "Copy a repository's blob bytes into the other store, hash-checked both sides",
		Long: `Copy one repository's blob bytes (or every repository's, with --all) into the
store --to names. Every source object is read and hashed against its digest
before it is written, and read and hashed again at the destination after.

An object the destination already holds at the same size is left alone, so a
second run picks up where an interrupted one stopped. With --deep the
destination's copy is also read and hashed, and one that does not hash is
written again. A source object that does not hash to its digest is reported
and never copied. Nothing is deleted from either store: 'repository blobs
prune' is the separate step that removes the source's copies.

STOP THE SERVER FIRST. Each repository is opened as its writer, as a snapshot
opens it, so the command refuses beside a running server, and a server that
starts while it runs is refused. A repository directory restored under the
data root with no row yet is imported first, as the server's boot would.

Exits nonzero when any object was damaged at the source or failed to copy.

  substratectl repository blobs migrate --all --to s3
  substratectl repository blobs migrate ada.example.com --to s3 --deep`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOutput(output); err != nil {
				return err
			}
			ctx := cmd.Context()
			_, fs, s3, err := blobStores(ctx)
			if err != nil {
				return err
			}
			from, dest, err := orderStores(to, fs, s3)
			if err != nil {
				return err
			}
			if !all && len(args) != 1 {
				return errors.New("name a repository, or pass --all")
			}
			// The engine first: its boot check imports a restored directory
			// that has no row, so --all then lists it.
			svc, err := a.openEngineMigrating(ctx)
			if err != nil {
				return lockHint(err)
			}
			defer func() { _ = svc.Close() }()
			ids, err := a.repositoryArgs(ctx, args, all)
			if err != nil {
				return err
			}
			opts := engine.BlobMigrateOptions{DryRun: dryRun, Deep: deep, Concurrency: concurrency}
			reports := make([]engine.BlobMigrateReport, 0, len(ids))
			failed := 0
			for _, id := range ids {
				r, err := svc.MigrateBlobs(ctx, id, from, dest, opts)
				if err != nil {
					return lockHint(fmt.Errorf("repository %s: %w", id, err))
				}
				if !r.OK() {
					failed++
				}
				reports = append(reports, r)
				if output != "json" {
					printBlobMigrate(a.out, r)
				}
			}
			if output == "json" {
				if err := printJSON(a.out, reports); err != nil {
					return err
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d repositories did not migrate whole", failed, len(reports))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "the store to copy into: s3 or fs (required)")
	cmd.Flags().BoolVar(&all, "all", false, "every repository")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "compare and report, copy nothing")
	cmd.Flags().BoolVar(&deep, "deep", false, "read and hash every object the destination already holds, and rewrite one that does not hash")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "objects copied at once")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: text|json")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func printBlobMigrate(w io.Writer, r engine.BlobMigrateReport) {
	verb := "copied"
	if r.DryRun {
		verb = "to copy"
	}
	fmt.Fprintf(w, "repository %s\n", r.Repository)
	fmt.Fprintf(w, "  from:     %s, %d object(s), %d bytes\n", r.From, r.Source, r.SourceBytes)
	fmt.Fprintf(w, "  to:       %s\n", r.To)
	fmt.Fprintf(w, "  present:  %d already there\n", r.Present)
	fmt.Fprintf(w, "  %-9s %d (%d bytes), %d of them replacing a damaged copy\n", verb+":", r.Copied, r.CopiedBytes, r.Repaired)
	for _, d := range r.SourceDamaged {
		fmt.Fprintf(w, "  DAMAGED:  %s does not hash to its digest at the source; not copied\n", d)
	}
	for _, f := range r.Failed {
		fmt.Fprintf(w, "  FAILED:   %s\n", f)
	}
	fmt.Fprintf(w, "  took:     %s\n", r.Took.Round(time.Millisecond))
}

func (a *app) repositoryBlobsPruneCommand() *cobra.Command {
	var (
		from, output string
		all, dryRun  bool
	)
	cmd := &cobra.Command{
		Use:   "prune [<repository>] --from fs|s3",
		Short: "Delete a repository's blob bytes from the store it is leaving, each once the other store's copy hashes",
		Long: `Delete one repository's blob bytes (or every repository's, with --all) from
the store --from names, which must NOT be the live one: --from equal to
SUBSTRATE_BLOB_STORE is refused.

Each object is deleted only after the other store's copy has been read and
hashed against its digest, right before the delete. An object the other
store does not hold is kept and reported. One whose copy does not hash stops
the prune for that repository: a store that holds damage under one name may
hold it under others, so run 'repository blobs migrate --deep' and prune
again.

STOP THE SERVER FIRST, for the same reason as migrate.

Exits nonzero when anything was kept.

  substratectl repository blobs prune --all --from fs --dry-run
  substratectl repository blobs prune --all --from fs`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOutput(output); err != nil {
				return err
			}
			ctx := cmd.Context()
			blobs, fs, s3, err := blobStores(ctx)
			if err != nil {
				return err
			}
			if from == blobs.Live() {
				return fmt.Errorf("refusing to prune %s: it is the live store (SUBSTRATE_BLOB_STORE=%s), and prune deletes from the store being left", from, blobs.Live())
			}
			var kept string
			switch from {
			case blobbytes.BackendFS:
				kept = blobbytes.BackendS3
			case blobbytes.BackendS3:
				kept = blobbytes.BackendFS
			}
			source, keptStore, err := orderStores(kept, fs, s3)
			if err != nil {
				return err
			}
			if !all && len(args) != 1 {
				return errors.New("name a repository, or pass --all")
			}
			svc, err := a.openEngineMigrating(ctx)
			if err != nil {
				return lockHint(err)
			}
			defer func() { _ = svc.Close() }()
			ids, err := a.repositoryArgs(ctx, args, all)
			if err != nil {
				return err
			}
			reports := make([]engine.BlobPruneReport, 0, len(ids))
			partial := 0
			for _, id := range ids {
				r, err := svc.PruneBlobs(ctx, id, source, keptStore, engine.BlobPruneOptions{DryRun: dryRun})
				if err != nil {
					return lockHint(fmt.Errorf("repository %s: %w", id, err))
				}
				if !r.OK() {
					partial++
				}
				reports = append(reports, r)
				if output != "json" {
					printBlobPrune(a.out, r)
				}
			}
			if output == "json" {
				if err := printJSON(a.out, reports); err != nil {
					return err
				}
			}
			if partial > 0 {
				return fmt.Errorf("%d of %d repositories kept objects at %s", partial, len(reports), from)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "the store to delete from: fs or s3, never the live one (required)")
	cmd.Flags().BoolVar(&all, "all", false, "every repository")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "check every object and delete nothing")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: text|json")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

func printBlobPrune(w io.Writer, r engine.BlobPruneReport) {
	verb := "deleted"
	if r.DryRun {
		verb = "to delete"
	}
	fmt.Fprintf(w, "repository %s\n", r.Repository)
	fmt.Fprintf(w, "  from:     %s\n", r.From)
	fmt.Fprintf(w, "  kept in:  %s\n", r.Kept)
	fmt.Fprintf(w, "  %-9s %d (%d bytes), each after its copy hashed\n", verb+":", r.Deleted, r.DeletedBytes)
	for _, d := range r.Absent {
		fmt.Fprintf(w, "  KEPT:     %s is not in %s\n", d, r.Kept)
	}
	if r.Mismatched != "" {
		fmt.Fprintf(w, "  STOPPED:  %s in %s does not hash to its digest; nothing more deleted here\n", r.Mismatched, r.Kept)
	}
	fmt.Fprintf(w, "  took:     %s\n", r.Took.Round(time.Millisecond))
}
