package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// An agent's pinned context (decision record 0197): the reads its
// `context.pinned` declares, evaluated once when a thread opens and rendered
// into the system prompt after the kinds list. The rendered block is stored on
// the thread row and replayed by every later run of the thread, so the
// prompt's prefix is the same bytes across messages and the provider's cache
// holds. A compaction clears it, and a changed declaration replaces it. The
// owner's instructions to the agent (agentinstructions.go) are read at the
// same moment and stored in the same block, ahead of the pinned records.

const (
	propThreadPinned = "pinnedContext"

	pinnedOpen  = "<pinned_context>"
	pinnedClose = "</pinned_context>"
	pinnedIntro = "Records this agent pins, read when this thread opened. A change made since is not shown here; query the record for its current value."
)

// pinnedDigest names the `context` declaration a snapshot was taken under,
// whether it carries the owner's instructions, and what it was captured
// under (captured, heldto.go capturedBy): whose work it was and what the
// reads' own scope answered for each pinned kind. A changed declaration, a
// snapshot an older binary stored, another person's or app's work, or a
// scope that answers otherwise now is a fresh snapshot rather than a stale
// one (decision 0276: the stored block is a read, held as every read is).
// encoding/json writes map keys sorted, so equal declarations digest equal.
func pinnedDigest(ag *vocabulary.Agent, instructions bool, captured string) string {
	buf, _ := json.Marshal(ag.Definition["context"])
	if instructions {
		buf = append(buf, "\x00instructions"...)
	}
	buf = append(append(buf, "\x00held:"...), captured...)
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:16])
}

// storedPinned answers the block a thread row holds when it was taken under
// the agent's current declaration, and false otherwise.
func storedPinned(ag *vocabulary.Agent, instructions bool, captured string, v any) (string, bool) {
	m, _ := v.(map[string]any)
	if m == nil {
		return "", false
	}
	block, ok := m["block"].(string)
	if !ok || m["digest"] != pinnedDigest(ag, instructions, captured) {
		return "", false
	}
	return block, true
}

// pinnedProps is the thread row's value for a snapshot.
func pinnedProps(ag *vocabulary.Agent, instructions bool, captured, block string, at time.Time) map[string]any {
	return map[string]any{"block": block, "digest": pinnedDigest(ag, instructions, captured), "takenAt": at.Format(time.RFC3339Nano)}
}

// snapshots reports whether this run keeps a snapshot at all: an agent that
// pins records, or one that reads the owner's instructions.
func (l *agentLoop) snapshots() bool {
	return len(l.ag.Context.Pinned) > 0 || l.takesInstructions()
}

// pinnedAnswer is the scope the agent's pinned reads run under, as it reads
// now (heldScope: the agent's reads, and every person's and app's the work
// is held to), and what it answers for each pinned kind, in declaration
// order. The block is read under that scope, and replayed only while the
// same function answers the same: there is no second account of what a
// holder may read.
func (ds *dataset) pinnedAnswer(ctx context.Context, ag *vocabulary.Agent) (queryScope, string, error) {
	scope := queryScope{}
	if ag.Reads != nil {
		scope.kinds = ag.Reads.Kinds
	}
	scope, err := ds.heldScope(ctx, scope)
	if err != nil {
		return scope, "", err
	}
	var b strings.Builder
	for _, pin := range ag.Context.Pinned {
		fmt.Fprintf(&b, "%s=%t;", pin.Kind, ag.Reads != nil && scope.allows(pin.Kind))
	}
	return scope, b.String(), nil
}

