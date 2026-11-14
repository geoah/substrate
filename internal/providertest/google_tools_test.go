package providertest

// The google bundle's tools (package version 51): twelve callables an agent
// takes as tools, sharing the `google_api.py` module. The proofs, from the
// shipped closure at ../../kinds/providers.substrate.reamde.dev/google:
//
//  1. TestGoogleToolsAdmit: every tool registers as a full tool card,
//     offered to agents under its declared name with its effect, writing no
//     record, reaching only Google's origin and loopback; the two write
//     toggles are owner bools whose scopes the manifest requests.
//
//  2. TestGoogleToolsCallGoogle: each tool runs in call mode through the
//     runner against a FAKE Google on loopback (the client config's apiBase
//     points at it). The arguments each case sends are held to the tool's
//     declared `arguments:` and its answer to its `returns:`, so the card a
//     model reads and the body cannot drift; every request the body made is
//     asserted, and no tool returns an effect.
//
//  3. TestGoogleToolsRefuseBeforeSending: a toggle that is off, a grant that
//     predates it, a dead sign-in, two accounts and no choice, a public
//     apiBase, a header that would inject another, and a redirect: each is
//     refused, and none sends Google anything (the redirect's target never
//     sees the token).
//
// The account entries are what the engine injects (invocationconfig.go):
// the account's properties and a live token, built here by hand.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	googleToolMe   = "ada@example.com"
	googleToolZone = "Europe/Athens"
)

// The tools, by function name: the tool name each is offered under, its
// effect, and the one origin it reaches.
var googleTools = map[string]struct{ tool, effect, host string }{
	"searchmail":   {"gmail_search", vocabulary.FunctionEffectRead, "gmail.googleapis.com"},
	"readthread":   {"gmail_read_thread", vocabulary.FunctionEffectRead, "gmail.googleapis.com"},
	"draftmail":    {"gmail_draft", vocabulary.FunctionEffectExternal, "gmail.googleapis.com"},
	"sendmail":     {"gmail_send", vocabulary.FunctionEffectIrreversible, "gmail.googleapis.com"},
	"labelthread":  {"gmail_label_thread", vocabulary.FunctionEffectExternal, "gmail.googleapis.com"},
	"createevent":  {"calendar_create_event", vocabulary.FunctionEffectIrreversible, "www.googleapis.com"},
	"updateevent":  {"calendar_update_event", vocabulary.FunctionEffectIrreversible, "www.googleapis.com"},
	"deleteevent":  {"calendar_delete_event", vocabulary.FunctionEffectIrreversible, "www.googleapis.com"},
	"respondevent": {"calendar_respond", vocabulary.FunctionEffectIrreversible, "www.googleapis.com"},
	"freebusy":     {"calendar_free_busy", vocabulary.FunctionEffectRead, "www.googleapis.com"},
	"searchdrive":  {"drive_search", vocabulary.FunctionEffectRead, "www.googleapis.com"},
	"readfile":     {"drive_read_file", vocabulary.FunctionEffectRead, "www.googleapis.com"},
}

// The tools whose call waits for the owner's approval: the send, and the
// calendar changes that email guests.
var googleToolsApproved = map[string]bool{
	"sendmail": true, "createevent": true, "updateevent": true, "deleteevent": true, "respondevent": true,
}

// The scope each toggle's consent carries, as the manifest's featureScopes
// block requests it.
var googleToggleScopes = map[string]string{
	"enabledGmail":         "https://www.googleapis.com/auth/gmail.readonly",
	"enabledGmailWrite":    "https://www.googleapis.com/auth/gmail.modify",
	"enabledCalendar":      "https://www.googleapis.com/auth/calendar.readonly",
	"enabledCalendarWrite": "https://www.googleapis.com/auth/calendar.events",
	"enabledDrive":         "https://www.googleapis.com/auth/drive.readonly",
}

