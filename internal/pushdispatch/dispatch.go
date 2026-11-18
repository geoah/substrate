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
	// MaxInFlight bounds the requests to relays at once.
	MaxInFlight = 4
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
	// called, and sends run on its goroutine. For tests on a fake clock.
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

	mu       sync.Mutex
	datasets Datasets
	holds    map[holdKey]*hold
	seq      uint64
	alerted  *alertedMap
	wake     chan struct{}
	done     chan struct{}
	closed   bool
	slots    chan struct{}
	inflight sync.WaitGroup
}

var _ substrate.PushSink = (*Dispatcher)(nil)

// New builds a Dispatcher; nothing is sent until Start hands it the
// service.
func New(o Options) *Dispatcher {
	d := &Dispatcher{
		view: o.View, docs: o.Documents, sender: o.Sender, log: o.Log, now: o.Now, manual: o.Manual,
		holds: map[holdKey]*hold{}, alerted: newAlertedMap(MaxAlerted),
		wake: make(chan struct{}, 1), done: make(chan struct{}), slots: make(chan struct{}, MaxInFlight),
	}
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
	d.mu.Unlock()
	if !d.manual {
		go d.loop()
	}
}

// Close stops the loop, drops what was waiting and waits for the sends in
// flight.
func (d *Dispatcher) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	d.holds = map[holdKey]*hold{}
	close(d.done)
	d.mu.Unlock()
	d.inflight.Wait()
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
		if s.Person == "" || s.Mode != "chat" {
			return
		}
		switch {
		case s.Message != "":
			d.hold(holdKey{repository: s.Repository, person: s.Person, thread: s.Thread, event: substrate.PushEventReply,
				item: substrate.PushItem{Kind: vocabulary.KindLLMMessage, ID: s.Message}},
				now.Add(Hold), substrate.PushCheck{Event: substrate.PushEventReply, Person: s.Person, Thread: s.Thread, Message: s.Message}, "")
		case (s.Status == "error" || s.Status == "overbudget") && s.At.Sub(s.TurnStarted) > FailedAfter:
			d.hold(holdKey{repository: s.Repository, person: s.Person, thread: s.Thread, event: substrate.PushEventFailed,
				item: substrate.PushItem{Kind: vocabulary.KindLLMThread, ID: s.Thread, Version: s.ThreadVersion}},
				now.Add(Hold), substrate.PushCheck{Event: substrate.PushEventFailed, Person: s.Person, Thread: s.Thread, TurnStarted: s.TurnStarted}, "")
		}
	case substrate.PushRaised:
		d.hold(holdKey{repository: s.Repository, person: s.Person, thread: s.Thread, event: substrate.PushEventRequest, item: s.Item},
			now.Add(Hold), substrate.PushCheck{Event: substrate.PushEventRequest, Person: s.Person, Thread: s.Thread, Item: s.Item}, "")
	case substrate.PushMarked, substrate.PushDecided:
		d.hold(holdKey{repository: s.Repository, person: s.Person, thread: s.Thread, event: substrate.PushEventRead},
			now.Add(ReadHold), substrate.PushCheck{Event: substrate.PushEventRead, Person: s.Person, Thread: s.Thread}, s.Token)
	}
}

// hold starts a wait, or restarts the one already under key. Past MaxHolds
// the oldest is dropped. d.mu is held.
func (d *Dispatcher) hold(key holdKey, due time.Time, check substrate.PushCheck, writer string) {
	h, ok := d.holds[key]
	if !ok {
		if len(d.holds) >= MaxHolds {
			var oldest *hold
			for _, x := range d.holds {
				if oldest == nil || x.seq < oldest.seq {
					oldest = x
				}
			}
			delete(d.holds, oldest.key)
			count(oldest.key.event, outcomeDropped)
		}
		h = &hold{key: key, check: check, writers: map[string]bool{}}
		d.holds[key] = h
	}
	d.seq++
	h.seq, h.due = d.seq, due
	if writer != "" {
		h.writers[writer] = true
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// loop fires the holds as they come due.
func (d *Dispatcher) loop() {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		d.mu.Lock()
		next := time.Duration(-1)
		for _, h := range d.holds {
			if w := h.due.Sub(d.now()); next < 0 || w < next {
				next = w
			}
		}
		d.mu.Unlock()
		if next < 0 {
			next = time.Hour
		}
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
			d.fire(d.now(), true)
		}
	}
}

