package engine

// The reads that find a record by what it points at seek the refs index, and
// a page's facts beside its rows cost the page, not each row. Row level
// security runs a clause it cannot prove leakproof only on the rows its
// policy admits, and jsonb containment, `->>` and `||` are not leakproof: a
// read probing `props` for a pointer reads every row of the kind, and one
// comparing a spelled path reads every pointer at the target. Each plan here
// is the statement the engine sent, taken on ds.db, the application's pool,
// as the role the engine runs under.

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// queryLog is a pgx tracer that keeps every statement the pool sends with
// its arguments, so a test can explain the very statement a read ran.
type queryLog struct {
	mu  sync.Mutex
	got []loggedQuery
}

type loggedQuery struct {
	sql  string
	args []any
}

func (l *queryLog) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, loggedQuery{sql: d.SQL, args: d.Args})
	return ctx
}

func (l *queryLog) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// take returns the statements sent since the last take and forgets them.
func (l *queryLog) take() []loggedQuery {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.got
	l.got = nil
	return out
}

// sent runs read and returns the one statement it sent whose text holds
// marker.
func (l *queryLog) sent(t *testing.T, marker string, read func()) loggedQuery {
	t.Helper()
	l.take()
	read()
	var found []loggedQuery
	for _, q := range l.take() {
		if strings.Contains(q.sql, marker) {
			found = append(found, q)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the read sent %d statements holding %q, want one", len(found), marker)
	}
	return found[0]
}

// analyzedPlan runs a logged statement under EXPLAIN ANALYZE on the
// application's pool, after checking the pool is the application's role.
// Seq scans are priced out, so a fixture of a few hundred rows still shows
// which index the predicate can seek: a predicate no index condition holds
// still filters, and says how many rows it read to do it.
func analyzedPlan(t *testing.T, ds *dataset, q loggedQuery) string {
	t.Helper()
	ctx := context.Background()
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var role string
	if err := tx.QueryRowContext(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if role != roleApp {
		t.Fatalf("the dataset's pool runs as %q, want %q: a plan taken past the policy proves nothing", role, roleApp)
	}
	if _, err := tx.ExecContext(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	return explainPlan(t, ctx, tx, `EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF) `+q.sql, q.args...)
}

var rowsRemoved = regexp.MustCompile(`Rows Removed by Filter: (\d+)`)

// hydrateStatements counts the statements that read what rides beside a
// page's rows: its former ids, annotations and blob manifests.
func hydrateStatements(sent []loggedQuery) int {
	n := 0
	for _, q := range sent {
		if strings.Contains(q.sql, "FROM former_ids") || strings.Contains(q.sql, "FROM annotations") ||
			strings.Contains(q.sql, "props->>'"+blobPropMediaType+"'") {
			n++
		}
	}
	return n
}

// assertSeeksRefs fails unless the plan reaches its rows through one of the
// refs indexes named and no node filters out more than allowed rows: the
// rows a read discards after finding them through the index are its own,
// never the kind's or the target's.
func assertSeeksRefs(t *testing.T, what, plan string, allowed int, indexes ...string) {
	t.Helper()
	if !slices.ContainsFunc(indexes, func(ix string) bool { return strings.Contains(plan, ix) }) {
		t.Fatalf("%s seeks none of %v:\n%s", what, indexes, plan)
	}
	for _, m := range rowsRemoved.FindAllStringSubmatch(plan, -1) {
		if n, _ := strconv.Atoi(m[1]); n > allowed {
			t.Fatalf("%s filters out %d rows, more than the %d it may:\n%s", what, n, allowed, plan)
		}
	}
}

// analyzeTables refreshes the planner's statistics and the visibility map,
// as a live table has them: planned against the empty tables a fixture
// starts from, a read prices a walk of every row as cheapest whatever its
// predicate is, and an index-only scan as no cheaper than a heap read.
func analyzeTables(t *testing.T, ds *dataset) {
	t.Helper()
	if _, err := ds.svc.admin.ExecContext(context.Background(), `VACUUM ANALYZE records, refs`); err != nil {
		t.Fatalf("vacuum analyze: %v", err)
	}
}

// oweRefs stands for a re-derivation of kind's refs rows for property that
// the pass behind the open has not finished, and returns its undo.
func oweRefs(t *testing.T, ds *dataset, kind, property string) func() {
	t.Helper()
	ctx := context.Background()
	if _, err := ds.db.ExecContext(ctx, `
		INSERT INTO index_reprojections (kind, refs, refs_properties, fts) VALUES ($1, true, ARRAY[$2], false)`,
		kind, property); err != nil {
		t.Fatalf("owe a re-derivation: %v", err)
	}
	return func() {
		t.Helper()
		if _, err := ds.db.ExecContext(ctx, `DELETE FROM index_reprojections WHERE kind = $1`, kind); err != nil {
			t.Fatal(err)
		}
	}
}

// dropRefs deletes one record's refs rows, as a declaration change the pass
// has not reached would leave them, so a read answering from refs and one
// answering from the row's properties disagree about it.
func dropRefs(t *testing.T, ds *dataset, kind, id string) {
	t.Helper()
	if _, err := ds.db.ExecContext(context.Background(),
		`DELETE FROM refs WHERE src_kind = $1 AND src = $2`, kind, id); err != nil {
		t.Fatalf("drop the refs rows of %s: %v", id, err)
	}
}

// The five reads that find a thread's messages: the transcript a run and a
// compaction replay, the next turn ordinal a decision writes under the
// changelog lock, the resume budget, the settle-time re-check and the
// judge's recent prose.
func TestReadPlanThreadReadsSeekTheRefsIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := &queryLog{}
	ds := openCoreDataset(t, WithTestQueryTracer(log))

	// Other threads' messages, so a read that scans the kind shows it.
	const noiseThreads, perThread = 12, 100
	for th := range noiseThreads {
		if err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
			for i := range perThread {
				if _, err := tx.put(substrate.PutInput{
					Kind: typeMessage, ID: fmt.Sprintf("n%02d-%03d", th, i),
					Properties: map[string]any{
						"role": "user", "content": "noise", "turn": i, msgRelThread: fmt.Sprintf("th%02d", th),
					},
				}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("plant thread %d: %v", th, err)
		}
	}
	// The thread read, a turn per write so each lands after the one before.
	const thread = "th-read"
	for i, turn := range []struct{ role, content string }{
		{"user", "hello"}, {"assistant", "hi"}, {msgRoleSystem, `{"event":"recordResolved"}`}, {"tool", "out"}, {"assistant", "bye"},
	} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: typeMessage, ID: "m" + strconv.Itoa(i),
			Properties: map[string]any{"role": turn.role, "content": turn.content, "turn": i, msgRelThread: thread},
		}); err != nil {
			t.Fatalf("put turn %d: %v", i, err)
		}
	}
	analyzeTables(t, ds)

	loop := &agentLoop{ds: ds, threadID: thread, openedAt: time.Now().Add(time.Hour)}
	transcript := func() string {
		t.Helper()
		rows, err := loop.loadThreadRows(ctx)
		if err != nil {
			t.Fatalf("transcript: %v", err)
		}
		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.id)
		}
		return strings.Join(ids, ",")
	}
	nextTurn := func() int64 {
		t.Helper()
		var next int64
		if err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
			var err error
			next, err = tx.nextThreadTurn(thread)
			return err
		}); err != nil {
			t.Fatalf("next turn: %v", err)
		}
		return next
	}
	resolutions := func() int {
		t.Helper()
		n, err := ds.systemRowsInLastHour(ctx, thread)
		if err != nil {
			t.Fatalf("resolutions: %v", err)
		}
		return n
	}
	prose := func() string {
		t.Helper()
		turns, err := ds.recentProseTurns(ctx, thread, 10)
		if err != nil {
			t.Fatalf("prose: %v", err)
		}
		out := make([]string, 0, len(turns))
		for _, turn := range turns {
			out = append(out, fmt.Sprint(turn["role"], ":", turn["content"]))
		}
		return strings.Join(out, ",")
	}
	// The re-check counts system rows written after the run opened; opened
	// an hour from now, it finds none and resumes nothing.
	recheck := func() { loop.recheckResolutions(ctx) }

	answers := func(when, wantTranscript string, wantNext int64, wantProse string) {
		t.Helper()
		if got := transcript(); got != wantTranscript {
			t.Fatalf("%s: the transcript is %q, want %q", when, got, wantTranscript)
		}
		if got := nextTurn(); got != wantNext {
			t.Fatalf("%s: the next turn is %d, want %d", when, got, wantNext)
		}
		if got := resolutions(); got != 1 {
			t.Fatalf("%s: %d resolutions in the last hour, want 1", when, got)
		}
		if got := prose(); got != wantProse {
			t.Fatalf("%s: the recent prose is %q, want %q", when, got, wantProse)
		}
	}
	answers("read through refs", "m0,m1,m2,m3,m4", 5, "user:hello,assistant:hi,assistant:bye")

	// Each statement seeks the thread's refs rows and reads only the
	// thread's messages; the prose read discards the thread's tool and
	// system turns, and nothing else.
	for _, read := range []struct {
		name, marker string
		run          func()
		allowed      int
	}{
		{"the transcript", "SELECT e.id, e.props FROM records e", func() { transcript() }, 0},
		{"the next turn", "max((e.props->>'turn')::bigint)", func() { nextTurn() }, 0},
		{"the resume budget", "SELECT count(*) FROM records e", func() { resolutions() }, 4},
		{"the re-check", "SELECT count(*) FROM records e", recheck, 5},
		{"the recent prose", "SELECT e.props->>'role', e.props->>'content' FROM records e", func() { prose() }, 2},
	} {
		q := log.sent(t, read.marker, read.run)
		if !strings.Contains(q.sql, "FROM refs r") || strings.Contains(q.sql, "@>") {
			t.Fatalf("%s does not read the thread through refs:\n%s", read.name, q.sql)
		}
		assertSeeksRefs(t, read.name, analyzedPlan(t, ds, q), read.allowed, "refs_dst_idx")
	}

	// The last turn's refs rows gone, a read through refs no longer finds it.
	dropRefs(t, ds, typeMessage, "m4")
	answers("refs rows dropped", "m0,m1,m2,m3", 4, "user:hello,assistant:hi")

	// While the kind's `thread` refs rows are owed, the rows' own properties
	// decide, and the turn is back.
	undo := oweRefs(t, ds, typeMessage, msgRelThread)
	answers("refs owed", "m0,m1,m2,m3,m4", 5, "user:hello,assistant:hi,assistant:bye")
	if q := log.sent(t, "SELECT count(*) FROM records e", recheck); strings.Contains(q.sql, "FROM refs r") {
		t.Fatalf("with refs owed the re-check still reads refs:\n%s", q.sql)
	}
	undo()
	answers("nothing owed again", "m0,m1,m2,m3", 4, "user:hello,assistant:hi")
}

