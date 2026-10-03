package providertest

// The Slack bundle's `slacksync` against a loopback Slack Web API. The fake
// serves one workspace whose users, channels, history and refusals a case
// sets, and counts every call per method and per argument, so a case can say
// which calls a run spent. A run is stepped page by page with every page's
// effects applied, the account row as stored riding the injected config the
// way the dispatcher hands it over.

import (
	"bytes"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

const (
	slackDir         = providersDir + "/slack"
	slackPackage     = "providers.substrate.reamde.dev/slack"
	slackAccountType = slackPackage + "/account"
	slackConfigType  = slackPackage + "/config"
	slackSyncFn      = slackPackage + "/slacksync"
	slackAccountID   = "acct"
	slackTeam        = "T0TEST"
	slackOwner       = "U0OWNER"
)

// fakeSlack is one workspace. `refuse` maps "<method> <argument>" to the
// error word Slack answers that call with; the argument is the channel, file,
// user or bot the call names.
type fakeSlack struct {
	fakeAPI

	mu       sync.Mutex
	users    []map[string]any
	channels []map[string]any
	history  map[string][]map[string]any
	// replies maps "<channel> <thread ts>" to the conversations.replies
	// page for that thread; a thread with no entry answers an empty page.
	replies map[string][]map[string]any
	refuse  map[string]string
	calls   map[string]int
	order   []string
	// threads is every conversations.replies call, "<channel> <thread ts>",
	// in order.
	threads []string
}

func newFakeSlack(t *testing.T) *fakeSlack {
	t.Helper()
	f := &fakeSlack{
		history: map[string][]map[string]any{},
		replies: map[string][]map[string]any{},
		refuse:  map[string]string{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !bearer(w, r) {
			return
		}
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		q := r.URL.Query()
		arg := q.Get("channel") + q.Get("file") + q.Get("user") + q.Get("bot")
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.calls == nil {
			f.calls = map[string]int{}
		}
		f.calls[method]++
		f.calls[method+" "+arg]++
		f.order = append(f.order, method+" "+arg)
		if word, ok := f.refuse[method+" "+arg]; ok {
			writeJSON(w, map[string]any{"ok": false, "error": word})
			return
		}
		switch method {
		case "auth.test":
			writeJSON(w, map[string]any{
				"ok": true, "team_id": slackTeam, "user_id": slackOwner,
				"url": "https://example.slack.com/",
			})
		case "team.info":
			writeJSON(w, map[string]any{"ok": true, "team": map[string]any{"id": slackTeam, "name": "Example"}})
		case "users.list":
			members := []any{slackUser(slackOwner)}
			for _, u := range f.users {
				members = append(members, u)
			}
			writeJSON(w, map[string]any{"ok": true, "members": members})
		case "conversations.list":
			writeJSON(w, map[string]any{"ok": true, "channels": f.channels})
		case "conversations.info":
			for _, c := range f.channels {
				if c["id"] == q.Get("channel") {
					writeJSON(w, map[string]any{"ok": true, "channel": c})
					return
				}
			}
			writeJSON(w, map[string]any{"ok": false, "error": "channel_not_found"})
		case "conversations.history":
			writeJSON(w, map[string]any{
				"ok": true, "has_more": false,
				"messages": f.history[q.Get("channel")],
			})
		case "conversations.replies":
			thread := q.Get("channel") + " " + q.Get("ts")
			f.threads = append(f.threads, thread)
			msgs := f.replies[thread]
			if msgs == nil {
				msgs = []map[string]any{}
			}
			writeJSON(w, map[string]any{"ok": true, "has_more": false, "messages": msgs})
		case "conversations.members":
			writeJSON(w, map[string]any{"ok": true, "members": []any{slackOwner}})
		case "users.info":
			writeJSON(w, map[string]any{"ok": true, "user": slackUser(q.Get("user"))})
		case "files.info":
			writeJSON(w, map[string]any{"ok": true, "file": map[string]any{
				"id": q.Get("file"), "name": "file " + q.Get("file"), "comments_count": 0,
			}})
		case "bots.info":
			writeJSON(w, map[string]any{"ok": true, "bot": map[string]any{
				"id": q.Get("bot"), "name": "bot " + q.Get("bot"),
			}})
		default:
			writeJSON(w, map[string]any{"ok": false, "error": "unknown_method"})
		}
	})
	f.serve(t, mux)
	return f
}

// callsTo is how many times one method was called, or one method for one
// argument ("conversations.history C2").
func (f *fakeSlack) callsTo(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[key]
}

// callOrder is every call so far, "<method> <argument>", in order.
func (f *fakeSlack) callOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.order)
}