// Tick fires every hold due at now and runs its sends to the end: the
// manual dispatcher's clock.
func (d *Dispatcher) Tick(now time.Time) {
	d.fire(now, false)
	d.inflight.Wait()
}

// Holds is how many waits are pending, for a test.
func (d *Dispatcher) Holds() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.holds)
}

// fire takes every hold due at now and delivers each.
func (d *Dispatcher) fire(now time.Time, async bool) {
	d.mu.Lock()
	var due []*hold
	for k, h := range d.holds {
		if !h.due.After(now) {
			due = append(due, h)
			delete(d.holds, k)
		}
	}
	datasets := d.datasets
	d.mu.Unlock()
	if datasets == nil {
		return
	}
	slices.SortFunc(due, func(a, b *hold) int { return int(a.seq - b.seq) })
	for _, h := range due {
		d.inflight.Add(1)
		run := func() {
			defer d.inflight.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := d.deliver(ctx, datasets, h); err != nil {
				count(h.key.event, outcomeFailed)
				d.log.Warn("substrate: a push could not be decided", "event", h.key.event, "error", err)
			}
		}
		if async {
			go run()
		} else {
			run()
		}
	}
}

// deliver decides one hold and sends what it decides.
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
	thread, err := ds.Get(ctx, vocabulary.KindLLMThread, h.key.thread)
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) {
			count(h.key.event, outcomeUnreadable)
			return nil
		}
		return err
	}
	item := h.key.item
	if h.key.event == substrate.PushEventReply {
		msg, err := ds.Get(ctx, item.Kind, item.ID)
		if err != nil {
			if errors.Is(err, substrate.ErrNotFound) {
				count(h.key.event, outcomeSeen)
				return nil
			}
			return err
		}
		item.Version = msg.Version
	}
	class := substrate.PushNotifiesReplies
	if h.key.event == substrate.PushEventRequest {
		class = substrate.PushNotifiesRequests
	}
	for _, person := range recipients {
		check := h.check
		check.Person = person
		answer, err := store.PushRecheck(ctx, check)
		if err != nil {
			return err
		}
		switch answer {
		case substrate.PushSeen:
			count(h.key.event, outcomeSeen)
			continue
		case substrate.PushArchived:
			count(h.key.event, outcomeArchived)
			continue
		}
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
			d.sendTo(ctx, ds, store, dv, thread, h.key.event, item, func(eventID string) alert {
				return alert{device: dv.ID, repository: h.key.repository, person: person, thread: h.key.thread,
					event: h.key.event, item: item, check: check, fingerprint: pushwire.EventFingerprint(eventID)}
			})
		}
	}
	return nil
}

// deliverRead sends the `read` a mark or a decision causes to each device
// of the person holding an alert it now covers, but the devices of the
// tokens that wrote it, and forgets those alerts.
func (d *Dispatcher) deliverRead(ctx context.Context, ds substrate.Dataset, store substrate.PushStore, h *hold) error {
	held := d.alerted.of(h.key.repository, h.key.thread, h.key.person)
	if len(held) == 0 {
		return nil
	}
	covered := map[string][]alert{}
	for _, a := range held {
		check := a.check
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
		d.alerted.forget(a)
		if h.writers[a.device] {
			continue
		}
		covered[a.device] = append(covered[a.device], a)
	}
	if len(covered) == 0 {
		return nil
	}
	thread, err := ds.Get(ctx, vocabulary.KindLLMThread, h.key.thread)
	if err != nil && !errors.Is(err, substrate.ErrNotFound) {
		return err
	}
	byPerson := map[string][]substrate.PushDevice{}
	for _, list := range covered {
		person := list[0].person
		if _, ok := byPerson[person]; ok {
			continue
		}
		if byPerson[person], err = store.PushDevices(ctx, person); err != nil {
			return err
		}
	}
	for device, list := range covered {
		var dv *substrate.PushDevice
		for i := range byPerson[list[0].person] {
			if byPerson[list[0].person][i].ID == device {
				dv = &byPerson[list[0].person][i]
			}
		}
		if dv == nil {
			continue
		}
		prints := make([]string, len(list))
		for i, a := range list {
			prints[i] = a.fingerprint
		}
		d.sendRead(ctx, ds, store, *dv, thread, h.key.thread, prints)
	}
	return nil
}

