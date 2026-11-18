package pushdispatch

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/metrics"
	"github.com/geoah/substrate/internal/push"
	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/pushwire/pushwiretest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	testRepo     = "rnd-repo"
	testThread   = "8f2c41d07a"
	testPerson   = "actor-ada"
	testClientID = "https://apps.example.com/apps/chat/client.json"
	testRelay    = "https://push.example.com"
)

// fakeSigner seals an event as base64url of its JSON beside a counter, so
// two seals of one event differ and an id grows with what it holds.
type fakeSigner struct {
	key  ed25519.PrivateKey
	mu   sync.Mutex
	seq  int
	open map[string]substrate.PushEvent
}

func newFakeSigner(token string) *fakeSigner {
	seed := sha256.Sum256([]byte("signer " + token))
	return &fakeSigner{key: ed25519.NewKeyFromSeed(seed[:]), open: map[string]substrate.PushEvent{}}
}

func (s *fakeSigner) PublicKey() ed25519.PublicKey { return s.key.Public().(ed25519.PublicKey) }

func (s *fakeSigner) Sign(t time.Time, body []byte) string { return pushwire.Sign(s.key, t, body) }

func (s *fakeSigner) SealEvent(e substrate.PushEvent) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	raw, err := json.Marshal(struct {
		N int
		E substrate.PushEvent
	}{s.seq, e})
	if err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	if len(id) > pushwire.MaxEventID {
		return "", fmt.Errorf("an id of %d characters is past %d", len(id), pushwire.MaxEventID)
	}
	s.open[id] = e
	return id, nil
}

func (s *fakeSigner) OpenEvent(id string) (substrate.PushEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.open[id]
	if !ok {
		return substrate.PushEvent{}, substrate.ErrNotFound
	}
	return e, nil
}

type deleteCall struct{ id, key string }

// fakeStore is the engine as the dispatcher reads it.
type fakeStore struct {
	substrate.Dataset

	mu       sync.Mutex
	threads  map[string]*substrate.Record
	messages map[string]*substrate.Record
	devices  map[string][]substrate.PushDevice
	admins   []string
	// recheck answers PushRecheck; nil answers send.
	recheck  func(substrate.PushCheck) string
	badge    []*substrate.Record
	signers  map[string]*fakeSigner
	deleted  []deleteCall
	checks   []substrate.PushCheck
	badgeN   int
	devicesN int
	// gone are the request records Get no longer finds, kind/id.
	gone map[string]bool
	// badgeErr, when set, fails every badge read.
	badgeErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		threads: map[string]*substrate.Record{
			testThread: {Kind: vocabulary.KindLLMThread, ID: testThread, Version: 7, Properties: map[string]any{"agent": "moss"}},
		},
		messages: map[string]*substrate.Record{},
		devices:  map[string][]substrate.PushDevice{},
		signers:  map[string]*fakeSigner{},
		gone:     map[string]bool{},
	}
}

func (s *fakeStore) Get(_ context.Context, kind, id string) (*substrate.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rec *substrate.Record
	switch kind {
	case vocabulary.KindLLMThread:
		rec = s.threads[id]
	case vocabulary.KindLLMMessage:
		rec = s.messages[id]
	default:
		// A request's record: present unless gone names it.
		if !s.gone[kind+"/"+id] {
			rec = &substrate.Record{Kind: kind, ID: id, Version: 1, Properties: map[string]any{"thread": testThread}}
		}
	}
	if rec == nil {
		return nil, fmt.Errorf("%w: %s/%s", substrate.ErrNotFound, kind, id)
	}
	return rec, nil
}

func (s *fakeStore) signer(token string) *fakeSigner {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.signers[token] == nil {
		s.signers[token] = newFakeSigner(token)
	}
	return s.signers[token]
}

func (s *fakeStore) PushSigner(_ context.Context, token string) (substrate.PushSigner, error) {
	return s.signer(token), nil
}

func (s *fakeStore) PushPerson(context.Context) (string, error) { return testPerson, nil }

func (s *fakeStore) PushDevices(_ context.Context, person string) ([]substrate.PushDevice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devicesN++
	return slices.Clone(s.devices[person]), nil
}

func (s *fakeStore) PushAdmins(context.Context) ([]string, error) { return s.admins, nil }

func (s *fakeStore) PushRecheck(_ context.Context, c substrate.PushCheck) (string, error) {
	s.mu.Lock()
	s.checks = append(s.checks, c)
	f := s.recheck
	s.mu.Unlock()
	if f == nil {
		return substrate.PushSend, nil
	}
	return f(c), nil
}

func (s *fakeStore) PushPending(context.Context, substrate.PushItem) (bool, error) { return true, nil }

func (s *fakeStore) PushMayDecide(context.Context, substrate.Actor, substrate.PushItem) (bool, error) {
	return true, nil
}

// PushBadgeThreads pages s.badge by id, 500 at a time, as the engine does.
func (s *fakeStore) PushBadgeThreads(_ context.Context, _, after string) ([]*substrate.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.badgeN++
	if s.badgeErr != nil {
		return nil, s.badgeErr
	}
	var page []*substrate.Record
	for _, th := range s.badge {
		if th.ID > after && len(page) < 500 {
			page = append(page, th)
		}
	}
	return page, nil
}

func (s *fakeStore) DeletePushDevice(_ context.Context, id, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, deleteCall{id, key})
	return true, nil
}

var _ substrate.PushStore = (*fakeStore)(nil)

type fakeDatasets struct{ ds substrate.Dataset }

func (f fakeDatasets) Dataset(context.Context, string) (substrate.Dataset, error) { return f.ds, nil }

// fakeView reads every record but the threads its token is kept from.
type fakeView struct{ hidden map[string]bool }

func (v fakeView) Reads(_ context.Context, rec *substrate.Record) (bool, error) {
	return !v.hidden[rec.ID], nil
}

type fakeDocs struct {
	mu    sync.Mutex
	doc   authority.AuthorityDocument
	err   error
	calls int
}

func (f *fakeDocs) Read(_ context.Context, name string, _ bool) (authority.Source, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return authority.Source{}, f.err
	}
	if name != f.doc.Authority {
		return authority.Source{}, errors.New("no such authority")
	}
	return authority.Source{Document: f.doc}, nil
}

type sent struct {
	relay string
	req   pushwire.PushRequest
}

// fakeSender records what is sent and answers by push key, sent otherwise.
type fakeSender struct {
	mu      sync.Mutex
	sent    []sent
	answers map[string]push.Answer
	// during, when set, runs inside a send, before it answers.
	during func(ctx context.Context, req pushwire.PushRequest)
}

func (f *fakeSender) Send(ctx context.Context, relay string, _ substrate.PushSigner, req pushwire.PushRequest) push.Answer {
	f.mu.Lock()
	f.sent = append(f.sent, sent{relay, req})
	during := f.during
	a, scripted := f.answers[req.PushKey]
	f.mu.Unlock()
	if during != nil {
		during(ctx, req)
	}
	if ctx.Err() != nil {
		return push.Answer{Outcome: push.Failed, Err: ctx.Err()}
	}
	if scripted {
		return a
	}
	return push.Answer{Outcome: push.Sent, Status: http.StatusOK}
}

func (f *fakeSender) all() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sent)
}

