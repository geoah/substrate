package engine

// THE PASS'S BYTE BOUND (compactpass.go compactionBatchBytes): every batch
// of the pass reads at most the bound in stored bytes beyond the largest
// line it reads, or, for a step that takes the line passing it, beyond its
// two largest: a window's batch, a long record's step, a forgetting and a
// kept deletion alike. A trigger of large delivery lines is compacted in such
// steps, one line larger than the bound among them, over passes and in one;
// a purged record of large lines is forgotten, and a shipped one kept, the
// same way; a window splits its records into batches by their bytes; a
// record whose every line is over the bound is compacted a line a step; and
// a step that retains two large lines reads them with its new one.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// byteBound is the tests' byte bound: small, so a few dozen lines pass it.
const byteBound = 64 << 10

type byteRead struct {
	pass                   int
	ids                    []string
	lines                  int
	bytes, largest, second int64
}

// bytePass is a dataset whose pass is held to byteBound, recording what each
// batch read, and, where stepwise, stopped after every batch as a pass out
// of its budget is.
type bytePass struct {
	d     *dataset
	mu    sync.Mutex
	pass  int
	reads []byteRead
	clock time.Time
}

func newBytePass(t *testing.T, stepwise bool, opts ...Option) *bytePass {
	t.Helper()
	b := &bytePass{clock: time.Now().UTC().Add(2 * time.Hour)}
	base := []Option{
		WithCompactionInterval(-1),
		WithTestCompactionClock(func() time.Time { return b.clock }),
		WithTestCompactionBytes(byteBound, func(_ string, ids []string, lines int, bytes, largest, second int64) {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.reads = append(b.reads, byteRead{pass: b.pass, ids: slices.Clone(ids), lines: lines, bytes: bytes, largest: largest, second: second})
		}),
	}
	if stepwise {
		base = append(base, WithTestCompactionFault(func(stage string) error {
			if stage == compactionAfterBatch {
				return context.DeadlineExceeded
			}
			return nil
		}))
	}
	b.d = openInternalDataset(t, append(base, opts...)...)
	return b
}

func (b *bytePass) run(t *testing.T) {
	t.Helper()
	b.mu.Lock()
	b.pass++
	b.mu.Unlock()
	if _, err := b.d.CompactionPass(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// of is every read that took id.
func (b *bytePass) of(id string) []byteRead {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []byteRead
	for _, r := range b.reads {
		if slices.Contains(r.ids, id) {
			out = append(out, r)
		}
	}
	return out
}

// heldToTheTwoLargest fails for every read of id past the byte bound beyond
// its two largest lines, what a step whose first new line passes the bound
// may read beside what the record retains, and answers the reads that took
// id.
func (b *bytePass) heldToTheTwoLargest(t *testing.T, id string) []byteRead {
	t.Helper()
	reads := b.of(id)
	for _, r := range reads {
		if r.bytes-r.largest-r.second > byteBound {
			t.Errorf("pass %d read %d lines of %s, %d bytes, the two largest %d and %d: %d past them, over the bound %d",
				r.pass, r.lines, id, r.bytes, r.largest, r.second, r.bytes-r.largest-r.second, byteBound)
		}
	}
	return reads
}

// opaque is n characters no compression shrinks, so a line's stored size is
// about its text.
func opaque(t *testing.T, n int) string {
	t.Helper()
	raw := make([]byte, n*3/4+3)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)[:n]
}

// largeDeliveries writes one parked failure per size on trigger, each
// carrying an opaque payload of that size whole, as a parked drain carried
// its cursor (#839), and retires each in the next transaction: each size is
// one large delivery line and one small one.
func largeDeliveries(t *testing.T, d *dataset, trigger string, sizes ...int) {
	t.Helper()
	ctx := context.Background()
	for i, size := range sizes {
		payload, err := json.Marshal(map[string]any{"cursor": opaque(t, size)})
		if err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := d.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			seq, err := tx.reserveSeq()
			if err != nil {
				return err
			}
			id = seq
			if err := tx.parkTx(trigger, foldFailure{
				ID: foldInt(seq), FireID: fmt.Sprintf("large-%d", i), Attempts: 1, LastError: "parked",
				ParkedAt: time.Now().UTC(), Payload: payload,
			}); err != nil {
				return err
			}
			return tx.appendDeliveryAt(trigger, seq)
		}); err != nil {
			t.Fatalf("park a large delivery: %v", err)
		}
		if err := d.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			if err := tx.unparkTx(trigger, id); err != nil {
				return err
			}
			return tx.settleDelivery(trigger)
		}); err != nil {
			t.Fatalf("retire a large delivery: %v", err)
		}
	}
}

