package engine

import (
	"math"
	"slices"
	"testing"
	"time"
)

// The ledger reconciles by thread: what a snapshot already holds for a
// thread is not added again, a committed continuation of a thread the
// snapshot lacks adds the thread's lifetime cost, and a run no settle
// committed adds its charge until it leaves the window.
func TestSpendLedgerReconcilesWithTheSnapshotByThread(t *testing.T) {
	now := time.Now()
	const agent = "a.example.com/a/agent"
	snapshot := func(threads map[string]float64) *spendSums {
		s := &spendSums{threads: map[string]spendThread{}, byAgent: map[string]float64{}}
		for id, usd := range threads {
			s.threads[id] = spendThread{agent: agent, usd: usd}
		}
		return s
	}
	for _, tc := range []struct {
		name     string
		run      spendRun
		snapshot map[string]float64
		want     float64
	}{
		{
			"a run going on a new thread adds its charge",
			spendRun{thread: "t", charge: 2},
			nil, 2,
		},
		{
			"a run going on a thread the snapshot holds adds its charge",
			spendRun{thread: "t", base: 5, charge: 2},
			map[string]float64{"t": 5},
			2,
		},
		{
			"a run whose commit the snapshot already holds adds nothing",
			spendRun{thread: "t", base: 5, charge: 2},
			map[string]float64{"t": 7},
			0,
		},
		{
			"a run going on a thread that finished before the window adds its charge alone",
			spendRun{thread: "t", base: 5, charge: 2},
			nil, 2,
		},
		{
			"a committed run the snapshot holds adds nothing",
			spendRun{thread: "t", base: 5, charge: 2, state: spendCommitted, lifetime: 7, at: now},
			map[string]float64{"t": 7},
			0,
		},
		{
			"a committed run the snapshot holds at its old cost adds its charge",
			spendRun{thread: "t", base: 5, charge: 2, state: spendCommitted, lifetime: 7, at: now},
			map[string]float64{"t": 5},
			2,
		},
		{
			"a committed continuation of a thread the snapshot lacks adds the thread's lifetime cost",
			spendRun{thread: "t", base: 5, charge: 2, state: spendCommitted, lifetime: 7, at: now},
			nil, 7,
		},
		{
			"a lost run adds its charge whatever the snapshot holds",
			spendRun{thread: "t", base: 5, charge: 2, state: spendLost, at: now},
			map[string]float64{"t": 5},
			2,
		},
		{
			"a lost run outside the window adds nothing",
			spendRun{thread: "t", charge: 2, state: spendLost, at: now.Add(-spendWindow - time.Minute)},
			nil, 0,
		},
		{
			"a committed run whose settle left the window adds nothing",
			spendRun{thread: "t", charge: 2, state: spendCommitted, lifetime: 7, at: now.Add(-spendWindow - time.Minute)},
			nil, 0,
		},
	} {
		run := tc.run
		run.agent = agent
		l := spendLedger{runs: []*spendRun{&run}}
		byAgent, all := l.unsettled(snapshot(tc.snapshot), now)
		if math.Abs(all-tc.want) > spendEpsilon || math.Abs(byAgent[agent]-tc.want) > spendEpsilon {
			t.Errorf("%s: added %v (agent %v), want %v", tc.name, all, byAgent[agent], tc.want)
		}
	}

	// Two runs on one thread count as the most either says it holds: a
	// committed turn and the next one going on the same thread.
	l := spendLedger{runs: []*spendRun{
		{agent: agent, thread: "t", charge: 1, state: spendCommitted, lifetime: 1, at: now},
		{agent: agent, thread: "t", base: 1, charge: 2},
	}}
	if _, all := l.unsettled(snapshot(nil), now); math.Abs(all-3) > spendEpsilon {
		t.Errorf("a committed turn and the next going: added %v, want 3", all)
	}
}

// prune keeps a committed run only while a snapshot in use may predate its
// commit, and a lost run only while it is inside the window.
func TestSpendLedgerPrunesWhatNoSnapshotNeeds(t *testing.T) {
	now := time.Now()
	l := spendLedger{runs: []*spendRun{
		{thread: "old-commit", state: spendCommitted, at: now.Add(-3 * spendViewMaxAge)},
		{thread: "new-commit", state: spendCommitted, at: now},
		{thread: "old-lost", charge: 1, state: spendLost, at: now.Add(-spendWindow - time.Minute)},
		{thread: "new-lost", charge: 1, state: spendLost, at: now},
		{thread: "free-lost", state: spendLost, at: now},
	}}
	l.now = func() time.Time { return now }
	going := l.open("a", "going", 0)
	committed := l.open("a", "committed", 0)
	l.charge(committed, 1)
	l.commit(committed, 1)
	lost := l.open("a", "lost", 0)
	l.charge(lost, 1)
	l.finish(committed)
	l.finish(lost)
	var kept []string
	for _, r := range l.runs {
		kept = append(kept, r.thread)
	}
	if want := []string{"new-commit", "new-lost", "going", "committed", "lost"}; !slices.Equal(kept, want) {
		t.Fatalf("kept %v, want %v", kept, want)
	}
	if going.state != spendRunning || committed.state != spendCommitted || lost.state != spendLost {
		t.Fatalf("states: going %v, committed %v, lost %v", going.state, committed.state, lost.state)
	}
}
