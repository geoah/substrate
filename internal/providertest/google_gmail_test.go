package providertest

// The Google bundle's GMAIL stream. Two kinds of proof, both
// from the shipped closure at ../../kinds/providers.substrate.reamde.dev/google:
//
//  1. TestGoogleGmailBundleAdmitsSchema — pure schema admission (no DB, no
//     uv): the gmail feature toggle now maps to a real scope, the two mirror
//     types and the shared emailaddress source declare what the body writes,
//     the emailaddress→person mapping resolves, and gmailsync's emit ceiling
//     names BOTH the mirrors and the two core messaging types. The ceiling is
//     the only thing standing between the body and a refused core write, so
//     it is asserted rather than assumed.
//
//  2. The stepper tests — the paged cursor driven page by page against a
//     loopback Gmail, so the cursor itself is observable: the run-start
//     historyId is the stamped watermark, a 404 (and only a 404) on
//     history.list drops to a windowed full re-read, the sweep that follows
//     is scoped to that window (an archived message outside it survives), a
//     tampered API base refuses to send the token, and one erroring account
//     never stalls the account behind it.
//
// Real Google API calls never run in a test (no creds); live OAuth + sync is
// verified against a connected account.

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestGoogleGmailBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	reg := bundleRegistry(t, googleDir)

	// The gmail toggle maps to a REAL scope now: review-google #1's gate was
	// "an unwired feature requests nothing", so this asserts the opposite is
	// now deliberate rather than accidental.
	b, ok := reg.BundleOf(googlePackage)
	if !ok {
		t.Fatalf("no bundle owns %s", googlePackage)
	}
	scopes := b.OAuth2.FeatureScopes["enabledGmail"]
	if len(scopes) != 1 || scopes[0] != "https://www.googleapis.com/auth/gmail.readonly" {
		t.Fatalf("enabledGmail scopes = %v, want the single gmail.readonly", scopes)
	}

	// The shared people source: a single, non-cascading subject SLOT, unpinned
	// and optional, and no mapping of its own. The kind it reaches is the
	// repository's to choose (record 49), and the repository's own mapping is
	// what carries a match probe.
	addr := mustKind(t, reg, googleAddressType)
	mustProps(t, addr, "account", "address", "displayName")
	ed, ok := addr.Prop("person")
	if !ok {
		t.Fatalf("emailaddress declares no `person` slot")
	}
	if ed.To != "" || ed.Required || ed.Repeated || ed.Cascades() || !ed.Subject || !ed.MustExist {
		t.Fatalf("person slot shape wrong: kind=%q required=%v repeated=%v cascades=%v subject=%v mustExist=%v",
			ed.To, ed.Required, ed.Repeated, ed.Cascades(), ed.Subject, ed.MustExist)
	}
	if ms := reg.MappingsFrom(googleAddressType); len(ms) != 0 {
		t.Fatalf("the google closure ships %d mappings from emailaddress; a provider ships none", len(ms))
	}
	if ms := reg.MappingsTo(googlePersonType); len(ms) != 0 {
		t.Fatalf("the google closure maps onto person: %v", ms)
	}

	// The two mirrors carry what the body writes.
	thread := mustKind(t, reg, googleThreadType)
	mustProps(t, thread, "account", "threadId", "syncGeneration", "subject",
		"preview", "lastMessageAt", "participants")
	msg := mustKind(t, reg, googleMessageType)
	mustProps(t, msg, "account", "messageId", "threadId", "rfcMessageId",
		"historyId", "syncGeneration", "labelIds", "subject", "preview",
		"text", "from", "to", "cc", "bcc", "sentAt", "sizeEstimate",
		"attachments", "raw")
	// `title` is a reserved built-in column and `snippet` is a template token;
	// `body` is declarable (#68) but this mirror keeps its prose in `text`, not
	// a `body` column. The names it DID pick are pinned here as the deliberate
	// choice, so none of these three may appear.
	for _, banned := range []string{"body", "title", "snippet"} {
		if _, ok := msg.Prop(banned); ok {
			t.Fatalf("message declares %q — reserved column, template token, or the unused body column", banned)
		}
	}

	// The function: the emit ceiling names the mirrors, the shared address
	// source and the account stamp, and NOTHING ELSE. This closure writes no
	// kind it does not own (record 49), so a core messaging kind in the
	// ceiling would be a license it must not hold.
	fn, err := reg.ResolveFunction(googleGmailFn)
	if err != nil {
		t.Fatalf("gmail sync %s did not register: %v", googleGmailFn, err)
	}
	mustEmit(t, fn, googleThreadType, googleMessageType, googleAddressType,
		googleAccountType)
	mustRead(t, fn, googleThreadType, googleMessageType)
	for _, ident := range fn.Caps.Emit {
		if strings.HasPrefix(ident, enginetest.SampleAuthority+"/") {
			t.Fatalf("gmailsync may write %s, a kind this package does not own", ident)
		}
	}
	if strings.Contains(fn.Source, "# /// script") {
		t.Fatalf("gmailsync declares PEP 723 dependencies — it is meant to run on the dependency-free fast path")
	}

	// The stream's own state, all connector-owned.
	acct := mustKind(t, reg, googleAccountType)
	for _, name := range []string{
		"gmailHistoryId",
		"gmailBackfillAnchorAt",
		"gmailBackfillResume",
		"gmailLastSyncedAt",
	} {
		p, ok := acct.Prop(name)
		if !ok {
			t.Fatalf("account misses %s", name)
		}
		if p.Writer != vocabulary.WriterConnector {
			t.Fatalf("account.%s writer = %q, want connector", name, p.Writer)
		}
	}
}

// --- the loopback Gmail ------------------------------------------------------

// fakeGmail is a Gmail in a box: the profile the run-start watermark comes
// off, one history page, the backfill listing and the message payloads. Its
// dials drive the expiry and vanishing-message paths.
type fakeGmail struct {
	fakeAPI

	// historyID is what users.getProfile reports — the run-start watermark.
	historyID string
	// historyGone makes history.list answer 404 (the too-old startHistoryId),
	// which is the ONLY status that may drop to a full re-read.
	historyGone bool
	// historyBad makes history.list answer 400, which must NOT.
	historyBad bool
	// history is the id set one history page reports as added.
	history []string
	// deleted is the id set one history page reports as removed.
	deleted []string
	// listed is the id set a backfill messages.list page reports — the
	// single-page shorthand for `pages`.
	listed []string
	// pages is the MULTI-page backfill listing, one id set per page. Page n
	// hands back the token "p<n+1>", so the body's page cap, its persisted
	// `gmailBackfillResume` and the resume that follows are all reachable —
	// none of which a one-page listing can reach at all.
	pages [][]string
	// msgs maps a message id to its messages.get payload.
	msgs map[string]map[string]any
	// missing ids answer 404 on messages.get.
	missing map[string]bool
}

