package vocabulary

import (
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/strictjson"
	"github.com/geoah/substrate/internal/substrate"
)

// The pinned-context bounds. The block rides the system prompt of every
// completion of a thread, so its cap is a cost knob: 8 KiB is a page of
// standing facts, and 64 KiB is as much as the inline prompt itself may be.
const (
	DefaultAgentPinnedBytes = 8 << 10
	MinAgentPinnedBytes     = 1 << 10
	MaxAgentPinnedBytes     = 64 << 10
	// DefaultAgentPinnedFirst and MaxAgentPinnedFirst bound one pinned read
	// the way a list page is bounded.
	DefaultAgentPinnedFirst = 50
	MaxAgentPinnedFirst     = 500
	// MaxAgentPins bounds the reads a thread's open runs before its first
	// completion.
	MaxAgentPins = 16
)

// AgentContext is `data.context` parsed: what the engine renders into the
// system prompt beside the agent's own prompt, without a tool call.
type AgentContext struct {
	// Pinned are the reads evaluated once when a thread opens, in declaration
	// order; the snapshot is kept for the thread's life.
	Pinned []AgentPin
	// MaxBytes caps the rendered block, its tags included.
	MaxBytes int
}

// AgentPin is one `context.pinned` entry: a list read of one kind, held to
// the agent's own `permissions.reads`.
type AgentPin struct {
	Kind    string
	Filter  substrate.Filter
	OrderBy []substrate.Order
	First   int
	// Fields are the properties each record renders with; empty renders its
	// title.
	Fields []string
}

var agentContextKeys = map[string]bool{"pinned": true, "maxBytes": true}

var agentPinKeys = map[string]bool{
	"kind": true, "filter": true, "orderBy": true, "first": true, "fields": true,
}

// parseAgentContext reads `data.context`. It runs after the grants parse,
// because a pinned kind outside `permissions.reads` is refused here, where the
// author sees it, rather than skipped at every thread open.
func (l *loader) parseAgentContext(where string, data map[string]any, a *Agent) bool {
	raw, present := data["context"]
	if !present || raw == nil {
		return true
	}
	c, ok := raw.(map[string]any)
	if !ok {
		l.errf("%s: data.context: %v is not an object", where, raw)
		return false
	}
	l.checkKeys(where+": data.context", c, agentContextKeys)
	var bounded bool
	if a.Context.MaxBytes, bounded = l.boundedIntFrom(where+": data.context.maxBytes", c, "maxBytes",
		DefaultAgentPinnedBytes, MinAgentPinnedBytes, MaxAgentPinnedBytes); !bounded {
		return false
	}
	pins := mslice(c, "pinned")
	if len(pins) > MaxAgentPins {
		l.errf("%s: data.context.pinned: %d entries — at most %d", where, len(pins), MaxAgentPins)
		return false
	}
	for i, pv := range pins {
		pwhere := fmt.Sprintf("%s: data.context.pinned[%d]", where, i)
		entry, ok := pv.(map[string]any)
		if !ok {
			l.errf("%s: a pinned read is a {kind, filter, orderBy, first, fields} map, got %T", pwhere, pv)
			return false
		}
		pin, ok := l.parseAgentPin(pwhere, entry, a)
		if !ok {
			return false
		}
		a.Context.Pinned = append(a.Context.Pinned, pin)
	}
	return true
}

func (l *loader) parseAgentPin(where string, entry map[string]any, a *Agent) (AgentPin, bool) {
	l.checkKeys(where, entry, agentPinKeys)
	var pin AgentPin
	pin.Kind = ReferentID(entry["kind"], CoreKind(DocKind))
	if pin.Kind == "" {
		l.errf("%s: kind is required — the one kind this read lists", where)
		return pin, false
	}
	if !Qualified(pin.Kind) || IsTypeGlob(pin.Kind) {
		l.errf("%s: kind %q — a full kind reference, no globs", where, pin.Kind)
		return pin, false
	}
	// No new permission: a pinned read is a read, so the grant that scopes
	// the query tool scopes it too.
	if a.Reads == nil {
		l.errf("%s: pinning %s needs data.permissions.reads, since a pinned read is held to the agent's reads grant", where, pin.Kind)
		return pin, false
	}
	granted := false
	for _, g := range a.Reads.Kinds {
		if GrantMatches(g, pin.Kind) {
			granted = true
			break
		}
	}
	if !granted {
		l.errf("%s: %s is not in data.permissions.reads.kinds, and a pinned read is held to the agent's reads grant", where, pin.Kind)
		return pin, false
	}
	// A string on the row (a field is a declared shape, and the grammar is
	// open-ended), in the records route's own `filter` spelling.
	if f := strings.TrimSpace(mstr(entry, "filter")); f != "" {
		if err := strictjson.DecodeBytes([]byte(f), &pin.Filter, false); err != nil {
			l.errf("%s: filter: %v — filter takes %s", where, err, strings.Join(strictjson.Keys(substrate.Filter{}), ", "))
			return pin, false
		}
		if len(pin.Filter.Kinds) > 0 {
			l.errf("%s: filter.kinds — a pinned read lists the one kind its `kind` names", where)
			return pin, false
		}
	}
	// A stable order is what makes the block the same bytes twice, so the
	// cached prefix holds; an unordered list is not refused anywhere else.
	order := mstr(entry, "orderBy")
	orders, err := substrate.ParseOrderBy(order)
	if err == nil && len(orders) == 0 {
		err = fmt.Errorf("orderBy is required — a pinned read renders in a stable order, so name one, e.g. createdAt or id")
	}
	if err != nil {
		l.errf("%s: %v", where, err)
		return pin, false
	}
	pin.OrderBy = orders
	var bounded bool
	if pin.First, bounded = l.boundedInt(where+".first", entry, "first", DefaultAgentPinnedFirst, MaxAgentPinnedFirst); !bounded {
		return pin, false
	}
	seen := map[string]bool{}
	for j, fv := range mslice(entry, "fields") {
		name, _ := fv.(string)
		if name == "" || seen[name] {
			l.errf("%s: fields[%d]: %v — a property name, named once", where, j, fv)
			return pin, false
		}
		seen[name] = true
		pin.Fields = append(pin.Fields, name)
	}
	return pin, true
}

// resolveAgentContext holds each pinned read to the registry: its kind must
// exist and each field must be a property the kind declares and may show.
func (r *Registry) resolveAgentContext(a *Agent) []string {
	var problems []string
	where := DocAgent + " " + a.Identity()
	for i, pin := range a.Context.Pinned {
		k, ok := r.ByIdentity(pin.Kind)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: data.context.pinned[%d]: unknown kind %q", where, i, pin.Kind))
			continue
		}
		for _, f := range pin.Fields {
			if f == "title" {
				continue
			}
			p, ok := k.Prop(f)
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("%s: data.context.pinned[%d].fields: %s declares no property %q", where, i, pin.Kind, f))
			case p.Sensitive():
				problems = append(problems, fmt.Sprintf("%s: data.context.pinned[%d].fields: %s.%s is sensitive and never renders into a prompt", where, i, pin.Kind, f))
			}
		}
	}
	return problems
}
