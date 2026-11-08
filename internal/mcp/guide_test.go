package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/vocabulary"
)

// Every topic is one the index can describe and one result carries: a
// heading, a one-sentence summary, and a size a model reads in one go.
func TestEveryTopicHasATitleASummaryAndAReadableSize(t *testing.T) {
	if len(Topics) != len(topicNames) {
		t.Fatalf("%d topics for %d names", len(Topics), len(topicNames))
	}
	for _, tp := range Topics {
		if tp.Title == "" || strings.HasPrefix(tp.Title, "#") {
			t.Errorf("%s: title %q", tp.Name, tp.Title)
		}
		if tp.Summary == "" || utf8.RuneCountInString(tp.Summary) > 200 || !strings.HasSuffix(strings.TrimSuffix(tp.Summary, "…"), ".") {
			t.Errorf("%s: summary %q is not one sentence of at most 200 characters", tp.Name, tp.Summary)
		}
		limit := 9000
		if tp.Name == "applets" {
			limit = 12000
		}
		if n := utf8.RuneCountInString(tp.Text); n > limit {
			t.Errorf("%s: %d characters, past the %d one topic may hold", tp.Name, n, limit)
		}
	}
}

// A topic names the tools by the names the door serves, so a model that
// follows it calls a tool that exists.
func TestTheGuideNamesOnlyToolsTheDoorServes(t *testing.T) {
	served := map[string]bool{}
	for _, tool := range Tools() {
		served[tool.Name] = true
	}
	named := regexp.MustCompile("the `?([a-z_]+)`? tool")
	for _, tp := range Topics {
		for _, m := range named.FindAllStringSubmatch(tp.Text, -1) {
			if !served[m[1]] {
				t.Errorf("%s names a tool %q the door does not serve", tp.Name, m[1])
			}
		}
	}
}

// The vocabulary topic names every property type a declaration may take, so
// a type added to the parser is a topic that fails until it says so.
func TestTheVocabularyTopicNamesEveryPropertyType(t *testing.T) {
	tp, _ := topic("vocabulary")
	for _, d := range vocabulary.Datatypes() {
		if d == vocabulary.DatatypeDigest {
			continue // only the substrate writes one
		}
		if !strings.Contains(tp.Text, "`"+string(d)+"`") {
			t.Errorf("the vocabulary topic does not name the property type `%s`", d)
		}
	}
}

// The traits topic names every core trait the seeded vocabulary declares.
func TestTheTraitsTopicNamesEveryCoreTrait(t *testing.T) {
	tp, _ := topic("traits")
	for _, name := range []string{
		"temporal", "recurring", "override", "accountconfig", "oauth2", "sync", "observed",
		"metric", "source", "sourcelink", "result", "revised", "lifecycle",
	} {
		if !strings.Contains(tp.Text, "substrate.reamde.dev/core/"+name) {
			t.Errorf("the traits topic does not name substrate.reamde.dev/core/%s", name)
		}
	}
}

func TestTheGuideListsItsTopicsAndReadsOne(t *testing.T) {
	text, isErr := call(t, &fakeAPI{}, "guide", `{}`)
	var index struct {
		Topics []map[string]string `json:"topics"`
	}
	if isErr || json.Unmarshal([]byte(text), &index) != nil || len(index.Topics) != len(topicNames) || index.Topics[0]["about"] == "" {
		t.Fatalf("the index = %s", text)
	}
	text, isErr = call(t, &fakeAPI{}, "guide", `{"topic":"vocabulary"}`)
	if isErr || !strings.HasPrefix(text, "# ") || strings.HasPrefix(text, `"`) {
		t.Fatalf("a topic, sent as markdown = %.200s", text)
	}
	if text, isErr = call(t, &fakeAPI{}, "guide", `{"topic":"nope"}`); !isErr || !strings.Contains(text, "records") {
		t.Fatalf("an unknown topic = %s", text)
	}
	text, _ = call(t, &fakeAPI{}, "applets", `{"op":"guide"}`)
	if !strings.HasPrefix(text, "# Building an applet") {
		t.Fatalf("the applets tool's guide = %.100s", text)
	}
}

