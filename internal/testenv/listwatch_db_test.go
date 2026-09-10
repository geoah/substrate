package testenv_test

// The list-to-watch handoff over a real socket. A client lists, opens a watch
// at the head the page reported, and must see every later change with neither
// a gap nor a double-see. internal/api pins the two halves against a fake
// (the page carries head and generation, the watch resumes from a cursor) and
// internal/engine pins the gapless head in Go, but only the real engine
// behind the real handler shows that the number one surface hands the other
// is the same number: an off-by-one there loses a write or replays one, and
// both look like an ordinary stream.

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/testenv"
)

func TestListHeadHandsOffToTheWatch(t *testing.T) {
	e := testenv.Start(t)
	e.ApplyVocabularyYAML(eventVocabulary)

	// Two writes before the list, so the head it reports is a real position
	// in a non-empty changelog rather than 0.
	for _, id := range []string{"before-1", "before-2"} {
		if status, body := e.Do(http.MethodPut, itemsPath+"/"+id,
			map[string]any{"properties": map[string]any{"name": id}}); status/100 != 2 {
			t.Fatalf("seed %s: %d %s", id, status, body)
		}
	}

	var page struct {
		Records []struct {
			ID string `json:"id"`
		} `json:"records"`
		Head       int64  `json:"head"`
		Generation string `json:"generation"`
	}
	mustDecode(t, e, http.MethodGet, itemsPath+"?first=1", nil, &page)
	if page.Head == 0 || page.Generation == "" {
		t.Fatalf("the list page carries head %d generation %q, want the handoff pair", page.Head, page.Generation)
	}

	// The watch opens at that head BEFORE the write it must deliver, so a row
	// it hands back is a live delivery and never a backfill.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	watch := e.Request(ctx, http.MethodGet, "/api/v1/changes?watch=1"+
		"&from="+strconv.FormatInt(page.Head, 10)+
		"&generation="+page.Generation+
		"&kinds="+eventRef+"/item", nil, nil)
	defer func() { _ = watch.Body.Close() }()
	if watch.StatusCode != http.StatusOK {
		t.Fatalf("open the watch at head %d: %d", page.Head, watch.StatusCode)
	}
	sc := bufio.NewScanner(watch.Body)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)

	// The stream opens with a bookmark, and it must be the head the list
	// reported: the two surfaces name one position, or the handoff is a guess.
	if !sc.Scan() {
		t.Fatalf("the watch closed before its bookmark: %v", sc.Err())
	}
	var opening struct {
		Bookmark   int64  `json:"bookmark"`
		Generation string `json:"generation"`
	}
	if err := json.Unmarshal(sc.Bytes(), &opening); err != nil {
		t.Fatalf("the opening frame is not a bookmark: %v (%s)", err, sc.Bytes())
	}
	if opening.Bookmark != page.Head || opening.Generation != page.Generation {
		t.Fatalf("the watch opened at %d/%q, want the list's %d/%q",
			opening.Bookmark, opening.Generation, page.Head, page.Generation)
	}

	const probe = "after-the-head"
	if status, body := e.Do(http.MethodPut, itemsPath+"/"+probe,
		map[string]any{"properties": map[string]any{"name": "written with the watch open"}}); status/100 != 2 {
		t.Fatalf("the probe write: %d %s", status, body)
	}

	// Exactly that change arrives, and nothing at or below the head: a row
	// the list already covered would be the double-see.
	for {
		if !sc.Scan() {
			t.Fatalf("the watch never delivered %s: %v", probe, sc.Err())
		}
		var row eventRow
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil || row.Seq == 0 {
			// A heartbeat or another framing line, not a change row.
			continue
		}
		if row.Seq <= page.Head {
			t.Fatalf("the watch delivered seq %d, at or below the list's head %d", row.Seq, page.Head)
		}
		if row.RecordID != probe {
			t.Fatalf("the watch delivered %q at seq %d before the probe: the handoff replayed a covered row",
				row.RecordID, row.Seq)
		}
		wantEvent(t, "list-to-watch handoff", row, probe, 1, false)
		return
	}
}