func newFakeGmail(t *testing.T) *fakeGmail {
	t.Helper()
	return &fakeGmail{
		historyID: "9000",
		msgs:      map[string]map[string]any{},
		missing:   map[string]bool{},
	}
}

// gets is how many messages.get calls the run made.
func (f *fakeGmail) gets() int { return f.count("get") }

// listPages is `pages` when it is set and the one-page `listed` otherwise.
func (f *fakeGmail) listPages() [][]string {
	if len(f.pages) > 0 {
		return f.pages
	}
	return [][]string{f.listed}
}

func (f *fakeGmail) start(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !bearer(w, r) {
			return
		}
		writeJSON(w, map[string]any{"emailAddress": "geoah@example.com", "historyId": f.historyID})
	})
	mux.HandleFunc("/gmail/v1/users/me/history", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		switch {
		case f.historyGone:
			w.WriteHeader(http.StatusNotFound)
			return
		case f.historyBad:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var records []any
		entry := func(id string) any {
			return map[string]any{"message": map[string]any{"id": id, "threadId": "t-" + id}}
		}
		for _, id := range f.history {
			records = append(records, map[string]any{
				"id": "1", "messagesAdded": []any{entry(id)},
			})
		}
		for _, id := range f.deleted {
			records = append(records, map[string]any{
				"id": "2", "messagesDeleted": []any{entry(id)},
			})
		}
		writeJSON(w, map[string]any{"history": records, "historyId": f.historyID})
	})
	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		pages := f.listPages()
		idx := 0
		// A Gmail list page token is short-lived: one this fake never issued
		// is a STALE token, and Gmail answers a stale token with 400.
		if tok := r.URL.Query().Get("pageToken"); tok != "" {
			n, err := strconv.Atoi(strings.TrimPrefix(tok, "p"))
			if err != nil || !strings.HasPrefix(tok, "p") || n < 1 || n >= len(pages) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			idx = n
		}
		var items []any
		for _, id := range pages[idx] {
			items = append(items, map[string]any{"id": id, "threadId": "t-" + id})
		}
		out := map[string]any{"messages": items}
		if idx+1 < len(pages) {
			out["nextPageToken"] = fmt.Sprintf("p%d", idx+1)
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		f.bump("get")
		id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
		if f.missing[id] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		msg, ok := f.msgs[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, msg)
	})
	f.serve(t, mux)
}

// gmailMessage builds one messages.get payload the body can parse.
func gmailMessage(id, thread, subject, from, to, internalDate, body string) map[string]any {
	return map[string]any{
		"id": id, "threadId": thread, "historyId": "9001",
		"internalDate": internalDate, "snippet": "snip " + id,
		"sizeEstimate": 1234, "labelIds": []any{"INBOX", "UNREAD"},
		"payload": map[string]any{
			"mimeType": "text/plain",
			"headers": []any{
				map[string]any{"name": "Subject", "value": subject},
				map[string]any{"name": "From", "value": from},
				map[string]any{"name": "To", "value": to},
				map[string]any{"name": "Message-ID", "value": "<" + id + "@example.com>"},
			},
			"body": map[string]any{"data": b64url(body), "size": len(body)},
		},
	}
}

