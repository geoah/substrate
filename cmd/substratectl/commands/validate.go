package commands

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
)

func (a *app) validateCommand() *cobra.Command {
	var files []string
	var partial bool
	cmd := &cobra.Command{
		Use:   "validate -f FILE",
		Short: "Check manifests offline with the server's own validator",
		Long: `Check the documents ` + "`apply -f`" + ` would send, without a server.

Declarations (the nine kinds declared into core) go through the loader the
server runs on ` + "`POST /api/v1/vocabulary/apply`" + `, built as packages
this repository owns, so a description over its limit, an unknown key or a
broken name is refused with the lines the server's 422 carries. A package
whose declarations pass is then resolved against the other packages among
the files and the core and llm packages this substratectl ships: reference
pins, trait bindings, agent tools, mappings.

Record documents get the checks apply makes before it sends them: the
envelope and its retired keys.

Some of it needs the repository, and validate says what it skipped:

  - a package whose package document is not among the files is checked
    under a placeholder header (version 1, no description): the header
    itself is not checked, and neither is a reference to a member of it
    the files leave out;
  - a declaration naming neither data.authority nor data.package is a change
    apply completes from the stored declaration, and is not checked;
  - a reference into a package that is neither among the files nor shipped
    is not resolved, since nothing here holds that package.

Any of those exits non-zero unless --partial accepts the partial check.
Files declaring into a seeded package (core, llm) are refused: validate
cannot check a change to one offline, and apply is refused one too. The
server alone checks declared defaults, references into the packages the
repository holds beyond these files, and each record's properties against
its kind.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(files) == 0 {
				return errors.New("no input: pass -f FILE (or -f - for stdin)")
			}
			docs, vocabularyDocs, err := a.readDocuments(files)
			if err != nil {
				return err
			}
			if len(docs) == 0 && len(vocabularyDocs) == 0 {
				return errors.New("no documents found in the input")
			}
			v, checkErr := validateDeclarations(vocabularyDocs)
			if len(vocabularyDocs) > 0 {
				fmt.Fprintf(a.out, "declarations: %s checked%s\n", countOf(v.checked, "document"), v.packageWords())
			}
			if len(docs) > 0 {
				fmt.Fprintf(a.out, "records: %s, envelope checked\n", countOf(len(docs), "document"))
			}
			if len(v.skipped) > 0 {
				fmt.Fprintf(a.out, "not checked offline:\n")
				for _, s := range v.skipped {
					fmt.Fprintf(a.out, "  - %s\n", s)
				}
			}
			fmt.Fprintf(a.out, "checked by the server only: declared defaults, references into packages beyond these files and the shipped core and llm, and record properties\n")
			if checkErr != nil {
				var ve *substrate.ValidationError
				if errors.As(checkErr, &ve) {
					// The server's 422 as the client renders it, so the
					// problem lines are the ones `apply` would print.
					return &apiError{
						Status:         422,
						Code:           "validation",
						Headline:       "the server would reject these declarations as invalid",
						Problems:       ve.Problems,
						ProblemDetails: ve.Details(),
					}
				}
				return checkErr
			}
			if len(v.skipped) > 0 && !partial {
				return fmt.Errorf("%s could not be checked offline (listed above): add what is missing to the files, or pass --partial to accept a partial check",
					countOf(len(v.skipped), "item"))
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&files, "filename", "f", nil, "YAML file to check, or - for stdin (repeatable)")
	cmd.Flags().BoolVar(&partial, "partial", false, "exit 0 when what could be checked passes, even though some of it needs the server")
	return cmd
}

// validation is what validateDeclarations checked and what it skipped.
type validation struct {
	// checked counts the declarations built: every one but the partial ones.
	checked int
	// packages are the packages the files declare into, sorted.
	packages []string
	// skipped names each thing the offline check could not decide.
	skipped []string
}

func (v validation) packageWords() string {
	if len(v.packages) == 0 {
		return ""
	}
	return fmt.Sprintf(" in %s (%s)", countOf(len(v.packages), "package"), strings.Join(v.packages, ", "))
}

// validateDeclarations runs the admission stages that need no repository,
// each through the engine's own function, so a refusal carries the lines the
// server's 422 carries for the same input.
func validateDeclarations(raw []map[string]any) (validation, error) {
	var v validation
	if len(raw) == 0 {
		return v, nil
	}
	partial := make([]bool, len(raw))
	partialIDs := map[string]string{}
	for i, r := range raw {
		short, id, ok := partialDeclaration(r)
		if !ok {
			v.checked++
			continue
		}
		partial[i] = true
		partialIDs[id] = short + " " + id
		v.skipped = append(v.skipped, fmt.Sprintf("%s %s: names neither data.authority nor data.package, so apply completes it from the stored declaration first", short, id))
	}
	// Every envelope parses, a partial one's too: completing it replaces its
	// data and keeps the rest.
	parsed, err := vocabulary.ParseDocuments(raw)
	if err != nil {
		return v, err
	}
	var docs []vocabulary.Document
	for i, d := range parsed {
		if !partial[i] {
			docs = append(docs, d)
		}
	}

	seed, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		return v, fmt.Errorf("load the shipped vocabulary: %w", err)
	}
	// Checking a seeded package would mean building it from the files' part
	// of it in place of the shipped closure, and the server refuses a token's
	// write into one anyway (engine authorizeDeclarationWrite).
	var seeded []string
	for _, d := range docs {
		if g := d.DeclaredPackage(); !slices.Contains(seeded, g) {
			if _, held := seed.PackageByName(g); held {
				seeded = append(seeded, g)
			}
		}
	}
	if len(seeded) > 0 {
		slices.Sort(seeded)
		return v, fmt.Errorf("the files declare into %s, seeded with the substrate: validate cannot check a change to a seeded package offline, and apply is refused one",
			strings.Join(seeded, " and "))
	}

	// A member is built only beside its package's header, so a package the
	// files carry no header for gets one that says nothing: its members are
	// checked and the header is not. A package whose identity breaks the
	// grammar gets none, because its members are refused for that by name.
	declared, headed, placeholder := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, d := range docs {
		g := d.DeclaredPackage()
		if d.Kind == vocabulary.DocPackage {
			headed[g] = true
		}
		if authority, name := vocabulary.SplitPackageRef(g); vocabulary.ValidAuthority(authority) && vocabulary.ValidPackage(name) {
			declared[g] = true
		}
	}
	v.packages = slices.Sorted(maps.Keys(declared))
	for _, g := range v.packages {
		if headed[g] {
			continue
		}
		header, err := vocabulary.DocumentFromMap(vocabulary.PackageManifest(g, 0))
		if err != nil {
			return v, err
		}
		docs = append(docs, header)
		placeholder[g] = true
		v.skipped = append(v.skipped, fmt.Sprintf("package %s: no package document among the files, so its members were checked under a placeholder header", g))
	}
	if len(docs) == 0 {
		return v, nil
	}
	built, err := vocabulary.BuildEachPackage(docs, func(string) string { return vocabulary.SourceInstalled })
	if err != nil {
		return v, err
	}

	// InstallAll reports every reference it cannot resolve. One into
	// something the files and the seed cannot hold is the check's gap, not
	// the author's mistake, so it is listed as skipped; the rest is refused.
	err = seed.InstallAll(built)
	var ve *substrate.ValidationError
	if !errors.As(err, &ve) {
		return v, err
	}
	gaps := map[string][]string{}
	var refused []string
	for _, p := range ve.Problems {
		id := missingIdentity(p)
		if id == "" {
			refused = append(refused, p)
			continue
		}
		g := identityPackage(id)
		_, shipped := seed.PackageByName(g)
		var gap string
		switch {
		case partialIDs[id] != "":
			gap = partialIDs[id] + ": apply completes it from the stored declaration"
		case g == "":
		case placeholder[g]:
			gap = "package " + g + ": its package document is not among the files"
		case !declared[g] && !shipped:
			gap = "package " + g + ": neither among the files nor shipped"
		}
		if gap == "" {
			refused = append(refused, p)
			continue
		}
		if !slices.Contains(gaps[gap], id) {
			gaps[gap] = append(gaps[gap], id)
		}
	}
	for _, gap := range slices.Sorted(maps.Keys(gaps)) {
		ids := gaps[gap]
		slices.Sort(ids)
		v.skipped = append(v.skipped, fmt.Sprintf("%s, so these references were not resolved: %s", gap, strings.Join(ids, ", ")))
	}
	if len(refused) > 0 {
		return v, &substrate.ValidationError{Problems: refused}
	}
	return v, nil
}

// The resolver's refusals of a reference the registry does not hold
// (vocabulary Registry.InstallAll): the identity is the quoted tail of an
// `unknown …` problem, or what `data.from` or `data.requires` names.
var (
	unknownQuoted = regexp.MustCompile(`: unknown (?:trait|referent kind|type|function|agent|kind) ("(?:[^"\\]|\\.)*")$`)
	unknownNamed  = regexp.MustCompile(`: data\.(?:from|requires) names (.+?), which this repository does not have`)
)

// missingIdentity is the identity an InstallAll problem says the registry
// does not hold, trimmed, or "" for any other problem.
func missingIdentity(problem string) string {
	if m := unknownQuoted.FindStringSubmatch(problem); m != nil {
		s, err := strconv.Unquote(m[1])
		if err != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}
	if m := unknownNamed.FindStringSubmatch(problem); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// identityPackage is the package an identity lives in: a `requires:` entry
// is a package itself. It answers "" for anything that is not a well-formed
// package or member identity, so a malformed reference stays refused.
func identityPackage(id string) string {
	g := id
	if authority, _ := vocabulary.SplitPackageRef(id); authority == "" {
		g = vocabulary.KindPackage(id)
	}
	authority, name := vocabulary.SplitPackageRef(g)
	if !vocabulary.ValidAuthority(authority) || !vocabulary.ValidPackage(name) {
		return ""
	}
	return g
}
