package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// The watch contract, per repository: the cursor IS `seq`, resume from a
// remembered one is gapless, and the horizon is 0 so any cursor at all is
// resumable. `seq` is allocated under the repository's own advisory lock, so
// it survives the only thing that could break it — a process that goes away
// between the write and the resume.
func TestWatchResumesGaplesslyAcrossARestart(t *testing.T) {
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() (substrate.Service, substrate.Dataset) {
		svc, err := engine.Open(ctx, dsn,
			engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open engine: %v", err)
		}
		ds, err := svc.Dataset(ctx, "geoah")
		if err != nil {
			t.Fatalf("open dataset: %v", err)
		}
		return svc, ds
	}

	svc1, err := engine.Open(ctx, dsn,
		engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	if _, err := svc1.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds1, err := svc1.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	importVocabulary(t, ds1, "tasks")
	mustPut(t, ds1, owner, substrate.PutInput{
		Kind:       "tasks.substrate.geoah.me/task",
		Properties: map[string]any{"title": "before the restart"},
	})
	// What a consumer remembers: the last seq it saw.
	cursor := maxSeq(t, ds1)
	if cursor == 0 {
		t.Fatal("the changelog is empty before the restart")
	}
	if err := svc1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	svc2, ds2 := open()
	t.Cleanup(func() { _ = svc2.Close() })
	mustPut(t, ds2, owner, substrate.PutInput{
		Kind:       "tasks.substrate.geoah.me/task",
		Properties: map[string]any{"title": "after the restart"},
	})
	mustPut(t, ds2, owner, substrate.PutInput{
		Kind:       "tasks.substrate.geoah.me/task",
		Properties: map[string]any{"title": "and one more"},
	})

	rows := changesSince(t, ds2, cursor)
	if len(rows) < 2 {
		t.Fatalf("the resume returned %d changes, want the two writes past the cursor", len(rows))
	}
	// Gapless: the first row past the cursor is the very next seq, and the
	// sequence runs without a hole. A restart resets nothing — the counter is
	// the changelog's own max, per repository.
	if rows[0].Seq != cursor+1 {
		t.Fatalf("resume from %d starts at seq %d, want %d", cursor, rows[0].Seq, cursor+1)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].Seq != rows[i-1].Seq+1 {
			t.Fatalf("a gap across the restart: seq %d follows %d", rows[i].Seq, rows[i-1].Seq)
		}
	}
	// Nothing before the cursor comes back.
	for _, ch := range rows {
		if ch.Seq <= cursor {
			t.Fatalf("the resume redelivered seq %d, at or below the cursor %d", ch.Seq, cursor)
		}
	}
	// And the head keeps climbing from where the old process left it.
	if head := maxSeq(t, ds2); head <= cursor {
		t.Fatalf("the head went backwards across the restart: %d then %d", cursor, head)
	}
}
