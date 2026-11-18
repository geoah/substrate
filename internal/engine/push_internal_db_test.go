package engine

// Push (docs/plans/push.md): a device is its token's own record and goes
// with it; the signer of a token's pushes is derived from the repository's
// DEK and the token's id; the engine hands its sink what happened, and
// answers the dispatcher's rechecks and the badge's candidates.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/pushdispatch"
	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// deviceProps is a device put's body, as the app writes it.
func deviceProps(key string, notifies ...any) map[string]any {
	p := map[string]any{"name": "iPhone", "platform": "ios", propDevicePushKey: key}
	if notifies != nil {
		p[propDeviceNotifies] = notifies
	}
	return p
}

// tokenCtx is a request of identity's person carrying the token tokenID.
func (rr *rolesRepository) tokenCtx(identity, tokenID string) context.Context {
	return substrate.WithPrincipal(substrate.WithOnBehalfOf(rr.as(identity), substrate.ActorPerson), tokenID)
}

// mint is a token of identity's person, with an optional expiry.
func (rr *rolesRepository) mint(t *testing.T, identity string, expires *time.Time) substrate.TokenInfo {
	t.Helper()
	info, _, err := rr.ds.MintToken(rr.as(identity), substrate.MintTokenInput{Label: "phone", ExpiresAt: expires, Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func (rr *rolesRepository) putDevice(ctx context.Context, id string, props map[string]any) (*substrate.Record, error) {
	return rr.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: typeDevice, ID: id, Properties: props})
}

func deviceRow(t *testing.T, ds *dataset, id string) *erow {
	t.Helper()
	row, err := ds.loadRowDB(context.Background(), eref{Kind: typeDevice, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// A device is written only at the id of the token the request carries, its
// `token` and `person` are filled and fixed, `seenAt` is stamped, `notifies`
// is required on the create and merged after it, and a viewer writes their
// own.
func TestADeviceIsWrittenOnlyAtItsOwnTokensID(t *testing.T) {
	t.Parallel()
	rr := newRolesRepository(t)
	ada := rr.mint(t, rr.adaID, nil)
	other := rr.mint(t, rr.adaID, nil)
	ctx := rr.tokenCtx(rr.adaID, ada.ID)

	if _, err := rr.putDevice(ctx, ada.ID, deviceProps("v1.key")); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a create without notifies: %v, want a validation refusal", err)
	}
	if _, err := rr.putDevice(ctx, ada.ID, map[string]any{"name": "iPhone", "platform": "ios", propDevicePushKey: "v1.key", propDeviceNotifies: []any{}}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a create with notifies []: %v, want a validation refusal", err)
	}
	props := deviceProps("v1.key", "replies", "requests")
	props[propDeviceSeenAt] = "2001-01-01T00:00:00Z"
	rec, err := rr.putDevice(ctx, ada.ID, props)
	if err != nil {
		t.Fatalf("the device's create: %v", err)
	}
	if rec.ID != ada.ID {
		t.Fatalf("the device is %s, want the token's id %s", rec.ID, ada.ID)
	}
	row := deviceRow(t, rr.ds, ada.ID)
	if got := referenceTargetOf(row, propDeviceToken); got != (eref{Kind: kindToken, ID: ada.ID}) {
		t.Fatalf("token = %v, want the request's", got)
	}
	if got := referenceTargetOf(row, propDevicePerson); got != (eref{Kind: kindActor, ID: actorID(substrate.ActorPerson)}) {
		t.Fatalf("person = %v, want the writer's actor", got)
	}
	seen, _ := row.Props[propDeviceSeenAt].(string)
	if seen == "" || strings.HasPrefix(seen, "2001") {
		t.Fatalf("seenAt = %q, want the engine's stamp", seen)
	}

	// An update merges: notifies left out keeps the stored value, [] is
	// refused, a new push key lands and seenAt moves.
	again, err := rr.putDevice(ctx, ada.ID, map[string]any{"name": "iPhone", "platform": "ios", propDevicePushKey: "v1.newer"})
	if err != nil {
		t.Fatalf("an update without notifies: %v", err)
	}
	if again.Version <= rec.Version {
		t.Fatalf("the update left the version at %d", again.Version)
	}
	row = deviceRow(t, rr.ds, ada.ID)
	if n, _ := row.Props[propDeviceNotifies].([]any); len(n) != 2 || row.Props[propDevicePushKey] != "v1.newer" {
		t.Fatalf("after the update: %v", row.Props)
	}
	if _, err := rr.putDevice(ctx, ada.ID, map[string]any{propDeviceNotifies: []any{}}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("an update emptying notifies: %v, want a validation refusal", err)
	}

	// Another token's id, or a token or a person not the request's.
	if _, err := rr.putDevice(ctx, other.ID, deviceProps("v1.key", "replies")); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a device at another token's id: %v, want forbidden", err)
	}
	named := deviceProps("v1.key", "replies")
	named[propDeviceToken] = vocabulary.RecordPath(kindToken, other.ID)
	if _, err := rr.putDevice(ctx, ada.ID, named); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a device naming another token: %v, want forbidden", err)
	}
	named = deviceProps("v1.key", "replies")
	named[propDevicePerson] = vocabulary.RecordPath(kindActor, actorID(substrate.PersonActor(rr.boID)))
	if _, err := rr.putDevice(ctx, ada.ID, named); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a device naming another person: %v, want forbidden", err)
	}
	// Naming its own token and person is the same write.
	named = deviceProps("v1.key", "replies")
	named[propDeviceToken] = vocabulary.RecordPath(kindToken, ada.ID)
	if _, err := rr.putDevice(ctx, ada.ID, named); err != nil {
		t.Fatalf("a device naming its own token: %v", err)
	}
	// A write with no token behind it, and one by code.
	if _, err := rr.putDevice(rr.as(rr.adaID), ada.ID, deviceProps("v1.key", "replies")); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a device written with no token: %v, want forbidden", err)
	}
	agent := substrate.Actor(substrate.AgentActorPrefix + rr.ds.Repository().Authority + ":runs:lead")
	if _, err := rr.ds.Put(ctx, agent, substrate.PutInput{Kind: typeDevice, ID: ada.ID, Properties: deviceProps("v1.key", "replies")}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("an agent's device: %v, want forbidden", err)
	}

	// A viewer writes their own.
	cy := rr.mint(t, rr.cyID, nil)
	if _, err := rr.putDevice(rr.tokenCtx(rr.cyID, cy.ID), cy.ID, deviceProps("v1.cy", "requests")); err != nil {
		t.Fatalf("a viewer's own device: %v", err)
	}
	if got := referenceTargetOf(deviceRow(t, rr.ds, cy.ID), propDevicePerson); got.ID != actorID(substrate.PersonActor(rr.cyID)) {
		t.Fatalf("the viewer's device names %v", got)
	}
}

