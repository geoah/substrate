package testenv_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/testenv"
)

// A listing orders ids in plain byte order, the order Go's string comparison
// gives, whatever the database's locale: upper case before lower case, and
// punctuation by its byte. Read a few at a time, every page resumes where the
// last ended, so a cursor Go hands back and the index agree (migration 0021).
func TestAListingOrdersIDsInByteOrder(t *testing.T) {
	e := testenv.Start(t, testenv.WithRepository(probeAuthority))
	e.ApplyVocabularyYAML(`
kind: substrate.reamde.dev/core/package
metadata:
  id: ` + probeRef + `
data:
  authority: ` + probeAuthority + `
  package: ` + probePackage + `
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ` + ref("label") + `
data:
  authority: ` + probeAuthority + `
  package: ` + probePackage + `
  names:
    singular: label
  ids:
    type: named
  properties:
    name:
      type: string
`)
	ids := []string{"b", "B", "a-1", "a_1", "A_1", "a.1", "a1", "a:1", "a@1", "a~1", "Zed", "zed", "0", "a/1"}
	for _, id := range ids {
		if status, body := e.Do(http.MethodPut, "/api/v1/"+ref("label")+"/"+url.PathEscape(id), map[string]any{
			"properties": map[string]any{"name": id},
		}); status != http.StatusCreated {
			t.Fatalf("put %q: %d %s", id, status, body)
		}
	}
	want := slices.Clone(ids)
	slices.Sort(want)

	filter, _ := json.Marshal(map[string]any{"kinds": []string{ref("label")}})
	var got []string
	after := ""
	for range ids {
		q := url.Values{"filter": {string(filter)}, "orderBy": {"id"}, "first": {"3"}}
		if after != "" {
			q.Set("after", after)
		}
		var page struct {
			Records []struct {
				ID string `json:"id"`
			} `json:"records"`
			Cursor string `json:"cursor"`
		}
		e.MustJSON(http.MethodGet, "/api/v1/records?"+q.Encode(), nil, &page)
		for _, r := range page.Records {
			got = append(got, r.ID)
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the listing, three at a time:\n  got  %q\n  want %q (byte order)", got, want)
	}
}
