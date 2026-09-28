package engine

// Compaction against a database: the compactor agent chats on the one
// provider row that declares a context window (1000 tokens, reserve 200, keep
// 60), the fake server reports whatever context size a turn scripts, and the
// summarizer is the next turn scripted for the same model.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
)

const compactor = crewPackage + "/compactor"

// hundred is a message of about 29 estimated tokens.
var hundred = strings.Repeat("h", 100)

func compactionRows(t *testing.T, ds *dataset, thread string) []threadRow {
	t.Helper()
	rows, err := (&agentLoop{ds: ds, threadID: thread}).loadThreadRows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func summaryRows(rows []threadRow) []threadRow {
	var out []threadRow
	for _, r := range rows {
		if r.role() == msgRoleSummary {
			out = append(out, r)
		}
	}
	return out
}

func rowIDWithContent(t *testing.T, rows []threadRow, role, content string) string {
	t.Helper()
	for _, r := range rows {
		if r.role() == role && r.content() == content {
			return r.id
		}
	}
	t.Fatalf("no %s row with content %.20q", role, content)
	return ""
}

func requestMessages(req map[string]any) []map[string]any {
	raw, _ := req["messages"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, m := range raw {
		mm, _ := m.(map[string]any)
		out = append(out, mm)
	}
	return out
}

func isSummarizerRequest(req map[string]any) bool {
	msgs := requestMessages(req)
	if len(msgs) == 0 {
		return false
	}
	system, _ := msgs[0]["content"].(string)
	return strings.HasPrefix(system, "You are a context summarization assistant")
}

func chat(t *testing.T, ds *dataset, thread, text string) (*substrate.AgentResult, []substrate.AgentEvent) {
	t.Helper()
	var events []substrate.AgentEvent
	res, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, compactor, thread, text, func(ev substrate.AgentEvent) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("chat %.20q: %v", text, err)
	}
	return res, events
}

func TestCompactionAtSettleWritesSummaryAndReplaysIt(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	q1, q2, q3 := "q1"+hundred, "q2"+hundred, "q3"+hundred
	r1, r2, r3 := "r1"+hundred, "r2"+hundred, "r3"+hundred

	// The first run crosses the threshold with nothing to fold yet: one run
	// is all tail.
	fake.script("compact", fakeTurn{content: r1, promptTokens: 900})
	res, events := chat(t, ds, "", q1)
	thread := res.Thread
	for _, ev := range events {
		if ev.Kind == substrate.AgentEventCompacted {
			t.Fatal("a one-run thread compacted")
		}
	}
	if n := len(fake.requestsOf("compact")); n != 1 {
		t.Fatalf("requests after run 1: %d", n)
	}

	fake.script("compact", fakeTurn{content: r2, promptTokens: 900}, fakeTurn{content: "SUMMARY ONE", promptTokens: 77})
	_, events = chat(t, ds, thread, q2)
	reqs := fake.requestsOf("compact")
	if len(reqs) != 3 || !isSummarizerRequest(reqs[2]) {
		t.Fatalf("run 2 made %d requests, the last a summarizer: %v", len(reqs), len(reqs) == 3 && isSummarizerRequest(reqs[2]))
	}
	summarizer := reqs[2]
	if _, ok := summarizer["tools"]; ok {
		t.Fatalf("the summarizer request carried tools: %v", summarizer["tools"])
	}
	if _, ok := reqs[1]["tools"]; !ok {
		t.Fatal("the chat request carried no tools, so the check above proves nothing")
	}
	msgs := requestMessages(summarizer)
	if len(msgs) != 2 || msgs[1]["role"] != "user" || !strings.Contains(msgs[1]["content"].(string), "<conversation>") {
		t.Fatalf("summarizer messages: %v", msgs)
	}
	if summarizer["max_completion_tokens"] != float64(compactionMinOutput) {
		t.Fatalf("summarizer ceiling = %v", summarizer["max_completion_tokens"])
	}
	var compacted *substrate.AgentEvent
	for i := range events {
		if events[i].Kind == substrate.AgentEventCompacted {
			compacted = &events[i]
		}
	}
	// tokensBefore is the size that fired the trigger: the measured context
	// of run 2's first completion plus the reply that landed after it.
	wantBefore := 900 + messageTokens(llm.Message{Content: r2})
	if compacted == nil || compacted.Thread != thread || compacted.TokensBefore != wantBefore || compacted.Covered != 2 {
		t.Fatalf("compacted event: %+v (want tokensBefore %d)", compacted, wantBefore)
	}
	if last := events[len(events)-1]; last.Kind != substrate.AgentEventDone {
		t.Fatalf("the stream did not end with done: %+v", last)
	}

	rows := compactionRows(t, ds, thread)
	summaries := summaryRows(rows)
	if len(summaries) != 1 {
		t.Fatalf("summary rows: %d", len(summaries))
	}
	s := summaries[0]
	covers, _ := s.props["covers"].(map[string]any)
	if covers["from"] != rowIDWithContent(t, rows, "user", q1) || covers["through"] != rowIDWithContent(t, rows, "assistant", r1) {
		t.Fatalf("covers = %v", covers)
	}
	if intProp(s.props, "tokensBefore") != wantBefore || s.props["model"] != "compact" || intProp(s.props, "promptTokens") != 77 {
		t.Fatalf("summary row: %+v", s.props)
	}
	if s.content() != "SUMMARY ONE" {
		t.Fatalf("summary content = %q", s.content())
	}
	th := agentThreadsOf(t, ds, "compactor")[0]
	// Two runs at 900 and the summarizer's 77.
	if intProp(th, "promptTokens") != 900+900+77 || th["status"] != threadOK {
		t.Fatalf("thread tally: promptTokens %v status %v", th["promptTokens"], th["status"])
	}
	// The covered rows stay: the summary replaces them in replay alone.
	if len(rows) != 5 {
		t.Fatalf("thread rows: %d", len(rows))
	}

	// The next continuation replays the summary where the range began, then
	// the kept tail, then the new turn. Its own settle folds the first
	// summary into a second one.
	fake.script("compact", fakeTurn{content: r3, promptTokens: 900}, fakeTurn{content: "SUMMARY TWO"})
	chat(t, ds, thread, q3)
	reqs = fake.requestsOf("compact")
	msgs = requestMessages(reqs[3])
	var roles, heads []string
	for _, m := range msgs {
		roles = append(roles, m["role"].(string))
		c, _ := m["content"].(string)
		heads = append(heads, c[:min(len(c), 12)])
	}
	if strings.Join(roles, ",") != "system,user,user,assistant,user" {
		t.Fatalf("continuation roles: %v (%v)", roles, heads)
	}
	framing, _ := msgs[1]["content"].(string)
	if !strings.Contains(framing, "<summary>\nSUMMARY ONE\n</summary>") ||
		msgs[2]["content"] != q2 || msgs[3]["content"] != r2 || msgs[4]["content"] != q3 {
		t.Fatalf("continuation messages: %v", heads)
	}
	if !isSummarizerRequest(reqs[4]) || !strings.Contains(requestMessages(reqs[4])[1]["content"].(string), "<previous-summary>\nSUMMARY ONE") {
		t.Fatal("the second compaction did not fold the first summary in")
	}
	rows = compactionRows(t, ds, thread)
	summaries = summaryRows(rows)
	if len(summaries) != 2 {
		t.Fatalf("summary rows: %d", len(summaries))
	}
	covers, _ = summaries[1].props["covers"].(map[string]any)
	if covers["from"] != rowIDWithContent(t, rows, "user", q1) || covers["through"] != rowIDWithContent(t, rows, "assistant", r2) {
		t.Fatalf("the folded summary's covers = %v", covers)
	}
}

func TestCompactionAtOpenUsesTheEstimate(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	// Long questions, short replies, and the fake's default context size: no
	// settle check fires, while the replayed history estimates past 800.
	long1, long2 := "L1"+strings.Repeat("l", 2000), "L2"+strings.Repeat("l", 2000)
	fake.script("compact", fakeTurn{content: "ok one"})
	res, _ := chat(t, ds, "", long1)
	fake.script("compact", fakeTurn{content: "ok two"})
	chat(t, ds, res.Thread, long2)
	if n := len(summaryRows(compactionRows(t, ds, res.Thread))); n != 0 {
		t.Fatalf("summary rows before the third run: %d", n)
	}

	fake.script("compact", fakeTurn{content: "OPEN SUMMARY"}, fakeTurn{content: "ok three"})
	_, events := chat(t, ds, res.Thread, "short question")
	reqs := fake.requestsOf("compact")
	if len(reqs) != 4 || !isSummarizerRequest(reqs[2]) {
		t.Fatalf("the continuation's first request was not the summarizer (%d requests)", len(reqs))
	}
	msgs := requestMessages(reqs[3])
	if len(msgs) != 5 || !strings.Contains(msgs[1]["content"].(string), "OPEN SUMMARY") ||
		msgs[2]["content"] != long2 || msgs[4]["content"] != "short question" {
		t.Fatalf("the completion after the open-time compaction: %d messages", len(msgs))
	}
	if events[0].Kind != substrate.AgentEventThread {
		t.Fatalf("the stream did not start with the thread: %+v", events[0])
	}
	seen := false
	for _, ev := range events {
		seen = seen || ev.Kind == substrate.AgentEventCompacted
	}
	if !seen {
		t.Fatal("no compacted event")
	}
	rows := compactionRows(t, ds, res.Thread)
	summaries := summaryRows(rows)
	if len(summaries) != 1 {
		t.Fatalf("summary rows: %d", len(summaries))
	}
	covers, _ := summaries[0].props["covers"].(map[string]any)
	if covers["from"] != rowIDWithContent(t, rows, "user", long1) {
		t.Fatalf("covers = %v", covers)
	}
	// The new user row was written once.
	n := 0
	for _, r := range rows {
		if r.content() == "short question" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the new user row landed %d times", n)
	}
}

func TestCompactionKeepsAPendingAskRaw(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	fake.script("compact", fakeTurn{calls: []fakeCall{{"ask", askArgs}}}, fakeTurn{content: "asked."})
	res, _ := chat(t, ds, "", "pick a color for me")
	thread := res.Thread
	if onlyInteraction(t, ds).Properties["state"] != interactionPending {
		t.Fatal("the ask is not pending")
	}
	q2, q3 := "q2"+hundred, "q3"+hundred
	fake.script("compact", fakeTurn{content: "r2" + hundred})
	chat(t, ds, thread, q2)
	fake.script("compact", fakeTurn{content: "r3" + hundred, promptTokens: 900}, fakeTurn{content: "TAIL SUMMARY"})
	chat(t, ds, thread, q3)

	rows := compactionRows(t, ds, thread)
	summaries := summaryRows(rows)
	if len(summaries) != 1 {
		t.Fatalf("the pending ask stalled compaction: %d summary rows", len(summaries))
	}
	covers, _ := summaries[0].props["covers"].(map[string]any)
	if covers["from"] != rowIDWithContent(t, rows, "user", q2) {
		t.Fatalf("covers.from = %v, want the run after the ask", covers["from"])
	}

	fake.script("compact", fakeTurn{content: "r4"})
	chat(t, ds, thread, "q4")
	reqs := fake.requestsOf("compact")
	msgs := requestMessages(reqs[len(reqs)-1])
	var contents []string
	for _, m := range msgs[1:] {
		c, _ := m["content"].(string)
		contents = append(contents, c)
	}
	if len(contents) < 4 || contents[0] != "pick a color for me" || contents[1] != "asked." ||
		!strings.Contains(contents[2], "TAIL SUMMARY") {
		t.Fatalf("the ask's run was not replayed raw ahead of the summary: %.40q", contents)
	}
}

func TestCompactionRejectsATruncatedSummary(t *testing.T) {
	t.Parallel()
	var logs syncBuffer
	ds := openInternalDataset(t, WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	fake := provisionAgents(t, ds)
	fake.script("compact", fakeTurn{content: "r1" + hundred})
	res, _ := chat(t, ds, "", "q1"+hundred)
	fake.script("compact", fakeTurn{content: "r2" + hundred, promptTokens: 900},
		fakeTurn{content: "HALF A SUMM", finish: "length"})
	res2, events := chat(t, ds, res.Thread, "q2"+hundred)
	if res2.Status != threadOK {
		t.Fatalf("status = %s", res2.Status)
	}
	for _, ev := range events {
		if ev.Kind == substrate.AgentEventCompacted {
			t.Fatal("a truncated summary emitted compacted")
		}
	}
	if n := len(summaryRows(compactionRows(t, ds, res.Thread))); n != 0 {
		t.Fatalf("a truncated summary landed: %d rows", n)
	}
	if !strings.Contains(logs.String(), "compaction at settle failed") || !strings.Contains(logs.String(), "output ceiling") {
		t.Fatalf("no warning logged: %s", logs.String())
	}
	th := agentThreadsOf(t, ds, "compactor")[0]
	// The refused call was still billed, so it is still charged.
	if th["status"] != threadOK || intProp(th, "promptTokens") != 10+900+10 {
		t.Fatalf("thread: status %v promptTokens %v", th["status"], th["promptTokens"])
	}
}

const overflowBody = `{"error":{"message":"This model's maximum context length is 1000 tokens. However, your messages resulted in 1400 tokens.","type":"invalid_request_error","code":"context_length_exceeded"}}`

func TestCompactionOnOverflowRetriesOnce(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	seed := func() string {
		fake.script("compact", fakeTurn{content: "r1"})
		res, _ := chat(t, ds, "", "q1"+strings.Repeat("o", 200))
		fake.script("compact", fakeTurn{content: "r2"})
		chat(t, ds, res.Thread, "q2"+strings.Repeat("o", 200))
		return res.Thread
	}

	thread := seed()
	before := len(fake.requestsOf("compact"))
	fake.script("compact",
		fakeTurn{status: 400, errBody: overflowBody},
		fakeTurn{content: "OVERFLOW SUMMARY"},
		fakeTurn{content: "fits now"})
	res, _ := chat(t, ds, thread, "q3")
	if res.Status != threadOK || res.Reply != "fits now" {
		t.Fatalf("result: %+v", res)
	}
	reqs := fake.requestsOf("compact")[before:]
	if len(reqs) != 3 || !isSummarizerRequest(reqs[1]) {
		t.Fatalf("requests: %d", len(reqs))
	}
	retry := requestMessages(reqs[2])
	if len(retry) != 5 || !strings.Contains(retry[1]["content"].(string), "OVERFLOW SUMMARY") || retry[4]["content"] != "q3" {
		t.Fatalf("the retry's messages: %d", len(retry))
	}
	if n := len(summaryRows(compactionRows(t, ds, thread))); n != 1 {
		t.Fatalf("summary rows: %d", n)
	}

	// A second refusal on the retry settles the thread as an error.
	thread = seed()
	fake.script("compact",
		fakeTurn{status: 400, errBody: overflowBody},
		fakeTurn{content: "ANOTHER SUMMARY"},
		fakeTurn{status: 400, errBody: overflowBody})
	if _, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, compactor, thread, "q3", func(substrate.AgentEvent) {}); err == nil {
		t.Fatal("a second overflow did not fail the run")
	}
	for _, th := range agentThreadsOf(t, ds, "compactor") {
		if th["__id"] == thread && th["status"] != threadError {
			t.Fatalf("status = %v", th["status"])
		}
	}
}

func TestCompactionOverflowRetriesOnTheLastTurn(t *testing.T) {
	t.Parallel()
	// compactorone has maxTurns 1. The provider refuses the one completion
	// the run may spend; compaction hands that turn back, so the compacted
	// retry still goes out instead of the run settling overbudget.
	ds, fake := openAgentDataset(t)
	one := crewPackage + "/compactorone"
	chatOne := func(thread, text string) *substrate.AgentResult {
		res, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, one, thread, text, func(substrate.AgentEvent) {})
		if err != nil {
			t.Fatalf("chat %.20q: %v", text, err)
		}
		return res
	}
	fake.script("compact", fakeTurn{content: "r1"})
	thread := chatOne("", "q1"+strings.Repeat("o", 200)).Thread
	fake.script("compact", fakeTurn{content: "r2"})
	chatOne(thread, "q2"+strings.Repeat("o", 200))

	before := len(fake.requestsOf("compact"))
	fake.script("compact",
		fakeTurn{status: 400, errBody: overflowBody},
		fakeTurn{content: "LAST TURN SUMMARY"},
		fakeTurn{content: "fits now"})
	res := chatOne(thread, "q3")
	if res.Status != threadOK || res.Reply != "fits now" || res.Turns != 1 {
		t.Fatalf("result: %+v", res)
	}
	reqs := fake.requestsOf("compact")[before:]
	if len(reqs) != 3 || !isSummarizerRequest(reqs[1]) {
		t.Fatalf("requests: %d", len(reqs))
	}
	if n := len(summaryRows(compactionRows(t, ds, thread))); n != 1 {
		t.Fatalf("summary rows: %d", n)
	}
}