// planScan matches a plan's table scan lines, planNode every node line, and
// planFiltered the rows a node read and threw away; EXPLAIN reports both row
// counts per loop.
var (
	planScan     = regexp.MustCompile(`(?:Seq|Index|Index Only|Bitmap Heap) Scan .*\(actual rows=(\d+) loops=(\d+)\)`)
	planNode     = regexp.MustCompile(`\(actual rows=\d+ loops=\d+\)|\(never executed\)`)
	planFiltered = regexp.MustCompile(`Rows Removed by Filter: (\d+)`)
)

// tableRowsRead totals the table rows a plan's scans read over every loop,
// kept or filtered out, so a scan repeated once per outer row counts as often
// as it ran. A bitmap index scan's rows are its heap scan's.
func tableRowsRead(plan string) int {
	total, loops, inScan := 0, 0, false
	for _, line := range strings.Split(plan, "\n") {
		if m := planScan.FindStringSubmatch(line); m != nil && !strings.Contains(line, "Bitmap Index Scan") {
			rows, _ := strconv.Atoi(m[1])
			loops, _ = strconv.Atoi(m[2])
			total += rows * loops
			inScan = true
			continue
		}
		if planNode.MatchString(line) {
			inScan = false
			continue
		}
		if m := planFiltered.FindStringSubmatch(line); m != nil && inScan {
			n, _ := strconv.Atoi(m[1])
			total += n * loops
		}
	}
	return total
}

