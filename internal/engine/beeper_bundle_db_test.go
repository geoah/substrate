package engine

// The Beeper bundle — the substrate's first NON-OAUTH connector. Proofs, from
// the shipped closure at ../../kinds/beeper.bundles.substrate.reamde.dev:
//
//  1. TestBeeperBundleAdmitsSchema — the closure ADMITS through the schema
//     loader with the non-OAuth shapes intact: the bundle declares one
//     `connector` input injected into its functions (no oauth2 trait, no
//     manifest `oauth2:` block, and the
//     pasted accessToken is the config kind's own secret-typed property), the account type
//     wears accountconfig WITHOUT oauth2 (its trait-contracted tokenRef/
//     tokenStatus/grantedScopes admit as dormant declarations, and the
//     resumable-walk state backfillResume is connector-written), the message
//     mirror carries its required room edge, every mirror property carries a
//     displayName (fleet F7), the sync declares the room-mirror read the
//     filter fallback needs, and NO recordmapping registers.
//
//  2. TestBeeperBundleInstallsAndSyncs — the whole closure installs into a
//     live repository and the sync RUNS against a fake homeserver (httptest):
//     the initial sync of MORE rooms than one 25-room frame holds drains
//     PAGED (fleet F6 / review P0 — rooms are the paged units, messages
//     arrive one /messages page per invocation) and every room + message
//     lands; a second live account is stamped ignored and never synced
//     (F8); a partial incremental delta patches only the fields it carried
//     — the stored name and network survive — while the
//     roomFilter falls back to the stored mirror to keep a matching room
//     syncing and drop a non-matching one; a provider failure stamps
//     `syncStatus: erroring: <reason>` instead of parking (F2/F3). Warms
//     the PEP 723 body through uv, so it SKIPS when uv is absent.
//
//  3. TestBeeperBundleBackfillCapResumes — a room deeper than the per-drain
//     page cap does NOT lose history silently (Codex H5): the drain stamps
//     `ok (1 room backfill pending)` with the resume token recorded in
//     backfillResume, and the next run resumes the walk from EXACTLY that
//     token and drains the room to completion.
//
// No live Beeper/Matrix call ever runs in a test.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/runner/substratefn"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	beeperExampleDir  = "../../kinds/beeper.bundles.substrate.reamde.dev"
	beeperAuthority   = "beeper.bundles.substrate.reamde.dev"
	beeperBundleRow   = beeperAuthority + "/beeper"
	beeperConfigType  = beeperAuthority + "/config"
	beeperAccountType = beeperAuthority + "/account"
	beeperRoomType    = beeperAuthority + "/room"
	beeperMessageType = beeperAuthority + "/message"
	beeperSyncFn      = beeperAuthority + "/messagessync"

	beeperFakeToken = "syt_test_beeper_access_token_1"

	// More rooms than one 25-room frame holds, so the initial sync MUST page:
	// a drain that dropped the handed-off tail would lose rooms
	// 26..30 and fail the per-room assertions below.
	beeperRoomsTotal = 30

	// The F8 stamp, verbatim from the body.
	beeperIgnoredStatus = "ignored: duplicate account (this provider is " +
		"one-account-per-repository until issue 011)"
)

