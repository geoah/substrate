package engine

// Conversion step 4: a record's `body` moves in with its other properties
// (decision 0273).
//
// Dialects 1 to 3 kept a declared `body` in a column of its own, so a record
// effect carried it as a delta field of its own, `delta.body`, beside `set`
// and `del`, with `""` as the cleared form. Dialect 4 has no such field: a value
// is `set.body` and a clear is `body` in `del`, as any other property's, which
// is what the write path now spells for the same change. Nothing else on the
// line moves: the payload's `properties` already named `body` when it changed,
// and no other effect carries it.
//
// The write path routed the name to the column, so a delta below dialect 4 never
// holds `body` in `set` or `del` as well; one that does is refused rather than
// merged, since one of the two values would be lost. A line with no
// `delta.body` is left as it is, so the step is idempotent.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/geoah/substrate/internal/changelogfile"
)

// movedBodyKey is the delta field below dialect 4 this step retires, and the
// property name its value moves to.
const movedBodyKey = "body"

func moveBodyIntoProperties(e *changelogfile.Entry) error {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return fmt.Errorf("seq %d: the payload: %w", e.Seq, err)
	}
	raw, ok := payload[foldPayloadKey]
	if !ok {
		return nil
	}
	var effects []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &effects); err != nil {
		return fmt.Errorf("seq %d: the effects: %w", e.Seq, err)
	}
	moved := false
	for i, eff := range effects {
		rawDelta, ok := eff["delta"]
		if !ok {
			continue
		}
		delta, changed, err := moveBodyInDelta(rawDelta)
		if err != nil {
			return fmt.Errorf("seq %d: effect %d: %w", e.Seq, i, err)
		}
		if changed {
			eff["delta"], moved = delta, true
		}
	}
	if !moved {
		return nil
	}
	out, err := json.Marshal(effects)
	if err != nil {
		return err
	}
	payload[foldPayloadKey] = out
	if e.Payload, err = json.Marshal(payload); err != nil {
		return err
	}
	return nil
}

// moveBodyInDelta answers the delta with its `body` field moved into `set`
// or `del`, and whether there was one to move. Every other field is carried
// as its own bytes.
func moveBodyInDelta(raw json.RawMessage) (json.RawMessage, bool, error) {
	var delta map[string]json.RawMessage
	if err := json.Unmarshal(raw, &delta); err != nil {
		return nil, false, fmt.Errorf("the delta: %w", err)
	}
	rawBody, ok := delta[movedBodyKey]
	if !ok {
		return raw, false, nil
	}
	// The field was a pointer to a string the write path set and omitted when
	// nil, so no release wrote `null` there, and `null` decodes without an
	// error into the empty string, the cleared form: a line holding it is
	// damaged, and converting it would erase what an earlier line set.
	if bytes.Equal(bytes.TrimSpace(rawBody), []byte("null")) {
		return nil, false, fmt.Errorf("delta.body is null, which no release wrote")
	}
	var body string
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return nil, false, fmt.Errorf("delta.body: %w", err)
	}
	// A `set` or `del` the line carries is an object and a list, as every
	// writer spelled them: `null` decodes without an error into a nil map or
	// list, which the move below would write into or carry as a list, so it
	// is refused with the rest.
	set := map[string]json.RawMessage{}
	if s, ok := delta["set"]; ok {
		if err := json.Unmarshal(s, &set); err != nil {
			return nil, false, fmt.Errorf("delta.set: %w", err)
		}
		if set == nil {
			return nil, false, fmt.Errorf("delta.set is null, not an object of properties")
		}
	}
	var del []string
	if d, ok := delta["del"]; ok {
		if err := json.Unmarshal(d, &del); err != nil {
			return nil, false, fmt.Errorf("delta.del: %w", err)
		}
		if del == nil {
			return nil, false, fmt.Errorf("delta.del is null, not a list of properties")
		}
	}
	if _, inSet := set[movedBodyKey]; inSet || slices.Contains(del, movedBodyKey) {
		return nil, false, fmt.Errorf("the delta carries body both as delta.body and among its properties")
	}
	delete(delta, movedBodyKey)
	var err error
	if body == "" {
		del = append(del, movedBodyKey)
		slices.Sort(del)
		delta["del"], err = json.Marshal(del)
	} else {
		set[movedBodyKey] = rawBody
		delta["set"], err = json.Marshal(set)
	}
	if err != nil {
		return nil, false, err
	}
	out, err := json.Marshal(delta)
	return out, true, err
}