func TestTheTopicsAreResources(t *testing.T) {
	s := NewServer("substrate", "test")
	_, m := handle(t, s, &fakeAPI{}, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, "")
	caps := m["result"].(map[string]any)["capabilities"].(map[string]any)
	if caps["resources"] == nil || caps["prompts"] == nil || caps["tools"] == nil {
		t.Fatalf("capabilities = %v", caps)
	}
	_, m = handle(t, s, &fakeAPI{}, `{"jsonrpc":"2.0","id":2,"method":"resources/list"}`, "")
	list := m["result"].(map[string]any)["resources"].([]any)
	if len(list) != len(topicNames) || list[0].(map[string]any)["uri"] != "substrate://guide/records" {
		t.Fatalf("resources = %v", list)
	}
	_, m = handle(t, s, &fakeAPI{}, `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"substrate://guide/traits"}}`, "")
	contents := m["result"].(map[string]any)["contents"].([]any)[0].(map[string]any)
	if contents["mimeType"] != "text/markdown" || !strings.Contains(contents["text"].(string), "substrate.reamde.dev/core/temporal") {
		t.Fatalf("read = %v", contents)
	}
	_, m = handle(t, s, &fakeAPI{}, `{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"substrate://guide/nope"}}`, "")
	if errorCode(m) != codeResourceNotFound {
		t.Fatalf("an unknown resource = %v", m)
	}
}

func TestThePromptsAreTheOwnersWorkflows(t *testing.T) {
	s := NewServer("substrate", "test")
	_, m := handle(t, s, &fakeAPI{}, `{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`, "")
	list := m["result"].(map[string]any)["prompts"].([]any)
	if len(list) != len(Prompts) {
		t.Fatalf("prompts = %v", list)
	}
	_, m = handle(t, s, &fakeAPI{}, `{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"new_collection","arguments":{"what":"my plants"}}}`, "")
	msg := m["result"].(map[string]any)["messages"].([]any)[0].(map[string]any)
	text := msg["content"].(map[string]any)["text"].(string)
	if msg["role"] != "user" || !strings.Contains(text, "my plants") || !strings.Contains(text, "plan true") {
		t.Fatalf("new_collection = %v", msg)
	}
	_, m = handle(t, s, &fakeAPI{}, `{"jsonrpc":"2.0","id":3,"method":"prompts/get","params":{"name":"new_collection"}}`, "")
	if text := m["result"].(map[string]any)["messages"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"].(string); !strings.Contains(text, "ask me what") {
		t.Fatalf("new_collection without its argument = %s", text)
	}
	for _, bad := range []string{
		`{"name":"nope"}`,
		`{"name":"tour","arguments":{"what":"x"}}`,
	} {
		_, m = handle(t, s, &fakeAPI{}, `{"jsonrpc":"2.0","id":4,"method":"prompts/get","params":`+bad+`}`, "")
		if errorCode(m) != codeInvalidParams {
			t.Errorf("prompts/get %s = %v", bad, m)
		}
	}
	// Each prompt names only tools the door serves.
	served := map[string]bool{}
	for _, tool := range Tools() {
		served[tool.Name] = true
	}
	for _, p := range Prompts {
		text := p.Text("x")
		for _, name := range []string{"overview", "describe_kind", "kinds", "query", "apply", "write", "applets"} {
			if strings.Contains(text, name) && !served[name] {
				t.Errorf("prompt %s names %s", p.Name, name)
			}
		}
	}
}

// overviewAPI answers each list by the kind its filter names.
type overviewAPI struct {
	pages map[string]string
	got   []Request
}

func (o *overviewAPI) Do(_ context.Context, req Request) Response {
	o.got = append(o.got, req)
	var f struct {
		Kinds []string `json:"kinds"`
	}
	_ = json.Unmarshal([]byte(req.Query.Get("filter")), &f)
	kind := ""
	if len(f.Kinds) > 0 {
		kind = f.Kinds[0]
	}
	if req.Query.Get("count") == "1" {
		return Response{Status: http.StatusOK, Body: []byte(`{"records":[],"count":3}`)}
	}
	body, ok := o.pages[kind]
	if !ok {
		return Response{Status: http.StatusNotFound, Body: []byte(`{"error":{"code":"not_found"}}`)}
	}
	return Response{Status: http.StatusOK, Body: []byte(body)}
}