func (f *fakeSender) to(device string) []sent {
	var out []sent
	for _, s := range f.all() {
		if s.req.PushKey == "key-"+device {
			out = append(out, s)
		}
	}
	return out
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

type rig struct {
	t      *testing.T
	d      *Dispatcher
	store  *fakeStore
	docs   *fakeDocs
	sender *fakeSender
	clock  *clock
	start  time.Time
	hidden map[string]map[string]bool
}

func newRig(t *testing.T) *rig {
	t.Helper()
	start := time.Unix(1763459123, 0).UTC()
	r := &rig{
		t: t, store: newFakeStore(), sender: &fakeSender{answers: map[string]push.Answer{}},
		clock: &clock{t: start}, start: start, hidden: map[string]map[string]bool{},
		docs: &fakeDocs{doc: authority.AuthorityDocument{Authority: "example.com", Apps: []authority.AuthorityApp{
			{ClientID: testClientID, Push: &authority.AppPush{Relay: testRelay}},
		}}},
	}
	r.d = New(Options{
		View: func(_ context.Context, _ substrate.Dataset, tok substrate.TokenInfo) (View, error) {
			return fakeView{hidden: r.hidden[tok.ID]}, nil
		},
		Documents: r.docs, Sender: r.sender, Now: r.clock.now, Manual: true,
	})
	r.d.Start(fakeDatasets{ds: r.store})
	t.Cleanup(r.d.Close)
	return r
}

// device registers a device of person, named by its token, taking notifies.
func (r *rig) device(person, id string, notifies ...string) {
	if len(notifies) == 0 {
		notifies = []string{substrate.PushNotifiesReplies, substrate.PushNotifiesRequests}
	}
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.devices[person] = append(r.store.devices[person], substrate.PushDevice{
		ID: id, Person: person, PushKey: "key-" + id, Notifies: notifies,
		Token: substrate.TokenInfo{ID: id, ClientID: testClientID, AppAuthority: "example.com", Identity: person},
	})
}

func (r *rig) message(id string, version int64) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.messages[id] = &substrate.Record{
		Kind: vocabulary.KindLLMMessage, ID: id, Version: version,
		Properties: map[string]any{"thread": testThread},
	}
}

// at sets the clock to start+d and fires what is due.
func (r *rig) at(d time.Duration) {
	r.clock.set(r.start.Add(d))
	r.d.Tick(r.start.Add(d))
}

// signal hands the dispatcher s at start+d.
func (r *rig) signal(d time.Duration, s substrate.PushSignal) {
	r.clock.set(r.start.Add(d))
	if s.Repository == "" {
		s.Repository = testRepo
	}
	if s.Thread == "" {
		s.Thread = testThread
	}
	if s.At.IsZero() {
		s.At = r.start.Add(d)
	}
	r.d.PushSignal(s)
}

func (r *rig) reply(d time.Duration, message string) {
	r.signal(d, substrate.PushSignal{
		Kind: substrate.PushSettled, Mode: "chat", Person: testPerson, Status: "ok",
		Message: message, TurnStarted: r.start.Add(d - time.Second),
	})
}

func (r *rig) raise(d time.Duration, person, id string, version int64) {
	r.signal(d, substrate.PushSignal{
		Kind: substrate.PushRaised, Mode: "chat", Person: person,
		Item: substrate.PushItem{Kind: vocabulary.KindLLMApproval, ID: id, Version: version},
	})
}

// opened is what an event id a device was sent seals.
func (r *rig) opened(device string, s sent) substrate.PushEvent {
	r.t.Helper()
	e, err := r.store.signer(device).OpenEvent(s.req.EventID)
	if err != nil {
		r.t.Fatalf("the event id sent to %s does not open under its token's signer: %v", device, err)
	}
	return e
}

// outcome reads substrate_push_total for one event and outcome.
func outcome(event, outcome string) float64 {
	var m dto.Metric
	if err := metrics.PushTotal.WithLabelValues(event, outcome).Write(&m); err != nil {
		panic(err)
	}
	return m.GetCounter().GetValue()
}

func TestReplyWaitsFiveSecondsAndNamesItsMessage(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.message("m1", 3)
	r.store.badge = []*substrate.Record{
		{Kind: vocabulary.KindLLMThread, ID: "a"}, {Kind: vocabulary.KindLLMThread, ID: "b"}, {Kind: vocabulary.KindLLMThread, ID: "c"},
	}
	r.reply(0, "m1")
	r.at(4 * time.Second)
	if got := r.sender.all(); len(got) != 0 {
		t.Fatalf("a reply went out before its five seconds: %+v", got)
	}
	r.at(5 * time.Second)
	got := r.sender.all()
	if len(got) != 1 {
		t.Fatalf("sent %d pushes, want 1", len(got))
	}
	s := got[0]
	if s.relay != testRelay || s.req.Event != pushwire.EventReply || s.req.Thread != testThread || s.req.Badge != 3 || s.req.PushKey != "key-tok1" {
		t.Fatalf("sent %+v to %s", s.req, s.relay)
	}
	e := r.opened("tok1", s)
	if e.Event != substrate.PushEventReply || e.Thread != testThread ||
		e.Item != (substrate.PushItem{Kind: vocabulary.KindLLMMessage, ID: "m1", Version: 3}) {
		t.Fatalf("the event id seals %+v", e)
	}
	if r.d.Holds() != 0 {
		t.Fatalf("%d holds left", r.d.Holds())
	}
}

func TestARecheckThatSaysSeenDropsTheReply(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.message("m1", 1)
	r.store.recheck = func(c substrate.PushCheck) string {
		if c.Event == substrate.PushEventReply && c.Message == "m1" && c.Person == testPerson {
			return substrate.PushSeen
		}
		return substrate.PushSend
	}
	seen := outcome(substrate.PushEventReply, outcomeSeen)
	r.reply(0, "m1")
	r.at(5 * time.Second)
	if got := r.sender.all(); len(got) != 0 {
		t.Fatalf("a seen reply was sent: %+v", got)
	}
	if outcome(substrate.PushEventReply, outcomeSeen) != seen+1 {
		t.Fatal("the seen reply was not counted")
	}
}

// Archived stops a reply and not a request: the store says so per event, and
// the dispatcher obeys each answer.
func TestArchivedStopsAReplyAndNotARequest(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.message("m1", 1)
	r.store.recheck = func(c substrate.PushCheck) string {
		if c.Event == substrate.PushEventReply {
			return substrate.PushArchived
		}
		return substrate.PushSend
	}
	r.reply(0, "m1")
	r.raise(0, testPerson, "ap1", 2)
	r.at(5 * time.Second)
	got := r.sender.all()
	if len(got) != 1 || got[0].req.Event != pushwire.EventRequest {
		t.Fatalf("sent %+v, want the request alone", got)
	}
	e := r.opened("tok1", got[0])
	if e.Item != (substrate.PushItem{Kind: vocabulary.KindLLMApproval, ID: "ap1", Version: 2}) {
		t.Fatalf("the request seals %+v", e.Item)
	}
	// The request's own recheck carried its item.
	var asked bool
	for _, c := range r.store.checks {
		if c.Event == substrate.PushEventRequest && c.Item.ID == "ap1" && !c.Request {
			asked = true
		}
	}
	if !asked {
		t.Fatalf("the request was not checked again: %+v", r.store.checks)
	}
}

func TestAFailurePushesOnlyAfterATurnOfMoreThanFiveSeconds(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.signal(0, substrate.PushSignal{
		Kind: substrate.PushSettled, Mode: "chat", Person: testPerson, Status: "error",
		ThreadVersion: 9, TurnStarted: r.start.Add(-3 * time.Second),
	})
	if r.d.Holds() != 0 {
		t.Fatal("a failure in a short turn is held")
	}
	r.signal(0, substrate.PushSignal{
		Kind: substrate.PushSettled, Mode: "chat", Person: testPerson, Status: "overbudget",
		ThreadVersion: 9, TurnStarted: r.start.Add(-6 * time.Second),
	})
	settled := r.start
	r.at(5 * time.Second)
	got := r.sender.all()
	if len(got) != 1 || got[0].req.Event != pushwire.EventFailed {
		t.Fatalf("sent %+v, want one failure", got)
	}
	e := r.opened("tok1", got[0])
	if e.Item != (substrate.PushItem{Kind: vocabulary.KindLLMThread, ID: testThread, Version: 9}) {
		t.Fatalf("the failure seals %+v", e.Item)
	}
	if c := r.store.checks[0]; c.Event != substrate.PushEventFailed || !c.SettledAt.Equal(settled) {
		t.Fatalf("the failure was checked as %+v", c)
	}
}

func TestSettlesThatPushNothing(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.message("m1", 1)
	for _, s := range []substrate.PushSignal{
		{Kind: substrate.PushSettled, Mode: "call", Person: testPerson, Status: "ok", Message: "m1"},
		{Kind: substrate.PushSettled, Mode: "manual", Person: testPerson, Status: "error", TurnStarted: r.start.Add(-time.Minute)},
		{Kind: substrate.PushSettled, Mode: "subagent", Person: testPerson, Status: "ok", Message: "m1"},
		{Kind: substrate.PushSettled, Mode: "chat", Status: "ok", Message: "m1"},
		{Kind: substrate.PushSettled, Mode: "chat", Person: testPerson, Status: "ok"},
		{Kind: substrate.PushSettled, Mode: "chat", Person: testPerson, Status: "stopped", TurnStarted: r.start.Add(-time.Minute)},
	} {
		r.signal(0, s)
	}
	r.at(time.Minute)
	if got := r.sender.all(); len(got) != 0 || r.d.Holds() != 0 {
		t.Fatalf("sent %+v, %d holds", got, r.d.Holds())
	}
}

