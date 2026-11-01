package engine

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The turn context (decision 0196): the block after a turn's text, stored on
// its row and replayed verbatim.

// messageParts is a wire user message's text parts: one for a plain string,
// one per part for a message carrying a turn context.
func messageParts(m map[string]any) []string {
	switch c := m["content"].(type) {
	case string:
		return []string{c}
	case []any:
		var out []string
		for _, p := range c {
			pm, _ := p.(map[string]any)
			s, _ := pm["text"].(string)
			out = append(out, s)
		}
		return out
	}
	return nil
}

// userMessagesOf is a request's user messages, in order.
func userMessagesOf(req map[string]any) []map[string]any {
	var out []map[string]any
	for _, m := range requestMessages(req) {
		if m["role"] == "user" {
			out = append(out, m)
		}
	}
	return out
}

func putTimezoneSetting(t *testing.T, ds *dataset, zone string) error {
	t.Helper()
	_, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: kindSetting, ID: timezoneSettingID, Properties: map[string]any{"value": zone},
	})
	return err
}

func TestTurnContextRidesTheUserMessageAndReplaysVerbatim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	widget := crewPackage + "/widget"
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: widget, ID: "w1", Properties: map[string]any{"name": "the <blue> widget"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	fake.script("lib", fakeTurn{content: "it is blue"}, fakeTurn{content: "still blue"})
	res, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/librarian", substrate.ChatInput{
		Message: "what color is this?", Timezone: "Europe/London",
		Context: []string{vocabulary.RecordPath(widget, "w1"), vocabulary.RecordPath(widget, "w1")},
	}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	reqs := fake.requestsOf("lib")
	users := userMessagesOf(reqs[0])
	first := messageParts(users[len(users)-1])
	if len(first) != 2 || first[0] != "what color is this?" {
		t.Fatalf("the user message is not the text then the context: %q", first)
	}
	block := first[1]
	now := regexp.MustCompile(`(?m)^now: \d{4}-\d\d-\d\dT\d\d:\d\d:00[+-]\d\d:\d\d \([A-Z][a-z]+day\), Europe/London$`)
	if !strings.HasPrefix(block, "<turn_context>\n") || !strings.HasSuffix(block, "\n</turn_context>") || !now.MatchString(block) {
		t.Fatalf("the block's frame or clock line is wrong:\n%s", block)
	}
	about := "about: substrate://" + vocabulary.RecordPath(widget, "w1")
	if strings.Count(block, about) != 1 || !strings.Contains(block, `"name":"the <blue> widget"`) {
		t.Fatalf("the block does not render the record once, unescaped:\n%s", block)
	}

	// The row keeps the block and the records it names.
	var row map[string]any
	for _, r := range threadMessages(t, ds, res.Thread) {
		if r["role"] == "user" {
			row = r
		}
	}
	if row[propMsgContext] != block {
		t.Fatalf("the user row's context = %v, want the block sent", row[propMsgContext])
	}
	wantRefs := []any{map[string]any{"kind": widget, "id": "w1"}}
	if !reflect.DeepEqual(row[propMsgContextRecords], wantRefs) {
		t.Fatalf("contextRecords = %v, want %v", row[propMsgContextRecords], wantRefs)
	}

	// A continuation replays the stored block, byte for byte, and the new
	// turn carries its own.
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/librarian", substrate.ChatInput{
		Thread: res.Thread, Message: "and now?",
	}, nil); err != nil {
		t.Fatalf("chat 2: %v", err)
	}
	reqs = fake.requestsOf("lib")
	replayed := userMessagesOf(reqs[1])
	if len(replayed) != 2 || !reflect.DeepEqual(replayed[0], users[len(users)-1]) {
		t.Fatalf("the replayed turn is not the bytes first sent:\n%v\n%v", replayed, users)
	}
	second := messageParts(replayed[1])
	if len(second) != 2 || second[0] != "and now?" || strings.Contains(second[1], "about:") || !strings.Contains(second[1], ", UTC\n") {
		t.Fatalf("the second turn's context = %q, want the clock alone in the repository's zone", second)
	}
}

