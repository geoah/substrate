package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/api"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Every continuation token the server mints whose position compares a
// record's kind or id carries substrate.IdentityOrder as its version, and
// one minted under the order before (the database's locale, before
// migration 0021) is refused as list again on every route and reader that
// takes it: the records route, for the owner and for an app, the assistant's
// query tool, a function body's list and an agent's query tool. The same
// token at this order is accepted by each, so the refusal is the version.
func TestATokenMintedUnderTheOldOrderIsRefusedEverywhere(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	const pkg = "paging.test.dev/order"
	item := pkg + "/item"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "item"}, map[string]any{
			"ids":        map[string]any{"type": vocabulary.IDsNamed},
			"traits":     []any{"substrate.reamde.dev/core/temporal(range)"},
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatalf("declare the item kind: %v", err)
	}
	// The locale orders them a, b, Zed, zz; bytes order them Zed, a, b, zz.
	// One time for all of them, so a window page breaks its ties on kind and
	// id.
	for _, id := range []string{"a", "b", "Zed", "zz"} {
		if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: item, ID: id, Properties: map[string]any{
			"name": id, "at": "2026-09-01T09:00:00Z", "endsAt": "2026-09-01T10:00:00Z",
		}}); err != nil {
			t.Fatal(err)
		}
	}

	_, owner, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	_, app, err := ds.MintToken(ctx, substrate.MintTokenInput{
		Label: "Notes", Scopes: []string{"read:" + item},
		ClientID: "https://notes.example.org/client.json", ClientName: "Notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, assistant, err := ds.MintToken(ctx, substrate.MintTokenInput{
		Label: "Claude Code", Scopes: []string{"mcp"},
		ClientID: "https://claude.ai/oauth/claude-code-client-metadata", ClientName: "Claude Code", AppLocal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := api.New(api.Config{Service: ds.svc})

	// The two codecs that compare an identity column, each with the filter
	// that makes the records route mint it.
	codecs := []struct {
		name   string
		filter string
		// old answers the token as the server before the order changed
		// minted it: the same position, the version it carried then.
		old func(t *testing.T, token string) string
	}{
		{"the records list's keyset", `{"kinds":["` + item + `"]}`, func(t *testing.T, token string) string {
			raw, err := base64.RawURLEncoding.DecodeString(token)
			if err != nil {
				t.Fatal(err)
			}
			return base64.RawURLEncoding.EncodeToString(withVersion(t, raw, substrate.IdentityOrder-1))
		}},
		{"the window read's cursor", `{"kinds":["` + item + `"],"properties":{"at":{"gte":"2026-09-01T00:00:00Z","lt":"2026-09-02T00:00:00Z"}}}`, func(t *testing.T, token string) string {
			body, ok := strings.CutPrefix(token, "w.")
			if !ok {
				t.Fatalf("%q is no window cursor", token)
			}
			raw, err := base64.RawURLEncoding.DecodeString(body)
			if err != nil {
				t.Fatal(err)
			}
			return "w." + base64.RawURLEncoding.EncodeToString(withVersion(t, raw, substrate.IdentityOrder-1))
		}},
	}

	// Each route and reader that takes a token answers whether it resumed
	// from it, and the refusal's text when it did not.
	records := func(bearer string) func(t *testing.T, filter, after string) (bool, string) {
		return func(t *testing.T, filter, after string) (bool, string) {
			t.Helper()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/records?first=1&filter="+url.QueryEscape(filter)+"&after="+url.QueryEscape(after), nil)
			req.Host = "substrate.example.com"
			req.Header.Set("Authorization", "Bearer "+bearer)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				return true, ""
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if rec.Code != http.StatusGone || body.Error.Code != "compacted" {
				t.Errorf("the records route answered %d %s, want 200 or 410 compacted", rec.Code, rec.Body.String())
			}
			return false, rec.Body.String()
		}
	}
	readers := []struct {
		name   string
		resume func(t *testing.T, filter, after string) (bool, string)
	}{
		{"the records route, the owner's token", records(owner)},
		{"the records route, an app's token", records(app)},
		{"the assistant's query tool", func(t *testing.T, filter, after string) (bool, string) {
			t.Helper()
			var f map[string]any
			if err := json.Unmarshal([]byte(filter), &f); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
				"name": "query", "arguments": map[string]any{"filter": f, "first": 1, "after": after},
			}})
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(raw)))
			req.Host = "substrate.example.com"
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+assistant)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			var ans struct {
				Result struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
					IsError bool `json:"isError"`
				} `json:"result"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &ans); err != nil || rec.Code != http.StatusOK || len(ans.Result.Content) != 1 {
				t.Fatalf("the query tool answered %d %s", rec.Code, rec.Body.String())
			}
			text := ans.Result.Content[0].Text
			if ans.Result.IsError && !strings.Contains(text, "answered 410") {
				t.Errorf("the query tool's refusal is %s, want the route's 410", text)
			}
			return !ans.Result.IsError, text
		}},
		{"a function body's list", func(t *testing.T, filter, after string) (bool, string) {
			t.Helper()
			_, err := (&callBackend{inv: &invocation{ds: ds}}).List(ctx, substrate.Query{Filter: mustFilter(t, filter), First: 1, After: after})
			if err != nil && !errors.Is(err, substrate.ErrStaleHistory) {
				t.Errorf("a function body's list refused with %v, want ErrStaleHistory", err)
			}
			if err != nil {
				return false, err.Error()
			}
			return true, ""
		}},
		{"an agent's query tool", func(t *testing.T, filter, after string) (bool, string) {
			t.Helper()
			var f map[string]any
			if err := json.Unmarshal([]byte(filter), &f); err != nil {
				t.Fatal(err)
			}
			out, ok, _ := ds.runQueryTool(ctx, queryScope{kinds: []string{item}, rows: 50, requireKinds: true},
				map[string]any{"filter": f, "first": float64(1), "after": after})
			return ok, out
		}},
	}

	for _, c := range codecs {
		page, err := ds.readList(ctx, substrate.Query{Filter: mustFilter(t, c.filter), First: 1})
		if err != nil || page.Cursor == "" {
			t.Fatalf("%s: the first page: %v, %v", c.name, page, err)
		}
		old := c.old(t, page.Cursor)
		for _, r := range readers {
			if ok, why := r.resume(t, c.filter, page.Cursor); !ok {
				t.Errorf("%s, %s: a token at this order was refused: %s", c.name, r.name, why)
			}
			ok, why := r.resume(t, c.filter, old)
			if ok || !strings.Contains(why, "list again") {
				t.Errorf("%s, %s: a token minted under the order before resumed (%v): %s", c.name, r.name, ok, why)
			}
		}
	}
}

// withVersion is a token's JSON body with its version set to v.
func withVersion(t *testing.T, raw []byte, v int) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["v"] != float64(substrate.IdentityOrder) {
		t.Fatalf("the token %s carries version %v, want substrate.IdentityOrder", raw, m["v"])
	}
	m["v"] = v
	out, _ := json.Marshal(m)
	return out
}

// mustFilter decodes a records filter as the records route does.
func mustFilter(t *testing.T, raw string) substrate.Filter {
	t.Helper()
	var f substrate.Filter
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatal(err)
	}
	return f
}