func TestGoogleToolsAdmit(t *testing.T) {
	t.Parallel()
	reg := bundleRegistry(t, googleDir)
	b, ok := reg.BundleOf(googlePackage)
	if !ok {
		t.Fatal("the google bundle did not register")
	}
	if _, ok := b.Modules["google_api.py"]; !ok {
		t.Fatalf("bundle ships modules %v, want google_api.py, the one place the tools send a token from", b.Modules)
	}
	for name, want := range googleTools {
		fn, err := reg.ResolveFunction(googlePackage + "/" + name)
		if err != nil {
			t.Fatalf("%s did not register: %v", name, err)
		}
		if fn.Description == "" || fn.Input == nil || fn.Output == nil {
			t.Fatalf("%s is not a full tool card", name)
		}
		if fn.Offer == nil || fn.Offer.ToolName != want.tool || fn.Effect != want.effect {
			t.Fatalf("%s offer = %+v effect = %q, want %s and %s", name, fn.Offer, fn.Effect, want.tool, want.effect)
		}
		if len(fn.Caps.Emit) != 0 {
			t.Fatalf("%s writes %v; a tool writes no record (decision record 0125)", name, fn.Caps.Emit)
		}
		// What reaches other people waits for the owner (decision record
		// 0254); a draft, a label and every read do not.
		if want := googleToolsApproved[name]; fn.RequiresApproval() != want {
			t.Fatalf("%s requires approval = %v, want %v", name, fn.RequiresApproval(), want)
		}
		if got := strings.Join(fn.Caps.Network, ","); got != want.host+",127.0.0.1,localhost" {
			t.Fatalf("%s network = %s, want %s and loopback alone", name, got, want.host)
		}
	}
	if got := b.OAuth2.FeatureAccess; len(got) != 2 {
		t.Fatalf("FeatureAccess = %v, want the two write toggles alone", got)
	}
	for toggle, scope := range map[string]string{
		"enabledGmailWrite":    googleToggleScopes["enabledGmailWrite"],
		"enabledCalendarWrite": googleToggleScopes["enabledCalendarWrite"],
	} {
		if !slices.Contains(b.OAuth2.FeatureScopes[toggle], scope) {
			t.Fatalf("featureScopes[%s] = %v, want %s", toggle, b.OAuth2.FeatureScopes[toggle], scope)
		}
		if b.OAuth2.FeatureAccess[toggle] != vocabulary.FeatureAccessWrite {
			t.Fatalf("featureScopes[%s] access = %q, want write", toggle, b.OAuth2.FeatureAccess[toggle])
		}
		p, ok := mustKind(t, reg, googleAccountKind).Prop(toggle)
		if !ok || p.Datatype != vocabulary.DatatypeBool || !slices.Equal(p.Writers, []string{substrate.ActorTypePerson}) || p.DisplayName == "" {
			t.Fatalf("account.%s = %+v, want a labeled bool a person writes", toggle, p)
		}
	}
}

// --- the fake ------------------------------------------------------------------

type googleCall struct {
	Method string
	Path   string
	Query  url.Values
	Body   map[string]any
}

// googleFake is Gmail, Calendar and Drive on one loopback origin, as the
// client config's apiBase makes them: each API keeps its own path prefix.
type googleFake struct {
	fakeAPI
	calls []googleCall
}

func newGoogleFake(t *testing.T) *googleFake {
	t.Helper()
	f := &googleFake{}
	f.serve(t, http.HandlerFunc(f.handle))
	return f
}

func (f *googleFake) seen() []googleCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *googleFake) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

func gmailB64(s string) string { return base64.URLEncoding.EncodeToString([]byte(s)) }

func header(name, value string) map[string]any { return map[string]any{"name": name, "value": value} }

// The event the update, delete and respond cases change: half an hour,
// the owner invited beside Bob.
func googleExistingEvent() map[string]any {
	return map[string]any{
		"id": "e2", "summary": "Design review", "status": "confirmed",
		"htmlLink": "https://calendar.google.com/event?eid=e2",
		"start":    map[string]any{"dateTime": "2026-11-12T10:00:00+02:00", "timeZone": googleToolZone},
		"end":      map[string]any{"dateTime": "2026-11-12T10:30:00+02:00", "timeZone": googleToolZone},
		"attendees": []any{
			map[string]any{"email": googleToolMe, "self": true, "responseStatus": "needsAction"},
			map[string]any{"email": "bob@example.com", "responseStatus": "accepted"},
		},
	}
}