// A device is deleted by its own token, by another token of its person and
// by an admin, and by nobody else.
func TestADeviceIsDeletedByItsPersonOrAnAdmin(t *testing.T) {
	t.Parallel()
	rr := newRolesRepository(t)
	cy := rr.mint(t, rr.cyID, nil)
	cyOther := rr.mint(t, rr.cyID, nil)
	dee := rr.mint(t, rr.deeID(t), nil)
	ada := rr.mint(t, rr.adaID, nil)
	put := func() {
		t.Helper()
		if _, err := rr.putDevice(rr.tokenCtx(rr.cyID, cy.ID), cy.ID, deviceProps("v1.cy", "replies")); err != nil {
			t.Fatal(err)
		}
	}
	del := func(ctx context.Context) error {
		_, err := rr.ds.Delete(ctx, substrate.ActorPerson, typeDevice, cy.ID, substrate.DeleteInput{})
		return err
	}
	put()
	if err := del(rr.tokenCtx(rr.deeID(t), dee.ID)); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("another viewer deleting it: %v, want forbidden", err)
	}
	if err := del(rr.tokenCtx(rr.cyID, cy.ID)); err != nil {
		t.Fatalf("its own token deleting it: %v", err)
	}
	put()
	if err := del(rr.tokenCtx(rr.cyID, cyOther.ID)); err != nil {
		t.Fatalf("another token of its person deleting it: %v", err)
	}
	put()
	if err := del(rr.tokenCtx(rr.adaID, ada.ID)); err != nil {
		t.Fatalf("an admin deleting it: %v", err)
	}
}

