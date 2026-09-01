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

	const authority = "leaktest.test.dev"
	leaker := vocabulary.AgentManifest(authority, "leaker", map[string]any{
		"description": "the key-leak fixture", "prompt": "You are leaker.",
		"provider": "leakllm", "model": "leak",
	})
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.AuthorityManifest(authority, 0), leaker,
	}); err != nil {
		t.Fatalf("install leaker: %v", err)
	}

	fake.script("leak", fakeTurn{
		status: http.StatusUnauthorized,
		errBody: `{"error":{"message":"Incorrect API key provided: ` + masked +
			`. You can find your API key at https://example.com/account/api-keys.","type":"invalid_request_error"}}`,
	})

	_, err := ds.CallAgent(ctx, authority+"/leaker", "go")
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
		typeThread, vocabulary.RecordPath(kindAgent, authority+"/leaker"))
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
