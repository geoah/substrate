package pushdispatch

import (
	"slices"
	"sync"

	"github.com/geoah/substrate/internal/substrate"
)

// alert is one alert being prepared, sent or sent, remembered so a `read` can take it
// back: which device, whose and on which thread, what it was about, the
// check that says whether it is covered now, and the fingerprint of its
// event id, which the `read` names it by (pushwire.EventFingerprint).
type alert struct {
	device, repository, person, thread, event string
	item                                      substrate.PushItem
	check                                     substrate.PushCheck
	fingerprint                               string
	seq                                       uint64
	// sending is true from before the alert's request to the relay until
	// its answer. A `read` that covers it meanwhile cannot take back what
	// the device may not hold yet: it marks it takeBack, and the send takes
	// it back once the relay took it, or drop where the device is the
	// writer's own.
	sending, takeBack, drop bool
}

// alertedMap is every alert remembered, at most max, the oldest forgotten
// first. It lives in memory: after a restart no `read` is sent for what was
// alerted before it, and the app reconciles when it comes to the front.
type alertedMap struct {
	mu  sync.Mutex
	max int
	seq uint64
	// low is at or below the oldest alert still remembered: seq only
	// grows, so the eviction walks forward from it and visits each seq
	// once.
	low  uint64
	byID map[uint64]*alert
}

func newAlertedMap(max int) *alertedMap {
	return &alertedMap{max: max, low: 1, byID: map[uint64]*alert{}}
}

// add remembers a, marked sending, and answers its seq.
func (m *alertedMap) add(a alert) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	for len(m.byID) >= m.max {
		for ; m.low <= m.seq; m.low++ {
			if _, ok := m.byID[m.low]; ok {
				delete(m.byID, m.low)
				m.low++
				break
			}
		}
	}
	m.seq++
	a.seq, a.sending = m.seq, true
	m.byID[a.seq] = &a
	return a.seq
}

// sent settles the send of alert seq: kept when the relay took it, gone
// otherwise. It answers what a `read` asked of it meanwhile: take it back,
// or forget it.
func (m *alertedMap) sent(seq uint64, took bool) (takeBack, drop bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[seq]
	if !ok {
		return false, false
	}
	a.sending = false
	if !took || a.drop {
		delete(m.byID, seq)
		return false, took && a.drop
	}
	return a.takeBack, false
}

// of is a copy of the alerts remembered on thread for person, every
// person's when person is "", oldest first, those still sending included.
func (m *alertedMap) of(repository, thread, person string) []alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []alert
	for _, a := range m.byID {
		if a.repository == repository && a.thread == thread && (person == "" || a.person == person) {
			out = append(out, *a)
		}
	}
	slices.SortFunc(out, func(a, b alert) int {
		switch {
		case a.seq < b.seq:
			return -1
		case a.seq > b.seq:
			return 1
		}
		return 0
	})
	return out
}

// covered marks a `read`'s coverage of alert seq: a sent alert is answered
// true, with its fingerprint, for the `read` to take back; one still being
// prepared or sent is marked to be taken back (or forgotten, drop) when its
// send settles, and answered false.
//
// The fingerprint answered is the alert's own as it stands under the lock,
// never a copy read before it: an alert whose send completed since the
// `read` listed it is named by its real fingerprint, and one with none yet
// is never named.
func (m *alertedMap) covered(seq uint64, drop bool) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[seq]
	if !ok {
		return "", false
	}
	if a.sending {
		if drop {
			a.drop = true
		} else {
			a.takeBack = true
		}
		return "", false
	}
	if a.fingerprint == "" {
		return "", false
	}
	return a.fingerprint, true
}

// forget forgets the alerts seqs.
func (m *alertedMap) forget(seqs ...uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, seq := range seqs {
		delete(m.byID, seq)
	}
}

// ready names the event id an alert being prepared was sealed under, by
// its fingerprint, and the item at the version it was sealed at.
func (m *alertedMap) ready(seq uint64, item substrate.PushItem, fingerprint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.byID[seq]; ok {
		a.item, a.fingerprint = item, fingerprint
	}
}

// canceled reports whether a `read` covered alert seq before its post:
// it is then forgotten and never posted.
func (m *alertedMap) canceled(seq uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[seq]
	if !ok {
		return true
	}
	if a.takeBack || a.drop {
		delete(m.byID, seq)
		return true
	}
	return false
}

// forgetDevice forgets every alert of a device that was deleted.
func (m *alertedMap) forgetDevice(device string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, a := range m.byID {
		if a.device == device {
			delete(m.byID, id)
		}
	}
}

// size is how many alerts are remembered, for a test.
func (m *alertedMap) size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byID)
}