// gmailHTMLMessage builds a messages.get payload carrying an html body and
// the text/plain alternative beside it, verbatim: the shape a marketing mail
// arrives in, and the one the body's flattener has to turn into markdown. A
// plain part that is empty or whitespace must not stand in for the html.
func gmailHTMLMessage(id, thread, subject, from, to, internalDate, plain, body string) map[string]any {
	msg := gmailMessage(id, thread, subject, from, to, internalDate, "")
	payload := msg["payload"].(map[string]any)
	payload["mimeType"] = "multipart/alternative"
	delete(payload, "body")
	parts := []any{}
	if plain != "" {
		parts = append(parts, map[string]any{
			"mimeType": "text/plain",
			"body":     map[string]any{"data": b64url(plain), "size": len(plain)},
		})
	}
	payload["parts"] = append(parts, map[string]any{
		"mimeType": "text/html",
		"body":     map[string]any{"data": b64url(body), "size": len(body)},
	})
	return msg
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

func gmailStepProps(extra map[string]any) map[string]any {
	return syncProps(extra, "enabledGmail")
}

// gmailStepConfig is the gmail account a stepped invocation walks.
func gmailStepConfig(props map[string]any) map[string]any {
	return stepConfig(googleAccountType, googleAccountID, props)
}

// TestGoogleGmailFakeSyncMirrors drives a first (backfill) sync against the
// loopback fake and asserts the whole shape the ticket promises: the mirrors,
// the core emailthread/emailmessage rows under the SAME derived ids, the
// people the addresses resolved to through the mapping's one hop, and the
// stamp carrying the RUN-START historyId.
func TestGoogleGmailFakeSyncMirrors(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	fake := newFakeGmail(t)
	fake.listed = []string{"m1", "m2"}
	fake.msgs["m1"] = gmailMessage("m1", "t-m1", "Rack layout",
		"Alice Example <alice@example.com>", "Ada <ada@example.com>",
		millisAgo(48*time.Hour), "the cold aisle plan")
	fake.msgs["m2"] = gmailMessage("m2", "t-m1", "Re: Rack layout",
		"Ada <ada@example.com>", "alice@example.com",
		millisAgo(24*time.Hour), "looks good")
	// The html message, in a thread of its own so the newest-wins assertions
	// on t-m1 keep their subject. Its text/plain alternative is whitespace,
	// which is a body only a sender's template thinks it is.
	fake.listed = append(fake.listed, "m3")
	fake.msgs["m3"] = gmailHTMLMessage("m3", "t-m3", "Datacenter tour",
		"Bob <bob@example.com>", "ada@example.com",
		millisAgo(72*time.Hour), "\r\n   \r\n",
		`<html><head><style>a{color:red}</style></head><body>`+
			`<img src="data:image/png;base64,`+strings.Repeat("A", 600)+`">`+
			`<p>Book a slot&nbsp;now: <a href="https://example.com/tour?a=1&amp;b=2">`+
			`click <strong>here</strong></a>.</p>`+
			`<ul><li><a href="https://example.com/map?to=(dc)">the map</a></li>`+
			`<li><a href="https://example.com/dir">directions<li>parking</ul>`+
			`<table><tr><td>Rack A<td>Rack B</tr></table>`+
			`<p>PS: <a href="https://example.com/ps">read this</p>`+
			`<p>and the sign-off</p>`+
			`</body></html>`)
	fake.start(t)

	_, ds := newDataset(t)
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL))

	googleSeedAccount(t, ds, googleAccountID)

	s := newStepper(t, ds, googleGmailFn, gmailStepConfig(gmailStepProps(nil)))
	effects := s.drainApplying(nil)

	// The mirrors landed under the SDK-derived ids.
	msgID := runner.ExternalID("gmail-message", "acct-step", "m1")
	mirror, err := ds.Get(ctx, googleMessageType, msgID)
	if err != nil {
		t.Fatalf("message mirror did not sync: %v", err)
	}
	if mirror.Properties["subject"] != "Rack layout" {
		t.Fatalf("mirror subject = %v", mirror.Properties["subject"])
	}
	if !strings.Contains(fmt.Sprint(mirror.Properties["text"]), "cold aisle") {
		t.Fatalf("mirror text = %v, want the decoded body", mirror.Properties["text"])
	}
	// The raw payload keeps the structure and drops the base64 bytes.
	raw := fmt.Sprint(mirror.Properties["raw"])
	if strings.Contains(raw, b64url("the cold aisle plan")) {
		t.Fatalf("raw still carries the inline body bytes: %s", raw)
	}

	// The html body reaches `text` as markdown with its hrefs intact: core's
	// emailmessage.text is a markdown property, and a reader given "click
	// here" without the target has nothing to click (#188). The fixture holds
	// every hazard the flattener has to survive at once: a whitespace
	// text/plain part that must not stand in for the body, an inline data:
	// URI, a table whose cells omit their optional end tags, a url whose `)`
	// would end a plain markdown destination early, an inline `<strong>`
	// inside a label, which must not cut the label short, and two `<a>` tags
	// with no `</a>`, which must end at the paragraph and the list item
	// rather than swallowing what follows into the link.
	htmlID := runner.ExternalID("gmail-message", "acct-step", "m3")
	htmlMirror, err := ds.Get(ctx, googleMessageType, htmlID)
	if err != nil {
		t.Fatalf("html message did not sync: %v", err)
	}
	wantText := "Book a slot now: [click here](https://example.com/tour?a=1&b=2).\n\n" +
		"- [the map](<https://example.com/map?to=(dc)>)\n" +
		"- [directions](https://example.com/dir)\n- parking\n\n" +
		"Rack A Rack B\n\n" +
		"PS: [read this](https://example.com/ps)\n\nand the sign-off"
	if got := fmt.Sprint(htmlMirror.Properties["text"]); got != wantText {
		t.Fatalf("html body flattened to %q, want %q", got, wantText)
	}
	// MIRRORS ONLY (record 49): no core messaging row is written at all, and
	// the mirror is where the flattened body lands.
	threadID := runner.ExternalID("gmail-thread", "acct-step", "t-m1")
	if got := fmt.Sprint(mirror.Properties["threadId"]); got != "t-m1" {
		t.Fatalf("message mirror threadId = %q", got)
	}
	for _, absent := range []struct{ kind, id string }{
		{coreMessageType, msgID},
		{coreMessageType, htmlID},
		{coreThreadType, threadID},
	} {
		if _, err := ds.Get(ctx, absent.kind, absent.id); err == nil {
			t.Fatalf("the sync wrote %s %s, a kind this package does not own", absent.kind, absent.id)
		}
	}

	// Every address on a header lands as one emailaddress mirror, with an
	// EMPTY subject slot: the kind it describes is the repository's to say.
	addrID := runner.ExternalID("google-address", "acct-step", "alice@example.com")
	addrRow, err := ds.Get(ctx, googleAddressType, addrID)
	if err != nil {
		t.Fatalf("emailaddress record did not sync: %v", err)
	}
	if got := refIDs(addrRow, "person"); len(got) != 0 {
		t.Fatalf("the address row filled its subject slot with %v, and no mapping is declared", got)
	}

	// Newest-wins on the thread: m2 is the later message, so its subject and
	// preview stand even though m1 was mirrored in the same batch.
	thread, err := ds.Get(ctx, googleThreadType, threadID)
	if err != nil {
		t.Fatalf("thread mirror did not sync: %v", err)
	}
	if thread.Properties["subject"] != "Re: Rack layout" {
		t.Fatalf("thread subject = %v, want the newest message's", thread.Properties["subject"])
	}
	if n := len(thread.Properties["participants"].([]any)); n != 2 {
		t.Fatalf("thread has %d participants, want 2", n)
	}

	// The stamp: the RUN-START historyId, never a later one.
	stamp := accountStamp(t, effects, googleAccountType, googleAccountID)
	if stamp["gmailHistoryId"] != "9000" {
		t.Fatalf("gmailHistoryId = %v, want the run-start 9000", stamp["gmailHistoryId"])
	}
	// The rollup the console reads AND this stream's own cadence anchor.
	if stamp["syncStatus"] != "ok" {
		t.Fatalf("syncStatus = %v", stamp["syncStatus"])
	}
	for _, key := range []string{"lastSyncedAt", "gmailLastSyncedAt"} {
		if s, _ := stamp[key].(string); s == "" {
			t.Fatalf("%s not stamped", key)
		}
	}
	if s, _ := stamp["gmailBackfillAnchorAt"].(string); s == "" {
		t.Fatalf("gmailBackfillAnchorAt not stamped on the first run")
	}
}

