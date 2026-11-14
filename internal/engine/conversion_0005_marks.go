package engine

// THE MARKS CONVERSION (decision 0284): a history line written while a
// property's mark was a manager row and a tier, rewritten to the mark. It is
// the chain's step to changelog dialect 5 (conversion.go), and runs after the
// actor links (conversion_0003_link_actors.go) and the body's move
// (conversion_0004_move_body_into_properties.go), so every actor it meets is
// already an id and a line's `body` is already an ordinary property, whose
// manager effect converts like any other's. A step needs nothing but the line: each stored tier is resolved once, here, into
// `pinned`, and nothing reads a declaration or the hand as it stands today.
//
// What a line holds and what it becomes:
//
//   - A `manager` effect becomes a `mark`, and one that names no actor, the
//     release, an `unmark`. The `owner` and the `bundle` tier were a hand's
//     direct write, which pins, so the mark is pinned and set by that actor.
//     The `machine` tier is not pinned, and it was either recompute crediting
//     a source's writer or a package's or the engine's own direct write; the
//     line tells them apart only where the effect is the line's own direct
//     write, so only there does the mark keep the actor as `setBy`. A line's
//     own effect says `onBehalfOf` where its actor is the line's and the line
//     acted for someone, and a mark says `setAt` where its row carried its
//     stamp.
//   - A resync's manager rows become its `marks`, by the same rule, each
//     with its stamp as `setAt`; a merge's moved managers become its moved
//     `marks`, which name no time, so a split matches them at any.
//   - A recompute's payload `managers` goes: the marks it wrote say it.
//   - A manager row on a property an earlier step took out of the line,
//     value and all (takenOutBelow), becomes no mark: there is no value
//     under it, and a mark never outlives its value. One on a property an
//     earlier step moved to another name follows the value there.
//   - A merge record's `moved` and a split record's `result` hold the same
//     shapes as values, and are rewritten the same way.
//
// NOTHING IS GUESSED. No line said which source record a synced value came
// from, so no converted mark has a `derivedFrom`: the first recompute after
// fills it from the live sources (mapping.go recomputeValues). A manager the
// mapping held on a subject slot became a link to its package, which the line
// cannot tell from a package's own write: its mark is never served (the
// read leaves a slot out) and goes at the slot's next write.
//
// A line already converted comes back unchanged: each rewrite reads only the
// old shape.

import (
	"bytes"
	"encoding/json"

	"github.com/geoah/substrate/internal/changelogfile"
)

// takenOutBelow is every property an earlier step of the chain takes out of
// each record effect of its kind, unconditionally, with the step that takes
// it, and the name it moves the value to where it moves it. A manager row on
// one, as an effect or as a resync's row, is dropped with the value, or
// follows it to its new name; a merge's moved rows name no kind, and the
// merge refuses core's own kinds (merge.go). A later step that takes a value
// out of a line, or moves it, adds its row here.
var takenOutBelow = []struct{ kind, property, movedTo, step string }{
	{kindRecoveryKey, recoveryWrapProperty, "", "drop_recovery_wrap"},
	{kindPackage, "actors", "", "link_actors"},
	{typeTriggerRun, "principal", "token", "link_actors"},
}

// takenOut reports whether an earlier step took property out of every line
// that writes a record of kind, and the name it moved the value to, "" where
// it dropped it.
func takenOut(kind, property any) (movedTo string, ok bool) {
	for _, t := range takenOutBelow {
		if kind == t.kind && property == t.property {
			return t.movedTo, true
		}
	}
	return "", false
}

// markLine rewrites one history line's manager rows into marks.
func markLine(e *changelogfile.Entry) error {
	if len(e.Payload) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(e.Payload))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil || payload == nil {
		return err
	}
	line := markedLine{actor: e.Actor, onBehalfOf: e.OnBehalfOf, recompute: payload["mechanism"] == "mapping"}
	if !markPayload(payload, line) {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	e.Payload = raw
	return nil
}

