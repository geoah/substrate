package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	rrule "github.com/teambition/rrule-go"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Triggers are DATA RECORDS (core.substrate.reamde.dev): one source bound to one
// callable, console-editable, changelog-visible. The engine's half is here —
// write-time admission (a trigger row that cannot dispatch never lands), the
// parse the dispatcher runs on, and the compiled-guard cache. The cursors,
// parked failures and schedule fire state stay engine tables (0007), never
// records, so no `*` subscription can match the bookkeeping.

const (
	typeTrigger = "core.substrate.reamde.dev/trigger"
	typeRun     = "core.substrate.reamde.dev/run"
)

// The callable kinds a trigger may name. The property is an open string on
// purpose, but a kind this build cannot dispatch is refused at write time
// rather than parked forever. `function` runs in the shared runner;
// `agent` runs the LLM loop (agentloop.go) — same sources, same cursor,
// same retries and park.
const (
	callableKindFunction = "function"
	callableKindAgent    = "agent"
)

// trigger is one parsed trigger record, dispatch-ready.
type trigger struct {
	ID      string
	Enabled bool

	// Exactly one source arm is set.
	Record   *recordSource
	Schedule *scheduleSource
	Webhook  bool

	CallableKind string
	CallableID   string
	// Exactly one of Callable/Agent is set once resolution succeeds; both
	// nil when the id no longer resolves (the callable was uninstalled after
	// the trigger was written) — the dispatcher skips the trigger, loudly,
	// without moving its cursor.
	Callable *vocabulary.Function
	Agent    *vocabulary.Agent
}

// runnable reports whether the trigger's callable resolved.
func (t *trigger) runnable() bool { return t.Callable != nil || t.Agent != nil }

// callableActor is the attribution the callable's writes carry — the
// self-exclusion key and the effects actor.
func (t *trigger) callableActor() string {
	switch {
	case t.Agent != nil:
		return t.Agent.Actor()
	case t.Callable != nil:
		return t.Callable.Actor()
	default:
		return ""
	}
}

// resolveCallable fills the resolved half from the registry, kind-aware.
func (t *trigger) resolveCallable(reg *vocabulary.Registry) {
	switch t.CallableKind {
	case callableKindAgent:
		if ag, err := reg.ResolveAgent(t.CallableID); err == nil {
			t.Agent = ag
		}
	default:
		if fn, err := reg.ResolveFunction(t.CallableID); err == nil {
			t.Callable = fn
		}
	}
	t.resolveKinds(reg)
}

// resolveKinds canonicalizes an record source's kind patterns against the
// repository's own vocabulary — the same resolve-at-the-gate the runner's
// reads allowlist gets. A kind has two spellings, `task` and
// `tasks.substrate.reamde.dev/task`, and the changelog rows the matcher compares against
// carry the IDENTITY, so a source declared in the bare spelling would validate
// and then never fire. A glob is left alone (it is not a reference), and so is
// a name the registry does not know: it matches nothing, which is exactly what
// an undeclared kind should do.
func (t *trigger) resolveKinds(reg *vocabulary.Registry) {
	if t.Record == nil {
		return
	}
	for i, pat := range t.Record.Kinds {
		if pat == "*" || strings.HasSuffix(pat, "/*") {
			continue
		}
		if ty, err := reg.Resolve(pat); err == nil && ty != nil {
			t.Record.Kinds[i] = ty.Identity
		}
	}
}

// recordSource is a changelog subscription: type globs, ops, an optional CEL
// guard and opt-in coalescing.
type recordSource struct {
	Kinds    []string
	Ops      []string
	When     string
	Coalesce bool
	// program is the compiled guard, filled from the dataset's cache at
	// load; nil when no guard is declared.
	program cel.Program
}

// matches reports whether a change to the given type identity with the given
// op fires this source.
func (s *recordSource) matches(typeIdent, op string) bool {
	okOp := false
	for _, o := range s.Ops {
		if o == op {
			okOp = true
			break
		}
	}
	if !okOp {
		return false
	}
	for _, pat := range s.Kinds {
		if vocabulary.MatchTypeGlob(pat, typeIdent) {
			return true
		}
	}
	return false
}

// scheduleSource is an RRULE recurrence. Occurrences anchor at StartsAt when
// declared, else at the trigger record's creation — a STABLE anchor, so fire
// ids are stable across dispatcher passes and restarts.
type scheduleSource struct {
	Recurrence string
	Timezone   string
	StartsAt   *time.Time
}

