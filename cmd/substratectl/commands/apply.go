package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func (a *app) applyCommand() *cobra.Command {
	var files []string
	cmd := &cobra.Command{
		Use:   "apply -f FILE",
		Short: "Create or update records from manifests",
		Long: `Apply one or more YAML manifests (--- separated, "-" reads stdin).

Every document wears the envelope — kind, metadata, data:

  kind: tasks.substrate.reamde.dev/task             # the kind reference; bare for a
                                         # repository-local kind (kind: task)
  metadata:
    id: t9                               # the record id; omit to create
    labels:
      owner/pinned: true
  data:
    properties:
      name: Send rack layout to Alex
      dueAt: 2026-08-08T00:00:00Z
      detail: "rack layout"
    edges:
      - rel: source
        to:
          kind: calendar.substrate.reamde.dev/transcript
          id: f81k

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

An edge target is ` + "`{authority, type, id}`" + `; bare ` + "`{id}`" + ` is the
shorthand on a single-target edge. ` + "`metadata.ifVersion`" + ` refuses the
write unless the stored version is that one.

Schema documents apply too (schema is records): a document declared into
core with one of the nine schema kinds (authority, kind, trait,
propertytype, recordmapping, function, agent, actor, bundle)
rides the batch schema verb — the whole input's schema documents are one
transaction, every one admitted by the loader or none, active on commit.`,
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
			cl, err := a.client()
			if err != nil {
				return err
			}
			// Schema documents travel first, as ONE batch — every document
			// admitted or none — so the record documents behind them can use
			// the types they declare.
			if len(vocabularyDocs) > 0 {
				if err := a.applySchemaDocuments(cmd.Context(), cl, vocabularyDocs); err != nil {
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
	return cmd
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
// one of the nine core meta-kinds.
func isSchemaDocument(node *yaml.Node) bool {
	var probe struct {
		Kind string `yaml:"kind"`
	}
	if err := node.Decode(&probe); err != nil {
		return false
	}
	authority, name := vocabulary.SplitKindRef(probe.Kind)
	return authority == vocabulary.AuthorityCore && vocabulary.VocabularyDocumentKind(name)
}

// applySchemaDocuments sends one schema batch and prints what landed.
func (a *app) applySchemaDocuments(ctx context.Context, cl *client, docs []map[string]any) error {
	ents, err := cl.applyVocabulary(ctx, docs)
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
		prior, _, err = cl.get(ctx, col.Authority, col.Name, id)
		if err != nil {
			var ae *apiError
			if !errors.As(err, &ae) || ae.Status != 404 {
				return err
			}
			prior = nil
		}
	}
	e, err := cl.put(ctx, col.Authority, col.Name, id, in)
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
