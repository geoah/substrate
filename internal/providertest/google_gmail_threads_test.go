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
// first member that has one, else by its first member's Gmail snippet, else,
// while it is thin, by its id; a message by its subject, else by its Gmail
// snippet, else by the opening of its body. Where a fixture has both, its
// snippet lacks the body's final period, so a title says which one it read.
func TestGoogleGmailRowsAreTitledByWhatTheMailSays(t *testing.T) {
	requirePython(t)
	t.Parallel()
	_, ds := newCoreDataset(t)

	fake := &threadedGmail{
		historyID: "9000",
		// m-2a is older than the delta: its thread embeds it, and nothing
		// reads it whole.
		delta: []string{"m-1a", "m-1b", "m-2b", "m-3", "m-4a", "m-4b"},
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
			// Read whole, and Gmail's preview is all it says: no Subject
			// header and no text part.
			"m-4a": gmailMail("m-4a", "t-4", "Photos from the trip", "",
				[2]string{"From", "grace@example.com"}),
			// No Subject header and no preview: the body is what is left.
			"m-4b": gmailMail("m-4b", "t-4", "", "Only the body says anything.",
				[2]string{"From", "ada@example.com"}),
		},
		// t-3 is not here: its read answers 404 and the row stays thin.
		threads: map[string][]string{
			"t-1": {"m-1a", "m-1b"}, "t-2": {"m-2a", "m-2b"}, "t-4": {"m-4a", "m-4b"},
		},
	}
	syncThreadedGmail(t, ds, fake)

	threads := titlesBy(t, ds, googleGmailThreadKind, "threadId")
	for tid, want := range map[string]string{
		"t-1": "Quarterly plan",
		"t-2": "The opening of an older note",
		"t-3": "t-3",
		"t-4": "Photos from the trip",
	} {
		if got, ok := threads[tid]; !ok || got != want {
			t.Errorf("thread %s is titled %q (mirrored %v), want %q", tid, got, ok, want)
		}
	}
	messages := titlesBy(t, ds, googleGmailMsgKind, "messageId")
	for mid, want := range map[string]string{
		"m-1a": "Quarterly plan",
		"m-1b": "Re: Quarterly plan",
		"m-2b": "A reply with no subject line",
		"m-3":  "Lunch",
		// Embedded by its thread and never read: no subject and no body yet,
		// and the preview the thread carried for it.
		"m-2a": "The opening of an older note",
		"m-4a": "Photos from the trip",
		"m-4b": "Only the body says anything.",
	} {
		if got, ok := messages[mid]; !ok || got != want {
			t.Errorf("message %s is titled %q (mirrored %v), want %q", mid, got, ok, want)
		}
	}
}

