package engine

// The llminteraction regressions (docs/plans/thread-interactions.md phase 2):
// the ask built-in lands a thread-stamped batch, admission judges the batch
// at every door, answers ride the answering transition alone and validate
// against the STORED questions, only the owner resolves, and the resolution
// reports back and resumes exactly like a decision.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func onlyInteraction(t *testing.T, ds *dataset) *substrate.Record {
	t.Helper()
	var id string
	if err := ds.db.QueryRowContext(context.Background(), `
		SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL`,
		vocabulary.KindLLMInteraction).Scan(&id); err != nil {
		t.Fatalf("no interaction landed: %v", err)
	}
	e, err := ds.Get(context.Background(), vocabulary.KindLLMInteraction, id)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

const askArgs = `{"questions":[
	{"id":"color","prompt":"Which color?","options":[{"value":"red","label":"Red"},{"value":"blue"}]},
	{"id":"sure","prompt":"Are you sure?"}
]}`

func TestAskLandsAnInteractionAndTheAnswerResumes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("askm",
		fakeTurn{calls: []fakeCall{{"ask", askArgs}}},
		fakeTurn{content: "asked."},
		fakeTurn{content: "noted: red, and yes."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/poller", "pick a color for me")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	interaction := onlyInteraction(t, ds)
	if got := storedReferencePath(interaction.Properties["thread"]); got != vocabulary.RecordPath(typeThread, res.Thread) {
		t.Fatalf("interaction thread = %v, want the asking thread", got)
	}
	if interaction.Properties["state"] != interactionPending {
		t.Fatalf("state = %v", interaction.Properties["state"])
	}
	// The yes/no question stored MATERIALIZED options: validation and replay
	// never rest on a convention.
	questions, _ := interaction.Properties["questions"].([]any)
	if len(questions) != 2 {
		t.Fatalf("questions: %d", len(questions))
	}
	sure, _ := questions[1].(map[string]any)
	opts, _ := sure["options"].([]any)
	if len(opts) != 2 {
		t.Fatalf("the yes/no question stored no materialized options: %+v", sure)
	}
	// The ask tool row carries the interaction's changelog entry.
	tool := lastToolMessage(t, ds, res.Thread)
	entry, ok := changesName(changesOfRow(tool), vocabulary.KindLLMInteraction)
	if !ok || entry["id"] != interaction.ID {
		t.Fatalf("the ask row carries no interaction change: %+v", tool["changes"])
	}

	// The owner answers: one CAS'd transition carrying the answers.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
		Properties: map[string]any{
			"state": interactionAnswered,
			"answers": []any{
				map[string]any{"question": "color", "selected": []any{"red"}},
				map[string]any{"question": "sure", "selected": []any{"yes"}},
			},
		},
		IfVersion: &interaction.Version,
	}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	system := systemMessages(t, ds, res.Thread)
	if len(system) != 1 {
		t.Fatalf("system messages: %d, want 1", len(system))
	}
	content, _ := system[0]["content"].(string)
	var env map[string]any
	if err := json.Unmarshal([]byte(content), &env); err != nil {
		t.Fatalf("the resolution is not an envelope: %q", content)
	}
	if env["event"] != "interactionAnswered" || env["answers"] == nil {
		t.Fatalf("envelope: %+v", env)
	}
	waitUntil(t, "the answer's resume", func() bool {
		for _, c := range assistantContents(t, ds, res.Thread) {
			if c == "noted: red, and yes." {
				return true
			}
		}
		return false
	})
	// The record is frozen after resolution: a later answers rewrite refuses.
	fresh, err := ds.Get(ctx, vocabulary.KindLLMInteraction, interaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
		Properties: map[string]any{"answers": []any{
			map[string]any{"question": "color", "selected": []any{"blue"}},
			map[string]any{"question": "sure", "selected": []any{"yes"}},
		}},
		IfVersion: &fresh.Version,
	}); err == nil {
		t.Fatal("a resolved interaction's answers were rewritten")
	}
}

func TestAskDismissalReportsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("askm",
		fakeTurn{calls: []fakeCall{{"ask", askArgs}}},
		fakeTurn{content: "asked."},
		fakeTurn{content: "understood, moving on."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/poller", "pick a color for me")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	interaction := onlyInteraction(t, ds)
	// A dismissal carries no answers.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
		Properties: map[string]any{
			"state": interactionDismissed,
			"answers": []any{
				map[string]any{"question": "color", "selected": []any{"red"}},
			},
		},
		IfVersion: &interaction.Version,
	}); err == nil {
		t.Fatal("a dismissal carried answers")
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionDismissed},
		IfVersion:  &interaction.Version,
	}); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	system := systemMessages(t, ds, res.Thread)
	if len(system) != 1 {
		t.Fatalf("system messages: %d", len(system))
	}
	if content, _ := system[0]["content"].(string); !jsonHasEvent(t, content, "interactionDismissed") {
		t.Fatalf("envelope: %s", content)
	}
	waitUntil(t, "the dismissal's resume", func() bool {
		for _, c := range assistantContents(t, ds, res.Thread) {
			if c == "understood, moving on." {
				return true
			}
		}
		return false
	})
}

func jsonHasEvent(t *testing.T, content, want string) bool {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal([]byte(content), &env); err != nil {
		return false
	}
	return env["event"] == want
}