func (f *googleFake) handle(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	if !bearer(w, r) {
		return
	}
	call := googleCall{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query()}
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		_ = json.Unmarshal(raw, &call.Body)
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()

	body := call.Body
	switch route := r.Method + " " + r.URL.Path; route {
	case "GET /gmail/v1/users/me/threads":
		writeJSON(w, map[string]any{"threads": []any{
			map[string]any{"id": "t1", "snippet": "about Friday"},
			map[string]any{"id": "t2"},
		}, "nextPageToken": "next"})
	case "GET /gmail/v1/users/me/threads/t1", "GET /gmail/v1/users/me/threads/t2":
		id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/threads/")
		if r.URL.Query().Get("format") == "metadata" {
			writeJSON(w, map[string]any{"id": id, "messages": []any{
				map[string]any{
					"id": id + "-a", "labelIds": []any{"INBOX"}, "internalDate": "1794477600000",
					"payload": map[string]any{"headers": []any{header("Subject", "Plans for "+id), header("From", "Sam <sam@example.com>")}},
				},
				map[string]any{
					"id": id + "-b", "labelIds": []any{"INBOX", "UNREAD"}, "internalDate": "1794481200000", "snippet": "see you",
					"payload": map[string]any{"headers": []any{header("From", "Bea <bea@example.com>")}},
				},
			}})
			return
		}
		writeJSON(w, map[string]any{"id": id, "messages": []any{
			map[string]any{"id": "m1", "labelIds": []any{"INBOX"}, "internalDate": "1794477600000", "payload": map[string]any{
				"mimeType": "multipart/mixed",
				"headers":  []any{header("Subject", "Plans"), header("From", "Sam <sam@example.com>"), header("To", googleToolMe), header("Cc", "bea@example.com")},
				"parts": []any{
					map[string]any{"mimeType": "text/plain", "body": map[string]any{"data": gmailB64("Friday at 8?")}},
					map[string]any{"mimeType": "application/pdf", "filename": "menu.pdf", "body": map[string]any{"size": 2048, "attachmentId": "x"}},
				},
			}},
			map[string]any{"id": "m2", "labelIds": []any{"SENT"}, "internalDate": "1794481200000", "payload": map[string]any{
				"mimeType": "text/html",
				"headers":  []any{header("Subject", "Re: Plans"), header("From", googleToolMe), header("To", "sam@example.com")},
				"body":     map[string]any{"data": gmailB64("<p>Friday works.</p><style>p{}</style><p>See you &amp; Bea</p>")},
			}},
		}})
	case "GET /gmail/v1/users/me/messages/m1":
		writeJSON(w, map[string]any{"id": "m1", "threadId": "t1", "payload": map[string]any{"headers": []any{
			header("Message-ID", "<orig@mail.example.com>"),
			header("References", "<first@mail.example.com>"),
			header("Subject", "Plans"),
			header("From", "Sam <sam@example.com>"),
			header("To", googleToolMe+", Bea <bea@example.com>"),
			header("Cc", "cal@example.com"),
		}}})
	case "POST /gmail/v1/users/me/drafts", "PUT /gmail/v1/users/me/drafts/d1":
		msg, _ := body["message"].(map[string]any)
		thread, _ := msg["threadId"].(string)
		if thread == "" {
			thread = "t9"
		}
		writeJSON(w, map[string]any{"id": "d1", "message": map[string]any{"id": "m9", "threadId": thread}})
	case "POST /gmail/v1/users/me/messages/send":
		thread, _ := body["threadId"].(string)
		if thread == "" {
			thread = "t11"
		}
		writeJSON(w, map[string]any{"id": "m11", "threadId": thread, "labelIds": []any{"SENT"}})
	case "GET /gmail/v1/users/me/labels":
		writeJSON(w, map[string]any{"labels": []any{
			map[string]any{"id": "INBOX", "name": "INBOX", "type": "system"},
			map[string]any{"id": "Label_7", "name": "Receipts", "type": "user"},
		}})
	case "POST /gmail/v1/users/me/threads/t1/modify":
		writeJSON(w, map[string]any{"id": "t1", "messages": []any{
			map[string]any{"id": "m1", "labelIds": []any{"Label_7", "STARRED"}},
		}})
	case "GET /calendar/v3/calendars/primary":
		writeJSON(w, map[string]any{"id": googleToolMe, "timeZone": googleToolZone})
	case "POST /calendar/v3/calendars/primary/events":
		out := map[string]any{"id": "e1", "status": "confirmed", "htmlLink": "https://calendar.google.com/event?eid=e1"}
		for k, v := range body {
			out[k] = v
		}
		if _, ok := body["conferenceData"]; ok {
			out["hangoutLink"] = "https://meet.google.com/abc-defg-hij"
		}
		writeJSON(w, out)
	case "GET /calendar/v3/calendars/primary/events/e2":
		writeJSON(w, googleExistingEvent())
	case "PATCH /calendar/v3/calendars/primary/events/e2":
		out := googleExistingEvent()
		for k, v := range body {
			out[k] = v
		}
		writeJSON(w, out)
	case "DELETE /calendar/v3/calendars/primary/events/e2":
		w.WriteHeader(http.StatusNoContent)
	case "POST /calendar/v3/freeBusy":
		writeJSON(w, map[string]any{"calendars": map[string]any{
			"primary":         map[string]any{"busy": []any{map[string]any{"start": "2026-11-12T08:00:00Z", "end": "2026-11-12T09:00:00Z"}}},
			"sam@example.com": map[string]any{"busy": []any{}, "errors": []any{map[string]any{"domain": "global", "reason": "notFound"}}},
		}})
	case "GET /drive/v3/files":
		writeJSON(w, map[string]any{"files": []any{map[string]any{
			"id": "f1", "name": "Budget", "mimeType": "application/vnd.google-apps.spreadsheet",
			"modifiedTime": "2026-11-01T09:00:00Z", "webViewLink": "https://docs.google.com/spreadsheets/d/f1",
			"owners": []any{map[string]any{"emailAddress": googleToolMe, "displayName": "Ada"}},
		}}})
	case "GET /drive/v3/files/f1":
		writeJSON(w, map[string]any{
			"id": "f1", "name": "Budget", "mimeType": "application/vnd.google-apps.spreadsheet",
			"modifiedTime": "2026-11-01T09:00:00Z", "webViewLink": "https://docs.google.com/spreadsheets/d/f1",
		})
	case "GET /drive/v3/files/f1/export":
		w.Header().Set("Content-Type", "text/csv")
		_, _ = io.WriteString(w, "item,cost\nrent,1200\n")
	case "GET /drive/v3/files/f2":
		if r.URL.Query().Get("alt") == "media" {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, strings.Repeat("notes ", 400))
			return
		}
		writeJSON(w, map[string]any{
			"id": "f2", "name": "notes.txt", "mimeType": "text/plain",
			"modifiedTime": "2026-11-02T09:00:00Z", "webViewLink": "https://drive.google.com/file/d/f2",
		})
	case "GET /drive/v3/files/f3":
		writeJSON(w, map[string]any{
			"id": "f3", "name": "scan.pdf", "mimeType": "application/pdf",
			"modifiedTime": "2026-11-03T09:00:00Z", "webViewLink": "https://drive.google.com/file/d/f3",
		})
	default:
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]any{"error": map[string]any{"code": 404, "message": "no fake route " + route}})
	}
}

// --- driving a tool ------------------------------------------------------------