// threadOrder is every conversations.replies call so far, "<channel> <thread
// ts>", in order.
func (f *fakeSlack) threadOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.threads)
}

// slackUser is a whole users.list member.
func slackUser(id string) map[string]any {
	return map[string]any{
		"id": id, "name": "user-" + strings.ToLower(id), "team_id": slackTeam,
		"deleted": false, "updated": 1700000000, "profile": map[string]any{"real_name": "User " + id},
	}
}

// slackChannel is one conversations.list entry for a public channel the owner
// is in.
func slackChannel(id string) map[string]any {
	return map[string]any{
		"id": id, "name": strings.ToLower(id), "is_channel": true,
		"is_member": true, "is_archived": false, "created": 1700000000, "updated": 1700000000000,
	}
}

// slackMessage is one history message by the given user.
func slackMessage(ts, user string) map[string]any {
	return map[string]any{"type": "message", "user": user, "ts": ts, "text": "message " + ts}
}

// slackSetup installs the closure, with any body substitutions, and writes
// the connector config and the account.
func slackSetup(t *testing.T, ds substrate.Dataset, f *fakeSlack, rewrites ...[2]string) {
	t.Helper()
	installRewired(t, ds, slackDir, rewrites...)
	mustPut(t, ds, substrate.PutInput{Kind: slackConfigType, ID: "default", Properties: map[string]any{
		"userToken": fakeToken, "apiBase": f.ts.URL,
	}})
	mustPut(t, ds, substrate.PutInput{Kind: slackAccountType, ID: slackAccountID, Properties: map[string]any{
		"enabledMessages": true, "backfillDepth": "all",
	}})
}

// slackRun drives ONE run the way the on-demand trigger delivers it and
// applies every page. override replaces account properties in the injected
// config only, which is how a case hands the body a stored drain state the
// row does not hold. It returns the account stamps the run wrote, in order.
func slackRun(t *testing.T, ds substrate.Dataset, f *fakeSlack, override map[string]any) []map[string]any {
	t.Helper()
	row := mustGet(t, ds, slackAccountType, slackAccountID)
	props := maps.Clone(row.Properties)
	maps.Copy(props, override)
	conn := mustGet(t, ds, slackConfigType, "default")
	cfg := map[string]any{
		"accounts": []any{map[string]any{
			"id": slackAccountID, "kind": slackAccountType,
			"version": row.Version, "properties": props,
		}},
		"inputs": map[string]any{"connector": map[string]any{
			"id": conn.ID, "kind": slackConfigType, "version": conn.Version,
			"properties": map[string]any{"userToken": fakeToken, "apiBase": f.ts.URL},
		}},
	}
	s := newStepper(t, ds, slackSyncFn, cfg)
	s.setEnvelope(map[string]any{"record": map[string]any{"kind": slackAccountType, "id": slackAccountID}})
	var resume any
	var stamps []map[string]any
	for i := 0; ; i++ {
		if i > 1000 {
			t.Fatal("the slack run did not end in 1000 invocations")
		}
		effects, _, cur := s.step(resume)
		for _, ef := range effects {
			if ef.Action == "patch" && ef.Kind == slackAccountType && ef.ID == slackAccountID {
				stamps = append(stamps, ef.Properties)
			}
		}
		s.apply()
		if cur == nil {
			return stamps
		}
		resume = cur
	}
}

