package engine

// THE ACTOR LINKS CONVERSION (decision 0270): a history line written before
// actors were records, rewritten to link them. It is the chain's step to
// changelog dialect 3 (conversion.go), and it needs nothing but the line: the
// id of an actor record is derived from the hand alone (vocabulary.ActorID),
// so every spelling a line holds becomes its link where it stands, the same
// in every repository and on every run. Which records those ids name is
// written by repository migration 9 at the first open after, from the hands
// the repository holds (repomigration_0009_link_actors.go).
//
// THE POSITIONS. The engine wrote a hand's text into these places, and only
// these; every other value, at any depth, is a person's or a callable's and
// comes back byte for byte. Each is read with the line's evidence, as the
// line's own actor is (spelledHand), and a refusal is found by the sentences
// the base composed around a hand (refusalForms) behind the wrappers its
// errors were stored in (refusalWrappers).
//
//   - The line: `actor` becomes the actor record's id; `principal`, the token
//     id, becomes `token`; an app's line gains `onBehalfOf`, the person's id
//     (no older line says which person's request started a function or an
//     agent, so theirs gain none).
//   - The payload: `managers`, `triggeredBy`; `conflict.reason` and
//     `policyVerdict.note`, refusals; `moved.managers[].actor`.
//   - The fold effects: a `manager` effect's `actor` and a resync's
//     `rows.managers[].actor`, each read by its own `principal`; a `park`
//     effect's `failure.lastError`, a refusal; the `substrate/conflict`
//     annotation's `value.reason` and the `policy/verdict` annotation's
//     `value.note`, refusals.
//   - A record effect's `delta.set`, by the record's kind:
//       - core/package: `declaredBy` links the actor record; `actors` goes;
//         `takenOverFrom` names an app by its publisher.
//       - core/blob: `createdBy` links the actor record.
//       - core/triggerrun: `caller` links the actor record; `principal` is
//         `token`; `reason`, a refusal.
//       - core/recordmerge: `moved.managers[].actor`. core/recordsplit:
//         `result`, a payload read as above.
//       - core/trigger: `when` reads `actorType` and `actorPackage` where it
//         read a spelling.
//       - llm/thread: `reason`, a stop ("stopped by …" and any spelling,
//         which says the type of the hand) or a refusal.
//       - llm/approval: `error`, a refusal.
//       - llm/message: `callable` and `toolCalls[].callable` become record
//         paths, and `content`, the JSON the engine composed, by role:
//           - `system`, the engine's own row: `callable`; a `toolCompleted`
//             whose `ok` is false, its `output` (the engine's error object:
//             a stop or a refusal); an `approvalDecided`'s `error`; a
//             `proposalConflicted`'s `reason`.
//           - `tool`: a call whose row's `ok` is false, `error`; a succeeded
//             call of the read tool (core/query), its `record` and each of
//             its `records`, read as the record itself is (its kind's
//             positions above, `propertyMeta.*.manager` and
//             `propertyMeta.*.alternatives[].actor`); a succeeded call of the
//             wait tool (core/wait), each `completed[]` job whose `ok` is
//             false, its `output`.
//           - `user` in a line an agent wrote: a trigger's envelope, exactly
//             the runner's shape (`change`, `record`, `repository`), its
//             `change.actor` and its `record`, read as the record itself is.
//             Any other content, a judge's input and a sub-agent's among it,
//             is left as it is, and so is a user row the person or an app
//             wrote.
//
// THE LINE SAYS WHETHER IT IS CONVERTED, never the shape of a text in it.
// Changelog dialect 1 wrote `principal` on every line, an empty one where no
// token stood behind the write, and this conversion is what takes the key
// away. A line with the key is converted whole, every actor in it a spelling
// whatever it looks like: the retired header took any label a person's
// request claimed, an id-shaped one included, and that label is the person.
// A line without it is already converted and comes back unchanged.

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// linkActorsLine rewrites one history line to link its actors.
func linkActorsLine(e *changelogfile.Entry) error {
	if !e.HasPrincipal {
		return nil
	}
	spelled, behind := e.Actor, e.Principal != ""
	e.Actor = vocabulary.ActorID(spelledHand(spelled, behind))
	if e.HasPrincipal {
		if e.Token == "" {
			e.Token = e.Principal
		}
		e.Principal, e.HasPrincipal = "", false
	}
	if e.OnBehalfOf == "" && spelled != e.Actor && substrate.IsAppActor(substrate.Actor(spelled)) {
		e.OnBehalfOf = vocabulary.ActorID(substrate.ActorPerson)
	}
	if len(e.Payload) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(e.Payload))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil || payload == nil {
		return err
	}
	if !linkPayloadActors(payload, lineEvidence{behind: behind, spelled: spelled}) {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	e.Payload = raw
	return nil
}

