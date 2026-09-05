// Package enginetest holds test-only install helpers shared by the engine's
// internal (package engine) and external (package engine_test) test suites.
//
// It exists because the v1 freeze removed the
// connector-registration shim — POST …/connectors, Dataset.RegisterConnector,
// and the substrate.ConnectorManifest/ConnectorTrigger wire types — and the
// retired connector/connectoraccount core kinds. Tests that used to install a
// authority (and hang sync-account fixtures off it) through that shim now go
// through the surviving install path, the schema-apply batch verb
// (ApplyVocabularyDocuments), with this package standing in for the old
// convenience:
//
//   - Install replays the old RegisterConnector shape (an authority's manifest
//     documents plus its default trigger records) over ApplyVocabularyDocuments.
//
//   - AccountType / AccountManifest give tests a stand-in for the removed
//     connectoraccount core kind: a plain installable account type they link
//     the shipped `account` reference (now unpinned) at.
//
//   - ImportVocabulary / SeededRegistry stand in for the creation seed's lost
//     half: repository creation seeds the CORE PACKAGE ALONE now, and the
//     shipped vocabulary (people, tasks, messaging, calendar) is a set of
//     sample packages a repository installs.
//
// It imports only substrate and schema, so both engine and engine_test may
// import it without a cycle.
package enginetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const typeTrigger = "substrate.reamde.dev/core/trigger"

// --- the shipped vocabulary, imported ------------------------------------------

// The two shipped trees, relative to the engine package: the provider packages
// under kinds/, the sample packages under samples/.
const (
	CatalogDir = "../../kinds/" + providerAuthority
	SamplesDir = "../../samples"
)

// The two shipped authorities a test installs from.
const (
	providerAuthority = "providers.substrate.reamde.dev"
	// SampleAuthority is the authority every shipped sample is authored under.
	SampleAuthority = "samples.substrate.reamde.dev"
)

// SamplePackage is a sample's package identity, from its bare name.
func SamplePackage(name string) string { return SampleAuthority + "/" + name }

// Vocabulary names the shipped SAMPLE packages a repository creation used to
// seed and no longer does (a fresh repository holds the core package alone).
// Order follows the requires: people and scheduling come before the packages
// that declare against them (messaging and calendar map onto people; tasks and
// calendar bind scheduling's traits).
var Vocabulary = []string{"people", "scheduling", "tasks", "messaging", "calendar"}

// ImportVocabulary installs shipped sample packages by their bare name
// ("people", "calendar") through the ONE install path — the schema-apply batch
// verb, under the bundle's own actor, exactly as a catalog install does. A test
// that reads or writes `samples.substrate.reamde.dev/people/person` calls this
// first, because the creation seed no longer writes that vocabulary into the
// repository.
func ImportVocabulary(ctx context.Context, ds substrate.Dataset, names ...string) error {
	if len(names) == 0 {
		names = Vocabulary
	}
	sa, ok := ds.(substrate.VocabularyApplier)
	if !ok {
		return errors.New("enginetest: dataset does not support ApplyVocabularyDocuments")
	}
	// A bundle's `requires:` is enforced on import, so a test naming only
	// "tasks" still needs people first. Requires are read from the bundle
	// document and imported ahead, the order the catalog install resolves.
	done := map[string]bool{}
	var importOne func(name string) error
	importOne = func(name string) error {
		if done[name] {
			return nil
		}
		done[name] = true
		docs, err := readBundleDir(filepath.Join(SamplesDir, name))
		if err != nil {
			return err
		}
		for _, req := range bundleRequires(docs) {
			// only a SAMPLE package is one this helper can install; a
			// provider requirement is the catalog's job.
			label, ok := strings.CutPrefix(req, SampleAuthority+"/")
			if !ok {
				continue
			}
			if err := importOne(label); err != nil {
				return err
			}
		}
		if _, err := sa.ApplyVocabularyDocuments(ctx, substrate.BundleActor(SampleAuthority, name), docs); err != nil {
			return fmt.Errorf("enginetest: import %s: %w", name, err)
		}
		return nil
	}
	for _, name := range names {
		if err := importOne(name); err != nil {
			return err
		}
	}
	return nil
}

// ShelfPackage is the fixture vocabulary InstallShelf brings: the shapes the
// engine suite exercises that no shipped vocabulary carries any more (the
// media bundle used to, until decision record 0015 held it out of the tree).
// `book` is a mapping target, so its id is server-assigned and pointers into it
// resolve canonically; `bookedition` carries the refinement-typed asin/isbn
// pair, an enum and the writer's own id; `description` is the one embeddable
// property.
const (
	ShelfAuthority = "shelf.test.dev"
	ShelfPackage   = ShelfAuthority + "/shelf"
)

