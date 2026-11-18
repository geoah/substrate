// Package pushdispatch turns the engine's push signals into pushes
// (docs/plans/push.md, section 4): it holds each event a few seconds,
// checks it again, finds the devices that may see it through the view an
// API request with each device's token gets, counts the badge, seals the
// event id, sends through internal/push, and deletes a device the relay
// refused, as the system. It remembers what it alerted so a `read` can take
// it back. Everything lives in memory and nothing is retried: a restart
// drops what was waiting, and the app reads the records when it opens.
package pushdispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/metrics"
	"github.com/geoah/substrate/internal/push"
	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	// Hold is how long a reply, a failure or a request waits before it is
	// checked again and sent.
	Hold = 5 * time.Second
	// ReadHold is how long a `read` waits, folding every source in that
	// time into one.
	ReadHold = 2 * time.Second
	// FailedAfter is how long a turn runs before its failure pushes: one
	// that fails sooner finds the person still where they sent it.
	FailedAfter = 5 * time.Second
	// MaxHolds bounds the holds waiting; past it the oldest is dropped.
	MaxHolds = 1024
	// MaxAlerted bounds the alerts remembered for a `read`; past it the
	// oldest is forgotten.
	MaxAlerted = 10000
	// MaxInFlight bounds the deliveries at once: each takes its slot before
	// it reads anything, so the database reads and the requests to relays
	// behind the holds are bounded together.
	MaxInFlight = 4
	// MaxHoldDelay bounds how far signals of one key that keep coming push
	// its wait past the first: a thread a person keeps marking still gets
	// its `read`.
	MaxHoldDelay = 4 * Hold
	// deliverTimeout bounds one delivery, its reads and its sends.
	deliverTimeout = 30 * time.Second
)

// The outcomes substrate_push_total counts.
const (
	outcomeSent       = "sent"
	outcomeSeen       = "seen"
	outcomeArchived   = "archived"
	outcomeNoDevice   = "no_device"
	outcomeNoRelay    = "no_relay"
	outcomeUnreadable = "unreadable"
	outcomeDeleted    = "deleted"
	outcomeLimited    = "limited"
	outcomeFailed     = "failed"
	outcomeDropped    = "dropped"
)

// Datasets opens a repository's dataset by its id: the service.
type Datasets interface {
	Dataset(ctx context.Context, repository string) (substrate.Dataset, error)
}

// View is what a token may see, as an API request with it would.
type View interface {
	Reads(ctx context.Context, rec *substrate.Record) (bool, error)
}

// Viewer builds the view of a request with tok over whole, the repository's
// own dataset (internal/api TokenView).
type Viewer func(ctx context.Context, whole substrate.Dataset, tok substrate.TokenInfo) (View, error)

// Sender sends one push (internal/push Client).
type Sender interface {
	Send(ctx context.Context, relay string, signer substrate.PushSigner, req pushwire.PushRequest) push.Answer
}

// Options configures a Dispatcher.
type Options struct {
	View      Viewer
	Documents push.Documents
	Sender    Sender
	Log       *slog.Logger
	Now       func() time.Time
	// Manual runs no goroutine of its own: holds fire only when Tick is
	// called, and deliveries run on its goroutine. For tests on a fake
	// clock.
	Manual bool
}

// Dispatcher is the engine's push sink.
type Dispatcher struct {
	view   Viewer
	docs   push.Documents
	sender Sender
	log    *slog.Logger
	now    func() time.Time
	manual bool

	// ctx is every delivery's parent: Close cancels it, so a send in
	// flight ends with the shutdown rather than its own timeout.
	ctx    context.Context
	cancel context.CancelFunc

	// mu guards everything below. A worker is started, and counted in
	// workers, only under it and only while not closed, so Close's wait
	// covers every delivery that will ever start.
	mu       sync.Mutex
	datasets Datasets
	holds    map[holdKey]*hold
	seq      uint64
	// queue is the work due and not yet taken; holds and queue together
	// stay within MaxHolds. running workers are alive, busy of them
	// delivering.
	queue   []*hold
	workers sync.WaitGroup
	running int
	busy    int
	// settled is the message the settle of the turn that raised a request
	// left, by the request's record: what a person reads the request at,
	// consulted at every recheck, so it reaches a hold, queued work, a
	// delivery being prepared and a remembered alert alike.
	settled      map[settleKey]string
	settledOrder []settleKey
	alerted      *alertedMap
	wake         chan struct{}
	done         chan struct{}
	closed       bool
}