// linkedActor is the id of the hand an unconverted line spells in a value
// the engine stored, read by the rule and with the evidence the line's own
// actor is: whether a token stood behind the line (behindToken).
func linkedActor(s string, behindToken bool) string {
	if s == "" {
		return s
	}
	return vocabulary.ActorID(spelledHand(s, behindToken))
}

// spelledHand is the hand a spelling in an unconverted line names. Every form
// the engine wrote is its own hand (substrate.CanonicalActor): the doors
// `api`, `console` and `substratectl` are the person, `substrate` and its
// facilities and a policy's judge are the system, and `bundle:`, `function:`,
// `agent:`, `app:` and the retired `connector:` are the code and the app they
// name. Any other text was a label a person's request claimed through the
// retired actor header, so the person: nothing is guessed from what it looks
// like. Two cases need more than the text:
//
//   - `id:` was never a form the engine wrote, only a label, so it is the
//     person, never the record id it seems to wrap.
//   - The header was read only under a token, and refused exactly the
//     reserved forms (substrate.ReservedActor). So behind a token, any text
//     the header did not refuse is the person's label, a `policy:` or a
//     `mapping:` spelling included; the engine wrote those behind none.
func spelledHand(s string, behindToken bool) substrate.Actor {
	a := substrate.Actor(s)
	switch {
	case s == "":
		return ""
	case strings.HasPrefix(s, "id:"):
		return substrate.ActorPerson
	case behindToken && !substrate.ReservedActor(a):
		return substrate.ActorPerson
	}
	return substrate.CanonicalActor(a)
}

// lineEvidence is what a value the engine stored in a line is read by: whether
// a token stood behind the line, and the line's own actor as it was spelled,
// which a sentence the engine composed about the line's hand names.
type lineEvidence struct {
	behind  bool
	spelled string
}

// spelledForm matches a hand's spelling in one of the forms the engine wrote,
// so a refusal naming a hand other than the line's own is found by its form.
const spelledForm = `(?:function|agent|bundle|app|connector|policy|mapping):[^\s"',;]+|substrate(?:\.[a-z]+)?|api|console|substratectl`

// refusalWrappers is what stood before a refusal where its error reached a
// stored field whole: the sentinels' own texts (`substrate: forbidden: `),
// and an accepted change request's (`substrate: the accepted diff did not
// apply: `, substrate.AcceptConflictError). Every one is the word
// `substrate:`, words without a colon, and a colon; a stored field may hold
// none, one, or several.
const refusalWrappers = `(?:substrate: [^:"]+: )*`

// refusalForms are the sentences the base composed around a hand in a
// refusal, each with the hand's place marked: a key outside the hand's
// namespace, a property another role writes, an acceptance a package's hand
// may not make, a token only the owner revokes, a function an agent's tool
// runs.
var refusalForms = []struct{ before, after string }{
	{`(actor ")`, `(")`},
	{`(role, not )`, `($|[\s.,;])`},
	{`(^` + refusalWrappers + `)`, `( may not (?:accept|materialize) )`},
	{`(, and )`, `( writes at the )`},
	{`(runs as )`, `('s tool)`},
}

// rewordRefusal rewrites the hand a refusal the engine composed names, in each
// sentence the base wrote around one, into the words a sentence names a hand
// by now (handWords): the line's own hand however it was spelled, and any
// other by its form. Nothing else in the sentence moves.
func (ev lineEvidence) rewordRefusal(s string) string {
	hand := spelledForm
	if ev.spelled != "" {
		hand = regexp.QuoteMeta(ev.spelled) + `|` + spelledForm
	}
	for _, form := range refusalForms {
		re := regexp.MustCompile(form.before + `(` + hand + `)` + form.after)
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			sub := re.FindStringSubmatch(m)
			words := handWords(spelledHand(sub[2], ev.behind))
			if form.before == `(actor ")` {
				return words
			}
			return sub[1] + words + sub[3]
		})
	}
	return s
}

