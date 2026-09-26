package engine

// A TRIGGER'S READ IS BOUNDED BY ITS KINDS (#637). The dispatcher read every
// changelog entry past a record trigger's cursor and filtered by kind in Go,
// so a replay of a trigger over one small kind in a large repository walked
// the whole changelog, 200 entries a batch. The read now names the kinds the
// source can match, and a short batch moves the scan position to the head it
// read first: the entries of other kinds are never returned, and the cursor
// still reaches head.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const (
	readWidgetKind = "widgets.test.dev/widgets/widget"
	readTaskKind   = "samples.substrate.reamde.dev/tasks/task"
)

// seedReadBacklog writes `noise` tasks with `widgets` widgets spread among
// them, more noise than one dispatcher batch holds.
func seedReadBacklog(t *testing.T, ds *dataset, noise, widgets int) {
	t.Helper()
	ctx := context.Background()
	every := noise / widgets
	for i := range noise {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: readTaskKind, ID: fmt.Sprintf("noise-%d", i), Properties: map[string]any{"name": "noise"},
		}); err != nil {
			t.Fatalf("put task: %v", err)
		}
		if i%every == 0 && i/every < widgets {
			if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
				Kind: readWidgetKind, ID: fmt.Sprintf("w-%d", i), Properties: map[string]any{"name": "w"},
			}); err != nil {
				t.Fatalf("put widget: %v", err)
			}
		}
	}
}

func TestATriggerReadReturnsOnlyItsKinds(t *testing.T) {
	const (
		noise   = triggerBatch + 50
		widgets = 3
	)
	ctx := context.Background()
	ds := openCursorDataset(t)
	seedReadBacklog(t, ds, noise, widgets)
	head := maxSeqOf(t, ds)

	for _, pat := range []string{readWidgetKind, "widgets.test.dev/widgets/*", "widgets.test.dev/*"} {
		t.Run(pat, func(t *testing.T) {
			tr := &trigger{ID: "probe", Record: &recordSource{Kinds: []string{pat}}}
			changes, scanned, err := ds.changesPast(ctx, tr, 0)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if len(changes) != widgets {
				t.Fatalf("the read returned %d entries, want the %d widget entries alone", len(changes), widgets)
			}
			for _, ch := range changes {
				if ch.Kind != readWidgetKind {
					t.Fatalf("the read returned an entry of %s (seq %d), a kind the source cannot match", ch.Kind, ch.Seq)
				}
			}
			if scanned != head {
				t.Fatalf("the short batch covers through %d, want the head %d", scanned, head)
			}
		})
	}

	t.Run("a kind no entry carries", func(t *testing.T) {
		tr := &trigger{ID: "probe", Record: &recordSource{Kinds: []string{"nobody.test.dev/*"}}}
		changes, scanned, err := ds.changesPast(ctx, tr, 0)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(changes) != 0 || scanned != head {
			t.Fatalf("read %d entries covering through %d, want none covering through head %d", len(changes), scanned, head)
		}
	})

	// The plan behind the read: with the table analyzed, Postgres reads the
	// named kind's range of changelog_kind_seq_idx past the cursor and
	// filters no entry of another kind out of a wider scan. The statement
	// has the shape changesPast builds.
	t.Run("the plan reads the kind index", func(t *testing.T) {
		if _, err := ds.svc.admin.ExecContext(ctx, `ANALYZE changelog`); err != nil {
			t.Fatalf("analyze: %v", err)
		}
		rows, err := ds.db.QueryContext(ctx, `EXPLAIN (ANALYZE, COSTS OFF)
			SELECT seq, ts, actor, op, record_id, kind, payload, hash FROM changelog
			WHERE seq > $1 AND seq <= $2 AND kind = ANY($3::text[])
			ORDER BY seq LIMIT $4`, 0, head, []string{readWidgetKind}, triggerBatch)
		if err != nil {
			t.Fatalf("explain: %v", err)
		}
		defer func() { _ = rows.Close() }()
		var plan []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, line)
		}
		text := strings.Join(plan, "\n")
		if !strings.Contains(text, "changelog_kind_seq_idx") || strings.Contains(text, "Rows Removed by Filter") {
			t.Fatalf("the read does not walk the kind index alone:\n%s", text)
		}
	})

	t.Run("every kind", func(t *testing.T) {
		tr := &trigger{ID: "probe", Record: &recordSource{Kinds: []string{"*"}}}
		changes, scanned, err := ds.changesPast(ctx, tr, 0)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(changes) != triggerBatch {
			t.Fatalf("a `*` read returned %d entries, want a full batch of %d", len(changes), triggerBatch)
		}
		if last := changes[len(changes)-1].Seq; scanned != last {
			t.Fatalf("a full batch covers through %d, want its last entry %d", scanned, last)
		}
	})
}

// The replay the issue measured: the trigger rewound to zero over a backlog
// of other kinds delivers every widget and ends at head, and every read it
// made returned only widget entries.
func TestAReplayOverOneKindDeliversItAndEndsAtHead(t *testing.T) {
	const (
		noise   = triggerBatch + 50
		widgets = 3
	)
	ctx := context.Background()
	ds := openCursorDataset(t)
	const id = "on-mirror.widgets.test.dev/widgets"
	// Dispatch once so the cursor exists, then write the backlog and rewind.
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	seedReadBacklog(t, ds, noise, widgets)
	if err := ds.ReplayTrigger(ctx, id, 0); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	var mirrored int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM records WHERE kind = $1 AND id LIKE 't-%' AND deleted_at IS NULL`,
		readTaskKind).Scan(&mirrored); err != nil {
		t.Fatal(err)
	}
	if mirrored != widgets {
		t.Fatalf("the replay mirrored %d widgets, want %d", mirrored, widgets)
	}
	var cursor int64
	if err := ds.db.QueryRowContext(ctx, `SELECT seq FROM trigger_cursors WHERE trigger_id = $1`, id).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if head := maxSeqOf(t, ds); cursor != head {
		t.Fatalf("the cursor is %d after the replay drained, want head %d", cursor, head)
	}
}