// TestBeeperBundleAdmitsSchema loads the builtin schema, then installs the
// bundle closure on top of it through the ordinary loader/resolver — the same
// admission the batch apply runs, minus the function-body warm. Every
// assertion is a rule the loader enforces at admission, aimed at the shapes
// that make this bundle the non-OAuth one.
func TestBeeperBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	// The registry an install actually admits into: the seeded tree (core
	// alone) plus the shipped VOCABULARY bundles this repository imported —
	// what a closure declaring onto people/tasks/messaging/calendar/media
	// needs present, and what `requires:` names.
	reg, err := enginetest.SeededRegistry("../../kinds/core.substrate.reamde.dev")
	if err != nil {
		t.Fatalf("build the repository registry: %v", err)
	}
	data, err := os.ReadFile(beeperExampleDir + "/bundle.yaml")
	if err != nil {
		t.Fatalf("read bundle.yaml: %v", err)
	}
	docs, err := vocabulary.ParseStream(data)
	if err != nil {
		t.Fatalf("parse bundle.yaml: %v", err)
	}
	authorities, err := vocabulary.BuildAuthorities(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("build the bundle authority: %v", err)
	}
	if err := reg.InstallAll(authorities); err != nil {
		t.Fatalf("the bundle closure did not admit: %v", err)
	}

	// The bundle exists, declares the one `connector` input injected into its
	// functions, and carries NO oauth2 manifest block — a Matrix access token
	// is pasted, never exchanged.
	b, ok := reg.BundleOf(beeperAuthority)
	if !ok {
		t.Fatalf("no bundle owns %s after install", beeperAuthority)
	}
	in, ok := b.Inputs["connector"]
	if !ok {
		t.Fatalf("bundle declares no connector input: %v", b.InputOrder)
	}
	if in.Kind != beeperConfigType {
		t.Fatalf("connector input kind = %q, want %q", in.Kind, beeperConfigType)
	}
	if in.Inject != vocabulary.BundleInputInjectFunctions {
		t.Fatalf("connector input inject = %q, want %q", in.Inject, vocabulary.BundleInputInjectFunctions)
	}
	if b.OAuth2 != nil {
		t.Fatalf("bundle carries an oauth2 manifest block — Beeper is not OAuth")
	}

	// The config type: no oauth2 trait. That trait would both demand a
	// manifest block and mark the client secret facility-owned; the pasted
	// token works BECAUSE this kind stays a plain connector config whose
	// secret injects usable.
	cfg, ok := reg.ByIdentity(beeperConfigType)
	if !ok {
		t.Fatalf("config type %s missing", beeperConfigType)
	}
	if cfg.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("config type implements oauth2 — there is no OAuth client to declare")
	}
	if p, ok := cfg.Prop("accessToken"); !ok || !p.Secret() {
		t.Fatalf("config accessToken is not a declared secret property (ok=%v)", ok)
	}

	// The account type: accountconfig WITHOUT oauth2. The trait's contracted
	// properties (tokenRef/tokenStatus/grantedScopes) admit as dormant
	// declarations, and the token does NOT live here — an accountconfig-trait
	// secret seals at rest and injects as ciphertext.
	acct, ok := reg.ByIdentity(beeperAccountType)
	if !ok {
		t.Fatalf("account type %s missing", beeperAccountType)
	}
	if !acct.Implements(vocabulary.TraitAccountConfigCore) {
		t.Fatalf("account type does not implement %s", vocabulary.TraitAccountConfigCore)
	}
	if acct.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("account type implements oauth2 — the non-OAuth shape is the point")
	}
	if _, ok := acct.Prop("accessToken"); ok {
		t.Fatalf("account declares accessToken — the pasted token belongs on the connector's config record")
	}
	for _, dormant := range []string{"tokenRef", "tokenStatus", "grantedScopes"} {
		if _, ok := acct.Prop(dormant); !ok {
			t.Fatalf("account misses the accountconfig trait contract property %s", dormant)
		}
	}
	// The resumable-walk state a capped drain records (Codex H5) is
	// connector-written like the other sync stamps.
	if p, ok := acct.Prop("backfillResume"); !ok || p.Writer != vocabulary.WriterConnector {
		t.Fatalf("account backfillResume is not a connector-written property (ok=%v)", ok)
	}

	// F7: every mirror property carries a displayName — the console renders
	// labels, never raw camelCase keys.
	for _, tid := range []string{beeperRoomType, beeperMessageType} {
		ty, ok := reg.ByIdentity(tid)
		if !ok {
			t.Fatalf("mirror type %s missing", tid)
		}
		for name, p := range ty.Props {
			if p.DisplayName == "" {
				t.Fatalf("%s.%s has no displayName (F7)", tid, name)
			}
		}
	}

	// The message mirror hangs off its room: required, single, ownerRef.
	msg, ok := reg.ByIdentity(beeperMessageType)
	if !ok {
		t.Fatalf("message type %s missing", beeperMessageType)
	}
	ed, ok := msg.Edge("room")
	if !ok {
		t.Fatalf("message declares no `room` edge")
	}
	if ed.To != beeperRoomType || !ed.Required || ed.Many {
		t.Fatalf("room edge shape wrong: to=%q required=%v many=%v", ed.To, ed.Required, ed.Many)
	}

	// NO mapping registers from either mirror type — bridged-sender identity
	// (ghost ids like @whatsapp_…) is a deliberate later slice, and
	// conversationmessage's required conversation/author edges refuse a
	// property-only mint (see the bundle README).
	if _, ok := reg.MappingFor(beeperMessageType); ok {
		t.Fatalf("a mapping registered from %s — this slice ships none", beeperMessageType)
	}
	if _, ok := reg.MappingFor(beeperRoomType); ok {
		t.Fatalf("a mapping registered from %s — this slice ships none", beeperRoomType)
	}

	// The sync function registers and declares the room-mirror read: the
	// roomFilter's fallback surface when a delta omits the fields (P1), and
	// the resumed-walk existence check.
	fn, err := reg.ResolveFunction(beeperSyncFn)
	if err != nil {
		t.Fatalf("sync function %s did not register: %v", beeperSyncFn, err)
	}
	if fn.Caps.Reads == nil {
		t.Fatalf("sync function declares no reads — the filter fallback needs the room mirror")
	}
	var readsRoom bool
	for _, ty := range fn.Caps.Reads.Kinds {
		readsRoom = readsRoom || ty == beeperRoomType
	}
	if !readsRoom {
		t.Fatalf("sync reads %v, want %s among them", fn.Caps.Reads.Kinds, beeperRoomType)
	}
}

