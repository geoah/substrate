package commands

// The operator's hand in the archive (decision 0279): what a pass that
// stopped leaves for a person to decide. Every command here is the
// operator's hat: a DSN, the engine opened exclusively, never beside the
// server.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func (a *app) archiveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "archive",
		Short: "Operator: settle a repository whose archive is not as its database left it",
		Long: `Settle a repository whose archive is not as its database left it.

A repository's archive is a copy of its database made after the commits
(docs/operations.md, "The archive's contract"). Each pass first holds the
store to what the database recorded of its last publication. When the store
holds something else (a newer archive, a manifest the database did not
publish, two manifests of one epoch, a deletion mark, the published manifest
gone), the pass writes and deletes nothing, the server goes on serving, and
'repository inspect' and the substrate_archive_stopped gauge say so. Nothing
decides between the two on its own: 'archive report' says what each side
holds that the other lacks, and 'archive resolve' keeps one of them, whole.

Every command here opens the database as its writer: stop the server first.`,
	}
	cmd.AddCommand(a.archiveReportCommand(), a.archiveResolveCommand(), a.archiveRemoveCommand(), a.archiveImportCommand())
	return cmd
}

func (a *app) archiveReportCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "report <repository>",
		Short: "Say what the database and the archive each hold that the other lacks",
		Long: `Say what the database and the archive each hold that the other lacks.

It lists every archive and deletion mark the store holds of the repository,
the archive the database writes and the manifest it last published, the
manifest a boot with no rows would restore, and, against that manifest: the
lines both hold that differ (compared only when both are in one changelog
dialect), the lines past the other's head on each side, the sealed values by
ref, and the blobs by digest. It changes nothing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if output != "" && output != "text" && output != "json" {
				return fmt.Errorf("unknown output format %q: use text or json", output)
			}
			svc, err := a.openEngineExclusive(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			r, err := svc.ArchiveReport(cmd.Context(), args[0])
			if err != nil {
				return lockHint(err)
			}
			if output == "json" {
				return printJSON(a.out, r)
			}
			fmt.Fprintf(a.out, "repository %s in %s\n", r.Repository, r.Store)
			fmt.Fprintf(a.out, "  database writes: archive %s\n", orNone(r.Archive))
			fmt.Fprintf(a.out, "  last published:  %s\n", orNone(r.Published))
			if r.Pending {
				fmt.Fprintln(a.out, "  about to publish: one manifest, which the next pass puts again")
			}
			if r.Stopped != "" {
				fmt.Fprintf(a.out, "  stopped:         %s\n", r.Stopped)
			}
			if r.Take != "" {
				fmt.Fprintf(a.out, "  to take:         %s, at the next boot\n", r.Take)
			}
			for _, l := range r.Listed {
				switch {
				case l.Deleted:
					fmt.Fprintf(a.out, "  in the store:    %s, a deletion mark\n", l.ID)
				default:
					fmt.Fprintf(a.out, "  in the store:    archive %s, epoch %d: %s\n", l.ID, l.Epoch, strings.Join(l.Manifests, ", "))
				}
			}
			if r.Restorable == "" {
				fmt.Fprintf(a.out, "  restorable:      none (%s)\n", r.NotRestorable)
				return nil
			}
			fmt.Fprintf(a.out, "  restorable:      %s\n", r.Restorable)
			fmt.Fprintf(a.out, "  history:         database head %d, archive head %d\n", r.DatabaseHead, r.ArchiveHead)
			if r.NotCompared != "" {
				fmt.Fprintf(a.out, "  lines:           not compared: %s\n", r.NotCompared)
			} else {
				fmt.Fprintf(a.out, "  lines:           %d of the seqs both hold differ", r.Differing)
				if r.FirstDiffering > 0 {
					fmt.Fprintf(a.out, ", the first at seq %d", r.FirstDiffering)
				}
				fmt.Fprintln(a.out)
			}
			fmt.Fprintf(a.out, "  only the database holds: %d line(s), %d sealed value(s), %d blob(s)\n", r.OnlyDatabase, len(r.SealedOnlyDatabase), r.BlobsOnlyDatabase)
			fmt.Fprintf(a.out, "  only the archive holds:  %d line(s), %d sealed value(s), %d blob(s)\n", r.OnlyArchive, len(r.SealedOnlyArchive), r.BlobsOnlyArchive)
			fmt.Fprintf(a.out, "  sealed values that differ: %d\n", len(r.SealedDiffering))
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output format: text|json")
	return cmd
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func (a *app) archiveResolveCommand() *cobra.Command {
	var keep, take bool
	cmd := &cobra.Command{
		Use:   "resolve <repository> --keep-database | --take-archive",
		Short: "Keep the database or the archive, whole",
		Long: `Keep the database or the archive, whole. Nothing is ever merged.