// linkedActorRef is the link a stored value names a hand by, from its
// spelling; a value that is not a spelling is left as it is.
func linkedActorRef(v any, behindToken bool) (any, bool) {
	s, ok := v.(string)
	if !ok || s == "" {
		return v, false
	}
	return vocabulary.ActorRef(linkedActor(s, behindToken)), true
}

// linkPayloadActors rewrites a line's payload in place and reports whether it
// changed anything. Every spelling the engine stored in it is read by the
// line's evidence, as the line's own actor is.
func linkPayloadActors(payload map[string]any, ev lineEvidence) bool {
	behindToken := ev.behind
	changed := false
	// An accepted change request's refusal, as the conflict recorded it.
	if conflict, ok := payload["conflict"].(map[string]any); ok {
		changed = rewordReason(conflict, "reason", ev) || changed
	}
	// A policy's verdict, whose note may quote the refusal its decision met.
	if verdict, ok := payload["policyVerdict"].(map[string]any); ok {
		changed = rewordReason(verdict, "note", ev) || changed
	}
	if managers, ok := payload["managers"].(map[string]any); ok {
		for name, v := range managers {
			if s, ok := v.(string); ok && linkedActor(s, behindToken) != s {
				managers[name] = linkedActor(s, behindToken)
				changed = true
			}
		}
	}
	if s, ok := payload["triggeredBy"].(string); ok && linkedActor(s, behindToken) != s {
		payload["triggeredBy"] = linkedActor(s, behindToken)
		changed = true
	}
	if moved, ok := payload["moved"].(map[string]any); ok {
		changed = linkManagerRows(moved["managers"]) || changed
	}
	effects, _ := payload[foldPayloadKey].([]any)
	for _, raw := range effects {
		effect, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch effect["kind"] {
		case "manager":
			changed = linkManagerRow(effect) || changed
		case "annotation":
			if value, ok := effect["value"].(map[string]any); ok {
				switch effect["key"] {
				case annApplyConflict:
					changed = rewordReason(value, "reason", ev) || changed
				case annPolicyVerdict:
					changed = rewordReason(value, "note", ev) || changed
				}
			}
		case "park":
			// A delivery's failure, whose error may be a refusal of the
			// callable's write.
			if failure, ok := effect["failure"].(map[string]any); ok {
				changed = rewordReason(failure, "lastError", ev) || changed
			}
		case "resync":
			if rows, ok := effect["rows"].(map[string]any); ok {
				changed = linkManagerRows(rows["managers"]) || changed
			}
		case "record":
			delta, _ := effect["delta"].(map[string]any)
			set, _ := delta["set"].(map[string]any)
			if set != nil {
				kind, _ := effect["ref"].(string)
				changed = linkRecordValues(kind, set, ev) || changed
			}
		}
	}
	return changed
}

// linkManagerRows rewrites each manager row of a list.
func linkManagerRows(v any) bool {
	rows, _ := v.([]any)
	changed := false
	for _, raw := range rows {
		if row, ok := raw.(map[string]any); ok {
			changed = linkManagerRow(row) || changed
		}
	}
	return changed
}

// linkManagerRow rewrites one manager row: its actor's id, and its token
// under the name a row now carries it by.
func linkManagerRow(row map[string]any) bool {
	changed := false
	// A row names the token its write stood behind, so it is read by the rule
	// the line's own actor is (spelledHand).
	token, _ := row["principal"].(string)
	if s, ok := row["actor"].(string); ok && s != "" {
		row["actor"] = vocabulary.ActorID(spelledHand(s, token != ""))
		changed = true
	}
	if principal, ok := row["principal"]; ok {
		if s, _ := principal.(string); s != "" {
			if _, has := row["token"]; !has {
				row["token"] = s
			}
		}
		delete(row, "principal")
		changed = true
	}
	return changed
}

