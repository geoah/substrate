package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestParseChangeFilterAcceptsRepeatedAndCommaLists(t *testing.T) {
	// recordId now REQUIRES its recordKind companion: the pair is
	// prepended to Types, then the explicit kinds= list follows.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/changes"+
		"?kinds=samples.substrate.reamde.dev/tasks/task,samples.substrate.reamde.dev/messaging/conversationmessage"+
		"&actors=api&actors=connector:gmail"+
		"&ops=put,patch&ops=delete"+
		"&excludeKinds=google.connectors.substrate.reamde.dev/google/syncrun"+
		"&excludeActors=system&excludeOps=gc,merge"+
		"&recordId=person-1&recordKind=samples.substrate.reamde.dev/people/person", nil)
	f, err := parseChangeFilter(req)
	if err != nil {
		t.Fatalf("parseChangeFilter: %v", err)
	}

	wantTypes := []string{"samples.substrate.reamde.dev/people/person", "samples.substrate.reamde.dev/tasks/task", "samples.substrate.reamde.dev/messaging/conversationmessage"}
	if len(f.Kinds) != len(wantTypes) {
		t.Fatalf("types = %v, want %v", f.Kinds, wantTypes)
	}
	for i, want := range wantTypes {
		if f.Kinds[i] != want {
			t.Fatalf("types = %v, want %v", f.Kinds, wantTypes)
		}
	}
	if len(f.Actors) != 2 || f.Actors[0] != "api" || f.Actors[1] != "connector:gmail" {
		t.Fatalf("actors = %v", f.Actors)
	}
	if len(f.Ops) != 3 || f.Ops[0] != substrate.OpPut || f.Ops[2] != substrate.OpDelete {
		t.Fatalf("ops = %v", f.Ops)
	}
	if len(f.ExcludeKinds) != 1 || f.ExcludeKinds[0] != "google.connectors.substrate.reamde.dev/google/syncrun" {
		t.Fatalf("exclude types = %v", f.ExcludeKinds)
	}
	if len(f.ExcludeActors) != 1 || f.ExcludeActors[0] != "system" {
		t.Fatalf("exclude actors = %v", f.ExcludeActors)
	}
	if len(f.ExcludeOps) != 2 || f.ExcludeOps[0] != substrate.OpGC || f.ExcludeOps[1] != substrate.OpMerge {
		t.Fatalf("exclude ops = %v", f.ExcludeOps)
	}
	if f.RecordID != "person-1" {
		t.Fatalf("record id = %q", f.RecordID)
	}
}

// D2: a bare recordId is not one record — an id can repeat across
// types — so recordId REQUIRES its recordKind companion, and the two are ANDed.
func TestParseChangeFilterRequiresKindCompanion(t *testing.T) {
	idOnly := httptest.NewRequest(http.MethodGet,
		"/api/v1/changes?recordId=e-1", nil)
	if _, err := parseChangeFilter(idOnly); err == nil {
		t.Fatal("recordId without recordKind must be an error — a bare id is not one record")
	}

	typeOnly := httptest.NewRequest(http.MethodGet,
		"/api/v1/changes?recordKind=samples.substrate.reamde.dev/people/person", nil)
	if _, err := parseChangeFilter(typeOnly); err == nil {
		t.Fatal("recordKind without recordId must be an error")
	}

	both := httptest.NewRequest(http.MethodGet,
		"/api/v1/changes?recordId=e-1&recordKind=samples.substrate.reamde.dev/people/person", nil)
	f, err := parseChangeFilter(both)
	if err != nil {
		t.Fatalf("recordId+recordKind must parse: %v", err)
	}
	if f.RecordID != "e-1" || len(f.Kinds) != 1 || f.Kinds[0] != "samples.substrate.reamde.dev/people/person" {
		t.Fatalf("recordId+recordKind must AND into (id, type): id=%q kinds=%v", f.RecordID, f.Kinds)
	}
}

// D2 end-to-end: recordId alone is a bad_request, refused before the feed is
// read.
func TestChangesRecordIdWithoutKindIsBadRequest(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodGet,
		"/api/v1/changes?recordId=shared", stubToken, nil)
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
}

// changeTypesFromNDJSON reads the ndjson change stream and returns each row's
// type, skipping the bookmark/heartbeat control frames (they carry no type).
func changeTypesFromNDJSON(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var types []string
	sc := bufio.NewScanner(rec.Body)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var row struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatalf("decode ndjson row %q: %v", sc.Text(), err)
		}
		if row.Kind != "" {
			types = append(types, row.Kind)
		}
	}
	return types
}