// TestGoogleGmailReplyAddressesMakeNoContact: GitHub's notification mail
// names mailboxes that are no party. `Reply-To` is a fresh
// `reply+…@reply.github.com` for every thread, and `To`/`Cc` carry the reason
// the mail was sent, `repo@`, `mention@` and the rest at
// `noreply.github.com`. Each became an `emailaddress` hub row and so, through
// core's mapping, a contact of its own: an account following GitHub filled
// its contacts with them. Such an address keeps its occurrence on the
// message, spelled and named as the header had it, and gets no hub row and no
// contact, so GitHub is one contact, its sender. A person's commit address
// under `users.noreply.github.com` is an identity and keeps both.
func TestGoogleGmailReplyAddressesMakeNoContact(t *testing.T) {
	requirePython(t)
	t.Parallel()
	_, ds := newCoreDataset(t)

	relay := func(token string) string { return "reply+" + token + "@reply.github.com" }
	notification := func(id, tid, token, cc string) map[string]any {
		return gmailMail(id, tid, "A comment", "A comment.",
			[2]string{"Subject", "Re: [owner/repo] A pull request (PR #" + id + ")"},
			[2]string{"From", "Grace <notifications@github.com>"},
			[2]string{"To", "owner/repo <repo@noreply.github.com>"},
			[2]string{"Cc", cc},
			[2]string{"Reply-To", "owner/repo <" + relay(token) + ">"})
	}
	const person = "12345+grace@users.noreply.github.com"
	fake := &threadedGmail{
		historyID: "9000",
		delta:     []string{"n-1", "n-2", "p-1"},
		mail: map[string]map[string]any{
			"n-1": notification("n-1", "t-n1", "AAEXAMPLEONE000000000000000000",
				"Mention <mention@noreply.github.com>"),
			"n-2": notification("n-2", "t-n2", "AAEXAMPLETWO000000000000000000",
				"Author <Author@NoReply.GitHub.com>, Your activity <your_activity@noreply.github.com>"),
			"p-1": gmailMail("p-1", "t-p1", "A patch", "A patch.",
				[2]string{"Subject", "[PATCH] Fix the title"},
				[2]string{"From", "Grace <" + person + ">"}),
		},
		threads: map[string][]string{"t-n1": {"n-1"}, "t-n2": {"n-2"}, "t-p1": {"p-1"}},
	}
	syncThreadedGmail(t, ds, fake)

	// A relay domain is matched exactly: the reasons' domain, never one
	// under it.
	domain := func(addr string) string {
		return strings.ToLower(addr[strings.LastIndexByte(addr, '@')+1:])
	}
	relayed := func(addr string) bool {
		d := domain(addr)
		return d == "reply.github.com" || d == "noreply.github.com"
	}
	hub := map[string]bool{}
	for _, row := range listLive(t, ds, googlePackage+"/emailaddress") {
		if addr, ok := row.Properties["address"].(string); ok {
			hub[addr] = true
		}
	}
	if !hub["notifications@github.com"] || !hub[person] {
		t.Fatalf("hub rows = %v, want GitHub's sender and the person", hub)
	}
	for addr := range hub {
		if relayed(addr) {
			t.Errorf("a relay address has a hub row: %s", addr)
		}
	}

	// GitHub is ONE contact, and the person under its domain is another.
	carrying := map[string][]string{}
	for _, row := range listLive(t, ds, vocabulary.CoreKind("contact")) {
		emails, _ := row.Properties["emails"].([]any)
		for _, e := range emails {
			addr, _ := e.(string)
			if relayed(addr) {
				t.Errorf("contact %s carries a relay address: %v", row.ID, emails)
			}
			if d := domain(addr); d == "github.com" || strings.HasSuffix(d, ".github.com") {
				carrying[addr] = append(carrying[addr], row.ID)
			}
		}
	}
	if len(carrying) != 2 || len(carrying["notifications@github.com"]) != 1 || len(carrying[person]) != 1 {
		t.Fatalf("contacts carrying a GitHub address = %v, want one for notifications@github.com and one for %s",
			carrying, person)
	}

	row := mustGet(t, ds, googleGmailMsgKind, message(t, ds, "n-1"))
	for name, want := range map[string]map[string]any{
		"replyTo": {"value": relay("AAEXAMPLEONE000000000000000000"), "name": "owner/repo"},
		"to":      {"value": "repo@noreply.github.com", "name": "owner/repo"},
		"cc":      {"value": "mention@noreply.github.com", "name": "Mention"},
	} {
		entries, _ := row.Properties[name].([]any)
		if len(entries) != 1 {
			t.Fatalf("%s = %v, want the one occurrence", name, row.Properties[name])
		}
		entry, _ := entries[0].(map[string]any)
		if entry["value"] != want["value"] || entry["name"] != want["name"] {
			t.Errorf("%s entry = %v, want the address and the name as the header spelled them", name, entry)
		}
		if _, ok := entry["address"]; ok {
			t.Errorf("%s entry = %v, names a hub row", name, entry)
		}
	}
	from, _ := row.Properties["from"].([]any)
	if len(from) != 1 || storedRefPath(from[0].(map[string]any)["address"]) == "" {
		t.Fatalf("from = %v, want the sender's occurrence naming its hub row", row.Properties["from"])
	}
}
