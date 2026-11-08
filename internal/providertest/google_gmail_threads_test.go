package providertest

// The google bundle's `syncgmail` over a mailbox whose threads answer
// `threads.get`: what each mirrored thread and message is called once the
// sync has written it, and which header addresses become hub rows.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// threadedGmail is one mailbox whose history delta names `delta`, whose
// `messages.get` answers every message in `mail`, and whose `threads.get`
// answers every thread in `threads` with its members, in order, as the
// minimal Message objects Gmail embeds. A thread it does not name answers
// 404, which leaves that thread thin.
type threadedGmail struct {
	fakeAPI
	historyID string
	delta     []string
	mail      map[string]map[string]any
	threads   map[string][]string
}

func (f *threadedGmail) start(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		writeJSON(w, map[string]any{"emailAddress": "owner@example.com", "historyId": f.historyID})
	})
	mux.HandleFunc("/gmail/v1/users/me/labels", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		writeJSON(w, map[string]any{"labels": []any{}})
	})
	mux.HandleFunc("/gmail/v1/users/me/history", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		var records []any
		for _, id := range f.delta {
			records = append(records, map[string]any{
				"id": f.historyID,
				"messagesAdded": []any{map[string]any{
					"message": map[string]any{"id": id, "threadId": f.mail[id]["threadId"]},
				}},
			})
		}
		writeJSON(w, map[string]any{"history": records, "historyId": f.historyID})
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		msg, ok := f.mail[strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, msg)
	})
	mux.HandleFunc("/gmail/v1/users/me/threads/", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		tid := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/threads/")
		members, ok := f.threads[tid]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var embedded []any
		snippet := ""
		for _, id := range members {
			msg := f.mail[id]
			minimal := map[string]any{}
			for _, key := range []string{"id", "threadId", "labelIds", "snippet", "historyId", "internalDate", "sizeEstimate"} {
				if v, ok := msg[key]; ok {
					minimal[key] = v
				}
			}
			embedded = append(embedded, minimal)
			snippet, _ = msg["snippet"].(string)
		}
		writeJSON(w, map[string]any{"id": tid, "historyId": f.historyID, "snippet": snippet, "messages": embedded})
	})
	f.serve(t, mux)
}

// gmailMail is one `messages.get?format=full` answer: the headers as given,
// in order, and a text/plain body.
func gmailMail(id, tid, snippet, body string, headers ...[2]string) map[string]any {
	lines := []any{}
	for _, h := range headers {
		lines = append(lines, map[string]any{"name": h[0], "value": h[1]})
	}
	return map[string]any{
		"id": id, "threadId": tid, "historyId": "100",
		"internalDate": millisAgo(2 * time.Hour), "snippet": snippet,
		"sizeEstimate": 1234, "labelIds": []any{"INBOX"},
		"payload": map[string]any{
			"mimeType": "text/plain",
			"headers":  lines,
			"body":     map[string]any{"data": b64url(body), "size": len(body)},
		},
	}
}

// syncThreadedGmail runs one history delta of `syncgmail` against the fake
// for a fresh account, to completion.
func syncThreadedGmail(t *testing.T, ds substrate.Dataset, fake *threadedGmail) {
	t.Helper()
	install(t, ds, googleDir, nil)
	fake.start(t)
	mustPut(t, ds, substrate.PutInput{
		Kind: googleAccountKind, ID: "acct-mail", Properties: map[string]any{"enabledGmail": true},
	})
	props := syncProps(map[string]any{
		"enabledGmail": true, "backfillDepth": "all",
		"gmailLastSyncedAt": ago(3 * time.Hour), "gmailBackfillAnchorAt": ago(48 * time.Hour),
		"gmailHistoryId": "8000",
	})
	cfg := stepConfig(googleAccountKind, "acct-mail", props)
	cfg["inputs"] = map[string]any{"client": map[string]any{
		"properties": map[string]any{"apiBase": fake.ts.URL},
	}}
	effects := newStepper(t, ds, googleGmailSyncFn, cfg).drainApplying(nil)
	if stamp := stampOf(effects, "acct-mail"); stamp == nil || stamp["gmailHistoryId"] != fake.historyID {
		t.Fatalf("the sync did not complete: stamp %v", stamp)
	}
}

// titlesBy is the title of every live row of one kind, keyed by one of its
// string properties.
func titlesBy(t *testing.T, ds substrate.Dataset, kind, key string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, row := range listLive(t, ds, kind) {
		if id, ok := row.Properties[key].(string); ok {
			out[id] = row.Title
		}
	}
	return out
}

