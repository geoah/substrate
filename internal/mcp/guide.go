package mcp

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/applet"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The guide is what an assistant reads when it needs it rather than up
// front: one topic per area, a few thousand characters each, served by the
// guide tool (the one channel every client shows the model) and mirrored as
// resources for a client that lets its user attach them.
//
//go:embed guide/*.md
var guideFiles embed.FS

// Topic is one guide topic.
type Topic struct {
	Name, Title, Summary, Text string
}

// topicNames is the guide's order: the data first, then the shape, then the
// machinery. Every name but applets is a file under guide/; applets is the
// applet package's own guide, so the format it describes and the checks that
// hold it ship together.
var topicNames = []string{
	"records", "query", "vocabulary", "traits", "packages",
	"agents", "automation", "observations", "console", "applets",
}

// guideScheme prefixes a topic's resource URI.
const guideScheme = "substrate://guide/"

// Topics are the guide's topics, in order.
var Topics = func() []Topic {
	out := make([]Topic, 0, len(topicNames))
	for _, name := range topicNames {
		text := applet.Guide
		if name != "applets" {
			b, err := guideFiles.ReadFile("guide/" + name + ".md")
			if err != nil {
				panic("mcp: the guide names " + name + " and ships no file for it")
			}
			text = string(b)
		}
		out = append(out, parseTopic(name, text))
	}
	return out
}()

// parseTopic reads a topic's title from its heading and its summary from the
// first sentence of the paragraph after it.
func parseTopic(name, text string) Topic {
	t := Topic{Name: name, Text: text}
	heading, rest, _ := strings.Cut(text, "\n")
	t.Title = strings.TrimSpace(strings.TrimPrefix(heading, "# "))
	para, _, _ := strings.Cut(strings.TrimLeft(rest, "\n"), "\n\n")
	t.Summary = firstSentence(para)
	return t
}

func topic(name string) (Topic, bool) {
	i := slices.IndexFunc(Topics, func(t Topic) bool { return t.Name == name })
	if i < 0 {
		return Topic{}, false
	}
	return Topics[i], true
}

func topicIndex() []map[string]string {
	out := make([]map[string]string, 0, len(Topics))
	for _, t := range Topics {
		out = append(out, map[string]string{"topic": t.Name, "about": t.Summary})
	}
	return out
}

// plainText is a tool's answer sent as it is, not as JSON: a guide topic is
// markdown, which JSON's escaping would make longer and harder to read.
type plainText string

// --- guide ---

func guideTool() Tool {
	return Tool{
		Name:  "guide",
		Title: "Read the guide",
		Description: "The substrate's documentation, one topic at a time: read a topic before your first work in its area. " +
			"Without topic it lists the topics, each with what it covers. records: writing records, change requests, questions " +
			"agents ask. query: filters, search, windows, aggregates. vocabulary: declaring packages and kinds, property types, " +
			"changing a kind safely. traits: what each trait gives a kind. packages: the catalog, samples, providers, inputs. " +
			"agents: Moss, the owner's agents, LLM providers. automation: functions, triggers, policies. observations: metrics " +
			"and units. console: the console's views. applets: building an applet.",
		Input: object(map[string]any{
			"topic": enum("the topic to read; absent lists them", topicNames...),
		}),
		Hints: readOnly,
		Run: func(_ context.Context, _ API, args json.RawMessage) (any, error) {
			var in struct {
				Topic string `json:"topic"`
			}
			if err := decode(args, &in); err != nil {
				return nil, err
			}
			if in.Topic == "" {
				return map[string]any{"topics": topicIndex(), "note": "read one with guide {topic}; each is also the resource " + guideScheme + "<topic>"}, nil
			}
			t, ok := topic(in.Topic)
			if !ok {
				return nil, refuse("no topic %q: the topics are %s", in.Topic, strings.Join(topicNames, ", "))
			}
			return plainText(t.Text), nil
		},
	}
}

// --- overview ---

// Kinds the overview reads.
const (
	kindRepository = "substrate.reamde.dev/core/repository"
	kindPackage    = "substrate.reamde.dev/core/package"
	kindAgent      = "substrate.reamde.dev/core/agent"
	kindFunction   = "substrate.reamde.dev/core/function"
	kindTrigger    = "substrate.reamde.dev/core/trigger"
	kindProvider   = "substrate.reamde.dev/llm/provider"
	kindQuestion   = "substrate.reamde.dev/llm/interaction"
)