// deeID is dee's identity id.
func (rr *rolesRepository) deeID(t *testing.T) string {
	t.Helper()
	row, err := rr.r.s.identityByName(context.Background(), rr.r.s.maint, rr.dee)
	if err != nil {
		t.Fatal(err)
	}
	return row.ID
}

// A device goes with its token: no longer listed once the token is gone,
// and deleted at the next sweep; one whose token expired is deleted by the
// sweep, with nothing in flight. The dispatcher's delete lands only while
// the record still holds the key the send carried.
func TestADeviceGoesWithItsToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newRolesRepository(t)
	person := actorID(substrate.ActorPerson)
	revoked := rr.mint(t, rr.adaID, nil)
	soon := time.Now().Add(2 * time.Second)
	expiring := rr.mint(t, rr.adaID, &soon)
	kept := rr.mint(t, rr.adaID, nil)
	for _, tok := range []substrate.TokenInfo{revoked, expiring, kept} {
		if _, err := rr.putDevice(rr.tokenCtx(rr.adaID, tok.ID), tok.ID, deviceProps("v1."+tok.ID, "replies")); err != nil {
			t.Fatal(err)
		}
	}
	listed := func() map[string]bool {
		t.Helper()
		devices, err := rr.ds.PushDevices(ctx, person)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, d := range devices {
			out[d.ID] = true
			if d.Token.ID != d.ID || d.PushKey != "v1."+d.ID || len(d.Notifies) != 1 {
				t.Fatalf("a listed device: %+v", d)
			}
		}
		return out
	}
	if got := listed(); len(got) != 3 {
		t.Fatalf("listed %v, want the three", got)
	}
	if _, err := rr.ds.Delete(rr.as(rr.adaID), substrate.ActorPerson, kindToken, revoked.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got := listed(); got[revoked.ID] {
		t.Fatal("a revoked token's device is still listed before the sweep")
	}
	waitFor(t, "the token to expire", func() bool { return time.Now().After(soon) })
	if got := listed(); got[expiring.ID] {
		t.Fatal("an expired token's device is still listed")
	}
	if _, err := rr.ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{revoked.ID, expiring.ID} {
		if row := deviceRow(t, rr.ds, id); row != nil && row.DeletedAt == nil {
			t.Fatalf("device %s outlived the sweep", id)
		}
	}
	if row := deviceRow(t, rr.ds, kept.ID); row == nil || row.DeletedAt != nil {
		t.Fatal("the sweep took a live token's device")
	}

	// The dispatcher's delete: refused for a key the record no longer
	// holds, landed for the one it does.
	if gone, err := rr.ds.DeletePushDevice(ctx, kept.ID, "v1.stale"); err != nil || gone {
		t.Fatalf("a delete under a stale key: %v %v", gone, err)
	}
	if gone, err := rr.ds.DeletePushDevice(ctx, kept.ID, "v1."+kept.ID); err != nil || !gone {
		t.Fatalf("a delete under the held key: %v %v", gone, err)
	}
	// A put after the purge is a fresh create.
	if _, err := rr.ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	rec, err := rr.putDevice(rr.tokenCtx(rr.adaID, kept.ID), kept.ID, deviceProps("v1.again", "replies"))
	if err != nil {
		t.Fatalf("a put after the purge: %v", err)
	}
	if rec.Version != 1 {
		t.Fatalf("a put after the purge is version %d, want a fresh record", rec.Version)
	}
}

