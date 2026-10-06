package providertest

// `syncgmail` under one invocation clock: a slow CPU, a response frame its
// staged payloads would overflow, and a history page whose deletions empty
// more threads than one invocation may read. Each fire must hand back inside
// the function's minute and under the runner's limits with what it did
// stored, and the next invocation must carry on from there.

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
)

const googleGmailThreadType = googlePackage + "/gmailthread"

// heavyGmail is one mailbox whose history delta runs over len(perPage)
// pages: page n adds perPage[n] messages and deletes `deleted` messages
// whose threads hold nothing else. Every message is htmlBytes of html with
// no text/plain alternative: link-dense (`<td><a href>w</a></td>` repeated)
// by default, or text-free layout markup when soup is set, which the
// flattener has to read to its work ceiling.
type heavyGmail struct {
	fakeAPI
	historyID string
	perPage   []int
	deleted   int
	htmlBytes int
	soup      bool

	once sync.Once
	html string
}

func (f *heavyGmail) added(page, i int) string { return fmt.Sprintf("h%02d-%03d", page, i) }

func (f *heavyGmail) gone(page, i int) string { return fmt.Sprintf("gone-%02d-%03d", page, i) }

func (f *heavyGmail) body() string {
	f.once.Do(func() {
		var b strings.Builder
		b.WriteString("<html><body><table><tr>")
		for i := 0; b.Len() < f.htmlBytes; i++ {
			if f.soup {
				b.WriteString(`<div class="s" style="height:1px;line-height:1px"><span style="display:none"></span>` +
					`<img src="https://img.example.com/s.gif" width="1" height="1"></div>`)
				continue
			}
			fmt.Fprintf(&b, `<td><a href="https://click.example.com/l/%d">w</a></td>`, i)
		}
		b.WriteString("</tr></table><p>the letter</p></body></html>")
		f.html = b.String()
	})
	return f.html
}

func (f *heavyGmail) start(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		writeJSON(w, map[string]any{"emailAddress": "heavy@example.com", "historyId": f.historyID})
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
		page := 0
		if tok := r.URL.Query().Get("pageToken"); tok != "" {
			page, _ = strconv.Atoi(strings.TrimPrefix(tok, "h"))
		}
		if r.URL.Query().Get("startHistoryId") == f.historyID {
			// Nothing has happened since the mailbox's own historyId.
			writeJSON(w, map[string]any{"historyId": f.historyID})
			return
		}
		var records []any
		for i := range f.perPage[page] {
			id := f.added(page, i)
			records = append(records, map[string]any{"id": f.historyID, "messagesAdded": []any{
				map[string]any{"message": map[string]any{"id": id, "threadId": "t-" + id}},
			}})
		}
		for i := range f.deleted {
			id := f.gone(page, i)
			records = append(records, map[string]any{"id": f.historyID, "messagesDeleted": []any{
				map[string]any{"message": map[string]any{"id": id, "threadId": "t-" + id}},
			}})
		}
		out := map[string]any{"history": records, "historyId": f.historyID}
		if page+1 < len(f.perPage) {
			out["nextPageToken"] = fmt.Sprintf("h%d", page+1)
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
		msg := gmailMessage(id, "heavy")
		html := f.body()
		msg["payload"] = map[string]any{
			"mimeType": "text/html",
			"headers":  msg["payload"].(map[string]any)["headers"],
			"body":     map[string]any{"data": b64url(html), "size": len(html)},
		}
		writeJSON(w, msg)
	})
	mux.HandleFunc("/gmail/v1/users/me/threads/", func(w http.ResponseWriter, r *http.Request) {
		if !bearer(w, r) {
			return
		}
		tid := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/threads/")
		writeJSON(w, map[string]any{
			"id": tid, "historyId": f.historyID, "snippet": "snip",
			"messages": []any{map[string]any{"id": strings.TrimPrefix(tid, "t-"), "threadId": tid}},
		})
	})
	f.serve(t, mux)
}

// emptiedThreads writes the thread row each deletion of the delta leaves
// with no message, under the id the body computes for it, and returns the
// row ids: the history page has to retract every one of them.
func (f *heavyGmail) emptiedThreads(t *testing.T, ds substrate.Dataset, aid string) []string {
	t.Helper()
	var rows []string
	for page := range f.perPage {
		for i := range f.deleted {
			tid := "t-" + f.gone(page, i)
			rid := runner.ExternalID("gmail-thread", aid, tid)
			mustPut(t, ds, substrate.PutInput{
				Kind: googleGmailThreadType, ID: rid,
				Properties: map[string]any{"account": aid, "threadId": tid},
			})
			rows = append(rows, rid)
		}
	}
	return rows
}