// rule compiles the recurrence against the anchor. createdAt is the trigger
// row's creation time, the anchor of last resort.
func (s *scheduleSource) rule(createdAt time.Time) (*rrule.RRule, error) {
	loc := time.UTC
	if s.Timezone != "" {
		l, err := time.LoadLocation(s.Timezone)
		if err != nil {
			return nil, fmt.Errorf("timezone %q: %w", s.Timezone, err)
		}
		loc = l
	}
	opt, err := rrule.StrToROption(s.Recurrence)
	if err != nil {
		return nil, fmt.Errorf("recurrence: %w", err)
	}
	anchor := createdAt
	if s.StartsAt != nil {
		anchor = *s.StartsAt
	}
	opt.Dtstart = anchor.In(loc)
	return rrule.NewRRule(*opt)
}

// dueFire computes the newest occurrence in (after, now]: missed ticks
// coalesce to exactly one fire. The zero time means nothing is due.
func (s *scheduleSource) dueFire(createdAt, after, now time.Time) (time.Time, error) {
	r, err := s.rule(createdAt)
	if err != nil {
		return time.Time{}, err
	}
	due := r.Between(after, now, true)
	for i := len(due) - 1; i >= 0; i-- {
		if due[i].After(after) {
			return due[i].UTC(), nil
		}
	}
	return time.Time{}, nil
}

// fireID is the stable identity of one schedule occurrence.
func fireID(at time.Time) string { return at.UTC().Format(time.RFC3339) }

// --- parsing and admission ------------------------------------------------------

// triggerSourceKeys is the source's key set: the three arms, plus the
// DISCRIMINATOR that names which of them this source is. The arm alone has
// always said it, so `kind` is optional — but a `kind` that disagrees with the
// arm present is refused rather than ignored, since one of the two would have to
// be the lie a reader believes.
var triggerSourceKeys = map[string]bool{
	"kind": true, "record": true, "schedule": true, "webhook": true,
}

// triggerSourceArms are the source keys that carry an arm, discriminator aside.
var triggerSourceArms = map[string]bool{"record": true, "schedule": true, "webhook": true}

var triggerRecordKeys = map[string]bool{"kinds": true, "ops": true, "when": true, "coalesce": true}

var triggerScheduleKeys = map[string]bool{"recurrence": true, "timezone": true, "startsAt": true}

// parseTrigger reads one trigger record's properties into the dispatch
// shape. Pure validation — the callable resolves separately, because the
// registry may legitimately have moved since the row was written.
func parseTrigger(id string, props map[string]any) (*trigger, error) {
	t := &trigger{ID: id, Enabled: true}
	if raw, has := props["enabled"]; has {
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("enabled: a boolean, got %T", raw)
		}
		t.Enabled = b
	}

	// callable is a reference: a {kind, id} record reference whose
	// kind is core.substrate.reamde.dev/function or core.substrate.reamde.dev/agent. Dispatch keys on
	// the LOCAL name of that kind — `function` or `agent` — beside the id.
	callable, ok := props["callable"].(map[string]any)
	if !ok || len(callable) == 0 {
		return nil, fmt.Errorf("callable is required: a {kind, id} reference to a function or agent")
	}
	callableRef, _ := callable["kind"].(string)
	t.CallableKind = vocabulary.KindName(callableRef)
	t.CallableID, _ = callable["id"].(string)
	if t.CallableKind == "" || t.CallableID == "" {
		return nil, fmt.Errorf("callable needs a kind and an id")
	}
	if t.CallableKind != callableKindFunction && t.CallableKind != callableKindAgent {
		return nil, fmt.Errorf("callable kind %q is not dispatchable — core.substrate.reamde.dev/function or core.substrate.reamde.dev/agent", callableRef)
	}

	source, ok := props["source"].(map[string]any)
	if !ok || len(source) == 0 {
		return nil, fmt.Errorf("source is required: exactly one of record, schedule, webhook")
	}
	var arms []string
	for k := range source {
		if !triggerSourceKeys[k] {
			return nil, fmt.Errorf("source: unknown key %q — kind, record, schedule or webhook", k)
		}
		if triggerSourceArms[k] {
			arms = append(arms, k)
		}
	}
	if len(arms) != 1 {
		sort.Strings(arms)
		return nil, fmt.Errorf("source carries exactly one arm, got %s", strings.Join(arms, "+"))
	}
	if declared, has := source["kind"]; has {
		if s, _ := declared.(string); s != arms[0] {
			return nil, fmt.Errorf("source.kind says %v and the arm present is %q — one source, named once", declared, arms[0])
		}
	}

	switch arms[0] {
	case "record":
		src, err := parseRecordSource(source["record"])
		if err != nil {
			return nil, err
		}
		t.Record = src
	case "schedule":
		src, err := parseScheduleSource(source["schedule"])
		if err != nil {
			return nil, err
		}
		t.Schedule = src
	case "webhook":
		m, isMap := source["webhook"].(map[string]any)
		if source["webhook"] != nil && !isMap {
			return nil, fmt.Errorf("source.webhook: an empty map")
		}
		if len(m) != 0 {
			return nil, fmt.Errorf("source.webhook declares nothing yet — an empty map")
		}
		t.Webhook = true
	}
	return t, nil
}

