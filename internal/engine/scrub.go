package engine

// The invocation scrubber: every secret and token INJECTED into a runner
// invocation (the resolved bundle config's secret-typed properties, the live
// access tokens) is replaced — by exact value — in everything that leaves
// the runner boundary: body logs, error text (and so run rows and
// parked-failure rows), outputs (and so agent tool-result transcripts, LLM
// turns, and API responses). Exact-value scrubbing is CONTAINMENT, not a
// defense: a body that transforms a secret (base64, split, hash) or ships it
// over its own network call defeats it — closing that requires opaque
// credential handles or a host-side credential broker instead of raw values
// (a recorded follow-up, deliberately not built here).

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// errSecretInEffects rejects a runCallable whose RETURNED effects carry an
// injected secret value verbatim in a property value, id or relation (review
// #3). Effect targets are ADDRESSED data — the outbound scrubber cannot
// redact them in place (a redacted id would corrupt the write), and an
// unchanged value never passes through the value/text scrub at all — so the
// safe move is to refuse the whole invocation before decode rather than
// persist the literal redaction marker as user data. Deterministic by
// construction: the same body reproduces it.
var errSecretInEffects = errors.New("a returned effect carries an injected secret value verbatim — rejected before it could persist")

// errSecretInContinuation rejects a runCallable whose PAGED-CHECKPOINT
// continuation (`more.cursor`) carries an injected secret value verbatim
// . The cursor is opaque bookkeeping the host JSON-encodes into
// `paged_cursors` untouched — it never passes through the outbound scrubber, so
// a secret copied into it would persist in plaintext and outlive its rotation.
// It cannot be redacted in place either: a redacted cursor corrupts resume. The
// safe move — the same one effect targets get — is to refuse the whole
// invocation before anything commits. Deterministic by construction: the same
// body reproduces it.
var errSecretInContinuation = errors.New("a paged continuation cursor carries an injected secret value verbatim — rejected before it could persist")

// scrubMinLen skips degenerate "secrets" whose replacement would shred
// ordinary text; anything shorter carries no secrecy worth the collateral.
const scrubMinLen = 4

// scrubber replaces a set of injected secret values in strings, errors and
// JSON-shaped values. Single-goroutine, like the invocation that owns it.
type scrubber struct {
	values map[string]bool
	repl   *strings.Replacer
}

func newScrubber() *scrubber {
	return &scrubber{values: map[string]bool{}}
}

// add registers injected secret values. Each value is matched raw AND in its
// JSON-escaped spelling, so a secret with quotes or backslashes still scrubs
// out of marshaled output.
func (s *scrubber) add(values ...string) {
	changed := false
	for _, v := range values {
		if len(v) < scrubMinLen || s.values[v] {
			continue
		}
		s.values[v] = true
		changed = true
	}
	if !changed {
		return
	}
	ordered := make([]string, 0, len(s.values))
	for v := range s.values {
		ordered = append(ordered, v)
	}
	// Longest first: when one secret prefixes another, the whole value wins.
	sort.Slice(ordered, func(i, j int) bool {
		if len(ordered[i]) != len(ordered[j]) {
			return len(ordered[i]) > len(ordered[j])
		}
		return ordered[i] < ordered[j]
	})
	var pairs []string
	for _, v := range ordered {
		pairs = append(pairs, v, Redacted)
		if esc := jsonEscape(v); esc != v {
			pairs = append(pairs, esc, Redacted)
		}
	}
	s.repl = strings.NewReplacer(pairs...)
}

// jsonEscape renders a string the way json.Marshal would, quotes stripped.
func jsonEscape(v string) string {
	buf, err := json.Marshal(v)
	if err != nil || len(buf) < 2 {
		return v
	}
	return string(buf[1 : len(buf)-1])
}

// found reports whether any registered secret value appears anywhere in the
// JSON-shaped value — the pre-decode gate over returned effects.
// It reuses the exact-value replacer: a secret is present exactly when
// scrubbing the marshaled form changes it, so ids, relations and nested
// property values are all covered in one pass. A value that will not marshal
// never reaches the applier, so it reads as not-found and decode rejects it
// on shape.
func (s *scrubber) found(v any) bool {
	if s == nil || s.repl == nil {
		return false
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return false
	}
	return s.text(string(buf)) != string(buf)
}

// text scrubs one string.
func (s *scrubber) text(in string) string {
	if s == nil || s.repl == nil {
		return in
	}
	return s.repl.Replace(in)
}

// err scrubs an error's rendered text while keeping the original chain
// intact underneath, so errors.Is/As (Deterministic trips, sentinel checks)
// still see through it.
func (s *scrubber) err(e error) error {
	if e == nil || s == nil || s.repl == nil {
		return e
	}
	msg := s.text(e.Error())
	if msg == e.Error() {
		return e
	}
	return &scrubbedError{msg: msg, cause: e}
}

// value scrubs a JSON-shaped value (a body's output) by round-tripping it
// through its serialized form. When a replacement breaks the JSON shape (a
// secret spanning structure), the scrubbed STRING is returned instead — the
// safe direction.
func (s *scrubber) value(v any) any {
	if v == nil || s == nil || s.repl == nil {
		return v
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return v
	}
	clean := s.text(string(buf))
	if clean == string(buf) {
		return v
	}
	var out any
	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		return clean
	}
	return out
}

// scrubbedError is an error whose TEXT is scrubbed but whose chain is not:
// Unwrap exposes the original, so classification survives redaction.
type scrubbedError struct {
	msg   string
	cause error
}

func (e *scrubbedError) Error() string { return e.msg }
func (e *scrubbedError) Unwrap() error { return e.cause }
