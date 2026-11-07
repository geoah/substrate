package engine

// The owner's instructions to one agent (decision record 0241): read into
// its system prompt when a thread opens, after the engine's guidance and
// before what it pins; written by the owner, or by an agent only through a
// proposal the owner accepts.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The shipped assistant's agents, the same in every repository.
const (
	mossID      = vocabulary.PackageAssistant + "/moss"
	architectID = vocabulary.PackageAssistant + "/architect"
)

func putInstruction(t *testing.T, ds *dataset, agent, text string, extra map[string]any) {
	t.Helper()
	props := map[string]any{"agent": vocabulary.RecordPath(kindAgent, agent), "text": text}
	for k, v := range extra {
		props[k] = v
	}
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: vocabulary.KindLLMInstruction, Properties: props}); err != nil {
		t.Fatalf("put instruction: %v", err)
	}
}

// instructionsOf is the owner_instructions block of a system prompt, empty
// when it carries none.
func instructionsOf(system string) string {
	start := strings.Index(system, instructionsOpen)
	end := strings.Index(system, instructionsClose)
	if start < 0 || end < start {
		return ""
	}
	return system[start : end+len(instructionsClose)]
}

func TestOwnerInstructionsRideTheirAgentsSystemPrompt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, _ := openMossDataset(t)
	putInstruction(t, ds, mossID, "Answer in plain paragraphs, never bullet points.", nil)
	putInstruction(t, ds, mossID, "Speak French.", map[string]any{"active": false})
	putInstruction(t, ds, architectID, "Give every new kind an optional notes property.", nil)
	putMemory(t, ds, fid("veg"), "The owner is vegetarian.", nil)

	fake.script(mossModel, fakeTurn{content: "fine"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, mossID, substrate.ChatInput{Message: "hi"}, nil); err != nil {
		t.Fatalf("chat: %v", err)
	}
	system := pinnedSystemOf(t, fake.requestsOf(mossModel)[0])
	block := instructionsOf(system)
	if !strings.Contains(block, "Answer in plain paragraphs, never bullet points.") {
		t.Fatalf("the active instruction is not in the prompt:\n%s", system)
	}
	if strings.Contains(block, "Speak French") || strings.Contains(block, "notes property") {
		t.Fatalf("an inactive instruction, or another agent's, reached the prompt:\n%s", block)
	}
	guidance, instructions, pinned := strings.Index(system, engineGuidance), strings.Index(system, instructionsOpen), strings.Index(system, pinnedOpen)
	if guidance < 0 || pinned < 0 || !(guidance < instructions && instructions < pinned) {
		t.Fatalf("want the guidance, then the instructions, then the pinned memories; at %d, %d, %d", guidance, instructions, pinned)
	}
}

// Moss changes how it works only with the owner: its glob grant cannot write
// an instruction, its proposal can, and the owner's accept is what the next
// thread reads.
func TestAnAgentChangesHowItWorksOnlyThroughTheOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, _ := openMossDataset(t)
	agentPath := vocabulary.RecordPath(kindAgent, mossID)
	instruction := map[string]any{"agent": agentPath, "text": "Go by Juniper.", "source": "agent"}
	fake.script(mossModel,
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", vocabulary.KindLLMInstruction, "", instruction)}}},
		fakeTurn{calls: []fakeCall{{"propose", toolArgs(t, map[string]any{
			"op": "create", "kind": vocabulary.KindLLMInstruction,
			"diff": map[string]any{"properties": instruction}, "rationale": "the owner asked",
		})}}},
		fakeTurn{content: "proposed"},
		fakeTurn{content: "noted"},
		fakeTurn{content: "I'm Juniper."},
	)
	res, err := ds.ChatAgent(ctx, substrate.ActorAPI, mossID, substrate.ChatInput{Message: "go by Juniper from now on"}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	rows := toolRows(t, ds, res.Thread)
	if content, _ := rows[0]["content"].(string); rows[0]["ok"] != false || !strings.Contains(content, "is the instruction kind") {
		t.Fatalf("write of an instruction under *: ok %v, %s", rows[0]["ok"], content)
	}
	if rows[1]["ok"] != true {
		t.Fatalf("propose: %v", rows[1]["content"])
	}

	req := onlyPatchRequest(t, ds)
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	waitUntil(t, "the resume after the accept", func() bool {
		replies := 0
		for _, m := range threadMessages(t, ds, res.Thread) {
			if m["role"] == "assistant" && m["toolCalls"] == nil {
				replies++
			}
		}
		return replies >= 2
	})

	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, mossID, substrate.ChatInput{Message: "who are you?"}, nil); err != nil {
		t.Fatalf("second chat: %v", err)
	}
	reqs := fake.requestsOf(mossModel)
	if block := instructionsOf(pinnedSystemOf(t, reqs[len(reqs)-1])); !strings.Contains(block, "Go by Juniper.") {
		t.Fatalf("the accepted instruction is not in the next thread's prompt:\n%s", block)
	}
}

// A repository born before the assistant shipped receives it at the next
// open: the shipped agents and the instruction kind, as the seed writes them.
func TestBootUpgradeDeliversTheAssistant(t *testing.T) {
	ctx := context.Background()
	dsn := MigratedDSN(t)
	openWith := func(tree Option) substrate.Service {
		svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey), tree)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	before := t.TempDir()
	if err := os.CopyFS(before, SeedKinds); err != nil {
		t.Fatalf("copy the seed tree: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(before, vocabulary.PackageAssistant)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(before, vocabulary.KindLLMInstruction+".yaml")); err != nil {
		t.Fatal(err)
	}
	svc := openWith(WithKindsDir(before))
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	old, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Get(ctx, kindAgent, mossID); err == nil {
		t.Fatal("the tree before the assistant already seeded Moss")
	}
	_ = svc.Close()

	ds, err := openWith(WithKindsFS(SeedKinds)).Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{mossID, architectID, vocabulary.PackageAssistant + "/librarian"} {
		if _, err := ds.Get(ctx, kindAgent, id); err != nil {
			t.Fatalf("the upgrade did not deliver %s: %v", id, err)
		}
	}
	if _, err := ds.Get(ctx, kindKind, vocabulary.KindLLMInstruction); err != nil {
		t.Fatalf("the upgrade did not deliver the instruction kind: %v", err)
	}
}
