package engine_test

// Issue 010, one step EARLIER than admission: a stored closure that no longer
// PARSES under this binary must not brick the repository either. The concrete
// landmine this covers is the provider refactor — an agent definition written
// before providers became records still carries `llm: <tier>`, a key the
// loader now refuses, and the loader reports every problem in a whole source's
// document stream at once. Without per-authority rebuilding, one such row
// fails every installed authority and the repository cannot open at all.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	lqAuthority  = "legacy.bundles.substrate.reamde.dev"
	lqAgent      = lqAuthority + "/summarizer"
	lqConfigType = lqAuthority + "/legacyconfig"
	kindAgentID  = "core.substrate.reamde.dev/agent"
)

// lqDocs is a minimal bundle closure carrying one agent, so a test can age
// its stored definition into a shape the loader refuses.
func lqDocs() []map[string]any {
	return []map[string]any{
		vocabulary.AuthorityManifest(lqAuthority, 0),
		vocabulary.ActorManifest(lqAuthority, vocabulary.AuthorityActor(lqAuthority)),
		vocabulary.BundleManifest(lqAuthority, map[string]any{
			"description": "a bundle carrying one agent",
			"installs":    []any{lqConfigType, lqAgent},
		}),
		vocabulary.KindManifest(lqAuthority,
			map[string]any{"singular": "legacyconfig", "plural": "legacyconfigs"},
			map[string]any{
				"properties": map[string]any{"note": map[string]any{"type": "string"}},
			}),
		vocabulary.AgentManifest(lqAuthority, "summarizer", map[string]any{
			"description": "summarizes what it is handed",
			"prompt":      "You summarize.",
			"provider":    "default",
			"model":       "anthropic/claude-haiku-4-5",
		}),
	}
}

// TestUnparseableStoredAgentQuarantinesInsteadOfBricking installs two bundles,
// then rewrites ONE of them to the pre-refactor agent shape (`llm:`, no
// provider/model) straight in the store. On the next open the repository must
// OPEN with only that authority quarantined — its reason naming the deleted
// key and its replacement — while the sibling installed bundle and the shipped
// vocabulary stay live. Re-applying the corrected manifest clears the mark.
func TestUnparseableStoredAgentQuarantinesInsteadOfBricking(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}

	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	// The sibling: another INSTALLED authority, so the surviving half is not
	// just the builtin source.
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, mbStandardDocs()); err != nil {
		t.Fatalf("install the mail bundle: %v", err)
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, lqDocs()); err != nil {
		t.Fatalf("install the legacy bundle: %v", err)
	}

	raw, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	// Fabricate the pre-refactor declaration in the store: `llm: cheap` where
	// this binary wants provider + model. A declaration row's PROPERTIES are the
	// declaration, so the fabrication is three property moves.
	if _, err := raw.ExecContext(ctx,
		`UPDATE records SET props = (props - 'provider' - 'model') || '{"llm": "cheap"}'::jsonb
		 WHERE kind = $1 AND id = $2`, kindAgentID, lqAgent); err != nil {
		t.Fatalf("fabricate the pre-refactor agent declaration: %v", err)
	}
	_ = svc.Close()

	// (a) The repository OPENS despite the unparseable stored definition.
	svc2 := open()
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("a repository holding one pre-refactor agent definition failed to open: %v", err)
	}
	ops2 := bundler(t, ds2)

	st := bundleStatusFor(t, ops2, lqAuthority)
	if !st.Quarantined {
		t.Fatalf("the legacy bundle should be quarantined: %+v", st)
	}
	if st.Installed || st.Enabled {
		t.Fatalf("a quarantined bundle is not installed/enabled: %+v", st)
	}
	// The reason names what the stored declaration is MISSING, so the operator
	// reading a status page learns what to re-apply. Not the retired `llm` key
	// itself: a declaration row reads back through the keys the loader admits and
	// nothing else, so a property left over from another binary is not read at all
	// — which is the same rule that lets a NEWER binary's stamped property ride
	// through this one untouched.
	if !strings.Contains(st.QuarantineReason, "provider") {
		t.Fatalf("quarantine reason should name the missing provider: %q", st.QuarantineReason)
	}

	// The sibling INSTALLED bundle is untouched — the parse failure was
	// isolated to its own authority.
	sibling := bundleStatusFor(t, ops2, mbAuthority)
	if sibling.Quarantined || !sibling.Installed || !sibling.Enabled {
		t.Fatalf("the sibling bundle must stay live: %+v", sibling)
	}
	mustPut(t, ds2, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})
	// …and so is the shipped vocabulary.
	mustPut(t, ds2, owner, substrate.PutInput{Kind: "task", Properties: map[string]any{"name": "still alive"}})

	// (b) Re-applying the corrected manifest clears the quarantine — the same
	// path an admission-quarantined closure clears through.
	if _, err := applier(t, ds2).ApplyVocabularyDocuments(ctx, owner, lqDocs()); err != nil {
		t.Fatalf("re-apply the corrected manifest: %v", err)
	}
	st = bundleStatusFor(t, ops2, lqAuthority)
	if st.Quarantined {
		t.Fatalf("re-applying the corrected manifest must clear the quarantine: %+v", st)
	}
	if !st.Installed || !st.Enabled {
		t.Fatalf("the re-applied bundle should be live: %+v", st)
	}

	// A subsequent open is clean.
	_ = svc2.Close()
	svc3 := open()
	t.Cleanup(func() { _ = svc3.Close() })
	ds3, err := svc3.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("reopen after the correction: %v", err)
	}
	st = bundleStatusFor(t, bundler(t, ds3), lqAuthority)
	if st.Quarantined || !st.Enabled {
		t.Fatalf("a healthy closure must open live: %+v", st)
	}
}