// slackCursors is the account's stored drain state.
func slackCursors(t *testing.T, ds substrate.Dataset) map[string]any {
	t.Helper()
	cur, _ := mustGet(t, ds, slackAccountType, slackAccountID).Properties["streamCursors"].(map[string]any)
	return cur
}

// slackStatus is the account's stored syncStatus.
func slackStatus(t *testing.T, ds substrate.Dataset) string {
	t.Helper()
	s, _ := mustGet(t, ds, slackAccountType, slackAccountID).Properties["syncStatus"].(string)
	return s
}

// syncBuffer is a log sink a test reads while the engine writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// lines counts the logged lines that contain every one of the words.
func (b *syncBuffer) lines(words ...string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	var n int
	for line := range strings.SplitSeq(b.buf.String(), "\n") {
		hit := line != ""
		for _, w := range words {
			hit = hit && strings.Contains(line, w)
		}
		if hit {
			n++
		}
	}
	return n
}

// slackDataset is a core repository whose engine logs into the returned sink.
func slackDataset(t *testing.T) (substrate.Dataset, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{}
	_, ds := newCoreDataset(t, engine.WithLogger(slog.New(slog.NewTextHandler(logs, nil))))
	return ds, logs
}

// TestSlackRefusedConversationIsAskedOnce: a conversation Slack answers
// `channel_not_found` for is asked once, logged once, and skipped by every
// later walk until its conversations.list entry changes. The unfixed body
// asked conversations.info and conversations.history again at the top of
// every walk and logged the same failure on every run.
func TestSlackRefusedConversationIsAskedOnce(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, logs := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1"), slackChannel("C2")}
	f.history["C1"] = []map[string]any{slackMessage("1700000000.000100", slackOwner)}
	f.refuse["conversations.info C2"] = "channel_not_found"
	f.refuse["conversations.history C2"] = "channel_not_found"
	slackSetup(t, ds, f)

	for range 3 {
		slackRun(t, ds, f, nil)
	}
	if n := f.callsTo("conversations.info C2"); n != 1 {
		t.Fatalf("conversations.info C2 was called %d times over three walks, want once", n)
	}
	if n := f.callsTo("conversations.history C2"); n != 0 {
		t.Fatalf("conversations.history C2 was called %d times after info refused it, want none", n)
	}
	if n := f.callsTo("conversations.history C1"); n != 3 {
		t.Fatalf("conversations.history C1 was called %d times, want once per walk (3)", n)
	}
	if n := logs.lines("function log", "channel_not_found"); n != 1 {
		t.Fatalf("the refusal was logged %d times over three runs, want once", n)
	}
	refused, _ := slackCursors(t, ds)["refused"].(map[string]any)
	if _, ok := refused["conversation C2"]; !ok {
		t.Fatalf("streamCursors.refused = %v, want the conversation C2 recorded", refused)
	}
	if st := slackStatus(t, ds); !strings.Contains(st, "refused") {
		t.Fatalf("syncStatus %q does not report the refusal", st)
	}

	// The owner is added back: the list entry changes, Slack answers, and
	// the next walk asks again and drops the refusal.
	delete(f.refuse, "conversations.info C2")
	delete(f.refuse, "conversations.history C2")
	f.channels[1]["updated"] = 1700000100000
	slackRun(t, ds, f, nil)
	if n := f.callsTo("conversations.history C2"); n != 1 {
		t.Fatalf("conversations.history C2 was called %d times after its list entry changed, want once", n)
	}
	refused, _ = slackCursors(t, ds)["refused"].(map[string]any)
	if len(refused) != 0 {
		t.Fatalf("streamCursors.refused = %v after the conversation answered, want empty", refused)
	}
}