// TestGoogleGmailFlattenerCapsGuardTheBody pins the two body-cap hazards in
// the html flattener, each of which stored the wrong `text` for a message
// (#191). The caps are lowered through the source seam so the fixtures stay
// small; the slice-versus-substitute ORDER and the label back-off are what
// the assertions turn on.
func TestGoogleGmailFlattenerCapsGuardTheBody(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	fake := newFakeGmail(t)
	fake.listed = []string{"mcap", "mlbl"}

	// mcap: a 400-character inline data: URI ahead of the letter, with the
	// source cap lowered to 200 below. Slicing the source before the data:
	// substitution leaves the letter past the cut and base64 inside it, so
	// the message syncs as the head of a payload; the substitution runs first
	// now and the letter survives.
	fake.msgs["mcap"] = gmailHTMLMessage("mcap", "t-mcap", "Inline photo",
		"Bob <bob@example.com>", "ada@example.com",
		millisAgo(72*time.Hour), "\r\n   \r\n",
		`<html><body><img src="data:image/png;base64,`+
			strings.Repeat("A", 400)+`"><p>letter after the photo</p></body></html>`)

	// mlbl: a body longer than the lowered 40-character text cap whose cut
	// lands inside a link label, before its `](` was written. The old back-off
	// only knew the destination half of a span and stored `[read the fu`; the
	// unclosed `[` is backed off now, leaving the padding it wrote before it.
	fake.msgs["mlbl"] = gmailHTMLMessage("mlbl", "t-mlbl", "Long body",
		"Bob <bob@example.com>", "ada@example.com",
		millisAgo(71*time.Hour), "\r\n   \r\n",
		`<html><body><p>paddingpaddingpaddingpaddi</p>`+
			`<p><a href="https://example.com/x">read the full report now</a></p>`+
			`</body></html>`)
	fake.start(t)

	_, ds := newDataset(t)
	// The caps are lowered through the source seam so the fixtures stay small.
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL),
		[2]string{"HTML_SOURCE_MAX = 4000000", "HTML_SOURCE_MAX = 200"},
		[2]string{"TEXT_MAX = 8000", "TEXT_MAX = 40"})
	googleSeedAccount(t, ds, googleAccountID)

	s := newStepper(t, ds, googleGmailFn, gmailStepConfig(gmailStepProps(nil)))
	s.drainApplying(nil)

	capID := runner.ExternalID("gmail-message", "acct-step", "mcap")
	capMirror, err := ds.Get(ctx, googleMessageType, capID)
	if err != nil {
		t.Fatalf("inline-photo message did not sync: %v", err)
	}
	if got := fmt.Sprint(capMirror.Properties["text"]); got != "letter after the photo" {
		t.Fatalf("an inline photo past the source cap replaced the body: text = %q", got)
	}

	lblID := runner.ExternalID("gmail-message", "acct-step", "mlbl")
	lblMirror, err := ds.Get(ctx, googleMessageType, lblID)
	if err != nil {
		t.Fatalf("long-body message did not sync: %v", err)
	}
	got := fmt.Sprint(lblMirror.Properties["text"])
	if strings.Contains(got, "[") {
		t.Fatalf("the body cap cut inside a link label: text = %q", got)
	}
	if got != "paddingpaddingpaddingpaddi" {
		t.Fatalf("the label back-off did not restore the body: text = %q", got)
	}
}

// TestGoogleGmailHistoryExpiryAndSweepScope is the regression the plan calls
// the archive-deletion hazard: only a 404 drops to a full re-read, and the
// sweep that follows deletes only inside the re-read WINDOW. A message whose
// sentAt predates the window survives with its old generation intact.
func TestGoogleGmailHistoryExpiryAndSweepScope(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	fake := newFakeGmail(t)
	fake.historyGone = true
	fake.listed = []string{"m1"}
	fake.msgs["m1"] = gmailMessage("m1", "t-m1", "Rack layout",
		"alice@example.com", "ada@example.com", millisAgo(24*time.Hour), "hi")
	fake.start(t)

	_, ds := newDataset(t)
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL))

	googleSeedAccount(t, ds, googleAccountID)

	// Two rows a previous run left behind, both under a stale generation: one
	// INSIDE the coming re-read window (last30d back from now) and one deep in
	// the archive, years before it.
	inside := runner.ExternalID("gmail-message", "acct-step", "stale-inside")
	archived := runner.ExternalID("gmail-message", "acct-step", "archived")
	for id, sentAt := range map[string]string{
		inside:   ago(24 * time.Hour),
		archived: ago(7 * 365 * 24 * time.Hour),
	} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: googleMessageType, ID: id,
			Properties: map[string]any{
				"account": "acct-step", "messageId": id, "threadId": "t-old",
				"syncGeneration": "an-older-generation", "sentAt": sentAt,
			},
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	s := newStepper(t, ds, googleGmailFn, gmailStepConfig(gmailStepProps(
		map[string]any{"gmailHistoryId": "8000"})))
	s.drainApplying(nil)

	// The in-window stale row is tombstoned; the archived one the window never
	// touched is still live. Deleting it would be the regression.
	swept, err := ds.Get(ctx, googleMessageType, inside)
	if err != nil {
		t.Fatalf("get the in-window stale row: %v", err)
	}
	if swept.DeletedAt == nil {
		t.Fatalf("the in-window stale row survived the sweep")
	}
	kept, err := ds.Get(ctx, googleMessageType, archived)
	if err != nil {
		t.Fatalf("get the archived row: %v", err)
	}
	if kept.DeletedAt != nil {
		t.Fatalf("the sweep deleted an ARCHIVED message outside the re-read window")
	}

	// The re-read really happened: the backfill listing ran with an `after:`
	// clause derived from the window floor.
	var sawAfter bool
	for _, q := range fake.seen() {
		if strings.Contains(q, "after%3A") || strings.Contains(q, "after:") {
			sawAfter = true
		}
	}
	if !sawAfter {
		t.Fatalf("no windowed backfill query ran: %v", fake.seen())
	}
}

