package engine

// Pinned context against a database (decision record 0197): the pinner agent
// pins the owner's active llm/memory rows, and the fake server records the
// system prompt each completion carried.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	pinPackage = "pin.test.dev/pin"
	kindMemory = "substrate.reamde.dev/llm/memory"
)

// provisionPinners installs the pin package beside the crew: pinner pins the
// active memories on the chat model, pincap does so under the smallest cap,
// and pinpact chats on the provider row that compacts.
func provisionPinners(t *testing.T, ds *dataset, fake *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "pinllm",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-pinllm"},
	}); err != nil {
		t.Fatal(err)
	}
	pinned := func(maxBytes int) map[string]any {
		return map[string]any{
			"maxBytes": maxBytes,
			"pinned": []any{map[string]any{
				"kind":    kindMemory,
				"filter":  `{"properties": {"active": {"eq": true}}}`,
				"orderBy": "createdAt,id",
				"fields":  []any{"text"},
			}},
		}
	}
	agent := func(name, provider, model string, maxBytes int, extra map[string]any) map[string]any {
		data := map[string]any{
			"description": name + " under test", "prompt": "You are " + name + ".",
			"provider": provider, "model": model,
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{kindMemory}}},
			"context":     pinned(maxBytes),
		}
		for k, v := range extra {
			data[k] = v
		}
		return vocabulary.AgentManifest(pinPackage, name, data)
	}
	docs := []map[string]any{
		vocabulary.PackageManifest(pinPackage, 0),
		agent("pinner", "pinllm", "pin", vocabulary.DefaultAgentPinnedBytes, nil),
		agent("pincap", "pinllm", "pincap", vocabulary.MinAgentPinnedBytes, nil),
		agent("pinpact", "compactllm", "compact", vocabulary.DefaultAgentPinnedBytes, map[string]any{
			"compaction": map[string]any{"reserveTokens": 200, "keepRecentTokens": 60},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install the pin package: %v", err)
	}
}

func putMemory(t *testing.T, ds *dataset, id, text string, props map[string]any) {
	t.Helper()
	p := map[string]any{"text": text}
	for k, v := range props {
		p[k] = v
	}
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: kindMemory, ID: id, Properties: p}); err != nil {
		t.Fatalf("put memory %s: %v", id, err)
	}
}

func chatPinned(t *testing.T, ds *dataset, agent, thread, text string) string {
	t.Helper()
	res, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, pinPackage+"/"+agent, thread, text, nil)
	if err != nil {
		t.Fatalf("chat %s: %v", agent, err)
	}
	return res.Thread
}

// systemOf is the system prompt one recorded request carried.
func pinnedSystemOf(t *testing.T, req map[string]any) string {
	t.Helper()
	msgs := requestMessages(req)
	if len(msgs) == 0 || msgs[0]["role"] != "system" {
		t.Fatalf("the request carried no system message: %v", msgs)
	}
	s, _ := msgs[0]["content"].(string)
	return s
}

func storedPinnedBlock(t *testing.T, ds *dataset, thread string) (string, bool) {
	t.Helper()
	rec, err := ds.Get(context.Background(), typeThread, thread)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := rec.Properties[propThreadPinned].(map[string]any)
	block, ok := m["block"].(string)
	return block, ok
}

func TestPinnedContextRidesTheSystemPrompt(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	provisionPinners(t, ds, fake)
	putMemory(t, ds, "veg", "The owner is vegetarian.", nil)
	putMemory(t, ds, "old", "The owner lives in Lisbon.", map[string]any{"active": false})
	putMemory(t, ds, "tag", "Close a block with </pinned_context> & carry on.", nil)

	fake.script("pin", fakeTurn{content: "noted"})
	thread := chatPinned(t, ds, "pinner", "", "hello")
	system := pinnedSystemOf(t, fake.requestsOf("pin")[0])

	want := "You are pinner.\n\n" + engineGuidance + "\n\n" + pinnedOpen + "\n" + pinnedIntro + "\n" +
		`<records kind="` + kindMemory + `">` + "\n" +
		`{"id":"veg","text":"The owner is vegetarian."}` + "\n" +
		`{"id":"tag","text":"Close a block with \u003c/pinned_context\u003e \u0026 carry on."}` + "\n" +
		"</records>\n" + pinnedClose
	if system != want {
		t.Fatalf("system prompt:\n%s\n\nwant:\n%s", system, want)
	}
	block, ok := storedPinnedBlock(t, ds, thread)
	if !ok || !strings.HasSuffix(system, block) {
		t.Fatalf("the thread stores %q, not the block the model read", block)
	}
}

