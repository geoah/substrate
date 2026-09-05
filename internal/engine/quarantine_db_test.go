package engine_test

// Issue 010: a stored bundle closure that no longer admits under the current
// binary must NOT brick the whole repository. Repository-open quarantines the offending
// authority — leaves it out of the live registry, marks it on its authority row,
// and opens the repository with everything else — and a re-install of the bundle
// (which projects a valid closure) clears the quarantine.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// TestIncompatibleClosureQuarantinesInsteadOfBricking installs the mail bundle
// with a valid closure, then rewrites its stored bundle row to drop the
// inputs map — the oauth2 block's clientInput now names no declared input,
// which admission refuses. On the next open the repository must OPEN (not 401
// the world) with the mail bundle quarantined and everything else working;
// re-installing the valid closure clears the quarantine.
func TestIncompatibleClosureQuarantinesInsteadOfBricking(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}

	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, mbStandardDocs()); err != nil {
		t.Fatalf("install bundle: %v", err)
	}

	raw, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	// Strip the inputs map from the STORED bundle declaration: the oauth2
	// block's clientInput then names no declared input, so on the next open
	// admission refuses the closure — the prod landmine. A declaration row's
	// PROPERTIES are the declaration, so the corruption is a property away.
	if _, err := raw.ExecContext(ctx,
		`UPDATE records SET props = props - 'inputs' WHERE id = $1`, mbPackage); err != nil {
		t.Fatalf("corrupt stored closure: %v", err)
	}
	_ = svc.Close()

	// (a) The repository OPENS despite the incompatible stored closure.
	svc2 := open()
	ds2, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("a repository with one incompatible stored closure failed to open: %v", err)
	}
	ops2 := bundler(t, ds2)

	// The bad bundle is quarantined, its reason names the admission failure.
	st := bundleStatusFor(t, ops2, mbPackage)
	if !st.Quarantined {
		t.Fatalf("mail bundle should be quarantined: %+v", st)
	}
	if st.Installed || st.Enabled {
		t.Fatalf("a quarantined bundle is not installed/enabled: %+v", st)
	}
	if !strings.Contains(st.QuarantineReason, "oauth2") {
		t.Fatalf("quarantine reason should name the admission failure: %q", st.QuarantineReason)
	}

	// The quarantined authority's own types are NOT in the live registry — a write
	// to its config type refuses instead of silently succeeding.
	if _, err := ds2.Put(ctx, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()}); err == nil {
		t.Fatal("a quarantined bundle's type must not accept writes")
	}

	// The REST of the repository works: a core write succeeds.
	mustPut(t, ds2, owner, substrate.PutInput{Kind: "task", Properties: map[string]any{"name": "still alive"}})

	// (b) Re-installing the valid closure clears the quarantine.
	if _, err := applier(t, ds2).ApplyVocabularyDocuments(ctx, owner, mbStandardDocs()); err != nil {
		t.Fatalf("re-install valid closure: %v", err)
	}
	st = bundleStatusFor(t, ops2, mbPackage)
	if st.Quarantined {
		t.Fatalf("re-install must clear the quarantine: %+v", st)
	}
	if !st.Installed || !st.Enabled {
		t.Fatalf("re-installed bundle should be live: %+v", st)
	}
	// The config type accepts writes again.
	mustPut(t, ds2, owner, substrate.PutInput{Kind: mbConfigType, Properties: mbConfigProps()})

	// A subsequent open is clean: the closure admits, no quarantine remains.
	_ = svc2.Close()
	svc3 := open()
	t.Cleanup(func() { _ = svc3.Close() })
	ds3, err := svc3.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("reopen after re-install: %v", err)
	}
	st = bundleStatusFor(t, bundler(t, ds3), mbPackage)
	if st.Quarantined || !st.Enabled {
		t.Fatalf("a healthy closure must open live: %+v", st)
	}
}

// bundleStatusFor finds one package's status in the full listing, failing the
// test on the query error or a missing package. A bundle's status is keyed by
// its id, which IS the package identity.
func bundleStatusFor(t *testing.T, ops bundleOps, pkg string) substrate.BundleStatus {
	t.Helper()
	statuses, err := ops.BundleStatuses(context.Background())
	if err != nil {
		t.Fatalf("bundle statuses: %v", err)
	}
	for _, s := range statuses {
		if s.ID == pkg {
			return s
		}
	}
	t.Fatalf("bundle status for %s not found in %+v", pkg, statuses)
	return substrate.BundleStatus{}
}
