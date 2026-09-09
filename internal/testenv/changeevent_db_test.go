package testenv_test

// The public change event over a real socket (decision 0061): every surface
// that serves a change row, the feed's history page and forward read, the
// collection watch and GraphQL's `changelog` and `history`, names the records
// the entry moved under `affected` and carries none of the stored replay
// effects. internal/api is tested against a fake that never held effects, so
// only the real engine behind the real handler can show they stay off.

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/testenv"
)

const (
	eventAuthority = "changeevent.example.substrate.reamde.dev"
	eventRef       = eventAuthority + "/feed"
	itemsPath      = "/api/v1/" + eventRef + "/item"
)

const eventVocabulary = `
kind: substrate.reamde.dev/core/package
metadata:
  id: ` + eventRef + `
data:
  authority: ` + eventAuthority + `
  package: feed
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ` + eventRef + `/item
data:
  authority: ` + eventAuthority + `
  package: feed
  names:
    singular: item
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: what the item is called
---
kind: substrate.reamde.dev/core/function
metadata:
  id: ` + eventRef + `/noop
data:
  authority: ` + eventAuthority + `
  package: feed
  description: delivers nothing, so a trigger can name it
  runtime: python
  permissions:
    writes: [` + eventRef + `/item]
  source: |
    def main(input, host):
        return {"effects": []}
`

// triggerPath is a trigger record the test writes: creating one appends a
// `delivery` entry, the engine's own ledger row (decision 0064), which no
// surface may serve.
const triggerPath = "/api/v1/substrate.reamde.dev/core/trigger/watch-items"

// eventRow is the part of a change row this test reads: the payload whole, so
// a leaked key is seen, and the event.
type eventRow struct {
	Seq      int64          `json:"seq"`
	Op       string         `json:"op"`
	RecordID string         `json:"recordId"`
	Payload  map[string]any `json:"payload"`
	Affected []struct {
		Kind    string `json:"kind"`
		ID      string `json:"id"`
		Version int64  `json:"version"`
		Deleted bool   `json:"deleted"`
	} `json:"affected"`
}

// wantEvent holds one row of one surface to the contract.
func wantEvent(t *testing.T, surface string, row eventRow, wantID string, wantVersion int64, wantDeleted bool) {
	t.Helper()
	if _, leaked := row.Payload["fold"]; leaked {
		t.Fatalf("%s: seq %d carries the stored replay effects: %v", surface, row.Seq, row.Payload)
	}
	if len(row.Affected) != 1 {
		t.Fatalf("%s: seq %d affected = %+v, want one record", surface, row.Seq, row.Affected)
	}
	a := row.Affected[0]
	if a.Kind != eventRef+"/item" || a.ID != wantID || a.Version != wantVersion || a.Deleted != wantDeleted {
		t.Fatalf("%s: seq %d affected = %+v, want %s at version %d (deleted=%v)", surface, row.Seq, a, wantID, wantVersion, wantDeleted)
	}
}