// The signer of a token's pushes is the same on every derivation, differs
// per token, survives a rename of the repository, and its event ids open
// for that token alone, within a day, at their longest within the wire's
// bound.
func TestATokensPushSignerIsItsOwn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newRolesRepository(t)
	one, two := rr.mint(t, rr.adaID, nil), rr.mint(t, rr.adaID, nil)
	sign := func(ds *dataset, id string) substrate.PushSigner {
		t.Helper()
		s, err := ds.PushSigner(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	a, b := sign(rr.ds, one.ID), sign(rr.ds, one.ID)
	if !a.PublicKey().Equal(b.PublicKey()) {
		t.Fatal("two derivations for one token differ")
	}
	if a.PublicKey().Equal(sign(rr.ds, two.ID).PublicKey()) {
		t.Fatal("two tokens share a signing key")
	}
	body := []byte(`{"event":"reply"}`)
	now := time.Now()
	if err := pushwire.Verify(a.Sign(now, body), a.PublicKey(), now, body); err != nil {
		t.Fatalf("a signature its own key refuses: %v", err)
	}

	item := substrate.PushItem{Kind: vocabulary.KindLLMApproval, ID: "01h2xcejqtf2nbrexx3vqjhp42", Version: 4}
	id, err := a.SealEvent(substrate.PushEvent{Event: substrate.PushEventRequest, Thread: "8f2c41d07a", Item: item})
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := a.SealEvent(substrate.PushEvent{Event: substrate.PushEventRequest, Thread: "8f2c41d07a", Item: item}); again == id {
		t.Fatal("two seals of one event are the same id")
	}
	got, err := b.OpenEvent(id)
	if err != nil || got.Event != substrate.PushEventRequest || got.Thread != "8f2c41d07a" || got.Item != item {
		t.Fatalf("the event opened as %+v, %v", got, err)
	}
	if _, err := sign(rr.ds, two.ID).OpenEvent(id); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("another token's signer opened it: %v", err)
	}
	if _, err := a.OpenEvent(id[:len(id)-2] + "AA"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("a changed id opened: %v", err)
	}
	later, err := derivePushSigner(rr.ds.dek, one.ID, func() time.Time { return time.Now().Add(substrate.PushEventLifetime + time.Minute) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := later.OpenEvent(id); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("an id past its day opened: %v", err)
	}

	// The longest item: ids at the record id alphabet's bound, the longest
	// kind of the four.
	long := strings.Repeat("a", vocabulary.MaxIDLen)
	maxed, err := a.SealEvent(substrate.PushEvent{
		Event: substrate.PushEventRequest, Thread: long,
		Item: substrate.PushItem{Kind: vocabulary.KindRecordPatchRequest, ID: long, Version: 1 << 62},
	})
	if err != nil || len(maxed) > pushwire.MaxEventID || len(maxed) < pushwire.MinEventID {
		t.Fatalf("the longest item sealed to %d characters: %v", len(maxed), err)
	}
	// A `read` of as many alerts as there are: split into ids that each
	// fit, covering every fingerprint in order.
	prints := make([]string, 200)
	for i := range prints {
		prints[i] = pushwire.EventFingerprint(strings.Repeat("x", i+pushwire.MinEventID))
	}
	ids, err := pushdispatch.SealRead(a, long, prints, time.Now())
	if err != nil || len(ids) < 2 {
		t.Fatalf("a read of 200 alerts sealed to %d ids: %v", len(ids), err)
	}
	var covered []string
	for _, id := range ids {
		if len(id) > pushwire.MaxEventID {
			t.Fatalf("a read id of %d characters", len(id))
		}
		e, err := a.OpenEvent(id)
		if err != nil {
			t.Fatal(err)
		}
		covered = append(covered, e.Alerts...)
	}
	if strings.Join(covered, ",") != strings.Join(prints, ",") {
		t.Fatal("the split reads do not cover the alerts in order")
	}

	// A rename keeps the keys: the derivation reads ids, never names.
	renamed := testdb.RepositoryLabel(t) + "z.example.com"
	if _, err := rr.r.s.RenameRepository(ctx, substrate.LoginInput{Identity: rr.ada, Repository: rr.ada, Password: peoplePassword}, renamed); err != nil {
		t.Fatalf("rename: %v", err)
	}
	d, err := rr.r.s.Dataset(ctx, renamed)
	if err != nil {
		t.Fatal(err)
	}
	if !sign(d.(*dataset), one.ID).PublicKey().Equal(a.PublicKey()) {
		t.Fatal("a rename changed the token's signing key")
	}
}

// signals is a sink that keeps what it is handed.
type signals struct {
	mu  sync.Mutex
	got []substrate.PushSignal
}

func (s *signals) PushSignal(sig substrate.PushSignal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, sig)
}

