package substratefn

// The deterministic-id contract: the Go SDK's ids are byte-identical to the
// Python SDK's. The golden vectors in ../testdata/id_vectors.json were minted
// by host.py; this test recomputes them through substratefn — a divergence here is
// a cross-runtime dedupe break. It also covers the empty-component rejection
// and the ASCII-only slug (Unicode dropped, not kept).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIDVectorsMatchPython(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "id_vectors.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var vecs struct {
		External []struct {
			Provider   string `json:"provider"`
			Account    string `json:"account"`
			ExternalID string `json:"externalId"`
			ID         string `json:"id"`
		} `json:"external"`
		URL []struct {
			URL string `json:"url"`
			ID  string `json:"id"`
		} `json:"url"`
	}
	if err := json.Unmarshal(raw, &vecs); err != nil {
		t.Fatalf("decode vectors: %v", err)
	}
	if len(vecs.External) == 0 || len(vecs.URL) == 0 {
		t.Fatalf("vectors file is empty")
	}
	for _, v := range vecs.External {
		if got := ExternalID(v.Provider, v.Account, v.ExternalID); got != v.ID {
			t.Errorf("ExternalID(%q,%q,%q) = %q, want %q (Python parity)",
				v.Provider, v.Account, v.ExternalID, got, v.ID)
		}
	}
	for _, v := range vecs.URL {
		if got := URLID(v.URL); got != v.ID {
			t.Errorf("URLID(%q) = %q, want %q (Python parity)", v.URL, got, v.ID)
		}
	}
}

func TestSlugifyASCIIOnly(t *testing.T) {
	// Unicode letters/digits are DROPPED (not kept), so the slug is provably
	// ASCII and byte-identical to Python's — the divergence the old
	// unicode.IsLetter path had.
	for in, want := range map[string]string{
		"prov":            "prov",
		"Provider Name!!": "provider-name",
		"a.b_c~d":         "a-b-c-d",
		"½x":              "x",
		"ÄÖÜ":             "",
		"例え":              "",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIDEmptyComponentRejected(t *testing.T) {
	h := &Host{}
	h.initSDK(nil)
	if got := h.IDs.External("", "a", "b"); got != "" || h.sdkErr == nil {
		t.Fatalf("empty provider: got %q err=%v, want an sdkErr and empty id", got, h.sdkErr)
	}

	h2 := &Host{}
	h2.initSDK(nil)
	if got := h2.IDs.URL("   "); got != "" || h2.sdkErr == nil {
		t.Fatalf("whitespace-only url: got %q err=%v, want an sdkErr and empty id", got, h2.sdkErr)
	}
}