func overviewTool() Tool {
	return Tool{
		Name:  "overview",
		Title: "Start here",
		Description: "Start here: one read of the owner's substrate. It answers the repository's name and its own namespace, ~ " +
			"(the home of every package the owner declares), each package with its source (builtin and published ones are read-only, installed ones are the owner's) and " +
			"how many kinds it holds, the agents (Moss, substrate.reamde.dev/assistant/moss, is the owner's assistant), the applets, the " +
			"LLM providers and whether each has its key, how many functions and triggers there are, what waits on the owner, and the " +
			"guide's topics.",
		Input: object(map[string]any{}),
		Hints: readOnly,
		Run: func(ctx context.Context, api API, args json.RawMessage) (any, error) {
			var in struct{}
			if err := decode(args, &in); err != nil {
				return nil, err
			}
			return overview(ctx, api), nil
		},
	}
}

// overview reads what an assistant needs before its first decision. A part
// that cannot be read is named under unread rather than failing the rest.
func overview(ctx context.Context, api API) map[string]any {
	out := map[string]any{}
	var unread []string
	page := func(part, kind string, filter map[string]any, first int) []listed {
		recs, _, err := list(ctx, api, kind, filter, first, "")
		if err != nil {
			unread = append(unread, part+": "+err.Error())
		}
		return recs
	}
	count := func(part, kind string, filter map[string]any) any {
		n, err := countOf(ctx, api, kind, filter)
		if err != nil {
			unread = append(unread, part+": "+err.Error())
			return nil
		}
		return n
	}

	if repo := page("authority", kindRepository, nil, 1); len(repo) > 0 {
		authority, _ := repo[0].Properties["authority"].(string)
		if authority == "" {
			authority = repo[0].ID
		}
		out["authority"] = authority
	}
	// The owner's own packages live under `~`, whatever the repository is
	// called (decision record 0259).
	out["ownNamespace"] = vocabulary.Local

	kinds, err := allKinds(ctx, api)
	if err != nil {
		unread = append(unread, "kinds: "+err.Error())
	}
	perPackage := map[string][2]int{}
	for _, k := range kinds {
		n := perPackage[k.Package]
		n[0]++
		if k.Purpose == "primary" {
			n[1]++
		}
		perPackage[k.Package] = n
	}
	packages := []map[string]any{}
	for _, p := range page("packages", kindPackage, nil, 500) {
		source, _ := p.Properties["source"].(string)
		n := perPackage[p.ID]
		entry := map[string]any{"package": p.ID, "source": source, "kinds": n[0], "primaryKinds": n[1]}
		if q, _ := p.Properties["quarantined"].(bool); q {
			entry["quarantined"] = true
		}
		packages = append(packages, entry)
	}
	slices.SortFunc(packages, func(a, b map[string]any) int { return strings.Compare(a["package"].(string), b["package"].(string)) })
	out["packages"] = packages

	agents := []map[string]any{}
	for _, a := range page("agents", kindAgent, nil, 200) {
		desc, _ := a.Properties["description"].(string)
		entry := map[string]any{"agent": a.ID, "description": firstSentence(desc)}
		if hidden, _ := a.Properties["hiddenFromChat"].(bool); hidden {
			entry["chat"] = false
		}
		agents = append(agents, entry)
	}
	out["agents"] = agents

	applets := []map[string]any{}
	for _, a := range page("applets", applet.Kind, nil, 100) {
		name, _ := a.Properties["name"].(string)
		applets = append(applets, map[string]any{"applet": a.ID, "name": name, "link": "/applets/" + a.ID})
	}
	out["applets"] = applets

	providers := []map[string]any{}
	for _, p := range page("providers", kindProvider, nil, 50) {
		key, _ := p.Properties["apiKey"].(string)
		providers = append(providers, map[string]any{"provider": p.ID, "keyed": key != ""})
	}
	out["llmProviders"] = providers
	if !slices.ContainsFunc(providers, func(p map[string]any) bool { return p["keyed"] == true }) {
		out["llmNote"] = "no LLM provider has its apiKey, so every agent, Moss among them, refuses to run until the owner " +
			"writes one (the guide topic agents says how)"
	}

	out["counts"] = map[string]any{
		"functions": count("functions", kindFunction, nil),
		"triggers":  count("triggers", kindTrigger, nil),
	}
	out["waitingOnTheOwner"] = map[string]any{
		"changeRequests": count("change requests", kindRequest, map[string]any{"properties": map[string]any{"decision": map[string]any{"eq": "proposed"}}}),
		"questions":      count("questions", kindQuestion, map[string]any{"properties": map[string]any{"state": map[string]any{"eq": "pending"}}}),
	}
	out["guide"] = topicIndex()
	out["next"] = "Declare new packages under ~, the owner's own namespace. Read a guide topic before your first work in its area, " +
		"kinds for a package's kinds, describe_kind before writing one, and query for records."
	if len(unread) > 0 {
		out["unread"] = unread
	}
	return out
}

