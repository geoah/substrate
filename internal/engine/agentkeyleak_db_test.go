package engine

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A completions 401 quotes the bearer it refused, and under the per-repository
// provider model that bearer is a repository's own key. This drives the whole
// path: a provider row with a wrong key, a real agent turn, a provider that
// answers 401 with the masked body a live endpoint sends. The error settles
// onto the thread record's reason (it lands in the changelog and survives) and
// is joined into the error the sweep logs, so neither may carry the key.
func TestAgentKeyNeverReachesThreadRecordOrError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	fake := newFakeLLM(t)

	// Fragments of one synthetic key; no real key appears in this tree. The
	// row carries it as the Bearer, and the 401 quotes it back MASKED, the
	// shape a live provider sends: prefix, asterisks, last four. An exact-match
	// scrub removes nothing from that; the masked-token pass is what catches it.
	const key = "sk-proj-notarealkey000000000000000000000000000cdef"
	prefix, suffix := key[:8], key[len(key)-4:]
	masked := prefix + strings.Repeat("*", 32) + suffix

	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "leakllm",
		Properties: map[string]any{
			"wire": "openai", "baseURL": fake.srv.URL, "apiKey": key,
			"pricing": []any{map[string]any{"model": "leak", "inputPer1M": "1", "outputPer1M": "5"}},
		},
	}); err != nil {
		t.Fatalf("put provider row: %v", err)
	}

	const pkg = "leaktest.test.dev/leaktest"
	leaker := vocabulary.AgentManifest(pkg, "leaker", map[string]any{
		"description": "the key-leak fixture", "prompt": "You are leaker.",
		"provider": "leakllm", "model": "leak",
	})
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(pkg, 0), leaker,
	}); err != nil {
		t.Fatalf("install leaker: %v", err)
	}

	fake.script("leak", fakeTurn{
		status: http.StatusUnauthorized,
		errBody: `{"error":{"message":"Incorrect API key provided: ` + masked +
			`. You can find your API key at https://example.com/account/api-keys.","type":"invalid_request_error"}}`,
	})

	_, err := ds.CallAgent(ctx, pkg+"/leaker", "go")
	if err == nil {
		t.Fatal("a 401 was not reported as an error")
	}

	assertNoKeyFragment := func(where, s string) {
		t.Helper()
		if strings.Contains(s, key) {
			t.Fatalf("%s carries the row's key whole: %v", where, s)
		}
		if strings.Contains(s, prefix) || strings.Contains(s, suffix) {
			t.Fatalf("%s carries a fragment of the row's key: %v", where, s)
		}
		if !strings.Contains(s, "Incorrect API key provided") {
			t.Fatalf("%s lost the endpoint's own message: %v", where, s)
		}
	}

	// The error the caller receives is the one errors.Join hands the sweep log.
	assertNoKeyFragment("the caller error", err.Error())

	// The thread record's reason is that same string at settle: the sink that
	// lands in the changelog and survives.
	rows, qerr := ds.db.QueryContext(ctx, `
		SELECT props->>'status', props->>'reason' FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND `+referencePathSQL("props", "agent")+` = $2`,
		typeThread, vocabulary.RecordPath(kindAgent, pkg+"/leaker"))
	if qerr != nil {
		t.Fatalf("query threads: %v", qerr)
	}
	defer func() { _ = rows.Close() }()
	seen := 0
	for rows.Next() {
		seen++
		var status, reason string
		if err := rows.Scan(&status, &reason); err != nil {
			t.Fatalf("scan thread: %v", err)
		}
		if status != threadError {
			t.Fatalf("thread status = %q, want %q", status, threadError)
		}
		if reason == "" {
			t.Fatal("the thread carries no reason to check")
		}
		assertNoKeyFragment("the thread reason", reason)
	}
	if seen != 1 {
		t.Fatalf("found %d error threads, want 1", seen)
	}
}