// rewordReason rewrites the hand a refusal stored under name names.
func rewordReason(m map[string]any, name string, ev lineEvidence) bool {
	s, ok := m[name].(string)
	if !ok {
		return false
	}
	if r := ev.rewordRefusal(s); r != s {
		m[name] = r
		return true
	}
	return false
}

// linkRecordValues rewrites the values a record effect writes on a record of
// kind.
func linkRecordValues(kind string, set map[string]any, ev lineEvidence) bool {
	behindToken := ev.behind
	changed := false
	link := func(name string) {
		if v, ok := linkedActorRef(set[name], behindToken); ok {
			set[name] = v
			changed = true
		}
	}
	switch kind {
	case kindPackage:
		link(substrate.PackageDeclaredBy)
		if _, ok := set["actors"]; ok {
			delete(set, "actors")
			changed = true
		}
		if s, ok := set[substrate.PackageTakenOverFrom].(string); ok && substrate.IsAppActor(substrate.Actor(s)) {
			set[substrate.PackageTakenOverFrom] = strings.TrimPrefix(s, substrate.AppActorPrefix)
			changed = true
		}
	case kindBlob:
		link(blobPropCreatedBy)
	case typeTriggerRun:
		link("caller")
		// A run's failure: the error its call or delivery ended with.
		changed = rewordReason(set, "reason", ev) || changed
		if principal, ok := set["principal"]; ok {
			if _, has := set["token"]; !has {
				set["token"] = principal
			}
			delete(set, "principal")
			changed = true
		}
	case kindRecordMerge:
		if moved, ok := set["moved"].(map[string]any); ok {
			changed = linkManagerRows(moved["managers"]) || changed
		}
	case kindRecordSplit:
		// A split's record keeps the payload of the split it performed, its
		// effects included.
		if result, ok := set["result"].(map[string]any); ok {
			changed = linkPayloadActors(result, ev) || changed
		}
	case typeMessage:
		if s, ok := set["callable"].(string); ok && callablePath(s) != s {
			set["callable"] = callablePath(s)
			changed = true
		}
		calls, _ := set["toolCalls"].([]any)
		for _, raw := range calls {
			if call, ok := raw.(map[string]any); ok {
				if s, ok := call["callable"].(string); ok && callablePath(s) != s {
					call["callable"] = callablePath(s)
					changed = true
				}
			}
		}
		switch set["role"] {
		case "system":
			changed = linkSystemRow(set, ev) || changed
		case "tool":
			changed = linkToolRow(set, ev) || changed
		case "user":
			// A user row an agent wrote is a trigger's envelope; one the
			// person or an app wrote is the person's own words.
			if strings.HasPrefix(ev.spelled, substrate.AgentActorPrefix) {
				changed = withContent(set, func(envelope map[string]any) bool {
					return linkEnvelope(envelope, ev)
				}) || changed
			}
		}
	case typeThread:
		// A settled thread's reason: a stop, or the error the run ended with.
		if !rewordStop(set, behindToken) {
			changed = rewordReason(set, "reason", ev) || changed
		} else {
			changed = true
		}
	case vocabulary.KindLLMApproval:
		// The approved call's failure, which may be a refusal of its write.
		changed = rewordReason(set, "error", ev) || changed
	case typeTrigger:
		if s, ok := set["when"].(string); ok && linkedGuard(s) != s {
			set["when"] = linkedGuard(s)
			changed = true
		}
	}
	return changed
}

// callablePath is the record path of the function or agent a callable's old
// actor spelling named, the value itself for anything else.
func callablePath(s string) string {
	for prefix, kind := range map[string]string{substrate.FunctionActorPrefix: kindFunction, substrate.AgentActorPrefix: kindAgent} {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			if parts := strings.Split(rest, ":"); len(parts) == 3 {
				return vocabulary.RecordPath(kind, strings.Join(parts, "/"))
			}
		}
	}
	return s
}

// stoppedBy opens the sentence the engine composed around the hand that
// stopped a turn ("stopped by " and the hand's spelling), in a thread's
// `reason` and in the output of a job the stop canceled, which the engine's
// system row of the job's end holds. Both are the engine's fields, so the
// hand in the sentence is converted with it, whatever its text; a person's
// own words are in neither.
const stoppedBy = "stopped by "

