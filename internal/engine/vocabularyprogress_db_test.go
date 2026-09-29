package engine_test

// The apply door holds its transaction and the registry lock for as long as
// a batch takes, which on a large repository is minutes, so it logs each step
// at info as the step starts: the wait for the batch ahead, taking the lock,
// each package's declarations, each walk over stored records that has work,
// and the end. An operator tailing the log sees where the batch is (issue
// 720).

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const applyLog = `msg="substrate: vocabulary apply: `

func TestVocabularyApplyLogsEachStepAsItStarts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var logs lockedLog
	_, ds := newCoreDataset(t, engine.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	id := repositoryIDOf(t, ds)
	label := map[string]any{"label": map[string]any{"type": "string"}}
	const shop, depot = "progress.example.com/shop", "progress.example.com/depot"
	widget := func(props map[string]any) map[string]any {
		return vocabulary.KindManifest(shop, map[string]any{"singular": "widget"}, map[string]any{"properties": props})
	}
	logged := func(wantErr bool, docs ...map[string]any) []string {
		t.Helper()
		before := len(logs.String())
		if _, err := ds.ApplyVocabularyDocuments(ctx, owner, docs); (err != nil) != wantErr {
			t.Fatalf("apply: %v, want an error: %v", err, wantErr)
		}
		lines := linesWith(logs.String()[before:], applyLog)
		for _, line := range lines {
			if !strings.Contains(line, "level=INFO") || !strings.Contains(line+" ", "repository="+id+" ") {
				t.Errorf("an apply line is not info or does not name the repository: %s", line)
			}
		}
		return lines
	}
	applied := func(docs ...map[string]any) []string { t.Helper(); return logged(false, docs...) }

	// Two packages: the crate's declared index, the lock, one line per
	// package in package order, the search index of the three new kinds, the
	// end.
	lines := applied(
		vocabulary.PackageManifest(shop, 0),
		widget(label),
		vocabulary.KindManifest(shop, map[string]any{"singular": "gadget"}, map[string]any{"properties": label}),
		vocabulary.PackageManifest(depot, 0),
		vocabulary.KindManifest(depot, map[string]any{"singular": "crate"}, map[string]any{
			"properties": label,
			"indices":    []any{map[string]any{"properties": []any{"label"}}},
		}),
	)
	wantSteps(t, lines,
		[]string{applyLog + `building an index"`, "kind=" + depot + "/crate", "index=idx_"},
		[]string{applyLog + `holding the registry lock, checking the batch against the stored records"`, "documents=5", "packages=2"},
		[]string{applyLog + `writing the declarations of one package"`, "package=" + depot, "declarations=2", "index=1", "packages=2"},
		[]string{applyLog + `writing the declarations of one package"`, "package=" + shop, "declarations=3", "index=2", "packages=2"},
		[]string{applyLog + `re-deriving the search index"`, "kinds=3"},
		[]string{applyLog + `committed"`, "took="},
	)

	// A backfill walks the stored widgets after the declarations are
	// written, and says so before it starts, so the log does not go quiet
	// after the last package line; the new property moves the widget's
	// search index too.
	mustPut(t, ds, owner, substrate.PutInput{Kind: shop + "/widget", Properties: map[string]any{"label": "a"}})
	lines = applied(widget(map[string]any{
		"label": map[string]any{"type": "string"},
		"mood":  map[string]any{"type": "string", "required": true, "default": "neutral"},
	}))
	wantSteps(t, lines,
		[]string{applyLog + `holding the registry lock, checking the batch against the stored records"`, "documents=1", "packages=1"},
		[]string{applyLog + `writing the declarations of one package"`, "package=" + shop, "index=1", "packages=1"},
		[]string{applyLog + `rewriting records for the declared conversions"`},
		[]string{applyLog + `re-deriving the search index"`, "kinds=1"},
		[]string{applyLog + `committed"`, "took="},
	)

	// A batch the guards refuse inside the transaction (dropping a property
	// a live widget holds) says it ended with an error, and nothing more.
	lines = logged(true, widget(map[string]any{
		"mood": map[string]any{"type": "string", "required": true, "default": "neutral"},
	}))
	wantSteps(t, lines,
		[]string{applyLog + `holding the registry lock, checking the batch against the stored records"`, "documents=1", "packages=1"},
		[]string{applyLog + `ended with an error"`, "took="},
	)

	// One the loader refuses before any lock is taken ends the same way.
	lines = logged(true, widget(map[string]any{"label": map[string]any{"type": "nosuchtype"}}))
	wantSteps(t, lines, []string{applyLog + `ended with an error"`, "took="})

	// A batch behind another says it is waiting before it waits.
	release := engine.HoldVocabularyWrites(ds)
	before := len(logs.String())
	done := make(chan error, 1)
	go func() {
		_, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{widget(map[string]any{
			"label": map[string]any{"type": "string"},
			"mood":  map[string]any{"type": "string", "required": true, "default": "neutral"},
		})})
		done <- err
	}()
	waiting := applyLog + `waiting for the vocabulary batch ahead of this one"`
	for deadline := time.Now().Add(30 * time.Second); !strings.Contains(logs.String()[before:], waiting); {
		if time.Now().After(deadline) {
			release()
			t.Fatalf("a batch behind another logged no wait:\n%s", logs.String()[before:])
		}
		time.Sleep(10 * time.Millisecond)
	}
	release()
	if err := <-done; err != nil {
		t.Fatalf("the batch that waited: %v", err)
	}
	wantSteps(t, linesWith(logs.String()[before:], applyLog),
		[]string{waiting},
		[]string{applyLog + `holding the registry lock, checking the batch against the stored records"`, "documents=1", "packages=1"},
		[]string{applyLog + `writing the declarations of one package"`, "package=" + shop, "index=1", "packages=1"},
		[]string{applyLog + `committed"`, "took="},
	)
}

// wantSteps asserts the apply logged exactly these lines, in this order, each
// carrying every fragment given for it.
func wantSteps(t *testing.T, lines []string, want ...[]string) {
	t.Helper()
	if len(lines) != len(want) {
		t.Fatalf("%d apply lines, want %d:\n%s", len(lines), len(want), strings.Join(lines, "\n"))
	}
	for i, fragments := range want {
		for _, f := range fragments {
			if !strings.Contains(lines[i], f) {
				t.Errorf("apply line %d lacks %s: %s", i+1, f, lines[i])
			}
		}
	}
}