func parseRecordSource(raw any) (*recordSource, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("source.record: a map with types/ops/when/coalesce")
	}
	for k := range m {
		if !triggerRecordKeys[k] {
			return nil, fmt.Errorf("source.record: unknown key %q", k)
		}
	}
	src := &recordSource{}
	types, _ := m["kinds"].([]any)
	for i, tv := range types {
		pat := fmt.Sprint(tv)
		if !vocabulary.ValidTypeGlob(pat) {
			return nil, fmt.Errorf("source.record.kinds[%d]: %q is not a kind reference, `<authority>/*` or `*`", i, pat)
		}
		src.Kinds = append(src.Kinds, pat)
	}
	if len(src.Kinds) == 0 {
		return nil, fmt.Errorf("source.record.kinds is required — a trigger watches something")
	}
	ops, _ := m["ops"].([]any)
	for i, ov := range ops {
		op := fmt.Sprint(ov)
		if !vocabulary.ValidFunctionOp(op) {
			return nil, fmt.Errorf("source.record.ops[%d]: %q is not an op — create, update or delete", i, op)
		}
		src.Ops = append(src.Ops, op)
	}
	if len(src.Ops) == 0 {
		src.Ops = []string{vocabulary.FunctionOpCreate, vocabulary.FunctionOpUpdate, vocabulary.FunctionOpDelete}
	}
	if raw, has := m["when"]; has {
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("source.record.when: a CEL expression string")
		}
		src.When = s
	}
	if raw, has := m["coalesce"]; has {
		b, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("source.record.coalesce: a boolean")
		}
		src.Coalesce = b
	}
	return src, nil
}

func parseScheduleSource(raw any) (*scheduleSource, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("source.schedule: a map with recurrence/timezone/startsAt")
	}
	for k := range m {
		if !triggerScheduleKeys[k] {
			return nil, fmt.Errorf("source.schedule: unknown key %q", k)
		}
	}
	src := &scheduleSource{}
	src.Recurrence, _ = m["recurrence"].(string)
	if src.Recurrence == "" {
		return nil, fmt.Errorf("source.schedule.recurrence is required — an RRULE")
	}
	src.Timezone, _ = m["timezone"].(string)
	if raw, has := m["startsAt"]; has {
		s, _ := raw.(string)
		ts, err := parseTime(s)
		if err != nil {
			return nil, fmt.Errorf("source.schedule.startsAt: %w", err)
		}
		ts = ts.UTC()
		src.StartsAt = &ts
	}
	// Compile now so a bad recurrence or timezone is a write error, not a
	// dispatcher log.
	if _, err := src.rule(time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("source.schedule: %w", err)
	}
	return src, nil
}

// validateTriggerRow is the write-time admission every trigger put/patch
// runs through: the parse must succeed, the guard must compile and the
// callable must resolve — a trigger row that cannot dispatch never lands.
// The callable resolves against the given registry — the live one for
// ordinary writes, the CANDIDATE for a bundle installation whose triggers may
// name functions the same batch installs. The callable check is skipped on
// INTERNAL writes: a substrate path may write a trigger before the installed
// registry has rebuilt, and a system write vouching for a callable is the
// dispatcher's skip-loudly problem, never a wedge.
func (ds *dataset) validateTriggerRow(reg *vocabulary.Registry, id string, props map[string]any, checkCallable bool) error {
	t, err := parseTrigger(id, props)
	if err != nil {
		return fmt.Errorf("%w: trigger: %w", substrate.ErrValidation, err)
	}
	if t.Record != nil && t.Record.When != "" {
		if _, err := ds.whenProgram(t.Record.When); err != nil {
			return fmt.Errorf("%w: trigger source.record.when: %w", substrate.ErrValidation, err)
		}
	}
	if checkCallable {
		switch t.CallableKind {
		case callableKindAgent:
			if _, err := reg.ResolveAgent(t.CallableID); err != nil {
				return fmt.Errorf("%w: trigger callable: %w", substrate.ErrValidation, err)
			}
		default:
			fn, err := reg.ResolveFunction(t.CallableID)
			if err != nil {
				return fmt.Errorf("%w: trigger callable: %w", substrate.ErrValidation, err)
			}
			// A HOST FUNCTION IS NOT A DELIVERY TARGET. The engine runs one under
			// the grants of whoever called it, and a delivery has no caller to
			// borrow from: nothing would scope `query`'s reads or `mutate`'s
			// writes. The shape that works is an agent carrying the tool, so the
			// refusal names it rather than leaving the row to park forever.
			if fn.IsHost() {
				return fmt.Errorf("%w: trigger callable: %s is a built-in — the engine runs it under a CALLER's grants, and a delivery has none: declare an agent carrying it as a tool and target the agent",
					substrate.ErrValidation, fn.Identity())
			}
			ds.warnDiscardedOutput(t, fn)
		}
	}
	return nil
}