// takePinned evaluates the agent's pinned reads and renders the block, with
// what their scope answered (pinnedAnswer), taken once and used by the reads
// themselves. It reports whether every read answered: a snapshot missing a
// read the agent is entitled to is used for this run and never stored, so
// the next run tries again instead of replaying the hole.
func (ds *dataset) takePinned(ctx context.Context, ag *vocabulary.Agent) (string, string, bool) {
	scope, answer, err := ds.pinnedAnswer(ctx, ag)
	if err != nil {
		ds.svc.log.Warn("substrate: an agent's pinned reads were skipped", "agent", ag.Identity(), "error", err)
		return "", "", false
	}
	complete := true
	sections := make([]pinnedSection, 0, len(ag.Context.Pinned))
	for i, pin := range ag.Context.Pinned {
		lines, err := ds.readPinned(ctx, ag, scope, pin)
		if err != nil {
			ds.svc.log.Warn("substrate: an agent's pinned read was skipped",
				"agent", ag.Identity(), "pinned", i, "kind", pin.Kind, "error", err)
			complete = false
			continue
		}
		sections = append(sections, pinnedSection{kind: pin.Kind, lines: lines})
	}
	return renderPinned(sections, ag.Context.MaxBytes), answer, complete
}

// readPinned runs one pinned read and renders each record as one JSON line.
// The loader held the kind to the grant; it is checked again here because
// the registry may have moved since, and a kind the grant no longer reaches
// is skipped with its reason logged, never read.
func (ds *dataset) readPinned(ctx context.Context, ag *vocabulary.Agent, scope queryScope, pin vocabulary.AgentPin) ([]string, error) {
	if ag.Reads == nil || !scope.allows(pin.Kind) {
		return nil, fmt.Errorf("%s is outside the agent's reads grant", pin.Kind)
	}
	ty, err := ds.resolveType(pin.Kind)
	if err != nil {
		return nil, err
	}
	q := substrate.Query{
		Filter:  pin.Filter,
		OrderBy: append([]substrate.Order(nil), pin.OrderBy...),
		First:   pin.First,
	}
	q.Filter.Kinds = []string{ty.Identity}
	page, err := ds.readList(ctx, q)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(page.Records))
	for _, rec := range page.Records {
		row := map[string]any{"id": rec.ID}
		if len(pin.Fields) == 0 {
			row["title"] = rec.Title
		}
		for _, f := range pin.Fields {
			if f == substrate.PropTitle {
				row[f] = rec.Title
				continue
			}
			if v, ok := rec.Properties[f]; ok && v != nil {
				row[f] = v
			}
		}
		// HTML escaping stays on: a record's text can then never close the
		// block it is quoted in.
		buf, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		lines = append(lines, string(buf))
	}
	return lines, nil
}

type pinnedSection struct {
	kind  string
	lines []string
}

// renderPinned lays the sections out in declaration order, each record on
// its own line under its kind, within maxBytes. Records are taken in order
// until the next one would not fit; the rest are counted in one closing
// line, so the cut is the same for the same records every time.
func renderPinned(sections []pinnedSection, maxBytes int) string {
	total := 0
	for _, s := range sections {
		total += len(s.lines)
	}
	head := pinnedOpen + "\n" + pinnedIntro + "\n"
	tail := pinnedClose
	full := layoutPinned(sections, -1)
	if len(head)+len(full)+len(tail) <= maxBytes {
		return head + full + tail
	}
	// The marker's length is bounded by its widest count, so reserving it
	// whole keeps the cut a function of the records alone.
	marker := func(left int) string {
		return fmt.Sprintf("[%d more pinned records left out: the block is capped at %d bytes]\n", left, maxBytes)
	}
	budget := maxBytes - len(head) - len(tail) - len(marker(total))
	shown := 0
	for n := 1; n <= total; n++ {
		if len(layoutPinned(sections, n)) > budget {
			break
		}
		shown = n
	}
	return head + layoutPinned(sections, shown) + marker(total-shown) + tail
}

