package e2e

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The query, changelog and blob cases (CASES.md's Queries, The changelog and
// Blobs groups). They run over the repository the stories left behind and
// write only under the `x-` prefix, so nothing a story asserts on moves.

const (
	xqTaskKind = "tasks.substrate.reamde.dev/task"
	xqChanges  = "/api/v1/changes"
	// xqBlobPrefix is the wire's one digest spelling: the hash function is
	// named in the id, so a second algorithm would be a second prefix.
	xqBlobPrefix = "blob-sha256-"
	// xqDueExists narrows a task list to the rows that HAVE a dueAt. The
	// property is optional and the unset rows sort last, which says nothing
	// about the order under test.
	xqDueExists = `{"properties":{"dueAt":{"exists":true}}}`
)

func init() {
	registerCase(300, "QRY-01", "The list grammar: filter, orderBy, keyset page",
		"A properties filter returns only the rows that match it; `orderBy` orders by a hot property in both "+
			"the string and the JSON spelling, ascending and descending; and a first=2/after= keyset walk "+
			"reaches the same records in the same order as one unpaged read, with no row repeated and none lost.",
		xqCaseListGrammar)
	registerCase(310, "QRY-02", "filter.kinds is refused on a collection list",
		"The collection path already names the kind, so a filter that names one too is a 400 rather than a "+
			"silent overwrite: both the path's own kind and a foreign one are refused with the same message.",
		xqCaseCollectionKindsFilter)
	registerCase(320, "QRY-03", "An unsupported query parameter is named, not ignored",
		"An unknown parameter is a 400 quoting it on both the collection list and the changelog feed, and a "+
			"singular/plural slip (`kind` for `kinds`, `filters` for `filter`) is told the spelling that works.",
		xqCaseUnknownParams)
	registerCase(330, "QRY-04", "The list hands off to the watch",
		"A list page carries the changelog head it was read at; a watch opened from that head bookmarks the "+
			"same seq, delivers a write that lands afterwards, and delivers every row the forward read holds "+
			"between the two: no change is skipped and no row at or below the head is delivered twice.",
		xqCaseListWatchHandoff)
	registerCase(340, "LOG-02", "The backward page walks history to its end",
		"`before`/`first` pages the changelog newest-first: the seqs strictly decrease across the whole walk, "+
			"the cursor is absent on the last page alone, and the walk reads exactly the rows the forward read "+
			"holds, down to seq 1.",
		xqCaseBackwardPage)
	registerCase(350, "LOG-03", "The feed filters narrow, and the singular is refused",
		"`kinds` and `excludeKinds` partition the feed exactly; `actors` returns one actor's rows; `ops` takes "+
			"the changelog's own verbs (put, patch, delete), a repeated or comma-joined list of them, and an "+
			"unknown verb narrows to nothing; the singular `op` is a 400 naming `ops`.",
		xqCaseFeedFilters)
	registerCase(360, "LOG-04", "recordId with recordKind is one record's history",
		"The pair narrows the feed to exactly the rows the unfiltered read holds for that record, and either "+
			"half alone is a 400 saying why an id without a kind does not name one record.",
		xqCaseRecordHistory)
	registerCase(370, "LOG-05", "A collection watch delivers its own kind only",
		"A watch on the task collection ignores a person written before the task it delivers, and refuses the "+
			"list grammar (`filter`, `first`) instead of quietly ignoring parameters a stream cannot honor.",
		xqCaseCollectionWatch)
	registerCase(380, "LOG-06", "Heartbeats keep an idle stream open",
		"A watch with nothing to deliver stays open and writes a bare `{}` control frame on the 30s interval, "+
			"so an idle client (and any proxy between) sees liveness instead of a dead socket.",
		xqCaseHeartbeat)
	registerCase(390, "BLOB-01", "Bytes in, digest out, the same bytes back",
		"A raw PUT answers the sha-256 the bytes hash to, with the size, the stored status and a Location at "+
			"the digest; the GET streams the bytes back under an ETag of the digest; and the same bytes PUT "+
			"again dedup to the one blob.",
		xqCaseBlobRoundTrip)
	registerCase(395, "BLOB-02", "A wrong-digest PUT is refused",
		"PUT at an address the bytes do not hash to is a 400 `bad_request` naming the mismatch, and stores "+
			"nothing: the address it was offered still reads 404.",
		xqCaseBlobWrongDigest)
}

// xqPage is the list wire shape: the records, the keyset cursor ("" once the
// walk is exhausted) and the changelog head the page was read at.
type xqPage struct {
	Records []record `json:"records"`
	Cursor  string   `json:"cursor"`
	Head    int64    `json:"head"`
}

// xqError is the wire's problem shape, which every refusal below is pinned
// against by code as well as by message.
type xqError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// xqValues builds a query from name/value pairs, so a case reads as the URL
// it sends and no two reads share one mutable url.Values.
func xqValues(pairs ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return v
}

// xqGet is a quiet read: the paging walks call it so one walk records one
// step in the report instead of one per page.
func xqGet(c *C, path string, out any) (int, []byte) {
	c.t.Helper()
	status, raw, err := httpJSON(c.r.hc, c.r.base, c.r.token, http.MethodGet, path, nil)
	c.requiref(err == nil, "GET %s: %v", path, err)
	if out != nil && status < 300 {
		c.requiref(json.Unmarshal(raw, out) == nil, "GET %s: undecodable body %s", path, raw)
	}
	return status, raw
}

// xqRefused decodes a refusal and returns its code and message.
func xqRefused(c *C, raw []byte) xqError {
	c.t.Helper()
	var e xqError
	c.requiref(json.Unmarshal(raw, &e) == nil, "undecodable error body: %s", raw)
	c.requiref(e.Error.Code != "", "the refusal carries no error code: %s", raw)
	return e
}

// xqBadRequest asserts a read is refused 400 with a message holding want.
func xqBadRequest(c *C, path, want string) xqError {
	c.t.Helper()
	status, raw := c.do(http.MethodGet, path, nil, nil)
	c.requiref(status == http.StatusBadRequest, "GET %s answered %d, want 400: %s", path, status, raw)
	e := xqRefused(c, raw)
	c.requiref(e.Error.Code == "bad_request", "GET %s was refused as %q, want bad_request", path, e.Error.Code)
	c.requiref(strings.Contains(e.Error.Message, want),
		"GET %s was refused with %q, which does not say %q", path, e.Error.Message, want)
	return e
}

// xqListTasks reads one page of the task collection.
func xqListTasks(c *C, v url.Values) xqPage {
	c.t.Helper()
	path := tasksCollection
	if len(v) > 0 {
		path += "?" + v.Encode()
	}
	var page xqPage
	status, raw := c.do(http.MethodGet, path, nil, &page)
	c.requiref(status == http.StatusOK, "GET %s answered %d: %s", path, status, raw)
	return page
}

func xqIDs(recs []record) []string {
	ids := make([]string, 0, len(recs))
	for _, rec := range recs {
		ids = append(ids, rec.ID)
	}
	return ids
}

func xqSameOrder(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func xqIndexOf(ids []string, want string) int {
	for i, id := range ids {
		if id == want {
			return i
		}
	}
	return -1
}

func xqSeqSet(rows []changeRow) map[int64]bool {
	set := make(map[int64]bool, len(rows))
	for _, row := range rows {
		set[row.Seq] = true
	}
	return set
}

// xqReadFeed reads the forward feed to its end under one set of filter
// parameters. One body holds at most 500 rows, so the read pages by `from`
// exactly as readChangesForward does.
func xqReadFeed(c *C, v url.Values) []changeRow {
	c.t.Helper()
	var rows []changeRow
	from := int64(0)
	for {
		q := url.Values{}
		for name, vals := range v {
			q[name] = vals
		}
		q.Set("from", strconv.FormatInt(from, 10))
		path := xqChanges + "?" + q.Encode()
		status, raw := xqGet(c, path, nil)
		c.requiref(status == http.StatusOK, "GET %s answered %d: %s", path, status, raw)
		page := 0
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var row changeRow
			c.requiref(json.Unmarshal([]byte(line), &row) == nil, "undecodable ndjson line: %s", line)
			if row.Seq == 0 {
				c.requiref(!strings.Contains(line, `"error"`), "the feed ended with an error frame: %s", line)
				continue
			}
			rows = append(rows, row)
			page++
			from = row.Seq
		}
		if page == 0 {
			return rows
		}
	}
}

// xqStream is one open ndjson stream. The request carries the deadline, so a
// read that stops before the wanted line is the deadline expiring, never a
// clean end of the feed.
type xqStream struct {
	resp   *http.Response
	sc     *bufio.Scanner
	cancel context.CancelFunc
}

// xqOpenStream opens a watch and asserts it streams. The stream outlives any
// sane client timeout, so it gets its own client and its own deadline.
func xqOpenStream(c *C, path string, deadline time.Duration) *xqStream {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.r.base+path, nil)
	if err != nil {
		cancel()
		c.requiref(false, "building the watch request for %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.r.token)
	resp, err := (&http.Client{}).Do(req) //nolint:bodyclose // closed by the stream's close
	if err != nil {
		cancel()
		c.requiref(false, "opening the watch %s: %v", path, err)
	}
	st := &xqStream{resp: resp, sc: bufio.NewScanner(resp.Body), cancel: cancel}
	// A changelog row carries its payload, and a bundle's function source
	// makes one row longer than the scanner's default 64 KiB line.
	st.sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	c.stepf("`GET %s` answered %d and streams", path, resp.StatusCode)
	c.requiref(resp.StatusCode == http.StatusOK, "GET %s answered %d, want 200", path, resp.StatusCode)
	return st
}

func (s *xqStream) close() {
	_ = s.resp.Body.Close()
	s.cancel()
}

// line reads the next ndjson line; false means the deadline ended the read.
func (s *xqStream) line() (string, bool) {
	if !s.sc.Scan() {
		return "", false
	}
	return s.sc.Text(), true
}

// bookmark asserts the stream's opening control frame and returns the seq the
// watch resumed at.
func (s *xqStream) bookmark(c *C) int64 {
	c.t.Helper()
	line, ok := s.line()
	c.requiref(ok, "the watch sent no bookmark: %v", s.sc.Err())
	var frame struct {
		Bookmark *int64 `json:"bookmark"`
	}
	c.requiref(json.Unmarshal([]byte(line), &frame) == nil && frame.Bookmark != nil,
		"the stream's first line is not a bookmark: %s", line)
	return *frame.Bookmark
}

// row reads the next data row, skipping heartbeats and failing on the
// reserved terminal error frame. false means the deadline ended the read.
func (s *xqStream) row(c *C) (changeRow, bool) {
	c.t.Helper()
	for {
		line, ok := s.line()
		if !ok {
			return changeRow{}, false
		}
		var row changeRow
		c.requiref(json.Unmarshal([]byte(line), &row) == nil, "undecodable watch line: %s", line)
		if row.Seq == 0 {
			c.requiref(!strings.Contains(line, `"error"`), "the watch ended with an error frame: %s", line)
			continue
		}
		return row, true
	}
}

// xqCaseListGrammar walks the whole list query: the properties filter, both
// orderBy spellings in both directions, and a keyset walk held against one
// unpaged read of the same collection.
func xqCaseListGrammar(c *C) {
	// Three tasks the case owns, a day apart, so the order under test is
	// total among them whatever else the collection holds.
	due := [][2]string{
		{"x-qry-first", "2027-03-01T09:00:00Z"},
		{"x-qry-second", "2027-03-02T09:00:00Z"},
		{"x-qry-third", "2027-03-03T09:00:00Z"},
	}
	for _, d := range due {
		c.putRec(tasksCollection, d[0], map[string]any{"name": "QRY-01 " + d[0], "dueAt": d[1]})
	}
	var done record
	status, raw := c.do(http.MethodPatch, tasksCollection+"/x-qry-third",
		map[string]any{"properties": map[string]any{"status": "done"}}, &done)
	c.requiref(status == http.StatusOK && done.prop("status") == "done",
		"moving x-qry-third to done answered %d: %s", status, raw)
	c.stepf("wrote `x-qry-first`, `x-qry-second` and `x-qry-third` a day apart and moved the third to `done`")

	// The properties filter: every row matches it, and the row that left the
	// state is gone from the answer.
	open := xqListTasks(c, xqValues("filter", `{"properties":{"status":{"eq":"open"}}}`, "first", "200"))
	for _, rec := range open.Records {
		c.requiref(rec.prop("status") == "open",
			"the status filter returned `%s` with status %q", rec.ID, rec.prop("status"))
	}
	openIDs := xqIDs(open.Records)
	c.requiref(xqIndexOf(openIDs, "x-qry-first") >= 0 && xqIndexOf(openIDs, "x-qry-second") >= 0,
		"the status=open filter dropped a task that is open: %v", openIDs)
	c.requiref(xqIndexOf(openIDs, "x-qry-third") < 0,
		"the status=open filter returned `x-qry-third`, which is done")
	c.stepf("`filter={\"properties\":{\"status\":{\"eq\":\"open\"}}}` answered %d records, every one `open`, "+
		"with the done task absent", len(open.Records))

	// orderBy, in the two spellings the wire takes: "dueAt[:desc]" and the
	// JSON array of {property, desc}. Both directions are asserted on the
	// values themselves, so a tie between two rows proves nothing either way.
	asc := xqListTasks(c, xqValues("filter", xqDueExists, "orderBy", "dueAt", "first", "200"))
	desc := xqListTasks(c, xqValues("filter", xqDueExists, "orderBy", "dueAt:desc", "first", "200"))
	descJSON := xqListTasks(c, xqValues("filter", xqDueExists,
		"orderBy", `[{"property":"dueAt","desc":true}]`, "first", "200"))
	c.requiref(len(asc.Records) >= 3, "only %d tasks carry a dueAt; the case wrote three", len(asc.Records))
	for i := 1; i < len(asc.Records); i++ {
		c.requiref(asc.Records[i-1].prop("dueAt") <= asc.Records[i].prop("dueAt"),
			"orderBy=dueAt is not ascending: %q before %q", asc.Records[i-1].prop("dueAt"), asc.Records[i].prop("dueAt"))
	}
	for i := 1; i < len(desc.Records); i++ {
		c.requiref(desc.Records[i-1].prop("dueAt") >= desc.Records[i].prop("dueAt"),
			"orderBy=dueAt:desc is not descending: %q before %q", desc.Records[i-1].prop("dueAt"), desc.Records[i].prop("dueAt"))
	}
	c.requiref(xqSameOrder(xqIDs(descJSON.Records), xqIDs(desc.Records)),
		"the JSON orderBy answered a different order than `dueAt:desc`")
	ascIDs, descIDs := xqIDs(asc.Records), xqIDs(desc.Records)
	c.requiref(xqIndexOf(ascIDs, "x-qry-first") < xqIndexOf(ascIDs, "x-qry-second") &&
		xqIndexOf(ascIDs, "x-qry-second") < xqIndexOf(ascIDs, "x-qry-third"),
		"the case's three tasks are out of dueAt order ascending: %v", ascIDs)
	c.requiref(xqIndexOf(descIDs, "x-qry-third") < xqIndexOf(descIDs, "x-qry-second") &&
		xqIndexOf(descIDs, "x-qry-second") < xqIndexOf(descIDs, "x-qry-first"),
		"the case's three tasks are out of dueAt order descending: %v", descIDs)
	c.stepf("`orderBy=dueAt` ordered %d dated tasks ascending, `orderBy=dueAt:desc` and the JSON "+
		"`[{\"property\":\"dueAt\",\"desc\":true}]` both answered the same descending order", len(asc.Records))

	// The keyset page against one unpaged read of the same collection.
	ref := xqListTasks(c, xqValues("first", "200"))
	c.requiref(ref.Cursor == "", "the reference read came back with a cursor, so it is not the whole collection")
	var paged []string
	seen := map[string]bool{}
	after, pages := "", 0
	for {
		v := xqValues("first", "2")
		if after != "" {
			v.Set("after", after)
		}
		var page xqPage
		path := tasksCollection + "?" + v.Encode()
		status, raw := xqGet(c, path, &page)
		c.requiref(status == http.StatusOK, "GET %s answered %d: %s", path, status, raw)
		c.requiref(len(page.Records) <= 2, "a first=2 page answered %d records", len(page.Records))
		for _, id := range xqIDs(page.Records) {
			c.requiref(!seen[id], "the keyset walk delivered `%s` twice", id)
			seen[id] = true
			paged = append(paged, id)
		}
		pages++
		after = page.Cursor
		if after == "" {
			break
		}
		// The cap catches a cursor that never empties; it is derived from the
		// records there are, because a fixed one is a limit on the collection.
		c.requiref(pages < len(ref.Records)/2+3,
			"the keyset walk did not exhaust in %d pages over %d records", pages, len(ref.Records))
	}
	c.requiref(xqSameOrder(paged, xqIDs(ref.Records)),
		"the keyset walk read %d records in %d pages, the unpaged read %d, and they differ: %v vs %v",
		len(paged), pages, len(ref.Records), paged, xqIDs(ref.Records))
	c.stepf("a first=2 keyset walk took %d pages to reach the same %d records, in the same order, "+
		"with none repeated and none lost", pages, len(paged))
}

// xqCaseCollectionKindsFilter pins the collection list's one forced predicate.
func xqCaseCollectionKindsFilter(c *C) {
	const want = "filter.kinds is not supported on a collection list"
	for _, kind := range []string{xqTaskKind, personKind} {
		path := tasksCollection + "?" + xqValues("filter", fmt.Sprintf(`{"kinds":[%q]}`, kind)).Encode()
		// The path's own kind is refused as flatly as a foreign one: the
		// refusal is about the key, not about a conflict of values.
		e := xqBadRequest(c, path, want)
		c.stepf("`filter.kinds=[%s]` on the task collection was refused 400 `%s`: %q", kind, e.Error.Code, e.Error.Message)
	}
}

// xqCaseUnknownParams pins that an unsupported parameter is named rather than
// dropped, on both read modes. A dropped narrowing returns the WHOLE
// collection looking like a filtered one.
func xqCaseUnknownParams(c *C) {
	e := xqBadRequest(c, tasksCollection+"?bogus=1", `unknown query parameter "bogus"`)
	c.stepf("`?bogus=1` on the task collection: 400 `%s`, %q", e.Error.Code, e.Error.Message)
	e = xqBadRequest(c, xqChanges+"?bogus=1", `unknown query parameter "bogus"`)
	c.stepf("`?bogus=1` on the changelog feed: 400 `%s`, %q", e.Error.Code, e.Error.Message)

	// The near misses: the feed's filter keys are plural and the list's
	// filter document is singular, and each slip is told the other spelling.
	e = xqBadRequest(c, xqChanges+"?kind=x", `unknown query parameter "kind"`)
	c.requiref(strings.Contains(e.Error.Message, `did you mean "kinds"`),
		"the singular `kind` was refused without naming `kinds`: %q", e.Error.Message)
	c.stepf("`?kind=x` on the feed: 400 naming the plural, %q", e.Error.Message)
	e = xqBadRequest(c, tasksCollection+"?filters=1", `unknown query parameter "filters"`)
	c.requiref(strings.Contains(e.Error.Message, `did you mean "filter"`),
		"the plural `filters` was refused without naming `filter`: %q", e.Error.Message)
	c.stepf("`?filters=1` on the collection: 400 naming the singular, %q", e.Error.Message)
}

// xqCaseListWatchHandoff proves the seam between the two reads: a list page's
// head is exactly where a watch resumes, with no row seen twice and none lost
// in between.
func xqCaseListWatchHandoff(c *C) {
	page := xqListTasks(c, xqValues("first", "1"))
	head := page.Head
	c.requiref(head > 0, "the list page carries head %d, and the stories wrote hundreds of rows", head)
	c.stepf("a task list page answered head %d, the changelog seq it was read at", head)

	st := xqOpenStream(c, fmt.Sprintf("%s?watch=1&from=%d", xqChanges, head), 30*time.Second)
	defer st.close()
	c.requiref(st.bookmark(c) == head, "the watch bookmarked a different seq than the list's head %d", head)

	// The write lands with the stream already open, so what proves the handoff
	// is a live delivery and never a backfill of rows that predate the watch.
	const probe = "x-handoff"
	c.putRec(tasksCollection, probe, map[string]any{"name": "The list-to-watch handoff"})

	var delivered []changeRow
	var got changeRow
	for {
		row, ok := st.row(c)
		c.requiref(ok, "the watch never delivered the write of `%s`: %v", probe, st.sc.Err())
		c.requiref(row.Seq > head, "the watch re-delivered seq %d, at or below the list's head %d", row.Seq, head)
		delivered = append(delivered, row)
		if row.RecordID == probe && row.Kind == xqTaskKind {
			got = row
			break
		}
	}

	// The forward read is the truth the watch is held against, in both
	// directions: nothing delivered is absent from it, and nothing it holds
	// up to the probe was skipped by the stream.
	forward := c.readChangesForward(head)
	inForward := xqSeqSet(forward)
	for _, row := range delivered {
		c.requiref(inForward[row.Seq],
			"the watch delivered seq %d, which a forward read from %d does not hold", row.Seq, head)
	}
	watched := xqSeqSet(delivered)
	for _, row := range forward {
		if row.Seq > got.Seq {
			continue
		}
		c.requiref(watched[row.Seq],
			"the forward read holds seq %d in (%d, %d], which the watch never delivered: the handoff skipped a change",
			row.Seq, head, got.Seq)
	}
	c.stepf("the watch opened at head %d delivered `%s` at seq %d and the %d rows in between, exactly the "+
		"rows a forward read from %d holds: nothing skipped, nothing repeated", head, probe, got.Seq, len(delivered)-1, head)
}

// xqCaseBackwardPage walks the changelog newest-first to its bottom.
func xqCaseBackwardPage(c *C) {
	forward := c.readChangesForward(0)
	c.requiref(len(forward) > 0, "the changelog is empty")
	head := forward[len(forward)-1].Seq

	// `before` is exclusive, so the walk starts one above the head to take
	// the head row itself; rows committed during the walk sit above it and
	// belong to neither read.
	before := head + 1
	var seqs []int64
	pages := 0
	cursored := true
	// The cap catches a cursor that never empties, so it is derived from the
	// rows there are (a full last page still costs one empty page after it)
	// rather than fixed, which a growing changelog would walk into.
	maxPages := len(forward)/50 + 3
	for {
		path := fmt.Sprintf("%s?before=%d&first=50", xqChanges, before)
		var body struct {
			Changes []changeRow `json:"changes"`
			Cursor  *int64      `json:"cursor"`
		}
		status, raw := xqGet(c, path, &body)
		c.requiref(status == http.StatusOK, "GET %s answered %d: %s", path, status, raw)
		c.requiref(len(body.Changes) <= 50, "a first=50 page answered %d rows", len(body.Changes))
		for _, row := range body.Changes {
			seqs = append(seqs, row.Seq)
		}
		pages++
		if body.Cursor == nil {
			cursored = false
			break
		}
		c.requiref(pages < maxPages, "the backward walk did not exhaust in %d pages over %d rows", pages, len(forward))
		before = *body.Cursor
	}
	c.requiref(!cursored, "the last backward page still carries a cursor")
	for i := 1; i < len(seqs); i++ {
		c.requiref(seqs[i] < seqs[i-1], "the backward walk is not strictly decreasing: %d after %d", seqs[i], seqs[i-1])
	}
	c.requiref(len(seqs) == len(forward),
		"the backward walk read %d rows, the forward read %d over the same range", len(seqs), len(forward))
	c.requiref(seqs[0] == head, "the walk started at seq %d, want the head %d", seqs[0], head)
	c.requiref(seqs[len(seqs)-1] == forward[0].Seq,
		"the walk stopped at seq %d, want the changelog's first row %d", seqs[len(seqs)-1], forward[0].Seq)
	c.stepf("`before=%d&first=50` walked %d rows newest-first in %d pages down to seq %d, strictly decreasing, "+
		"the last page carrying no cursor: the same rows the forward read holds", head+1, len(seqs), pages, seqs[len(seqs)-1])
}

// xqCaseFeedFilters pins each narrowing of the changelog feed, and the
// refusal of the singular spelling that used to answer the whole feed.
func xqCaseFeedFilters(c *C) {
	// The case owns a delete, so the delete class is never empty whatever
	// else ran first.
	const doomed = "x-log-doomed"
	c.putRec(tasksCollection, doomed, map[string]any{"name": "A task LOG-03 deletes"})
	status, raw := c.do(http.MethodDelete, tasksCollection+"/"+doomed, nil, nil)
	c.requiref(status == http.StatusOK, "deleting `%s` answered %d: %s", doomed, status, raw)

	// The unfiltered read first: every narrowing below is held against the
	// rows it holds, and a row committed afterwards sits above its head and
	// belongs to no comparison.
	all := xqReadFeed(c, url.Values{})
	c.requiref(len(all) > 0, "the changelog is empty")
	head := all[len(all)-1].Seq
	within := func(rows []changeRow) []changeRow {
		var kept []changeRow
		for _, row := range rows {
			if row.Seq <= head {
				kept = append(kept, row)
			}
		}
		return kept
	}
	count := func(match func(changeRow) bool) int {
		n := 0
		for _, row := range all {
			if match(row) {
				n++
			}
		}
		return n
	}

	// kinds and excludeKinds partition the feed exactly.
	tasks := within(xqReadFeed(c, xqValues("kinds", xqTaskKind)))
	for _, row := range tasks {
		c.requiref(row.Kind == xqTaskKind, "`kinds=%s` returned a `%s` row at seq %d", xqTaskKind, row.Kind, row.Seq)
	}
	wantTasks := count(func(row changeRow) bool { return row.Kind == xqTaskKind })
	c.requiref(len(tasks) == wantTasks && wantTasks > 0,
		"`kinds=%s` returned %d rows; the unfiltered feed holds %d of that kind", xqTaskKind, len(tasks), wantTasks)
	others := within(xqReadFeed(c, xqValues("excludeKinds", xqTaskKind)))
	for _, row := range others {
		c.requiref(row.Kind != xqTaskKind, "`excludeKinds=%s` returned a `%s` row at seq %d", xqTaskKind, row.Kind, row.Seq)
	}
	c.requiref(len(tasks)+len(others) == len(all),
		"`kinds` (%d) and `excludeKinds` (%d) do not partition the %d-row feed", len(tasks), len(others), len(all))
	c.stepf("`kinds` and `excludeKinds` split the %d-row feed into %d task rows and %d others, with no row in "+
		"both and none in neither", len(all), len(tasks), len(others))

	// actors: the door's own writes, and nothing a bundle or an agent wrote.
	api := within(xqReadFeed(c, xqValues("actors", "api")))
	for _, row := range api {
		c.requiref(row.Actor == "api", "`actors=api` returned a `%s` row at seq %d", row.Actor, row.Seq)
	}
	wantAPI := count(func(row changeRow) bool { return row.Actor == "api" })
	c.requiref(len(api) == wantAPI && wantAPI > 0,
		"`actors=api` returned %d rows; the unfiltered feed holds %d", len(api), wantAPI)
	c.stepf("`actors=api` returned the feed's %d door-written rows and none of the bundle or agent rows", len(api))

	// ops takes the CHANGELOG's own verbs. put, patch and delete are the
	// write classes; a value outside the vocabulary is not refused, it
	// simply matches nothing, so a client that guesses `create` gets an
	// empty feed rather than a wrong one.
	deletes := within(xqReadFeed(c, xqValues("ops", "delete")))
	deleted := map[string]bool{}
	for _, row := range deletes {
		c.requiref(row.Op == "delete", "`ops=delete` returned a `%s` row at seq %d", row.Op, row.Seq)
		deleted[row.RecordID] = true
	}
	c.requiref(deleted[doomed], "`ops=delete` does not hold the delete of `%s`", doomed)
	writes := within(xqReadFeed(c, xqValues("ops", "put,patch")))
	for _, row := range writes {
		c.requiref(row.Op == "put" || row.Op == "patch",
			"`ops=put,patch` returned a `%s` row at seq %d", row.Op, row.Seq)
	}
	c.requiref(len(writes) > 0, "`ops=put,patch` returned nothing")
	invented := xqReadFeed(c, xqValues("ops", "create"))
	c.requiref(len(invented) == 0, "`ops=create` matched %d rows; `create` is not a changelog verb", len(invented))
	c.stepf("`ops=delete` returned %d rows including the case's own delete, the comma-joined `ops=put,patch` "+
		"returned %d, and `ops=create` matched nothing: the verbs are the changelog's (put, patch, delete), "+
		"not create/update/delete", len(deletes), len(writes))

	// The singular is the guess that used to return everything.
	e := xqBadRequest(c, xqChanges+"?op=delete", `unknown query parameter "op"`)
	c.requiref(strings.Contains(e.Error.Message, `did you mean "ops"`),
		"the singular `op` was refused without naming `ops`: %q", e.Error.Message)
	c.stepf("`?op=delete` was refused 400 naming the plural: %q", e.Error.Message)
}

// xqCaseRecordHistory narrows the feed to one record, which takes the id and
// the kind together because an id alone names one row per kind that uses it.
func xqCaseRecordHistory(c *C) {
	const id = "task-welcome-flow"
	all := xqReadFeed(c, url.Values{})
	c.requiref(len(all) > 0, "the changelog is empty")
	head := all[len(all)-1].Seq
	want := map[int64]bool{}
	for _, row := range all {
		if row.RecordID == id && row.Kind == xqTaskKind {
			want[row.Seq] = true
		}
	}
	c.requiref(len(want) > 0, "the changelog holds no row for `%s`; the stories wrote it", id)

	rows := xqReadFeed(c, xqValues("recordId", id, "recordKind", xqTaskKind))
	got := 0
	for _, row := range rows {
		if row.Seq > head {
			continue
		}
		got++
		c.requiref(row.RecordID == id && row.Kind == xqTaskKind,
			"the narrowed feed returned `%s`/`%s` at seq %d", row.Kind, row.RecordID, row.Seq)
		c.requiref(want[row.Seq], "the narrowed feed returned seq %d, which is not a row of `%s`", row.Seq, id)
	}
	c.requiref(got == len(want),
		"the narrowed feed returned %d rows for `%s`; the unfiltered feed holds %d", got, id, len(want))
	c.stepf("`recordId=%s&recordKind=%s` returned exactly the %d rows that record's whole history holds", id, xqTaskKind, got)

	// Either half alone is refused, and the refusal says why.
	e := xqBadRequest(c, xqChanges+"?recordId="+url.QueryEscape(id), "recordId requires recordKind")
	c.stepf("`recordId` without `recordKind`: 400, %q", e.Error.Message)
	e = xqBadRequest(c, xqChanges+"?recordKind="+url.QueryEscape(xqTaskKind), "recordKind requires recordId")
	c.stepf("`recordKind` without `recordId`: 400, %q", e.Error.Message)
}

// xqCaseCollectionWatch pins the per-collection watch: its kind, and the list
// parameters a stream cannot honor.
func xqCaseCollectionWatch(c *C) {
	st := xqOpenStream(c, tasksCollection+"?watch=1", 30*time.Second)
	defer st.close()
	head := st.bookmark(c)

	// The person is written FIRST and lands at the lower seq: were the
	// collection watch the whole feed, its row would arrive before the task's.
	c.putRec(personCollection, "x-log-watcher",
		map[string]any{"name": "Wren Watcher", "emails": []string{"wren@acme.example"}})
	const probe = "x-log-watched"
	c.putRec(tasksCollection, probe, map[string]any{"name": "The collection watch probe"})

	rows := 0
	for {
		row, ok := st.row(c)
		c.requiref(ok, "the task collection's watch delivered nothing for `%s` before the deadline: %v", probe, st.sc.Err())
		rows++
		c.requiref(row.Kind == xqTaskKind,
			"the task collection's watch delivered a `%s` row (`%s`) at seq %d", row.Kind, row.RecordID, row.Seq)
		c.requiref(row.Seq > head, "the collection watch re-delivered seq %d, at or below its bookmark %d", row.Seq, head)
		if row.RecordID == probe {
			break
		}
	}
	c.stepf("the watch on the task collection delivered `%s` (and %d other task rows) while ignoring the person "+
		"written before it", probe, rows-1)

	// The list grammar is refused rather than ignored: a `filter` a stream
	// drops would return the whole collection's tail looking narrowed.
	for _, param := range [][2]string{
		{"filter", `{"properties":{"status":{"eq":"open"}}}`},
		{"first", "2"},
	} {
		path := tasksCollection + "?" + xqValues("watch", "1", param[0], param[1]).Encode()
		e := xqBadRequest(c, path, param[0]+" is not supported with watch=1")
		c.stepf("`?watch=1&%s=…` on the collection: 400, %q", param[0], e.Error.Message)
	}
}

// xqCaseHeartbeat proves an idle stream is alive rather than merely silent.
func xqCaseHeartbeat(c *C) {
	// The interval is 30s and the case writes nothing, so it waits once, for
	// up to 40s. Nothing else here costs wall clock.
	start := time.Now()
	st := xqOpenStream(c, xqChanges+"?watch=1", 40*time.Second)
	defer st.close()
	st.bookmark(c)
	for {
		line, ok := st.line()
		c.requiref(ok, "the idle watch delivered no heartbeat in %s and the stream ended: %v",
			time.Since(start).Round(time.Second), st.sc.Err())
		var frame map[string]json.RawMessage
		c.requiref(json.Unmarshal([]byte(line), &frame) == nil, "undecodable watch line: %s", line)
		if len(frame) == 0 {
			c.stepf("the idle stream stayed open and sent a bare `{}` heartbeat after %s, decoding as a control "+
				"frame with no keys", time.Since(start).Round(time.Second))
			return
		}
		if _, isRow := frame["seq"]; isRow {
			continue // a write landed while the case waited; keep reading
		}
		c.requiref(false, "a control frame that is neither a heartbeat nor a row: %s", line)
	}
}

// xqCaseBlobRoundTrip stores bytes and reads them back by the digest they
// hash to.
func xqCaseBlobRoundTrip(c *C) {
	body := []byte("substrate live e2e BLOB-01: content addressing gives these bytes exactly one name.\n")
	sum := sha256.Sum256(body)
	want := xqBlobPrefix + hex.EncodeToString(sum[:])

	status, hdr, raw := xqRawByte(c, http.MethodPut, "/api/v1/blobs", body, "application/octet-stream")
	c.requiref(status == http.StatusCreated, "PUT /api/v1/blobs answered %d, want 201: %s", status, raw)
	info := xqBlobInfo(c, raw)
	c.requiref(info.Digest == want, "the store answered digest %q; the bytes hash to %q", info.Digest, want)
	c.requiref(info.Size == int64(len(body)), "the manifest says %d bytes, the body was %d", info.Size, len(body))
	c.requiref(info.Status == "stored", "the manifest's status is %q, want stored", info.Status)
	c.requiref(info.MediaType == "application/octet-stream", "the manifest's mediaType is %q", info.MediaType)
	c.requiref(hdr.Get("Location") == "/api/v1/blobs/"+want,
		"the Location header is %q, want the digest's own path", hdr.Get("Location"))
	c.stepf("`PUT /api/v1/blobs` with %d raw bytes answered 201 and digest `%s` (the sha-256 of the body), "+
		"status stored, Location at its own path", len(body), short(info.Digest))

	status, hdr, got := xqRawByte(c, http.MethodGet, "/api/v1/blobs/"+want, nil, "")
	c.requiref(status == http.StatusOK, "reading the blob answered %d: %s", status, got)
	c.requiref(bytes.Equal(got, body), "the blob read back %d bytes, %d were written", len(got), len(body))
	c.requiref(hdr.Get("ETag") == `"`+want+`"`, "the ETag is %q, want the quoted digest", hdr.Get("ETag"))
	c.requiref(hdr.Get("Content-Type") == "application/octet-stream",
		"the read's Content-Type is %q, want the type the PUT declared", hdr.Get("Content-Type"))
	c.stepf("`GET /api/v1/blobs/{digest}` streamed the same %d bytes back under `ETag: \"%s\"`", len(got), short(want))

	// The same bytes are the same blob: dedup is by digest, so a second PUT
	// adds an address for nothing new.
	status, _, raw = xqRawByte(c, http.MethodPut, "/api/v1/blobs", body, "application/octet-stream")
	c.requiref(status == http.StatusCreated, "the second PUT answered %d: %s", status, raw)
	again := xqBlobInfo(c, raw)
	c.requiref(again.Digest == want && again.Size == info.Size,
		"the same bytes stored a second time answered digest %q, size %d", again.Digest, again.Size)
	// The addressed spelling stores the same way when the address is right.
	status, _, raw = xqRawByte(c, http.MethodPut, "/api/v1/blobs/"+want, body, "application/octet-stream")
	c.requiref(status == http.StatusCreated, "PUT at the digest answered %d: %s", status, raw)
	c.requiref(xqBlobInfo(c, raw).Digest == want, "PUT at the digest answered a different digest: %s", raw)
	c.stepf("the same bytes PUT again, at `/blobs` and at `/blobs/{digest}`, answered the one digest `%s`", short(want))
}

// xqCaseBlobWrongDigest pins the refusal that keeps the address honest.
func xqCaseBlobWrongDigest(c *C) {
	body := []byte("substrate live e2e BLOB-02: these bytes do not hash to the address they are offered at.\n")
	wrong := xqBlobPrefix + strings.Repeat("0", 64)

	status, _, raw := xqRawByte(c, http.MethodPut, "/api/v1/blobs/"+wrong, body, "application/octet-stream")
	c.requiref(status == http.StatusBadRequest, "a wrong-digest PUT answered %d, want 400: %s", status, raw)
	e := xqRefused(c, raw)
	c.requiref(e.Error.Code == "bad_request", "the refusal's code is %q, want bad_request", e.Error.Code)
	c.requiref(strings.Contains(e.Error.Message, "digest mismatch") &&
		strings.Contains(e.Error.Message, "do not hash to the addressed digest"),
		"the refusal does not name the mismatch: %q", e.Error.Message)
	c.stepf("`PUT /api/v1/blobs/%s` with bytes that hash elsewhere: 400 `%s`, %q", prefix(wrong, 24), e.Error.Code, e.Error.Message)

	// The refusal stored nothing: an address is a claim about bytes, so a
	// rejected claim must not leave a readable blob behind it.
	status, _, raw = xqRawByte(c, http.MethodGet, "/api/v1/blobs/"+wrong, nil, "")
	c.requiref(status == http.StatusNotFound, "the refused digest reads %d, want 404: %s", status, raw)
	c.stepf("the refused address still reads 404: the mismatch stored nothing")
}

// xqBlob is the blob manifest a PUT answers. The bytes are never in it: they
// live in the byte store and stream through GET /blobs/{digest}.
type xqBlob struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Status    string `json:"status"`
}

func xqBlobInfo(c *C, raw []byte) xqBlob {
	c.t.Helper()
	var info xqBlob
	c.requiref(json.Unmarshal(raw, &info) == nil, "undecodable blob manifest: %s", raw)
	return info
}

// xqRawByte sends one request whose body is bytes, not JSON: the blob routes
// take the bytes THEMSELVES, and the answer's headers (Location, ETag) are
// part of what the case asserts, so this returns them.
func xqRawByte(c *C, method, path string, body []byte, contentType string) (int, http.Header, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.r.base+path, rd)
	c.requiref(err == nil, "building %s %s: %v", method, path, err)
	req.Header.Set("Authorization", "Bearer "+c.r.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.r.hc.Do(req)
	c.requiref(err == nil, "%s %s: %v", method, path, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	c.requiref(err == nil, "reading %s %s: %v", method, path, err)
	c.stepf("`%s %s` answered %d", method, path, resp.StatusCode)
	return resp.StatusCode, resp.Header, raw
}