// The resolution sweep reads each message of the kind at most once, never
// once per settled thread. Under row level security no index reaches the
// thread pointer on a message's `props`, and the probe of it per thread this
// replaced ran for a quarter of an hour on a repository of 23k threads,
// holding back every concurrent index build behind its snapshot.
func TestReadPlanResolutionSweepReadsEachMessageOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := &queryLog{}
	ds := openCoreDataset(t, WithTestQueryTracer(log))

	now := nowUTC()
	settledAgo, settlesLater := now.Add(-time.Hour), now.Add(time.Hour)
	putThread := func(tx *txn, id, status string, finished time.Time) error {
		_, err := tx.put(substrate.PutInput{Kind: typeThread, ID: id, Properties: map[string]any{
			"agent": "sweep.test.dev/crew/agent", "provider": "fake", "model": "fake", "mode": "call",
			"agentDepth": 0, "status": status, "startedAt": settledAgo.Add(-time.Hour).Format(time.RFC3339Nano),
			"finishedAt": finished.Format(time.RFC3339Nano),
		}})
		return err
	}
	putMessage := func(tx *txn, id, thread, role string, turn int) error {
		_, err := tx.put(substrate.PutInput{Kind: typeMessage, ID: id, Properties: map[string]any{
			"role": role, "content": "x", "turn": turn, msgRelThread: thread,
		}})
		return err
	}
	// Threads that settle after every row they hold, a system row among
	// them: the sweep owes none of them anything.
	const quiet, perThread = 12, 20
	messages := 0
	for th := range quiet {
		if err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
			id := fmt.Sprintf("quiet-%02d", th)
			if err := putThread(tx, id, threadOK, settlesLater); err != nil {
				return err
			}
			for i := range perThread {
				role := "user"
				if i == perThread/2 {
					role = msgRoleSystem
				}
				if err := putMessage(tx, fmt.Sprintf("%s-%03d", id, i), id, role, i); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("plant thread %d: %v", th, err)
		}
		messages += perThread
	}
	// Threads that settled before their last row. Only a live system row
	// on a live thread that settled ok or over budget is owed a resume.
	late := []struct{ id, status, role string }{
		{"owed-ok", threadOK, msgRoleSystem},
		{"owed-overbudget", threadOverBudget, msgRoleSystem},
		{"still-running", threadRunning, msgRoleSystem},
		{"errored", threadError, msgRoleSystem},
		{"only-prose", threadOK, "assistant"},
		{"row-deleted", threadOK, msgRoleSystem},
		{"thread-deleted", threadOK, msgRoleSystem},
	}
	for _, th := range late {
		if err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
			if err := putThread(tx, th.id, th.status, settledAgo); err != nil {
				return err
			}
			if err := putMessage(tx, th.id+"-000", th.id, "assistant", 0); err != nil {
				return err
			}
			return putMessage(tx, th.id+"-001", th.id, th.role, 1)
		}); err != nil {
			t.Fatalf("plant thread %s: %v", th.id, err)
		}
		messages += 2
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeMessage, "row-deleted-001", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the row: %v", err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeThread, "thread-deleted", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the thread: %v", err)
	}
	analyzeTables(t, ds)

	owed := func(when, want string) {
		t.Helper()
		ids, err := ds.unconsumedResolutions(ctx)
		if err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		if got := strings.Join(ids, ","); got != want {
			t.Fatalf("%s: the sweep owes %q, want %q", when, got, want)
		}
	}
	// What the sweep may read: each message's refs row and the message
	// itself, and each thread, once.
	budget := 2*messages + quiet + len(late)
	sweep := func(when, marker, want string) (loggedQuery, string) {
		t.Helper()
		q := log.sent(t, marker, func() { owed(when, want) })
		plan := analyzedPlan(t, ds, q)
		if n := tableRowsRead(plan); n > budget {
			t.Fatalf("%s: the sweep read %d rows, more than the %d it may:\n%s", when, n, budget, plan)
		}
		return q, plan
	}

	q, plan := sweep("read through refs", "WITH settled", "owed-ok,owed-overbudget")
	if !strings.Contains(q.sql, "FROM refs r") || !strings.Contains(plan, "refs_dst_idx") {
		t.Fatalf("the sweep does not seek the messages through refs:\n%s\n%s", q.sql, plan)
	}

	// The owed row's refs rows gone, a read through refs no longer finds it.
	dropRefs(t, ds, typeMessage, "owed-ok-001")
	sweep("refs rows dropped", "WITH settled", "owed-overbudget")

	// While the kind's `thread` refs rows are owed, the rows' own properties
	// decide, and the thread is owed again.
	undo := oweRefs(t, ds, typeMessage, msgRelThread)
	if q, _ := sweep("refs owed", "WITH resolved", "owed-ok,owed-overbudget"); strings.Contains(q.sql, "FROM refs r") {
		t.Fatalf("with refs owed the sweep still reads refs:\n%s", q.sql)
	}
	undo()
	sweep("nothing owed again", "WITH settled", "owed-overbudget")
}

