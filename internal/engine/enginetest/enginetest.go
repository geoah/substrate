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
//     the shipped `account` edge (now `to: any`) at.
//
//   - ImportVocabulary / SeededRegistry stand in for the creation seed's lost
//     half: repository creation seeds CORE ALONE now, and the shipped
//     vocabulary (people, tasks, messaging, calendar, media) is a set of
//     vocabulary bundles a repository imports.
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

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const typeTrigger = "core.substrate.reamde.dev/trigger"

// --- the shipped vocabulary, imported ------------------------------------------

// CatalogDir is where the shipped bundles live, relative to the engine package.
const CatalogDir = "../../kinds"

// Vocabulary names the shipped VOCABULARY bundles — the authorities repository
// creation used to seed and no longer does (a fresh repository holds core
// alone). Order matters only in that people comes first: messaging, calendar
// and media all declare against it.
var Vocabulary = []string{"people", "tasks", "messaging", "calendar", "media"}

// ImportVocabulary imports shipped vocabulary bundles by their bare label
// ("people", "media") through the ONE install path — the schema-apply batch
// verb, under the bundle's own actor, exactly as a catalog install does. A test
// that reads or writes `people.substrate.reamde.dev/person` calls this first, because the
// creation seed no longer writes that vocabulary into the repository.
func ImportVocabulary(ctx context.Context, ds substrate.Dataset, names ...string) error {
	if len(names) == 0 {
		names = Vocabulary
	}
	sa, ok := ds.(vocabularyApplier)
	if !ok {
		return errors.New("enginetest: dataset does not support ApplyVocabularyDocuments")
	}
	for _, name := range names {
		docs, err := readBundleDir(filepath.Join(CatalogDir, name+".substrate.reamde.dev"))
		if err != nil {
			return err
		}
		if _, err := sa.ApplyVocabularyDocuments(ctx, substrate.BundleActor(name), docs); err != nil {
			return fmt.Errorf("enginetest: import %s: %w", name, err)
		}
	}
	return nil
}

// SeededRegistry is the registry a repository holds right after creation plus
// the shipped vocabulary bundles it imported: the seeded tree (core alone) with
// the named vocabulary installed on top, exactly as the changelog would hold it. It
// is the registry half of ImportVocabulary, for the admission tests that never
// open a database.
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
		raws, err := readBundleDir(filepath.Join(CatalogDir, name+".substrate.reamde.dev"))
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
	authorities, err := vocabulary.BuildAuthorities(docs, vocabulary.SourceInstalled)
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

// vocabularyApplier is the bundle-tier install surface (a concrete engine
// method, not on the frozen substrate.Dataset interface — ruling A12). Both
// *engine.dataset and any test fake satisfy it.
type vocabularyApplier interface {
	ApplyVocabularyDocuments(ctx context.Context, actor substrate.Actor, raw []map[string]any) ([]*substrate.Record, error)
}

// Install installs an authority's schema documents through the batch verb, then
// writes its default triggers create-only — the RegisterConnector shim's
// behavior, minus the retired connector bookkeeping row. Re-installing is
// the upgrade verb (the authority is replaced whole); an existing trigger row is
// left exactly as it stands.
func Install(ctx context.Context, ds substrate.Dataset, actor substrate.Actor, m Manifest) error {
	sa, ok := ds.(vocabularyApplier)
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
// edge is now `to: any`, so it links here fine.
const (
	AccountAuthority = "testacct.example.com"
	AccountType      = "testacct.example.com/account"
)

// AccountManifest is the installable account type (provider/label/status, the
// three properties connectoraccount carried).
func AccountManifest() Manifest {
	return Manifest{
		Name:      "testacct",
		Authority: AccountAuthority,
		Manifests: []map[string]any{
			{
				"kind":     "core.substrate.reamde.dev/authority",
				"metadata": map[string]any{"id": AccountAuthority},
				"data":     map[string]any{"version": "v1"},
			},
			{
				"kind":     "core.substrate.reamde.dev/kind",
				"metadata": map[string]any{"id": AccountType},
				"data": map[string]any{
					"authority":       AccountAuthority,
					"names":           map[string]any{"singular": "account", "plural": "accounts"},
					"displayTemplate": "{label}",
					"properties": map[string]any{
						"provider": map[string]any{"type": "string"},
						"label":    map[string]any{"type": "string"},
						"status":   map[string]any{"type": "string"},
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
