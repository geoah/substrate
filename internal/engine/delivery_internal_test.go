package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// A ledger effect reads back from both stores. The table's jsonb spells a
// seq `18`; the segment file's canonical JSON spells it `1.8E1`, and a
// rebuild decodes the file, so an integer field that refused that spelling
// would refuse every delivery entry ever written.
func TestALedgerEffectDecodesFromTheFileSpelling(t *testing.T) {
	t.Parallel()
	for _, spelling := range []string{`18`, `1.8E1`, `18.0`} {
		raw := `{"fold":[{"kind":"cursor","ref":"` + typeTrigger + `","id":"on-x","seq":` + spelling + `},` +
			`{"kind":"park","ref":"` + typeTrigger + `","id":"on-x","failure":{"id":` + spelling +
			`,"seq":` + spelling + `,"attempts":3,"lastError":"x","parkedAt":"2026-09-08T10:00:00Z"}},` +
			`{"kind":"page","ref":"` + typeTrigger + `","id":"on-x","page":{"chain":"c","cursor":` + spelling +
			`,"version":` + spelling + `,"pages":2,"startedAt":"2026-09-08T10:00:00Z","kind":"record","identity":"7"}}]}`
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.UseNumber()
		var payload map[string]any
		if err := dec.Decode(&payload); err != nil {
			t.Fatal(err)
		}
		ch := substrate.Change{Seq: 1, Op: substrate.OpDelivery, Kind: typeTrigger, RecordID: "on-x", Payload: payload}
		ops, err := foldOpsOf(ch)
		if err != nil || len(ops) != 3 {
			t.Fatalf("spelling %s decoded as %+v (%v)", spelling, ops, err)
		}
		if ops[0].Seq == nil || *ops[0].Seq != 18 {
			t.Fatalf("spelling %s: cursor seq %v, want 18", spelling, ops[0].Seq)
		}
		if ops[1].Failure == nil || ops[1].Failure.ID != 18 || ops[1].Failure.Seq != 18 || ops[1].Failure.Attempts != 3 {
			t.Fatalf("spelling %s: failure %+v", spelling, ops[1].Failure)
		}
		if ops[2].Page == nil || ops[2].Page.Version != 18 || ops[2].Page.Pages != 2 || string(ops[2].Page.Cursor) != spelling {
			t.Fatalf("spelling %s: page %+v", spelling, ops[2].Page)
		}
	}
	// A fraction is not a seq: refused, never rounded.
	raw := `{"fold":[{"kind":"cursor","ref":"` + typeTrigger + `","id":"on-x","seq":1.5}]}`
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if _, err := foldOpsOf(substrate.Change{Seq: 1, Op: substrate.OpDelivery, Payload: payload}); err == nil {
		t.Fatal("a fractional seq decoded")
	}
}

// A cursor seq of 0 (a replay from the start) survives the round trip: the
// field is a pointer so omitempty cannot drop it.
func TestACursorResetToZeroIsCarried(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal([]foldOp{{Kind: foldCursor, Ref: typeTrigger, ID: "on-x", Seq: ptrTo(foldInt(0))}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"seq":0`) {
		t.Fatalf("the reset was dropped: %s", raw)
	}
	var back []foldOp
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back[0].Seq == nil || *back[0].Seq != 0 {
		t.Fatalf("the reset read back as %v", back[0].Seq)
	}
}

// Missed occurrences are listed oldest first and bounded: a schedule six
// hours behind an hourly rule owes six fires, and asks for them in order,
// four at a time.
func TestDueFiresAreOldestFirstAndBounded(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	src := &scheduleSource{Recurrence: "FREQ=HOURLY", Timezone: "UTC", StartsAt: &anchor}
	after := anchor.Add(30 * time.Minute)
	now := anchor.Add(6*time.Hour + 30*time.Minute)

	due, err := src.dueFires(anchor, after, now, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 4 {
		t.Fatalf("%d occurrences, want the bound of 4", len(due))
	}
	for i, at := range due {
		if want := anchor.Add(time.Duration(i+1) * time.Hour); !at.Equal(want) {
			t.Fatalf("occurrence %d is %s, want %s", i, at, want)
		}
	}
	due, err = src.dueFires(anchor, after, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 6 {
		t.Fatalf("%d occurrences, want the 6 that are overdue", len(due))
	}
	// Nothing overdue: an empty list, not the anchor.
	due, err = src.dueFires(anchor, now, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Fatalf("a schedule at now owes %v", due)
	}
}

// The parked copy of a request keeps the headers that describe the body and
// the ones a provider identifies and signs a delivery with, by exact name and
// nothing else: a name that only contains a known word is dropped.
func TestParkedHeadersKeepOnlyWhatAReplayNeeds(t *testing.T) {
	t.Parallel()
	for name, kept := range map[string]bool{
		"content-type": true, "Content-Length": true, "user-agent": true,
		"x-hub-signature-256": true, "stripe-signature": true, "X-GitHub-Event": true,
		"x-github-delivery": true, "x-slack-request-timestamp": true, "webhook-id": true,
		"authorization": false, "cookie": false, "x-api-key": false, "x-webhook-secret": false,
		"x-goog-channel-token": false, "x-auth-user": false, "x-test": false, "x-forwarded-for": false,
		"x-event-authorization": false, "x-signature-token": false, "x-delivery-cookie": false,
	} {
		if got := parkedHeaderKept(name); got != kept {
			t.Fatalf("header %q: kept %v, want %v", name, got, kept)
		}
	}
}