// ---- the fake homeserver ----------------------------------------------------

func beeperRoomID(i int) string {
	return fmt.Sprintf("!r%02d:beeper.local", i)
}

func beeperMsgEvent(id, sender, body string, ts int64) map[string]any {
	return map[string]any{
		"type": "m.room.message", "event_id": id, "sender": sender,
		"origin_server_ts": ts,
		"content":          map[string]any{"msgtype": "m.text", "body": body},
	}
}

func beeperWriteJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode fake response: %v", err)
	}
}

func beeperRequireToken(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+beeperFakeToken {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errcode":"M_UNKNOWN_TOKEN"}`))
		return false
	}
	return true
}

// beeperRoomOfPath extracts the room id from /_matrix/client/v3/rooms/{id}/messages
// (http.Request.URL.Path arrives percent-decoded).
func beeperRoomOfPath(path string) string {
	rest := strings.TrimPrefix(path, "/_matrix/client/v3/rooms/")
	return strings.TrimSuffix(rest, "/messages")
}

// beeperFakeHS drives the main flow: an initial /sync of beeperRoomsTotal
// rooms (every room's messages then arrive through one /messages page), a
// partial incremental delta that OMITS names and carries only owner-sent
// events, and a switchable hard-failure mode (F3). Every request
// must present the pasted token — the body's one credential.
type beeperFakeHS struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	fail     bool
	msgFroms map[string][]string // room id -> the /messages `from` tokens seen
}

