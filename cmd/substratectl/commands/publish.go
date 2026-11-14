package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The repository's publishing routes (decision record 0289), owner-only.
const (
	pathPublish = "/api/v1/packages/publish"
	pathRelease = "/api/v1/packages/release"
)

func (a *app) publishCommand() *cobra.Command {
	var check, allow, yes, release bool
	var file, document, closure, as, output string
	var version int64
	cmd := &cobra.Command{
		Use:   "publish <~/package> --as <authority> [--yes] | publish <authority/package> --release | publish <authority/package> --check --file closure.yaml --version N --closure https://example.com/closure.yaml",
		Short: "Publish one of this repository's own packages under an authority, write its next release, or check a release you host yourself",
		Long: `Publish one of this repository's own packages, in ~, under an authority
whose DNS you control. Another substrate can then install it.

  substratectl publish '~/recipes' --as ada.example.com
  substratectl publish '~/recipes' --as ada.example.com --yes

Without --yes it changes nothing and prints what the publish does: the name
the package takes, the kinds and records that move to it, the records
elsewhere whose references follow them, your other packages whose
declarations follow, what blocks it, and the TXT record to publish at
_substrate.<authority>, with whether DNS shows it yet. With --yes the server
publishes, once DNS shows the record and nothing blocks it, and writes the
first release. It is permanent: the package leaves ~ for good and keeps the
published name, and it cannot be unpublished. Quote ~/package, or the shell
expands the ~.

--release writes the next release of a package this repository publishes:
the package's declarations as they are now, at its next version. Substrates
that installed the package can take it as an update. A package with nothing
changed since its last release is refused, and so is one whose authority's
_substrate record no longer names this repository's document.

  substratectl publish ada.example.com/recipes --release

--check is the other door, for a package whose authority document you host
yourself: it checks an immutable release against the live document and prints
the version entry to add, uploading nothing. --check and --as are exclusive.

  substratectl publish example.com/notes --check --file notes-2.yaml --version 2 --closure https://example.com/packages/notes-2.yaml

To hand a record's pinned values back to sync, use substratectl release.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if release {
				if check || as != "" || yes {
					return errors.New("--release is a form of its own: it writes the next release of a package this repository publishes, with no --as, --yes or --check")
				}
				for _, name := range []string{"file", "document", "closure", "version", "allow-narrowing"} {
					if cmd.Flags().Changed(name) {
						return fmt.Errorf("--%s belongs to --check, not to --release", name)
					}
				}
				return a.publishRelease(cmd.Context(), args[0], output)
			}
			if check && as != "" {
				return errors.New("--check and --as are exclusive: --as publishes one of this repository's own packages (~/package) through its server, --check checks a release of a package whose authority document you host yourself")
			}
			if as != "" {
				for _, name := range []string{"file", "document", "closure", "version", "allow-narrowing"} {
					if cmd.Flags().Changed(name) {
						return fmt.Errorf("--%s belongs to --check, not to --as", name)
					}
				}
				return a.publishOwn(cmd.Context(), args[0], as, yes, output)
			}
			if yes || output != "" {
				return errors.New("--yes belongs to --as <authority>, which publishes one of this repository's own packages, and --output to --as or --release")
			}
			if !check {
				return errors.New("publish requires --as <authority> to publish one of this repository's own packages (~/package), or --check to check a release whose authority document you host yourself")
			}
			if file == "" || closure == "" || version < 1 {
				return errors.New("--file, --closure and a positive --version are required")
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			name, _ := vocabulary.SplitPackageRef(args[0])
			opts := authority.Options{}
			if document != "" {
				opts.LocalDocuments = map[string]string{name: document}
			}
			entry, err := checkPublication(cmd.Context(), authority.NewReader(opts), args[0], version, closure, raw, allow)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(a.out)
			enc.SetIndent("", "  ")
			return enc.Encode(entry)
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "the authority to publish one of this repository's own packages under: a domain whose DNS you control")
	cmd.Flags().BoolVar(&yes, "yes", false, "with --as: publish, rather than print what the publish would do")
	cmd.Flags().BoolVar(&release, "release", false, "write the next release of a package this repository publishes (<authority>/<package>)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "with --as or --release: output format, table|json|yaml")
	cmd.Flags().BoolVar(&check, "check", false, "validate without publishing")
	cmd.Flags().BoolVar(&allow, "allow-narrowing", false, "allow a removed or renamed property from the newest release; a removed or renamed kind is always refused")
	cmd.Flags().StringVarP(&file, "file", "f", "", "candidate closure file")
	cmd.Flags().StringVar(&document, "document", "", "authority document file; otherwise read the live document")
	cmd.Flags().StringVar(&closure, "closure", "", "URL or development path to list for this release")
	cmd.Flags().Int64Var(&version, "version", 0, "publisher's package version")
	return cmd
}

// publishOwn is the repository's door: the plan, or with yes the publish.
// The server proves the authority and holds the blockers either way, so the
// plan is printed as it answers and the publish is refused in its words.
func (a *app) publishOwn(ctx context.Context, arg, to string, yes bool, output string) error {
	if err := outputFormat(output); err != nil {
		return err
	}
	pkg := a.localPackage(arg)
	if owner, _ := vocabulary.SplitPackageRef(pkg); owner == "" {
		return fmt.Errorf("%q names no package: publish --as takes one of this repository's own, ~/<package>, quoted so the shell leaves the ~ alone", arg)
	}
	cl, err := a.client()
	if err != nil {
		return err
	}
	var plan substrate.PublishPlan
	in := substrate.PublishInput{Package: pkg, Authority: to, Confirm: yes}
	if err := cl.do(ctx, http.MethodPost, pathPublish, nil, in, &plan); err != nil {
		hint := "nothing was changed"
		if yes {
			hint = fmt.Sprintf("nothing was changed; run `substratectl publish '%s' --as %s` for the plan: what blocks it, and the TXT record to publish with whether DNS shows it yet", pkg, to)
		}
		return refused(err, "the publish was refused", hint)
	}
	return printAs(a, output, plan, func() error {
		if yes {
			return a.printPublished(plan)
		}
		return a.printPublishPlan(plan)
	})
}

// localPackage is a package argument to publish as the server answers it: an
// unquoted `~/recipes` arrives as `$HOME/recipes`, and the context's
// repository name is another spelling of `~` for a package not yet
// published.
func (a *app) localPackage(ref string) string {
	ref = unexpandedLocal(ref)
	if ctx, err := a.resolveContext(); err == nil && ctx.Repository != "" {
		ref = vocabulary.LocalizeRef(ref, ctx.Repository)
	}
	return ref
}

// unexpandedLocal is ref with a shell's expansion of an unquoted `~/<package>`
// to `$HOME/<package>` undone. A released package is named as written: the
// repository's own name may be the authority it is published under, so it is
// never folded to `~` here.
func unexpandedLocal(ref string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/" {
		if rest, ok := strings.CutPrefix(ref, home+"/"); ok && rest != "" && !strings.Contains(rest, "/") {
			return vocabulary.Local + "/" + rest
		}
	}
	return ref
}

func (a *app) printPublishPlan(p substrate.PublishPlan) error {
	if err := a.printPlanFacts(p); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "\nThis is permanent: %s leaves ~ for good and keeps the name %s.\nIt cannot be unpublished, or published again under another name.\n", p.Package, p.Published)
	if len(p.Blockers) > 0 {
		fmt.Fprintln(a.out, "\nblocked by:")
		for _, b := range p.Blockers {
			fmt.Fprintf(a.out, "  - %s\n", b)
		}
	}
	to, _ := vocabulary.SplitPackageRef(p.Published)
	if p.Record != "" {
		fmt.Fprintf(a.out, "\nTo prove %s is yours, publish this TXT record, replacing any _substrate record at that name:\n\n", to)
		tw := newTable(a.out)
		fmt.Fprintln(tw, "NAME\tTYPE\tVALUE")
		fmt.Fprintf(tw, "%s\tTXT\t%q\n", p.Name, p.Record)
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	if p.Proven {
		fmt.Fprintln(a.out, "\nDNS: the record is there")
	} else {
		fmt.Fprintf(a.out, "\nDNS: not found yet: %s\n", p.Reason)
	}
	retry := fmt.Sprintf("substratectl publish '%s' --as %s", p.Package, to)
	switch {
	case len(p.Blockers) > 0:
		fmt.Fprintf(a.out, "\nNothing was changed. Clear what blocks it, then run: %s\n", retry)
	case !p.Proven:
		fmt.Fprintf(a.out, "\nNothing was changed. Once DNS shows the record, publish with: %s --yes\n", retry)
	default:
		fmt.Fprintf(a.out, "\nNothing was changed. Publish with: %s --yes\n", retry)
	}
	return nil
}

// printPlanFacts is the plan's labeled half: what moves, and where to.
func (a *app) printPlanFacts(p substrate.PublishPlan) error {
	tw := newTable(a.out)
	fmt.Fprintf(tw, "package:\t%s\n", p.Package)
	fmt.Fprintf(tw, "published as:\t%s\n", p.Published)
	label := "kinds that move:"
	for _, k := range p.Kinds {
		fmt.Fprintf(tw, "%s\t%s -> %s\n", label, p.Package+"/"+vocabulary.KindName(k), k)
		label = ""
	}
	if len(p.Kinds) == 0 {
		fmt.Fprintf(tw, "%s\tnone\n", label)
	}
	fmt.Fprintf(tw, "records that move:\t%d\n", p.Records)
	fmt.Fprintf(tw, "references repointed:\t%d (records elsewhere that point at them)\n", p.References)
	label = "packages that follow:"
	for _, other := range p.Packages {
		fmt.Fprintf(tw, "%s\t%s\n", label, other)
		label = ""
	}
	if len(p.Packages) == 0 {
		fmt.Fprintf(tw, "%s\tnone\n", label)
	}
	return tw.Flush()
}

func (a *app) printPublished(p substrate.PublishPlan) error {
	fmt.Fprintf(a.out, "published %s as %s\n\n", p.Package, p.Published)
	if err := a.printPlanFacts(p); err != nil {
		return err
	}
	if p.Release != nil {
		fmt.Fprintln(a.out, "\nfirst release:")
		if err := printRelease(a.out, *p.Release); err != nil {
			return err
		}
	}
	fmt.Fprintf(a.out, "\nWrite the next release after changing its declarations: substratectl publish %s --release\n", p.Published)
	return nil
}

func checkPublication(ctx context.Context, reader *authority.Reader, id string, version int64, closure string, raw []byte, allow bool) (authority.AuthorityPackageVersion, error) {
	entry := authority.AuthorityPackageVersion{}
	if len(raw) > authority.MaxClosure {
		return entry, errors.New("closure exceeds 4 MB")
	}
	candidate, err := catalog.ParseClosure(raw, id, version)
	if err != nil {
		return entry, err
	}
	if renames := candidate.KindRenames(); len(renames) > 0 {
		return entry, fmt.Errorf("the candidate renames a kind, which a published package may not do: %s. "+
			"Keep the old kind declared, mark it `deprecated: true`, and declare the new one beside it without movedFrom", strings.Join(renames, "; "))
	}
	source, err := reader.Read(ctx, candidate.Authority, true)
	if err != nil {
		return entry, err
	}
	sum := sha256.Sum256(raw)
	// The dialect the closure needs rides the entry, the header's where it
	// states more, so a server older than any key of it passes over this
	// version before fetching it (decision 0268). A header that states less
	// than its keys need is refused by the parse above.
	entry = authority.AuthorityPackageVersion{Version: int(version), Closure: closure, SHA256: hex.EncodeToString(sum[:]), Dialect: int(max(candidate.Dialect(), candidate.NeedsDialect()))}
	// Parsing the updated document holds the proposed entry to the same shape
	// as the server, without fetching a file that has not been uploaded yet.
	probe := authority.AuthorityDocument{Authority: candidate.Authority, Packages: []authority.AuthorityPackage{{Package: candidate.Package, Versions: []authority.AuthorityPackageVersion{entry}}}}
	encoded, err := json.Marshal(probe)
	if err != nil {
		return entry, err
	}
	if _, err := authority.ParseAuthorityDocument(encoded, candidate.Authority); err != nil {
		return entry, err
	}
	var newest authority.AuthorityPackageVersion
	var published []authority.AuthorityPackageVersion
	for _, p := range source.Document.Packages {
		if p.Package != candidate.Package {
			continue
		}
		for _, v := range p.Versions {
			if int64(v.Version) == version && !strings.EqualFold(v.SHA256, entry.SHA256) {
				return entry, fmt.Errorf("version %d is already published with another digest", version)
			}
			if int64(v.Version) > version {
				return entry, fmt.Errorf("version %d is below published version %d", version, v.Version)
			}
			if v.Version > newest.Version {
				newest = v
			}
			if int64(v.Version) != version {
				published = append(published, v)
			}
		}
	}
	// Every kind any published version declared stays declared, whatever
	// --allow-narrowing says: repositories that took that version hold its
	// records under the kind's name (decision 0269).
	for _, v := range published {
		body, err := reader.Closure(ctx, source, v)
		if err != nil {
			return entry, fmt.Errorf("read published version %d to check the kinds it declared: %w", v.Version, err)
		}
		held, err := catalog.ParseClosure(body, id, int64(v.Version))
		if err != nil {
			return entry, fmt.Errorf("published version %d: %w", v.Version, err)
		}
		if removed := catalog.RemovedKinds(held, candidate); len(removed) > 0 {
			return entry, fmt.Errorf("the candidate removes %s, which version %d published, and a published package may not remove a kind. "+
				"Keep it declared and mark it `deprecated: true`", strings.Join(removed, ", "), v.Version)
		}
	}
	if newest.Version > 0 {
		body, err := reader.Closure(ctx, source, newest)
		if err != nil {
			return entry, err
		}
		held, err := catalog.ParseClosure(body, id, int64(newest.Version))
		if err != nil {
			return entry, err
		}
		narrows, _ := catalog.CompareClosures(held, candidate)
		if narrows && !allow {
			return entry, errors.New("the candidate removes or renames declarations; review it and use --allow-narrowing")
		}
	}
	return entry, nil
}