// TestGoogleGmailHistoryBadStatusPropagates pins the other half of the expiry
// rule: a 400 is a request bug, not an expired watermark, and it must surface
// as an erroring stamp rather than silently full-re-reading the mailbox.
func TestGoogleGmailHistoryBadStatusPropagates(t *testing.T) {
	t.Parallel()
	requireUV(t)
	fake := newFakeGmail(t)
	fake.historyBad = true
	fake.start(t)

	_, ds := newDataset(t)
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL))

	googleSeedAccount(t, ds, googleAccountID)

	s := newStepper(t, ds, googleGmailFn, gmailStepConfig(gmailStepProps(
		map[string]any{"gmailHistoryId": "8000"})))
	effects := s.drainApplying(nil)

	stamp := accountStamp(t, effects, googleAccountType, googleAccountID)
	status, _ := stamp["syncStatus"].(string)
	if !strings.HasPrefix(status, "erroring: ") || !strings.Contains(status, "400") {
		t.Fatalf("syncStatus = %q, want an erroring HTTP 400", status)
	}
	for _, key := range []string{"lastSyncedAt", "gmailLastSyncedAt"} {
		if _, ok := stamp[key]; ok {
			t.Fatalf("the erroring stamp advanced %s — the next window would skip unread mail", key)
		}
	}
	for _, q := range fake.seen() {
		if strings.Contains(q, "after") {
			t.Fatalf("a 400 silently triggered a full re-read: %v", fake.seen())
		}
	}
}

// TestGoogleGmailOriginPinRefusal points the body's API base at a
// non-loopback, non-Gmail origin: the token is never sent, the account stamps
// erroring with the refusal, and the chain completes instead of parking.
func TestGoogleGmailOriginPinRefusal(t *testing.T) {
	t.Parallel()
	requireUV(t)
	_, ds := newDataset(t)
	googleInstall(t, ds, gmailAPIAt("https://intercepted.example"))

	googleSeedAccount(t, ds, googleAccountID)

	s := newStepper(t, ds, googleGmailFn, gmailStepConfig(gmailStepProps(nil)))
	effects, _, cur := s.step(nil)
	if cur != nil {
		t.Fatalf("the refusal did not end the chain")
	}
	stamp := accountStamp(t, effects, googleAccountType, googleAccountID)
	status, _ := stamp["syncStatus"].(string)
	if !strings.HasPrefix(status, "erroring: ") ||
		!strings.Contains(status, "refusing to send credentials") {
		t.Fatalf("syncStatus = %q, want an erroring refusal", status)
	}
	if !strings.Contains(status, "intercepted.example") {
		t.Fatalf("the refusal does not name the refused origin: %q", status)
	}
	for _, key := range []string{"lastSyncedAt", "gmailLastSyncedAt"} {
		if _, ok := stamp[key]; ok {
			t.Fatalf("a refused run stamped %s", key)
		}
	}
}

// TestGoogleGmailPerAccountIsolation proves F2: the first account's dead
// grant stamps only itself, and the account behind it still syncs.
func TestGoogleGmailPerAccountIsolation(t *testing.T) {
	t.Parallel()
	requireUV(t)
	fake := newFakeGmail(t)
	fake.listed = []string{"m1"}
	fake.msgs["m1"] = gmailMessage("m1", "t-m1", "Rack layout",
		"alice@example.com", "ada@example.com", millisAgo(24*time.Hour), "hi")
	fake.start(t)

	_, ds := newDataset(t)
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL))

	googleSeedAccount(t, ds, googleAccountID)
	googleSeedAccount(t, ds, "acct-dead")

	cfg := map[string]any{"accounts": []any{
		map[string]any{
			"id": "acct-dead", "type": googleAccountType,
			"properties": gmailStepProps(nil),
			"tokenError": "the stored grant could not be refreshed",
		},
		map[string]any{
			"id": "acct-step", "type": googleAccountType,
			"properties": gmailStepProps(nil), "token": "at-1",
		},
	}}
	s := newStepper(t, ds, googleGmailFn, cfg)
	effects := s.drainApplying(nil)

	dead := stampOf(effects, "acct-dead")
	if dead == nil {
		t.Fatalf("the dead-grant account got no stamp")
	}
	if status, _ := dead["syncStatus"].(string); !strings.HasPrefix(status, "erroring: ") {
		t.Fatalf("acct-dead syncStatus = %q", status)
	}
	for _, key := range []string{"lastSyncedAt", "gmailLastSyncedAt"} {
		if _, ok := dead[key]; ok {
			t.Fatalf("the erroring account advanced its %s", key)
		}
	}
	live := accountStamp(t, effects, googleAccountType, googleAccountID)
	if live["syncStatus"] != "ok" {
		t.Fatalf("the healthy account behind a poisoned one did not sync: %v", live)
	}
}

// TestGoogleGmailAddressConverges is the mapping proof the emailaddress type
// exists for: two accounts seeing the SAME address resolve to ONE person, and
// an address an address-book contact already minted a person for links onto
// that person rather than minting a shell beside it.
func TestGoogleGmailAddressConverges(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	_, ds := newDataset(t)
	googleInstall(t, ds)
	// The PEOPLE SAMPLE ships the two mappings this test needs, onto `contact`
	// and `emailaddress` (record 49). They were dropped when people was
	// imported, because this provider was absent; re-applying that closure
	// now that it is installed is what lands them, which is the re-import the
	// console asks a reader for.
	if err := enginetest.DeclareMappings(ctx, ds); err != nil {
		t.Fatalf("land the people sample's google mappings: %v", err)
	}

	// A contact the address book already synced: its mapping minted a person.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: googleContactType, ID: "seed-contact",
		Properties: map[string]any{
			"account": "acct-a", "resourceName": "people/c1",
			"name":   map[string]any{"displayName": "Ada Lovelace"},
			"emails": []any{map[string]any{"value": "ada@example.com"}},
		},
	}); err != nil {
		t.Fatalf("seed contact: %v", err)
	}
	want := googlePersonOf(t, ds, googleContactType, "seed-contact")

	// The same address seen through mail, on TWO different accounts: two
	// records, one person, and it is the address book's person.
	var addrs []string
	for _, account := range []string{"acct-a", "acct-b"} {
		id := runner.ExternalID("google-address", account, "ada@example.com")
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: googleAddressType, ID: id,
			Properties: map[string]any{
				"account": account, "address": "ada@example.com", "displayName": "Ada",
			},
		}); err != nil {
			t.Fatalf("put address for %s: %v", account, err)
		}
		addrs = append(addrs, id)
	}
	first := googlePersonOf(t, ds, googleAddressType, addrs[0])
	second := googlePersonOf(t, ds, googleAddressType, addrs[1])
	if first != second {
		t.Fatalf("the same address on two accounts resolved two people: %s and %s", first, second)
	}
	if first != want {
		t.Fatalf("the mail address minted a shell (%s) instead of linking the contact's person (%s)",
			first, want)
	}
}

