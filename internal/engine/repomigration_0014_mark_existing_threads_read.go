package engine

// Repository migration 14: every thread a chat list shows is given its
// newest message and what waits on a person in it, and every person a mark
// that reads through it (decision 0313).
//
// A thread from before this release settled without `lastMessage`, and
// nobody has a mark of it. Left so, every one of them would read as never
// opened the day a client starts telling read from unread by the mark. So
// each live, settled thread whose `mode` a chat list shows (every mode but
// `subagent` and `judge`) and which holds no `lastMessage` takes the newest
// message it holds, and each live person of the repository takes a mark of
// it that reads through that message. A thread that is still `running` is
// left for the open's recovery, which settles it through the write path and
// stamps it there, with no mark: the person has not seen how it ended.
//
// The count is what the three kinds say now, written where the thread's
// differs, so a second run corrects nothing a write has kept right.
//
// A thread that already holds `lastMessage` settled under this release or
// was migrated: it is given no mark, so a run over a repository imported
// into a fresh database does not read for a person what they never opened.
// Every write is the system's ordinary record write.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// agentModeJudge is the mode of a policy's judge (judge.go), which, like a
// sub-agent's thread, is part of another run and no conversation of its own.
const agentModeJudge = "judge"

func migrateMarkExistingThreadsRead(t *txn) (string, error) {
	people, err := t.livePersonActorIDs()
	if err != nil {
		return "", err
	}
	threads, err := t.unmarkedThreads()
	if err != nil {
		return "", err
	}
	waiting, err := t.waitingByThread()
	if err != nil {
		return "", err
	}
	newest := map[string]string{}
	if len(threads) > 0 {
		if newest, err = t.newestMessageByThread(); err != nil {
			return "", err
		}
	}
	// One pass over the threads that need their newest message, then the
	// threads whose count alone is off.
	stamped, counted, marks := 0, 0, 0
	seen := map[string]bool{}
	for _, th := range threads {
		seen[th.id] = true
		set := map[string]any{}
		last := newest[th.id]
		if last != "" {
			set[propThreadLastMessage] = referenceValueOf(vocabulary.RecordPath(typeMessage, last))
			stamped++
		}
		if n := waiting[th.id]; n != th.waiting {
			set[propThreadWaiting] = n
			counted++
		}
		if len(set) > 0 {
			if err := t.migrateSet(eref{Kind: typeThread, ID: th.id}, set); err != nil {
				return "", err
			}
		}
		if last == "" {
			continue
		}
		for _, person := range people {
			id := threadMarkID(th.id, person)
			if !vocabulary.ValidID(id) {
				continue
			}
			ref := eref{Kind: typeThreadMark, ID: id}
			if held, err := t.loadRow(ref, false); err != nil {
				return "", err
			} else if held != nil {
				continue
			}
			if err := t.migrateRowAs(substrate.ActorSystem, ref, map[string]any{
				propMarkThread:      referenceValueOf(vocabulary.RecordPath(typeThread, th.id)),
				propMarkReadBy:      referenceValueOf(vocabulary.RecordPath(kindActor, person)),
				propMarkReadThrough: referenceValueOf(vocabulary.RecordPath(typeMessage, last)),
				propMarkReadAt:      t.now.Format(time.RFC3339Nano),
			}, true, th.title); err != nil {
				return "", err
			}
			marks++
		}
	}
	held, err := t.threadWaitingCounts()
	if err != nil {
		return "", err
	}
	for _, th := range held {
		if seen[th.id] || waiting[th.id] == th.waiting {
			continue
		}
		if err := t.migrateSet(eref{Kind: typeThread, ID: th.id}, map[string]any{propThreadWaiting: waiting[th.id]}); err != nil {
			return "", err
		}
		counted++
	}
	return fmt.Sprintf("gave %d thread(s) their newest message and %d a count of what waits, and wrote %d mark(s) for %d person(s)",
		stamped, counted, marks, len(people)), nil
}

// migrateSet sets properties on a stored row as the system's ordinary write,
// naming the ones it set.
func (t *txn) migrateSet(ref eref, set map[string]any) error {
	row, err := t.loadRow(ref, true)
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("no %s row %s", ref.Kind, ref.ID)
	}
	before := row.clone()
	for name, v := range set {
		row.Props[name] = v
	}
	res, err := t.foldRow(before, row, false, false)
	if err != nil || !res.changed {
		return err
	}
	return t.appendChange(substrate.ActorSystem, substrate.OpPatch, ref.ID, ref.Kind, map[string]any{"properties": sortedKeys(set)})
}