func TestASecondSignalUnderOneKeyRestartsTheWait(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.raise(0, testPerson, "ap1", 1)
	r.raise(3*time.Second, testPerson, "ap1", 1)
	r.at(5 * time.Second)
	if got := r.sender.all(); len(got) != 0 {
		t.Fatalf("the restarted wait fired early: %+v", got)
	}
	r.at(8 * time.Second)
	if got := r.sender.all(); len(got) != 1 {
		t.Fatalf("sent %d, want 1", len(got))
	}
}

func TestTwoCardsOnOneThreadSendTwoPushes(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.raise(0, testPerson, "ap1", 4)
	r.raise(0, testPerson, "ap2", 5)
	r.at(5 * time.Second)
	got := r.sender.all()
	if len(got) != 2 || got[0].req.EventID == got[1].req.EventID {
		t.Fatalf("sent %+v", got)
	}
	items := map[string]bool{}
	for _, s := range got {
		if s.req.Event != pushwire.EventRequest || s.req.Thread != testThread {
			t.Fatalf("sent %+v", s.req)
		}
		items[r.opened("tok1", s).Item.ID] = true
	}
	if !items["ap1"] || !items["ap2"] {
		t.Fatalf("the two ids name %v", items)
	}
}

func TestADevicesNotifiesChoosesItsEvents(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1", substrate.PushNotifiesRequests)
	r.device(testPerson, "tok2", substrate.PushNotifiesReplies)
	r.message("m1", 1)
	r.reply(0, "m1")
	r.raise(0, testPerson, "ap1", 1)
	r.at(5 * time.Second)
	if got := r.sender.to("tok1"); len(got) != 1 || got[0].req.Event != pushwire.EventRequest {
		t.Fatalf("tok1 took %+v", got)
	}
	if got := r.sender.to("tok2"); len(got) != 1 || got[0].req.Event != pushwire.EventReply {
		t.Fatalf("tok2 took %+v", got)
	}
}

func TestADeviceWhoseViewDoesNotReadTheThreadIsNotSentTo(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.device(testPerson, "tok2")
	r.hidden["tok2"] = map[string]bool{testThread: true}
	r.message("m1", 1)
	unreadable := outcome(substrate.PushEventReply, outcomeUnreadable)
	r.reply(0, "m1")
	r.at(5 * time.Second)
	if len(r.sender.to("tok1")) != 1 || len(r.sender.to("tok2")) != 0 {
		t.Fatalf("sent %+v", r.sender.all())
	}
	if outcome(substrate.PushEventReply, outcomeUnreadable) != unreadable+1 {
		t.Fatal("the unreadable device was not counted")
	}
}

// With no device left, nothing else is done: no authority lookup and no
// badge.
func TestNoDeviceReadsNoDocumentAndNoBadge(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1", substrate.PushNotifiesRequests)
	r.message("m1", 1)
	none := outcome(substrate.PushEventReply, outcomeNoDevice)
	r.reply(0, "m1")
	r.at(5 * time.Second)
	if len(r.sender.all()) != 0 || r.docs.calls != 0 || r.store.badgeN != 0 {
		t.Fatalf("sent %d, read %d documents and %d badges", len(r.sender.all()), r.docs.calls, r.store.badgeN)
	}
	if outcome(substrate.PushEventReply, outcomeNoDevice) != none+1 {
		t.Fatal("no device was not counted")
	}
}

func TestARequestOfAThreadForNobodyGoesToTheAdminsWhoReadIt(t *testing.T) {
	r := newRig(t)
	r.store.admins = []string{"actor-admin1", "actor-admin2"}
	r.device("actor-admin1", "tok-a1")
	r.device("actor-admin2", "tok-a2")
	r.hidden["tok-a2"] = map[string]bool{testThread: true}
	r.raise(0, "", "ap1", 1)
	// A reply of a thread for nobody is never held.
	r.signal(0, substrate.PushSignal{Kind: substrate.PushSettled, Mode: "chat", Status: "ok", Message: "m1"})
	r.at(5 * time.Second)
	if len(r.sender.to("tok-a1")) != 1 || len(r.sender.to("tok-a2")) != 0 || len(r.sender.all()) != 1 {
		t.Fatalf("sent %+v", r.sender.all())
	}
	// Each admin is checked again as themself.
	var people []string
	for _, c := range r.store.checks {
		people = append(people, c.Person)
	}
	if !slices.Contains(people, "actor-admin1") || !slices.Contains(people, "actor-admin2") {
		t.Fatalf("checked %v", people)
	}
}

func TestATokenWhoseAppNamesNoRelaySendsNothing(t *testing.T) {
	cases := map[string]func(*rig){
		"no app authority": func(r *rig) { r.store.devices[testPerson][0].Token.AppAuthority = "" },
		"no client id":     func(r *rig) { r.store.devices[testPerson][0].Token.ClientID = "" },
		"a local app":      func(r *rig) { r.store.devices[testPerson][0].Token.AppLocal = true },
		"no push":          func(r *rig) { r.docs.doc.Apps[0].Push = nil },
		"another app":      func(r *rig) { r.docs.doc.Apps[0].ClientID = "https://apps.example.com/other.json" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.device(testPerson, "tok1")
			change(r)
			noRelay := outcome(substrate.PushEventRequest, outcomeNoRelay)
			r.raise(0, testPerson, "ap1", 1)
			r.at(5 * time.Second)
			if len(r.sender.all()) != 0 {
				t.Fatalf("sent %+v", r.sender.all())
			}
			if outcome(substrate.PushEventRequest, outcomeNoRelay) != noRelay+1 {
				t.Fatal("no relay was not counted")
			}
		})
	}
}

// After the relay answers: each answer that says the key will never be sent
// to again deletes the device with the key the send carried, read through a
// real client from the wire's own refusal fixtures; every other answer
// deletes nothing.
func TestTheRelaysAnswerDecidesTheDelete(t *testing.T) {
	var refusals []pushwiretest.Refusal
	pushwiretest.Read(t, "errors.json", &refusals)
	deleting := map[string]bool{
		pushwire.CodeGone: true, pushwire.CodeSignatureRefused: true, pushwire.CodeKeyRefused: true,
		pushwire.CodeKeyExpired: true, pushwire.CodeTopicRefused: true, pushwire.CodeEnvironmentRefused: true,
	}
	seen := map[string]bool{}
	for _, ref := range refusals {
		var env pushwire.ErrorResponse
		if err := json.Unmarshal(ref.Body, &env); err != nil {
			t.Fatal(err)
		}
		seen[env.Error.Code] = true
		t.Run(fmt.Sprintf("%d %s", ref.Status, env.Error.Code), func(t *testing.T) {
			relay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(ref.Status)
				_, _ = w.Write(ref.Body)
			}))
			defer relay.Close()
			r := newRig(t)
			r.docs.doc.Apps[0].Push.Relay = relay.URL
			r.d.sender = push.New(push.Options{HTTP: relay.Client()})
			r.device(testPerson, "tok1")
			r.raise(0, testPerson, "ap1", 1)
			r.at(5 * time.Second)
			want := []deleteCall(nil)
			if deleting[env.Error.Code] {
				want = []deleteCall{{"tok1", "key-tok1"}}
			}
			if !slices.Equal(r.store.deleted, want) {
				t.Fatalf("deleted %+v, want %+v", r.store.deleted, want)
			}
		})
	}
	for _, code := range pushwire.Codes {
		if !seen[code] {
			t.Errorf("errors.json carries no refusal with %s", code)
		}
	}
}

// A device deleted after the relay refused its key is forgotten with its
// alerts, and a key the store no longer holds deletes nothing.
func TestADeleteNamesTheKeyTheSendCarried(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.device(testPerson, "tok2")
	r.sender.answers["key-tok2"] = push.Answer{Outcome: push.Delete, Status: http.StatusGone, Code: pushwire.CodeGone}
	r.raise(0, testPerson, "ap1", 1)
	r.at(5 * time.Second)
	if !slices.Equal(r.store.deleted, []deleteCall{{"tok2", "key-tok2"}}) {
		t.Fatalf("deleted %+v", r.store.deleted)
	}
	if n := r.d.alerted.size(); n != 1 {
		t.Fatalf("%d alerts remembered, want tok1's alone", n)
	}
}