// A list filtered by a repeated reference reads refs at the property's own
// site, every item's row, where it read every row of the kind with one
// containment probe per value.
func TestReadPlanRepeatedReferenceFilterSeeksTheRefsIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := &queryLog{}
	ds, target, pointer := refIndexDataset(t, WithTestQueryTracer(log))
	// Every bulk pointer has seen a; only p3 has seen c.
	if err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
		for i := range 400 {
			if _, err := tx.put(substrate.PutInput{
				Kind: pointer, ID: fmt.Sprintf("bulk%03d", i), Properties: map[string]any{"alsoSeen": []any{"a"}},
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("plant pointers: %v", err)
	}
	analyzeTables(t, ds)
	list := func(f substrate.Filter) string {
		t.Helper()
		page, err := ds.List(ctx, substrate.Query{First: 500, Filter: f})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		return recordIDs(page)
	}
	seen := func(c substrate.Cond) substrate.Filter {
		return substrate.Filter{Kinds: []string{pointer}, Properties: map[string]substrate.Cond{"alsoSeen": c}}
	}

	q := log.sent(t, "FROM refs r", func() { list(seen(substrate.Cond{Eq: "c"})) })
	if strings.Contains(q.sql, "@>") || strings.Contains(q.sql, "r.ord") {
		t.Fatalf("the repeated reference filter is not one refs arm over every item:\n%s", q.sql)
	}
	assertSeeksRefs(t, "the repeated reference filter", analyzedPlan(t, ds, q), 0, "refs_dst_idx")

	// The rows: one value, two, the first item and a later one, none, and a
	// filter naming no kind.
	if got := list(seen(substrate.Cond{Eq: "c"})); got != "p3" {
		t.Fatalf("alsoSeen c answered %q, want p3", got)
	}
	if got := list(seen(substrate.Cond{In: []any{"b", "c"}})); got != "p2,p3" {
		t.Fatalf("alsoSeen in [b, c] answered %q, want p2,p3", got)
	}
	if got := list(seen(substrate.Cond{Eq: "nowhere"})); got != "" {
		t.Fatalf("alsoSeen nowhere answered %q, want nothing", got)
	}
	if got := list(substrate.Filter{Properties: map[string]substrate.Cond{
		"alsoSeen": {Eq: vocabulary.RecordPath(target, "c")},
	}}); got != "p3" {
		t.Fatalf("a kindless filter on alsoSeen c answered %q, want p3", got)
	}
	// A pointer stored under a target's former id answers a filter naming the
	// record it became.
	if _, err := ds.db.ExecContext(ctx,
		`INSERT INTO former_ids (record_kind, former_id, record_id) VALUES ($1, 'old-c', 'c')`, target); err != nil {
		t.Fatalf("plant a former id: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: pointer, ID: "pold", Properties: map[string]any{"alsoSeen": []any{"b", "old-c"}},
	}); err != nil {
		t.Fatalf("put the pointer at the former id: %v", err)
	}
	if got := list(seen(substrate.Cond{Eq: "c"})); got != "p3,pold" {
		t.Fatalf("alsoSeen c answered %q, want p3,pold", got)
	}

	// p3's refs rows gone, refs no longer finds it; owed, its properties do.
	dropRefs(t, ds, pointer, "p3")
	if got := list(seen(substrate.Cond{Eq: "c"})); got != "pold" {
		t.Fatalf("with p3's refs rows dropped, alsoSeen c answered %q, want refs alone to decide", got)
	}
	undo := oweRefs(t, ds, pointer, "alsoSeen")
	if got := list(seen(substrate.Cond{Eq: "c"})); got != "p3,pold" {
		t.Fatalf("with refs owed, alsoSeen c answered %q, want p3,pold off their properties", got)
	}
	undo()
}

// A referencing page's matches seek the page's own pointers: each arm names
// the source kind and ids as plain columns, which refs_dst_idx carries right
// after the target, so a hot target's other pointers are never read (#798).
// The target holds a few thousand pointers: on a few hundred the planner
// rightly prices filtering them all as cheaper than one probe per page row.
func TestReadPlanReferencingMatchesSeekThePage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := &queryLog{}
	ds, target, pointer := refIndexDataset(t, WithTestQueryTracer(log))
	const hot, batch = 2000, 500
	for from := 0; from < hot; from += batch {
		if err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
			for i := from; i < from+batch; i++ {
				if _, err := tx.put(substrate.PutInput{
					Kind: pointer, ID: fmt.Sprintf("h%04d", i), Properties: map[string]any{"target": "b"},
				}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("plant pointers from %d: %v", from, err)
		}
	}
	analyzeTables(t, ds)

	var page *substrate.Page
	q := log.sent(t, "SELECT r.src_kind, r.src, r.property, r.path FROM refs r", func() {
		var err error
		// By id, descending, so p2 is on the page with both its sites.
		if page, err = ds.List(ctx, substrate.Query{First: 50, Filter: substrate.Filter{
			Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath(target, "b")},
		}, OrderBy: []substrate.Order{{Property: "id", Desc: true}}}); err != nil {
			t.Fatalf("list: %v", err)
		}
	})
	if strings.Contains(q.sql, "||") {
		t.Fatalf("the matches read compares a spelled path:\n%s", q.sql)
	}
	// Either refs index seeks the page alone: refs_dst_idx by the target and
	// then the page's sources, refs_pkey by the page's sources and then the
	// target, discarding the page's own other pointers (p2's at a). A spelled
	// path seeks only the target and filters every other pointer at it.
	assertSeeksRefs(t, "the matches read", analyzedPlan(t, ds, q), 1, "refs_dst_idx", "refs_pkey")

	// The matches: one entry per record on the page, p2 from both its sites.
	if len(page.Records) != 50 || len(page.Matches) != 50 || page.Records[0].ID != "p2" {
		t.Fatalf("a page of %d records carries %d matches, want 50 of each, p2 first", len(page.Records), len(page.Matches))
	}
	for _, e := range page.Records {
		sites := page.Matches[vocabulary.RecordPath(e.Kind, e.ID)]
		want := []substrate.ReferenceSite{{Property: "target"}}
		if e.ID == "p2" {
			want = []substrate.ReferenceSite{{Property: "alsoSeen"}, {Property: "target"}}
		}
		if fmt.Sprint(sites) != fmt.Sprint(want) {
			t.Fatalf("%s matches %+v, want %+v", e.ID, sites, want)
		}
	}
}

