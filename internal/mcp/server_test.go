package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// fakeAPI records each request and answers the next canned response.
type fakeAPI struct {
	got     []Request
	answers []Response
}

func (f *fakeAPI) Do(_ context.Context, req Request) Response {
	f.got = append(f.got, req)
	if len(f.answers) == 0 {
		return Response{Status: http.StatusOK, Header: http.Header{}, Body: []byte(`{}`)}
	}
	res := f.answers[0]
	f.answers = f.answers[1:]
	if res.Header == nil {
		res.Header = http.Header{}
	}
	return res
}

func handle(t *testing.T, s *Server, api API, msg, header string) (int, map[string]any) {
	t.Helper()
	status, out := s.Handle(context.Background(), api, []byte(msg), header)
	if out == nil {
		return status, nil
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("answer %s: %v", out, err)
	}
	return status, m
}

func errorCode(m map[string]any) float64 {
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

func TestTheProtocolsEdges(t *testing.T) {
	s := NewServer("substrate", "test")
	api := &fakeAPI{}
	for _, c := range []struct {
		name, msg, header string
		status            int
		code              float64
	}{
		{"a batch", `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, "", 400, codeInvalidRequest},
		{"not JSON", `{`, "", 400, codeParse},
		{"not 2.0", `{"jsonrpc":"1.0","id":1,"method":"ping"}`, "", 400, codeInvalidRequest},
		{"an unknown method", `{"jsonrpc":"2.0","id":1,"method":"completion/complete"}`, "", 200, codeMethodNotFound},
		{"discover under its own header", `{"jsonrpc":"2.0","id":1,"method":"server/discover"}`, "2026-07-28", 200, codeMethodNotFound},
		{"a version not spoken", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, "2024-11-05", 400, codeInvalidRequest},
		{"an unknown tool", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope"}}`, "", 200, codeInvalidParams},
	} {
		status, m := handle(t, s, api, c.msg, c.header)
		if status != c.status || errorCode(m) != c.code {
			t.Errorf("%s = %d %v, want %d with code %v", c.name, status, m, c.status, c.code)
		}
	}
	for _, msg := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":7,"result":{}}`,
	} {
		if status, m := handle(t, s, api, msg, ""); status != http.StatusAccepted || m != nil {
			t.Errorf("%s = %d %v, want 202 with no body", msg, status, m)
		}
	}
	if len(api.got) != 0 {
		t.Fatalf("the protocol's edges made %d requests", len(api.got))
	}
}

func TestInitializeNegotiatesAVersionAndCarriesTheRules(t *testing.T) {
	s := NewServer("substrate", "1.2.3")
	_, m := handle(t, s, &fakeAPI{}, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`, "")
	r := m["result"].(map[string]any)
	if r["protocolVersion"] != "2025-03-26" || r["serverInfo"].(map[string]any)["version"] != "1.2.3" {
		t.Fatalf("initialize = %v", r)
	}
	first, _, _ := strings.Cut(r["instructions"].(string), "\n")
	if !strings.Contains(first, "overview") || !strings.Contains(first, "guide") || len(first) > 250 {
		t.Fatalf("the instructions' first line is %q", first)
	}
	// Claude Code keeps the first 4096 characters of a server's instructions.
	if n := len(r["instructions"].(string)); n > 4096 {
		t.Fatalf("the instructions are %d bytes, past the 4096 a client keeps", n)
	}
}

func call(t *testing.T, api *fakeAPI, tool, args string) (string, bool) {
	t.Helper()
	s := NewServer("substrate", "test")
	_, m := handle(t, s, api, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+tool+`","arguments":`+args+`}}`, "")
	r, ok := m["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call %s = %v", tool, m)
	}
	if _, has := r["structuredContent"]; has {
		t.Fatalf("tools/call %s carried structuredContent, which Codex shows alone", tool)
	}
	content := r["content"].([]any)[0].(map[string]any)
	return content["text"].(string), r["isError"].(bool)
}

func TestARefusalIsTheToolsErrorWithTheAPIsWords(t *testing.T) {
	api := &fakeAPI{answers: []Response{{Status: 422, Body: []byte(`{"error":{"code":"validation","problemDetails":[{"path":"properties.due"}]}}`)}}}
	text, isErr := call(t, api, "write", `{"op":"put","kind":"ada.example.com/tasks/task","id":"t1","properties":{"due":"x"}}`)
	if !isErr || !strings.Contains(text, "422") || !strings.Contains(text, "properties.due") {
		t.Fatalf("a refused write = %q", text)
	}
	text, isErr = call(t, &fakeAPI{}, "write", `{"op":"put","kind":"ada.example.com/tasks/task","nope":1}`)
	if !isErr || !strings.Contains(text, "nope") {
		t.Fatalf("an unknown argument = %q, want it named", text)
	}
}

// An answer past the limit is never cut mid-JSON. A read whose size is one
// long value answers with that value cut and named; a mutation's answer that
// still does not fit is a receipt, never an error, since the write took
// effect; a read of many small values is the caller's to page.
func TestAnAnswerPastTheLimitIsShortenedReceiptedOrPaged(t *testing.T) {
	long := `{"records":[{"id":"t1","properties":{"body":"` + strings.Repeat("a", MaxResult) + `"}}]}`
	text, isErr := call(t, &fakeAPI{answers: []Response{{Status: 200, Body: []byte(long)}}}, "query", `{"kind":"ada.example.com/tasks/task"}`)
	var short struct {
		Cut    []string       `json:"cut"`
		Answer map[string]any `json:"answer"`
	}
	if isErr || json.Unmarshal([]byte(text), &short) != nil || len(short.Cut) != 1 || short.Cut[0] != "records[0].properties.body" || len(text) > MaxResult {
		t.Fatalf("a read past the limit = %v %.300s", isErr, text)
	}

	var many strings.Builder
	many.WriteString(`{"id":"r1","kind":"ada.example.com/tasks/task","version":3,"properties":{`)
	for i := range 3000 {
		if i > 0 {
			many.WriteString(",")
		}
		fmt.Fprintf(&many, `"p%d":"short value %d"`, i, i)
	}
	many.WriteString("}}")
	text, isErr = call(t, &fakeAPI{answers: []Response{{Status: 201, Body: []byte(many.String())}}}, "write",
		`{"op":"put","kind":"ada.example.com/tasks/task","properties":{"name":"x"}}`)
	if isErr || !strings.Contains(text, `"id":"r1"`) || !strings.Contains(text, `"version":3`) || !strings.Contains(text, "took effect") {
		t.Fatalf("a write whose answer is past the limit = %v %.300s", isErr, text)
	}

	text, isErr = call(t, &fakeAPI{answers: []Response{{Status: 200, Body: []byte(many.String())}}}, "query", `{"kind":"ada.example.com/tasks/task","id":"r1"}`)
	if !isErr || !strings.Contains(text, "smaller `first`") {
		t.Fatalf("a read of many small values past the limit = %v %.300s", isErr, text)
	}
}

// MCP admits a request id that is a string or an integer, and a message is
// a request, a notification or a response, nothing else.
func TestAMalformedEnvelopeIsRefused(t *testing.T) {
	s := NewServer("substrate", "test")
	for _, msg := range []string{
		`{"jsonrpc":"2.0","id":1}`,
		`{"jsonrpc":"2.0","id":null,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":{},"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1.5,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":7}`,
		`{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":1,"message":"x"}}`,
		`{"jsonrpc":"2.0","method":""}`,
	} {
		api := &fakeAPI{}
		if status, m := handle(t, s, api, msg, ""); status != http.StatusBadRequest || errorCode(m) != codeInvalidRequest || len(api.got) != 0 {
			t.Errorf("%s = %d %v, want 400 invalid request", msg, status, m)
		}
	}
	for _, msg := range []string{`{"jsonrpc":"2.0","id":"a","method":"ping"}`, `{"jsonrpc":"2.0","id":7,"method":"ping"}`} {
		if status, m := handle(t, s, &fakeAPI{}, msg, ""); status != http.StatusOK || m["error"] != nil {
			t.Errorf("%s = %d %v", msg, status, m)
		}
	}
}

func TestHistoryPagesWithTheGenerationItsCursorNeeds(t *testing.T) {
	if text, isErr := call(t, &fakeAPI{}, "history", `{"before":40}`); !isErr || !strings.Contains(text, "generation") {
		t.Fatalf("before alone = %q", text)
	}
	api := &fakeAPI{}
	if text, isErr := call(t, api, "history", `{"before":40,"generation":"g1","first":5}`); isErr {
		t.Fatal(text)
	}
	if q := api.got[0].Query; q.Get("before") != "40" || q.Get("generation") != "g1" || q.Get("first") != "5" {
		t.Fatalf("history query = %v", q)
	}
}

// The unit table is larger than one result (the shipped one is about 33 KiB
// compact), so the tool pages it, each page with the conversions touching it.
func TestUnitsPagesTheTable(t *testing.T) {
	var units, conversions []string
	for i := range 400 {
		units = append(units, fmt.Sprintf(`{"id":"u%03d","name":"unit number %d with a long enough name","symbol":"s%d","aliases":["alias-%d"],"root":"r","factor":"1"}`, i, i, i, i))
		if i > 0 {
			conversions = append(conversions, fmt.Sprintf(`{"id":"c%d","from":"u%03d","to":"u%03d","factor":"2"}`, i, i-1, i))
		}
	}
	table := `{"revision":7,"units":[` + strings.Join(units, ",") + `],"conversions":[` + strings.Join(conversions, ",") + `],"unseeded":[]}`
	if len(table) <= MaxResult {
		t.Fatalf("the test table is %d bytes, not past the limit", len(table))
	}
	read := func(args string) map[string]any {
		t.Helper()
		text, isErr := call(t, &fakeAPI{answers: []Response{{Status: 200, Body: []byte(table)}}}, "units", args)
		if isErr || len(text) > MaxResult {
			t.Fatalf("units %s = %v %.200s", args, isErr, text)
		}
		var out map[string]any
		_ = json.Unmarshal([]byte(text), &out)
		return out
	}
	seen := 0
	after := ""
	for range 10 {
		page := read(`{"action":"list","after":"` + after + `"}`)
		seen += len(page["units"].([]any))
		cursor, _ := page["cursor"].(string)
		if cursor == "" {
			break
		}
		after = cursor
	}
	if seen != 400 {
		t.Fatalf("paging read %d units, want 400", seen)
	}
	page := read(`{"action":"list","q":"ALIAS-12"}`)
	got := page["units"].([]any)
	if len(got) != 11 || len(page["conversions"].([]any)) == 0 {
		t.Fatalf("q page = %d units", len(got))
	}
}

func TestAFileReadsInChunksToItsEnd(t *testing.T) {
	data := strings.Repeat("é", 30<<10) // two bytes a rune, so a chunk ends on a boundary
	var got strings.Builder
	offset := 0
	for range 10 {
		api := &fakeAPI{answers: []Response{{Status: 200, Header: http.Header{"Content-Type": {"text/plain"}}, Body: []byte(data)}}}
		text, isErr := call(t, api, "files", fmt.Sprintf(`{"action":"read","digest":"d","offset":%d}`, offset))
		var chunk struct {
			Text string `json:"text"`
			Next *int   `json:"next"`
		}
		if isErr || json.Unmarshal([]byte(text), &chunk) != nil || len(text) > MaxResult {
			t.Fatalf("chunk at %d = %.200s", offset, text)
		}
		got.WriteString(chunk.Text)
		if chunk.Next == nil {
			break
		}
		offset = *chunk.Next
	}
	if got.String() != data {
		t.Fatalf("the chunks joined are %d bytes, want the file's %d", got.Len(), len(data))
	}
}

func TestAPropertyReadsAlone(t *testing.T) {
	rec := `{"id":"n1","kind":"ada.example.com/notes/note","version":2,"properties":{"title":"Hi","body":"` + strings.Repeat("x", 9000) + `"}}`
	api := &fakeAPI{answers: []Response{{Status: 200, Body: []byte(rec)}, {Status: 200, Body: []byte(rec)}}}
	text, _ := call(t, api, "query", `{"kind":"ada.example.com/notes/note","id":"n1","property":"body"}`)
	var chunk struct {
		Length int    `json:"length"`
		Value  string `json:"value"`
		Next   int    `json:"next"`
	}
	if json.Unmarshal([]byte(text), &chunk) != nil || chunk.Length != 9000 || chunk.Next != chunkChars || len(chunk.Value) != chunkChars {
		t.Fatalf("the first chunk = %.200s", text)
	}
	text, _ = call(t, api, "query", `{"kind":"ada.example.com/notes/note","id":"n1","property":"body","offset":8192}`)
	if !strings.Contains(text, `"offset":8192`) || strings.Contains(text, `"next"`) {
		t.Fatalf("the last chunk = %.200s", text)
	}
}

func TestEachToolBuildsItsRequest(t *testing.T) {
	const task = "ada.example.com/tasks/task"
	for _, c := range []struct {
		tool, args    string
		method, path  string
		query, body   string
		idempotencyOK string
	}{
		{"describe_kind", `{"kind":"` + task + `"}`, "GET", "/substrate.reamde.dev/core/kind/ada.example.com%2Ftasks%2Ftask", "", "", ""},
		{"query", `{"kind":"` + task + `","id":"a/b"}`, "GET", "/" + task + "/a%2Fb", "", "", ""},
		{
			"query", `{"kind":"` + task + `","filter":{"properties":{"done":{"eq":false}}}}`, "GET", "/records",
			`filter=%7B%22kinds%22%3A%5B%22ada.example.com%2Ftasks%2Ftask%22%5D%2C%22properties%22%3A%7B%22done%22%3A%7B%22eq%22%3Afalse%7D%7D%7D&first=20`, "", "",
		},
		{"query", `{"kinds":["` + task + `"],"q":"milk","mode":"lexical","first":5}`, "GET", "/records", "", "", ""},
		{"history", `{"kind":"` + task + `","id":"t1","values":true}`, "GET", "/changes", "first=20&recordId=t1&recordKind=ada.example.com%2Ftasks%2Ftask&values=1", "", ""},
		{"write", `{"op":"put","kind":"` + task + `","properties":{"name":"x"},"idempotencyKey":"k"}`, "POST", "/records", "", `{"kind":"` + task + `","properties":{"name":"x"}}`, "k"},
		{"write", `{"op":"patch","kind":"` + task + `","id":"t1","properties":{"status":"done"},"ifVersion":3}`, "PATCH", "/" + task + "/t1", "", `{"ifVersion":3,"properties":{"status":"done"}}`, ""},
		{"write", `{"op":"delete","kind":"` + task + `","id":"t1","ifVersion":3}`, "DELETE", "/" + task + "/t1", "ifVersion=3", "", ""},
		{"decide", `{"id":"r1","decision":"accepted","ifVersion":2}`, "PATCH", "/substrate.reamde.dev/core/recordpatchrequest/r1", "", `{"ifVersion":2,"properties":{"decision":"accepted"}}`, ""},
		{"apply", `{"documents":[{"kind":"x"}],"plan":true}`, "POST", "/vocabulary/plan", "", `{"documents":[{"kind":"x"}]}`, ""},
		{"apply", `{"documents":[{"kind":"x"}],"plan":true,"holdWaitingMappings":true}`, "POST", "/vocabulary/plan", "", `{"documents":[{"kind":"x"}],"holdWaitingMappings":true}`, ""},
		{"query", `{"kind":"` + task + `","count":true,"first":1}`, "GET", "/records", `count=1&filter=%7B%22kinds%22%3A%5B%22ada.example.com%2Ftasks%2Ftask%22%5D%7D&first=1`, "", ""},
		{"packages", `{"action":"disable","id":"ada.example.com/notes"}`, "PATCH", "/substrate.reamde.dev/core/bundle/ada.example.com%2Fnotes", "", `{"properties":{"disabled":true}}`, ""},
		{"call", `{"function":"ada.example.com/notes/stats","input":{"text":"hi"},"idempotencyKey":"c"}`, "POST", "/substrate.reamde.dev/core/function/ada.example.com%2Fnotes%2Fstats/call", "", `{"input":{"text":"hi"}}`, "c"},
		{"triggers", `{"action":"wake","id":"ada-sync"}`, "POST", "/substrate.reamde.dev/core/trigger/ada-sync/wake", "", "", ""},
		{"triggers", `{"action":"forget","id":"ada-sync","failure":4}`, "DELETE", "/substrate.reamde.dev/core/trigger/ada-sync/parked/4", "", "", ""},
		{"units", `{"action":"convert","value":"5","from":"km","to":"mi"}`, "GET", "/units/convert", "from=km&to=mi&value=5", "", ""},
		{"merge", `{"action":"split","merge":"m1"}`, "POST", "/split", "", `{"merge":"m1"}`, ""},
	} {
		api := &fakeAPI{}
		text, isErr := call(t, api, c.tool, c.args)
		if isErr || len(api.got) != 1 {
			t.Fatalf("%s %s = %q after %d requests", c.tool, c.args, text, len(api.got))
		}
		got := api.got[0]
		if got.Method != c.method || got.Path != c.path {
			t.Errorf("%s %s requested %s %s, want %s %s", c.tool, c.args, got.Method, got.Path, c.method, c.path)
		}
		if c.query != "" && got.Query.Encode() != c.query {
			t.Errorf("%s %s query = %s, want %s", c.tool, c.args, got.Query.Encode(), c.query)
		}
		if c.body != "" {
			b, _ := json.Marshal(got.Body)
			if string(b) != c.body {
				t.Errorf("%s %s body = %s, want %s", c.tool, c.args, b, c.body)
			}
		}
		if got.IdempotencyKey != c.idempotencyOK {
			t.Errorf("%s %s idempotency key = %q, want %q", c.tool, c.args, got.IdempotencyKey, c.idempotencyOK)
		}
	}
}

// packages' add reads the entry's preview, then takes it; an update that
// keeps some kind's history for less time is refused before the add, naming
// each kind and the console page that lists it for the person (decision 0282).
func TestPackagesAddPreviewsAndRefusesAShorterHistory(t *testing.T) {
	for _, id := range []string{"providers.substrate.reamde.dev/google", "samples.substrate.reamde.dev/tasks"} {
		api := &fakeAPI{}
		if text, isErr := call(t, api, "packages", `{"action":"add","id":"`+id+`"}`); isErr || len(api.got) != 2 {
			t.Fatalf("add %s = %q after %d requests, want the preview and the add", id, text, len(api.got))
		}
		escaped := "/catalog/" + Escape(id)
		if read, add := api.got[0], api.got[1]; read.Method != http.MethodGet || read.Path != escaped || add.Method != http.MethodPost || add.Path != escaped+"/add" {
			t.Fatalf("add %s requested %s %s then %s %s", id, read.Method, read.Path, add.Method, add.Path)
		}
	}
	api := &fakeAPI{answers: []Response{{Status: http.StatusOK, Body: []byte(`{"id":"providers.substrate.reamde.dev/google","upgrade":{"available":true,"retentionShortens":[{"kind":"providers.substrate.reamde.dev/google/contact","from":"all","to":"P30D"}]}}`)}}}
	text, isErr := call(t, api, "packages", `{"action":"add","id":"providers.substrate.reamde.dev/google"}`)
	if !isErr || len(api.got) != 1 {
		t.Fatalf("an add that keeps less history = %q after %d requests, want refused after the preview alone", text, len(api.got))
	}
	for _, want := range []string{"providers.substrate.reamde.dev/google/contact: all -> P30D", "package's page in the console"} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal does not say %q: %s", want, text)
		}
	}
	// A preview that does not read as the shape it expects is no clean one.
	api = &fakeAPI{answers: []Response{{Status: http.StatusOK, Body: []byte(`{"id":"providers.substrate.reamde.dev/google","upgrade":{"available":true,"retentionShortens":"providers.substrate.reamde.dev/google/contact"}}`)}}}
	text, isErr = call(t, api, "packages", `{"action":"add","id":"providers.substrate.reamde.dev/google"}`)
	if !isErr || len(api.got) != 1 || !strings.Contains(text, "could not be read, so nothing is added") {
		t.Fatalf("an add whose preview does not decode = %q after %d requests, want refused after the preview alone", text, len(api.got))
	}
}