// layoutPinned renders the first n records across the sections, all of them
// when n is negative. A section with nothing shown is left out.
func layoutPinned(sections []pinnedSection, n int) string {
	var b strings.Builder
	for _, s := range sections {
		lines := s.lines
		if n >= 0 {
			lines = lines[:min(len(lines), n)]
			n -= len(lines)
		}
		if len(lines) == 0 {
			continue
		}
		b.WriteString(`<records kind="` + s.kind + `">` + "\n")
		for _, line := range lines {
			b.WriteString(line)
			b.WriteByte('\n')
		}
		b.WriteString("</records>\n")
	}
	return b.String()
}

// openPinned takes a new thread's snapshot: the block for this run, and the
// value the thread row is created with, nil when the agent pins nothing or a
// read failed.
func (l *agentLoop) openPinned(ctx context.Context) any {
	if !l.snapshots() {
		return nil
	}
	at := nowUTC()
	var parts []string
	complete := true
	if l.takesInstructions() {
		block, err := l.ds.takeInstructions(ctx, l.ag)
		if err != nil {
			l.ds.svc.log.Warn("substrate: an agent's instructions were not read", "agent", l.ag.Identity(), "error", err)
			complete = false
		}
		parts = append(parts, block)
	}
	answer := ""
	if len(l.ag.Context.Pinned) > 0 {
		block, read, ok := l.ds.takePinned(ctx, l.ag)
		complete = complete && ok
		answer = read
		parts = append(parts, block)
	}
	if hook := l.ds.afterPinnedRead; hook != nil {
		hook()
	}
	l.pinnedBlock = composeSystem(parts...)
	if !complete {
		return nil
	}
	// The block is stored under what its own reads' scope answered, not
	// under a second read of the rules, which could answer otherwise.
	return pinnedProps(l.ag, l.takesInstructions(), l.ds.capturedBy(ctx, l.actor, answer), l.pinnedBlock, at)
}

// continuePinned sets a continued thread's block: the stored one when it was
// taken under the agent's current declaration, for the same work, while its
// reads' scope answers now (pinnedAnswer) what it answered then; else a
// fresh snapshot, stored on the row. A scope that cannot be read now
// replays nothing: the block is taken again, under what reads. The row
// holds none when a compaction cleared it or the thread predates pinning.
func (l *agentLoop) continuePinned(ctx context.Context, stored any) error {
	if !l.snapshots() {
		return nil
	}
	replay, answer := true, ""
	if len(l.ag.Context.Pinned) > 0 {
		_, now, err := l.ds.pinnedAnswer(ctx, l.ag)
		if err != nil {
			l.ds.svc.log.Warn("substrate: a pinned block was not replayed: its reads' scope was not read", "agent", l.ag.Identity(), "error", err)
			replay = false
		}
		answer = now
	}
	if block, ok := storedPinned(l.ag, l.takesInstructions(), l.ds.capturedBy(ctx, l.actor, answer), stored); replay && ok {
		l.pinnedBlock = block
		return nil
	}
	props := l.openPinned(ctx)
	if props == nil {
		return nil
	}
	return l.ds.runRecordTx(ctx, l.actor, exemptConversation, func(t *txn) error {
		t.causedBy = l.in.causedBy
		_, err := t.patch(eref{Kind: typeThread, ID: l.threadID}, substrate.PatchInput{Properties: map[string]any{
			propThreadPinned: props,
		}})
		return err
	})
}

// dropPinned clears the thread's stored block after a compaction, so the
// next run takes a fresh snapshot: the history it replays has moved anyway,
// and the facts may have.
func (l *agentLoop) dropPinned(ctx context.Context) {
	if !l.snapshots() {
		return
	}
	if err := l.ds.runRecordTx(ctx, l.actor, exemptConversation, func(t *txn) error {
		t.causedBy = l.in.causedBy
		_, err := t.patch(eref{Kind: typeThread, ID: l.threadID}, substrate.PatchInput{Properties: map[string]any{
			propThreadPinned: nil,
		}})
		return err
	}); err != nil {
		l.ds.svc.log.Warn("substrate: clearing a compacted thread's pinned context failed", "thread", l.threadID, "error", err)
	}
}