--keep-database starts a new archive: the next pass writes the database
whole into it, and the archive the database wrote before is left in the store
for you to remove ('archive remove') once the new one is published. Run it
after restoring the database from a backup, too.

--take-archive replaces the repository whole, at the next boot, by the
newest archive's newest manifest, which must be restorable now: every row
and every file the server holds of the repository goes, and the archive's
snapshot comes in, into a new archive. What the database held that the
archive lacks ('archive report') is lost.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if keep == take {
				return errors.New("choose one: --keep-database or --take-archive")
			}
			svc, err := a.openEngineExclusive(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			r, err := svc.ResolveArchive(cmd.Context(), args[0], keep)
			if err != nil {
				return lockHint(err)
			}
			if keep {
				fmt.Fprintf(a.out, "repository %s keeps its database: the next pass writes archive %s\n", r.Repository, r.Archive)
				if r.Left != "" {
					fmt.Fprintf(a.out, "  archive %s is left in the store; remove it with 'archive remove %s %s' once the new one is published\n", r.Left, r.Repository, r.Left)
				}
				return nil
			}
			fmt.Fprintf(a.out, "repository %s takes its archive: the next boot replaces it by %s\n", r.Repository, r.Take)
			return nil
		},
	}
	cmd.Flags().BoolVar(&keep, "keep-database", false, "keep the database, and start a new archive from it")
	cmd.Flags().BoolVar(&take, "take-archive", false, "replace the repository by its archive at the next boot")
	return cmd
}

func (a *app) archiveRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <repository> <archive id>",
		Short: "Remove an archive the database no longer writes",
		Long: `Remove one archive of a repository from the store: every object and
manifest under it. The archive the database writes is refused, and so is a
deletion mark, which stays. 'archive report' lists the archives.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineExclusive(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			n, err := svc.RemoveArchive(cmd.Context(), args[0], args[1])
			if err != nil {
				return lockHint(err)
			}
			fmt.Fprintf(a.out, "archive %s removed: %d object(s)\n", args[1], n)
			return nil
		},
	}
}

func (a *app) archiveImportCommand() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "import <repository id> <archive id>",
		Short: "Bring back a repository from one of its archives, past a deletion or under another name",
		Long: `Restore a repository the database does not hold from the newest manifest
of one archive you name, into a new archive. A boot restores a repository
with no rows on its own unless a deletion mark covers its newest archive;
this is the one way back past that mark.

--name restores it under another name than its manifest gives: the way in
for an archive the server set aside at start-up because another repository
here holds its name. Its own kinds are under ~, so no record changes; the
new archive's first manifest carries the new name. Stop the server first.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.openEngineExclusive(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = svc.Close() }()
			repo, err := svc.ImportArchive(cmd.Context(), args[0], args[1], name)
			if err != nil {
				return lockHint(err)
			}
			fmt.Fprintf(a.out, "repository %s (%s) imported from archive %s\n", repo.Authority, repo.ID, args[1])
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the name the repository takes in place of its manifest's")
	return cmd
}