// InstallShelf installs the shelf fixture vocabulary through the same batch
// apply an import rides, under its own bundle actor. It requires people:
// the author and narrator references land on person.
func InstallShelf(ctx context.Context, ds substrate.Dataset) error {
	sa, ok := ds.(substrate.VocabularyApplier)
	if !ok {
		return errors.New("enginetest: dataset does not support ApplyVocabularyDocuments")
	}
	docs := []map[string]any{
		vocabulary.PackageManifest(ShelfPackage, 1),
		{
			"kind":     "substrate.reamde.dev/core/propertytype",
			"metadata": map[string]any{"id": ShelfPackage + "/asin"},
			"data": map[string]any{
				"authority":   ShelfAuthority,
				"package":     "shelf",
				"description": "Amazon's audiobook identifier",
				"base":        "string",
				"pattern":     "^B0[A-Z0-9]{8}$",
			},
		},
		{
			"kind":     "substrate.reamde.dev/core/propertytype",
			"metadata": map[string]any{"id": ShelfPackage + "/isbn"},
			"data": map[string]any{
				"authority":   ShelfAuthority,
				"package":     "shelf",
				"description": "ISBN-10 or ISBN-13, normalized and hyphen-free",
				"base":        "string",
				"pattern":     "^(97[89])?[0-9]{9}[0-9X]$",
			},
		},
		vocabulary.KindManifest(ShelfPackage,
			map[string]any{"singular": "book", "plural": "books"},
			map[string]any{
				"properties": map[string]any{
					"subtitle":    map[string]any{"type": "string"},
					"description": map[string]any{"type": "markdown", "embed": true},
					"author": map[string]any{
						"type": "reference", "kind": "samples.substrate.reamde.dev/people/person",
						"repeated": true, "mustExist": true,
					},
				},
			}),
		vocabulary.KindManifest(ShelfPackage,
			map[string]any{"singular": "bookedition", "plural": "bookeditions"},
			map[string]any{
				"properties": map[string]any{
					"format":   map[string]any{"type": "enum", "values": []any{"print", "ebook", "audiobook"}},
					"language": map[string]any{"type": "string"},
					"asin":     map[string]any{"type": "asin"},
					"isbn":     map[string]any{"type": "isbn"},
					"mediaRef": map[string]any{"type": "url"},
					"duration": map[string]any{"type": "duration"},
					// The mapping's subject: single, required, and the referent
					// must exist, which is what a mapping source promises.
					"work": map[string]any{
						"type": "reference", "kind": ShelfPackage + "/book",
						"required": true, "mustExist": true, "subject": true,
					},
					"narrator": map[string]any{
						"type": "reference", "kind": "samples.substrate.reamde.dev/people/person",
						"mustExist": true,
					},
				},
			}),
		vocabulary.MappingManifest(ShelfPackage, "bookeditionwork", map[string]any{
			"from": ShelfPackage + "/bookedition", "to": ShelfPackage + "/book", "property": "work",
		}),
	}
	if _, err := sa.ApplyVocabularyDocuments(ctx, substrate.BundleActor(ShelfAuthority, "shelf"), docs); err != nil {
		return fmt.Errorf("enginetest: install the shelf fixture: %w", err)
	}
	return nil
}

// InstallBundle installs one shipped sample package that ships callables
// ("web", "firecrawl") through the ordinary batch apply, under the bundle's own
// actor — the closure a catalog install carries, minus the catalog. A test that
// needs function, agent and bundle declaration rows calls this.
func InstallBundle(ctx context.Context, ds substrate.Dataset, name string) error {
	sa, ok := ds.(substrate.VocabularyApplier)
	if !ok {
		return errors.New("enginetest: dataset does not support ApplyVocabularyDocuments")
	}
	all, err := readBundleDir(filepath.Join(SamplesDir, name))
	if err != nil {
		return err
	}
	// A bundle directory carries its DELIVERY WIRING too (trigger records), which
	// the batch apply does not take: the closure's declarations install here and
	// the wiring is the caller's to write if it wants it.
	var docs []map[string]any
	for _, d := range all {
		kind, _ := d["kind"].(string)
		if vocabulary.VocabularyDocumentKind(vocabulary.KindName(kind)) {
			docs = append(docs, d)
		}
	}
	if _, err := sa.ApplyVocabularyDocuments(ctx, substrate.BundleActor(SampleAuthority, name), docs); err != nil {
		return fmt.Errorf("enginetest: install %s: %w", name, err)
	}
	return nil
}

// bundleRequires reads the packages a bundle document declares against.
func bundleRequires(docs []map[string]any) []string {
	for _, d := range docs {
		if kind, _ := d["kind"].(string); kind != "substrate.reamde.dev/core/bundle" {
			continue
		}
		data, _ := d["data"].(map[string]any)
		rs, _ := data["requires"].([]any)
		out := make([]string, 0, len(rs))
		for _, r := range rs {
			out = append(out, fmt.Sprint(r))
		}
		return out
	}
	return nil
}