// sendTo sends one alert to one device and remembers it once the relay
// took it.
func (d *Dispatcher) sendTo(ctx context.Context, ds substrate.Dataset, store substrate.PushStore, dv substrate.PushDevice,
	thread *substrate.Record, event string, item substrate.PushItem, remember func(eventID string) alert,
) {
	relay, signer, badge, outcome := d.prepare(ctx, ds, store, dv, thread)
	if outcome != "" {
		count(event, outcome)
		return
	}
	eventID, err := signer.SealEvent(substrate.PushEvent{Event: event, Thread: thread.ID, Item: item, IssuedAt: d.now()})
	if err != nil {
		count(event, outcomeFailed)
		d.log.Warn("substrate: a push's event could not be sealed", "event", event, "error", err)
		return
	}
	if d.send(ctx, store, dv, relay, signer, pushwire.PushRequest{
		PushKey: dv.PushKey, Event: event, Thread: thread.ID, Badge: badge, EventID: eventID,
	}) {
		d.alerted.add(remember(eventID))
	}
}

// sendRead sends the `read` events that take prints back from one device,
// as many as the fingerprints need: each event id holds what fits
// pushwire.MaxEventID.
func (d *Dispatcher) sendRead(ctx context.Context, ds substrate.Dataset, store substrate.PushStore, dv substrate.PushDevice,
	thread *substrate.Record, threadID string, prints []string,
) {
	if thread == nil {
		thread = &substrate.Record{Kind: vocabulary.KindLLMThread, ID: threadID}
	}
	relay, signer, badge, outcome := d.prepare(ctx, ds, store, dv, thread)
	if outcome != "" {
		count(substrate.PushEventRead, outcome)
		return
	}
	ids, err := SealRead(signer, threadID, prints, d.now())
	if err != nil {
		count(substrate.PushEventRead, outcomeFailed)
		d.log.Warn("substrate: a read's event could not be sealed", "error", err)
		return
	}
	for _, id := range ids {
		if !d.send(ctx, store, dv, relay, signer, pushwire.PushRequest{
			PushKey: dv.PushKey, Event: substrate.PushEventRead, Thread: threadID, Badge: badge, EventID: id,
		}) {
			return
		}
	}
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

// prepare finds what a send to one device needs: the device's token's view
// reads the thread, its app names a relay, the badge as that view counts
// it, and the token's signer. A non-empty outcome is why not.
func (d *Dispatcher) prepare(ctx context.Context, ds substrate.Dataset, store substrate.PushStore, dv substrate.PushDevice,
	thread *substrate.Record,
) (string, substrate.PushSigner, int, string) {
	view, err := d.view(ctx, ds, dv.Token)
	if err != nil {
		return "", nil, 0, outcomeUnreadable
	}
	if thread.Properties != nil {
		ok, err := view.Reads(ctx, thread)
		if err != nil || !ok {
			return "", nil, 0, outcomeUnreadable
		}
	}
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

// send posts one push under the in-flight bound and acts on the answer. It
// reports whether the relay took it.
func (d *Dispatcher) send(ctx context.Context, store substrate.PushStore, dv substrate.PushDevice, relay string,
	signer substrate.PushSigner, req pushwire.PushRequest,
) bool {
	select {
	case d.slots <- struct{}{}:
	case <-ctx.Done():
		count(req.Event, outcomeFailed)
		return false
	}
	answer := d.sender.Send(ctx, relay, signer, req)
	<-d.slots
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