// settleKey names a request's record in one repository.
type settleKey struct{ repository, kind, id string }

var _ substrate.PushSink = (*Dispatcher)(nil)

// New builds a Dispatcher; nothing is sent until Start hands it the
// service.
func New(o Options) *Dispatcher {
	d := &Dispatcher{
		view: o.View, docs: o.Documents, sender: o.Sender, log: o.Log, now: o.Now, manual: o.Manual,
		holds: map[holdKey]*hold{}, settled: map[settleKey]string{}, alerted: newAlertedMap(MaxAlerted),
		wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	d.ctx, d.cancel = context.WithCancel(context.Background())
	if d.log == nil {
		d.log = slog.Default()
	}
	if d.now == nil {
		d.now = time.Now
	}
	return d
}

// Start hands the dispatcher the service whose datasets it reads, and
// starts its loop unless it is manual.
func (d *Dispatcher) Start(datasets Datasets) {
	d.mu.Lock()
	d.datasets = datasets
	closed := d.closed
	d.mu.Unlock()
	if !d.manual && !closed {
		go d.loop()
	}
}

// Close stops the loop, drops what was waiting, cancels the deliveries in
// flight and waits for them: after it returns, from any caller, no delivery
// runs and none starts.
func (d *Dispatcher) Close() {
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		d.holds = map[holdKey]*hold{}
		d.queue = nil
		close(d.done)
	}
	d.mu.Unlock()
	d.cancel()
	d.workers.Wait()
}

// holdKey is what one wait is keyed by: a reply, a failure and a request by
// their recipient, thread, event and item, so one person reading cannot
// cancel another's alert and a second item on one thread is its own wait; a
// `read` by its recipient and thread alone, so every source folds into one.
type holdKey struct {
	repository, person, thread, event string
	item                              substrate.PushItem
}

// hold is one event waiting.
type hold struct {
	key   holdKey
	due   time.Time
	first time.Time
	seq   uint64
	check substrate.PushCheck
	// writers are the tokens whose writes caused a `read`, whose own
	// devices are never sent it.
	writers map[string]bool
}

// PushSignal is the engine's hand-off. It never blocks on anything but the
// dispatcher's own lock.
func (d *Dispatcher) PushSignal(s substrate.PushSignal) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	now := d.now()
	switch s.Kind {
	case substrate.PushSettled:
		if len(s.Raised) > 0 {
			// The turn raised requests: they push, and no reply and no
			// failure does, even for a request decided before the settle.
			// The person reads them at the message the settle left.
			for _, item := range s.Raised {
				d.settle(settleKey{s.Repository, item.Kind, item.ID}, s.Wrote)
			}
			return
		}
		if s.Person == "" || s.Mode != "chat" {
			return
		}
		switch {
		case s.Message != "":
			d.hold(holdKey{
				repository: s.Repository, person: s.Person, thread: s.Thread, event: substrate.PushEventReply,
				item: substrate.PushItem{Kind: vocabulary.KindLLMMessage, ID: s.Message},
			}, now, Hold, substrate.PushCheck{Event: substrate.PushEventReply, Person: s.Person, Thread: s.Thread, Message: s.Message}, "")
		case (s.Status == "error" || s.Status == "overbudget") && s.At.Sub(s.TurnStarted) > FailedAfter:
			d.hold(holdKey{
				repository: s.Repository, person: s.Person, thread: s.Thread, event: substrate.PushEventFailed,
				item: substrate.PushItem{Kind: vocabulary.KindLLMThread, ID: s.Thread, Version: s.ThreadVersion},
			}, now, Hold, substrate.PushCheck{Event: substrate.PushEventFailed, Person: s.Person, Thread: s.Thread, SettledAt: s.At}, "")
		}
	case substrate.PushRaised:
		d.hold(holdKey{repository: s.Repository, person: s.Person, thread: s.Thread, event: substrate.PushEventRequest, item: s.Item},
			now, Hold, substrate.PushCheck{Event: substrate.PushEventRequest, Person: s.Person, Thread: s.Thread, Item: s.Item}, "")
	case substrate.PushMarked, substrate.PushDecided:
		d.hold(holdKey{repository: s.Repository, person: s.Person, thread: s.Thread, event: substrate.PushEventRead},
			now, ReadHold, substrate.PushCheck{Event: substrate.PushEventRead, Person: s.Person, Thread: s.Thread}, s.Token)
	}
}