// rewordStop rewrites a thread's `reason` the engine wrote for a stop to say
// which type of actor stopped the turn, the spelling read as the line's own
// actor is (behindToken).
func rewordStop(set map[string]any, behindToken bool) bool {
	s, ok := set["reason"].(string)
	if !ok {
		return false
	}
	hand, ok := strings.CutPrefix(s, stoppedBy)
	if !ok || hand == "" {
		return false
	}
	set["reason"] = stoppedByPhrase(substrate.ActorTypeOf(spelledHand(hand, behindToken)))
	return true
}

// withContent rewrites a message's content, the JSON object the engine
// composed, through fn, and reports whether it changed. Content that is no
// JSON object is left alone.
func withContent(set map[string]any, fn func(map[string]any) bool) bool {
	content, ok := set["content"].(string)
	if !ok {
		return false
	}
	dec := json.NewDecoder(strings.NewReader(content))
	dec.UseNumber()
	var object map[string]any
	if dec.Decode(&object) != nil || object == nil || !fn(object) {
		return false
	}
	raw, err := json.Marshal(object)
	if err != nil {
		return false
	}
	set["content"] = string(raw)
	return true
}

// linkSystemRow rewrites the event the engine's own row of a thread holds,
// by event: the callable it names; a `toolCompleted` that did not succeed,
// whose `output` is the engine's error object (a stop or a refusal); an
// `approvalDecided`'s `error`; a `proposalConflicted`'s `reason`. A
// succeeded job's output is its callable's, and is left as it is.
func linkSystemRow(set map[string]any, ev lineEvidence) bool {
	return withContent(set, func(event map[string]any) bool {
		changed := false
		if s, ok := event["callable"].(string); ok && callablePath(s) != s {
			event["callable"] = callablePath(s)
			changed = true
		}
		switch event["event"] {
		case eventToolCompleted:
			if event["ok"] == false {
				changed = rewordErrorObject(event, "output", ev) || changed
			}
		case eventApprovalDecided:
			changed = rewordReason(event, "error", ev) || changed
		case "proposalConflicted":
			changed = rewordReason(event, "reason", ev) || changed
		}
		return changed
	})
}

// The two host functions whose results the engine composed from what it read:
// the read tool's records and the wait tool's job ends.
var (
	callableQuery = vocabulary.RecordPath(kindFunction, "substrate.reamde.dev/core/query")
	callableWait  = vocabulary.RecordPath(kindFunction, "substrate.reamde.dev/core/wait")
)

// linkToolRow rewrites a tool's result row. A call that did not succeed (the
// row's `ok` is false) holds the engine's error object, whose `error` may be a
// refusal. A succeeded call's result is its callable's own, except the two
// host functions': the read tool's `record` and each of its `records`,
// converted as the record itself is, and each of the wait tool's `completed`
// jobs that did not succeed, whose `output` is the engine's error object.
func linkToolRow(set map[string]any, ev lineEvidence) bool {
	if set["ok"] == false {
		return withContent(set, func(content map[string]any) bool {
			return rewordReason(content, "error", ev)
		})
	}
	if set["ok"] != true {
		return false
	}
	callable, _ := set["callable"].(string)
	switch callablePath(callable) {
	case callableQuery:
		return withContent(set, func(content map[string]any) bool {
			changed := false
			if record, ok := content["record"].(map[string]any); ok {
				changed = linkEchoedRecord(record, ev) || changed
			}
			for _, raw := range asItems(content["records"]) {
				if record, ok := raw.(map[string]any); ok {
					changed = linkEchoedRecord(record, ev) || changed
				}
			}
			return changed
		})
	case callableWait:
		return withContent(set, func(content map[string]any) bool {
			changed := false
			for _, raw := range asItems(content["completed"]) {
				if job, ok := raw.(map[string]any); ok && job["ok"] == false {
					changed = rewordErrorObject(job, "output", ev) || changed
				}
			}
			return changed
		})
	}
	return false
}