// TestGoogleGmailCappedRereadDefersSweep is the DATA-LOSS regression, and the
// reason the fake pages its listing at all.
//
// A 404 on history.list drops to a windowed full re-read with the sweep
// armed. That walk is capped — by list pages and by invocations — so on a
// mailbox bigger than the cap it TRUNCATES and persists a resume. The sweep
// deletes every in-window mirror row this generation did not stamp, and a
// truncated walk stamped nothing behind its page token: running it there
// deletes everything older than the newest page, mirror and core, with the
// core thread deletes cascading their messages. Under `backfillDepth: all`
// the window has no floor at all and it takes the whole archive.
//
// So: the truncated run must NOT sweep, the resume must carry the pending
// sweep across the run boundary, and the run that finally DRAINS the walk
// must sweep — otherwise the reconciliation is silently lost instead.
func TestGoogleGmailCappedRereadDefersSweep(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	fake := newFakeGmail(t)
	fake.historyGone = true
	// THREE listing pages against a two-page cap: the walk stops with page
	// three still unread, exactly the shape a real 2,000-message cap has.
	fake.pages = [][]string{{"m1"}, {"m2"}, {"m3"}}
	for i, id := range []string{"m1", "m2", "m3"} {
		fake.msgs[id] = gmailMessage(id, "t-"+id, "Rack layout "+id,
			"alice@example.com", "ada@example.com",
			millisAgo(time.Duration(i+1)*24*time.Hour), "hi")
	}
	fake.start(t)

	_, ds := newDataset(t)
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL),
		[2]string{"MAX_LIST_PAGES = 20", "MAX_LIST_PAGES = 2"})
	googleSeedAccount(t, ds, googleAccountID)

	// A row a previous generation left behind, INSIDE the coming window and
	// BEHIND the page cap — the capped walk never reaches it, so nothing this
	// run learns says it should die.
	inside := runner.ExternalID("gmail-message", "acct-step", "stale-inside")
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: googleMessageType, ID: inside,
		Properties: map[string]any{
			"account": "acct-step", "messageId": "stale-inside",
			"threadId": "t-old", "syncGeneration": "an-older-generation",
			"sentAt": ago(36 * time.Hour),
		},
	}); err != nil {
		t.Fatalf("seed the in-window row: %v", err)
	}

	props := gmailStepProps(map[string]any{"gmailHistoryId": "8000"})
	first := newStepper(t, ds, googleGmailFn, gmailStepConfig(props)).
		drainApplying(nil)

	// ROUND ONE — truncated. The row the walk never reached is still live.
	row, err := ds.Get(ctx, googleMessageType, inside)
	if err != nil {
		t.Fatalf("get the in-window row: %v", err)
	}
	if row.DeletedAt != nil {
		t.Fatalf("a TRUNCATED full re-read swept a row its walk never reached — " +
			"every message older than the last page it read is gone")
	}
	stamp := accountStamp(t, first, googleAccountType, googleAccountID)
	resume, _ := stamp["gmailBackfillResume"].(map[string]any)
	if resume == nil || resume["pageToken"] == "" {
		t.Fatalf("the capped walk persisted no resume: %v", stamp["gmailBackfillResume"])
	}
	if resume["sweep"] != true {
		t.Fatalf("the resume dropped the PENDING sweep (%v) — the reconciliation "+
			"would be lost the moment the walk was truncated", resume)
	}
	if _, leaked := resume["token"]; leaked {
		t.Fatalf("the resume carries a credential: %v", resume)
	}

	// ROUND TWO — the persisted resume drains the walk, and NOW the sweep runs
	// under the same generation and the same window.
	props = gmailStepProps(map[string]any{
		"gmailHistoryId": "8000", "gmailBackfillResume": resume,
	})
	second := newStepper(t, ds, googleGmailFn, gmailStepConfig(props)).
		drainApplying(nil)

	if _, err := ds.Get(ctx, googleMessageType,
		runner.ExternalID("gmail-message", "acct-step", "m3")); err != nil {
		t.Fatalf("the resumed run never read the page behind the cap: %v", err)
	}
	row, err = ds.Get(ctx, googleMessageType, inside)
	if err != nil {
		t.Fatalf("get the in-window row: %v", err)
	}
	if row.DeletedAt == nil {
		t.Fatalf("the run that DRAINED the walk never swept — the deletes a full " +
			"re-read cannot tombstone are lost forever")
	}
	// The rows the resumed walk did stamp survive it.
	for _, id := range []string{"m1", "m2", "m3"} {
		kept, err := ds.Get(ctx, googleMessageType,
			runner.ExternalID("gmail-message", "acct-step", id))
		if err != nil || kept.DeletedAt != nil {
			t.Fatalf("the sweep deleted %s, which this generation stamped: %v %v", id, kept, err)
		}
	}
	stamp = accountStamp(t, second, googleAccountType, googleAccountID)
	if held, _ := stamp["gmailBackfillResume"].(map[string]any); len(held) != 0 {
		t.Fatalf("a drained walk left a resume behind: %v", held)
	}
}

// TestGoogleGmailStaleResumeTokenRestarts pins the other half of the resume
// contract: a Gmail list page token is short-lived, so one held across a run
// boundary 400s. Resuming it unconditionally parks the account on a dead
// token forever — the run stamps erroring, persists the same token, and the
// next run repeats it. The window restarts from page one instead.
func TestGoogleGmailStaleResumeTokenRestarts(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	fake := newFakeGmail(t)
	fake.listed = []string{"m1"}
	fake.msgs["m1"] = gmailMessage("m1", "t-m1", "Rack layout",
		"alice@example.com", "ada@example.com", millisAgo(24*time.Hour), "hi")
	fake.start(t)

	_, ds := newDataset(t)
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL))
	googleSeedAccount(t, ds, googleAccountID)

	// A resume the fake will never honor: this is what a page token looks
	// like an hour after it was issued.
	props := gmailStepProps(map[string]any{"gmailBackfillResume": map[string]any{
		"pageToken": "p-issued-last-run", "floor": ago(30 * 24 * time.Hour),
		"generation": "gen-1", "historyId": "8500",
	}})
	effects := newStepper(t, ds, googleGmailFn, gmailStepConfig(props)).
		drainApplying(nil)

	stamp := accountStamp(t, effects, googleAccountType, googleAccountID)
	if stamp["syncStatus"] != "ok" {
		t.Fatalf("syncStatus = %v — a stale page token parked the account instead "+
			"of restarting its window", stamp["syncStatus"])
	}
	if _, err := ds.Get(ctx, googleMessageType,
		runner.ExternalID("gmail-message", "acct-step", "m1")); err != nil {
		t.Fatalf("the restarted window synced nothing: %v", err)
	}
	if held, _ := stamp["gmailBackfillResume"].(map[string]any); len(held) != 0 {
		t.Fatalf("the dead token was persisted again: %v", held)
	}
	var restarted bool
	for _, q := range fake.seen() {
		if strings.Contains(q, "maxResults=100") && !strings.Contains(q, "pageToken") {
			restarted = true
		}
	}
	if !restarted {
		t.Fatalf("no tokenless listing ran — the window never restarted: %v", fake.seen())
	}
}