// markedLine is what a manager row's conversion reads off the line around
// it: who wrote the line, whom it acted for, and whether it was recompute's.
// A payload kept inside a record's value stands outside any line, and reads
// as one nobody wrote.
type markedLine struct {
	actor, onBehalfOf string
	recompute         bool
}

// markPayload rewrites a payload in place and reports whether it changed
// anything.
func markPayload(payload map[string]any, line markedLine) bool {
	changed := false
	if _, ok := payload["managers"]; ok {
		delete(payload, "managers")
		changed = true
	}
	if moved, ok := payload["moved"].(map[string]any); ok {
		changed = markMovedManagers(moved) || changed
	}
	effects, _ := payload[foldPayloadKey].([]any)
	kept := effects[:0:0]
	for _, raw := range effects {
		effect, ok := raw.(map[string]any)
		if !ok {
			kept = append(kept, raw)
			continue
		}
		switch effect["kind"] {
		case "manager":
			changed = true
			if to, ok := takenOut(effect["ref"], effect["property"]); ok {
				if to == "" {
					continue
				}
				effect["property"] = to
			}
			effect = markOfManager(effect, line, true)
		case "resync":
			rows, _ := effect["rows"].(map[string]any)
			if managers, ok := rows["managers"].([]any); ok {
				var marks []any
				for _, raw := range managers {
					row, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					if to, ok := takenOut(row["kind"], row["property"]); ok {
						if to == "" {
							continue
						}
						row["property"] = to
					}
					marks = append(marks, markOfManager(row, line, false))
				}
				delete(rows, "managers")
				if len(marks) > 0 {
					rows["marks"] = marks
				}
				changed = true
			}
		case "record":
			delta, _ := effect["delta"].(map[string]any)
			set, _ := delta["set"].(map[string]any)
			switch effect["ref"] {
			case kindRecordMerge:
				if moved, ok := set["moved"].(map[string]any); ok {
					changed = markMovedManagers(moved) || changed
				}
			case kindRecordSplit:
				// A split's record keeps the payload of the split it
				// performed, its effects included.
				if result, ok := set["result"].(map[string]any); ok {
					changed = markPayload(result, markedLine{}) || changed
				}
			}
		}
		kept = append(kept, effect)
	}
	if changed && effects != nil {
		payload[foldPayloadKey] = kept
	}
	return changed
}

// markOfManager is the mark a manager effect or a resync's manager row
// becomes. An effect keeps its `kind`, `ref` and `id`, a row its `kind` and
// `id`, and both their property. direct says the row is an effect of the
// line itself, the one place the line can say who wrote an unpinned value.
func markOfManager(row map[string]any, line markedLine, direct bool) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"ref", "id", "property"} {
		if v, ok := row[key]; ok {
			out[key] = v
		}
	}
	actor, _ := row["actor"].(string)
	if direct {
		out["kind"] = "mark"
		if actor == "" {
			out["kind"] = "unmark"
			return out
		}
	} else if v, ok := row["kind"]; ok {
		out["kind"] = v
	}
	pinned := row["tier"] != "machine"
	sure := pinned || (direct && !line.recompute && actor == line.actor)
	if pinned {
		out["pinned"] = true
	}
	if sure && actor != "" {
		out["setBy"] = actor
		if direct && actor == line.actor && line.onBehalfOf != "" {
			out["onBehalfOf"] = line.onBehalfOf
		}
	}
	if at, ok := row["updatedAt"]; ok {
		out["setAt"] = at
	}
	return out
}

// markMovedManagers rewrites a merge's moved manager rows into the marks it
// moved, and reports whether there were any. A moved row names no time.
func markMovedManagers(moved map[string]any) bool {
	managers, ok := moved["managers"].([]any)
	if !ok {
		return false
	}
	var marks []any
	for _, raw := range managers {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		m := map[string]any{"property": row["property"]}
		if row["tier"] != "machine" {
			m["pinned"] = true
			if actor, _ := row["actor"].(string); actor != "" {
				m["setBy"] = actor
			}
		}
		marks = append(marks, m)
	}
	delete(moved, "managers")
	if marks == nil {
		marks = []any{}
	}
	moved["marks"] = marks
	return true
}
