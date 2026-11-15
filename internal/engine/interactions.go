package engine

// The llm/interaction kind's own admission and guards, symmetric to the request
// kind's (admitRequestDiff, guardImmutableEnvelope): the generic object
// machinery checks field SHAPE only, so everything an ask promises — unique
// question ids, bounded batches, materialized yes/no options, answers only
// on the answering transition, the owner's hand alone resolving — is
// enforced here, at the write path, for every door at once.

import (
	"context"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The batch bounds: past them a form stops being a question.
const (
	maxInteractionQuestions = 8
	maxInteractionOptions   = 32
)

// propInteractionState is the interaction's one machine.
const propInteractionState = "state"

// The interaction's states, as the declaration spells them.
const (
	interactionPending   = "pending"
	interactionAnswered  = "answered"
	interactionDismissed = "dismissed"
	// interactionReplied is a batch its person answered in a chat message of
	// their own words (decision record 0327).
	interactionReplied = "replied"
)

// propRepliedIn links the chat message a `replied` interaction was answered
// with.
const propRepliedIn = "repliedIn"

// admitInteraction validates the CREATING write. The envelope is immutable
// afterwards, so this is the one moment the questions are judged — and the
// one moment yes/no options are materialized, so validation and replay never
// rest on a convention.
func (t *txn) admitInteraction(sp *applySpec) error {
	// The thread reference is the LOOP's stamp (dispatchAsk), never a generic
	// writer's: nothing points an interaction at somebody else's thread from
	// the outside. The reference being required, a generic create refuses
	// here rather than at the pointer check, with the reason.
	if _, named := sp.props[msgRelThread]; named && !t.interactionThread {
		return fmt.Errorf("%w: an interaction's thread is stamped by the agent loop's ask call — nothing else names it",
			substrate.ErrForbidden)
	}
	if _, named := sp.props["answers"]; named {
		return fmt.Errorf("%w: answers are written by the answering transition, never at create", substrate.ErrValidation)
	}
	if want, ok := sp.states[propInteractionState]; ok && want != interactionPending {
		return fmt.Errorf("%w: an interaction is born pending — %q skips the user", substrate.ErrValidation, want)
	}
	questions := objectRows(sp.props["questions"])
	if len(questions) == 0 {
		return fmt.Errorf("%w: an interaction needs at least one question", substrate.ErrValidation)
	}
	if len(questions) > maxInteractionQuestions {
		return fmt.Errorf("%w: at most %d questions per interaction — past that a form stops being a question",
			substrate.ErrValidation, maxInteractionQuestions)
	}
	ids := map[string]bool{}
	for i, q := range questions {
		id, _ := q["id"].(string)
		if id == "" {
			return fmt.Errorf("%w: questions[%d] needs an id — answers echo it", substrate.ErrValidation, i)
		}
		if ids[id] {
			return fmt.Errorf("%w: question id %q repeats — ids key the answers", substrate.ErrValidation, id)
		}
		ids[id] = true
		if prompt, _ := q["prompt"].(string); prompt == "" {
			return fmt.Errorf("%w: questions[%d] needs a prompt", substrate.ErrValidation, i)
		}
		options := objectRows(q["options"])
		if len(options) > maxInteractionOptions {
			return fmt.Errorf("%w: questions[%d]: at most %d options", substrate.ErrValidation, i, maxInteractionOptions)
		}
		multi, _ := q["multi"].(bool)
		if len(options) == 0 {
			if multi {
				return fmt.Errorf("%w: questions[%d]: multi needs authored options — yes/no is exactly one",
					substrate.ErrValidation, i)
			}
			// Materialize the yes/no options INTO the stored row: the answer
			// validates against stored values, never against a convention
			// that can drift.
			q["options"] = []any{
				map[string]any{"value": "yes"},
				map[string]any{"value": "no"},
			}
			continue
		}
		values := map[string]bool{}
		for j, o := range options {
			v, _ := o["value"].(string)
			if v == "" {
				return fmt.Errorf("%w: questions[%d].options[%d] needs a value", substrate.ErrValidation, i, j)
			}
			if values[v] {
				return fmt.Errorf("%w: questions[%d]: option value %q repeats", substrate.ErrValidation, i, v)
			}
			values[v] = true
		}
	}
	return nil
}

// guardInteraction holds every LATER write to the envelope contract: the
// questions and the thread are what the user read, the answers are the one
// transition's cargo, and resolving is the owner's hand alone.
func (t *txn) guardInteraction(sp *applySpec) error {
	for _, name := range []string{"questions", msgRelThread} {
		next, named := sp.props[name]
		if named && !jsonEqual(sp.existing.Props[name], next) {
			return fmt.Errorf("%w: %s is immutable on an interaction — what the user read is what the agent asked",
				substrate.ErrForbidden, name)
		}
	}
	target, transitioning := sp.states[propInteractionState]
	cur := sp.existing.States[propInteractionState]
	if transitioning && target != cur {
		// Asks are always a person's: installed code answering an ask — its
		// own or anybody's — would make "what did the person authorize" a
		// record of the agent.
		if t.installedCode() {
			return fmt.Errorf("%w: an interaction is resolved by a person, never by installed code", substrate.ErrForbidden)
		}
	}
	// Answering in words is the chat send's alone (replyToInteraction): the
	// state and the link land with the message that says the words, so no
	// write of the record claims a message that was never sent.
	if transitioning && target == interactionReplied && target != cur && t.replying == "" {
		return fmt.Errorf("%w: an interaction is answered in words by the chat message that says them: send the message with repliesTo naming it",
			substrate.ErrForbidden)
	}
	answers, answersNamed := sp.props["answers"]
	answering := transitioning && target == interactionAnswered && cur == interactionPending
	if answersNamed && !answering {
		return fmt.Errorf("%w: answers ride the answering transition alone — patch state to answered with them, once",
			substrate.ErrForbidden)
	}
	if answering {
		if err := validateInteractionAnswers(sp.existing.Props["questions"], answers); err != nil {
			return err
		}
	}
	if transitioning && target == interactionDismissed && answersNamed {
		return fmt.Errorf("%w: a dismissal carries no answers — it declines the whole batch", substrate.ErrValidation)
	}
	return nil
}

// pendingQuestion reads, under its row's lock, whether interaction is a
// question of thread that still waits: what a chat message that names it
// (ChatInput.RepliesTo) would answer. It answers the version it read, which
// the reply's transition is held to, or nil for one resolved meanwhile: that
// is no error, and the message is then sent as any other. An id that names
// no interaction of this thread is refused in the same words whatever it
// does name, so a guessed id says nothing about another thread's questions.
func (t *txn) pendingQuestion(thread, interaction string) (*int64, error) {
	ref := eref{Kind: vocabulary.KindLLMInteraction, ID: interaction}
	if err := t.lockRecord(ref); err != nil {
		return nil, err
	}
	row, err := t.loadRow(ref, true)
	if err != nil {
		return nil, err
	}
	if row == nil || row.DeletedAt != nil || row.Kind != vocabulary.KindLLMInteraction || referenceID(row.Props[msgRelThread]) != thread {
		return nil, fmt.Errorf("%w: repliesTo %q is not a question asked in thread %s", substrate.ErrValidation, interaction, thread)
	}
	if row.States[propInteractionState] != interactionPending {
		return nil, nil
	}
	return &row.Version, nil
}

// replyToInteraction moves the pending interaction to `replied` and links
// message, the chat message that answers it, as sender: the hand that sends
// the message is the hand that resolves the question, held as any write of
// the kind is. version is what pendingQuestion read in this transaction, and
// the caller has written the message in it too, so the two land together or
// not at all. The transition notifies nothing: the message is what the
// thread hears, and the turn that reads it is the one continuation.
func (t *txn) replyToInteraction(interaction, message string, version *int64, sender substrate.Actor) error {
	was := t.replying
	t.replying = message
	defer func() { t.replying = was }()
	return t.asActor(sender, func() error {
		_, err := t.patch(eref{Kind: vocabulary.KindLLMInteraction, ID: interaction}, substrate.PatchInput{
			Properties: map[string]any{propInteractionState: interactionReplied},
			IfVersion:  version,
		})
		return err
	})
}

// validateInteractionAnswers holds the answers to the STORED questions:
// every question answered exactly once, every selection a stored option
// value, single-select exactly one, multi at least one.
func validateInteractionAnswers(storedQuestions, given any) error {
	questions := objectRows(storedQuestions)
	byID := map[string]map[string]any{}
	for _, q := range questions {
		if id, _ := q["id"].(string); id != "" {
			byID[id] = q
		}
	}
	answers := objectRows(given)
	if len(answers) == 0 {
		return fmt.Errorf("%w: answering carries the answers — every question, exactly once", substrate.ErrValidation)
	}
	seen := map[string]bool{}
	for i, a := range answers {
		id, _ := a["question"].(string)
		q, ok := byID[id]
		if !ok {
			return fmt.Errorf("%w: answers[%d] answers %q, which the interaction never asked", substrate.ErrValidation, i, id)
		}
		if seen[id] {
			return fmt.Errorf("%w: question %q is answered twice", substrate.ErrValidation, id)
		}
		seen[id] = true
		values := map[string]bool{}
		for _, o := range objectRows(q["options"]) {
			if v, _ := o["value"].(string); v != "" {
				values[v] = true
			}
		}
		selected, _ := a["selected"].([]any)
		if len(selected) == 0 {
			return fmt.Errorf("%w: question %q needs a selection", substrate.ErrValidation, id)
		}
		multi, _ := q["multi"].(bool)
		if !multi && len(selected) != 1 {
			return fmt.Errorf("%w: question %q takes exactly one selection", substrate.ErrValidation, id)
		}
		picked := map[string]bool{}
		for _, sv := range selected {
			v, _ := sv.(string)
			if !values[v] {
				return fmt.Errorf("%w: question %q has no option %q — answers name stored option values",
					substrate.ErrValidation, id, v)
			}
			if picked[v] {
				return fmt.Errorf("%w: question %q selects %q twice", substrate.ErrValidation, id, v)
			}
			picked[v] = true
		}
	}
	if len(seen) != len(byID) {
		return fmt.Errorf("%w: every question is answered exactly once — %d of %d answered; dismiss the batch to decline it",
			substrate.ErrValidation, len(seen), len(byID))
	}
	return nil
}

// dispatchAsk lands one llm/interaction: the soft interaction. Gated by the
// agent's emit naming the interaction kind (checked at load, held again
// here); the questions ride to admission verbatim, where the batch contract
// is enforced for every door at once; the thread is the loop's stamp.
func (l *agentLoop) dispatchAsk(ctx context.Context, c *toolRun, args map[string]any) (string, bool) {
	if !l.emitAllows(vocabulary.KindLLMInteraction) {
		return toolError(vocabulary.KindLLMInteraction + " is not in the agent's effective emit allowlist"), false
	}
	questions, ok := args["questions"].([]any)
	if !ok || len(questions) == 0 {
		return toolError("questions is required — a list of {id, prompt, options?, multi?}"), false
	}
	id, err := newID()
	if err != nil {
		return toolError(err.Error()), false
	}
	err = l.ds.runRecordTx(ctx, l.actor, exemptAsk, func(t *txn) error {
		t.causedBy = l.in.causedBy
		t.changeSink = &c.changes
		t.interactionThread = true
		_, err := t.put(substrate.PutInput{
			Kind: vocabulary.KindLLMInteraction, ID: id,
			Properties: map[string]any{
				"questions":  questions,
				msgRelThread: l.threadID,
			},
		})
		return err
	})
	if err != nil {
		return toolError(err.Error()), false
	}
	l.in.tally.addEffect("ask")
	return toolJSON(map[string]any{"id": id}), true
}