// alertsOf is the fingerprints the read events a device was sent seal.
func (r *rig) readsTo(device string) [][]string {
	var out [][]string
	for _, s := range r.sender.to(device) {
		if s.req.Event == pushwire.EventRead {
			out = append(out, r.opened(device, s).Alerts)
		}
	}
	return out
}

func (r *rig) alertsTo(device, event string) []sent {
	var out []sent
	for _, s := range r.sender.to(device) {
		if s.req.Event == event {
			out = append(out, s)
		}
	}
	return out
}

func TestAMarkTakesBackTheReplyItCoversButNotOnTheWritersDevice(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.device(testPerson, "tok2")
	r.message("m1", 1)
	r.reply(0, "m1")
	r.at(5 * time.Second)
	if len(r.sender.all()) != 2 {
		t.Fatalf("sent %d replies, want 2", len(r.sender.all()))
	}
	r.store.recheck = func(c substrate.PushCheck) string { return substrate.PushSeen }
	// tok1 read the thread: its own device is not told.
	r.signal(10*time.Second, substrate.PushSignal{Kind: substrate.PushMarked, Person: testPerson, Token: "tok1"})
	r.at(11 * time.Second)
	if got := r.readsTo("tok2"); len(got) != 0 {
		t.Fatalf("a read went out before its two seconds: %v", got)
	}
	r.at(12 * time.Second)
	if got := r.readsTo("tok1"); len(got) != 0 {
		t.Fatalf("the writer's own device was sent %v", got)
	}
	reply := r.alertsTo("tok2", pushwire.EventReply)[0]
	got := r.readsTo("tok2")
	if len(got) != 1 || !slices.Equal(got[0], []string{pushwire.EventFingerprint(reply.req.EventID)}) {
		t.Fatalf("tok2 was sent reads %v, want the reply's fingerprint", got)
	}
	read := r.alertsTo("tok2", pushwire.EventRead)[0]
	if read.req.Thread != testThread || r.opened("tok2", read).Thread != testThread {
		t.Fatalf("the read names %+v", read.req)
	}
	if n := r.d.alerted.size(); n != 0 {
		t.Fatalf("%d alerts still remembered", n)
	}
}

// A partial read, one that stops before the newest alerted message, covers
// only the older alert, and the newer stays remembered.
func TestAPartialReadCoversOnlyTheOlderAlert(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.message("m1", 1)
	r.message("m2", 1)
	r.reply(0, "m1")
	r.reply(time.Second, "m2")
	r.at(6 * time.Second)
	older := r.alertsTo("tok1", pushwire.EventReply)
	if len(older) != 2 {
		t.Fatalf("sent %d replies, want 2", len(older))
	}
	var first sent
	for _, s := range older {
		if r.opened("tok1", s).Item.ID == "m1" {
			first = s
		}
	}
	r.store.recheck = func(c substrate.PushCheck) string {
		if c.Message == "m1" {
			return substrate.PushSeen
		}
		return substrate.PushSend
	}
	r.signal(10*time.Second, substrate.PushSignal{Kind: substrate.PushMarked, Person: testPerson, Token: "tok-elsewhere"})
	r.at(12 * time.Second)
	got := r.readsTo("tok1")
	if len(got) != 1 || !slices.Equal(got[0], []string{pushwire.EventFingerprint(first.req.EventID)}) {
		t.Fatalf("reads %v, want m1's fingerprint alone", got)
	}
	if n := r.d.alerted.size(); n != 1 {
		t.Fatalf("%d alerts remembered, want m2's", n)
	}
}

// A decision on another device covers the request's alert, and a newer
// alert of the same card, sent before the read fires, is left: the read
// names alerts by their event ids' fingerprints, never by the card.
func TestADecidedRequestIsTakenBackAndANewerAlertOfItStays(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.raise(0, testPerson, "ap1", 1)
	r.at(5 * time.Second)
	first := r.alertsTo("tok1", pushwire.EventRequest)[0]
	// The card waits again at version 3, and that alert goes out before the
	// decision's read fires.
	r.store.recheck = func(c substrate.PushCheck) string {
		if c.Request && c.Item.Version == 1 {
			return substrate.PushSeen
		}
		return substrate.PushSend
	}
	r.raise(10*time.Second, testPerson, "ap1", 3)
	r.signal(14*time.Second, substrate.PushSignal{
		Kind: substrate.PushDecided, Person: testPerson, Token: "tok-mac",
		Item: substrate.PushItem{Kind: vocabulary.KindLLMApproval, ID: "ap1", Version: 2},
	})
	r.at(15 * time.Second)
	if n := len(r.alertsTo("tok1", pushwire.EventRequest)); n != 2 {
		t.Fatalf("sent %d requests, want 2", n)
	}
	r.at(16 * time.Second)
	got := r.readsTo("tok1")
	if len(got) != 1 || !slices.Equal(got[0], []string{pushwire.EventFingerprint(first.req.EventID)}) {
		t.Fatalf("reads %v, want the first alert's fingerprint alone", got)
	}
	if n := r.d.alerted.size(); n != 1 {
		t.Fatalf("%d alerts remembered, want the newer one", n)
	}
}

// Every source within two seconds folds into one read.
func TestReadsFoldIntoOne(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.message("m1", 1)
	r.reply(0, "m1")
	r.raise(0, testPerson, "ap1", 1)
	r.at(5 * time.Second)
	r.store.recheck = func(substrate.PushCheck) string { return substrate.PushSeen }
	r.signal(10*time.Second, substrate.PushSignal{Kind: substrate.PushMarked, Person: testPerson, Token: "tok-x"})
	r.signal(11*time.Second, substrate.PushSignal{
		Kind: substrate.PushDecided, Person: testPerson, Token: "tok-y",
		Item: substrate.PushItem{Kind: vocabulary.KindLLMApproval, ID: "ap1", Version: 2},
	})
	r.at(12 * time.Second)
	if got := r.readsTo("tok1"); len(got) != 0 {
		t.Fatalf("the folded read fired with its first source: %v", got)
	}
	r.at(13 * time.Second)
	if got := r.readsTo("tok1"); len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("reads %v, want one covering both alerts", got)
	}
}