func TestTurnContextZoneIsTheRequestsThenTheSettingThenUTC(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	chat := func(zone string) (string, error) {
		t.Helper()
		fake.script("chat", fakeTurn{content: "ok"})
		before := len(fake.requestsOf("chat"))
		if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi", Timezone: zone}, nil); err != nil {
			return "", err
		}
		users := userMessagesOf(fake.requestsOf("chat")[before])
		parts := messageParts(users[len(users)-1])
		return parts[len(parts)-1], nil
	}
	zoneOfBlock := func(block string) string {
		line := regexp.MustCompile(`(?m)^now: .*, (.+)$`).FindStringSubmatch(block)
		if line == nil {
			t.Fatalf("no clock line in:\n%s", block)
		}
		return line[1]
	}

	if block, err := chat(""); err != nil || zoneOfBlock(block) != "UTC" {
		t.Fatalf("no zone anywhere: %v, %q", err, block)
	}
	if err := putTimezoneSetting(t, ds, "Asia/Tokyo"); err != nil {
		t.Fatalf("put the zone setting: %v", err)
	}
	if block, err := chat(""); err != nil || zoneOfBlock(block) != "Asia/Tokyo" || !strings.Contains(block, "+09:00 (") {
		t.Fatalf("the setting's zone: %v, %q", err, block)
	}
	if block, err := chat("America/New_York"); err != nil || zoneOfBlock(block) != "America/New_York" {
		t.Fatalf("the request's zone: %v, %q", err, block)
	}

	// A zone nobody can load is refused at both doors, and the chat opens
	// no thread.
	for _, zone := range []string{"Mars/Olympus_Mons", "Local"} {
		if err := putTimezoneSetting(t, ds, zone); !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("setting %q: err = %v, want ErrValidation", zone, err)
		}
		n := len(fake.requestsOf("chat"))
		if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", substrate.ChatInput{Message: "hi", Timezone: zone}, nil); !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("request %q: err = %v, want ErrValidation", zone, err)
		}
		if len(fake.requestsOf("chat")) != n {
			t.Fatalf("a refused zone reached the model")
		}
	}
	if err := putTimezoneSetting(t, ds, ""); err != nil {
		t.Fatalf("an empty zone is UTC and stays admitted: %v", err)
	}
}

func TestTurnContextRefusesARecordTheAgentDoesNotRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	widget := crewPackage + "/widget"
	paths := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = vocabulary.RecordPath(widget, "w1")
		}
		return out
	}
	for _, tc := range []struct {
		name    string
		agent   string
		context []string
		want    error
	}{
		{"a kind outside the grant", "librarian", []string{vocabulary.RecordPath(typeThread, "t1")}, substrate.ErrValidation},
		{"an agent with no reads", "chatter", []string{vocabulary.RecordPath(widget, "w1")}, substrate.ErrValidation},
		{"a record that does not exist", "librarian", []string{vocabulary.RecordPath(widget, "nope")}, substrate.ErrNotFound},
		{"not a record path", "librarian", []string{"widget/w1"}, substrate.ErrValidation},
		{"too many records", "librarian", paths(substrate.MaxChatContextRecords + 1), substrate.ErrValidation},
	} {
		_, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/"+tc.agent, substrate.ChatInput{Message: "look", Context: tc.context}, nil)
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if n := len(fake.requestsOf("lib")) + len(fake.requestsOf("chat")); n != 0 {
		t.Fatalf("a refused turn reached the model %d times", n)
	}
	if threads := agentThreadsOf(t, ds, "librarian"); len(threads) != 0 {
		t.Fatalf("a refused turn opened %d threads", len(threads))
	}
}

// msgText is a wire message's own text, ahead of any turn context.
func msgText(m map[string]any) string {
	if parts := messageParts(m); len(parts) > 0 {
		return parts[0]
	}
	return ""
}