// TestGoogleGmailHistoryDeltaAddsAndSkips drives the INCREMENTAL path the
// fixture's `history`, `missing` and `msgCalls` dials exist for and nothing
// exercised: messagesAdded hydrates, a message that vanished between the
// delta and the fetch is retracted and COUNTED, and the stamp says so.
func TestGoogleGmailHistoryDeltaAddsAndSkips(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	fake := newFakeGmail(t)
	fake.historyID = "9500"
	fake.history = []string{"m1", "m2"}
	fake.missing["m2"] = true
	fake.msgs["m1"] = gmailMessage("m1", "t-m1", "Rack layout",
		"alice@example.com", "ada@example.com", millisAgo(2*time.Hour), "hi")
	fake.start(t)

	_, ds := newDataset(t)
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL))
	googleSeedAccount(t, ds, googleAccountID)

	effects := newStepper(t, ds, googleGmailFn, gmailStepConfig(
		gmailStepProps(map[string]any{"gmailHistoryId": "9000"}))).drainApplying(nil)

	if _, err := ds.Get(ctx, googleMessageType,
		runner.ExternalID("gmail-message", "acct-step", "m1")); err != nil {
		t.Fatalf("the delta's added message did not sync: %v", err)
	}
	// Both ids were fetched; the 404 did not abort the batch behind it.
	if n := fake.gets(); n != 2 {
		t.Fatalf("messages.get ran %d times, want one per delta id", n)
	}
	var sawGet bool
	for _, p := range fake.seenPaths() {
		if p == "/gmail/v1/users/me/messages/m1" {
			sawGet = true
		}
	}
	if !sawGet {
		t.Fatalf("no messages.get for m1: %v", fake.seenPaths())
	}
	stamp := accountStamp(t, effects, googleAccountType, googleAccountID)
	if stamp["syncStatus"] != "ok (1 skipped)" {
		t.Fatalf("syncStatus = %v, want the vanished message counted", stamp["syncStatus"])
	}
	// The RUN-START watermark, read before a single message was.
	if stamp["gmailHistoryId"] != "9500" {
		t.Fatalf("gmailHistoryId = %v, want the run-start 9500", stamp["gmailHistoryId"])
	}
	// An incremental run is not a re-read: no windowed listing, no sweep.
	for _, q := range fake.seen() {
		if strings.Contains(q, "after") {
			t.Fatalf("an incremental delta ran a windowed re-read: %v", fake.seen())
		}
	}
}

// TestGoogleGmailHistoryDeleteRetractsEmptyThread: a history delta's
// retractions take the messages AND, once a thread has no message left, the
// derived thread rows. Threads are derived from the message stream — nothing
// ever revisits one whose messages are all gone — so a thread left standing
// here is stale in both halves forever.
func TestGoogleGmailHistoryDeleteRetractsEmptyThread(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	fake := newFakeGmail(t)
	fake.listed = []string{"m9"}
	fake.msgs["m9"] = gmailMessage("m9", "t-m9", "Rack layout",
		"alice@example.com", "ada@example.com", millisAgo(4*time.Hour), "hi")
	fake.start(t)

	_, ds := newDataset(t)
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL))
	googleSeedAccount(t, ds, googleAccountID)

	// Round one: an ordinary backfill writes the message and its thread.
	newStepper(t, ds, googleGmailFn, gmailStepConfig(gmailStepProps(nil))).
		drainApplying(nil)
	msgID := runner.ExternalID("gmail-message", "acct-step", "m9")
	threadID := runner.ExternalID("gmail-thread", "acct-step", "t-m9")
	for _, ref := range []struct{ typ, id string }{
		{googleMessageType, msgID},
		{googleThreadType, threadID},
	} {
		if _, err := ds.Get(ctx, ref.typ, ref.id); err != nil {
			t.Fatalf("round one wrote no %s: %v", ref.typ, err)
		}
	}

	// Round two: the delta says the thread's only message is gone.
	fake.historyID = "9600"
	fake.deleted = []string{"m9"}
	newStepper(t, ds, googleGmailFn, gmailStepConfig(gmailStepProps(
		map[string]any{"gmailHistoryId": "9000"}))).drainApplying(nil)

	for _, ref := range []struct{ typ, id, what string }{
		{googleMessageType, msgID, "the message mirror"},
		{googleThreadType, threadID, "the thread mirror"},
	} {
		row, err := ds.Get(ctx, ref.typ, ref.id)
		if err != nil {
			t.Fatalf("get %s: %v", ref.typ, err)
		}
		if row.DeletedAt == nil {
			t.Fatalf("%s survived the retraction of its last message", ref.what)
		}
	}
}