// A list page reads its former ids, annotations and blob manifests in one
// statement each, so a page of 50 sends what a page of 5 does, and each
// record still carries its own. The referents an expanded page carries are
// read the same way (loadReferents).
func TestReadPlanListHydratesAPageInOneStatementPerFact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := &queryLog{}
	ds := openCoreDataset(t, WithTestQueryTracer(log))
	const pkg = "readplan.test.dev/readplan"
	doc, note := pkg+"/doc", pkg+"/note"
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "readplan", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "note"},
				map[string]any{"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"file": map[string]any{"type": "blobref"},
				}}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "doc"},
				map[string]any{"properties": map[string]any{
					"name":  map[string]any{"type": "string"},
					"file":  map[string]any{"type": "blobref"},
					"files": map[string]any{"type": "blobref", "repeated": true},
					"see":   map[string]any{"type": "reference", "kind": note},
				}}),
		},
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	png, err := ds.PutBlob(ctx, substrate.ActorAPI, substrate.BlobUpload{MediaType: "image/png", Name: "a.png"}, []byte("first"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	txt, err := ds.PutBlob(ctx, substrate.ActorAPI, substrate.BlobUpload{MediaType: "text/plain", Name: "b.txt"}, []byte("second"), "")
	if err != nil {
		t.Fatalf("put blob: %v", err)
	}
	// Every doc names both blobs, carries an annotation of its own and
	// points at a note of its own, which names the txt.
	for i := range 60 {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: note, ID: fmt.Sprintf("n%02d", i),
			Properties: map[string]any{"name": strconv.Itoa(i), "file": txt.Digest},
		}); err != nil {
			t.Fatalf("put note %d: %v", i, err)
		}
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: doc, ID: fmt.Sprintf("d%02d", i),
			Properties: map[string]any{
				"name": strconv.Itoa(i), "file": png.Digest, "files": []any{txt.Digest, png.Digest},
				"see": fmt.Sprintf("n%02d", i),
			},
			Annotations: map[string]any{"readplan/n": strconv.Itoa(i)},
		}); err != nil {
			t.Fatalf("put doc %d: %v", i, err)
		}
	}
	// d01 absorbs a merged doc, so its former id is one record's alone.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: doc, ID: "gone", Properties: map[string]any{"name": "gone"},
	}); err != nil {
		t.Fatalf("put the doc to merge: %v", err)
	}
	if _, err := ds.Merge(ctx, substrate.ActorAPI, substrate.MergeInput{Kind: doc, Winner: "d01", Loser: "gone"}); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// page reads one page of docs, by id, and counts the statements that
	// read what rides beside its rows.
	page := func(first int, expand ...string) (*substrate.Page, int) {
		t.Helper()
		log.take()
		p, err := ds.List(ctx, substrate.Query{
			First: first, WithAnnotations: true, Expand: expand,
			Filter:  substrate.Filter{Kinds: []string{doc}},
			OrderBy: []substrate.Order{{Property: "id"}},
		})
		if err != nil {
			t.Fatalf("list %d: %v", first, err)
		}
		return p, hydrateStatements(log.take())
	}
	small, smallN := page(5)
	large, largeN := page(50)
	if len(small.Records) != 5 || len(large.Records) != 50 {
		t.Fatalf("pages of %d and %d records, want 5 and 50", len(small.Records), len(large.Records))
	}
	if smallN != 3 || largeN != 3 {
		t.Fatalf("a page of 5 sent %d statements for its former ids, annotations and blobs, a page of 50 sent %d; want 3 each", smallN, largeN)
	}
	// Expanded, the referents add their former ids and blobs, one statement
	// each, however many the page names.
	small, smallN = page(5, "see")
	expanded, expandedN := page(50, "see")
	if len(small.Included) != 5 || len(expanded.Included) != 50 {
		t.Fatalf("expanded pages carry %d and %d referents, want 5 and 50", len(small.Included), len(expanded.Included))
	}
	if smallN != 5 || expandedN != 5 {
		t.Fatalf("an expanded page of 5 sent %d statements for its former ids, annotations and blobs, one of 50 sent %d; want 5 each", smallN, expandedN)
	}
	for i := range 50 {
		path := vocabulary.RecordPath(note, fmt.Sprintf("n%02d", i))
		n := expanded.Included[path]
		if n == nil {
			t.Fatalf("the expanded page does not carry %s", path)
		}
		file, ok := n.Properties["file"].(map[string]any)
		if !ok || file["digest"] != txt.Digest || file["name"] != "b.txt" {
			t.Fatalf("%s file reads %#v, want the txt's manifest", path, n.Properties["file"])
		}
	}

	for i, e := range large.Records {
		if want := fmt.Sprintf("d%02d", i); e.ID != want {
			t.Fatalf("record %d is %s, want %s", i, e.ID, want)
		}
		if got := e.Annotations["readplan/n"]; got != strconv.Itoa(i) {
			t.Fatalf("%s carries annotation %v, want its own %d", e.ID, got, i)
		}
		wantFormer := "[]"
		if e.ID == "d01" {
			wantFormer = "[gone]"
		}
		if got := fmt.Sprint(e.FormerIDs); got != wantFormer {
			t.Fatalf("%s carries former ids %s, want %s", e.ID, got, wantFormer)
		}
		file, ok := e.Properties["file"].(map[string]any)
		if !ok || file["digest"] != png.Digest || file["name"] != "a.png" || file["mediaType"] != "image/png" {
			t.Fatalf("%s file reads %#v, want the png's manifest", e.ID, e.Properties["file"])
		}
		files, ok := e.Properties["files"].([]any)
		if !ok || len(files) != 2 {
			t.Fatalf("%s files reads %#v, want two manifests", e.ID, e.Properties["files"])
		}
		first, _ := files[0].(map[string]any)
		second, _ := files[1].(map[string]any)
		if first["digest"] != txt.Digest || first["name"] != "b.txt" || second["digest"] != png.Digest || second["name"] != "a.png" {
			t.Fatalf("%s files reads %#v, want the txt then the png manifest", e.ID, files)
		}
		// Two values naming one blob are two maps: a reader editing one
		// does not edit the other.
		second["name"] = "edited"
		if file["name"] != "a.png" {
			t.Fatalf("%s: two values naming one blob share one manifest map", e.ID)
		}
	}
}