// heavyFires runs scheduled fires of `syncgmail` over the account the
// heavyGmail serves until one stamps the delta's watermark, each fire
// invocation by invocation, applying each, and handing the next fire the
// resume the last one stored on the account, the way the hourly schedule
// does. It fails the test on an invocation that errs or takes longer than
// `within`, and on a delta still owed after `fires`. It answers each
// invocation's duration and effects, in order.
func heavyFires(t *testing.T, ds substrate.Dataset, fake *heavyGmail, aid string,
	within time.Duration, fires int,
) ([]time.Duration, [][]engine.StepEffect) {
	t.Helper()
	var took []time.Duration
	var effects [][]engine.StepEffect
	var resume any
	for fire := 1; fire <= fires; fire++ {
		props := syncProps(map[string]any{
			"enabledGmail": true, "syncFrequency": "hourly", "backfillDepth": "all",
			"gmailLastSyncedAt": ago(3 * time.Hour), "gmailBackfillAnchorAt": ago(48 * time.Hour),
			"gmailHistoryId": "8000", "gmailBackfillResume": resume,
		})
		cfg := stepConfig(googleAccountType, aid, props)
		cfg["inputs"] = map[string]any{"client": map[string]any{
			"properties": map[string]any{"apiBase": fake.ts.URL},
		}}
		s := newStepper(t, ds, googleGmailSyncFn, cfg)
		var cursor any
		for n := 1; ; n++ {
			if n > 40 {
				t.Fatalf("fire %d did not drain in 40 invocations", fire)
			}
			began := time.Now()
			got, _, cur, err := s.s.Step(context.Background(), cursor)
			if err != nil {
				t.Fatalf("fire %d invocation %d failed after %s: %v", fire, n, time.Since(began), err)
			}
			took = append(took, time.Since(began))
			if took[len(took)-1] > within {
				t.Fatalf("fire %d invocation %d took %s, past %s", fire, n, took[len(took)-1], within)
			}
			s.apply()
			effects = append(effects, got)
			if cur == nil {
				break
			}
			cursor = cur
		}
		row := mustGet(t, ds, googleAccountType, aid)
		if row.Properties["gmailHistoryId"] == fake.historyID {
			return took, effects
		}
		resume = row.Properties["gmailBackfillResume"]
	}
	t.Fatalf("the delta is still owed after %d fires; took %v", fires, took)
	return nil, nil
}

