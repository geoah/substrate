package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func (a *app) applyCommand() *cobra.Command {
	var files []string
	var as string
	var asMine, allowDataLoss bool
	cmd := &cobra.Command{
		Use:   "apply -f FILE",
		Short: "Create or update records from manifests",
		Long: `Apply one or more YAML manifests (--- separated, "-" reads stdin).

Every document wears the envelope — kind, metadata, data:

  kind: samples.substrate.reamde.dev/tasks/task             # the kind reference; a bare
                                         # kind: task resolves in the registry
  metadata:
    id: t9                               # the record id; omit to create
    labels:
      owner/pinned: true
  data:
    properties:
      name: Send rack layout to Alex
      dueAt: 2026-08-08T00:00:00Z
      detail: "rack layout"
      source: samples.substrate.reamde.dev/calendar/transcript/f81k

A qualified kind resolves outright; a bare one resolves against the kind
registry, and a name several authorities declare (every bundle installs a
` + "`config`" + `) has to be qualified. A document with ` + "`metadata.id`" + `
is PUT at that id; without one it is POSTed to the collection. The
` + "`status`" + ` block written by ` + "`substratectl get -o yaml`" + ` is ignored, so
get output is directly apply-able.

Everything authored is a property: ` + "`body`" + ` and the temporal
properties sit in ` + "`data.properties`" + ` beside the declared ones, and so
does a state's current value — which apply cannot move: a transition is
` + "`substratectl patch --state <name>=<state>`" + `. So does
` + "`title`" + `, on a kind that stores one; a kind declaring a
` + "`displayTemplate`" + ` renders its title instead (` + "`task`" + ` from
` + "`name`" + `, above) and drops a written one.

A pointer at another record is a property too: a ` + "`type: reference`" + `
property holds the target's ` + "`<kind>/<id>`" + ` path (` + "`source`" + `,
above), or a list of them where the declaration says ` + "`repeated`" + `.
` + "`metadata.ifVersion`" + ` refuses the write unless the stored version is
that one.

Schema documents apply too (schema is records): a document declared into
core with one of the nine schema kinds (authority, kind, trait,
propertytype, recordmapping, function, agent, actor, bundle)
rides the batch schema verb — the whole input's schema documents are one
transaction, every one admitted by the loader or none, active on commit.

--as <authority> REHOMES the input first: every mention of the one authority
the documents are authored under is rewritten to the one named, which is what
importing a shipped sample by hand takes (` + "`substratectl import`" + ` does
the same server-side). ` + "`--as-mine`" + ` uses the authority this context
logged in with. The input must be authored under a single authority, the
target must be one a repository may own, and core references are untouched.
When the input carries a package document, the request names the package it
was authored as (` + "`origin`" + `), and the server stamps the landed copy
with it as an import would: the catalog then previews the copy's upgrades,
and a later --as apply over a copy you edited since is refused until it is
confirmed, exactly like a re-import. One package per run: an input carrying
several package documents is refused, because one request names one origin.

A schema change that removes values from stored records (a dropped property
records still carry, an enum value renamed onto one the declaration keeps) is
refused until it is confirmed. --allow-data-loss previews the plan first
(` + "`POST /api/v1/vocabulary/plan`" + `), prints the steps that remove
values with the records each touches, and confirms exactly that plan: a write
that lands in between, or a plan that reads differently, is refused again.
The removed values stay in the changelog.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(files) == 0 {
				return errors.New("no input: pass -f FILE (or -f - for stdin)")
			}
			docs, vocabularyDocs, err := a.readDocuments(files)
			if err != nil {
				return err
			}
			if asMine {
				if as != "" {
					return errors.New("--as and --as-mine both name where to rehome: pass one")
				}
				// The authority the context already knows, so importing a
				// sample by hand does not make anybody retype their own name
				// and mistype it.
				if as, err = a.contextAuthority(); err != nil {
					return err
				}
			}
			// The origin a rehomed input claims (decision record 0070): the
			// package it was authored as, which the server stamps on the
			// landed copy exactly as `substratectl import` does. Empty when
			// nothing is rehomed or the input carries no package document.
			var origin string
			if as != "" {
				if origin, err = rehomeInput(docs, vocabularyDocs, as); err != nil {
					return err
				}
			}
			if len(docs) == 0 && len(vocabularyDocs) == 0 {
				return errors.New("no documents found in the input")
			}
			cl, err := a.client()
			if err != nil {
				return err
			}
			// Schema documents travel first, as ONE batch — every document
			// admitted or none — so the record documents behind them can use
			// the types they declare.
			if len(vocabularyDocs) > 0 {
				if err := a.applySchemaDocuments(cmd.Context(), cl, vocabularyDocs, allowDataLoss, origin); err != nil {
					return err
				}
			}
			for _, d := range docs {
				if err := a.applyDocument(cmd.Context(), cl, d); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&files, "filename", "f", nil, "YAML file to apply, or - for stdin (repeatable)")
	cmd.Flags().StringVar(&as, "as", "", "rehome the input under this authority before applying it")
	// A separate spelling rather than an optional value for `--as`: pflag
	// treats a string flag's empty NoOptDefVal as "a value is required", so
	// `--as` alone would be a usage error rather than a default.
	cmd.Flags().BoolVar(&asMine, "as-mine", false, "rehome the input under this repository's own authority")
	cmd.Flags().BoolVar(&allowDataLoss, "allow-data-loss", false, "preview the schema change and confirm the plan even where it removes values from stored records")
	return cmd
}

// contextAuthority is the repository the current context recorded at register
// or login, which IS its authority: what `--as-mine` rehomes onto. A context
// written before the field existed carries none, and `--as` then has to be
// spelled out.
func (a *app) contextAuthority() (string, error) {
	ctx, err := a.resolveContext()
	if err != nil {
		return "", err
	}
	if ctx.Repository == "" {
		return "", errors.New("this context records no repository: pass --as <authority>, or log in again to store it")
	}
	return ctx.Repository, nil
}

// rehomeInput rewrites every mention of the authority the input is authored
// under to `to`, in place: the client-side half of a sample import (decision
// record 0048). The walk is the server's own (vocabulary.RehomeAuthority) over
// the WHOLE document, labels and annotations included, so a file applied this
// way lands exactly what `substratectl import` would. It answers the ORIGIN
// the rehomed input claims (decision record 0070): the one package document's
// id as authored, or "" when the input carries none or several, since the
// stamp lands on one package row.
func rehomeInput(docs []*document, vocabularyDocs []map[string]any, to string) (string, error) {
	if err := rehomeTarget(to); err != nil {
		return "", err
	}
	from, err := authoredAuthority(vocabularyDocs)
	if err != nil {
		return "", err
	}
	if from == to {
		return "", nil
	}
	origin, err := authoredPackage(vocabularyDocs)
	if err != nil {
		return "", err
	}
	rehomedVocabulary, err := vocabulary.RehomeAuthority(vocabularyDocs, from, to)
	if err != nil {
		return "", err
	}
	copy(vocabularyDocs, rehomedVocabulary)
	for _, d := range docs {
		// The WHOLE record document goes through the walk, not three of its
		// fields: a label key, an annotation value and a property alike can
		// spell the authority, and one left behind is a document naming a
		// package this repository does not have.
		wrapped := map[string]any{
			"kind":        d.Kind,
			"id":          d.Metadata.ID,
			"labels":      d.Metadata.Labels,
			"annotations": d.Metadata.Annotations,
			"properties":  d.Data.Properties,
		}
		rehomedDoc, err := vocabulary.RehomeAuthority([]map[string]any{wrapped}, from, to)
		if err != nil {
			return "", err
		}
		out := rehomedDoc[0]
		d.Kind, _ = out["kind"].(string)
		d.Metadata.ID, _ = out["id"].(string)
		d.Metadata.Labels, _ = out["labels"].(map[string]any)
		d.Metadata.Annotations, _ = out["annotations"].(map[string]any)
		d.Data.Properties, _ = out["properties"].(map[string]any)
	}
	return origin, nil
}

// authoredPackage is the id of the ONE package document the input carries, as
// authored: the origin a rehomed copy claims. An input with no package
// document lands no package row to stamp and answers "". One with several is
// several copies, which the request's one `origin` cannot name, and letting
// it through would replace every one of them unstamped and unconfirmed
// (decision record 0070), so it is refused: one package per run.
func authoredPackage(vocabularyDocs []map[string]any) (string, error) {
	var found []string
	for _, d := range vocabularyDocs {
		if kind, _ := d["kind"].(string); kind != corePackage+"/"+vocabulary.DocPackage {
			continue
		}
		if id := mapString(d["metadata"], "id"); id != "" {
			found = append(found, id)
		}
	}
	switch len(found) {
	case 0:
		return "", nil
	case 1:
		return found[0], nil
	default:
		sort.Strings(found)
		return "", fmt.Errorf("--as rehomes one package per run, and the input carries %s: apply one package per run, so each copy is stamped with its origin and confirmed on its own", strings.Join(found, " and "))
	}
}

// rehomeTarget holds `--as` to an authority a REPOSITORY may own. The grammar
// is the one registration takes (vocabulary.ValidRepositoryAuthority), and the
// publisher's own name is refused outright: `substrate.reamde.dev` and
// everything under it is where the shipped vocabulary publishes, so a closure
// rehomed there reads as the substrate's own and the server refuses it anyway
// (engine authorizeNewPackage). Saying so here names the flag that did it.
func rehomeTarget(to string) error {
	if !vocabulary.ValidRepositoryAuthority(to) {
		return fmt.Errorf("--as %s is not an authority a repository may own: pass a DNS-style name (my.example.com)", to)
	}
	if to == publisherAuthority || strings.HasSuffix(to, "."+publisherAuthority) {
		return fmt.Errorf("--as %s is under %s, where the shipped vocabulary publishes: rehome onto your own authority instead",
			to, publisherAuthority)
	}
	return nil
}

// publisherAuthority is the name the shipped vocabulary publishes under. The
// server refuses a repository claiming it; this is the same rule said before
// the request leaves.
const publisherAuthority = "substrate.reamde.dev"

// authoredAuthority is the ONE authority the input's declarations are written
// under, which is what --as rewrites. Data records are not asked: a shipped
// trigger is
// a record of a CORE kind and names the closure's authority only inside its
// properties, so reading them would find core and refuse a legal input.
func authoredAuthority(vocabularyDocs []map[string]any) (string, error) {
	seen := map[string]bool{}
	var found []string
	for _, d := range vocabularyDocs {
		authority := mapString(d["data"], "authority")
		if authority == "" {
			authority, _, _ = vocabulary.SplitKindRef(mapString(d["metadata"], "id"))
		}
		if authority == "" || seen[authority] {
			continue
		}
		seen[authority] = true
		found = append(found, authority)
	}
	switch len(found) {
	case 0:
		return "", errors.New("--as needs a declaration to rehome: the input carries none naming an authority of its own")
	case 1:
		return found[0], nil
	default:
		sort.Strings(found)
		return "", fmt.Errorf("--as rehomes one authority, but the input declares under %s", strings.Join(found, " and "))
	}
}

func mapString(v any, key string) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// readDocuments parses the input streams, splitting the two planes: schema
// documents (the nine kinds declared into core) ride the batch apply verb as
// raw envelope maps; everything else is a record document.
func (a *app) readDocuments(files []string) ([]*document, []map[string]any, error) {
	var docs []*document
	var vocabularyDocs []map[string]any
	for _, name := range files {
		var r io.Reader
		if name == "-" {
			r = a.in
		} else {
			f, err := os.Open(name)
			if err != nil {
				return nil, nil, fmt.Errorf("open %s: %w", name, err)
			}
			defer f.Close()
			r = f
		}
		dec := yaml.NewDecoder(r)
		for i := 1; ; i++ {
			var node yaml.Node
			err := dec.Decode(&node)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, nil, fmt.Errorf("parse %s: %w", name, err)
			}
			body := unwrapNode(&node)
			if emptyNode(body) {
				continue
			}
			if isSchemaDocument(body) {
				var raw map[string]any
				if err := body.Decode(&raw); err != nil {
					return nil, nil, fmt.Errorf("parse %s: %w", documentRef(name, i), err)
				}
				vocabularyDocs = append(vocabularyDocs, raw)
				continue
			}
			d, err := nodeDocument(body, documentRef(name, i))
			if err != nil {
				return nil, nil, err
			}
			docs = append(docs, d)
		}
	}
	return docs, vocabularyDocs, nil
}

// isSchemaDocument recognizes a schema manifest by its envelope: a record of
// one of the core meta-kinds.
func isSchemaDocument(node *yaml.Node) bool {
	var probe struct {
		Kind string `yaml:"kind"`
	}
	if err := node.Decode(&probe); err != nil {
		return false
	}
	return vocabulary.KindPackage(probe.Kind) == vocabulary.PackageCore &&
		vocabulary.VocabularyDocumentKind(vocabulary.KindName(probe.Kind))
}

// applySchemaDocuments sends one schema batch and prints what landed. With
// allowDataLoss it previews the batch first and, where the plan is lossy or
// replaces a copy edited since its origin stamp (decision record 0070), prints
// what goes and confirms that plan and no other: the consent the server takes
// is the preview's hash and changelog head, so a bare "yes" is never sent
// (decision 0067). A plan that loses nothing needs no consent and is applied
// as it is. `origin` is the package a rehomed input was authored as, or "".
func (a *app) applySchemaDocuments(ctx context.Context, cl *client, docs []map[string]any, allowDataLoss bool, origin string) error {
	var confirm *substrate.ConversionConfirm
	if allowDataLoss {
		plan, err := cl.planVocabulary(ctx, docs, origin)
		if err != nil {
			return err
		}
		if plan.Lossy || plan.DiscardsEdits {
			fmt.Fprintf(a.out, "confirming plan %s at changelog seq %d, which %s:\n", plan.PlanHash, plan.ChangelogSeq,
				lossWords(&substrate.BundleUpgrade{ConversionPlan: plan.ConversionPlan, DiscardsEdits: plan.DiscardsEdits}))
			if plan.DiscardsEdits {
				fmt.Fprintf(a.out, "  replaces your copy of %s whole: the declarations edited since it was imported go with it\n", origin)
			}
			for _, s := range plan.Steps {
				if s.Lossy {
					fmt.Fprintf(a.out, "  %s\n", stepLine(s))
				}
			}
			confirm = &substrate.ConversionConfirm{PlanHash: plan.PlanHash, ChangelogSeq: plan.ChangelogSeq}
		}
	}
	ents, err := cl.applyVocabulary(ctx, docs, confirm, origin)
	if err != nil {
		return err
	}
	for _, e := range ents {
		fmt.Fprintf(a.out, "%s/%s applied\n", vocabulary.KindName(e.Kind), e.ID)
	}
	return nil
}

// documentRef names a document for error messages: "task.yaml document 2",
// and "stdin document 1" for the "-" stream.
func documentRef(file string, index int) string {
	if file == "-" {
		file = "stdin"
	}
	return fmt.Sprintf("%s document %d", file, index)
}

func (a *app) applyDocument(ctx context.Context, cl *client, d *document) error {
	in, err := d.putInput()
	if err != nil {
		return err
	}
	col, err := a.collectionForKind(ctx, d.Kind)
	if err != nil {
		return err
	}
	id := d.Metadata.ID
	var prior *substrate.Record
	if id != "" {
		in.ID = id
		prior, _, err = cl.get(ctx, col.pkg(), col.Name, id)
		if err != nil {
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != 404 {
				return err
			}
			prior = nil
		}
	}
	e, err := cl.put(ctx, col.pkg(), col.Name, id, in)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s %s\n", col.ref(e.ID), applyVerb(prior, e))
	return nil
}

// applyVerb names what the write did: unchanged when the returned version
// matches what was read (no-op suppression leaves the version still),
// created for a first write, updated otherwise.
func applyVerb(prior, e *substrate.Record) string {
	if prior != nil {
		if prior.Version == e.Version {
			return "unchanged"
		}
		return "updated"
	}
	if e.Version <= 1 {
		return "created"
	}
	return "updated"
}