// TestSlackRefusedItemsAreAskedOnce: a bot and a file Slack answers
// `bot_not_found` and `file_not_found` for are asked once across walks. An
// incremental walk re-reads a day of history, so the unfixed body met the
// same message, queued the same bot and file, and asked again every walk.
func TestSlackRefusedItemsAreAskedOnce(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	msg := slackMessage("1700000000.000100", slackOwner)
	msg["bot_id"] = "B0GONE"
	msg["files"] = []any{map[string]any{"id": "F0GONE", "name": "gone", "mode": "hosted", "file_access": "visible"}}
	f.history["C1"] = []map[string]any{msg}
	f.refuse["bots.info B0GONE"] = "bot_not_found"
	f.refuse["files.info F0GONE"] = "file_not_found"
	slackSetup(t, ds, f)

	for range 3 {
		slackRun(t, ds, f, nil)
	}
	if n := f.callsTo("bots.info B0GONE"); n != 1 {
		t.Fatalf("bots.info B0GONE was called %d times over three walks, want once", n)
	}
	if n := f.callsTo("files.info F0GONE"); n != 1 {
		t.Fatalf("files.info F0GONE was called %d times over three walks, want once", n)
	}

	// The owner pastes a token again: the config record moves, and what the
	// old token was refused is asked once more.
	was := mustGet(t, ds, slackConfigType, "default").Version
	mustPut(t, ds, substrate.PutInput{Kind: slackConfigType, ID: "default", Properties: map[string]any{
		"userToken": fakeToken, "apiBase": f.ts.URL + "/",
	}})
	if now := mustGet(t, ds, slackConfigType, "default").Version; now == was {
		t.Fatalf("the config edit did not move its version (%d)", now)
	}
	slackRun(t, ds, f, nil)
	if n := f.callsTo("bots.info B0GONE"); n != 2 {
		t.Fatalf("bots.info B0GONE was called %d times after the config was edited, want twice", n)
	}
}

// TestSlackFreshPassSkipsRefusedConversation is the production symptom: a
// walk past `history` spends the start of every run on new history, and a
// conversation that now answers `channel_not_found` was asked again, and
// logged again, on every one of those runs.
func TestSlackFreshPassSkipsRefusedConversation(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, logs := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1"), slackChannel("C2")}
	f.history["C1"] = []map[string]any{slackMessage("1700000000.000100", slackOwner)}
	f.history["C2"] = []map[string]any{slackMessage("1700000000.000200", slackOwner)}
	slackSetup(t, ds, f, [2]string{"DRAIN_CALLS = 300", "DRAIN_CALLS = 12"})
	for range 5 {
		if slackRun(t, ds, f, nil); slackCursors(t, ds)["phase"] == "done" {
			break
		}
	}
	if p := slackCursors(t, ds)["phase"]; p != "done" {
		t.Fatalf("the first walk did not finish in five runs: phase %v", p)
	}
	c1, c2 := f.callsTo("conversations.history C1"), f.callsTo("conversations.history C2")

	// A backlog of files keeps the walk past `history` for several runs.
	files := make([]any, 0, 100)
	for i := range 100 {
		files = append(files, fmt.Sprintf("F%04d", i))
	}
	f.refuse["conversations.history C2"] = "channel_not_found"
	backlog := map[string]any{"streamCursors": map[string]any{
		"phase": "files", "cursor": "", "files": files, "convs": []any{"C1", "C2"},
		"fresh": []any{}, "cold": false,
	}}
	slackRun(t, ds, f, backlog)
	slackRun(t, ds, f, nil)
	slackRun(t, ds, f, nil)
	if p := slackCursors(t, ds)["phase"]; p != "files" {
		t.Fatalf("the backlog walk left phase %v, want files (the case needs a walk past history)", p)
	}
	if n := f.callsTo("conversations.history C1") - c1; n != 3 {
		t.Fatalf("conversations.history C1 was called %d times in three runs, want once per run", n)
	}
	if n := f.callsTo("conversations.history C2") - c2; n != 1 {
		t.Fatalf("conversations.history C2 was called %d times in three runs, want once: the call Slack refused", n)
	}
	if n := logs.lines("function log", "channel_not_found"); n != 1 {
		t.Fatalf("the refusal was logged %d times over three runs, want once", n)
	}
}