func TestSealReadSplitsAMaximumBatch(t *testing.T) {
	signer := newFakeSigner("tok1")
	thread := strings.Repeat("t", vocabulary.MaxIDLen)
	var prints []string
	for i := range 200 {
		prints = append(prints, pushwire.EventFingerprint(fmt.Sprintf("event-%d", i)))
	}
	ids, err := SealRead(signer, thread, prints, time.Unix(1763459123, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) < 2 {
		t.Fatalf("200 fingerprints fit %d id", len(ids))
	}
	var all []string
	for _, id := range ids {
		if len(id) > pushwire.MaxEventID {
			t.Fatalf("an id of %d characters", len(id))
		}
		e, err := signer.OpenEvent(id)
		if err != nil {
			t.Fatal(err)
		}
		if e.Event != substrate.PushEventRead || e.Thread != thread || len(e.Alerts) == 0 {
			t.Fatalf("an id seals %+v", e)
		}
		all = append(all, e.Alerts...)
	}
	if !slices.Equal(all, prints) {
		t.Fatal("the ids do not cover every fingerprint, in order")
	}
	// Each id but the last is full: one more fingerprint would not fit.
	for _, id := range ids[:len(ids)-1] {
		e, _ := signer.OpenEvent(id)
		if _, err := signer.SealEvent(substrate.PushEvent{Event: e.Event, Thread: e.Thread, Alerts: append(e.Alerts, prints[0]), IssuedAt: e.IssuedAt}); err == nil {
			t.Fatalf("an id holding %d fingerprints had room for one more", len(e.Alerts))
		}
	}
}

// The waits are bounded: past MaxHolds the oldest is dropped and counted.
func TestTheHoldsAreBounded(t *testing.T) {
	r := newRig(t)
	dropped := outcome(substrate.PushEventRequest, outcomeDropped)
	for i := range MaxHolds + 1 {
		r.raise(0, testPerson, fmt.Sprintf("ap%d", i), 1)
	}
	if r.d.Holds() != MaxHolds {
		t.Fatalf("%d holds, want %d", r.d.Holds(), MaxHolds)
	}
	r.d.mu.Lock()
	_, first := r.d.holds[holdKey{
		repository: testRepo, person: testPerson, thread: testThread, event: substrate.PushEventRequest,
		item: substrate.PushItem{Kind: vocabulary.KindLLMApproval, ID: "ap0", Version: 1},
	}]
	r.d.mu.Unlock()
	if first {
		t.Fatal("the oldest hold was kept")
	}
	if outcome(substrate.PushEventRequest, outcomeDropped) != dropped+1 {
		t.Fatal("the dropped hold was not counted")
	}
}

func TestTheAlertedMapForgetsTheOldest(t *testing.T) {
	m := newAlertedMap(3)
	for i := range 5 {
		m.add(alert{repository: testRepo, thread: testThread, person: testPerson, fingerprint: fmt.Sprint(i)})
	}
	m.forget(m.of(testRepo, testThread, testPerson)[0].seq)
	m.add(alert{repository: testRepo, thread: testThread, person: testPerson, fingerprint: "5"})
	m.add(alert{repository: testRepo, thread: testThread, person: testPerson, fingerprint: "6"})
	var got []string
	for _, a := range m.of(testRepo, testThread, testPerson) {
		got = append(got, a.fingerprint)
	}
	if !slices.Equal(got, []string{"4", "5", "6"}) {
		t.Fatalf("remembered %v", got)
	}
}

// The real loop fires a hold once it is due, without a Tick.
func TestTheLoopFiresWhatIsDue(t *testing.T) {
	store := newFakeStore()
	sender := &fakeSender{answers: map[string]push.Answer{}}
	docs := &fakeDocs{doc: authority.AuthorityDocument{Authority: "example.com", Apps: []authority.AuthorityApp{
		{ClientID: testClientID, Push: &authority.AppPush{Relay: testRelay}},
	}}}
	var mu sync.Mutex
	now := time.Unix(1763459123, 0)
	clockNow := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	d := New(Options{
		View:      func(context.Context, substrate.Dataset, substrate.TokenInfo) (View, error) { return fakeView{}, nil },
		Documents: docs, Sender: sender, Now: clockNow,
	})
	store.devices[testPerson] = []substrate.PushDevice{{
		ID: "tok1", Person: testPerson, PushKey: "key-tok1",
		Notifies: []string{substrate.PushNotifiesRequests},
		Token:    substrate.TokenInfo{ID: "tok1", ClientID: testClientID, AppAuthority: "example.com"},
	}}
	d.Start(fakeDatasets{ds: store})
	defer d.Close()
	// The clock then passes its due time: the loop fires a hold already
	// overdue at once, never an hour later.
	d.PushSignal(substrate.PushSignal{
		Kind: substrate.PushRaised, Repository: testRepo, Thread: testThread, Mode: "chat",
		Person: testPerson, Item: substrate.PushItem{Kind: vocabulary.KindLLMApproval, ID: "ap1", Version: 1},
	})
	mu.Lock()
	now = now.Add(Hold + time.Second)
	mu.Unlock()
	// Woken with nothing else waiting.
	d.wake <- struct{}{}
	deadline := time.Now().Add(5 * time.Second)
	for len(sender.all()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the loop never fired the due hold")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A token revoked while its alert waits behind another delivery gets
// nothing: the devices are listed once the delivery has its slot.
func TestATokenRevokedWhileQueuedGetsNothing(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.device("actor-bo", "tok2")
	r.message("m1", 1)
	r.message("m2", 1)
	r.reply(0, "m1")
	r.signal(0, substrate.PushSignal{
		Kind: substrate.PushSettled, Mode: "chat", Person: "actor-bo", Status: "ok",
		Message: "m2", TurnStarted: r.start.Add(-time.Second),
	})
	r.sender.mu.Lock()
	r.sender.during = func(_ context.Context, req pushwire.PushRequest) {
		if req.PushKey != "key-tok1" {
			return
		}
		r.store.mu.Lock()
		delete(r.store.devices, "actor-bo")
		r.store.mu.Unlock()
	}
	r.sender.mu.Unlock()
	r.at(Hold)
	if got := r.sender.to("tok2"); len(got) != 0 {
		t.Fatalf("a token revoked while queued was sent %+v", got)
	}
	if got := r.sender.to("tok1"); len(got) != 1 {
		t.Fatalf("the first device got %d pushes", len(got))
	}
}

// A device whose token reads the thread but not the item gets no alert.
func TestATokenThatReadsTheThreadButNotTheItemGetsNothing(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.hidden["tok1"] = map[string]bool{"ap1": true}
	r.raise(0, testPerson, "ap1", 1)
	r.message("m1", 1)
	r.hidden["tok1"]["m1"] = true
	r.reply(0, "m1")
	r.at(Hold)
	if got := r.sender.all(); len(got) != 0 {
		t.Fatalf("an item the token cannot read was alerted: %+v", got)
	}
	r.store.mu.Lock()
	r.store.gone[vocabulary.KindLLMApproval+"/ap2"] = true
	r.store.mu.Unlock()
	r.raise(Hold, testPerson, "ap2", 1)
	r.at(2 * Hold)
	if got := r.sender.all(); len(got) != 0 {
		t.Fatalf("a request whose record is gone was alerted: %+v", got)
	}
}

// A `read` that covers an alert still in flight takes it back once the
// relay took it, and never before.
func TestAReadCoversAnAlertStillInFlight(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.message("m1", 1)
	r.reply(0, "m1")
	read := &hold{
		key:     holdKey{repository: testRepo, person: testPerson, thread: testThread, event: substrate.PushEventRead},
		check:   substrate.PushCheck{Event: substrate.PushEventRead, Person: testPerson, Thread: testThread},
		writers: map[string]bool{},
	}
	r.sender.mu.Lock()
	r.sender.during = func(ctx context.Context, req pushwire.PushRequest) {
		if req.Event != pushwire.EventReply {
			return
		}
		// The person reads the reply while its push is in flight.
		r.store.mu.Lock()
		r.store.recheck = func(substrate.PushCheck) string { return substrate.PushSeen }
		r.store.mu.Unlock()
		if err := r.d.deliver(ctx, fakeDatasets{ds: r.store}, read); err != nil {
			t.Error(err)
		}
		if n := len(r.readsTo("tok1")); n != 0 {
			t.Errorf("a read went out before the alert it covers: %d", n)
		}
	}
	r.sender.mu.Unlock()
	r.at(Hold)
	alerts := r.alertsTo("tok1", pushwire.EventReply)
	reads := r.readsTo("tok1")
	if len(alerts) != 1 || len(reads) != 1 || !slices.Equal(reads[0], []string{pushwire.EventFingerprint(alerts[0].req.EventID)}) {
		t.Fatalf("alerts %+v, reads %v: want the in-flight alert taken back once sent", alerts, reads)
	}
	if n := r.d.alerted.size(); n != 0 {
		t.Fatalf("%d alerts remembered after their take-back", n)
	}
}

// Close cancels a delivery in flight, waits for it, and no delivery starts
// after it returns.
func TestCloseIsABarrier(t *testing.T) {
	store := newFakeStore()
	sender := &fakeSender{answers: map[string]push.Answer{}}
	entered := make(chan struct{})
	var once sync.Once
	sender.during = func(ctx context.Context, _ pushwire.PushRequest) {
		once.Do(func() { close(entered) })
		<-ctx.Done()
	}
	start := time.Unix(1763459123, 0).UTC()
	c := &clock{t: start}
	docs := &fakeDocs{doc: authority.AuthorityDocument{Authority: "example.com", Apps: []authority.AuthorityApp{
		{ClientID: testClientID, Push: &authority.AppPush{Relay: testRelay}},
	}}}
	d := New(Options{
		View:      func(context.Context, substrate.Dataset, substrate.TokenInfo) (View, error) { return fakeView{}, nil },
		Documents: docs, Sender: sender, Now: c.now,
	})
	for i, id := range []string{"tok1", "tok2", "tok3", "tok4", "tok5", "tok6"} {
		person := fmt.Sprintf("actor-%d", i)
		store.devices[person] = []substrate.PushDevice{{
			ID: id, Person: person, PushKey: "key-" + id, Notifies: []string{substrate.PushNotifiesReplies},
			Token: substrate.TokenInfo{ID: id, ClientID: testClientID, AppAuthority: "example.com"},
		}}
		msg := fmt.Sprintf("m%d", i)
		store.messages[msg] = &substrate.Record{Kind: vocabulary.KindLLMMessage, ID: msg, Version: 1, Properties: map[string]any{"thread": testThread}}
		d.PushSignal(substrate.PushSignal{
			Kind: substrate.PushSettled, Repository: testRepo, Thread: testThread, Mode: "chat", Person: person,
			Status: "ok", Message: msg, At: start, TurnStarted: start,
		})
	}
	d.Start(fakeDatasets{ds: store})
	c.set(start.Add(Hold))
	d.fire(c.now())
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("no delivery reached its send")
	}
	closed := make(chan struct{})
	go func() { d.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return while a send waited on its context")
	}
	before := len(sender.all())
	if before > MaxInFlight {
		t.Fatalf("%d sends ran at once, past %d", before, MaxInFlight)
	}
	d.PushSignal(substrate.PushSignal{Kind: substrate.PushSettled, Repository: testRepo, Thread: testThread, Mode: "chat", Person: "actor-0", Status: "ok", Message: "m0", At: start})
	d.fire(start.Add(time.Hour))
	time.Sleep(50 * time.Millisecond)
	if after := len(sender.all()); after != before {
		t.Fatalf("a delivery ran after Close: %d sends, then %d", before, after)
	}
}

// Signals that keep coming under one key push its wait no further than
// MaxHoldDelay past the first.
func TestAHoldIsNotPushedPastItsCeiling(t *testing.T) {
	r := newRig(t)
	for i := range 20 {
		r.signal(time.Duration(i)*time.Second, substrate.PushSignal{Kind: substrate.PushMarked, Person: testPerson, Token: "tok9"})
	}
	r.d.mu.Lock()
	var due time.Time
	for _, h := range r.d.holds {
		due = h.due
	}
	r.d.mu.Unlock()
	if want := r.start.Add(MaxHoldDelay); !due.Equal(want) {
		t.Fatalf("the read is due at %v, want the ceiling %v", due, want)
	}
}

// The badge pages on past a page the view keeps little of, to 999.
func TestTheBadgePagesUntilItCounts999(t *testing.T) {
	store := newFakeStore()
	hidden := map[string]bool{}
	for i := range 1600 {
		id := fmt.Sprintf("th%05d", i)
		store.badge = append(store.badge, &substrate.Record{Kind: vocabulary.KindLLMThread, ID: id})
		if i < 550 {
			hidden[id] = true
		}
	}
	n, err := push.Badge(context.Background(), store, fakeView{hidden: hidden}.Reads, testPerson)
	if err != nil || n != substrate.PushBadgeMax {
		t.Fatalf("the badge counted %d, %v: want %d past a page the view hides", n, err, substrate.PushBadgeMax)
	}
	store.badge = store.badge[:1200]
	if n, _ := push.Badge(context.Background(), store, fakeView{hidden: hidden}.Reads, testPerson); n != 650 {
		t.Fatalf("the badge counted %d, want every readable candidate, 650", n)
	}
}

// Everything a later device's send is held to is read again after an
// earlier device's send: a token revoked, or a view that lost the thread,
// meanwhile, gets nothing; the badge each carries is counted then.
func TestEachSendReadsItsDeviceAndViewAgain(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.device(testPerson, "tok2")
	r.device(testPerson, "tok3")
	r.message("m1", 1)
	r.store.badge = []*substrate.Record{
		{Kind: vocabulary.KindLLMThread, ID: "th1"},
		{Kind: vocabulary.KindLLMThread, ID: "th2"},
	}
	r.reply(0, "m1")
	r.sender.mu.Lock()
	r.sender.during = func(_ context.Context, req pushwire.PushRequest) {
		if req.PushKey != "key-tok1" {
			return
		}
		// While the first send is slow: tok2 is revoked, and tok3's person
		// is demoted so that its view no longer reads one badge thread.
		r.store.mu.Lock()
		r.store.devices[testPerson] = slices.DeleteFunc(r.store.devices[testPerson], func(dv substrate.PushDevice) bool { return dv.ID == "tok2" })
		r.store.mu.Unlock()
		r.hidden["tok3"] = map[string]bool{"th2": true}
	}
	r.sender.mu.Unlock()
	r.at(Hold)
	if got := r.sender.to("tok2"); len(got) != 0 {
		t.Fatalf("a token revoked during an earlier send was sent %+v", got)
	}
	one, three := r.sender.to("tok1"), r.sender.to("tok3")
	if len(one) != 1 || len(three) != 1 {
		t.Fatalf("the live devices got %d and %d pushes", len(one), len(three))
	}
	if one[0].req.Badge != 2 || three[0].req.Badge != 1 {
		t.Fatalf("badges %d and %d, want each as its view counts at its send: 2 and 1", one[0].req.Badge, three[0].req.Badge)
	}
}

// A person demoted so their view no longer reads the thread between the
// signal and the delivery is sent nothing.
func TestADemotedPersonIsSentNothing(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.message("m1", 1)
	r.reply(0, "m1")
	r.hidden["tok1"] = map[string]bool{testThread: true}
	r.at(Hold)
	if got := r.sender.all(); len(got) != 0 {
		t.Fatalf("a demoted person was sent %+v", got)
	}
}

// The settle of the turn that raised a request reaches its delivery
// wherever it is: waiting, queued or being prepared.
func TestASettleReachesARequestWhereverItIs(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.device(testPerson, "tok2")
	r.raise(0, testPerson, "ap1", 1)
	item := substrate.PushItem{Kind: vocabulary.KindLLMApproval, ID: "ap1", Version: 1}
	r.sender.mu.Lock()
	r.sender.during = func(_ context.Context, req pushwire.PushRequest) {
		if req.PushKey == "key-tok1" {
			// The settle lands while the request is being delivered.
			r.signal(Hold, substrate.PushSignal{Kind: substrate.PushSettled, Mode: "chat", Person: testPerson, Status: "ok", Raised: []substrate.PushItem{item}, Wrote: "m9"})
		}
	}
	r.sender.mu.Unlock()
	r.at(Hold)
	var last substrate.PushCheck
	r.store.mu.Lock()
	for _, c := range r.store.checks {
		if c.Event == substrate.PushEventRequest {
			last = c
		}
	}
	r.store.mu.Unlock()
	if last.SettleMessage != "m9" {
		t.Fatalf("the second device's recheck read %+v, want the settle's message", last)
	}
	if r.d.Holds() != 0 {
		t.Fatal("a settle that raised a request held a reply")
	}
}

// A take-back of an alert covered in flight carries the badge as it
// stands after what covered it.
func TestATakeBackCarriesAFreshBadge(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.message("m1", 1)
	r.store.badge = []*substrate.Record{{Kind: vocabulary.KindLLMThread, ID: testThread}}
	r.reply(0, "m1")
	read := &hold{
		key:     holdKey{repository: testRepo, person: testPerson, thread: testThread, event: substrate.PushEventRead},
		check:   substrate.PushCheck{Event: substrate.PushEventRead, Person: testPerson, Thread: testThread},
		writers: map[string]bool{},
	}
	r.sender.mu.Lock()
	r.sender.during = func(ctx context.Context, req pushwire.PushRequest) {
		if req.Event != pushwire.EventReply {
			return
		}
		r.store.mu.Lock()
		r.store.recheck = func(substrate.PushCheck) string { return substrate.PushSeen }
		r.store.badge = nil
		r.store.mu.Unlock()
		if err := r.d.deliver(ctx, fakeDatasets{ds: r.store}, read); err != nil {
			t.Error(err)
		}
	}
	r.sender.mu.Unlock()
	r.at(Hold)
	var reads []sent
	for _, s := range r.sender.to("tok1") {
		if s.req.Event == pushwire.EventRead {
			reads = append(reads, s)
		}
	}
	if len(reads) != 1 || reads[0].req.Badge != 0 {
		t.Fatalf("the take-back: %+v, want one read with the fresh badge 0", reads)
	}
}

// MaxHolds bounds waiting and queued work together: past it the oldest
// queued work is dropped, and counted.
func TestTheBoundCoversQueuedWork(t *testing.T) {
	r := newRig(t)
	dropped := outcome(substrate.PushEventRequest, outcomeDropped)
	r.d.mu.Lock()
	for i := range MaxHolds {
		r.d.queue = append(r.d.queue, &hold{key: holdKey{
			repository: testRepo, event: substrate.PushEventRequest,
			item: substrate.PushItem{Kind: vocabulary.KindLLMApproval, ID: fmt.Sprintf("q%d", i)},
		}})
	}
	r.d.mu.Unlock()
	r.raise(0, testPerson, "ap-new", 1)
	r.d.mu.Lock()
	queued, held, first := len(r.d.queue), len(r.d.holds), r.d.queue[0].key.item.ID
	r.d.mu.Unlock()
	if queued+held != MaxHolds || first != "q1" {
		t.Fatalf("%d queued and %d held, the oldest queued %s: want %d in all, q0 dropped", queued, held, first, MaxHolds)
	}
	if outcome(substrate.PushEventRequest, outcomeDropped) != dropped+1 {
		t.Fatal("the dropped queued work was not counted")
	}
}

// A hold that comes due while every running worker is busy starts a worker
// of its own, while slots remain, rather than waiting for a busy one.
func TestAnIdleSlotTakesNewWork(t *testing.T) {
	store := newFakeStore()
	sender := &fakeSender{answers: map[string]push.Answer{}}
	release := make(chan struct{})
	second := make(chan struct{})
	sender.during = func(_ context.Context, req pushwire.PushRequest) {
		switch req.PushKey {
		case "key-tok0":
			<-release
		case "key-tok1":
			close(second)
		}
	}
	start := time.Unix(1763459123, 0).UTC()
	c := &clock{t: start}
	docs := &fakeDocs{doc: authority.AuthorityDocument{Authority: "example.com", Apps: []authority.AuthorityApp{
		{ClientID: testClientID, Push: &authority.AppPush{Relay: testRelay}},
	}}}
	d := New(Options{
		View:      func(context.Context, substrate.Dataset, substrate.TokenInfo) (View, error) { return fakeView{}, nil },
		Documents: docs, Sender: sender, Now: c.now,
	})
	for i := range 2 {
		person, id, msg := fmt.Sprintf("actor-%d", i), fmt.Sprintf("tok%d", i), fmt.Sprintf("m%d", i)
		store.devices[person] = []substrate.PushDevice{{
			ID: id, Person: person, PushKey: "key-" + id, Notifies: []string{substrate.PushNotifiesReplies},
			Token: substrate.TokenInfo{ID: id, ClientID: testClientID, AppAuthority: "example.com"},
		}}
		store.messages[msg] = &substrate.Record{Kind: vocabulary.KindLLMMessage, ID: msg, Version: 1, Properties: map[string]any{"thread": testThread}}
	}
	d.Start(fakeDatasets{ds: store})
	defer d.Close()
	defer close(release)
	for i := range 2 {
		person, msg := fmt.Sprintf("actor-%d", i), fmt.Sprintf("m%d", i)
		d.PushSignal(substrate.PushSignal{
			Kind: substrate.PushSettled, Repository: testRepo, Thread: testThread, Mode: "chat", Person: person,
			Status: "ok", Message: msg, At: start.Add(time.Duration(i) * time.Minute), TurnStarted: start,
		})
		c.set(start.Add(time.Duration(i)*time.Minute + Hold))
		d.fire(c.now())
	}
	select {
	case <-second:
	case <-time.After(10 * time.Second):
		t.Fatal("the second delivery waited for the busy worker")
	}
}

// Every caller of Close waits for the workers, not only the first.
func TestEveryCloseCallerWaits(t *testing.T) {
	store := newFakeStore()
	sender := &fakeSender{answers: map[string]push.Answer{}}
	entered := make(chan struct{})
	var finished atomic.Bool
	sender.during = func(ctx context.Context, _ pushwire.PushRequest) {
		close(entered)
		<-ctx.Done()
		time.Sleep(100 * time.Millisecond)
		finished.Store(true)
	}
	start := time.Unix(1763459123, 0).UTC()
	c := &clock{t: start}
	docs := &fakeDocs{doc: authority.AuthorityDocument{Authority: "example.com", Apps: []authority.AuthorityApp{
		{ClientID: testClientID, Push: &authority.AppPush{Relay: testRelay}},
	}}}
	d := New(Options{
		View:      func(context.Context, substrate.Dataset, substrate.TokenInfo) (View, error) { return fakeView{}, nil },
		Documents: docs, Sender: sender, Now: c.now,
	})
	store.devices[testPerson] = []substrate.PushDevice{{
		ID: "tok1", Person: testPerson, PushKey: "key-tok1", Notifies: []string{substrate.PushNotifiesReplies},
		Token: substrate.TokenInfo{ID: "tok1", ClientID: testClientID, AppAuthority: "example.com"},
	}}
	store.messages["m1"] = &substrate.Record{Kind: vocabulary.KindLLMMessage, ID: "m1", Version: 1, Properties: map[string]any{"thread": testThread}}
	d.Start(fakeDatasets{ds: store})
	d.PushSignal(substrate.PushSignal{Kind: substrate.PushSettled, Repository: testRepo, Thread: testThread, Mode: "chat", Person: testPerson, Status: "ok", Message: "m1", At: start, TurnStarted: start})
	c.set(start.Add(Hold))
	d.fire(c.now())
	<-entered
	var early atomic.Bool
	var callers sync.WaitGroup
	for range 3 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			d.Close()
			if !finished.Load() {
				early.Store(true)
			}
		}()
	}
	callers.Wait()
	if early.Load() {
		t.Fatal("a Close returned before the delivery in flight finished")
	}
}