func TestTheOverviewNamesTheAuthorityAndWhatTheOwnerHas(t *testing.T) {
	api := &overviewAPI{pages: map[string]string{
		kindRepository: `{"records":[{"id":"ada.example.com","properties":{"authority":"ada.example.com"}}]}`,
		kindKind: `{"records":[` +
			`{"id":"ada.example.com/garden/plant","properties":{"description":"A plant. It grows."}},` +
			`{"id":"ada.example.com/garden/bed","properties":{"purpose":"supporting"}},` +
			`{"id":"substrate.reamde.dev/core/kind","properties":{"purpose":"internal"}}]}`,
		kindPackage: `{"records":[{"id":"substrate.reamde.dev/core","properties":{"source":"builtin"}},` +
			`{"id":"ada.example.com/garden","properties":{"source":"installed"}}]}`,
		kindAgent: `{"records":[{"id":"substrate.reamde.dev/assistant/moss","properties":{"description":"Your assistant. More."}},` +
			`{"id":"substrate.reamde.dev/assistant/architect","properties":{"hiddenFromChat":true}}]}`,
		kindProvider: `{"records":[{"id":"anthropic","properties":{"apiKey":"<redacted>"}},{"id":"openai","properties":{}}]}`,
		// The applets list fails: the rest stands, and the failure is named.
	}}
	s := NewServer("substrate", "test")
	_, m := handle(t, s, api, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"overview","arguments":{}}}`, "")
	text := m["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var got struct {
		Authority string           `json:"authority"`
		Packages  []map[string]any `json:"packages"`
		Agents    []map[string]any `json:"agents"`
		Providers []map[string]any `json:"llmProviders"`
		Waiting   map[string]any   `json:"waitingOnTheOwner"`
		Guide     []any            `json:"guide"`
		Unread    []string         `json:"unread"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err, text)
	}
	if got.Authority != "ada.example.com" || len(got.Guide) != len(topicNames) {
		t.Fatalf("overview = %s", text)
	}
	if len(got.Packages) != 2 || got.Packages[0]["package"] != "ada.example.com/garden" ||
		got.Packages[0]["kinds"] != float64(2) || got.Packages[0]["primaryKinds"] != float64(1) || got.Packages[0]["source"] != "installed" {
		t.Fatalf("packages = %v", got.Packages)
	}
	if got.Agents[0]["description"] != "Your assistant." || got.Agents[1]["chat"] != false {
		t.Fatalf("agents = %v", got.Agents)
	}
	if got.Providers[0]["keyed"] != true || got.Providers[1]["keyed"] != false {
		t.Fatalf("providers = %v", got.Providers)
	}
	if got.Waiting["changeRequests"] != float64(3) {
		t.Fatalf("waiting = %v", got.Waiting)
	}
	if len(got.Unread) != 1 || !strings.HasPrefix(got.Unread[0], "applets:") {
		t.Fatalf("unread = %v", got.Unread)
	}
	for _, req := range api.got {
		if req.Method != http.MethodGet || req.Path != "/records" {
			t.Fatalf("the overview requested %s %s", req.Method, req.Path)
		}
	}
}

func TestKindsGroupsByPackageAndNarrows(t *testing.T) {
	page := `{"records":[` +
		`{"id":"substrate.reamde.dev/core/trigger","properties":{"description":"Runs on changes.","purpose":"internal"}},` +
		`{"id":"ada.example.com/tasks/task","properties":{"description":"A thing to do. It has a status.","purpose":"primary"}},` +
		`{"id":"ada.example.com/notes/note","properties":{}}]}`
	kinds := func(args string) (map[string]any, string) {
		t.Helper()
		api := &fakeAPI{answers: []Response{{Status: 200, Body: []byte(page)}}}
		text, isErr := call(t, api, "kinds", args)
		if isErr {
			t.Fatal(text)
		}
		if q := api.got[0].Query.Get("filter"); q != `{"kinds":["substrate.reamde.dev/core/kind"]}` {
			t.Fatalf("filter = %s", q)
		}
		var out map[string]any
		_ = json.Unmarshal([]byte(text), &out)
		return out, text
	}
	out, text := kinds(`{}`)
	groups := out["packages"].([]any)
	if out["count"] != float64(3) || len(groups) != 3 || groups[0].(map[string]any)["package"] != "ada.example.com/notes" {
		t.Fatalf("kinds = %s", text)
	}
	note := groups[0].(map[string]any)["kinds"].([]any)[0].(map[string]any)
	if note["purpose"] != "primary" {
		t.Fatalf("a kind without a purpose = %v", note)
	}
	out, text = kinds(`{"package":"ada.example.com","purposes":["primary"],"q":"status"}`)
	if out["count"] != float64(1) || !strings.Contains(text, `"description":"A thing to do."`) {
		t.Fatalf("narrowed = %s", text)
	}
	out, text = kinds(`{"first":1}`)
	if out["cursor"] != "ada.example.com/notes/note" {
		t.Fatalf("a first page = %s", text)
	}
	out, text = kinds(`{"after":"ada.example.com/notes/note"}`)
	if out["count"] != float64(2) {
		t.Fatalf("the next page = %s", text)
	}
}
