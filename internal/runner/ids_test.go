package runner

// The deterministic-id contract: ids.go is byte-identical to host.py's
// `ids.external` and `ids.url`. The golden vectors in testdata/id_vectors.json
// were minted by host.py; this test recomputes them through ids.go, so a
// refactor of either side that changes an id fails here instead of silently
// splitting one harvested record into two. It also covers the ASCII-only slug
// (Unicode dropped, not kept).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIDVectorsMatchPython(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "id_vectors.json"))
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
	// Unicode letters and digits are DROPPED (not kept), so the slug is
	// provably ASCII and byte-identical to Python's.
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