func newBeeperFakeHS(t *testing.T) *beeperFakeHS {
	t.Helper()
	f := &beeperFakeHS{t: t, msgFroms: map[string][]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/_matrix/client/v3/sync", f.handleSync)
	mux.HandleFunc("/_matrix/client/v3/rooms/", f.handleMessages)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *beeperFakeHS) setFail(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = v
}

func (f *beeperFakeHS) ghost(i int) string {
	if i == 1 {
		return "@telegram_7770001:beeper.local"
	}
	return "@whatsapp_15551234567:beeper.local"
}

func (f *beeperFakeHS) handleSync(w http.ResponseWriter, r *http.Request) {
	if !beeperRequireToken(w, r) {
		return
	}
	f.mu.Lock()
	fail := f.fail
	f.mu.Unlock()
	if fail {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errcode":"M_UNKNOWN"}`))
		return
	}
	rooms := map[string]any{}
	next := "s1"
	switch r.URL.Query().Get("since") {
	case "":
		// The initial sync: every joined room, named state + one ghost-sent
		// timeline event (the bridge marker the body reads the network off —
		// it stages NOTHING from this window).
		for i := range beeperRoomsTotal {
			rooms[beeperRoomID(i)] = map[string]any{
				"state": map[string]any{"events": []any{map[string]any{
					"type": "m.room.name", "event_id": fmt.Sprintf("$st%02d", i),
					"sender":  f.ghost(i),
					"content": map[string]any{"name": fmt.Sprintf("Room %02d", i)},
				}}},
				"timeline": map[string]any{
					"limited": false, "prev_batch": "pb",
					"events": []any{beeperMsgEvent(fmt.Sprintf("$tl%02d", i),
						f.ghost(i), "marker", time.Now().Add(-time.Hour).UnixMilli())},
				},
			}
		}
	case "s1":
		// The partial delta (P1): both rooms arrive WITHOUT name state and
		// with only owner-sent events — no bridge ghost, no name. A whole-put
		// (or a filter read off the delta alone) would erase names, reset
		// networks, and drop both rooms.
		ownerTS := time.Now().Add(-10 * time.Minute).UnixMilli()
		rooms[beeperRoomID(0)] = map[string]any{
			"timeline": map[string]any{"limited": false, "prev_batch": "pb", "events": []any{
				beeperMsgEvent("$delta1", "@geoah:beeper.com", "a fresh reply", ownerTS),
			}},
			"summary": map[string]any{"m.joined_member_count": 3},
		}
		rooms[beeperRoomID(1)] = map[string]any{
			"timeline": map[string]any{"limited": false, "prev_batch": "pb", "events": []any{
				beeperMsgEvent("$delta2", "@geoah:beeper.com", "should be filtered", ownerTS),
			}},
		}
		next = "s2"
	default:
		next = "s3"
	}
	beeperWriteJSON(f.t, w, map[string]any{
		"next_batch": next,
		"rooms":      map[string]any{"join": rooms},
	})
}

func (f *beeperFakeHS) handleMessages(w http.ResponseWriter, r *http.Request) {
	if !beeperRequireToken(w, r) {
		return
	}
	rid := beeperRoomOfPath(r.URL.Path)
	from := r.URL.Query().Get("from")
	f.mu.Lock()
	f.msgFroms[rid] = append(f.msgFroms[rid], from)
	f.mu.Unlock()
	chunk := []any{}
	switch from {
	case "s1":
		// The initial history walk, one page per room: the seed message.
		var i int
		if _, err := fmt.Sscanf(rid, "!r%02d:beeper.local", &i); err == nil {
			chunk = append(chunk, beeperMsgEvent(fmt.Sprintf("$seed%02d", i),
				f.ghost(i), fmt.Sprintf("hello from room %02d", i),
				time.Now().Add(-time.Hour).UnixMilli()))
		}
	case "s2":
		// The delta walk: only the room the filter kept should ever ask.
		ownerTS := time.Now().Add(-10 * time.Minute).UnixMilli()
		if rid == beeperRoomID(0) {
			chunk = append(chunk, beeperMsgEvent("$delta1", "@geoah:beeper.com",
				"a fresh reply", ownerTS))
		}
		if rid == beeperRoomID(1) {
			chunk = append(chunk, beeperMsgEvent("$delta2", "@geoah:beeper.com",
				"should be filtered", ownerTS))
		}
	}
	// No `end` token: one page drains each room's history.
	beeperWriteJSON(f.t, w, map[string]any{"chunk": chunk})
}

// ---- shared drivers ----------------------------------------------------------

// beeperInstall applies the shipped closure + triggers into the repository,
// skipping when uv cannot warm the PEP 723 body.
func beeperInstall(t *testing.T, ds *dataset) {
	t.Helper()
	ctx := context.Background()
	vocabularyDocs := loadYAMLDocs(t, beeperExampleDir+"/bundle.yaml")
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, vocabularyDocs); err != nil {
		if isUVProvisionError(err) {
			t.Skipf("bundle install could not warm the PEP 723 body (uv offline?): %v", err)
		}
		t.Fatalf("install the beeper bundle: %v", err)
	}
	for _, m := range loadYAMLDocs(t, beeperExampleDir+"/triggers.yaml") {
		putDataDoc(t, ds, m)
	}
}

// beeperResync clears the account's completion marker under a DIFFERENT
// function actor's dispatch context (writer: connector admits any
// bundle-tier write context — ticket 002: the tier is dispatch data, so
// the test stamps it the way runCallable's effect apply does; the trigger's
// self-actor exclusion only screens the sync's own stamps), so the
// on-connect trigger re-fires and the next drain runs incrementally off the
// stored syncToken.
func beeperResync(t *testing.T, ds *dataset, accountID string) {
	t.Helper()
	actor := substrate.Actor("function.testresync." + beeperAuthority)
	if err := ds.inTx(context.Background(), actor, false, func(tx *txn) error {
		tx.tier = substrate.TierBundle // the dispatch write context
		_, err := tx.patch(eref{Kind: beeperAccountType, ID: accountID}, substrate.PatchInput{
			Properties: map[string]any{"lastSyncedAt": nil},
		})
		return err
	}); err != nil {
		t.Fatalf("clear lastSyncedAt: %v", err)
	}
	drainTriggers(t, ds)
}

func beeperCountByAccount(t *testing.T, ds *dataset, typ, accountID string) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM records
		 WHERE kind = $1 AND deleted_at IS NULL AND props->>'account' = $2`,
		typ, accountID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func beeperMaxRunPages(t *testing.T, ds *dataset) int {
	t.Helper()
	var pages int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT COALESCE(MAX((props->>'pages')::int), 0) FROM records
		 WHERE kind = $1 AND deleted_at IS NULL`, typeRun).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	return pages
}