// livePersonActorIDs are the ids of the actors of the people in the
// repository: live person actors that are not disabled.
func (t *txn) livePersonActorIDs() ([]string, error) {
	rows, err := t.query(`SELECT id FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->>'type' = $2
		AND COALESCE(props->>'disabled', '') <> 'true' ORDER BY id`, kindActor, substrate.ActorTypePerson)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// migratedThread is one thread as the migration reads it.
type migratedThread struct {
	id, title string
	waiting   int
}

// unmarkedThreads are the live, settled threads a chat list shows that hold
// no `lastMessage`, in id order.
func (t *txn) unmarkedThreads() ([]migratedThread, error) {
	return t.migratedThreads(` AND COALESCE(props->>'status', '') <> $2 AND COALESCE(props->>'mode', '') NOT IN ($3, $4)
		AND props->'lastMessage' IS NULL`, threadRunning, agentModeSubagent, agentModeJudge)
}

// threadWaitingCounts are the live threads that hold a count, or that one of
// the three kinds names: the ones whose count a run may have to correct.
func (t *txn) threadWaitingCounts() ([]migratedThread, error) {
	return t.migratedThreads(``)
}

func (t *txn) migratedThreads(where string, args ...any) ([]migratedThread, error) {
	rows, err := t.query(`SELECT id, title, COALESCE(props->>'waitingOnPerson', '') FROM records
		WHERE kind = $1 AND deleted_at IS NULL`+where+` ORDER BY id`, append([]any{typeThread}, args...)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []migratedThread
	for rows.Next() {
		var th migratedThread
		var waiting string
		if err := rows.Scan(&th.id, &th.title, &waiting); err != nil {
			return nil, err
		}
		// A count nothing can read as a number is one to write again.
		if n, err := strconv.Atoi(waiting); err == nil {
			th.waiting = n
		} else if waiting != "" {
			th.waiting = -1
		}
		out = append(out, th)
	}
	return out, rows.Err()
}

// threadIDOfPath is the id of the thread a stored `thread` reference path
// names, "" for anything else.
func threadIDOfPath(path string) string {
	kind, id, ok := vocabulary.SplitRecordPath(path)
	if !ok || kind != typeThread {
		return ""
	}
	return id
}

// newestMessageByThread reads every live message once and keeps, for each
// thread, its newest: the highest `turn`, then the latest written, never a
// `summary` row (threadmarks.go newestMessage). It reads the rows' own
// properties, since the migration runs before the stored vocabulary loads
// and the refs rows may follow an older declaration. `turn` is read as text
// and parsed here: a cast a stored value fails would fail the open.
func (t *txn) newestMessageByThread() (map[string]string, error) {
	rows, err := t.query(`SELECT id, `+referencePathSQL("props", msgRelThread)+`, COALESCE(props->>'turn', ''), created_at
		FROM records WHERE kind = $1 AND deleted_at IS NULL AND COALESCE(props->>'role', '') <> $2`, typeMessage, msgRoleSummary)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	best := map[string]messagePlace{}
	for rows.Next() {
		var id, turn string
		var path *string
		var at time.Time
		if err := rows.Scan(&id, &path, &turn, &at); err != nil {
			return nil, err
		}
		if path == nil {
			continue
		}
		thread := threadIDOfPath(*path)
		if thread == "" {
			continue
		}
		// A row with no ordinal stands before every row that has one.
		place := messagePlace{turn: -1, at: at, id: id}
		if n, err := strconv.ParseFloat(turn, 64); err == nil {
			place.turn = int64(n)
		}
		if held, ok := best[thread]; !ok || held.before(place) {
			best[thread] = place
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(best))
	for thread, place := range best {
		out[thread] = place.id
	}
	return out, nil
}

// waitingByThread counts, per thread, the live records of the three kinds
// that still wait on a person (threadmarks.go waitingKinds), from the rows'
// own columns.
func (t *txn) waitingByThread() (map[string]int, error) {
	rows, err := t.query(`SELECT kind, states, `+referencePathSQL("props", msgRelThread)+`
		FROM records WHERE kind = ANY($1) AND deleted_at IS NULL`, sortedKeys(waitingKinds))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var kind string
		var raw []byte
		var path *string
		if err := rows.Scan(&kind, &raw, &path); err != nil {
			return nil, err
		}
		if path == nil {
			continue
		}
		var states map[string]string
		if err := json.Unmarshal(raw, &states); err != nil {
			continue
		}
		w := waitingKinds[kind]
		if thread := threadIDOfPath(*path); thread != "" && states[w.machine] == w.state {
			out[thread]++
		}
	}
	return out, rows.Err()
}
