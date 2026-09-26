package providertest

// The google bundle's `syncgmail` over a scheduled queue of TWO accounts,
// stepped against a loopback Gmail that serves one mailbox per token (#651).
// One account is resuming a long backfill; the other only owes a history
// delta. A scheduled run must stamp both, and the account holding the
// backfill must mirror the mail that arrived since that walk began.

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

const (
	googleDir          = providersDir + "/google"
	googlePackage      = "providers.substrate.reamde.dev/google"
	googleAccountType  = googlePackage + "/account"
	googleGmailMsgType = googlePackage + "/gmailmessage"
	googleGmailSyncFn  = googlePackage + "/syncgmail"
	gmailBackfillPages = 30
	gmailListPage      = 100
)

// gmailBox is one account's mailbox: the profile's historyId, the ids a
// history delta reports as added, and a paged messages.list. Every
// messages.get for an id the box names answers a parseable message.
type gmailBox struct {
	historyID string
	added     []string
	pages     int
	prefix    string
}

func (b *gmailBox) page(n int) []string {
	ids := make([]string, 0, gmailListPage)
	for i := range gmailListPage {
		ids = append(ids, fmt.Sprintf("%s-p%02d-%03d", b.prefix, n, i))
	}
	return ids
}

// fakeGmailBoxes routes each request to the mailbox its bearer names and logs
// the calls per mailbox in order, so a test can say which came first.
type fakeGmailBoxes struct {
	fakeAPI
	boxes map[string]*gmailBox

	mu    sync.Mutex
	calls map[string][]string
}

func (f *fakeGmailBoxes) log(token, call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string][]string{}
	}
	f.calls[token] = append(f.calls[token], call)
}

func (f *fakeGmailBoxes) callsOf(token string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls[token]...)
}

func (f *fakeGmailBoxes) start(t *testing.T) {
	t.Helper()
	box := func(w http.ResponseWriter, r *http.Request) (string, *gmailBox) {
		f.record(r)
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		b, ok := f.boxes[token]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return "", nil
		}
		return token, b
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		token, b := box(w, r)
		if b == nil {
			return
		}
		f.log(token, "profile")
		writeJSON(w, map[string]any{"emailAddress": b.prefix + "@example.com", "historyId": b.historyID})
	})
	mux.HandleFunc("/gmail/v1/users/me/labels", func(w http.ResponseWriter, r *http.Request) {
		if _, b := box(w, r); b == nil {
			return
		}
		writeJSON(w, map[string]any{"labels": []any{}})
	})
	mux.HandleFunc("/gmail/v1/users/me/history", func(w http.ResponseWriter, r *http.Request) {
		token, b := box(w, r)
		if b == nil {
			return
		}
		f.log(token, "history:"+r.URL.Query().Get("startHistoryId"))
		var records []any
		for _, id := range b.added {
			records = append(records, map[string]any{
				"id": b.historyID,
				"messagesAdded": []any{map[string]any{
					"message": map[string]any{"id": id, "threadId": "t-" + id},
				}},
			})
		}
		writeJSON(w, map[string]any{"history": records, "historyId": b.historyID})
	})
	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		token, b := box(w, r)
		if b == nil {
			return
		}
		tok := r.URL.Query().Get("pageToken")
		f.log(token, "list:"+tok)
		n := 0
		if tok != "" {
			v, err := strconv.Atoi(strings.TrimPrefix(tok, "p"))
			if err != nil || v < 1 || v >= b.pages {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			n = v
		}
		var items []any
		for _, id := range b.page(n) {
			items = append(items, map[string]any{"id": id, "threadId": "t-" + id})
		}
		out := map[string]any{"messages": items}
		if n+1 < b.pages {
			out["nextPageToken"] = fmt.Sprintf("p%d", n+1)
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		_, b := box(w, r)
		if b == nil {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
		if !strings.HasPrefix(id, b.prefix+"-") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, gmailMessage(id, b.prefix))
	})
	// threads.get is left unanswered: a 404 keeps every thread thin, which
	// the body tolerates, and nothing here asserts on threads.
	f.serve(t, mux)
}

// gmailMessage is one messages.get payload the body parses.
func gmailMessage(id, owner string) map[string]any {
	body := "body of " + id
	return map[string]any{
		"id": id, "threadId": "t-" + id, "historyId": "100",
		"internalDate": millisAgo(2 * time.Hour), "snippet": "snip " + id,
		"sizeEstimate": 1234, "labelIds": []any{"INBOX"},
		"payload": map[string]any{
			"mimeType": "text/plain",
			"headers": []any{
				map[string]any{"name": "Subject", "value": "subject " + id},
				map[string]any{"name": "From", "value": "sender@example.com"},
				map[string]any{"name": "To", "value": owner + "@example.com"},
				map[string]any{"name": "Message-ID", "value": "<" + id + "@example.com>"},
			},
			"body": map[string]any{"data": b64url(body), "size": len(body)},
		},
	}
}

func b64url(s string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var out strings.Builder
	data := []byte(s)
	for i := 0; i < len(data); i += 3 {
		var chunk [3]byte
		n := copy(chunk[:], data[i:])
		out.WriteByte(alphabet[chunk[0]>>2])
		out.WriteByte(alphabet[(chunk[0]&0x03)<<4|chunk[1]>>4])
		if n > 1 {
			out.WriteByte(alphabet[(chunk[1]&0x0f)<<2|chunk[2]>>6])
		}
		if n > 2 {
			out.WriteByte(alphabet[chunk[2]&0x3f])
		}
	}
	return out.String()
}