// sizeOf is what record ref's lines come to, up to the head.
func sizeOf(t *testing.T, d *dataset, ref eref) lineSize {
	t.Helper()
	sizes, err := d.recordLines(context.Background(), ref.Kind, []string{ref.ID}, 1<<62)
	if err != nil {
		t.Fatal(err)
	}
	return sizes[ref.ID]
}

// writtenLines counts record ref's lines carrying an effect other than a
// state or a ledger entry: lines as they were written.
func writtenLines(t *testing.T, d *dataset, ref eref) int {
	t.Helper()
	var n int
	if err := d.db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2
		  AND jsonb_path_exists(payload, '$.fold[*] ? (@.kind != "state" && @.kind != "ledger")')`, ref.Kind, ref.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A trigger whose delivery lines come to several times the byte bound, one
// of them three times it, and to far fewer than the line bound, compacted in
// steps to its state and its ledger entry, with the fold a replay gives
// before and after. Where each pass stops after one batch, every step is the
// first of its pass and reads at most the bound beyond its two largest
// lines; in one pass, every step after the first reads at most the bound
// beyond its largest.
func TestALongTriggerOfLargeLinesIsCompactedInStepsOfTheByteBound(t *testing.T) {
	t.Parallel()
	for _, stepwise := range []bool{true, false} {
		t.Run(map[bool]string{true: "a pass stopped after every batch", false: "one pass"}[stepwise], func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			b := newBytePass(t, stepwise, WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != typeTrigger, time.Hour }))
			installLedgerConnector(t, b.d, nowUTC().Add(-time.Hour).Truncate(time.Minute))
			sizes := make([]int, 0, 25)
			for i := range 24 {
				if i == 12 {
					sizes = append(sizes, 3*byteBound)
				}
				sizes = append(sizes, byteBound/4)
			}
			largeDeliveries(t, b.d, ledgerHourly, sizes...)
			if _, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "the head"}}); err != nil {
				t.Fatal(err)
			}
			ref := eref{Kind: typeTrigger, ID: ledgerHourly}
			before := sizeOf(t, b.d, ref)
			if before.over() < 4*byteBound || before.lines >= b.d.batchLines() || before.largest < 3*byteBound {
				t.Fatalf("the trigger's lines come to %+v, want four times the byte bound beyond a line three times it, within the line bound: the test proves nothing", before)
			}
			// A record trigger's scan position is the table's alone
			// (delivery.go): the fold to hold the pass to is the history's
			// replay.
			if _, err := b.d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
				t.Fatal(err)
			}
			want := FoldRows(t, b.d)
			passes := 0
			for ; passes < 40 && writtenLines(t, b.d, ref) > 0; passes++ {
				b.run(t)
			}
			if n, after := writtenLines(t, b.d, ref), sizeOf(t, b.d, ref); n != 0 || after.lines > 2 {
				t.Fatalf("after %d passes the trigger keeps %+v, %d lines as written, want its state and its ledger entry", passes, after, n)
			}
			reads := b.heldToTheTwoLargest(t, ledgerHourly)
			switch {
			case len(reads) < 5:
				t.Errorf("the trigger was compacted in %d steps, want steps of the byte bound", len(reads))
			case stepwise && passes < 2:
				t.Errorf("the trigger was compacted in %d passes, want steps over passes", passes)
			case !stepwise:
				for i, r := range reads[1:] {
					if r.bytes-r.largest > byteBound {
						t.Errorf("step %d of the pass read %d bytes, %d past its largest line, over the bound", i+2, r.bytes, r.bytes-r.largest)
					}
				}
			}
			if !slices.ContainsFunc(reads, func(r byteRead) bool { return r.largest >= 3*byteBound }) {
				t.Error("no step read the line three times the bound: it was never taken")
			}
			if diff := DiffFoldTables(want, FoldRows(t, b.d)); len(diff) > 0 {
				t.Errorf("the passes changed the fold:\n%s", strings.Join(diff, "\n"))
			}
			if _, err := b.d.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
				t.Fatal(err)
			}
			if diff := DiffFoldTables(want, FoldRows(t, b.d)); len(diff) > 0 {
				t.Errorf("the compacted history replays into another fold:\n%s", strings.Join(diff, "\n"))
			}
		})
	}
}

// A purged record whose lines come to several times the byte bound, past
// the deletion period: forgotten whole, or, where a package's hand created
// it, kept as its deletion's line, in steps none reading more than the bound
// beyond its two largest lines, over passes.
func TestALargeDeletionIsForgottenAndKeptInStepsOfTheByteBound(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		hand  substrate.Actor
		ended func(t *testing.T, d *dataset, ref eref) bool
	}{
		{"forgotten", substrate.ActorPerson, func(t *testing.T, d *dataset, ref eref) bool { return sizeOf(t, d, ref).lines == 0 }},
		{"kept", substrate.BundleActor("shipper.example.com", "shipper"), func(t *testing.T, d *dataset, ref eref) bool {
			done, err := d.keptForm(context.Background(), []eref{ref}, map[eref]bool{ref: true})
			if err != nil {
				t.Fatal(err)
			}
			return done[ref]
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			b := newBytePass(t, true, WithDeletionsKept(time.Hour), WithTestPurgeKeepsHistory(),
				WithTestCompactionRetention(func(string) (bool, time.Duration) { return true, 0 }))
			id := vocabulary.DerivedMintedID("large", t.Name())
			if err := b.d.inTx(ctx, c.hand, true, func(tx *txn) error {
				_, err := tx.put(substrate.PutInput{Kind: ledgerTask, ID: id, Properties: map[string]any{"name": opaque(t, byteBound/4)}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			for range 20 {
				if _, err := b.d.Patch(ctx, substrate.ActorPerson, ledgerTask, id, substrate.PatchInput{Properties: map[string]any{"name": opaque(t, byteBound/4)}}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := b.d.Delete(ctx, substrate.ActorPerson, ledgerTask, id, substrate.DeleteInput{Purge: true}); err != nil {
				t.Fatal(err)
			}
			if _, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "the head"}}); err != nil {
				t.Fatal(err)
			}
			ref := eref{Kind: ledgerTask, ID: id}
			if before := sizeOf(t, b.d, ref); before.over() < 4*byteBound || before.lines >= b.d.batchLines() {
				t.Fatalf("the record's lines come to %+v, want four times the byte bound within the line bound: the test proves nothing", before)
			}
			passes := 0
			for ; passes < 40 && !c.ended(t, b.d, ref); passes++ {
				b.run(t)
			}
			if !c.ended(t, b.d, ref) {
				t.Fatalf("after %d passes the record keeps %+v, want it %s", passes, sizeOf(t, b.d, ref), c.name)
			}
			if reads := len(b.heldToTheTwoLargest(t, id)); passes < 2 || reads < 5 {
				t.Errorf("the record was %s over %d passes in %d steps, want steps of the byte bound over passes", c.name, passes, reads)
			}
			if head, err := b.d.Head(ctx); err != nil || (c.name == "kept") != (head.Horizon == 0) {
				t.Errorf("the horizon is %d (%v) after the record was %s", head.Horizon, err, c.name)
			}
		})
	}
}

// A window over records that each come to under the byte bound batches them
// by their bytes: no batch reads more than the bound beyond its largest
// line, and none takes them all.
func TestAWindowBatchesItsRecordsByTheirBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBytePass(t, false, WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != ledgerTask, time.Hour }))
	var ids []string
	for range 6 {
		rec, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": opaque(t, byteBound/8)}})
		if err != nil {
			t.Fatal(err)
		}
		for range 3 {
			if _, err := b.d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": opaque(t, byteBound/8)}}); err != nil {
				t.Fatal(err)
			}
		}
		ids = append(ids, rec.ID)
	}
	if _, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "the head"}}); err != nil {
		t.Fatal(err)
	}
	b.run(t)
	batches := 0
	b.mu.Lock()
	for _, r := range b.reads {
		if !slices.ContainsFunc(r.ids, func(id string) bool { return slices.Contains(ids, id) }) {
			continue
		}
		batches++
		if r.bytes-r.largest > byteBound {
			t.Errorf("a batch of %d records read %d bytes, %d past its largest line, over the bound", len(r.ids), r.bytes, r.bytes-r.largest)
		}
		if len(r.ids) >= len(ids) {
			t.Errorf("a batch took all %d records, %d bytes: the byte bound split nothing", len(r.ids), r.bytes)
		}
	}
	b.mu.Unlock()
	if batches < 2 {
		t.Errorf("the window ran %d batches over the records, want them split by their bytes", batches)
	}
	for _, id := range ids {
		if n := writtenLines(t, b.d, eref{Kind: ledgerTask, ID: id}); n != 0 {
			t.Errorf("record %s keeps %d lines as written after the pass, want its state", id, n)
		}
	}
}

// A record whose every line is larger than the byte bound: a step reads the
// state the step before left, as large as the record's value, and takes one
// new line, so each step reads more than the bound beyond its largest line
// and no more than the bound beyond its two largest; the record is
// compacted one line a step, over passes, to its state, but for its first
// step, which takes its first line and the one that passes the bound.
func TestARecordWhoseEveryLineIsOverTheByteBoundIsCompactedALineAStep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBytePass(t, true, WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != ledgerTask, time.Hour }))
	rec, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": opaque(t, byteBound+byteBound/4)}})
	if err != nil {
		t.Fatal(err)
	}
	const patches = 6
	for range patches {
		if _, err := b.d.Patch(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.PatchInput{Properties: map[string]any{"name": opaque(t, byteBound+byteBound/4)}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "the head"}}); err != nil {
		t.Fatal(err)
	}
	ref := eref{Kind: ledgerTask, ID: rec.ID}
	var smallest int64 = 1 << 62
	rows, err := b.d.db.QueryContext(ctx, `SELECT pg_column_size(payload) FROM changelog WHERE kind = $1 AND record_id = $2`, ref.Kind, ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		smallest = min(smallest, n)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if smallest <= byteBound {
		t.Fatalf("the record's smallest line stores %d bytes, want every line over the bound %d: the test proves nothing", smallest, byteBound)
	}
	passes := 0
	for ; passes < 40 && writtenLines(t, b.d, ref) > 0; passes++ {
		b.run(t)
	}
	if n, after := writtenLines(t, b.d, ref), sizeOf(t, b.d, ref); n != 0 || after.lines != 1 {
		t.Fatalf("after %d passes the record keeps %+v, %d lines as written, want its state", passes, after, n)
	}
	reads := b.heldToTheTwoLargest(t, rec.ID)
	if len(reads) < patches {
		t.Errorf("the record of %d lines was compacted in %d steps, want one line a step after the first", patches+1, len(reads))
	}
	if !slices.ContainsFunc(reads, func(r byteRead) bool { return r.bytes-r.largest > byteBound }) {
		t.Error("no step read past the bound beyond its largest line: the state a step retains was never read with a new line")
	}
}

// A step retains two large lines, a parked webhook fire's, which stays, and
// the state the step before left, which carries that fire's payload, and its
// first new line is as large: it reads all three, more than the bound beyond
// its largest, and no more than the bound beyond its two largest. The
// trigger is compacted, one large line a step, down to what the fire holds:
// its line and every delivery line under it.
func TestAStepThatRetainsTwoLargeLinesReadsNoMoreThanTheBoundBeyondItsTwoLargest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBytePass(t, true, WithTestCompactionRetention(func(kind string) (bool, time.Duration) { return kind != typeTrigger, time.Hour }))
	installLedgerConnector(t, b.d, nowUTC().Add(-time.Hour).Truncate(time.Minute))
	large := byteBound * 7 / 8
	payload, err := json.Marshal(map[string]any{"request": opaque(t, large)})
	if err != nil {
		t.Fatal(err)
	}
	// The fire stays parked: its line holds every delivery effect under it,
	// and the trigger's ledger carries its payload.
	var fire int64
	if err := b.d.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		seq, err := tx.reserveSeq()
		if err != nil {
			return err
		}
		fire = seq
		if err := tx.parkTx(ledgerHourly, foldFailure{
			ID: foldInt(seq), FireID: "held", Attempts: 1, LastError: "parked", ParkedAt: time.Now().UTC(), Payload: payload,
		}); err != nil {
			return err
		}
		return tx.appendDeliveryAt(ledgerHourly, seq)
	}); err != nil {
		t.Fatal(err)
	}
	sizes := make([]int, 8)
	for i := range sizes {
		sizes[i] = large
	}
	largeDeliveries(t, b.d, ledgerHourly, sizes...)
	if _, err := b.d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "the head"}}); err != nil {
		t.Fatal(err)
	}
	ref := eref{Kind: typeTrigger, ID: ledgerHourly}
	above := func() int {
		t.Helper()
		var n int
		if err := b.d.db.QueryRowContext(ctx, `
			SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2 AND seq > $3
			  AND jsonb_path_exists(payload, '$.fold[*] ? (@.kind != "state" && @.kind != "ledger")')`, ref.Kind, ref.ID, fire).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := above(); n < len(sizes) {
		t.Fatalf("the trigger has %d lines as written above the parked fire's, want %d at least: the test proves nothing", n, len(sizes))
	}
	passes := 0
	for ; passes < 60 && above() > 0; passes++ {
		b.run(t)
	}
	if n := above(); n != 0 {
		t.Fatalf("after %d passes the trigger keeps %d lines as written above the parked fire's, want none", passes, n)
	}
	reads := b.heldToTheTwoLargest(t, ledgerHourly)
	if !slices.ContainsFunc(reads, func(r byteRead) bool { return r.bytes-r.largest > byteBound }) {
		t.Errorf("no step of %d read past the bound beyond its largest line: the test proves nothing", len(reads))
	}
	if got := failureIDs(t, b.d, ledgerHourly); len(got) != 1 {
		t.Errorf("the trigger's parked failures are %v after the passes, want the held fire alone", got)
	}
}
