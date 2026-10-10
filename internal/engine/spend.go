package engine

// The spend cap (decision 0149). An agent's `budgets.spendCentsPerDay` and the
// repository's `substrate.reamde.dev/llm/spendCentsPerDay` setting cap what
// root agent runs may spend in a rolling 24 hours. At a cap the dispatcher
// HOLDS the agent's triggers: the delivery is not claimed, so the cursor or
// fire state stays, no run row and no park is written, and the next pass
// checks again. A chat, a direct call, a hand's run, wake or retry, and any
// other root run are refused before the loop opens a thread.
//
// Spend is what the threads RECORDED: llm/thread.costUSD over the
// repository's root threads whose `finishedAt` falls inside the window (a
// root thread's cost already rolls up its sub-agents'), read once per
// dispatcher pass as a snapshot per thread. What the snapshot cannot hold
// yet is kept here, in memory, per root run and keyed by its thread: the
// charges of a run still going, the cost a settle committed after the
// snapshot was read, and the charges of a run whose settle never committed,
// which no row will ever carry and which therefore stay counted for the
// whole window. Reconciling by thread is what keeps a run from counting
// twice when the snapshot already holds its settle.
//
// The documented overshoot: a run admitted under a cap runs to its own end
// (its maxTurns and deadline still bound it), and a model call already
// running is charged only when it returns, so an admission made meanwhile
// cannot see it. The in-memory part does not survive a restart.

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	// spendSettingID is the repository's cap: a core `setting` under the llm
	// package's prefix (decision 0076). Nothing seeds it, so an absent row or
	// an empty value is no cap.
	spendSettingID = "substrate.reamde.dev/llm/spendCentsPerDay"
	// spendWindow is how far back the rolling window reaches.
	spendWindow = 24 * time.Hour
	// spendViewMaxAge bounds how old a read of the snapshot and the
	// repository cap may be for a check made outside a dispatcher pass (a
	// status read, a chat), since a pass is what expires it.
	spendViewMaxAge = time.Minute
	// spendEpsilon absorbs a float sum's last bits.
	spendEpsilon = 1e-9
)

// errSpendHeld marks a run refused at a spend cap. It is ErrGuard to every
// caller that only knows the sentinels, and its own value to the code that
// has to tell a hold from any other refusal.
var errSpendHeld = fmt.Errorf("%w", substrate.ErrGuard)

// spendSnapshotQuery reads the window's settled spend per root thread. Root
// threads only (agentDepth 0), because a root thread's costUSD already holds
// its sub-agents' spend. Tombstoned threads count: the money was spent. The
// updated_at bound is a prefilter the index serves: a settle writes
// finishedAt in a patch, so a row whose finishedAt is in the window was
// updated in it too; the minute of slack covers the transaction's own clock.
var spendSnapshotQuery = `
	SELECT id, ` + referencePathSQL("props", "agent") + `, coalesce((props->>'costUSD')::float8, 0)
	FROM records
	WHERE kind = $1
	  AND coalesce(props->>'agentDepth', '0') = '0'
	  AND updated_at >= $2::timestamptz - interval '1 minute'
	  AND (props->>'finishedAt')::timestamptz >= $2::timestamptz`

// spendLedger is one repository's spend bookkeeping in this process.
type spendLedger struct {
	// fill serializes reads of the cap and the snapshot, so concurrent
	// checks make one query rather than one each.
	fill sync.Mutex

	mu sync.Mutex
	// repo is the last read of the repository cap, sums the last snapshot
	// of the window; nil until a check needs one and after a pass expires
	// them.
	repo *spendRepoCap
	sums *spendSums
	// runs is every root run going now, and the ones that ended lately whose
	// cost a snapshot may not hold yet (spendRun).
	runs []*spendRun
	// held is the triggers the dispatcher holds now, so a hold is logged
	// when it starts and when it ends, never on every pass.
	held map[string]bool
	// finishing, set only by tests, runs as a root run ends: after its
	// settle committed or failed, before a run no settle committed is
	// marked lost.
	finishing func(*spendRun)
	// now, set only by tests, is the ledger's clock: the window's cutoff,
	// every stamp and every age read it. nil is the wall clock.
	now func() time.Time
}

// clock is the time the ledger reads.
func (s *spendLedger) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// spendRunState is where one root run's charges are counted.
type spendRunState int

const (
	// spendRunning: the loop runs and its charges are on no row yet.
	spendRunning spendRunState = iota
	// spendCommitted: a settle committed the thread row with `lifetime`.
	spendCommitted
	// spendLost: the run ended and no settle committed its charges (a
	// failed write, a panic, a canceled context, a deleted thread); no row
	// will carry them, so they count until they leave the window.
	spendLost
)