func (s *signals) take() []substrate.PushSignal {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.got
	s.got = nil
	return out
}

func (s *signals) of(kind substrate.PushSignalKind) []substrate.PushSignal {
	var out []substrate.PushSignal
	for _, sig := range s.take() {
		if sig.Kind == kind {
			out = append(out, sig)
		}
	}
	return out
}

// The engine hands its sink a chat turn's settle with the assistant message
// it named, a failure with none, a request raised and decided with its
// record, and a mark that moved; a turn that raised a request hands out
// that request and no settle. The rechecks and the badge read the same
// state back.
func TestTheEngineHandsOutWhatAPushIsAbout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	sink := &signals{}
	r.ds.svc.pushSink = sink
	ada := r.person(r.adaID, substrate.ActorPerson)
	person := actorID(substrate.ActorPerson)
	token := r.mint(t, r.adaID, nil)
	reading := substrate.WithPrincipal(ada, token.ID)

	// A reply.
	thread, _ := r.chat(t, ada, substrate.ActorPerson, "lead", "", fakeTurn{content: "one."})
	settles := sink.of(substrate.PushSettled)
	if len(settles) != 1 {
		t.Fatalf("a reply handed out %d settles", len(settles))
	}
	s := settles[0]
	if s.Thread != thread || s.Mode != agentModeChat || s.Person != person || s.Status != threadOK ||
		s.Message != lastMessageOf(t, r.ds, thread) || s.Repository != r.ds.scope.Repository || s.TurnStarted.IsZero() {
		t.Fatalf("the settle: %+v", s)
	}
	check := substrate.PushCheck{Event: substrate.PushEventReply, Person: person, Thread: thread, Message: s.Message}
	if got, err := r.ds.PushRecheck(ctx, check); err != nil || got != substrate.PushSend {
		t.Fatalf("an unread reply rechecks %q, %v", got, err)
	}
	badge := func() map[string]bool {
		t.Helper()
		threads, err := r.ds.PushBadgeThreads(ctx, person, "")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, th := range threads {
			if th.Kind != typeThread || th.Properties["agent"] == nil {
				t.Fatalf("a badge candidate: %+v", th)
			}
			out[th.ID] = true
		}
		return out
	}
	if !badge()[thread] {
		t.Fatal("an unread chat is no badge candidate")
	}
	// Read through it: the recheck says seen, the badge drops it, and the
	// mark is handed out with the token that wrote it.
	if _, err := r.read(reading, thread, s.Message); err != nil {
		t.Fatal(err)
	}
	marks := sink.of(substrate.PushMarked)
	if len(marks) != 1 || marks[0].Thread != thread || marks[0].Person != person || marks[0].Token != token.ID {
		t.Fatalf("the mark handed out %+v", marks)
	}
	if got, _ := r.ds.PushRecheck(ctx, check); got != substrate.PushSeen {
		t.Fatalf("a read reply rechecks %q", got)
	}
	if badge()[thread] {
		t.Fatal("a read chat is still a badge candidate")
	}
	// Marked unread counts again, archived does not, and a write that moves
	// nothing hands out nothing.
	markID := threadMarkID(thread, person)
	if _, err := r.ds.Patch(reading, substrate.ActorPerson, typeThreadMark, markID, substrate.PatchInput{Properties: map[string]any{propMarkUnread: true}}); err != nil {
		t.Fatal(err)
	}
	if len(sink.of(substrate.PushMarked)) != 0 {
		t.Fatal("marking unread handed out a mark")
	}
	if !badge()[thread] {
		t.Fatal("a chat marked unread is no badge candidate")
	}
	if _, err := r.ds.Patch(reading, substrate.ActorPerson, typeThreadMark, markID, substrate.PatchInput{Properties: map[string]any{propMarkArchived: true}}); err != nil {
		t.Fatal(err)
	}
	if got := sink.of(substrate.PushMarked); len(got) != 1 {
		t.Fatalf("archiving handed out %d marks", len(got))
	}
	if badge()[thread] {
		t.Fatal("an archived chat nothing waits in is a badge candidate")
	}
	if got, _ := r.ds.PushRecheck(ctx, check); got != substrate.PushArchived {
		t.Fatalf("an archived reply rechecks %q", got)
	}

	// A failure: the first call of a new chat fails, so the thread names
	// the person's own message, and the settle names no assistant message.
	r.fake.script("lead", fakeTurn{status: 400})
	if _, err := r.ds.ChatAgent(ada, substrate.ActorPerson, r.pkg+"/lead", substrate.ChatInput{Message: "a first message", MessageID: "push-first-failed"}, nil); err == nil {
		t.Fatal("a failed model call answered no error")
	}
	settles = sink.of(substrate.PushSettled)
	if len(settles) != 1 || settles[0].Status != threadError || settles[0].Message != "" {
		t.Fatalf("a failed first call handed out %+v", settles)
	}
	failed := settles[0]
	// A mark taken during the turn, before the settle, saw nothing of the
	// failure; one at or after the settle saw it.
	fcheck := substrate.PushCheck{Event: substrate.PushEventFailed, Person: person, Thread: failed.Thread, SettledAt: failed.At.Add(time.Hour)}
	if _, err := r.read(reading, failed.Thread, "push-first-failed"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ds.PushRecheck(ctx, fcheck); got != substrate.PushSend {
		t.Fatalf("a failure read before it settled rechecks %q", got)
	}
	fcheck.SettledAt = failed.At
	if got, _ := r.ds.PushRecheck(ctx, fcheck); got != substrate.PushSeen {
		t.Fatalf("a failure read at its settle rechecks %q", got)
	}

	// A question: the turn raises it, and its settle names it and the
	// message it wrote, and no reply; answering it hands out its decision
	// with the token that wrote it.
	sink.take()
	asked, _ := r.chat(t, ada, substrate.ActorPerson, "keeper", "",
		fakeTurn{calls: []fakeCall{{"ask", runsAsk}}}, fakeTurn{content: "asked."}, fakeTurn{content: "noted."})
	all := sink.take()
	var raised, settledAsk []substrate.PushSignal
	for _, sig := range all {
		switch sig.Kind {
		case substrate.PushSettled:
			settledAsk = append(settledAsk, sig)
		case substrate.PushRaised:
			raised = append(raised, sig)
		}
	}
	if len(raised) != 1 || raised[0].Thread != asked || raised[0].Item.Kind != vocabulary.KindLLMInteraction || raised[0].Person != person {
		t.Fatalf("the question handed out %+v", raised)
	}
	item := raised[0].Item
	if len(settledAsk) != 1 || settledAsk[0].Message != "" || len(settledAsk[0].Raised) != 1 || settledAsk[0].Raised[0] != item ||
		settledAsk[0].Wrote == "" || settledAsk[0].Wrote != lastMessageOf(t, r.ds, asked) {
		t.Fatalf("the turn that asked settled as %+v, want the question raised, the message it wrote and no reply", settledAsk)
	}
	rcheck := substrate.PushCheck{Event: substrate.PushEventRequest, Person: person, Thread: asked, Item: item, SettleMessage: settledAsk[0].Wrote}
	if got, _ := r.ds.PushRecheck(ctx, rcheck); got != substrate.PushSend {
		t.Fatalf("a pending question rechecks %q", got)
	}
	// Read through the message its turn wrote, the person saw the card.
	if _, err := r.read(reading, asked, settledAsk[0].Wrote); err != nil {
		t.Fatal(err)
	}
	sink.take()
	if got, _ := r.ds.PushRecheck(ctx, rcheck); got != substrate.PushSeen {
		t.Fatalf("a question read through its turn's message rechecks %q", got)
	}
	// A newer version of the card is a new item: the old one is no longer
	// pending at its version.
	if stale := (substrate.PushItem{Kind: item.Kind, ID: item.ID, Version: item.Version - 1}); stale.Version > 0 {
		if pending, _ := r.ds.PushPending(ctx, stale); pending {
			t.Fatal("an older version of a pending card reads as pending")
		}
	}
	if pending, err := r.ds.PushPending(ctx, item); err != nil || !pending {
		t.Fatalf("a pending question: %v %v", pending, err)
	}
	rec, err := r.ds.Get(ctx, item.Kind, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ds.Patch(reading, substrate.ActorPerson, item.Kind, item.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionAnswered, "answers": []any{map[string]any{"question": "sure", "selected": []any{"yes"}}}},
		IfVersion:  &rec.Version,
	}); err != nil {
		t.Fatal(err)
	}
	decided := sink.of(substrate.PushDecided)
	if len(decided) != 1 || decided[0].Item.ID != item.ID || decided[0].Token != token.ID {
		t.Fatalf("the answer handed out %+v", decided)
	}
	if got, _ := r.ds.PushRecheck(ctx, rcheck); got != substrate.PushSeen {
		t.Fatalf("an answered question rechecks %q", got)
	}

	// The resumed turn's settle is handed out once its commit is over,
	// which is after a read sees it.
	waitFor(t, "the resume's settle handed out", func() bool {
		for _, sig := range sink.take() {
			if sig.Kind == substrate.PushSettled && sig.Thread == asked && sig.Status == threadOK {
				return true
			}
		}
		return false
	})

	// The admins: ada is one, bo, an editor, is not.
	admins, err := r.ds.PushAdmins(ctx)
	if err != nil || len(admins) != 1 || admins[0] != person {
		t.Fatalf("the admins: %v %v", admins, err)
	}

	// No sink, nothing handed out.
	r.ds.svc.pushSink = nil
	r.chat(t, ada, substrate.ActorPerson, "lead", "", fakeTurn{content: "quiet."})
	if got := sink.take(); len(got) != 0 {
		t.Fatalf("a dataset with no sink handed out %+v", got)
	}
}