// TestGoogleGmailMalformedAddressSkipped is the parked-drain regression.
// `emailaddress.address` is an `email`-kind property, and the engine
// validates it with Go's net/mail.ParseAddress at effect APPLY — outside this
// body's try/except. An address pattern looser than Go's therefore does not
// fail one header: it rolls the whole page's transaction back, and the drain
// parks there deterministically on every retry.
func TestGoogleGmailMalformedAddressSkipped(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	fake := newFakeGmail(t)
	fake.listed = []string{"m1"}
	// Every one of these is a dot-atom shape Go's parser rejects and the old
	// pattern admitted, and each survives header parsing intact so the body
	// really does see it; `ada@example.com` is the one legitimate recipient.
	fake.msgs["m1"] = gmailMessage("m1", "t-m1", "Rack layout",
		"alice@example.com",
		`a..b@example.com, .a@example.com, ada@example.com, a.@example.com`,
		millisAgo(3*time.Hour), "hi")
	fake.start(t)

	_, ds := newDataset(t)
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL))
	googleSeedAccount(t, ds, googleAccountID)

	effects := newStepper(t, ds, googleGmailFn, gmailStepConfig(
		gmailStepProps(nil))).drainApplying(nil)

	stamp := accountStamp(t, effects, googleAccountType, googleAccountID)
	if stamp["syncStatus"] != "ok" {
		t.Fatalf("syncStatus = %v", stamp["syncStatus"])
	}
	msgID := runner.ExternalID("gmail-message", "acct-step", "m1")
	if _, err := ds.Get(ctx, googleMessageType, msgID); err != nil {
		t.Fatalf("message mirror did not sync: %v", err)
	}
	for _, bad := range []string{"a..b@example.com", ".a@example.com", "a.@example.com"} {
		id := runner.ExternalID("google-address", "acct-step", bad)
		if row, err := ds.Get(ctx, googleAddressType, id); err == nil && row.DeletedAt == nil {
			t.Fatalf("the sync wrote an emailaddress the engine would refuse: %q", bad)
		}
	}
}

// TestGoogleGmailSyncsWithNoMappingDeclared is the regression the review
// caught: with the core rows gone, a FRESH repository that has declared no
// mapping at all must still get its mail. Before phase 4 the mirror and the
// core row rode in one page, so the refused core write rolled the whole page
// back and a gmail sync delivered NOTHING; now the mirrors are the output and
// the subject slots simply stay empty.
//
// Nothing here declares a mapping, and nothing imports the people sample: this
// is a repository with core and the google closure, which is what a first
// connect looks like.
func TestGoogleGmailSyncsWithNoMappingDeclared(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	fake := newFakeGmail(t)
	fake.listed = []string{"m1"}
	fake.msgs["m1"] = gmailMessage("m1", "t-m1", "Rack layout",
		"Alice Example <alice@example.com>", "ada@example.com",
		millisAgo(24*time.Hour), "the cold aisle plan")
	fake.start(t)

	_, ds := newCoreDataset(t)
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL))
	googleSeedAccount(t, ds, googleAccountID)

	effects := newStepper(t, ds, googleGmailFn, gmailStepConfig(gmailStepProps(nil))).
		drainApplying(nil)

	// The run completed, and the mirrors are all there.
	if stamp := accountStamp(t, effects, googleAccountType, googleAccountID); stamp["syncStatus"] != "ok" {
		t.Fatalf("syncStatus = %v, want a clean run with no mapping declared", stamp["syncStatus"])
	}
	msgID := runner.ExternalID("gmail-message", "acct-step", "m1")
	if got := mustGet(t, ds, googleMessageType, msgID).Properties["subject"]; got != "Rack layout" {
		t.Fatalf("message mirror subject = %v", got)
	}
	threadID := runner.ExternalID("gmail-thread", "acct-step", "t-m1")
	if _, err := ds.Get(ctx, googleThreadType, threadID); err != nil {
		t.Fatalf("thread mirror did not sync: %v", err)
	}
	addrID := runner.ExternalID("google-address", "acct-step", "alice@example.com")
	addr, err := ds.Get(ctx, googleAddressType, addrID)
	if err != nil {
		t.Fatalf("emailaddress mirror did not sync: %v", err)
	}
	if got := refIDs(addr, "person"); len(got) != 0 {
		t.Fatalf("the address row filled its subject slot with %v, and no mapping is declared", got)
	}
}

// TestGoogleGmailBackfillBoundedByInvocations is the LIVELOCK regression.
//
// The engine bounds a paged drain by cumulative wall clock from the chain's
// first committed page and refuses a middle page BEFORE committing it. A
// backfill bounded only by list pages needs ~5 invocations per page and ~100
// for a full walk, so it is cut off with nothing persisted, and the next
// scheduled run restarts from page one and dies in the same place — forever.
//
// The chain is therefore bounded by INVOCATIONS too: every run stops early
// enough to COMMIT its resume, and every run resumes further along than the
// last, so the walk actually finishes across runs.
func TestGoogleGmailBackfillBoundedByInvocations(t *testing.T) {
	t.Parallel()
	requireUV(t)
	ctx := context.Background()
	fake := newFakeGmail(t)
	ids := []string{"m1", "m2", "m3", "m4", "m5", "m6"}
	for i, id := range ids {
		fake.pages = append(fake.pages, []string{id})
		fake.msgs[id] = gmailMessage(id, "t-"+id, "Rack layout "+id,
			"alice@example.com", "ada@example.com",
			millisAgo(time.Duration(i+1)*time.Hour), "hi")
	}
	fake.start(t)

	_, ds := newDataset(t)
	// Six pages is well under the 20-page cap: only the invocation bound can
	// stop this walk, which is exactly the point.
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL),
		[2]string{"MAX_CALLS = 12", "MAX_CALLS = 4"})
	googleSeedAccount(t, ds, googleAccountID)

	var resume map[string]any
	seenTokens := map[string]bool{}
	runs := 0
	for {
		runs++
		if runs > len(ids)+2 {
			t.Fatalf("the walk never drained across %d runs", runs)
		}
		extra := map[string]any{}
		if resume != nil {
			extra["gmailBackfillResume"] = resume
		}
		s := newStepper(t, ds, googleGmailFn, gmailStepConfig(gmailStepProps(extra)))
		effects := s.drainApplying(nil)
		if s.n > 8 {
			t.Fatalf("run %d took %d invocations — the bound did not bind", runs, s.n)
		}
		stamp := accountStamp(t, effects, googleAccountType, googleAccountID)
		held, _ := stamp["gmailBackfillResume"].(map[string]any)
		if len(held) == 0 {
			break
		}
		tok, _ := held["pageToken"].(string)
		if tok == "" || seenTokens[tok] {
			t.Fatalf("run %d made no forward progress: token %q already seen %v",
				runs, tok, seenTokens)
		}
		seenTokens[tok] = true
		resume = held
	}
	if runs < 2 {
		t.Fatalf("the whole walk drained in one run — the invocation bound never tripped")
	}
	for _, id := range ids {
		if _, err := ds.Get(ctx, googleMessageType,
			runner.ExternalID("gmail-message", "acct-step", id)); err != nil {
			t.Fatalf("%s never synced across %d runs: %v", id, runs, err)
		}
	}
}