// settle records the message a request is read at, the oldest forgotten
// past MaxAlerted. d.mu is held.
func (d *Dispatcher) settle(key settleKey, message string) {
	if _, held := d.settled[key]; !held {
		if len(d.settledOrder) >= MaxAlerted {
			delete(d.settled, d.settledOrder[0])
			d.settledOrder = d.settledOrder[1:]
		}
		d.settledOrder = append(d.settledOrder, key)
	}
	d.settled[key] = message
}

// checkOf is a check as it stands now: for a request, with the message the
// settle of the turn that raised it left, wherever the check was copied.
func (d *Dispatcher) checkOf(repository string, c substrate.PushCheck) substrate.PushCheck {
	if c.Event != substrate.PushEventRequest {
		return c
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if m, ok := d.settled[settleKey{repository, c.Item.Kind, c.Item.ID}]; ok {
		c.SettleMessage = m
	}
	return c
}

// hold starts a wait of wait from now, or restarts the one already under
// key, never past MaxHoldDelay after its first signal. Past MaxHolds, held
// and queued together, the oldest is dropped. d.mu is held.
func (d *Dispatcher) hold(key holdKey, now time.Time, wait time.Duration, check substrate.PushCheck, writer string) {
	h, ok := d.holds[key]
	if !ok {
		if len(d.holds)+len(d.queue) >= MaxHolds {
			d.dropOldest()
		}
		h = &hold{key: key, first: now, check: check, writers: map[string]bool{}}
		d.holds[key] = h
	}
	d.seq++
	h.seq, h.due = d.seq, now.Add(wait)
	if ceiling := h.first.Add(MaxHoldDelay); h.due.After(ceiling) {
		h.due = ceiling
	}
	if writer != "" {
		h.writers[writer] = true
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// dropOldest drops the oldest work: queued work was due first, then the
// oldest hold. d.mu is held.
func (d *Dispatcher) dropOldest() {
	if len(d.queue) > 0 {
		count(d.queue[0].key.event, outcomeDropped)
		d.queue = d.queue[1:]
		return
	}
	var oldest *hold
	for _, x := range d.holds {
		if oldest == nil || x.seq < oldest.seq {
			oldest = x
		}
	}
	if oldest != nil {
		delete(d.holds, oldest.key)
		count(oldest.key.event, outcomeDropped)
	}
}

// loop fires the holds as they come due.
func (d *Dispatcher) loop() {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		// An hour when nothing waits; a hold already due fires at once.
		d.mu.Lock()
		next, now := time.Hour, d.now()
		for _, h := range d.holds {
			next = min(next, h.due.Sub(now))
		}
		d.mu.Unlock()
		timer.Reset(max(next, 0))
		select {
		case <-d.done:
			return
		case <-d.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
			d.fire(d.now())
		}
	}
}

// Tick fires every hold due at now and runs its deliveries to the end, on
// the caller's goroutine: the manual dispatcher's clock.
func (d *Dispatcher) Tick(now time.Time) {
	d.fire(now)
	d.workers.Wait()
}

// Holds is how many waits are pending, for a test.
func (d *Dispatcher) Holds() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.holds)
}