// listed is one record of a page, as the overview and the kinds list read it.
type listed struct {
	ID         string         `json:"id"`
	Properties map[string]any `json:"properties"`
}

// list reads one page of one kind's records.
func list(ctx context.Context, api API, kind string, filter map[string]any, first int, after string) ([]listed, string, error) {
	f := map[string]any{"kinds": []string{kind}}
	for k, v := range filter {
		f[k] = v
	}
	q := url.Values{"first": {strconv.Itoa(first)}}
	if after != "" {
		q.Set("after", after)
	}
	if err := jsonParam(q, "filter", f); err != nil {
		return nil, "", err
	}
	raw, err := send(ctx, api, Request{Method: http.MethodGet, Path: "/records", Query: q})
	if err != nil {
		return nil, "", err
	}
	var p struct {
		Records []listed `json:"records"`
		Cursor  string   `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "", err
	}
	return p.Records, p.Cursor, nil
}

// countOf is the size of one kind's filtered set.
func countOf(ctx context.Context, api API, kind string, filter map[string]any) (int64, error) {
	f := map[string]any{"kinds": []string{kind}}
	for k, v := range filter {
		f[k] = v
	}
	q := url.Values{"first": {"1"}, "count": {"1"}}
	if err := jsonParam(q, "filter", f); err != nil {
		return 0, err
	}
	raw, err := send(ctx, api, Request{Method: http.MethodGet, Path: "/records", Query: q})
	if err != nil {
		return 0, err
	}
	var p struct {
		Count *int64 `json:"count"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return 0, err
	}
	if p.Count == nil {
		return 0, fmt.Errorf("the page carried no count")
	}
	return *p.Count, nil
}

// --- resources ---

func (s *Server) listResources() []map[string]any {
	out := make([]map[string]any, 0, len(Topics))
	for _, t := range Topics {
		out = append(out, map[string]any{
			"uri": guideScheme + t.Name, "name": t.Name, "title": t.Title,
			"description": t.Summary, "mimeType": "text/markdown", "size": len(t.Text),
		})
	}
	return out
}

func readResource(params json.RawMessage) (map[string]any, *rpcError) {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.URI == "" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "resources/read names a resource by uri"}
	}
	t, ok := topic(strings.TrimPrefix(p.URI, guideScheme))
	if !ok || !strings.HasPrefix(p.URI, guideScheme) {
		return nil, &rpcError{Code: codeResourceNotFound, Message: "no resource " + p.URI + ": resources/list names them"}
	}
	return map[string]any{"contents": []map[string]any{{"uri": p.URI, "mimeType": "text/markdown", "text": t.Text}}}, nil
}

// --- prompts ---

// Prompt is a workflow the owner starts by name in a client that offers
// prompts (Claude Code shows each as a slash command). Each is a request in
// the owner's voice that points at the tools and topics the work needs.
type Prompt struct {
	Name, Title, Description string
	// Argument is the one optional free-text argument, "" for none: a client
	// may split arguments on whitespace, so a prompt takes at most one.
	Argument, ArgumentDescription string
	Text                          func(arg string) string
}