// warnDiscardedOutput says so when a trigger names a callable whose work cannot
// leave it. A delivery DISCARDS the output — only the effects are applied — so a
// body that declares no `emit:`, no `call:` and no `network:` can change nothing
// anywhere: the row admits (a pure function is legal, and `emit:` is optional
// now), but firing it forever is almost certainly not what was meant.
//
// A warning, never a refusal: the declaration may be mid-edit, and a trigger the
// engine refused would have to be re-created rather than fixed.
func (ds *dataset) warnDiscardedOutput(t *trigger, fn *vocabulary.Function) {
	if len(fn.Caps.Emit) > 0 || len(fn.Caps.Call) > 0 || len(fn.Caps.Network) > 0 {
		return
	}
	ds.svc.log.Warn("substrate: trigger fires a function whose output is discarded — it declares no emit, no call and no network, so a delivery can change nothing; call it instead, or give it the grant it needs",
		"repository", ds.Repository().Name, "trigger", t.ID, "function", fn.Identity())
}

// --- loading ---------------------------------------------------------------------

// loadTriggers reads every live trigger record, parsed and resolved, ordered
// by id. A row that no longer parses or resolves is carried with its error
// so callers can report it instead of silently skipping.
type loadedTrigger struct {
	*trigger
	CreatedAt time.Time
	Err       error
}