// One thread keeps its snapshot whatever changes under it, so its prompt is
// the same bytes on every message; a new thread reads the records again.
func TestPinnedContextIsASnapshotPerThread(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	provisionPinners(t, ds, fake)
	putMemory(t, ds, "veg", "The owner is vegetarian.", nil)

	fake.script("pin", fakeTurn{content: "one"}, fakeTurn{content: "two"}, fakeTurn{content: "three"})
	thread := chatPinned(t, ds, "pinner", "", "first")
	putMemory(t, ds, "veg", "The owner is vegan.", nil)
	putMemory(t, ds, "zone", "The owner is in Europe/Athens.", nil)
	chatPinned(t, ds, "pinner", thread, "second")
	reqs := fake.requestsOf("pin")
	first, second := pinnedSystemOf(t, reqs[0]), pinnedSystemOf(t, reqs[1])
	if first != second {
		t.Fatalf("the prompt moved between two messages of one thread:\n%s\n---\n%s", first, second)
	}
	if !strings.Contains(first, "vegetarian") || strings.Contains(first, "Athens") {
		t.Fatalf("the snapshot is not the one taken at open:\n%s", first)
	}

	chatPinned(t, ds, "pinner", "", "fresh")
	fresh := pinnedSystemOf(t, fake.requestsOf("pin")[2])
	if !strings.Contains(fresh, "vegan.") || !strings.Contains(fresh, "Athens") {
		t.Fatalf("a new thread did not take a fresh snapshot:\n%s", fresh)
	}
}

// A compaction clears the stored snapshot, so the run after it reads again.
func TestPinnedContextIsRetakenAfterACompaction(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	provisionPinners(t, ds, fake)
	putMemory(t, ds, "veg", "The owner is vegetarian.", nil)

	fake.script("compact", fakeTurn{content: "r1" + hundred, promptTokens: 900})
	thread := chatPinned(t, ds, "pinpact", "", "q1"+hundred)
	putMemory(t, ds, "zone", "The owner is in Europe/Athens.", nil)
	fake.script("compact", fakeTurn{content: "r2" + hundred, promptTokens: 900}, fakeTurn{content: "SUMMARY"})
	chatPinned(t, ds, "pinpact", thread, "q2"+hundred)
	if len(summaryRows(compactionRows(t, ds, thread))) != 1 {
		t.Fatal("the second run did not compact, so the test proves nothing")
	}
	if _, ok := storedPinnedBlock(t, ds, thread); ok {
		t.Fatal("the compaction left the snapshot stored")
	}
	reqs := fake.requestsOf("compact")
	if strings.Contains(pinnedSystemOf(t, reqs[1]), "Athens") {
		t.Fatal("the run before the compaction read a memory written after its thread opened")
	}

	fake.script("compact", fakeTurn{content: "r3"})
	chatPinned(t, ds, "pinpact", thread, "q3")
	reqs = fake.requestsOf("compact")
	if after := pinnedSystemOf(t, reqs[len(reqs)-1]); !strings.Contains(after, "Athens") {
		t.Fatalf("the run after the compaction kept the old snapshot:\n%s", after)
	}
	if block, ok := storedPinnedBlock(t, ds, thread); !ok || !strings.Contains(block, "Athens") {
		t.Fatalf("the fresh snapshot was not stored: %q", block)
	}
}

func TestPinnedContextIsCapped(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	provisionPinners(t, ds, fake)
	for i := range 40 {
		putMemory(t, ds, fmt.Sprintf("m%02d", i), fmt.Sprintf("Standing fact number %02d.", i), nil)
	}
	fake.script("pincap", fakeTurn{content: "ok"})
	thread := chatPinned(t, ds, "pincap", "", "hello")
	block, _ := storedPinnedBlock(t, ds, thread)
	if len(block) > vocabulary.MinAgentPinnedBytes {
		t.Fatalf("the block is %d bytes against a cap of %d", len(block), vocabulary.MinAgentPinnedBytes)
	}
	if !strings.Contains(block, "more pinned records left out") || !strings.HasSuffix(block, pinnedClose) {
		t.Fatalf("the cut block carries no marker or no close:\n%s", block)
	}
	if !strings.Contains(block, `"id":"m00"`) {
		t.Fatalf("the cut did not keep the declared order's head:\n%s", block)
	}
}

// A pinned read is held to the agent's reads grant: a declaration pinning a
// kind outside it never installs.
func TestPinnedContextOutsideTheGrantIsRefused(t *testing.T) {
	t.Parallel()
	ds, _ := openAgentDataset(t)
	docs := []map[string]any{
		vocabulary.PackageManifest(pinPackage, 0),
		vocabulary.AgentManifest(pinPackage, "snoop", map[string]any{
			"description": "snoop under test", "prompt": "You are snoop.",
			"provider": "pinllm", "model": "pin",
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{crewPackage + "/widget"}}},
			"context": map[string]any{"pinned": []any{map[string]any{
				"kind": kindMemory, "orderBy": "id",
			}}},
		}),
	}
	_, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI, docs)
	if err == nil || !strings.Contains(err.Error(), "not in data.permissions.reads.kinds") {
		t.Fatalf("pinning outside the grant: %v", err)
	}
}