// Prompts are the door's prompts.
var Prompts = []Prompt{
	{
		Name: "tour", Title: "Tour my substrate",
		Description: "Get to know the owner's substrate: what it holds and what waits on them.",
		Text: func(string) string {
			return "Call overview, then tell me in a few short lines what my substrate holds: my own collections " +
				"(the packages under my authority and the catalogues I installed, with their primary kinds), the agents " +
				"and applets I have, and anything waiting on me (change requests to decide, questions agents asked). " +
				"Then offer two or three things you could do next."
		},
	},
	{
		Name: "new_collection", Title: "Start a collection",
		Description: "Design a new collection (a kind) for something the owner wants to keep track of.",
		Argument:    "what", ArgumentDescription: "what to keep track of, such as my plants",
		Text: func(what string) string {
			return "I want to start keeping track of " + orAsk(what, "something new") + ". Read the guide topics " +
				"vocabulary and traits, call overview for my authority, and read the kinds I already have (kinds, then " +
				"describe_kind for any that might fit). Prefer extending a kind that fits, or importing a sample from the " +
				"catalog, over a near-duplicate. Ask me any choice that changes the shape. Then preview the batch with " +
				"apply plan true, show me the kinds and properties in plain words, and apply only when I say so."
		},
	},
	{
		Name: "new_applet", Title: "Build an applet",
		Description: "Build an applet, a small phone-first app over the owner's records.",
		Argument:    "what", ArgumentDescription: "what the applet is for, such as logging my runs",
		Text: func(what string) string {
			return "Build me an applet for " + orAsk(what, "something I do often") + ". Read the guide topic applets " +
				"first, then the kinds it shows (describe_kind) and a few of their records (query). Ask me what I will " +
				"mostly do with it if that is not clear. Write it with write, preview every screen with applets op " +
				"preview against real records, fix every problem, and give me its link, /applets/<id>."
		},
	},
	{
		Name: "inbox", Title: "What needs me",
		Description: "What waits on the owner: change requests, questions from agents, and what is due.",
		Text: func(string) string {
			return "What needs me? Read the guide topic records for how change requests and questions work, then list " +
				"the change requests waiting on my decision (substrate.reamde.dev/core/recordpatchrequest, decision " +
				"proposed) and the questions agents asked me (substrate.reamde.dev/llm/interaction, state pending), " +
				"and anything due or overdue in my own collections. For each, say in one line what it is and what you " +
				"would do. Decide or answer nothing until I say."
		},
	},
	{
		Name: "tidy", Title: "Tidy a collection",
		Description: "Find duplicates, gaps and inconsistent values in one of the owner's collections.",
		Argument:    "collection", ArgumentDescription: "the collection, such as contacts",
		Text: func(collection string) string {
			return "Look over my " + orAsk(collection, "collections") + " for duplicates, missing values and " +
				"inconsistent spellings. Read the kind's declaration first (describe_kind) and its records page by page " +
				"(query). Show me what you found and what you would change; change, merge or delete nothing until I agree."
		},
	},
}

func orAsk(arg, fallback string) string {
	if arg = strings.TrimSpace(arg); arg != "" {
		return arg
	}
	return fallback + " (ask me what, first)"
}

func listPrompts() []map[string]any {
	out := make([]map[string]any, 0, len(Prompts))
	for _, p := range Prompts {
		entry := map[string]any{"name": p.Name, "title": p.Title, "description": p.Description}
		if p.Argument != "" {
			entry["arguments"] = []map[string]any{{"name": p.Argument, "description": p.ArgumentDescription, "required": false}}
		}
		out = append(out, entry)
	}
	return out
}

func getPrompt(params json.RawMessage) (map[string]any, *rpcError) {
	var p struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "prompts/get names a prompt and its arguments"}
	}
	i := slices.IndexFunc(Prompts, func(x Prompt) bool { return x.Name == p.Name })
	if i < 0 {
		return nil, &rpcError{Code: codeInvalidParams, Message: "no prompt named " + p.Name}
	}
	pr := Prompts[i]
	for k := range p.Arguments {
		if k != pr.Argument {
			return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("prompt %s takes no argument %q", pr.Name, k)}
		}
	}
	return map[string]any{
		"description": pr.Description,
		"messages": []map[string]any{{
			"role":    "user",
			"content": map[string]any{"type": "text", "text": pr.Text(p.Arguments[pr.Argument])},
		}},
	}, nil
}