// gmailMessageIDs is the Gmail message ids of every mirrored message.
func gmailMessageIDs(t *testing.T, ds substrate.Dataset) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, row := range listLive(t, ds, googleGmailMsgType) {
		if id, ok := row.Properties["messageId"].(string); ok {
			out[id] = true
		}
	}
	return out
}

// TestGoogleGmailScheduledRunServesEveryAccount is #651: one scheduled run
// over two due accounts, the first resuming a 30-page backfill at page 3 and
// the second owing only a history delta. Before the fix the first account
// spent the chain's twelve invocations on its walk and the second met the
// budget at `plan`, unstamped; and the first resumed its walk before the
// history delta, so the mail that arrived since the walk began stayed
// unmirrored until the walk finished.
func TestGoogleGmailScheduledRunServesEveryAccount(t *testing.T) {
	requirePython(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, googleDir, nil)

	fake := &fakeGmailBoxes{boxes: map[string]*gmailBox{
		"at-a": {historyID: "5000", added: []string{"a-new"}, pages: gmailBackfillPages, prefix: "a"},
		"at-b": {historyID: "7000", added: []string{"b-new"}, pages: 1, prefix: "b"},
	}}
	fake.start(t)

	for _, id := range []string{"acct-a", "acct-b"} {
		mustPut(t, ds, substrate.PutInput{
			Kind: googleAccountType, ID: id,
			Properties: map[string]any{"enabledGmail": true},
		})
	}
	base := map[string]any{
		"enabledGmail": true, "syncFrequency": "hourly", "backfillDepth": "all",
		"gmailLastSyncedAt": ago(3 * time.Hour), "gmailBackfillAnchorAt": ago(48 * time.Hour),
	}
	propsA := syncProps(base)
	propsA["gmailHistoryId"] = "4000"
	propsA["gmailBackfillResume"] = map[string]any{
		"pageToken": "p3", "historyId": "4500", "generation": "g-a",
	}
	propsB := syncProps(base)
	propsB["gmailHistoryId"] = "6000"
	config := func(a map[string]any) map[string]any {
		return map[string]any{
			"accounts": []any{
				map[string]any{"id": "acct-a", "type": googleAccountType, "properties": a, "token": "at-a"},
				map[string]any{"id": "acct-b", "type": googleAccountType, "properties": propsB, "token": "at-b"},
			},
			"inputs": map[string]any{"client": map[string]any{
				"properties": map[string]any{"apiBase": fake.ts.URL},
			}},
		}
	}

	// No envelope: the scheduled path, which queues every due account.
	s := newStepper(t, ds, googleGmailSyncFn, config(propsA))
	effects := s.drainApplying(nil)

	stampB := stampOf(effects, "acct-b")
	if stampB == nil {
		t.Fatalf("acct-b was never stamped: the first account spent the run (calls %v)",
			fake.callsOf("at-b"))
	}
	if stampB["gmailLastSyncedAt"] == nil || stampB["gmailHistoryId"] != "7000" {
		t.Fatalf("acct-b stamp = %v, want gmailLastSyncedAt and gmailHistoryId 7000", stampB)
	}

	callsA := fake.callsOf("at-a")
	if len(callsA) < 2 || callsA[1] != "history:4500" {
		t.Fatalf("acct-a calls = %v, want the history delta from the walk's watermark 4500 before its listing", callsA)
	}
	mirrored := gmailMessageIDs(t, ds)
	for _, id := range []string{"a-new", "b-new"} {
		if !mirrored[id] {
			t.Fatalf("message %s is not mirrored; acct-a calls %v", id, callsA)
		}
	}

	stampA := stampOf(effects, "acct-a")
	if stampA == nil {
		t.Fatal("acct-a was never stamped")
	}
	if stampA["gmailLastSyncedAt"] != nil {
		t.Fatalf("acct-a stamped gmailLastSyncedAt with its walk still owed: %v", stampA)
	}
	resume, _ := stampA["gmailBackfillResume"].(map[string]any)
	if resume["pageToken"] == nil || resume["pageToken"] == "p3" || resume["historyId"] != "4500" {
		t.Fatalf("acct-a resume = %v, want the walk moved past p3 under its watermark 4500", resume)
	}
	if resume["deltaHistoryId"] != "5000" {
		t.Fatalf("acct-a resume = %v, want deltaHistoryId 5000 (this run's profile read)", resume)
	}

	// The next run starts the delta where this one ended, not at the walk's
	// watermark again. The stored row is what the injected config carries.
	row := mustGet(t, ds, googleAccountType, "acct-a")
	next := syncProps(base)
	next["gmailHistoryId"] = "4000"
	next["gmailBackfillResume"] = row.Properties["gmailBackfillResume"]
	fake.boxes["at-a"].historyID = "5100"
	before := len(fake.callsOf("at-a"))
	s2 := newStepper(t, ds, googleGmailSyncFn, config(next))
	s2.drainApplying(nil)
	callsA = fake.callsOf("at-a")[before:]
	if len(callsA) < 2 || callsA[1] != "history:5000" {
		t.Fatalf("second run acct-a calls = %v, want the delta from 5000", callsA)
	}
}