// assertRetracted fails the test on a row of `ids` that is still live.
func assertRetracted(t *testing.T, ds substrate.Dataset, kind string, ids []string) {
	t.Helper()
	for _, id := range ids {
		row, err := ds.Get(context.Background(), kind, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if row.DeletedAt == nil {
			t.Fatalf("thread %s outlived the deletion of its last message", id)
		}
	}
}

// messagesIn is the Gmail ids of the message rows an invocation put.
func messagesIn(effects []engine.StepEffect) []string {
	var ids []string
	for _, ef := range effects {
		if ef.Action == "put" && ef.Kind == googleGmailMsgType {
			if id, ok := ef.Properties["messageId"].(string); ok && ef.Properties["payload"] != nil {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// TestGoogleGmailASlowCPUHandsBackInsideTheMinute is the 2026-10-06 finding
// on an arm board: the work after a fetch had no clock, and fifteen messages
// of html flattened there took the invocation past its minute. Here the
// flattener is slowed to 6 s per 600 KB message (a sleep per 64 KB chunk)
// over a two-page history delta of 12 and 3 messages, so the old body spent
// 72 s in one hydrate and the runner killed it with nothing stored. Every invocation now
// hands back well inside the minute with the messages it read stored, the
// next carries on from the rest, a flatten that ran into its own 3 s stop
// says so in the body, and by the next fire all fifteen are mirrored and the
// watermark has advanced (the chain's own 45 s hands the thread reads of the
// last three to it).
func TestGoogleGmailASlowCPUHandsBackInsideTheMinute(t *testing.T) {
	requirePython(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	installRewired(t, ds, googleDir, [2]string{
		"            parser.feed(markup[cut:cut + HTML_CHUNK])\n",
		"            __import__(\"time\").sleep(0.6)\n            parser.feed(markup[cut:cut + HTML_CHUNK])\n",
	})
	fake := &heavyGmail{historyID: "9000", perPage: []int{12, 3}, deleted: 2, htmlBytes: 600000, soup: true}
	fake.start(t)
	const aid = "acct-slowcpu"
	mustPut(t, ds, substrate.PutInput{Kind: googleAccountType, ID: aid, Properties: map[string]any{"enabledGmail": true}})
	emptied := fake.emptiedThreads(t, ds, aid)

	took, effects := heavyFires(t, ds, fake, aid, 50*time.Second, 2)

	// The first hydrate stopped short of the page and stored what it read.
	first := -1
	for i, efs := range effects {
		if len(messagesIn(efs)) > 0 {
			first = i
			break
		}
	}
	if first < 0 {
		t.Fatalf("no invocation stored a message; took %v", took)
	}
	if n := len(messagesIn(effects[first])); n == 0 || n >= 12 {
		t.Fatalf("the first hydrate stored %d of the page's 12 messages; want a clock cut", n)
	}
	mirrored := gmailMessageIDs(t, ds)
	for page, n := range fake.perPage {
		for i := range n {
			if id := fake.added(page, i); !mirrored[id] {
				t.Fatalf("message %s is not mirrored after the fire; took %v", id, took)
			}
		}
	}
	var cut bool
	for _, row := range listLive(t, ds, googleGmailMsgType) {
		if body, _ := row.Properties["body"].(string); strings.Contains(body, "flattening the message's html took too long") {
			cut = true
		}
	}
	if !cut {
		t.Fatal("no body says its flatten was cut short")
	}
	assertRetracted(t, ds, googleGmailThreadType, emptied)
	if resume, _ := mustGet(t, ds, googleAccountType, aid).Properties["gmailBackfillResume"].(map[string]any); len(resume) != 0 {
		t.Fatalf("resume = %v after the watermark landed, want nothing owed", resume)
	}
}

// TestGoogleGmailAHydrateFitsTheResponseFrame: fifteen messages of 600 KB
// html are 12 MB of base64 in one hydrate, and the runner refuses a response
// frame past 8 MiB and keeps nothing of it, so the old body failed every
// fire with nothing stored. The hydrate now stops fetching once its staged
// payloads reach 5 MB, and the fire stores all fifteen over three
// invocations.
func TestGoogleGmailAHydrateFitsTheResponseFrame(t *testing.T) {
	requirePython(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, googleDir, nil)
	fake := &heavyGmail{historyID: "9000", perPage: []int{15}, htmlBytes: 600000}
	fake.start(t)
	const aid = "acct-frame"
	mustPut(t, ds, substrate.PutInput{Kind: googleAccountType, ID: aid, Properties: map[string]any{"enabledGmail": true}})

	_, effects := heavyFires(t, ds, fake, aid, 50*time.Second, 1)

	hydrates := 0
	for _, efs := range effects {
		if n := len(messagesIn(efs)); n > 0 {
			hydrates++
			if n > 7 {
				t.Fatalf("one invocation staged %d messages of 800 KB payload each, past the frame budget", n)
			}
		}
	}
	if hydrates < 3 {
		t.Fatalf("the messages were staged in %d invocations, want the frame budget to split them", hydrates)
	}
	if got := len(gmailMessageIDs(t, ds)); got != 15 {
		t.Fatalf("mirrored %d messages, want 15", got)
	}
}

// TestGoogleGmailAHistoryPageEmptyingManyThreadsCompletes: each thread a
// history page's deletions empty costs two host reads, and the engine fails
// the whole delivery at read 257 of an invocation, so a page that emptied
// 150 threads parked the fire on its first invocation, every hour. The
// threads past the invocation's 200 reads are now judged at the top of the
// next one, and every emptied thread row is retracted.
func TestGoogleGmailAHistoryPageEmptyingManyThreadsCompletes(t *testing.T) {
	requirePython(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, googleDir, nil)
	fake := &heavyGmail{historyID: "9000", perPage: []int{2}, deleted: 150, htmlBytes: 2000}
	fake.start(t)
	const aid = "acct-emptied"
	mustPut(t, ds, substrate.PutInput{Kind: googleAccountType, ID: aid, Properties: map[string]any{"enabledGmail": true}})
	emptied := fake.emptiedThreads(t, ds, aid)

	heavyFires(t, ds, fake, aid, 50*time.Second, 1)

	assertRetracted(t, ds, googleGmailThreadType, emptied)
	if got := len(gmailMessageIDs(t, ds)); got != 2 {
		t.Fatalf("mirrored %d messages, want the page's 2", got)
	}
}