// linkEnvelope rewrites a trigger's envelope, the content of the user row a
// triggered agent's run opens with, when it is exactly the shape the runner
// composed (`change`, `record`, `repository`): the change's `actor`, the hand
// whose write fired the trigger, and the record, converted as the record
// itself is. Any other content an agent wrote as a user row, a judge's input
// among it, is left as it is.
func linkEnvelope(envelope map[string]any, ev lineEvidence) bool {
	if len(envelope) != 3 {
		return false
	}
	change, ok := envelope["change"].(map[string]any)
	repository, isRepo := envelope["repository"].(map[string]any)
	_, hasRecord := envelope["record"]
	if !ok || !isRepo || repository["authority"] == nil || !hasRecord || change["seq"] == nil || change["op"] == nil {
		return false
	}
	changed := false
	if s, ok := change["actor"].(string); ok && s != "" {
		change["actor"] = linkedActor(s, ev.behind)
		changed = true
	}
	if record, ok := envelope["record"].(map[string]any); ok {
		changed = linkEchoedRecord(record, ev) || changed
	}
	return changed
}

// linkEchoedRecord rewrites a record the engine copied into what it wrote (a
// read tool's result, a trigger's envelope) as the record itself is
// converted: the values of its kind that name a hand, and each property's
// manager and alternatives. A message it copied is converted by its role
// but for a user row, whose writer the copy does not say.
func linkEchoedRecord(record map[string]any, ev lineEvidence) bool {
	changed := false
	kind, _ := record["kind"].(string)
	if props, ok := record["properties"].(map[string]any); ok {
		changed = linkRecordValues(kind, props, lineEvidence{behind: ev.behind}) || changed
	}
	meta, _ := record["propertyMeta"].(map[string]any)
	for _, raw := range meta {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if s, ok := m["manager"].(string); ok && s != "" {
			m["manager"] = linkedActor(s, ev.behind)
			changed = true
		}
		for _, alt := range asItems(m["alternatives"]) {
			if a, ok := alt.(map[string]any); ok {
				if s, ok := a["actor"].(string); ok && s != "" {
					a["actor"] = linkedActor(s, ev.behind)
					changed = true
				}
			}
		}
	}
	return changed
}

// rewordErrorObject rewrites the engine's error object (`{"error": …}`, as
// toolError composed it) stored as a string under name: a stop or a refusal.
func rewordErrorObject(m map[string]any, name string, ev lineEvidence) bool {
	s, ok := m[name].(string)
	if !ok {
		return false
	}
	var out map[string]any
	if json.Unmarshal([]byte(s), &out) != nil || len(out) != 1 {
		return false
	}
	msg, ok := out["error"].(string)
	if !ok {
		return false
	}
	reworded := ev.rewordRefusal(msg)
	if hand, ok := strings.CutPrefix(msg, stoppedBy); ok && hand != "" {
		reworded = stoppedByPhrase(substrate.ActorTypeOf(spelledHand(hand, ev.behind)))
	}
	if reworded == msg {
		return false
	}
	m[name] = toolError(reworded)
	return true
}

var (
	// guardPackageCode matches a guard that told a package's code by the
	// prefix of its actors' spelling.
	guardPackageCode = regexp.MustCompile(`change\.actor\.startsWith\("(function|agent):([^:"]+):([^:"]+):"\)`)
	// guardActorEquals matches a guard that compared the actor to a spelling.
	guardActorEquals = regexp.MustCompile(`change\.actor\s*(==|!=)\s*"([^"]+)"`)
)

// linkedGuard is a trigger guard reading the change's actor as a link: a
// package's code by `actorType` and `actorPackage`, one hand by its id.
func linkedGuard(when string) string {
	when = guardPackageCode.ReplaceAllString(when,
		`("actorPackage" in change && change.actorPackage == "$2/$3" && change.actorType == "$1")`)
	return guardActorEquals.ReplaceAllStringFunc(when, func(m string) string {
		parts := guardActorEquals.FindStringSubmatch(m)
		// A guard names a hand by the form the engine spelled it in, never a
		// label: it reads changes yet to come, not the line it is stored in.
		return `change.actor ` + parts[1] + ` "` + linkedActor(parts[2], false) + `"`
	})
}