// SeededRegistry is the registry a repository holds right after creation plus
// the shipped sample packages it installed: the seeded tree (the core package
// alone) with the named vocabulary installed on top, exactly as the changelog
// would hold it. It is the registry half of ImportVocabulary, for the
// admission tests that never open a database.
func SeededRegistry(kindsDir string, names ...string) (*vocabulary.Registry, error) {
	reg, err := vocabulary.LoadDir(kindsDir)
	if err != nil {
		return nil, fmt.Errorf("enginetest: load the seeded tree: %w", err)
	}
	if len(names) == 0 {
		names = Vocabulary
	}
	var docs []vocabulary.Document
	for _, name := range names {
		raws, err := readBundleDir(filepath.Join(SamplesDir, name))
		if err != nil {
			return nil, err
		}
		for _, raw := range raws {
			d, err := vocabulary.DocumentFromMap(raw)
			if err != nil {
				return nil, fmt.Errorf("enginetest: %s: %w", name, err)
			}
			docs = append(docs, d)
		}
	}
	authorities, err := vocabulary.BuildPackages(docs, vocabulary.SourceInstalled)
	if err != nil {
		return nil, fmt.Errorf("enginetest: build the shipped vocabulary: %w", err)
	}
	if err := reg.InstallAll(authorities); err != nil {
		return nil, fmt.Errorf("enginetest: import the shipped vocabulary: %w", err)
	}
	return reg, nil
}

// readBundleDir decodes every manifest document in a shipped bundle directory.
func readBundleDir(dir string) ([]map[string]any, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("enginetest: %w", err)
	}
	var out []map[string]any
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("enginetest: %w", err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		for {
			var m map[string]any
			err := dec.Decode(&m)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("enginetest: %s: %w", e.Name(), err)
			}
			if len(m) > 0 {
				out = append(out, m)
			}
		}
	}
	return out, nil
}

// Manifest is the legacy connector-registration shape, kept for tests only:
// the authority's manifest documents plus its default trigger records.
type Manifest struct {
	Name      string
	Authority string
	Manifests []map[string]any
	Triggers  []Trigger
}

// Trigger is one default trigger record a Manifest installs (create-only).
type Trigger struct {
	ID         string
	Properties map[string]any
}

// Install installs an authority's schema documents through the batch verb, then
// writes its default triggers create-only — the RegisterConnector shim's
// behavior, minus the retired connector bookkeeping row. Re-installing is
// the upgrade verb (the authority is replaced whole); an existing trigger row is
// left exactly as it stands.
func Install(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, m Manifest) error {
	sa, ok := ds.(substrate.VocabularyApplier)
	if !ok {
		return errors.New("enginetest: dataset does not support ApplyVocabularyDocuments")
	}
	if len(m.Manifests) > 0 {
		if _, err := sa.ApplyVocabularyDocuments(ctx, actor, m.Manifests); err != nil {
			return err
		}
	}
	for _, tr := range m.Triggers {
		if _, err := ds.Get(ctx, typeTrigger, tr.ID); err == nil {
			continue // create-only: an existing trigger stands.
		} else if !errors.Is(err, substrate.ErrNotFound) {
			return err
		}
		if _, err := ds.Put(ctx, actor, substrate.PutInput{
			Kind: typeTrigger, ID: tr.ID, Properties: tr.Properties,
		}); err != nil {
			return err
		}
	}
	return nil
}

// AccountAuthority and AccountType name the test stand-in for the removed
// connectoraccount core kind: a plain installable account type. Tests that
// modeled a synced provider account as a connectoraccount install this type
// (InstallAccountType) and use AccountType in its place; the shipped `account`
// reference is unpinned now, so it points here fine.
const (
	AccountAuthority = "testacct.example.com"
	AccountPackage   = AccountAuthority + "/testacct"
	AccountType      = AccountPackage + "/account"
)

// AccountManifest is the installable account type (provider/label/status, the
// three properties connectoraccount carried). It binds `accountconfig`, the
// trait the shipped provider account kinds bind, so a shared kind's
// trait-pinned `account` reference (calendar, conversation, emailthread) may
// name it. The trait's own properties are redeclared, as the real account
// kinds redeclare them.
func AccountManifest() Manifest {
	return Manifest{
		Name:      "testacct",
		Authority: AccountAuthority,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(AccountPackage, 1),
			{
				"kind":     "substrate.reamde.dev/core/kind",
				"metadata": map[string]any{"id": AccountType},
				"data": map[string]any{
					"authority":       AccountAuthority,
					"package":         "testacct",
					"names":           map[string]any{"singular": "account", "plural": "accounts"},
					"displayTemplate": "{label}",
					"traits":          []any{"accountconfig"},
					"properties": map[string]any{
						"provider":      map[string]any{"type": "string"},
						"label":         map[string]any{"type": "string"},
						"status":        map[string]any{"type": "string"},
						"tokenRef":      map[string]any{"type": "secret"},
						"tokenStatus":   map[string]any{"type": "string"},
						"grantedScopes": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

// InstallAccountType installs AccountManifest into the dataset.
func InstallAccountType(ctx context.Context, ds substrate.Dataset, actor substrate.Actor) error {
	return Install(ctx, ds, actor, AccountManifest())
}