func (ds *dataset) loadTriggers(ctx context.Context) ([]loadedTrigger, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, props, created_at FROM records
		WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`, typeTrigger)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	// One bundle-lifecycle read serves the whole pass: a blocked bundle's
	// callables load unresolved, so its triggers skip and cursors stand still.
	states, err := ds.bundleStates(ctx)
	if err != nil {
		return nil, err
	}
	var out []loadedTrigger
	for rows.Next() {
		var id string
		var raw []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &raw, &createdAt); err != nil {
			return nil, err
		}
		var props map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &props); err != nil {
				out = append(out, loadedTrigger{trigger: &trigger{ID: id}, Err: err})
				continue
			}
		}
		lt := loadedTrigger{CreatedAt: createdAt.UTC()}
		t, err := parseTrigger(id, props)
		if err != nil {
			lt.trigger, lt.Err = &trigger{ID: id}, err
			out = append(out, lt)
			continue
		}
		lt.trigger = t
		if t.Record != nil && t.Record.When != "" {
			prog, err := ds.whenProgram(t.Record.When)
			if err != nil {
				lt.Err = err
				out = append(out, lt)
				continue
			}
			t.Record.program = prog
		}
		t.resolveCallable(ds.registry())
		ds.blockBundledCallable(t, states)
		out = append(out, lt)
	}
	return out, rows.Err()
}

// triggerByID loads and resolves one trigger record.
func (ds *dataset) triggerByID(ctx context.Context, id string) (*trigger, time.Time, error) {
	row, err := ds.loadRowDB(ctx, eref{Kind: typeTrigger, ID: id})
	if err != nil {
		return nil, time.Time{}, err
	}
	if row == nil || row.DeletedAt != nil || row.Kind != typeTrigger {
		return nil, time.Time{}, fmt.Errorf("%w: trigger %s", substrate.ErrNotFound, id)
	}
	t, err := parseTrigger(id, row.Props)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: trigger %s: %w", substrate.ErrValidation, id, err)
	}
	if t.Record != nil && t.Record.When != "" {
		prog, err := ds.whenProgram(t.Record.When)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("%w: trigger %s: when: %w", substrate.ErrValidation, id, err)
		}
		t.Record.program = prog
	}
	t.resolveCallable(ds.registry())
	states, err := ds.bundleStates(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	ds.blockBundledCallable(t, states)
	return t, row.CreatedAt.UTC(), nil
}

// --- the compiled-guard cache ----------------------------------------------------

// whenPrograms caches compiled `when:` guards by source text, per dataset:
// triggers re-parse every dispatcher pass, and CEL compilation is the only
// expensive part. Bounded by eviction-on-size — guard sources are small and
// few, this is belt and braces.
type whenCache struct {
	mu       sync.Mutex
	programs map[string]cel.Program
}

const whenCacheMax = 512

func (ds *dataset) whenProgram(src string) (cel.Program, error) {
	c := &ds.whens
	c.mu.Lock()
	if p, ok := c.programs[src]; ok {
		c.mu.Unlock()
		return p, nil
	}
	c.mu.Unlock()
	p, err := vocabulary.CompileWhen(src)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.programs == nil || len(c.programs) >= whenCacheMax {
		c.programs = map[string]cel.Program{}
	}
	c.programs[src] = p
	c.mu.Unlock()
	return p, nil
}

// --- bookkeeping hooks (the write path calls these) ------------------------------

// initTriggerBookkeeping runs inside the transaction that creates (or
// restores) a trigger row: the cursor initializes AT the creation's own
// changelog seq — the trigger reacts to what happens next, and a write
// between creation and the first dispatch is never skipped — and a schedule
// source's fire state initializes at now, so the first fire is the next
// occurrence. One exception beats the creation seq: a DEFAULT trigger
// (`on-<callable identity>`) whose callable still owns a pre-wave-1 cursor
// ADOPTS that position and its parked failures atomically, so a subscription
// that lived on the function itself — dropped whole by the run-arm cleanup
// or a stored-blob promotion before the trigger existed — resumes exactly
// where it stood instead of restarting at head.
func (t *txn) initTriggerBookkeeping(id string, props map[string]any) error {
	seq := t.maxSeq
	if legacy, ok := t.adoptableLegacyCursor(id, props); ok {
		var old sql.NullInt64
		err := t.row(`SELECT seq FROM trigger_cursors WHERE trigger_id = $1`, legacy).Scan(&old)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// Nothing to adopt: the trigger starts at its own creation.
		case err != nil:
			return err
		default:
			seq = old.Int64
			if _, err := t.exec(`DELETE FROM trigger_cursors WHERE trigger_id = $1`, legacy); err != nil {
				return err
			}
			if _, err := t.exec(`
				UPDATE trigger_failures SET trigger_id = $2 WHERE trigger_id = $1`,
				legacy, id); err != nil {
				return err
			}
		}
	}
	if _, err := t.exec(`
		INSERT INTO trigger_cursors (trigger_id, seq, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT (repository, trigger_id) DO UPDATE SET seq = EXCLUDED.seq, updated_at = EXCLUDED.updated_at`,
		id, seq, t.now); err != nil {
		return err
	}
	if src, _ := props["source"].(map[string]any); src != nil {
		if _, has := src["schedule"]; has {
			if _, err := t.exec(`
				INSERT INTO trigger_schedule (trigger_id, fired_at, updated_at) VALUES ($1, $2, $2)
				ON CONFLICT (repository, trigger_id) DO UPDATE SET fired_at = EXCLUDED.fired_at, updated_at = EXCLUDED.updated_at`,
				id, t.now); err != nil {
				return err
			}
		}
	}
	return nil
}

// adoptableLegacyCursor reports the callable identity whose legacy delivery
// state this trigger may adopt: only the default trigger of a FUNCTION
// callable (`on-<identity>`) qualifies, and never while a live TRIGGER record
// answers to that identity — a cursor keyed by another live trigger's id is
// that trigger's, not a pre-wave-1 leftover.
func (t *txn) adoptableLegacyCursor(id string, props map[string]any) (string, bool) {
	callable, _ := props["callable"].(map[string]any)
	cid, _ := callable["id"].(string)
	if cid == "" || id != "on-"+cid {
		return "", false
	}
	var one int
	err := t.row(`SELECT 1 FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
		typeTrigger, cid).Scan(&one)
	if err == nil {
		return "", false
	}
	return cid, true
}

// dropTriggerBookkeeping runs inside the transaction that tombstones a
// trigger row: cursor, parked failures, fire state and any in-flight paged
// cursors die with it. A restore re-initializes at its own
// moment.
func (t *txn) dropTriggerBookkeeping(id string) error {
	for _, q := range []string{
		`DELETE FROM trigger_cursors WHERE trigger_id = $1`,
		`DELETE FROM trigger_failures WHERE trigger_id = $1`,
		`DELETE FROM trigger_schedule WHERE trigger_id = $1`,
		`DELETE FROM paged_cursors WHERE trigger_id = $1`,
	} {
		if _, err := t.exec(q, id); err != nil {
			return err
		}
	}
	return nil
}
