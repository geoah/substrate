package mcp

import (
	"context"
	"encoding/json"
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
		{"an unknown method", `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`, "", 200, codeMethodNotFound},
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
	if !strings.Contains(first, "kinds tool") || len(first) > 250 {
		t.Fatalf("the instructions' first line, all Codex shows, is %q", first)
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

func TestAnAnswerPastTheLimitIsRefusedWithTheWayToPage(t *testing.T) {
	big := `{"records":[{"properties":{"x":"` + strings.Repeat("a", MaxResult) + `"}}]}`
	text, isErr := call(t, &fakeAPI{answers: []Response{{Status: 200, Body: []byte(big)}}}, "query", `{"kind":"ada.example.com/tasks/task"}`)
	if !isErr || !strings.Contains(text, "smaller `first`") {
		t.Fatalf("a large answer = %q", text)
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
		{"decide", `{"id":"r1","decision":"accepted"}`, "PATCH", "/substrate.reamde.dev/core/recordpatchrequest/r1", "", `{"properties":{"decision":"accepted"}}`, ""},
		{"apply", `{"documents":[{"kind":"x"}],"plan":true}`, "POST", "/vocabulary/plan", "", `{"documents":[{"kind":"x"}]}`, ""},
		{"packages", `{"action":"install","id":"providers.substrate.reamde.dev/google"}`, "POST", "/catalog/providers.substrate.reamde.dev%2Fgoogle/install", "", "", ""},
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

func TestKindsSummarizesTheDeclarations(t *testing.T) {
	page := `{"records":[{"id":"ada.example.com/tasks/task","properties":{"description":"A thing to do. It has a status.","purpose":"primary"}},` +
		`{"id":"substrate.reamde.dev/core/trigger","properties":{"description":"Runs on changes.","purpose":"internal"}},` +
		`{"id":"ada.example.com/notes/note","properties":{}}],"cursor":"c2"}`
	api := &fakeAPI{answers: []Response{{Status: 200, Body: []byte(page)}}}
	text, isErr := call(t, api, "kinds", `{}`)
	if isErr {
		t.Fatal(text)
	}
	var got struct {
		Kinds  []map[string]string `json:"kinds"`
		Cursor string              `json:"cursor"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Kinds) != 3 || got.Kinds[0]["description"] != "A thing to do." || got.Kinds[1]["purpose"] != "internal" ||
		got.Kinds[2]["purpose"] != "primary" || got.Cursor != "c2" {
		t.Fatalf("kinds = %s", text)
	}
	if q := api.got[0].Query.Get("filter"); q != `{"kinds":["substrate.reamde.dev/core/kind"]}` {
		t.Fatalf("filter = %s", q)
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