// A filter naming a kind that declares the repeated reference and one that
// declares the same name a list of strings matches the string list's rows
// that hold the path's text, as the containment probes did, and answers the
// same whether refs decide or a re-derivation is owed.
func TestReadPlanRepeatedReferenceFilterKeepsAStringListsRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, target, pointer := refIndexDataset(t)
	const pkg = "readplan.test.dev/plain"
	tagged := pkg + "/tagged"
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "plain", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "tagged"},
				map[string]any{"properties": map[string]any{
					"alsoSeen": map[string]any{"type": "string", "repeated": true},
				}}),
		},
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	// s1 holds c's path, s2 b's, s3 c's bare id, which names no path.
	for id, seen := range map[string][]any{
		"s1": {vocabulary.RecordPath(target, "c"), "other"},
		"s2": {vocabulary.RecordPath(target, "b")},
		"s3": {"c"},
	} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: tagged, ID: id, Properties: map[string]any{"alsoSeen": seen},
		}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	list := func(f substrate.Filter) string {
		t.Helper()
		page, err := ds.List(ctx, substrate.Query{First: 100, Filter: f})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		return recordIDs(page)
	}
	answers := func(when string) {
		t.Helper()
		for _, c := range []struct {
			name string
			f    substrate.Filter
			want string
		}{
			{"alsoSeen c over both kinds", substrate.Filter{
				Kinds: []string{pointer, tagged}, Properties: map[string]substrate.Cond{"alsoSeen": {Eq: "c"}},
			}, "p3,s1"},
			{"alsoSeen in [b, c] over both kinds", substrate.Filter{
				Kinds: []string{pointer, tagged}, Properties: map[string]substrate.Cond{"alsoSeen": {In: []any{"b", "c"}}},
			}, "p2,p3,s1,s2"},
			{"a kindless alsoSeen c", substrate.Filter{
				Properties: map[string]substrate.Cond{"alsoSeen": {Eq: vocabulary.RecordPath(target, "c")}},
			}, "p3,s1"},
		} {
			if got := list(c.f); got != c.want {
				t.Fatalf("%s: %s answered %q, want %q", when, c.name, got, c.want)
			}
		}
	}
	answers("refs decide")
	undo := oweRefs(t, ds, pointer, "alsoSeen")
	answers("refs owed")
	undo()
}