// A kind or a function is a full reference, so a path built from it has a
// kind's three segments and lands on the record route.
func TestAToolTakesAKindOnlyAsAFullReference(t *testing.T) {
	for _, args := range []string{
		`{"kind":"task","id":"x"}`,
		`{"kind":"authorizations","id":"x"}`,
		`{"kind":"a/b","id":"x"}`,
		`{"kind":"ada.example.com/tasks/*","id":"x"}`,
		`{"kind":"../../tokens","id":"x"}`,
	} {
		api := &fakeAPI{}
		if text, isErr := call(t, api, "query", args); !isErr || len(api.got) != 0 {
			t.Errorf("query %s = %q after %d requests, want refused before any", args, text, len(api.got))
		}
	}
}

func TestAChatAnswersTheReplyAndTheThreadAsFarAsTheTurnGot(t *testing.T) {
	done := `{"kind":"delta","thread":"th1","text":"Hel"}` + "\n" + `{"kind":"delta","text":"lo"}` + "\n" +
		`{"kind":"toolFinished","tool":"query","ok":true}` + "\n" +
		`{"kind":"done","result":{"reply":"Hello","thread":"th1"}}` + "\n"
	got, err := readTurn([]byte(done))
	if err != nil || got["reply"] != "Hello" || got["thread"] != "th1" || got["status"] != "done" || len(got["tools"].([]map[string]any)) != 1 {
		t.Fatalf("a finished turn = %v, %v", got, err)
	}
	got, _ = readTurn([]byte(`{"kind":"delta","thread":"th2","text":"Work"}` + "\n"))
	if got["status"] != "running" || got["reply"] != "Work" || got["thread"] != "th2" {
		t.Fatalf("a turn still running = %v", got)
	}
	got, _ = readTurn([]byte(`{"kind":"delta","thread":"th3"}` + "\n" + `{"kind":"error","error":"no provider key"}` + "\n"))
	if got["status"] != "failed" || got["error"] != "no provider key" {
		t.Fatalf("a failed turn = %v", got)
	}
}

func TestFilesReadTextAsTextAndBytesAsBase64(t *testing.T) {
	api := &fakeAPI{answers: []Response{
		{Status: 200, Header: http.Header{"Content-Type": {"text/plain"}}, Body: []byte("hello")},
		{Status: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: []byte{0x89, 'P', 'N', 'G'}},
	}}
	text, _ := call(t, api, "files", `{"action":"read","digest":"sha256:a"}`)
	if !strings.Contains(text, `"text":"hello"`) {
		t.Fatalf("a text file = %s", text)
	}
	text, _ = call(t, api, "files", `{"action":"read","digest":"sha256:b"}`)
	if !strings.Contains(text, `"base64":"iVBORw=="`) {
		t.Fatalf("a binary file = %s", text)
	}
	text, isErr := call(t, api, "files", `{"action":"store","content":"aGk=","encoding":"base64","name":"hi.txt","mediaType":"text/plain"}`)
	if isErr || string(api.got[2].Raw) != "hi" || api.got[2].ContentType != "text/plain" || api.got[2].Query.Get("name") != "hi.txt" {
		t.Fatalf("store = %s, request %+v", text, api.got[2])
	}
}
