package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// A database restored from a dump keeps the row and its generation, so the
// operator rotates by hand (decision 0056): the new generation is on the row,
// a restart reads it back, and a cursor saved under the old one no longer
// matches the head clients are held to.
func TestRotateHistoryGenerationHoldsTheHeadAndSurvivesARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "before the rotation"}})
	before, err := ds.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	root := engine.DataRootOf(svc)

	report, err := svc.(engine.GenerationRotator).RotateHistoryGeneration(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if report.Previous != before.Generation || report.Generation == "" || report.Generation == before.Generation {
		t.Fatalf("report = %+v, want a new generation replacing %q", report, before.Generation)
	}
	// The open dataset serves the new generation at once, at the same head:
	// a rotation rewrites no history.
	after, err := ds.Head(ctx)
	if err != nil || after.Generation != report.Generation || after.Seq != before.Seq {
		t.Fatalf("head after the rotation = %+v (%v), want %q at seq %d", after, err, report.Generation, before.Seq)
	}

	_ = svc.Close()
	svc2 := mustReopen(t, dsn, root)
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if restarted, err := ds2.Head(ctx); err != nil || restarted.Generation != report.Generation {
		t.Fatalf("head after a restart = %+v (%v), want the rotated generation %q", restarted, err, report.Generation)
	}
}

// A running server holds the directory lock and has the old generation
// cached, so a second process refuses to rotate under it, naming the lock.
func TestRotateHistoryGenerationRefusesWhileTheServerHoldsTheLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, dsn := newDatasetWithDSN(t)
	second, err := reopen(t, dsn, engine.DataRootOf(svc))
	if err != nil {
		t.Fatalf("a second process could not boot beside the server: %v", err)
	}
	_, err = second.(engine.GenerationRotator).RotateHistoryGeneration(ctx, testdb.Repository(t))
	if err == nil {
		t.Fatal("a rotation landed beside a running server")
	}
	if !errors.Is(err, engine.ErrChangelogLocked) || !errors.Is(err, changelogfile.ErrLocked) {
		t.Fatalf("the refusal must be the lock's: %v", err)
	}
}