// A request of a thread for nobody goes to the people who hold `admin` at
// its send: one demoted during an earlier admin's send gets nothing.
func TestAnAdminDemotedBeforeTheirSendGetsNoRequest(t *testing.T) {
	r := newRig(t)
	r.store.admins = []string{"actor-admin1", "actor-admin2"}
	r.device("actor-admin1", "tok-a1")
	r.device("actor-admin2", "tok-a2")
	r.raise(0, "", "ap1", 1)
	r.sender.mu.Lock()
	r.sender.during = func(_ context.Context, req pushwire.PushRequest) {
		if req.PushKey == "key-tok-a1" {
			r.store.mu.Lock()
			r.store.admins = []string{"actor-admin1"}
			r.store.mu.Unlock()
		}
	}
	r.sender.mu.Unlock()
	r.at(Hold)
	if got := r.sender.to("tok-a2"); len(got) != 0 {
		t.Fatalf("an admin demoted before their send got %+v", got)
	}
	if got := r.sender.to("tok-a1"); len(got) != 1 {
		t.Fatalf("the admin still holding it got %d", len(got))
	}
}

// A `read` names an alert by its fingerprint as it stands when the read
// takes it: one whose send completes during the read's recheck by its real
// fingerprint, one still being prepared never, by an empty one or at all.
func TestAReadRacingAPreparingAlertNamesOnlyRealFingerprints(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	item := substrate.PushItem{Kind: vocabulary.KindLLMMessage, ID: "m1", Version: 1}
	preparing := func() uint64 {
		return r.d.alerted.add(alert{
			device: "tok1", repository: testRepo, person: testPerson, thread: testThread, event: substrate.PushEventReply,
			item: item, check: substrate.PushCheck{Event: substrate.PushEventReply, Person: testPerson, Thread: testThread, Message: "m1"},
		})
	}
	read := func() *hold {
		return &hold{
			key:     holdKey{repository: testRepo, person: testPerson, thread: testThread, event: substrate.PushEventRead},
			check:   substrate.PushCheck{Event: substrate.PushEventRead, Person: testPerson, Thread: testThread},
			writers: map[string]bool{},
		}
	}
	// It completes during the read's recheck: the read names it as sent.
	seq := preparing()
	r.store.recheck = func(substrate.PushCheck) string {
		r.d.alerted.ready(seq, item, "fp-real")
		r.d.alerted.sent(seq, true)
		return substrate.PushSeen
	}
	if err := r.d.deliver(context.Background(), fakeDatasets{ds: r.store}, read()); err != nil {
		t.Fatal(err)
	}
	reads := r.readsTo("tok1")
	if len(reads) != 1 || !slices.Equal(reads[0], []string{"fp-real"}) {
		t.Fatalf("reads %v, want the completed alert by its real fingerprint", reads)
	}
	// It is still being prepared: the read names nothing and marks it.
	seq = preparing()
	r.store.recheck = func(substrate.PushCheck) string { return substrate.PushSeen }
	if err := r.d.deliver(context.Background(), fakeDatasets{ds: r.store}, read()); err != nil {
		t.Fatal(err)
	}
	if reads := r.readsTo("tok1"); len(reads) != 1 {
		t.Fatalf("a read of an alert still being prepared went out: %v", reads)
	}
	if !r.d.alerted.canceled(seq) {
		t.Fatal("the read did not mark the alert it covered while it was prepared")
	}
}