// TestSlackHydrateStepsOverListedUsers: a resumed walk carries no `known`
// list, so every author its history names is queued for users.info. The
// unfixed body called users.info for every one of them although users.list
// had already written each row whole; the drain now steps over those rows
// without a call, as the file and bot phases do.
func TestSlackHydrateStepsOverListedUsers(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	for i := range 20 {
		f.users = append(f.users, slackUser(fmt.Sprintf("U%04d", i)))
	}
	slackSetup(t, ds, f)
	slackRun(t, ds, f, nil)

	for i := range 20 {
		f.history["C1"] = append(f.history["C1"],
			slackMessage(fmt.Sprintf("1700000000.%06d", 100+i), fmt.Sprintf("U%04d", i)))
	}
	f.history["C1"] = append(f.history["C1"], slackMessage("1700000000.000900", "U0GUEST"))
	slackRun(t, ds, f, map[string]any{"streamCursors": map[string]any{
		"phase": "history", "cursor": "", "queue": []any{"C1"}, "convs": []any{"C1"},
		"fresh": []any{}, "cold": false,
	}})
	if n := f.callsTo("users.info"); n != 1 {
		t.Fatalf("users.info was called %d times, want once: only U0GUEST is not a users.list row", n)
	}
	if n := f.callsTo("users.info U0GUEST"); n != 1 {
		t.Fatalf("users.info U0GUEST was called %d times, want once", n)
	}
}

// TestSlackUnreadableFilesAreNotFetched: a file embed that says the owner
// cannot read it (deleted, hidden by the plan's limit, access denied) is
// written as it came and never sent to files.info, which would only refuse.
func TestSlackUnreadableFilesAreNotFetched(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	msg := slackMessage("1700000000.000100", slackOwner)
	msg["files"] = []any{
		map[string]any{"id": "F0SEEN", "name": "seen", "mode": "hosted", "file_access": "visible"},
		map[string]any{"id": "F0STUB", "file_access": "check_file_info"},
		map[string]any{"id": "F0DELETED", "mode": "tombstone"},
		map[string]any{"id": "F0LIMIT", "mode": "hidden_by_limit"},
		map[string]any{"id": "F0DENIED", "file_access": "access_denied"},
	}
	f.history["C1"] = []map[string]any{msg}
	slackSetup(t, ds, f)
	slackRun(t, ds, f, nil)
	for id, want := range map[string]int{
		"F0SEEN": 1, "F0STUB": 1, "F0DELETED": 0, "F0LIMIT": 0, "F0DENIED": 0,
	} {
		if n := f.callsTo("files.info " + id); n != want {
			t.Fatalf("files.info %s was called %d times, want %d", id, n, want)
		}
	}
}

// TestSlackThreadsDrainBeforeFiles: a walk in its `files` phase with thread
// replies queued behind it (the history-first pass queues them) walks the
// threads first. The unfixed body spent the run on files.info and left the
// replies for the next walk, after every file, bot and roster.
func TestSlackThreadsDrainBeforeFiles(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	f.history["C1"] = []map[string]any{slackMessage("1700000000.000100", slackOwner)}
	slackSetup(t, ds, f, [2]string{"DRAIN_CALLS = 300", "DRAIN_CALLS = 12"})
	slackRun(t, ds, f, nil)

	files := make([]any, 0, 50)
	for i := range 50 {
		files = append(files, fmt.Sprintf("F%04d", i))
	}
	before := len(f.callOrder())
	slackRun(t, ds, f, map[string]any{"streamCursors": map[string]any{
		"phase": "files", "cursor": "", "files": files, "convs": []any{},
		"threads": []any{[]any{"C1", "1700000000.000100"}}, "fresh": []any{}, "cold": false,
	}})
	calls := f.callOrder()[before:]
	replies := slices.Index(calls, "conversations.replies C1")
	firstFile := slices.IndexFunc(calls, func(c string) bool { return strings.HasPrefix(c, "files.info ") })
	if replies < 0 || (firstFile >= 0 && replies > firstFile) {
		t.Fatalf("the run's calls were %v: want the queued thread walked before any files.info", calls)
	}
	if left, _ := slackCursors(t, ds)["threads"].([]any); len(left) != 0 {
		t.Fatalf("threads left after the run: %v", left)
	}
}