// fire queues every hold due at now and starts a worker for each queued
// item no idle worker will take, at most MaxInFlight alive.
func (d *Dispatcher) fire(now time.Time) {
	d.mu.Lock()
	if d.closed || d.datasets == nil {
		d.mu.Unlock()
		return
	}
	var due []*hold
	for k, h := range d.holds {
		if !h.due.After(now) {
			due = append(due, h)
			delete(d.holds, k)
		}
	}
	slices.SortFunc(due, func(a, b *hold) int {
		switch {
		case a.seq < b.seq:
			return -1
		case a.seq > b.seq:
			return 1
		}
		return 0
	})
	d.queue = append(d.queue, due...)
	if d.manual {
		// One worker, on the caller's goroutine.
		start := d.running == 0 && len(d.queue) > 0
		if start {
			d.running++
			d.workers.Add(1)
		}
		d.mu.Unlock()
		if start {
			d.work()
		}
		return
	}
	for d.running < MaxInFlight && d.running-d.busy < len(d.queue) {
		d.running++
		d.workers.Add(1)
		go d.work()
	}
	d.mu.Unlock()
}

// work delivers queued holds until none are left: one slot of
// MaxInFlight, taken before anything is read.
func (d *Dispatcher) work() {
	defer d.workers.Done()
	for {
		d.mu.Lock()
		if d.closed || len(d.queue) == 0 {
			d.running--
			d.mu.Unlock()
			return
		}
		h := d.queue[0]
		d.queue = d.queue[1:]
		d.busy++
		datasets := d.datasets
		d.mu.Unlock()
		ctx, cancel := context.WithTimeout(d.ctx, deliverTimeout)
		if err := d.deliver(ctx, datasets, h); err != nil {
			count(h.key.event, outcomeFailed)
			d.log.Warn("substrate: a push could not be decided", "event", h.key.event, "error", err)
		}
		cancel()
		d.mu.Lock()
		d.busy--
		d.mu.Unlock()
	}
}