// A badge counts `chat` threads alone, and only those that act for the
// person: with two people, each counts their own chat and not the other's,
// though ada, an admin, reads both.
func TestTheBadgeCountsChatsAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	ada, bo := actorID(substrate.ActorPerson), actorID(substrate.PersonActor(r.boID))
	r.fake.script("lead", fakeTurn{content: "called."})
	res, err := r.ds.CallAgent(r.person(r.adaID, substrate.ActorPerson), r.pkg+"/lead", "go")
	if err != nil {
		t.Fatal(err)
	}
	adas, _ := r.chat(t, r.person(r.adaID, substrate.ActorPerson), substrate.ActorPerson, "lead", "", fakeTurn{content: "ada's."})
	bos, _ := r.chat(t, r.person(r.boID, substrate.ActorPerson), substrate.ActorPerson, "lead", "", fakeTurn{content: "bo's."})
	if _, person := threadPushInfo(storedThread(t, r.ds, bos)); person != bo {
		t.Fatalf("bo's chat acts for %q, want bo's actor %q", person, bo)
	}
	badge := func(person string) map[string]bool {
		t.Helper()
		threads, err := r.ds.PushBadgeThreads(ctx, person, "")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, th := range threads {
			out[th.ID] = true
		}
		return out
	}
	if got := badge(ada); !got[adas] || got[bos] || got[res.Thread] {
		t.Fatalf("ada's badge candidates %v: want her chat %s alone, not bo's %s nor the call %s", got, adas, bos, res.Thread)
	}
	if got := badge(bo); !got[bos] || got[adas] {
		t.Fatalf("bo's badge candidates %v: want his chat %s alone, not ada's %s", got, bos, adas)
	}
}