// TestSlackDrainSharesOneConnection: the runner keeps the body's process
// across the invocations of a drain, and every call of the drain rides the
// one kept-alive connection instead of opening its own.
func TestSlackDrainSharesOneConnection(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	for i := range 6 {
		id := fmt.Sprintf("C%d", i+1)
		f.channels = append(f.channels, slackChannel(id))
		f.history[id] = []map[string]any{slackMessage("1700000000.000100", slackOwner)}
	}
	slackSetup(t, ds, f)
	slackRun(t, ds, f, nil)

	calls := len(f.callOrder())
	if calls < 10 {
		t.Fatalf("the drain made %d calls, too few to say anything about reuse: %v", calls, f.callOrder())
	}
	if n := f.connections(); n != 1 {
		t.Fatalf("the drain's %d calls opened %d connections, want them all on one", calls, n)
	}
}

// slackParent is a thread parent with one reply, posted at latestReply.
func slackParent(ts, latestReply string) map[string]any {
	m := slackMessage(ts, slackOwner)
	m["thread_ts"] = ts
	m["reply_count"] = 1
	m["reply_users_count"] = 1
	m["reply_users"] = []any{slackOwner}
	m["latest_reply"] = latestReply
	return m
}

// slackReply is one reply in the thread under parent.
func slackReply(ts, parent string) map[string]any {
	m := slackMessage(ts, slackOwner)
	m["thread_ts"] = parent
	m["parent_user_id"] = slackOwner
	return m
}

// slackMessageRow is the live message row with this ts.
func slackMessageRow(t *testing.T, ds substrate.Dataset, ts string) *substrate.Record {
	t.Helper()
	for _, r := range listLive(t, ds, slackPackage+"/message") {
		if r.Properties["ts"] == ts {
			return r
		}
	}
	t.Fatalf("no message row with ts %s", ts)
	return nil
}

// TestSlackNewRepliesDrainBeforeWatchedThreads: a resumed walk holding a
// backlog of watch re-checks in `threads`, stored before `newReplies`
// existed, meets a thread with a new reply on its history-first pass. That
// thread is walked before any watch re-check, and the old backlog still
// drains. The unfixed body appended it behind the backlog, which is how a
// thread with a known new reply sat at position 1,105 of 1,506.
func TestSlackNewRepliesDrainBeforeWatchedThreads(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1"), slackChannel("C2")}
	f.history["C1"] = []map[string]any{slackMessage("1700000000.000100", slackOwner)}
	f.history["C2"] = []map[string]any{slackMessage("1700000500.000100", slackOwner)}
	slackSetup(t, ds, f)
	slackRun(t, ds, f, nil)
	if p := slackCursors(t, ds)["phase"]; p != "done" {
		t.Fatalf("the first walk did not finish: phase %v", p)
	}

	// C2's message takes its first reply.
	const parent, reply = "1700000500.000100", "1700000600.000200"
	f.history["C2"] = []map[string]any{slackParent(parent, reply)}
	f.replies["C2 "+parent] = []map[string]any{slackParent(parent, reply), slackReply(reply, parent)}
	watch := make([]any, 0, 40)
	for i := range 40 {
		watch = append(watch, []any{"C1", fmt.Sprintf("1690000000.%06d", i)})
	}
	before := len(f.threadOrder())
	slackRun(t, ds, f, map[string]any{"streamCursors": map[string]any{
		"phase": "replies", "cursor": "", "threads": watch, "convs": []any{"C1", "C2"},
		"fresh": []any{}, "cold": false,
	}})
	calls := f.threadOrder()[before:]
	if len(calls) == 0 || calls[0] != "C2 "+parent {
		t.Fatalf("conversations.replies calls were %v: want the thread with a new reply (C2 %s) first", calls, parent)
	}
	if len(calls) != 41 {
		t.Fatalf("conversations.replies was called %d times, want 41: the new thread and all 40 stored re-checks", len(calls))
	}
	cur := slackCursors(t, ds)
	for _, k := range []string{"newReplies", "threads"} {
		if left, _ := cur[k].([]any); len(left) != 0 {
			t.Fatalf("streamCursors.%s = %v after the run, want empty", k, left)
		}
	}
	slackMessageRow(t, ds, reply)
}