// deliver decides one hold and sends what it decides, device by device:
// which devices to try is read once, and everything each send is held to is
// read again right before it (sendAlert), so a revocation during an earlier
// device's send holds the later one.
func (d *Dispatcher) deliver(ctx context.Context, datasets Datasets, h *hold) error {
	ds, err := datasets.Dataset(ctx, h.key.repository)
	if err != nil {
		return err
	}
	store, ok := ds.(substrate.PushStore)
	if !ok {
		return errors.New("the dataset keeps no devices")
	}
	if h.key.event == substrate.PushEventRead {
		return d.deliverRead(ctx, ds, store, h)
	}
	recipients := []string{h.key.person}
	if h.key.person == "" {
		if h.key.event != substrate.PushEventRequest {
			return nil
		}
		if recipients, err = store.PushAdmins(ctx); err != nil {
			return err
		}
	}
	class := substrate.PushNotifiesReplies
	if h.key.event == substrate.PushEventRequest {
		class = substrate.PushNotifiesRequests
	}
	for _, person := range recipients {
		devices, err := store.PushDevices(ctx, person)
		if err != nil {
			return err
		}
		devices = slices.DeleteFunc(devices, func(dv substrate.PushDevice) bool { return !slices.Contains(dv.Notifies, class) })
		if len(devices) == 0 {
			count(h.key.event, outcomeNoDevice)
			continue
		}
		for _, dv := range devices {
			if err := d.sendAlert(ctx, ds, store, h, person, class, dv.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// sendAlert sends one alert to one device. It is remembered from before
// anything is read, so a `read` that comes while it is prepared or sent
// covers it; then the device, its token and its person are read again, the
// recheck runs, the thread and the item are read live and the token's view
// must read both, and coverage is asked once more right before the post.
// Once the relay took it, a `read` that covered it meanwhile takes it back
// with a badge counted then.
func (d *Dispatcher) sendAlert(ctx context.Context, ds substrate.Dataset, store substrate.PushStore, h *hold, person, class, deviceID string) error {
	event := h.key.event
	base := h.check
	base.Person = person
	seq := d.alerted.add(alert{
		device: deviceID, repository: h.key.repository, person: person, thread: h.key.thread,
		event: event, item: h.key.item, check: base,
	})
	settled := false
	defer func() {
		if !settled {
			d.alerted.sent(seq, false)
		}
	}()
	// A request that acts for nobody goes to the people who hold `admin`
	// at its send, not at its hold: one demoted meanwhile gets nothing.
	if h.key.person == "" {
		admins, err := store.PushAdmins(ctx)
		if err != nil {
			return err
		}
		if !slices.Contains(admins, person) {
			count(event, outcomeUnreadable)
			return nil
		}
	}
	devices, err := store.PushDevices(ctx, person)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(devices, func(dv substrate.PushDevice) bool { return dv.ID == deviceID && slices.Contains(dv.Notifies, class) })
	if i < 0 {
		count(event, outcomeNoDevice)
		return nil
	}
	dv := devices[i]
	recheck := func() (bool, error) {
		answer, err := store.PushRecheck(ctx, d.checkOf(h.key.repository, base))
		switch {
		case err != nil:
			return false, err
		case answer == substrate.PushSeen:
			count(event, outcomeSeen)
		case answer == substrate.PushArchived:
			count(event, outcomeArchived)
		default:
			return true, nil
		}
		return false, nil
	}
	if ok, err := recheck(); err != nil || !ok {
		return err
	}
	thread, record, item, why, err := d.records(ctx, ds, h)
	if err != nil {
		return err
	}
	if why != "" {
		count(event, why)
		return nil
	}
	view, err := d.view(ctx, ds, dv.Token)
	if err != nil {
		count(event, outcomeUnreadable)
		return nil
	}
	for _, rec := range []*substrate.Record{thread, record} {
		if ok, err := view.Reads(ctx, rec); err != nil || !ok {
			count(event, outcomeUnreadable)
			return nil
		}
	}
	relay, signer, badge, outcome := d.prepare(ctx, store, dv, view)
	if outcome != "" {
		count(event, outcome)
		return nil
	}
	eventID, err := signer.SealEvent(substrate.PushEvent{Event: event, Thread: thread.ID, Item: item, IssuedAt: d.now()})
	if err != nil {
		count(event, outcomeFailed)
		d.log.Warn("substrate: a push's event could not be sealed", "event", event, "error", err)
		return nil
	}
	fingerprint := pushwire.EventFingerprint(eventID)
	d.alerted.ready(seq, item, fingerprint)
	// Coverage once more, right before the post: a mark that committed
	// while this was prepared, or a `read` that marked it.
	if d.alerted.canceled(seq) {
		count(event, outcomeSeen)
		return nil
	}
	if ok, err := recheck(); err != nil || !ok {
		return err
	}
	took := d.send(ctx, store, dv, relay, signer, pushwire.PushRequest{
		PushKey: dv.PushKey, Event: event, Thread: thread.ID, Badge: badge, EventID: eventID,
	})
	settled = true
	if takeBack, _ := d.alerted.sent(seq, took); takeBack {
		// Covered while it was in flight: take it back now, with the badge
		// as a view built now counts it after what covered it. The wire
		// carries a badge on every push, so where it cannot be counted the
		// take-back waits for the next `read`, which finds the alert sent.
		fresh, err := d.view(ctx, ds, dv.Token)
		if err != nil {
			return nil
		}
		badge, err := push.Badge(ctx, store, fresh.Reads, dv.Person)
		if err != nil {
			d.log.Warn("substrate: a take-back's badge could not be counted; the next read takes it back", "error", err)
			return nil
		}
		if d.sendReadIDs(ctx, store, dv, relay, signer, h.key.thread, []string{fingerprint}, badge) {
			d.alerted.forget(seq)
		}
	}
	return nil
}

// records reads what an alert is held to, live: the thread, and the item
// (the thread itself for a failure), with the item at the version an alert
// names. why is the outcome when either is gone.
func (d *Dispatcher) records(ctx context.Context, ds substrate.Dataset, h *hold) (thread, record *substrate.Record, item substrate.PushItem, why string, err error) {
	item = h.key.item
	thread, err = ds.Get(ctx, vocabulary.KindLLMThread, h.key.thread)
	if errors.Is(err, substrate.ErrNotFound) {
		return nil, nil, item, outcomeUnreadable, nil
	}
	if err != nil {
		return nil, nil, item, "", err
	}
	if h.key.event == substrate.PushEventFailed {
		return thread, thread, item, "", nil
	}
	record, err = ds.Get(ctx, item.Kind, item.ID)
	if errors.Is(err, substrate.ErrNotFound) {
		return nil, nil, item, outcomeSeen, nil
	}
	if err != nil {
		return nil, nil, item, "", err
	}
	if h.key.event == substrate.PushEventReply {
		item.Version = record.Version
	}
	return thread, record, item, "", nil
}

// deliverRead sends the `read` a mark or a decision causes to each device
// holding an alert it now covers, but the devices of the tokens that wrote
// it, and forgets each alert once the relay took the `read` naming it. An
// alert still being prepared or sent is marked instead, and its own
// delivery drops it or takes it back. A `read` names only fingerprints the
// device holds, so no view of the thread is asked for.
func (d *Dispatcher) deliverRead(ctx context.Context, ds substrate.Dataset, store substrate.PushStore, h *hold) error {
	held := d.alerted.of(h.key.repository, h.key.thread, h.key.person)
	if len(held) == 0 {
		return nil
	}
	type take struct {
		seq         uint64
		fingerprint string
	}
	covered := map[string][]take{}
	persons := map[string]string{}
	for _, a := range held {
		check := d.checkOf(a.repository, a.check)
		if a.event == substrate.PushEventRequest {
			check.Request = true
		}
		answer, err := store.PushRecheck(ctx, check)
		if err != nil {
			return err
		}
		if answer == substrate.PushSend {
			continue
		}
		writer := h.writers[a.device]
		fingerprint, sent := d.alerted.covered(a.seq, writer)
		if !sent {
			continue
		}
		if writer {
			d.alerted.forget(a.seq)
			continue
		}
		covered[a.device] = append(covered[a.device], take{a.seq, fingerprint})
		persons[a.device] = a.person
	}
	for device, list := range covered {
		seqs := make([]uint64, len(list))
		prints := make([]string, len(list))
		for i, a := range list {
			seqs[i], prints[i] = a.seq, a.fingerprint
		}
		// The device as it stands at this send, not as an earlier send of
		// this `read` found it.
		found, err := store.PushDevices(ctx, persons[device])
		if err != nil {
			return err
		}
		i := slices.IndexFunc(found, func(dv substrate.PushDevice) bool { return dv.ID == device })
		if i < 0 {
			// The device went: nothing of it is left to take back.
			d.alerted.forget(seqs...)
			continue
		}
		dv := found[i]
		relay, signer, badge, outcome := d.prepareRead(ctx, ds, store, dv)
		if outcome != "" {
			count(substrate.PushEventRead, outcome)
			continue
		}
		if d.sendReadIDs(ctx, store, dv, relay, signer, h.key.thread, prints, badge) {
			d.alerted.forget(seqs...)
		}
	}
	return nil
}

// SealRead seals prints into as few `read` event ids as hold them, each
// within pushwire.MaxEventID: the fingerprints are taken in order and an
// event closed when the next would not fit.
func SealRead(signer substrate.PushSigner, thread string, prints []string, now time.Time) ([]string, error) {
	var ids []string
	for start := 0; start < len(prints); {
		end := start + 1
		id, err := signer.SealEvent(substrate.PushEvent{Event: substrate.PushEventRead, Thread: thread, Alerts: prints[start:end], IssuedAt: now})
		if err != nil {
			return nil, err
		}
		for end < len(prints) {
			next, err := signer.SealEvent(substrate.PushEvent{Event: substrate.PushEventRead, Thread: thread, Alerts: prints[start : end+1], IssuedAt: now})
			if err != nil {
				break
			}
			id, end = next, end+1
		}
		ids = append(ids, id)
		start = end
	}
	return ids, nil
}

// prepare finds what a send to one device needs beside its view: its app
// names a relay, the badge as the view counts it now, and the token's
// signer. A non-empty outcome is why not.
func (d *Dispatcher) prepare(ctx context.Context, store substrate.PushStore, dv substrate.PushDevice, view View) (string, substrate.PushSigner, int, string) {
	relay, err := push.Relay(ctx, d.docs, dv.Token)
	switch {
	case errors.Is(err, push.ErrNoRelay):
		return "", nil, 0, outcomeNoRelay
	case err != nil:
		d.log.Warn("substrate: a push's relay could not be read", "error", err)
		return "", nil, 0, outcomeFailed
	}
	badge, err := push.Badge(ctx, store, view.Reads, dv.Person)
	if err != nil {
		return "", nil, 0, outcomeFailed
	}
	signer, err := store.PushSigner(ctx, dv.ID)
	if err != nil {
		return "", nil, 0, outcomeFailed
	}
	return relay, signer, badge, ""
}

// prepareRead is prepare for a `read`, which names no thread its view must
// read: the view counts the badge alone.
func (d *Dispatcher) prepareRead(ctx context.Context, ds substrate.Dataset, store substrate.PushStore, dv substrate.PushDevice) (string, substrate.PushSigner, int, string) {
	view, err := d.view(ctx, ds, dv.Token)
	if err != nil {
		return "", nil, 0, outcomeUnreadable
	}
	return d.prepare(ctx, store, dv, view)
}

// sendReadIDs sends the `read` events that take prints back from one
// device, sealed as SealRead splits them, and reports whether the relay
// took every one.
func (d *Dispatcher) sendReadIDs(ctx context.Context, store substrate.PushStore, dv substrate.PushDevice, relay string,
	signer substrate.PushSigner, thread string, prints []string, badge int,
) bool {
	ids, err := SealRead(signer, thread, prints, d.now())
	if err != nil {
		count(substrate.PushEventRead, outcomeFailed)
		d.log.Warn("substrate: a read's event could not be sealed", "error", err)
		return false
	}
	for _, id := range ids {
		if !d.send(ctx, store, dv, relay, signer, pushwire.PushRequest{
			PushKey: dv.PushKey, Event: substrate.PushEventRead, Thread: thread, Badge: badge, EventID: id,
		}) {
			return false
		}
	}
	return true
}

// send posts one push and acts on the answer: the caller holds a delivery
// slot. It reports whether the relay took it.
func (d *Dispatcher) send(ctx context.Context, store substrate.PushStore, dv substrate.PushDevice, relay string,
	signer substrate.PushSigner, req pushwire.PushRequest,
) bool {
	if ctx.Err() != nil {
		count(req.Event, outcomeFailed)
		return false
	}
	answer := d.sender.Send(ctx, relay, signer, req)
	host := relayHost(relay)
	switch answer.Outcome {
	case push.Sent:
		count(req.Event, outcomeSent)
		d.log.Debug("substrate: a push was sent", "event", req.Event, "relay", host)
		return true
	case push.Delete:
		deleted, err := store.DeletePushDevice(ctx, dv.ID, dv.PushKey)
		switch {
		case err != nil:
			count(req.Event, outcomeFailed)
			d.log.Warn("substrate: a device the relay refused could not be deleted", "device", dv.ID, "code", answer.Code, "error", err)
		case deleted:
			count(req.Event, outcomeDeleted)
			d.alerted.forgetDevice(dv.ID)
			d.log.Info("substrate: a device the relay refused was deleted", "device", dv.ID, "code", answer.Code)
		default:
			count(req.Event, outcomeFailed)
		}
	case push.Limited:
		count(req.Event, outcomeLimited)
		d.log.Warn("substrate: a push relay limited a push", "relay", host, "status", answer.Status, "code", answer.Code)
	default:
		count(req.Event, outcomeFailed)
		attrs := []any{"relay", host, "status", answer.Status, "code", answer.Code}
		if answer.Err != nil {
			attrs = append(attrs, "error", answer.Err)
		}
		d.log.Warn("substrate: a push relay failed", attrs...)
	}
	return false
}

// relayHost is the host a log line names a relay by.
func relayHost(relay string) string {
	if u, err := url.Parse(relay); err == nil {
		return u.Host
	}
	return ""
}

// count adds one to substrate_push_total.
func count(event, outcome string) {
	metrics.PushTotal.WithLabelValues(event, outcome).Inc()
}

// String is a hold key for a log or a test.
func (k holdKey) String() string {
	return fmt.Sprintf("%s %s %s", k.event, k.thread, k.item.ID)
}