// googleToolAccount is one injected account entry: its address, the toggles
// it has on, the scopes their consent granted, and a live token.
func googleToolAccount(id, email string, toggles ...string) map[string]any {
	props := map[string]any{"email": email}
	var scopes []any
	for _, toggle := range toggles {
		props[toggle] = true
		scopes = append(scopes, googleToggleScopes[toggle], "https://www.googleapis.com/auth/userinfo.email")
	}
	props["grantedScopes"] = scopes
	return map[string]any{"id": id, "kind": googleAccountKind, "properties": props, "token": fakeToken}
}

func googleToolConfig(base string, accounts ...map[string]any) map[string]any {
	list := make([]any, len(accounts))
	for i, a := range accounts {
		list[i] = a
	}
	return map[string]any{
		"accounts": list,
		"inputs":   map[string]any{"client": map[string]any{"properties": map[string]any{"apiBase": base}}},
	}
}

// googleTools is one installed google bundle and the registry its cards are
// read from.
type googleToolRig struct {
	t    *testing.T
	ds   substrate.Dataset
	reg  *vocabulary.Registry
	fake *googleFake
}

func newGoogleToolRig(t *testing.T) *googleToolRig {
	t.Helper()
	requirePython(t)
	t.Parallel()
	_, ds := newCoreDataset(t)
	install(t, ds, googleDir, nil)
	return &googleToolRig{t: t, ds: ds, reg: bundleRegistry(t, googleDir), fake: newGoogleFake(t)}
}

// run calls one tool with args under cfg, holding the args to the tool's
// card first and its answer to its returns after, and refusing any effect.
func (g *googleToolRig) run(name string, cfg, args map[string]any) (map[string]any, error) {
	g.t.Helper()
	fn, err := g.reg.ResolveFunction(googlePackage + "/" + name)
	if err != nil {
		g.t.Fatal(err)
	}
	if err := vocabulary.CheckValue(fn.Input, args); err != nil {
		g.t.Fatalf("%s: the case's arguments break the card: %v", name, err)
	}
	s, err := engine.NewStepper(g.ds, fn.Identity(), cfg)
	if err != nil {
		g.t.Fatal(err)
	}
	s.SetArgs(args)
	effects, out, more, err := s.Step(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	if len(effects) != 0 || more != nil {
		g.t.Fatalf("%s returned %d effects and a cursor %v; a tool writes nothing", name, len(effects), more)
	}
	if err := vocabulary.CheckValue(fn.Output, out); err != nil {
		g.t.Fatalf("%s: the answer breaks its returns: %v\n%v", name, err, out)
	}
	m, _ := out.(map[string]any)
	return m, nil
}

func (g *googleToolRig) ok(name string, cfg, args map[string]any) map[string]any {
	g.t.Helper()
	out, err := g.run(name, cfg, args)
	if err != nil {
		g.t.Fatalf("%s: %v", name, err)
	}
	return out
}

// refused runs a call that must fail naming want, and sends Google nothing.
func (g *googleToolRig) refused(name string, cfg, args map[string]any, want string) {
	g.t.Helper()
	g.fake.reset()
	_, err := g.run(name, cfg, args)
	if err == nil || !strings.Contains(err.Error(), want) {
		g.t.Fatalf("%s(%v): err = %v, want a refusal naming %q", name, args, err, want)
	}
	if calls := g.fake.seen(); len(calls) != 0 {
		g.t.Fatalf("%s: the refused call still sent %d requests: %+v", name, len(calls), calls)
	}
}

// only resets the log, runs the call and returns the requests it made.
func (g *googleToolRig) only(name string, cfg, args map[string]any) (map[string]any, []googleCall) {
	g.t.Helper()
	g.fake.reset()
	out := g.ok(name, cfg, args)
	return out, g.fake.seen()
}

func routes(calls []googleCall) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Method + " " + c.Path
	}
	return out
}

func wantRoutes(t *testing.T, name string, calls []googleCall, want ...string) {
	t.Helper()
	if got := routes(calls); !slices.Equal(got, want) {
		t.Fatalf("%s sent %v, want %v", name, got, want)
	}
}

// sentMail decodes the RFC 5322 message a draft or a send carried.
func sentMail(t *testing.T, raw any) *mail.Message {
	t.Helper()
	s, _ := raw.(string)
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("raw is not base64url: %v", err)
	}
	m, err := mail.ReadMessage(strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("raw is not a message: %v\n%s", err, b)
	}
	return m
}