// installSecretToolBundle stands up a bundle whose config carries a secret and
// a function that leaks it into a note effect, plus a non-bundled agent naming
// that function as a tool.
func installSecretToolBundle(t *testing.T, ds *dataset, fake *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "leakllm",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-leakllm"},
	}); err != nil {
		t.Fatalf("put llm/provider row: %v", err)
	}
	leak := vocabulary.FunctionManifest(secretToolPackage, "leaktool", map[string]any{
		"description": "copies the config secret into a note",
		"runtime":     vocabulary.RuntimePython,
		"permissions": map[string]any{"writes": []any{secretToolPackage + "/snote"}},
		"source": `
def main(input, host):
    props = input["config"]["inputs"]["connector"]["properties"]
    return {"effects": [{"action": "put", "kind": "secretb.bundles.substrate.reamde.dev/secretb/snote",
                         "id": "s-note", "properties": {"text": props["apiToken"]}}],
            "output": {"ok": True}}
`,
	})
	docs := []map[string]any{
		vocabulary.PackageManifest(secretToolPackage, 0),
		vocabulary.BundleManifest(secretToolPackage, map[string]any{
			"description": "the secret tool bundle",
			"inputs": map[string]any{
				"connector": map[string]any{"kind": secretToolPackage + "/sconfig", "inject": "functions"},
			},
			"installs": []any{secretToolPackage + "/sconfig", secretToolPackage + "/snote", secretToolPackage + "/leaktool"},
		}),
		vocabulary.KindManifest(secretToolPackage,
			map[string]any{"singular": "sconfig"},
			map[string]any{"properties": map[string]any{
				"apiToken": map[string]any{"type": "secret"},
			}}),
		vocabulary.KindManifest(secretToolPackage,
			map[string]any{"singular": "snote"},
			map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
		leak,
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install secret tool bundle: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: secretToolPackage + "/sconfig", ID: "s-cfg",
		Properties: map[string]any{"apiToken": secretToolSecret},
	}); err != nil {
		t.Fatalf("put config: %v", err)
	}
	const agPackage = "secretuser.test.dev/secretuser"
	user := vocabulary.AgentManifest(agPackage, "leaker", map[string]any{
		"description": "invokes the leaking tool", "prompt": "You leak.",
		"provider": "leakllm", "model": "leak",
		"tools":       []any{map[string]any{"function": secretToolPackage + "/leaktool"}},
		"permissions": map[string]any{"writes": []any{secretToolPackage + "/snote"}},
	})
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(agPackage, 0), user,
	}); err != nil {
		t.Fatalf("install leaker agent: %v", err)
	}
}

func TestAgentToolEffectCarryingABundleSecretIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installSecretToolBundle(t, ds, fake)

	fake.script("leak",
		fakeTurn{calls: []fakeCall{{"leaktool", `{}`}}},
		fakeTurn{content: "done"},
	)
	res, err := ds.CallAgent(ctx, "secretuser.test.dev/secretuser/leaker", "go")
	if err != nil {
		t.Fatalf("call leaker: %v", err)
	}
	// The tool call surfaced an error, and no snote persisted.
	sawError := false
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["role"] != "tool" {
			continue
		}
		body := fmtContent(m["content"])
		if strings.Contains(body, secretToolSecret) {
			t.Fatalf("the secret reached the agent transcript: %s", body)
		}
		if strings.Contains(body, "error") {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("the leaking tool did not surface a rejection to the agent")
	}
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{secretToolPackage + "/snote"}}, First: 5,
	})
	if err != nil {
		t.Fatalf("list snotes: %v", err)
	}
	if len(page.Records) != 0 {
		t.Fatalf("a secret-bearing effect persisted through the agent tool: %d", len(page.Records))
	}
}

func fmtContent(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

const (
	secretToolPackage = "secretb.bundles.substrate.reamde.dev/secretb"
	secretToolSecret  = "sk-agenttool-supersecret-99"
)