// A take-back whose badge cannot be counted is not sent with an older
// count: the alert stays, and the next read takes it back.
func TestATakeBackWithNoBadgeWaitsForTheNextRead(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.message("m1", 1)
	r.reply(0, "m1")
	read := func() *hold {
		return &hold{
			key:     holdKey{repository: testRepo, person: testPerson, thread: testThread, event: substrate.PushEventRead},
			check:   substrate.PushCheck{Event: substrate.PushEventRead, Person: testPerson, Thread: testThread},
			writers: map[string]bool{},
		}
	}
	r.sender.mu.Lock()
	r.sender.during = func(ctx context.Context, req pushwire.PushRequest) {
		if req.Event != pushwire.EventReply {
			return
		}
		r.store.mu.Lock()
		r.store.recheck = func(substrate.PushCheck) string { return substrate.PushSeen }
		r.store.badgeErr = errors.New("the badge could not be read")
		r.store.mu.Unlock()
		if err := r.d.deliver(ctx, fakeDatasets{ds: r.store}, read()); err != nil {
			t.Error(err)
		}
	}
	r.sender.mu.Unlock()
	r.at(Hold)
	if reads := r.readsTo("tok1"); len(reads) != 0 {
		t.Fatalf("a take-back went out with no badge counted: %v", reads)
	}
	if n := r.d.alerted.size(); n != 1 {
		t.Fatalf("%d alerts remembered, want the one left for the next read", n)
	}
	r.store.mu.Lock()
	r.store.badgeErr = nil
	r.store.mu.Unlock()
	r.sender.mu.Lock()
	r.sender.during = nil
	r.sender.mu.Unlock()
	if err := r.d.deliver(context.Background(), fakeDatasets{ds: r.store}, read()); err != nil {
		t.Fatal(err)
	}
	if reads := r.readsTo("tok1"); len(reads) != 1 {
		t.Fatalf("the next read took back %v", reads)
	}
}