// TestBeeperBundleInstallsAndSyncs applies the whole closure into a live
// repository, then drives the delivery mechanism end to end against the fake
// homeserver: the PAGED initial sync (more rooms than one frame holds), the
// duplicate-account stamp, the partial-delta patch + mirror-fallback filter,
// and the erroring stamp. It warms the PEP 723 sync body through uv, so it
// skips when uv is absent or cannot provision.
func TestBeeperBundleInstallsAndSyncs(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the sync body warms through uv at install")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	beeperInstall(t, ds)

	// The bundle row and every schema member landed as its own record.
	for id, wantType := range map[string]string{
		beeperBundleRow:   "core.substrate.reamde.dev/bundle",
		beeperConfigType:  "core.substrate.reamde.dev/kind",
		beeperAccountType: "core.substrate.reamde.dev/kind",
		beeperRoomType:    "core.substrate.reamde.dev/kind",
		beeperMessageType: "core.substrate.reamde.dev/kind",
		beeperSyncFn:      "core.substrate.reamde.dev/function",
	} {
		row, err := ds.Get(ctx, wantType, id)
		if err != nil {
			t.Fatalf("member %s did not install: %v", id, err)
		}
		if row.Kind != wantType {
			t.Fatalf("member %s is a %s, want %s", id, row.Kind, wantType)
		}
	}

	// Computed status: installed, enabled, the connector input unresolved
	// (surfaced as a missing setup item) until a config row exists, one
	// function.
	st, err := ds.BundleStatus(ctx, beeperAuthority)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if !st.Installed || !st.Enabled {
		t.Fatalf("bundle not live: installed=%v enabled=%v", st.Installed, st.Enabled)
	}
	if len(st.Inputs) != 1 || st.Inputs[0].Name != "connector" || st.Inputs[0].Kind != beeperConfigType {
		t.Fatalf("status inputs = %+v, want the one connector input", st.Inputs)
	}
	if st.Inputs[0].Record != "" || st.Inputs[0].Via != "" {
		t.Fatalf("connector input resolved with no config record created: %+v", st.Inputs[0])
	}
	if len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupMissing || st.Setup[0].Input != "connector" {
		t.Fatalf("status setup = %+v, want the one missing-input item", st.Setup)
	}
	if st.Functions != 1 {
		t.Fatalf("status functions = %d, want 1", st.Functions)
	}
	for _, id := range []string{"beeper-messages-on-connect", "beeper-messages-scheduled"} {
		row, err := ds.Get(ctx, typeTrigger, id)
		if err != nil {
			t.Fatalf("trigger %s did not install: %v", id, err)
		}
		if row.Kind != typeTrigger {
			t.Fatalf("trigger %s is a %s", id, row.Kind)
		}
	}

	// Configure against the fake homeserver (loopback — the pinning rule's
	// test seam): the connector's config record carries the pasted token, and
	// the account carries only the owner's toggles.
	f := newBeeperFakeHS(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: beeperConfigType, ID: "beeper-config",
		Properties: map[string]any{
			"homeserverUrl": f.srv.URL,
			"accessToken":   beeperFakeToken,
		},
	}); err != nil {
		t.Fatalf("put config: %v", err)
	}
	// syncFrequency off keeps the hourly schedule inert, so every drain in
	// this test is driven deterministically by the on-connect trigger (whose
	// record path deliberately ignores cadence) — a schedule fire racing the
	// on-connect delivery would double-sync the fixtures.
	const acctID = "beeper-acct-1"
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: beeperAccountType, ID: acctID,
		Properties: map[string]any{
			"userId":          "@geoah:beeper.com",
			"enabledMessages": true,
			"syncFrequency":   "off",
			"backfillDepth":   "last30d",
		},
	}); err != nil {
		t.Fatalf("put account: %v", err)
	}

	// The account create satisfies the on-connect guard — drain the delivery
	// to quiescence. The initial sync spans 30 rooms: one /sync page (staging
	// nothing), TWO 25-room mirror pages, and one /messages page per room,
	// all one chain off the causal path.
	drainTriggers(t, ds)

	// PAGED (P0): every room landed — including the 26th..30th, which only a
	// working host.page.more handoff reaches — with names, the ghost-derived
	// network, and the account stamp.
	for i := range beeperRoomsTotal {
		room, err := ds.Get(ctx, beeperRoomType, substratefn.ExternalID("beeper", acctID, beeperRoomID(i)))
		if err != nil {
			t.Fatalf("room %02d mirror missing (did the rooms page hand off?): %v", i, err)
		}
		if got := room.Properties["name"]; got != fmt.Sprintf("Room %02d", i) {
			t.Fatalf("room %02d name = %v", i, got)
		}
		wantNet := "whatsapp"
		if i == 1 {
			wantNet = "telegram"
		}
		if got := room.Properties["network"]; got != wantNet {
			t.Fatalf("room %02d network = %v, want %s", i, got, wantNet)
		}
		if got := room.Properties["account"]; got != acctID {
			t.Fatalf("room %02d account = %v", i, got)
		}
	}
	if n := beeperCountByAccount(t, ds, beeperMessageType, acctID); n != beeperRoomsTotal {
		t.Fatalf("message mirrors = %d, want %d (one seed per room)", n, beeperRoomsTotal)
	}
	msg, err := ds.Get(ctx, beeperMessageType, substratefn.ExternalID("beeper", acctID, "$seed07"))
	if err != nil {
		t.Fatalf("seed message mirror missing: %v", err)
	}
	if got := msg.Properties["text"]; got != "hello from room 07" {
		t.Fatalf("message text = %v", got)
	}
	if got := msg.Properties["sender"]; got != "@whatsapp_15551234567:beeper.local" {
		t.Fatalf("message sender = %v", got)
	}
	if got, _ := msg.Properties["sentAt"].(string); got == "" {
		t.Fatalf("message sentAt not set")
	}
	// The chain really was multi-page: the run ledger records the page count.
	if pages := beeperMaxRunPages(t, ds); pages < 4 {
		t.Fatalf("on-connect drain ran %d pages, want >= 4 — the initial sync is not paging", pages)
	}

	// The account stamped AFTER its messages: token, completion marker, clean
	// status, and NO pending backfill (every room drained in one page).
	acct, err := ds.Get(ctx, beeperAccountType, acctID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got := acct.Properties["syncToken"]; got != "s1" {
		t.Fatalf("account syncToken = %v, want s1", got)
	}
	if got := acct.Properties["syncStatus"]; got != "ok" {
		t.Fatalf("account syncStatus = %v, want ok", got)
	}
	if got, _ := acct.Properties["lastSyncedAt"].(string); got == "" {
		t.Fatalf("account lastSyncedAt not stamped")
	}
	if m, ok := acct.Properties["backfillResume"].(map[string]any); ok && len(m) > 0 {
		t.Fatalf("backfillResume = %v, want empty — nothing was capped", m)
	}

	// F8: a second live account is stamped ignored and NEVER synced — no
	// second /sync, no rooms bearing its id, no completion marker.
	const dupID = "beeper-acct-2"
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: beeperAccountType, ID: dupID,
		Properties: map[string]any{
			"userId":          "@second:beeper.com",
			"enabledMessages": true,
			"syncFrequency":   "off",
			"backfillDepth":   "last30d",
		},
	}); err != nil {
		t.Fatalf("put duplicate account: %v", err)
	}
	drainTriggers(t, ds)
	dup, err := ds.Get(ctx, beeperAccountType, dupID)
	if err != nil {
		t.Fatalf("get duplicate account: %v", err)
	}
	if got := dup.Properties["syncStatus"]; got != beeperIgnoredStatus {
		t.Fatalf("duplicate syncStatus = %v, want %q", got, beeperIgnoredStatus)
	}
	if _, ok := dup.Properties["syncToken"]; ok {
		t.Fatalf("duplicate account carries a syncToken — it synced")
	}
	if _, ok := dup.Properties["lastSyncedAt"]; ok {
		t.Fatalf("duplicate account carries lastSyncedAt — it synced")
	}
	if n := beeperCountByAccount(t, ds, beeperRoomType, dupID); n != 0 {
		t.Fatalf("%d rooms mirrored for the duplicate account, want 0", n)
	}

	// P1: a partial incremental delta — no name state, owner-only events —
	// must patch, not whole-put, and the roomFilter must judge rooms by the
	// STORED mirror when the delta omits the fields. Filter "whatsapp": room
	// 00 (stored network whatsapp) keeps syncing, room 01 (telegram) drops.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, beeperAccountType, acctID, substrate.PatchInput{
		Properties: map[string]any{"roomFilter": "whatsapp"},
	}); err != nil {
		t.Fatalf("set roomFilter: %v", err)
	}
	beeperResync(t, ds, acctID)

	room0, err := ds.Get(ctx, beeperRoomType, substratefn.ExternalID("beeper", acctID, beeperRoomID(0)))
	if err != nil {
		t.Fatalf("room 00 gone after the delta: %v", err)
	}
	if got := room0.Properties["name"]; got != "Room 00" {
		t.Fatalf("the delta erased room 00's name: %v (whole-put instead of patch?)", got)
	}
	if got := room0.Properties["network"]; got != "whatsapp" {
		t.Fatalf("the delta reset room 00's network: %v", got)
	}
	raw, _ := room0.Properties["raw"].(map[string]any)
	if raw["m.joined_member_count"] == nil {
		t.Fatalf("room 00 raw not patched from the delta summary: %v — the filter "+
			"dropped a matching room instead of reading the stored mirror", room0.Properties["raw"])
	}
	if _, err := ds.Get(ctx, beeperMessageType, substratefn.ExternalID("beeper", acctID, "$delta1")); err != nil {
		t.Fatalf("the delta message did not mirror: %v", err)
	}
	room1, err := ds.Get(ctx, beeperRoomType, substratefn.ExternalID("beeper", acctID, beeperRoomID(1)))
	if err != nil {
		t.Fatalf("room 01 gone after the delta: %v", err)
	}
	if got := room1.Properties["name"]; got != "Room 01" {
		t.Fatalf("room 01 name = %v", got)
	}
	if got := room1.Properties["network"]; got != "telegram" {
		t.Fatalf("room 01 network = %v", got)
	}
	if _, err := ds.Get(ctx, beeperMessageType, substratefn.ExternalID("beeper", acctID, "$delta2")); err == nil {
		t.Fatalf("$delta2 mirrored — the filter passed a room whose stored network does not match")
	}
	acct, err = ds.Get(ctx, beeperAccountType, acctID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got := acct.Properties["syncToken"]; got != "s2" {
		t.Fatalf("account syncToken = %v, want s2", got)
	}
	if got := acct.Properties["syncStatus"]; got != "ok" {
		t.Fatalf("account syncStatus = %v, want ok", got)
	}

	// F2/F3: a provider failure stamps `erroring: <reason>` on the account —
	// written, bounded, delivery completed — instead of freezing the status
	// at its last value while the chain parks.
	f.setFail(true)
	beeperResync(t, ds, acctID)
	acct, err = ds.Get(ctx, beeperAccountType, acctID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	status, _ := acct.Properties["syncStatus"].(string)
	if !strings.HasPrefix(status, "erroring: ") {
		t.Fatalf("account syncStatus = %q, want an erroring: <reason> stamp", status)
	}
	if got := acct.Properties["syncToken"]; got != "s2" {
		t.Fatalf("a failed /sync moved the token: %v", got)
	}
	if _, ok := acct.Properties["lastSyncedAt"]; ok {
		t.Fatalf("an erroring run stamped lastSyncedAt — the window floor advanced past unseen records")
	}
}

// ---- the backfill-cap fake ---------------------------------------------------

// beeperCapFake serves ONE room whose history outlasts the body's per-drain
// page cap: every /messages page returns three fresh events and another end
// token, until finish() flips it to a drained history.
type beeperCapFake struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	finished bool
	pageSeq  int
	froms    []string
}