// storedThread is a thread's stored row.
func storedThread(t *testing.T, ds *dataset, id string) *erow {
	t.Helper()
	row, err := ds.loadRowDB(context.Background(), eref{Kind: typeThread, ID: id})
	if err != nil || row == nil {
		t.Fatalf("thread %s: %v", id, err)
	}
	return row
}

// A write that rolls back hands out nothing, and push's rechecks answer
// while a write holds the changelog: they read a snapshot and take neither
// the write gate nor the lock.
func TestPushReadsNeitherWaitOnWritesNorSeeRolledBackOnes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	sink := &signals{}
	r.ds.svc.pushSink = sink
	ada := r.person(r.adaID, substrate.ActorPerson)
	person := actorID(substrate.ActorPerson)
	thread, _ := r.chat(t, ada, substrate.ActorPerson, "lead", "", fakeTurn{content: "one."})
	reply := lastMessageOf(t, r.ds, thread)
	sink.take()

	err := r.ds.inTx(ada, substrate.ActorPerson, false, func(tx *txn) error {
		if _, err := tx.put(substrate.PutInput{Kind: typeThreadMark, Properties: map[string]any{propMarkThread: thread, propMarkReadThrough: reply}}); err != nil {
			return err
		}
		return errors.New("roll back")
	})
	if err == nil || len(sink.take()) != 0 {
		t.Fatalf("a rolled-back mark: %v, signals handed out", err)
	}

	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- r.ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			if _, err := tx.put(substrate.PutInput{Kind: r.note, ID: fid("held"), Properties: map[string]any{"name": "held"}}); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	quick, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	got, err := r.ds.PushRecheck(quick, substrate.PushCheck{Event: substrate.PushEventReply, Person: person, Thread: thread, Message: reply})
	if err != nil || got != substrate.PushSend {
		t.Fatalf("a recheck under a held write: %q, %v", got, err)
	}
	if _, err := r.ds.PushPending(quick, substrate.PushItem{Kind: vocabulary.KindLLMInteraction, ID: "none", Version: 1}); err != nil {
		t.Fatalf("a pending read under a held write: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A write of a device's labels alone carries no properties, and lands.
func TestADeviceWriteOfLabelsAloneLands(t *testing.T) {
	t.Parallel()
	rr := newRolesRepository(t)
	tok := rr.mint(t, rr.adaID, nil)
	ctx := rr.tokenCtx(rr.adaID, tok.ID)
	if _, err := rr.putDevice(ctx, tok.ID, deviceProps("v1.key", "replies")); err != nil {
		t.Fatal(err)
	}
	if _, err := rr.ds.Patch(ctx, substrate.ActorPerson, typeDevice, tok.ID, substrate.PatchInput{Labels: map[string]any{"example.com/room": "kitchen"}}); err != nil {
		t.Fatalf("a patch of labels alone: %v", err)
	}
	if _, err := rr.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: typeDevice, ID: tok.ID, Labels: map[string]any{"example.com/room": "hall"}}); err != nil {
		t.Fatalf("a put of labels alone: %v", err)
	}
}

// The sweep's batch holds only devices to delete, so a batch smaller than
// the live devices ahead of an expired one still reaches it.
func TestTheDeviceSweepReachesPastLiveDevices(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newRolesRepository(t)
	soon := time.Now().Add(time.Second)
	var expired substrate.TokenInfo
	for i := range 4 {
		var exp *time.Time
		if i == 3 {
			exp = &soon
		}
		tok := rr.mint(t, rr.adaID, exp)
		if exp != nil {
			expired = tok
		}
		if _, err := rr.putDevice(rr.tokenCtx(rr.adaID, tok.ID), tok.ID, deviceProps("v1."+tok.ID, "replies")); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "the token to expire", func() bool { return time.Now().After(soon) })
	if n, err := rr.ds.sweepDevices(ctx, nowUTC(), 1); err != nil || n != 1 {
		t.Fatalf("a sweep of one swept %d: %v", n, err)
	}
	if row := deviceRow(t, rr.ds, expired.ID); row == nil || row.DeletedAt == nil {
		t.Fatal("the expired token's device outlived a sweep of one")
	}
}