// A read reads each device again at its send: one revoked during the
// read's earlier send gets nothing, and its alerts are forgotten.
func TestAReadReadsEachDeviceAgainAtItsSend(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.device(testPerson, "tok2")
	r.message("m1", 1)
	r.reply(0, "m1")
	r.at(Hold)
	if len(r.sender.all()) != 2 {
		t.Fatalf("the reply went to %d devices", len(r.sender.all()))
	}
	r.store.recheck = func(substrate.PushCheck) string { return substrate.PushSeen }
	revoked := false
	r.sender.mu.Lock()
	r.sender.during = func(_ context.Context, req pushwire.PushRequest) {
		if req.Event != pushwire.EventRead || revoked {
			return
		}
		revoked = true
		other := "tok2"
		if req.PushKey == "key-tok2" {
			other = "tok1"
		}
		r.store.mu.Lock()
		r.store.devices[testPerson] = slices.DeleteFunc(r.store.devices[testPerson], func(dv substrate.PushDevice) bool { return dv.ID == other })
		r.store.mu.Unlock()
	}
	r.sender.mu.Unlock()
	r.signal(Hold, substrate.PushSignal{Kind: substrate.PushMarked, Person: testPerson, Token: "tok9"})
	r.at(Hold + ReadHold)
	if n := len(r.readsTo("tok1")) + len(r.readsTo("tok2")); n != 1 {
		t.Fatalf("%d reads went out, want one: the other device was revoked during it", n)
	}
	if n := r.d.alerted.size(); n != 0 {
		t.Fatalf("%d alerts remembered of a taken-back and a revoked device", n)
	}
}

// A take-back reads its device again: an alert covered while in flight,
// whose token is revoked during that send, is never taken back with a
// thread and a badge to the revoked device, and its alert is forgotten.
func TestAnImmediateTakeBackSkipsADeviceRevokedDuringTheSend(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	r.message("m1", 1)
	r.reply(0, "m1")
	read := &hold{
		key:     holdKey{repository: testRepo, person: testPerson, thread: testThread, event: substrate.PushEventRead},
		check:   substrate.PushCheck{Event: substrate.PushEventRead, Person: testPerson, Thread: testThread},
		writers: map[string]bool{},
	}
	r.sender.mu.Lock()
	r.sender.during = func(ctx context.Context, req pushwire.PushRequest) {
		if req.Event != pushwire.EventReply {
			return
		}
		r.store.mu.Lock()
		r.store.recheck = func(substrate.PushCheck) string { return substrate.PushSeen }
		r.store.mu.Unlock()
		if err := r.d.deliver(ctx, fakeDatasets{ds: r.store}, read); err != nil {
			t.Error(err)
		}
		r.store.mu.Lock()
		delete(r.store.devices, testPerson)
		r.store.mu.Unlock()
	}
	r.sender.mu.Unlock()
	r.at(Hold)
	if reads := r.readsTo("tok1"); len(reads) != 0 {
		t.Fatalf("a take-back went to a device revoked during the alert's send: %v", reads)
	}
	if n := r.d.alerted.size(); n != 0 {
		t.Fatalf("%d alerts remembered of a device that went", n)
	}
}

// A `read` that needs several requests reads its device again before
// each: one revoked during the first request gets no more, and its alerts
// are forgotten.
func TestAReadOfSeveralRequestsStopsAtARevokedDevice(t *testing.T) {
	r := newRig(t)
	r.device(testPerson, "tok1")
	var prints []string
	for i := range 80 {
		seq := r.d.alerted.add(alert{
			device: "tok1", repository: testRepo, person: testPerson, thread: testThread, event: substrate.PushEventReply,
			check: substrate.PushCheck{Event: substrate.PushEventReply, Person: testPerson, Thread: testThread, Message: "m1"},
		})
		fingerprint := pushwire.EventFingerprint(fmt.Sprintf("event-%02d-%s", i, strings.Repeat("x", 40)))
		r.d.alerted.ready(seq, substrate.PushItem{Kind: vocabulary.KindLLMMessage, ID: fmt.Sprintf("m%d", i), Version: 1}, fingerprint)
		r.d.alerted.sent(seq, true)
		prints = append(prints, fingerprint)
	}
	if ids, err := SealRead(r.store.signer("tok1"), testThread, prints, r.start); err != nil || len(ids) < 2 {
		t.Fatalf("80 fingerprints seal to %d ids (%v): the test needs several requests", len(ids), err)
	}
	r.store.recheck = func(substrate.PushCheck) string { return substrate.PushSeen }
	r.sender.mu.Lock()
	r.sender.during = func(_ context.Context, req pushwire.PushRequest) {
		if req.Event != pushwire.EventRead {
			return
		}
		r.store.mu.Lock()
		delete(r.store.devices, testPerson)
		r.store.mu.Unlock()
	}
	r.sender.mu.Unlock()
	read := &hold{
		key:     holdKey{repository: testRepo, person: testPerson, thread: testThread, event: substrate.PushEventRead},
		check:   substrate.PushCheck{Event: substrate.PushEventRead, Person: testPerson, Thread: testThread},
		writers: map[string]bool{},
	}
	if err := r.d.deliver(context.Background(), fakeDatasets{ds: r.store}, read); err != nil {
		t.Fatal(err)
	}
	if reads := r.readsTo("tok1"); len(reads) != 1 {
		t.Fatalf("%d read requests went out, want the first alone: the device was revoked during it", len(reads))
	}
	if n := r.d.alerted.size(); n != 0 {
		t.Fatalf("%d alerts remembered of a device that went", n)
	}
}