const beeperCapRoomID = "!cap:beeper.local"

func newBeeperCapFake(t *testing.T) *beeperCapFake {
	t.Helper()
	f := &beeperCapFake{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("/_matrix/client/v3/sync", func(w http.ResponseWriter, r *http.Request) {
		if !beeperRequireToken(w, r) {
			return
		}
		rooms := map[string]any{}
		next := "s1"
		if r.URL.Query().Get("since") == "" {
			rooms[beeperCapRoomID] = map[string]any{
				"state": map[string]any{"events": []any{map[string]any{
					"type": "m.room.name", "event_id": "$stcap",
					"sender":  "@whatsapp_15551234567:beeper.local",
					"content": map[string]any{"name": "Cap room"},
				}}},
				"timeline": map[string]any{
					"limited": true, "prev_batch": "pb",
					"events": []any{beeperMsgEvent("$capmarker",
						"@whatsapp_15551234567:beeper.local", "marker",
						time.Now().Add(-time.Hour).UnixMilli())},
				},
			}
		} else {
			next = "s2"
		}
		beeperWriteJSON(f.t, w, map[string]any{
			"next_batch": next,
			"rooms":      map[string]any{"join": rooms},
		})
	})
	mux.HandleFunc("/_matrix/client/v3/rooms/", func(w http.ResponseWriter, r *http.Request) {
		if !beeperRequireToken(w, r) {
			return
		}
		from := r.URL.Query().Get("from")
		f.mu.Lock()
		f.froms = append(f.froms, from)
		finished := f.finished
		var page int
		if !finished {
			f.pageSeq++
			page = f.pageSeq
		}
		f.mu.Unlock()
		if finished {
			// The history's end: an empty chunk drains the walk.
			beeperWriteJSON(f.t, w, map[string]any{"chunk": []any{}})
			return
		}
		ts := time.Now().Add(-time.Hour).UnixMilli()
		chunk := []any{}
		for j := range 3 {
			chunk = append(chunk, beeperMsgEvent(
				fmt.Sprintf("$cap-%d-%d", page, j),
				"@whatsapp_15551234567:beeper.local",
				fmt.Sprintf("history %d-%d", page, j), ts))
		}
		beeperWriteJSON(f.t, w, map[string]any{
			"chunk": chunk, "end": fmt.Sprintf("end-%d", page),
		})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *beeperCapFake) finish() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished = true
}

func (f *beeperCapFake) lastFrom() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.froms) == 0 {
		return ""
	}
	return f.froms[len(f.froms)-1]
}