func mailBody(t *testing.T, m *mail.Message) string {
	t.Helper()
	b, err := io.ReadAll(m.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func asStrings(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, x := range list {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

// --- the calls -----------------------------------------------------------------

func TestGoogleToolsCallGoogle(t *testing.T) {
	g := newGoogleToolRig(t)
	base := g.fake.ts.URL
	all := googleToolConfig(base, googleToolAccount("acct-1", googleToolMe,
		"enabledGmail", "enabledGmailWrite", "enabledCalendar", "enabledCalendarWrite", "enabledDrive"))

	t.Run("searchmail", func(t *testing.T) {
		out, calls := g.only("searchmail", all, map[string]any{"query": "from:sam newer_than:7d", "limit": 2})
		wantRoutes(t, "searchmail", calls, "GET /gmail/v1/users/me/threads",
			"GET /gmail/v1/users/me/threads/t1", "GET /gmail/v1/users/me/threads/t2")
		if q := calls[0].Query; q.Get("q") != "from:sam newer_than:7d" || q.Get("maxResults") != "2" {
			t.Fatalf("threads.list query = %v", q)
		}
		if q := calls[1].Query; q.Get("format") != "metadata" || !slices.Contains(q["metadataHeaders"], "Subject") {
			t.Fatalf("threads.get query = %v", q)
		}
		threads, _ := out["threads"].([]any)
		first, _ := threads[0].(map[string]any)
		if len(threads) != 2 || first["thread"] != "t1" || first["subject"] != "Plans for t1" ||
			first["from"] != "Bea <bea@example.com>" || first["unread"] != true || first["messages"] != float64(2) ||
			first["lastMessage"] != "t1-b" || first["date"] != "2026-11-12T11:00:00Z" ||
			first["link"] != "https://mail.google.com/mail/u/ada@example.com/#all/t1" || out["more"] != true {
			t.Fatalf("searchmail answered %v", out)
		}
	})

	t.Run("readthread", func(t *testing.T) {
		out, calls := g.only("readthread", all, map[string]any{"thread": "t1"})
		wantRoutes(t, "readthread", calls, "GET /gmail/v1/users/me/threads/t1")
		msgs, _ := out["messages"].([]any)
		first, _ := msgs[0].(map[string]any)
		second, _ := msgs[1].(map[string]any)
		if first["body"] != "Friday at 8?" || first["cc"] != "bea@example.com" {
			t.Fatalf("the plain message read as %v", first)
		}
		if files, _ := first["attachments"].([]any); len(files) != 1 {
			t.Fatalf("attachments = %v, want menu.pdf named", first["attachments"])
		}
		if second["body"] != "Friday works.\n\nSee you & Bea" {
			t.Fatalf("the html message read as %q", second["body"])
		}
		if out["truncated"] != false {
			t.Fatalf("truncated = %v", out["truncated"])
		}
	})

	t.Run("draftmail new", func(t *testing.T) {
		out, calls := g.only("draftmail", all, map[string]any{
			"to": []any{"Sam Example <sam@example.com>"}, "subject": "Dinner", "body": "Friday at 8?",
		})
		wantRoutes(t, "draftmail", calls, "POST /gmail/v1/users/me/drafts")
		msg, _ := calls[0].Body["message"].(map[string]any)
		if _, threaded := msg["threadId"]; threaded {
			t.Fatalf("a new draft named a thread: %v", msg)
		}
		m := sentMail(t, msg["raw"])
		if m.Header.Get("To") != "Sam Example <sam@example.com>" || m.Header.Get("Subject") != "Dinner" ||
			m.Header.Get("In-Reply-To") != "" || strings.TrimSpace(mailBody(t, m)) != "Friday at 8?" {
			t.Fatalf("the draft carried %v", m.Header)
		}
		if out["draft"] != "d1" || out["thread"] != "t9" || out["subject"] != "Dinner" {
			t.Fatalf("draftmail answered %v", out)
		}
	})

	t.Run("draftmail reply all", func(t *testing.T) {
		out, calls := g.only("draftmail", all, map[string]any{"replyTo": "m1", "replyAll": true, "body": "Works for me."})
		wantRoutes(t, "draftmail", calls, "GET /gmail/v1/users/me/messages/m1", "POST /gmail/v1/users/me/drafts")
		if q := calls[0].Query; q.Get("format") != "metadata" || !slices.Contains(q["metadataHeaders"], "Message-ID") {
			t.Fatalf("messages.get query = %v", q)
		}
		msg, _ := calls[1].Body["message"].(map[string]any)
		m := sentMail(t, msg["raw"])
		if msg["threadId"] != "t1" || m.Header.Get("In-Reply-To") != "<orig@mail.example.com>" ||
			m.Header.Get("References") != "<first@mail.example.com> <orig@mail.example.com>" ||
			m.Header.Get("Subject") != "Re: Plans" || m.Header.Get("To") != "Sam <sam@example.com>" ||
			m.Header.Get("Cc") != "Bea <bea@example.com>, cal@example.com" {
			t.Fatalf("the reply carried thread %v and %v", msg["threadId"], m.Header)
		}
		if out["thread"] != "t1" || !slices.Equal(asStrings(out["cc"]), []string{"Bea <bea@example.com>", "cal@example.com"}) {
			t.Fatalf("draftmail answered %v", out)
		}
	})

	t.Run("draftmail replace", func(t *testing.T) {
		_, calls := g.only("draftmail", all, map[string]any{"draft": "d1", "to": []any{"sam@example.com"}, "body": "Saturday instead?"})
		wantRoutes(t, "draftmail", calls, "PUT /gmail/v1/users/me/drafts/d1")
		if calls[0].Body["id"] != "d1" {
			t.Fatalf("drafts.update body = %v", calls[0].Body)
		}
	})

	t.Run("sendmail", func(t *testing.T) {
		// A reply threads under the message it answers and carries exactly
		// the recipients and subject the call names: the owner approved the
		// call's own, so none comes from the message (m1 has a Cc).
		out, calls := g.only("sendmail", all, map[string]any{
			"replyTo": "m1", "to": []any{"sam@example.com"}, "subject": "Re: Plans", "body": "Works for me.",
		})
		wantRoutes(t, "sendmail", calls, "GET /gmail/v1/users/me/messages/m1", "POST /gmail/v1/users/me/messages/send")
		m := sentMail(t, calls[1].Body["raw"])
		if calls[1].Body["threadId"] != "t1" || m.Header.Get("In-Reply-To") != "<orig@mail.example.com>" ||
			m.Header.Get("To") != "sam@example.com" || m.Header.Get("Cc") != "" || m.Header.Get("Subject") != "Re: Plans" {
			t.Fatalf("the reply carried thread %v and %v", calls[1].Body["threadId"], m.Header)
		}
		if _, has := out["cc"]; has {
			t.Fatalf("a send answered recipients its call never named: %v", out)
		}
		out, calls = g.only("sendmail", all, map[string]any{
			"to": []any{"sam@example.com"}, "bcc": []any{"cal@example.com"}, "subject": "Menu", "body": "Attached below.",
		})
		wantRoutes(t, "sendmail", calls, "POST /gmail/v1/users/me/messages/send")
		m = sentMail(t, calls[0].Body["raw"])
		if m.Header.Get("Bcc") != "cal@example.com" || out["message"] != "m11" ||
			out["link"] != "https://mail.google.com/mail/u/ada@example.com/#all/t11" {
			t.Fatalf("the send carried %v and answered %v", m.Header, out)
		}
	})

	t.Run("labelthread", func(t *testing.T) {
		out, calls := g.only("labelthread", all, map[string]any{
			"thread": "t1", "add": []any{"Receipts", "starred"}, "remove": []any{"inbox"},
		})
		wantRoutes(t, "labelthread", calls, "GET /gmail/v1/users/me/labels", "POST /gmail/v1/users/me/threads/t1/modify")
		add, remove := asStrings(calls[1].Body["addLabelIds"]), asStrings(calls[1].Body["removeLabelIds"])
		slices.Sort(add)
		if !slices.Equal(add, []string{"Label_7", "STARRED"}) || !slices.Equal(remove, []string{"INBOX"}) {
			t.Fatalf("threads.modify got add %v remove %v", add, remove)
		}
		if !slices.Equal(asStrings(out["labels"]), []string{"Label_7", "STARRED"}) {
			t.Fatalf("labelthread answered %v", out)
		}
		// A system label alone needs no label listing.
		_, calls = g.only("labelthread", all, map[string]any{"thread": "t1", "remove": []any{"UNREAD"}})
		wantRoutes(t, "labelthread", calls, "POST /gmail/v1/users/me/threads/t1/modify")
	})

	t.Run("createevent", func(t *testing.T) {
		out, calls := g.only("createevent", all, map[string]any{
			"summary": "Dinner", "start": "2026-11-13T20:00", "attendees": []any{"Sam <sam@example.com>"},
			"meet": true, "location": "Home",
		})
		wantRoutes(t, "createevent", calls, "GET /calendar/v3/calendars/primary", "POST /calendar/v3/calendars/primary/events")
		q, ev := calls[1].Query, calls[1].Body
		start, _ := ev["start"].(map[string]any)
		end, _ := ev["end"].(map[string]any)
		if q.Get("sendUpdates") != "all" || q.Get("conferenceDataVersion") != "1" ||
			start["dateTime"] != "2026-11-13T20:00:00" || start["timeZone"] != googleToolZone ||
			end["dateTime"] != "2026-11-13T21:00:00" || ev["location"] != "Home" {
			t.Fatalf("events.insert got %v %v", q, ev)
		}
		if guests, _ := ev["attendees"].([]any); len(guests) != 1 || guests[0].(map[string]any)["email"] != "sam@example.com" {
			t.Fatalf("attendees = %v", ev["attendees"])
		}
		if out["event"] != "e1" || out["meet"] != "https://meet.google.com/abc-defg-hij" ||
			!slices.Equal(asStrings(out["attendees"]), []string{"sam@example.com"}) {
			t.Fatalf("createevent answered %v", out)
		}
		// An all-day event needs no zone and tells nobody.
		_, calls = g.only("createevent", all, map[string]any{"summary": "Off", "start": "2026-11-20"})
		wantRoutes(t, "createevent", calls, "POST /calendar/v3/calendars/primary/events")
		end, _ = calls[0].Body["end"].(map[string]any)
		if end["date"] != "2026-11-21" || calls[0].Query.Get("sendUpdates") != "none" {
			t.Fatalf("an all-day event went as %v %v", calls[0].Query, calls[0].Body)
		}
	})

	t.Run("updateevent", func(t *testing.T) {
		out, calls := g.only("updateevent", all, map[string]any{
			"event": "e2", "start": "2026-11-13T15:00",
			"addAttendees": []any{"cal@example.com"}, "removeAttendees": []any{"bob@example.com"},
		})
		wantRoutes(t, "updateevent", calls, "GET /calendar/v3/calendars/primary/events/e2",
			"PATCH /calendar/v3/calendars/primary/events/e2")
		patch := calls[1].Body
		start, _ := patch["start"].(map[string]any)
		end, _ := patch["end"].(map[string]any)
		if start["dateTime"] != "2026-11-13T15:00:00" || start["timeZone"] != googleToolZone ||
			end["dateTime"] != "2026-11-13T15:30:00" {
			t.Fatalf("a moved start kept no length: %v", patch)
		}
		var guests []string
		for _, x := range patch["attendees"].([]any) {
			guests = append(guests, x.(map[string]any)["email"].(string))
		}
		if !slices.Equal(guests, []string{googleToolMe, "cal@example.com"}) || calls[1].Query.Get("sendUpdates") != "all" {
			t.Fatalf("attendees %v, sendUpdates %v", guests, calls[1].Query)
		}
		if _, has := patch["summary"]; has || out["event"] != "e2" {
			t.Fatalf("the patch sent %v and answered %v", patch, out)
		}
	})

	t.Run("deleteevent", func(t *testing.T) {
		out, calls := g.only("deleteevent", all, map[string]any{"event": "e2"})
		wantRoutes(t, "deleteevent", calls, "GET /calendar/v3/calendars/primary/events/e2",
			"DELETE /calendar/v3/calendars/primary/events/e2")
		if calls[1].Query.Get("sendUpdates") != "all" || out["deleted"] != true || out["summary"] != "Design review" {
			t.Fatalf("delete sent %v and answered %v", calls[1].Query, out)
		}
	})

	t.Run("respondevent", func(t *testing.T) {
		out, calls := g.only("respondevent", all, map[string]any{"event": "e2", "response": "accepted", "comment": "See you there"})
		wantRoutes(t, "respondevent", calls, "GET /calendar/v3/calendars/primary/events/e2",
			"PATCH /calendar/v3/calendars/primary/events/e2")
		guests, _ := calls[1].Body["attendees"].([]any)
		me, _ := guests[0].(map[string]any)
		bob, _ := guests[1].(map[string]any)
		if len(guests) != 2 || me["responseStatus"] != "accepted" || me["comment"] != "See you there" ||
			bob["responseStatus"] != "accepted" || bob["email"] != "bob@example.com" || out["response"] != "accepted" {
			t.Fatalf("respond sent %v and answered %v", calls[1].Body, out)
		}
	})

	t.Run("freebusy", func(t *testing.T) {
		out, calls := g.only("freebusy", all, map[string]any{
			"start": "2026-11-12T09:00:00+02:00", "end": "2026-11-12T18:00:00+02:00",
			"calendars": []any{"primary", "sam@example.com"},
		})
		wantRoutes(t, "freebusy", calls, "POST /calendar/v3/freeBusy")
		if calls[0].Body["timeMin"] != "2026-11-12T09:00:00+02:00" || len(calls[0].Body["items"].([]any)) != 2 {
			t.Fatalf("freeBusy got %v", calls[0].Body)
		}
		cals, _ := out["calendars"].([]any)
		var errs int
		for _, c := range cals {
			if m := c.(map[string]any); m["error"] == "notFound" {
				errs++
			}
		}
		if len(cals) != 2 || errs != 1 {
			t.Fatalf("freebusy answered %v", out)
		}
	})

	t.Run("searchdrive", func(t *testing.T) {
		out, calls := g.only("searchdrive", all, map[string]any{"text": "Ada's budget", "q": "mimeType = 'application/vnd.google-apps.spreadsheet'"})
		wantRoutes(t, "searchdrive", calls, "GET /drive/v3/files")
		q := calls[0].Query
		if q.Get("q") != `trashed = false and fullText contains 'Ada\'s budget' and (mimeType = 'application/vnd.google-apps.spreadsheet')` ||
			q.Get("orderBy") != "" || q.Get("supportsAllDrives") != "true" {
			t.Fatalf("files.list query = %v", q)
		}
		files, _ := out["files"].([]any)
		if len(files) != 1 || files[0].(map[string]any)["file"] != "f1" || out["more"] != false {
			t.Fatalf("searchdrive answered %v", out)
		}
		_, calls = g.only("searchdrive", all, map[string]any{})
		if calls[0].Query.Get("orderBy") != "modifiedTime desc" || calls[0].Query.Get("q") != "trashed = false" {
			t.Fatalf("a bare listing sent %v", calls[0].Query)
		}
	})

	t.Run("readfile", func(t *testing.T) {
		out, calls := g.only("readfile", all, map[string]any{"file": "f1"})
		wantRoutes(t, "readfile", calls, "GET /drive/v3/files/f1", "GET /drive/v3/files/f1/export")
		if calls[1].Query.Get("mimeType") != "text/csv" || out["content"] != "item,cost\nrent,1200\n" || out["truncated"] != false {
			t.Fatalf("a sheet read as %v (%v)", out, calls[1].Query)
		}
		out, calls = g.only("readfile", all, map[string]any{"file": "f2", "maxChars": 1000})
		wantRoutes(t, "readfile", calls, "GET /drive/v3/files/f2", "GET /drive/v3/files/f2")
		if calls[1].Query.Get("alt") != "media" || len(out["content"].(string)) != 1000 || out["truncated"] != true {
			t.Fatalf("a text file read as %d chars, truncated %v", len(out["content"].(string)), out["truncated"])
		}
		out, calls = g.only("readfile", all, map[string]any{"file": "f3"})
		wantRoutes(t, "readfile", calls, "GET /drive/v3/files/f3")
		if _, has := out["content"]; has || !strings.Contains(out["note"].(string), "application/pdf") {
			t.Fatalf("a pdf answered %v", out)
		}
	})
}

// --- the refusals --------------------------------------------------------------

func TestGoogleToolsRefuseBeforeSending(t *testing.T) {
	g := newGoogleToolRig(t)
	base := g.fake.ts.URL
	msg := map[string]any{"to": []any{"sam@example.com"}, "subject": "Hi", "body": "Hello"}

	// The read toggles alone admit no write, whatever the grant says.
	reader := googleToolConfig(base, googleToolAccount("acct-1", googleToolMe, "enabledGmail", "enabledCalendar"))
	g.refused("sendmail", reader, msg, "Gmail sending")
	g.refused("draftmail", reader, msg, "Gmail sending")
	g.refused("createevent", reader, map[string]any{"summary": "x", "start": "2026-11-20"}, "Calendar editing")
	g.refused("searchdrive", reader, map[string]any{"text": "x"}, "Drive")

	// A write toggle turned on before the reconnect that grants its scope.
	stale := googleToolAccount("acct-1", googleToolMe, "enabledGmail")
	stale["properties"].(map[string]any)["enabledGmailWrite"] = true
	g.refused("sendmail", googleToolConfig(base, stale), msg, "reconnects the account")

	// A dead sign-in, and one not finished.
	dead := googleToolAccount("acct-1", googleToolMe, "enabledGmailWrite")
	dead["tokenError"] = "invalid_grant"
	g.refused("sendmail", googleToolConfig(base, dead), msg, "cannot sign in (invalid_grant)")
	pending := googleToolAccount("acct-1", googleToolMe, "enabledGmailWrite")
	delete(pending, "token")
	g.refused("sendmail", googleToolConfig(base, pending), msg, "not connected yet")
	g.refused("sendmail", googleToolConfig(base), msg, "no Google account is connected")

	// Two accounts that both can: one must be named, and naming one picks it.
	work := googleToolAccount("acct-2", "ada@work.example.com", "enabledGmailWrite")
	both := googleToolConfig(base, googleToolAccount("acct-1", googleToolMe, "enabledGmailWrite"), work)
	g.refused("sendmail", both, msg, "several connected accounts")
	g.refused("sendmail", both, map[string]any{"account": "nobody@example.com", "body": "x", "to": []any{"a@example.com"}}, "no connected account is")
	named := map[string]any{"account": "ADA@work.example.com", "to": []any{"sam@example.com"}, "subject": "Hi", "body": "Hello"}
	if out := g.ok("sendmail", both, named); out["account"] != "ada@work.example.com" {
		t.Fatalf("naming the work account sent as %v", out["account"])
	}

	// What the message itself may not carry.
	all := googleToolConfig(base, googleToolAccount("acct-1", googleToolMe, "enabledGmailWrite", "enabledCalendar", "enabledCalendarWrite"))
	g.refused("sendmail", all, map[string]any{"to": []any{"sam@example.com"}, "subject": "Hi\nBcc: eve@example.com", "body": "x"}, "line break")
	g.refused("sendmail", all, map[string]any{"to": []any{"sam@example.com\r\nBcc: eve@example.com"}, "body": "x"}, "line break")
	g.refused("sendmail", all, map[string]any{"to": []any{"not an address"}, "body": "x"}, "not one email address")
	g.refused("sendmail", all, map[string]any{"to": []any{"sam@example.com"}}, "body is required")
	g.refused("sendmail", all, map[string]any{"body": "x"}, "at least one recipient")
	g.refused("sendmail", all, map[string]any{"to": []any{"sam@example.com"}, "body": "x"}, "subject is required")
	// A send carries only what the owner approves: no draft by its id, whose
	// contents could change while the approval waits, and no recipients a
	// reply would take from the message it answers.
	send, err := g.reg.ResolveFunction(googlePackage + "/sendmail")
	if err != nil {
		t.Fatal(err)
	}
	for arg, v := range map[string]any{"draft": "d1", "replyAll": true} {
		if err := vocabulary.CheckValue(send.Input, map[string]any{arg: v, "to": []any{"sam@example.com"}, "subject": "Hi", "body": "x"}); err == nil {
			t.Errorf("gmail_send's card takes %s", arg)
		}
	}
	g.refused("freebusy", all, map[string]any{"start": "2026-11-12T09:00", "end": "2026-11-12T18:00:00Z"}, "with its offset")
	g.refused("createevent", all, map[string]any{"summary": "x", "start": "2026-11-20", "end": "2026-11-20T10:00:00Z"}, "both dates or both datetimes")

	// The token goes to Google or loopback, nowhere else.
	public := googleToolConfig("https://gmail.example.com", googleToolAccount("acct-1", googleToolMe, "enabledGmailWrite"))
	g.refused("sendmail", public, msg, "only a loopback host")

	// A redirect is refused, and its target never sees the token.
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		writeJSON(w, map[string]any{})
	}))
	t.Cleanup(target.Close)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirector.Close)
	moved := googleToolConfig(redirector.URL, googleToolAccount("acct-1", googleToolMe, "enabledGmailWrite"))
	if _, err := g.run("sendmail", moved, msg); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("a redirect answered %v, want it refused", err)
	}
	if n := reached.Load(); n != 0 {
		t.Fatalf("the redirect's target received %d requests", n)
	}
}