func TestInteractionAdmissionAndGuards(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)

	// Generic creates cannot exist: thread is loop-stamped AND required, so
	// both roads refuse with their own reason.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindLLMInteraction,
		Properties: map[string]any{
			"thread":    "somethread",
			"questions": []any{map[string]any{"id": "q", "prompt": "?"}},
		},
	}); err == nil {
		t.Fatal("a generic write stamped an interaction's thread")
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindLLMInteraction,
		Properties: map[string]any{
			"questions": []any{map[string]any{"id": "q", "prompt": "?"}},
		},
	}); err == nil {
		t.Fatal("a threadless interaction landed")
	}

	// The batch contract, judged through the loop's own door: each bad batch
	// is a tool error the model sees, and no interaction lands.
	bad := []string{
		`{"questions":[{"id":"a","prompt":"?"},{"id":"a","prompt":"again?"}]}`,            // duplicate ids
		`{"questions":[{"id":"a","prompt":"?","options":[{"value":"x"},{"value":"x"}]}]}`, // duplicate values
		`{"questions":[{"id":"a","prompt":"?","multi":true}]}`,                            // multi without options
		`{"questions":[{"id":"a"}]}`,                                                      // no prompt
		`{"questions":[{"id":"q1","prompt":"?"},{"id":"q2","prompt":"?"},{"id":"q3","prompt":"?"},{"id":"q4","prompt":"?"},{"id":"q5","prompt":"?"},{"id":"q6","prompt":"?"},{"id":"q7","prompt":"?"},{"id":"q8","prompt":"?"},{"id":"q9","prompt":"?"}]}`, // nine questions
	}
	turns := make([]fakeTurn, 0, len(bad)+1)
	for _, args := range bad {
		turns = append(turns, fakeTurn{calls: []fakeCall{{"ask", args}}})
	}
	turns = append(turns, fakeTurn{content: "gave up."})
	fake.script("askm", turns...)
	res, err := ds.CallAgent(ctx, crewAuthority+"/poller", "ask badly")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var n int
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL`,
		vocabulary.KindLLMInteraction).Scan(&n); err != nil || n != 0 {
		t.Fatalf("bad batches landed %d interactions: %v", n, err)
	}
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["role"] == "tool" && m["ok"] == true {
			t.Fatalf("a bad batch reported ok: %v", m["content"])
		}
	}
}

func TestOnlyTheOwnerResolvesAnInteraction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("askm",
		fakeTurn{calls: []fakeCall{{"ask", askArgs}}},
		fakeTurn{content: "asked."},
	)
	if _, err := ds.CallAgent(ctx, crewAuthority+"/poller", "ask away"); err != nil {
		t.Fatalf("call: %v", err)
	}
	interaction := onlyInteraction(t, ds)

	// A bundle actor answering — even with the interaction kind in its emit —
	// refuses: asks are always the user's.
	fake.script("med",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation Meddle($id: ID!, $input: JSON!) {
  patch(kind: "core.substrate.reamde.dev/llminteraction", id: $id, input: $input) { id }
}`,
			"variables": map[string]any{"id": interaction.ID, "input": map[string]any{
				"properties": map[string]any{
					"state": "answered",
					"answers": []any{
						map[string]any{"question": "color", "selected": []any{"red"}},
						map[string]any{"question": "sure", "selected": []any{"yes"}},
					},
				},
			}},
		})}}},
		fakeTurn{content: "tried."},
	)
	mres, err := ds.CallAgent(ctx, crewAuthority+"/meddler", "answer it yourself")
	if err != nil {
		t.Fatalf("meddler call: %v", err)
	}
	tool := lastToolMessage(t, ds, mres.Thread)
	if tool["ok"] == true {
		t.Fatalf("installed code resolved an interaction: %v", tool["content"])
	}
	fresh, err := ds.Get(ctx, vocabulary.KindLLMInteraction, interaction.ID)
	if err != nil || fresh.Properties["state"] != interactionPending {
		t.Fatalf("interaction after the meddling: %+v %v", fresh.Properties, err)
	}

	// The owner path demands ifVersion (the marker's generalized rule) and
	// full, valid answers.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionAnswered, "answers": []any{
			map[string]any{"question": "color", "selected": []any{"red"}},
			map[string]any{"question": "sure", "selected": []any{"yes"}},
		}},
	}); err == nil {
		t.Fatal("an answer landed without ifVersion")
	}
	for _, answers := range [][]any{
		{map[string]any{"question": "color", "selected": []any{"red"}}},                                                                       // half the batch
		{map[string]any{"question": "color", "selected": []any{"green"}}, map[string]any{"question": "sure", "selected": []any{"yes"}}},       // undeclared value
		{map[string]any{"question": "color", "selected": []any{"red", "blue"}}, map[string]any{"question": "sure", "selected": []any{"yes"}}}, // two on single-select
	} {
		if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
			Properties: map[string]any{"state": interactionAnswered, "answers": answers},
			IfVersion:  &interaction.Version,
		}); err == nil {
			t.Fatalf("an invalid answer landed: %+v", answers)
		}
	}
	// Answers without the transition refuse too.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
		Properties: map[string]any{"answers": []any{
			map[string]any{"question": "color", "selected": []any{"red"}},
			map[string]any{"question": "sure", "selected": []any{"yes"}},
		}},
		IfVersion: &interaction.Version,
	}); err == nil {
		t.Fatal("answers landed without the answering transition")
	}
}