// TestBeeperBundleBackfillCapResumes proves a room deeper than the per-drain
// page cap loses nothing silently (Codex H5): the capped drain stamps a
// disclosed `ok (1 room backfill pending)` with the resume token in
// backfillResume — the syncToken advances only WITH that record — and the
// next run resumes the walk from exactly that token, drains the room, and
// clears the pending state.
func TestBeeperBundleBackfillCapResumes(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the sync body warms through uv at install")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	beeperInstall(t, ds)

	f := newBeeperCapFake(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: beeperConfigType, ID: "beeper-config",
		Properties: map[string]any{
			"homeserverUrl": f.srv.URL,
			"accessToken":   beeperFakeToken,
		},
	}); err != nil {
		t.Fatalf("put config: %v", err)
	}
	// syncFrequency off: the on-connect trigger (and beeperResync) drive
	// every drain, so the schedule cannot race in a second walk.
	const acctID = "beeper-cap-1"
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: beeperAccountType, ID: acctID,
		Properties: map[string]any{
			"userId":          "@geoah:beeper.com",
			"enabledMessages": true,
			"syncFrequency":   "off",
			"backfillDepth":   "last30d",
		},
	}); err != nil {
		t.Fatalf("put account: %v", err)
	}
	drainTriggers(t, ds)

	// The drain walked exactly the cap (20 pages × 3 events), then RECORDED
	// where it stopped instead of silently abandoning the tail.
	if n := beeperCountByAccount(t, ds, beeperMessageType, acctID); n != 60 {
		t.Fatalf("capped drain mirrored %d messages, want 60 (20 pages of 3)", n)
	}
	acct, err := ds.Get(ctx, beeperAccountType, acctID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got := acct.Properties["syncStatus"]; got != "ok (1 room backfill pending)" {
		t.Fatalf("account syncStatus = %v, want the disclosed pending stamp", got)
	}
	if got := acct.Properties["syncToken"]; got != "s1" {
		t.Fatalf("account syncToken = %v, want s1", got)
	}
	resume, _ := acct.Properties["backfillResume"].(map[string]any)
	st, _ := resume[beeperCapRoomID].(map[string]any)
	if st == nil {
		t.Fatalf("backfillResume carries no entry for the capped room: %v", resume)
	}
	if got := st["from"]; got != "end-20" {
		t.Fatalf("backfillResume from = %v, want end-20 (the cap page's end token)", got)
	}

	// The next run RESUMES from exactly the recorded token, finds the
	// history's end, and clears the pending state.
	f.finish()
	beeperResync(t, ds, acctID)
	if got := f.lastFrom(); got != "end-20" {
		t.Fatalf("the resumed walk started from %q, want the recorded end-20", got)
	}
	acct, err = ds.Get(ctx, beeperAccountType, acctID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got := acct.Properties["syncStatus"]; got != "ok" {
		t.Fatalf("account syncStatus = %v, want ok after the resumed drain", got)
	}
	if got := acct.Properties["syncToken"]; got != "s2" {
		t.Fatalf("account syncToken = %v, want s2", got)
	}
	if m, ok := acct.Properties["backfillResume"].(map[string]any); ok && len(m) > 0 {
		t.Fatalf("backfillResume = %v, want cleared after the resumed drain", m)
	}
}