// spendRun is one root run's spend, keyed by the root thread it settles on.
type spendRun struct {
	agent  string
	thread string
	// base is the thread's costUSD when the run opened it: 0 for a new
	// thread, the stored total for a continued one.
	base float64
	// charge is what the run's chain charged, in USD.
	charge float64
	state  spendRunState
	// lifetime is the costUSD the committed settle wrote.
	lifetime float64
	// at is when the run left spendRunning: its settle's commit, or its end.
	at time.Time
}

// spendRepoCap is one read of the repository's cap.
type spendRepoCap struct {
	readAt time.Time
	// cents is the setting's value; nil is no cap.
	cents *int
	// bad is the setting's value when it is not a whole number of cents at
	// or above 0; every agent run is held until it is.
	bad string
}

// spendSums is one snapshot of the window's settled spend, in USD.
type spendSums struct {
	readAt time.Time
	// threads is each root thread's agent identity and cost.
	threads map[string]spendThread
	byAgent map[string]float64
	all     float64
}

// spendThread is one root thread in a snapshot.
type spendThread struct {
	agent string
	usd   float64
}

// expire drops both reads, so the next check reads the cap and the snapshot
// again. The dispatcher calls it at the start of every pass.
func (s *spendLedger) expire() {
	s.mu.Lock()
	s.repo, s.sums = nil, nil
	s.mu.Unlock()
}

// expireCap drops the cap's read alone: a hand's entry and a status read
// see a cap written a moment ago, while the snapshot stays one per pass.
func (s *spendLedger) expireCap() {
	s.mu.Lock()
	s.repo = nil
	s.mu.Unlock()
}

// open starts counting one root run on its thread.
func (s *spendLedger) open(agent, thread string, base float64) *spendRun {
	r := &spendRun{agent: agent, thread: thread, base: base}
	s.mu.Lock()
	s.runs = append(s.runs, r)
	s.mu.Unlock()
	return r
}

// charge adds one completion's cost to a running root run.
func (s *spendLedger) charge(r *spendRun, usd float64) {
	if r == nil || usd <= 0 {
		return
	}
	s.mu.Lock()
	r.charge += usd
	s.mu.Unlock()
}

// commit records that a settle committed the run's thread row with
// lifetime as its costUSD. The settle calls it the moment its transaction
// commits, before anything else the loop does on its way out, so a check in
// between reads the thread's whole cost and not the run's charge alone.
func (s *spendLedger) commit(r *spendRun, lifetime float64) {
	if r == nil {
		return
	}
	s.mu.Lock()
	r.state, r.lifetime, r.at = spendCommitted, lifetime, s.clock()
	s.mu.Unlock()
}

// finish ends a root run. A run no settle committed is lost: no row will
// carry its charges.
func (s *spendLedger) finish(r *spendRun) {
	if s.finishing != nil {
		s.finishing(r)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.state == spendRunning {
		r.state, r.at = spendLost, s.clock()
	}
	s.prune(s.clock())
}

// prune drops what no check needs any longer, each run by its own stamp: a
// committed run older than twice the snapshot's age limit, because every
// snapshot in use was read after it committed; a lost run once it leaves the
// window. Every spend read prunes, not only a run's end, so an idle
// repository's cap is not held by an entry nothing ends. Caller holds mu.
func (s *spendLedger) prune(now time.Time) {
	keep := s.runs[:0]
	for _, r := range s.runs {
		switch {
		case r.state == spendCommitted && now.Sub(r.at) >= 2*spendViewMaxAge:
		case r.state == spendLost && (now.Sub(r.at) >= spendWindow || r.charge <= 0):
		default:
			keep = append(keep, r)
		}
	}
	clear(s.runs[len(keep):])
	s.runs = keep
}

// unsettled is what the snapshot does not hold, per agent identity and in
// all, reconciled by thread. Per thread it wants the most any run on it
// says the thread holds: a committed run's lifetime; a running run's base
// plus its charge where the thread's earlier cost is inside the window (the
// snapshot holds the thread, or a run committed it), else its charge alone,
// because a continuation of a thread that finished before the window brings
// only its new spend into it until it settles. What the snapshot already
// holds for the thread is not added again. A lost run adds its charge
// whatever the snapshot says, because no row carries it. Caller holds mu.
func (s *spendLedger) unsettled(sums *spendSums, now time.Time) (map[string]float64, float64) {
	type want struct {
		agent     string
		committed float64
		has       bool
		running   []*spendRun
	}
	byThread := map[string]*want{}
	byAgent := map[string]float64{}
	all := 0.0
	for _, r := range s.runs {
		switch {
		case r.state == spendLost:
			if now.Sub(r.at) < spendWindow {
				byAgent[r.agent] += r.charge
				all += r.charge
			}
			continue
		case r.state == spendCommitted && now.Sub(r.at) >= spendWindow:
			// Its settle has left the window, and so has what it cost.
			continue
		}
		w := byThread[r.thread]
		if w == nil {
			w = &want{agent: r.agent}
			byThread[r.thread] = w
		}
		if r.state == spendCommitted {
			w.committed = max(w.committed, r.lifetime)
			w.has = true
		} else {
			w.running = append(w.running, r)
		}
	}
	for id, w := range byThread {
		have, held := sums.threads[id]
		target := w.committed
		for _, r := range w.running {
			if held || w.has {
				target = max(target, r.base+r.charge)
			} else {
				target = max(target, r.charge)
			}
		}
		if d := target - have.usd; d > spendEpsilon {
			byAgent[w.agent] += d
			all += d
		}
	}
	return byAgent, all
}

// spendCap reads the repository's cap: the setting row's value, parsed.
func (ds *dataset) spendCap(ctx context.Context) (*spendRepoCap, error) {
	out := &spendRepoCap{readAt: ds.spend.clock()}
	row, err := ds.loadRowDB(ctx, eref{Kind: kindSetting, ID: spendSettingID})
	if err != nil || row == nil || row.DeletedAt != nil {
		return out, err
	}
	value := propString(row, propSettingValue)
	if value == "" {
		return out, nil
	}
	n, ok := parseSpendCents(value)
	if !ok {
		out.bad = value
		return out, nil
	}
	out.cents = &n
	return out, nil
}

// parseSpendCents reads a cap: a whole number of cents from 0 to the largest
// integer an `int` property holds exactly.
func parseSpendCents(value string) (int, bool) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 || n > vocabulary.MaxAgentSpendCentsPerDay {
		return 0, false
	}
	return int(n), true
}