// A window read finds the overrides of its series through refs_dst_idx, by
// each series' kind and id, so another series' overrides are never read;
// and its rows, its series and its overrides each read their former ids and
// annotations in one statement, so a page of 50 sends what a page of 5 does.
func TestReadPlanWindowSeeksOverridesAndHydratesEachPart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := &queryLog{}
	ds := openCoreDataset(t, WithTestQueryTracer(log))
	const pkg = "readplan.test.dev/timeline"
	meeting, series, exception := pkg+"/meeting", pkg+"/series", pkg+"/exception"
	temporal := vocabulary.TraitTemporalCore + "(range)"
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "timeline", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "meeting"}, map[string]any{
				"traits": []any{temporal},
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"team": map[string]any{"type": "string"},
				},
			}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "series"}, map[string]any{
				"traits": []any{temporal, vocabulary.TraitRecurringCore},
				"properties": map[string]any{
					"name":       map[string]any{"type": "string"},
					"team":       map[string]any{"type": "string"},
					"recurrence": map[string]any{"type": "recurrence"},
					"rdates":     map[string]any{"type": "datetime", "repeated": true},
					"exdates":    map[string]any{"type": "datetime", "repeated": true},
					"timezone":   map[string]any{"type": "timezone"},
				},
			}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "exception"}, map[string]any{
				"traits": []any{temporal, vocabulary.TraitOverrideCore},
				"properties": map[string]any{
					"name":         map[string]any{"type": "string"},
					"recurrenceOf": map[string]any{"type": "reference", "kind": series},
					"originalAt":   map[string]any{"type": "datetime"},
				},
			}),
		},
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	from, to := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	stamp := func(at time.Time) string { return at.Format(time.RFC3339) }
	if err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
		// Sixty meetings in the window, an hour apart, each annotated.
		for i := range 60 {
			at := from.Add(time.Duration(i) * time.Hour)
			if _, err := tx.put(substrate.PutInput{
				Kind: meeting, ID: fmt.Sprintf("m%02d", i),
				Properties: map[string]any{
					"name": strconv.Itoa(i), "team": "core", "at": stamp(at), "endsAt": stamp(at.Add(30 * time.Minute)),
				},
				Annotations: map[string]any{"readplan/n": strconv.Itoa(i)},
			}); err != nil {
				return err
			}
		}
		// Three weekly series the window's filter admits, each with an
		// override of its second slot, and one series it does not.
		for _, s := range []struct{ id, team string }{{"s0", "core"}, {"s1", "core"}, {"s2", "core"}, {"hot", "other"}} {
			if _, err := tx.put(substrate.PutInput{
				Kind: series, ID: s.id,
				Properties: map[string]any{
					"name": s.id, "team": s.team, "recurrence": "RRULE:FREQ=WEEKLY", "timezone": "Europe/London",
					"at": "2026-07-01T08:00:00Z", "endsAt": "2026-07-01T08:30:00Z",
				},
			}); err != nil {
				return err
			}
		}
		for i := range 3 {
			if _, err := tx.put(substrate.PutInput{
				Kind: exception, ID: fmt.Sprintf("s%d_20260708T080000Z", i),
				Properties: map[string]any{
					"name": "moved", "at": "2026-07-08T09:00:00Z", "endsAt": "2026-07-08T09:30:00Z",
					"recurrenceOf": fmt.Sprintf("s%d", i), "originalAt": "2026-07-08T08:00:00Z",
				},
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("plant the timeline: %v", err)
	}
	// The series the filter leaves out holds a few thousand overrides, none
	// of whose slots is in the window: a read that does not seek its own
	// series' pointers reads them and filters them out.
	const hot, batch = 2000, 500
	for start := 0; start < hot; start += batch {
		if err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
			for i := start; i < start+batch; i++ {
				slot := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour)
				if _, err := tx.put(substrate.PutInput{
					Kind: exception, ID: fmt.Sprintf("hot_%04d", i),
					Properties: map[string]any{
						"name": "hot", "at": stamp(slot), "endsAt": stamp(slot.Add(30 * time.Minute)),
						"recurrenceOf": "hot", "originalAt": stamp(slot),
					},
				}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatalf("plant the hot overrides from %d: %v", start, err)
		}
	}
	analyzeTables(t, ds)

	// window reads one page and returns the statements it sent.
	window := func(first int) (*substrate.WindowPage, []loggedQuery) {
		t.Helper()
		log.take()
		page, err := ds.Window(ctx, substrate.WindowQuery{
			Filter: substrate.Filter{Kinds: []string{meeting, series}, Properties: map[string]substrate.Cond{
				"team": {Eq: "core"},
				"at":   {Gte: stamp(from), Lt: stamp(to)},
			}},
			From: from, To: to, First: first, WithAnnotations: true,
		})
		if err != nil {
			t.Fatalf("window %d: %v", first, err)
		}
		return page, log.take()
	}
	ids := func(records []*substrate.Record) string {
		out := make([]string, 0, len(records))
		for _, e := range records {
			out = append(out, e.ID)
		}
		return strings.Join(out, ",")
	}
	small, smallSent := window(5)
	large, largeSent := window(50)
	if len(small.Rows) != 5 || len(large.Rows) != 50 {
		t.Fatalf("windows of %d and %d rows, want 5 and 50", len(small.Rows), len(large.Rows))
	}
	// Former ids for the rows, the series and the overrides, annotations for
	// the rows.
	if smallN, largeN := hydrateStatements(smallSent), hydrateStatements(largeSent); smallN != 4 || largeN != 4 {
		t.Fatalf("a window of 5 rows sent %d statements for its former ids and annotations, one of 50 sent %d; want 4 each", smallN, largeN)
	}
	for i, e := range large.Rows {
		if want := fmt.Sprintf("m%02d", i); e.ID != want || e.Annotations["readplan/n"] != strconv.Itoa(i) {
			t.Fatalf("row %d is %s annotated %v, want %s annotated %d", i, e.ID, e.Annotations, want, i)
		}
	}
	if got := ids(large.Series); got != "s0,s1,s2" {
		t.Fatalf("the series are %q, want s0,s1,s2", got)
	}
	if got := ids(large.Overrides); got != "s0_20260708T080000Z,s1_20260708T080000Z,s2_20260708T080000Z" {
		t.Fatalf("the overrides are %q, want one per series", got)
	}

	var overrides []loggedQuery
	for _, q := range smallSent {
		if strings.Contains(q.sql, "(props->>'originalAt')::timestamptz") {
			overrides = append(overrides, q)
		}
	}
	if len(overrides) != 1 {
		t.Fatalf("the window sent %d overrides reads, want one", len(overrides))
	}
	q := overrides[0]
	if strings.Contains(q.sql, "||") {
		t.Fatalf("the overrides read compares a spelled path:\n%s", q.sql)
	}
	assertSeeksRefs(t, "the overrides read", analyzedPlan(t, ds, q), 0, "refs_dst_idx")
}

// The lexical arm's statistics read a repository at or under statsSample
// rows whole, without TABLESAMPLE, so the policy's index finds its rows
// rather than a sample visiting every page of every repository; every
// sampling method answers the same averages over a whole sample; and a
// search holding stale statistics answers them while its read refreshes them.
func TestReadPlanSearchStatisticsSample(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := &queryLog{}
	ds, _, _ := refIndexDataset(t, WithTestQueryTracer(log))
	// The first read must land inside the search that started it, so log.sent
	// sees its statement before the search answers.
	ds.statsCache.mu.Lock()
	ds.statsCache.wait = time.Minute
	ds.statsCache.mu.Unlock()

	q := log.sent(t, "avg(nullif(l.a, 0))", func() {
		if _, err := ds.searchStats(ctx); err != nil {
			t.Fatalf("statistics: %v", err)
		}
	})
	if strings.Contains(q.sql, "TABLESAMPLE") {
		t.Fatalf("a repository under %d rows was sampled:\n%s", statsSample, q.sql)
	}

	var rows int64
	var avg [4]float64
	for _, method := range []string{sampleEvery, sampleSystem, sampleBernoulli} {
		n, got, err := ds.sampleBandLengths(ctx, method, 100)
		if err != nil {
			t.Fatalf("%q at 100%%: %v", method, err)
		}
		if method == sampleEvery {
			rows, avg = n, got
			if rows == 0 || avg[0] == 0 {
				t.Fatalf("the whole read found %d rows and title average %v, want the fixture's rows", rows, avg[0])
			}
			continue
		}
		if n != rows || got != avg {
			t.Fatalf("%s at 100%% read %d rows averaging %v, the whole read %d averaging %v", method, n, got, rows, avg)
		}
	}

	// Stale statistics: a search answers them at once and the read it starts
	// lands for the next search.
	stale := searchStats{n: -1, at: time.Now().Add(-2 * statsTTL)}
	ds.statsCache.mu.Lock()
	ds.statsCache.s = stale
	ds.statsCache.mu.Unlock()
	if got, err := ds.searchStats(ctx); err != nil || got.n != stale.n {
		t.Fatalf("with stale statistics held the search answered n=%v (%v), want the held n=%v", got.n, err, stale.n)
	}
	deadline := time.Now().Add(30 * time.Second)
	for ds.statsCache.held().n != float64(rows) {
		if time.Now().After(deadline) {
			t.Fatalf("the refresh never landed: held n=%v, want %d", ds.statsCache.held().n, rows)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
