package substratefn

import "testing"

// The host's search answer carries the hits and the embed backlog; the typed
// reader keeps both, and a reply from a host that predates `pending` reads as
// zero rather than failing.
func TestDecodeSearchResultKeepsHitsAndPending(t *testing.T) {
	res, err := decodeSearchResult([]byte(`{"hits":[{"record":{"id":"a"},"lexical":0.5}],"pending":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Record == nil || res.Hits[0].Record.ID != "a" || res.Pending != 3 {
		t.Fatalf("decoded %+v", res)
	}
	res, err = decodeSearchResult([]byte(`{"hits":[]}`))
	if err != nil || res.Pending != 0 || len(res.Hits) != 0 {
		t.Fatalf("a reply without pending = %+v, %v", res, err)
	}
	if _, err := decodeSearchResult([]byte(`{"hits":`)); err == nil {
		t.Fatal("a torn reply decoded")
	}
}