// spendRepo returns the repository cap, read again when the last read is
// gone or older than spendViewMaxAge.
func (ds *dataset) spendRepo(ctx context.Context) (*spendRepoCap, error) {
	s := &ds.spend
	fresh := func() *spendRepoCap {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.repo != nil && s.clock().Sub(s.repo.readAt) < spendViewMaxAge {
			return s.repo
		}
		return nil
	}
	if r := fresh(); r != nil {
		return r, nil
	}
	s.fill.Lock()
	defer s.fill.Unlock()
	if r := fresh(); r != nil {
		return r, nil
	}
	r, err := ds.spendCap(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.repo = r
	s.mu.Unlock()
	return r, nil
}

// spendSettled returns the window's snapshot, read again when the last one
// is gone or older than spendViewMaxAge.
func (ds *dataset) spendSettled(ctx context.Context) (*spendSums, error) {
	s := &ds.spend
	fresh := func() *spendSums {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.sums != nil && s.clock().Sub(s.sums.readAt) < spendViewMaxAge {
			return s.sums
		}
		return nil
	}
	if r := fresh(); r != nil {
		return r, nil
	}
	s.fill.Lock()
	defer s.fill.Unlock()
	if r := fresh(); r != nil {
		return r, nil
	}
	sums, err := ds.querySpendSums(ctx, s.clock().UTC().Add(-spendWindow))
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.sums = sums
	s.mu.Unlock()
	return sums, nil
}

// querySpendSums snapshots the root threads that finished at or after
// since: each one's agent identity and cost, and the sums per agent and in
// all.
func (ds *dataset) querySpendSums(ctx context.Context, since time.Time) (*spendSums, error) {
	readAt := ds.spend.clock()
	rows, err := ds.db.QueryContext(ctx, spendSnapshotQuery, typeThread, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := &spendSums{readAt: readAt, threads: map[string]spendThread{}, byAgent: map[string]float64{}}
	for rows.Next() {
		var id string
		var agent sql.NullString
		var usd float64
		if err := rows.Scan(&id, &agent, &usd); err != nil {
			return nil, err
		}
		ident := strings.TrimPrefix(agent.String, kindAgent+"/")
		out.threads[id] = spendThread{agent: ident, usd: usd}
		out.byAgent[ident] += usd
		out.all += usd
	}
	return out, rows.Err()
}

// spendOf is what ag's root runs and the repository's root runs have spent
// in the window, in USD: the snapshot, reconciled with what it does not hold
// yet (spendLedger.unsettled).
func (ds *dataset) spendOf(ctx context.Context, ag *vocabulary.Agent) (agentUSD, allUSD float64, err error) {
	sums, err := ds.spendSettled(ctx)
	if err != nil {
		return 0, 0, err
	}
	ident := ag.Identity()
	s := &ds.spend
	s.mu.Lock()
	now := s.clock()
	s.prune(now)
	byAgent, all := s.unsettled(sums, now)
	s.mu.Unlock()
	return sums.byAgent[ident] + byAgent[ident], sums.all + all, nil
}

// spendHold says why a root run of ag is held, or "" when it may run: the
// agent's own cap first, then the repository's, compared in cents against
// the spend, so a zero cap holds every run.
func (ds *dataset) spendHold(ctx context.Context, ag *vocabulary.Agent) (string, error) {
	agentCap := ag.Budgets.SpendCentsPerDay
	repo, err := ds.spendRepo(ctx)
	if err != nil {
		return "", err
	}
	if repo.bad != "" {
		return fmt.Sprintf("spend cap unreadable: setting %s holds %q, which is not a whole number of cents, so every agent run is held until it is",
			spendSettingID, repo.bad), nil
	}
	if agentCap == nil && repo.cents == nil {
		return "", nil
	}
	agentUSD, allUSD, err := ds.spendOf(ctx, ag)
	if err != nil {
		return "", err
	}
	if agentCap != nil && reachedCap(agentUSD, *agentCap) {
		return fmt.Sprintf("spend cap reached: agent %s spent %d of its %d cents in the last 24 hours (budgets.spendCentsPerDay)",
			ag.Identity(), spentCents(agentUSD), *agentCap), nil
	}
	if repo.cents != nil && reachedCap(allUSD, *repo.cents) {
		return fmt.Sprintf("spend cap reached: the repository spent %d of its %d cents in the last 24 hours (setting %s)",
			spentCents(allUSD), *repo.cents, spendSettingID), nil
	}
	return "", nil
}

// reachedCap compares a spend in USD against a cap in cents. The epsilon
// keeps a float sum that should equal the cap from reading one hair under.
func reachedCap(usd float64, capCents int) bool {
	return usd*100 >= float64(capCents)-spendEpsilon
}

// spentCents rounds a spend up to whole cents, so a held run never reads as
// spending less than its cap. The epsilon absorbs a float's last bit
// (0.07 * 100 is 7.000000000000001).
func spentCents(usd float64) int64 {
	return int64(math.Ceil(usd*100 - spendEpsilon))
}

// spendRefusal is the error a refused root run answers: ErrGuard, carrying
// the hold's text.
func spendRefusal(text string) error {
	return fmt.Errorf("%w: %s", errSpendHeld, text)
}

// refuseAtSpendCap checks a root run of ag that no dispatcher walk admitted:
// a chat, a direct call, a hand's run, wake or retry. It reads the
// repository cap again, so a cap raised a moment ago admits the run. It
// answers the context the run goes on with, marked admitted, or the refusal.
func (ds *dataset) refuseAtSpendCap(ctx context.Context, ag *vocabulary.Agent) (context.Context, error) {
	ds.spend.expireCap()
	text, err := ds.spendHold(ctx, ag)
	if err != nil {
		return ctx, err
	}
	if text != "" {
		return ctx, spendRefusal(text)
	}
	return withSpendAdmitted(ctx, ag), nil
}

// holdAgentDelivery is the dispatcher's check before it claims one delivery
// of an agent trigger. Held means the walk stops where it stands: nothing is
// claimed, so the cursor or fire state does not move, and the next pass asks
// again. A trigger whose callable is not an agent is never held here; a
// function that runs an agent meets the refusal inside its body.
func (ds *dataset) holdAgentDelivery(ctx context.Context, tr *trigger) (context.Context, bool, error) {
	if tr.Agent == nil {
		return ctx, false, nil
	}
	text, err := ds.spendHold(ctx, tr.Agent)
	if err != nil {
		return ctx, false, err
	}
	s := &ds.spend
	s.mu.Lock()
	was := s.held[tr.ID]
	if text != "" {
		if s.held == nil {
			s.held = map[string]bool{}
		}
		s.held[tr.ID] = true
	} else {
		delete(s.held, tr.ID)
	}
	s.mu.Unlock()
	switch {
	case text != "" && !was:
		ds.svc.log.Warn("substrate: agent trigger held at a spend cap, its deliveries wait",
			"trigger", logSafeID(tr.ID), "callable", tr.CallableID, "held", text)
	case text == "" && was:
		ds.svc.log.Info("substrate: agent trigger under its spend cap again, its deliveries resume",
			"trigger", logSafeID(tr.ID), "callable", tr.CallableID)
	}
	if text != "" {
		return ctx, true, nil
	}
	return withSpendAdmitted(ctx, tr.Agent), false, nil
}

// spendAdmittedKey marks a context whose root run of one agent was already
// checked against the caps, by the dispatcher's walk or a hand's entry: the
// loop entry does not check it twice, because a refusal there comes after
// the delivery was claimed and would park it.
type spendAdmittedKey struct{}

func withSpendAdmitted(ctx context.Context, ag *vocabulary.Agent) context.Context {
	return context.WithValue(ctx, spendAdmittedKey{}, ag.Identity())
}

func spendAdmitted(ctx context.Context, ag *vocabulary.Agent) bool {
	id, _ := ctx.Value(spendAdmittedKey{}).(string)
	return id == ag.Identity()
}
