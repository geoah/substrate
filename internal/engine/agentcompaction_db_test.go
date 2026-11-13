package engine

// Compaction against a database: the compactor agent chats on the one
// provider row that declares a context window (1000 tokens, reserve 200, keep
// 60), the fake server reports whatever context size a turn scripts, and the
// summarizer is the next turn scripted for the same model.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	res, err := ds.ChatAgent(context.Background(), substrate.ActorPerson, compactor, substrate.ChatInput{Thread: thread, Message: text}, func(ev substrate.AgentEvent) {
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
		c := msgText(m)
		heads = append(heads, c[:min(len(c), 12)])
	}
	if strings.Join(roles, ",") != "system,user,user,assistant,user" {
		t.Fatalf("continuation roles: %v (%v)", roles, heads)
	}
	framing, _ := msgs[1]["content"].(string)
	if !strings.Contains(framing, "<summary>\nSUMMARY ONE\n</summary>") ||
		msgText(msgs[2]) != q2 || msgText(msgs[3]) != r2 || msgText(msgs[4]) != q3 {
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
		msgText(msgs[2]) != long2 || msgText(msgs[4]) != "short question" {
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
		c := msgText(m)
		contents = append(contents, c)
	}
	// The blocked head replays raw, the ask's call and its answer included.
	if len(contents) < 6 || contents[0] != "pick a color for me" || msgs[2]["tool_calls"] == nil ||
		msgs[3]["role"] != "tool" || contents[3] != "asked." ||
		!strings.Contains(contents[4], "TAIL SUMMARY") {
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
	if len(retry) != 5 || !strings.Contains(retry[1]["content"].(string), "OVERFLOW SUMMARY") || msgText(retry[4]) != "q3" {
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
	if _, err := ds.ChatAgent(context.Background(), substrate.ActorPerson, compactor, substrate.ChatInput{Thread: thread, Message: "q3"}, func(substrate.AgentEvent) {}); err == nil {
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
		res, err := ds.ChatAgent(context.Background(), substrate.ActorPerson, one, substrate.ChatInput{Thread: thread, Message: text}, func(substrate.AgentEvent) {})
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

// A completion retried after an answer that broke past its usage reports
// both attempts' prompts added up, which is no measure of the context the
// thread holds: the settle-time check estimates it instead, so a thread well
// under its window is not compacted because one of its calls was retried.
// The retry happens on a one-shot completion, which a resumed thread makes.
func TestARetriedCompletionsSummedPromptDoesNotCompactTheThread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	head := `{"id":"msg_1","type":"message","role":"assistant","model":"compact","content":[{"type":"text","text":"rc` + hundred + `"}],"stop_reason":"end_turn"`
	stream := strings.Join([]string{
		`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_0","type":"message","role":"assistant","model":"compact","content":[],"usage":{"input_tokens":100,"output_tokens":1}}}`,
		`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"r` + hundred + `"}}`,
		`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
		`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
	}, "\n\n") + "\n\n"
	var mu sync.Mutex
	var oneShots, streams, summaries int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		summarizer := strings.Contains(string(body), "You are a context summarization assistant")
		streamed := strings.Contains(string(body), `"stream":true`)
		switch {
		case summarizer:
			summaries++
		case streamed:
			streams++
		default:
			oneShots++
		}
		n := oneShots
		mu.Unlock()
		switch {
		case summarizer:
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"id":"msg_s","type":"message","role":"assistant","model":"compact","content":[{"type":"text","text":"SUMMARY"}],"stop_reason":"end_turn","usage":{"input_tokens":50,"output_tokens":5}}`)
		case streamed:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, stream)
		case n == 1:
			// The resumed run's first attempt: 500 prompt tokens reported,
			// then the body breaks. Its retry reports the same 500 again.
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			partial := head + `,"usage":{"input_tokens":500,"output_tokens":5}`
			_, _ = fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nRetry-After-Ms: 0\r\nContent-Length: %d\r\n\r\n%s",
				len(partial)+1000, partial)
			_ = buf.Flush()
			_ = conn.Close()
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, head+`,"usage":{"input_tokens":500,"output_tokens":5}}`)
		}
	}))
	t.Cleanup(srv.Close)
	// The window is 1000 with the threshold at 800: one attempt's 500 is under
	// it, the two attempts' 1000 over it.
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: typeProvider, ID: "compactllm",
		Properties: map[string]any{
			"wire": "anthropic", "baseURL": srv.URL, "apiKey": "row-key-compactllm",
			"pricing": []any{map[string]any{"model": "compact", "inputPer1M": "1", "outputPer1M": "5", "contextWindow": 1000}},
		},
	}); err != nil {
		t.Fatalf("put llm/provider row compactllm: %v", err)
	}
	// Two chat runs give the thread a history a compaction could fold.
	res, _ := chat(t, ds, "", "q1"+hundred)
	chat(t, ds, res.Thread, "q2"+hundred)
	ds.continueThread(ctx, res.Thread, "")
	mu.Lock()
	defer mu.Unlock()
	if streams != 2 || oneShots != 2 {
		t.Fatalf("the endpoint saw %d streamed and %d one-shot requests, want the chats' two streams, the resumed run's dropped attempt and its retry", streams, oneShots)
	}
	if summaries != 0 {
		t.Fatalf("the thread compacted (%d summarizer requests) on a summed prompt", summaries)
	}
	if rows := summaryRows(compactionRows(t, ds, res.Thread)); len(rows) != 0 {
		t.Fatalf("the thread holds %d summary rows, want none", len(rows))
	}
}