// TestGoogleGmailRowsAreTitledByWhatTheMailSays: a thread was titled by its
// Gmail thread id, a hex string that named nothing a reader recognizes,
// wherever a record label appears. A thread is titled by the subject of its
// first member that has one, else by its first member's snippet, else, while
// it is thin, by its id; a message by its subject, else by the opening of its
// body.
func TestGoogleGmailRowsAreTitledByWhatTheMailSays(t *testing.T) {
	requirePython(t)
	t.Parallel()
	_, ds := newCoreDataset(t)

	fake := &threadedGmail{
		historyID: "9000",
		// m-2a is older than the delta: its thread embeds it, and nothing
		// reads it whole.
		delta: []string{"m-1a", "m-1b", "m-2b", "m-3"},
		mail: map[string]map[string]any{
			"m-1a": gmailMail("m-1a", "t-1", "Numbers attached", "Numbers attached.",
				[2]string{"Subject", "Quarterly plan"}, [2]string{"From", "ada@example.com"}),
			"m-1b": gmailMail("m-1b", "t-1", "Looks right", "Looks right.",
				[2]string{"Subject", "Re: Quarterly plan"}, [2]string{"From", "grace@example.com"}),
			"m-2a": gmailMail("m-2a", "t-2", "The opening of an older note", "The opening of an older note.",
				[2]string{"Subject", "Never read whole"}, [2]string{"From", "ada@example.com"}),
			"m-2b": gmailMail("m-2b", "t-2", "A reply with no subject line", "A reply with no subject line.",
				[2]string{"From", "grace@example.com"}),
			"m-3": gmailMail("m-3", "t-3", "Noon?", "Noon?",
				[2]string{"Subject", "Lunch"}, [2]string{"From", "ada@example.com"}),
		},
		// t-3 is not here: its read answers 404 and the row stays thin.
		threads: map[string][]string{"t-1": {"m-1a", "m-1b"}, "t-2": {"m-2a", "m-2b"}},
	}
	syncThreadedGmail(t, ds, fake)

	threads := titlesBy(t, ds, googleGmailThreadKind, "threadId")
	for tid, want := range map[string]string{
		"t-1": "Quarterly plan",
		"t-2": "The opening of an older note",
		"t-3": "t-3",
	} {
		if got, ok := threads[tid]; !ok || got != want {
			t.Errorf("thread %s is titled %q (mirrored %v), want %q", tid, got, ok, want)
		}
	}
	messages := titlesBy(t, ds, googleGmailMsgKind, "messageId")
	for mid, want := range map[string]string{
		"m-1a": "Quarterly plan",
		"m-1b": "Re: Quarterly plan",
		"m-2b": "A reply with no subject line.",
		"m-3":  "Lunch",
		// Embedded by its thread and never read: no subject and no body yet.
		"m-2a": "",
	} {
		if got, ok := messages[mid]; !ok || got != want {
			t.Errorf("message %s is titled %q (mirrored %v), want %q", mid, got, ok, want)
		}
	}
}

// TestGoogleGmailReplyAddressesMakeNoContact: a notification service that
// takes replies by mail names a fresh `Reply-To` mailbox for every thread,
// and each one became an `emailaddress` hub row and so, through core's
// mapping, a contact of its own: an account following GitHub filled its
// contacts with one `reply+…@reply.github.com` per thread. Such an address
// keeps its occurrence on the message, spelled as the header spelled it, and
// gets no hub row and no contact. The service's one sender is an identity and
// keeps both.
func TestGoogleGmailReplyAddressesMakeNoContact(t *testing.T) {
	requirePython(t)
	t.Parallel()
	_, ds := newCoreDataset(t)

	relay := func(token string) string { return "reply+" + token + "@reply.github.com" }
	notification := func(id, tid, token string) map[string]any {
		return gmailMail(id, tid, "A comment", "A comment.",
			[2]string{"Subject", "Re: [owner/repo] A pull request (PR #" + id + ")"},
			[2]string{"From", "Grace <notifications@github.com>"},
			[2]string{"To", "owner/repo <repo@noreply.github.com>"},
			[2]string{"Reply-To", "owner/repo <" + relay(token) + ">"})
	}
	fake := &threadedGmail{
		historyID: "9000",
		delta:     []string{"n-1", "n-2"},
		mail: map[string]map[string]any{
			"n-1": notification("n-1", "t-n1", "AAEXAMPLEONE000000000000000000"),
			"n-2": notification("n-2", "t-n2", "AAEXAMPLETWO000000000000000000"),
		},
		threads: map[string][]string{"t-n1": {"n-1"}, "t-n2": {"n-2"}},
	}
	syncThreadedGmail(t, ds, fake)

	hub := map[string]bool{}
	for _, row := range listLive(t, ds, googlePackage+"/emailaddress") {
		if addr, ok := row.Properties["address"].(string); ok {
			hub[addr] = true
		}
	}
	if !hub["notifications@github.com"] || !hub["repo@noreply.github.com"] {
		t.Fatalf("hub rows = %v, want the sender and the recipient", hub)
	}
	for addr := range hub {
		if strings.HasSuffix(addr, "@reply.github.com") {
			t.Errorf("a reply address has a hub row: %s", addr)
		}
	}

	var sender bool
	for _, row := range listLive(t, ds, vocabulary.CoreKind("contact")) {
		emails, _ := row.Properties["emails"].([]any)
		for _, e := range emails {
			addr, _ := e.(string)
			if strings.HasSuffix(addr, "@reply.github.com") {
				t.Errorf("contact %s carries a reply address: %v", row.ID, emails)
			}
			sender = sender || addr == "notifications@github.com"
		}
	}
	if !sender {
		t.Error("no contact carries notifications@github.com: core's mapping did not run on the hub")
	}

	row := mustGet(t, ds, googleGmailMsgKind, message(t, ds, "n-1"))
	replyTo, _ := row.Properties["replyTo"].([]any)
	if len(replyTo) != 1 {
		t.Fatalf("replyTo = %v, want the one occurrence", row.Properties["replyTo"])
	}
	entry, _ := replyTo[0].(map[string]any)
	if entry["value"] != relay("AAEXAMPLEONE000000000000000000") || entry["name"] != "owner/repo" {
		t.Fatalf("replyTo entry = %v, want the address and the name as the header spelled them", entry)
	}
	if _, ok := entry["address"]; ok {
		t.Fatalf("replyTo entry = %v, names a hub row", entry)
	}
	from, _ := row.Properties["from"].([]any)
	if len(from) != 1 || storedRefPath(from[0].(map[string]any)["address"]) == "" {
		t.Fatalf("from = %v, want the sender's occurrence naming its hub row", row.Properties["from"])
	}
}