func TestEveryChangeSurfaceServesTheEventAndNoEffects(t *testing.T) {
	e := testenv.Start(t)
	e.ApplyVocabularyYAML(eventVocabulary)

	// A disabled trigger: its creation records its starting cursor as a
	// `delivery` entry and it never dispatches, so the ledger has one row
	// here and the item's history stays the three writes below.
	if status, body := e.Do(http.MethodPut, triggerPath, map[string]any{
		"properties": map[string]any{
			"enabled":  false,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{eventRef + "/item"}}},
			"callable": "substrate.reamde.dev/core/function/" + eventRef + "/noop",
		},
	}); status/100 != 2 {
		t.Fatalf("put trigger: %d %s", status, body)
	}

	// Three writes to one record: created at version 1, updated to 2,
	// deleted at 3. Each surface below reads the same three rows back.
	for _, name := range []string{"first", "second"} {
		status, body := e.Do(http.MethodPut, itemsPath+"/a", map[string]any{
			"properties": map[string]any{"name": name},
		})
		if status/100 != 2 {
			t.Fatalf("put: %d %s", status, body)
		}
	}
	if status, body := e.Do(http.MethodDelete, itemsPath+"/a", nil); status/100 != 2 {
		t.Fatalf("delete: %d %s", status, body)
	}
	want := []struct {
		op      string
		version int64
		deleted bool
	}{{"put", 1, false}, {"put", 2, false}, {"delete", 3, true}}

	// check holds a surface's rows, oldest first, to the three writes.
	check := func(surface string, rows []eventRow) {
		t.Helper()
		var mine []eventRow
		for _, r := range rows {
			if strings.HasSuffix(r.RecordID, "a") && len(r.Affected) > 0 && r.Affected[0].Kind == eventRef+"/item" {
				mine = append(mine, r)
			}
		}
		if len(mine) != len(want) {
			t.Fatalf("%s: %d rows of the item, want %d: %+v", surface, len(mine), len(want), mine)
		}
		for i, w := range want {
			if mine[i].Op != w.op {
				t.Fatalf("%s: row %d op = %s, want %s", surface, i, mine[i].Op, w.op)
			}
			wantEvent(t, surface, mine[i], "a", w.version, w.deleted)
		}
	}

	// The feed's history page, newest first.
	var page struct {
		Changes []eventRow `json:"changes"`
	}
	mustDecode(t, e, http.MethodGet, "/api/v1/changes?first=100&kinds="+eventRef+"/item", nil, &page)
	rows := page.Changes
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	check("history page", rows)

	// The feed's forward read is ndjson: a bookmark, then the rows.
	status, raw := e.Do(http.MethodGet, "/api/v1/changes?from=0&kinds="+eventRef+"/item", nil)
	if status != http.StatusOK {
		t.Fatalf("forward read: %d %s", status, raw)
	}
	check("forward read", ndjsonRows(t, strings.Split(strings.TrimSpace(string(raw)), "\n")))

	// The collection watch, drained from seq 0 and closed once the delete
	// arrives: the stream stays open past its last row by design.
	check("collection watch", watchRows(t, e, itemsPath+"?watch=1&from=0", 3))
	check("feed watch", watchRows(t, e, "/api/v1/changes?watch=1&from=0&kinds="+eventRef+"/item", 3))

	// GraphQL, both doors to a Change: the changelog walk and a record's own
	// history. Preview surface, same projection.
	var gql struct {
		Data struct {
			Changelog struct {
				Changes []eventRow `json:"changes"`
			} `json:"changelog"`
		} `json:"data"`
		Errors []map[string]any `json:"errors"`
	}
	const fields = `{ seq op recordId payload affected { kind id version deleted } }`
	mustDecode(t, e, http.MethodPost, "/api/v1/graphql", map[string]any{
		"query": `{ changelog(first: 100, filter: {kinds: ["` + eventRef + `/item"]}) { changes ` + fields + ` } }`,
	}, &gql)
	if len(gql.Errors) > 0 {
		t.Fatalf("graphql changelog: %v", gql.Errors)
	}
	check("graphql changelog", gql.Data.Changelog.Changes)

	var hist struct {
		Data struct {
			Record struct {
				History []eventRow `json:"history"`
			} `json:"record"`
		} `json:"data"`
		Errors []map[string]any `json:"errors"`
	}
	mustDecode(t, e, http.MethodPost, "/api/v1/graphql", map[string]any{
		"query": `{ record(kind: "` + eventRef + `/item", id: "a") { history(first: 100) ` + fields + ` } }`,
	}, &hist)
	if len(hist.Errors) > 0 {
		t.Fatalf("graphql history: %v", hist.Errors)
	}
	check("graphql history", hist.Data.Record.History)

	// Unfiltered, every surface serves the trigger's own write and never the
	// ledger entry beside it. This guards the HIDING: it cannot see the
	// engine's tables, so it does not prove the entry exists (the engine's
	// own tests do), and a binary that wrote no ledger would pass it too.
	noLedger := func(surface string, rows []eventRow) {
		t.Helper()
		sawTrigger := false
		for _, r := range rows {
			if r.Op == "delivery" {
				t.Fatalf("%s: seq %d is a delivery entry", surface, r.Seq)
			}
			if r.Op == "put" && r.RecordID == "watch-items" {
				sawTrigger = true
			}
		}
		if !sawTrigger {
			t.Fatalf("%s: the trigger's own write is missing, so the surface was not read whole", surface)
		}
	}
	var all struct {
		Changes []eventRow `json:"changes"`
	}
	mustDecode(t, e, http.MethodGet, "/api/v1/changes?first=1000", nil, &all)
	noLedger("history page", all.Changes)
	status, raw = e.Do(http.MethodGet, "/api/v1/changes?from=0", nil)
	if status != http.StatusOK {
		t.Fatalf("forward read: %d %s", status, raw)
	}
	noLedger("forward read", ndjsonRows(t, strings.Split(strings.TrimSpace(string(raw)), "\n")))
	mustDecode(t, e, http.MethodPost, "/api/v1/graphql", map[string]any{
		"query": `{ changelog(first: 1000) { changes ` + fields + ` } }`,
	}, &gql)
	if len(gql.Errors) > 0 {
		t.Fatalf("graphql changelog: %v", gql.Errors)
	}
	noLedger("graphql changelog", gql.Data.Changelog.Changes)
}

// mustDecode performs a request that has to succeed and decodes its body.
func mustDecode(t *testing.T, e *testenv.Env, method, path string, body, into any) {
	t.Helper()
	status, raw := e.Do(method, path, body)
	if status < 200 || status >= 300 {
		t.Fatalf("%s %s: %d %s", method, path, status, raw)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode %s %s: %v (%s)", method, path, err, raw)
	}
}

// ndjsonRows decodes the change rows of an ndjson body: a line without `seq`
// is a control frame and is skipped.
func ndjsonRows(t *testing.T, lines []string) []eventRow {
	t.Helper()
	var out []eventRow
	for _, line := range lines {
		if line == "" || !strings.Contains(line, `"seq"`) {
			continue
		}
		var row eventRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("undecodable row %q: %v", line, err)
		}
		out = append(out, row)
	}
	return out
}

// watchRows opens a watch stream and reads it until n change rows arrived,
// then closes it.
func watchRows(t *testing.T, e *testenv.Env, path string, n int) []eventRow {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open %s: %d", path, resp.StatusCode)
	}
	var lines []string
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for len(ndjsonRows(t, lines)) < n && sc.Scan() {
		lines = append(lines, sc.Text())
	}
	rows := ndjsonRows(t, lines)
	if len(rows) < n {
		t.Fatalf("%s delivered %d rows before the deadline, want %d: %v", path, len(rows), n, sc.Err())
	}
	return rows
}