// TestSlackThreadIsQueuedOnce: a stored queue that holds one thread twice,
// and a thread the radar finds again while it waits as a watch re-check,
// each cost one conversations.replies call. The thread with a new reply
// moves ahead of the re-check that was queued before it.
func TestSlackThreadIsQueuedOnce(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	// `old` replied long before `hot`, so only `hot` is newer than the
	// replies cursor less its margin: the radar finds `hot`, the watch `old`.
	const old, oldReply = "1700000000.000100", "1700000001.000100"
	const hot, hotReply = "1700050000.000100", "1700050001.000100"
	f.history["C1"] = []map[string]any{slackParent(hot, hotReply), slackParent(old, oldReply)}
	f.replies["C1 "+old] = []map[string]any{slackParent(old, oldReply), slackReply(oldReply, old)}
	f.replies["C1 "+hot] = []map[string]any{slackParent(hot, hotReply), slackReply(hotReply, hot)}
	slackSetup(t, ds, f)
	slackRun(t, ds, f, nil)
	if p := slackCursors(t, ds)["phase"]; p != "done" {
		t.Fatalf("the first walk did not finish: phase %v", p)
	}

	before := len(f.threadOrder())
	slackRun(t, ds, f, map[string]any{"streamCursors": map[string]any{
		"phase": "history", "cursor": "", "queue": []any{"C1"}, "convs": []any{"C1"},
		"fresh": []any{}, "cold": false,
		"threads": []any{[]any{"C1", old}, []any{"C1", old}, []any{"C1", hot}},
	}})
	calls := f.threadOrder()[before:]
	if want := []string{"C1 " + hot, "C1 " + old}; !slices.Equal(calls, want) {
		t.Fatalf("conversations.replies calls were %v, want %v: each thread once, the new reply first", calls, want)
	}
}

// TestSlackParentCarriesLatestReplyAt: a thread parent records when its
// newest reply was posted, from `latest_reply`, at the exact microsecond.
func TestSlackParentCarriesLatestReplyAt(t *testing.T) {
	requireUV(t)
	t.Parallel()
	ds, _ := slackDataset(t)
	f := newFakeSlack(t)
	f.channels = []map[string]any{slackChannel("C1")}
	const parent, reply = "1700000000.000100", "1700000600.123456"
	f.history["C1"] = []map[string]any{slackParent(parent, reply), slackMessage("1690000000.000100", slackOwner)}
	// The thread answers an empty page, so the reply row is never written
	// and the instant comes from the parent as history lists it.
	slackSetup(t, ds, f)
	slackRun(t, ds, f, nil)

	row := slackMessageRow(t, ds, parent)
	raw, _ := row.Properties["latestReplyAt"].(string)
	got, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("latestReplyAt = %v on the parent: %v", row.Properties["latestReplyAt"], err)
	}
	if want := time.Unix(1700000600, 123456000); !got.Equal(want) {
		t.Fatalf("latestReplyAt = %s, want %s", got.UTC().Format(time.RFC3339Nano), want.UTC().Format(time.RFC3339Nano))
	}
	if _, ok := slackMessageRow(t, ds, "1690000000.000100").Properties["latestReplyAt"]; ok {
		t.Fatal("a message with no replies carries latestReplyAt")
	}
}
