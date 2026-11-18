package pushdispatch

import (
	"sync"

	"github.com/geoah/substrate/internal/substrate"
)

// alert is one alert the relay took, remembered so a `read` can take it
// back: which device, whose and on which thread, what it was about, the
// check that says whether it is covered now, and the fingerprint of its
// event id, which the `read` names it by (pushwire.EventFingerprint).
type alert struct {
	device, repository, person, thread, event string
	item                                      substrate.PushItem
	check                                     substrate.PushCheck
	fingerprint                               string
	seq                                       uint64
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
	byID map[uint64]alert
}

func newAlertedMap(max int) *alertedMap {
	return &alertedMap{max: max, low: 1, byID: map[uint64]alert{}}
}

func (m *alertedMap) add(a alert) {
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
	a.seq = m.seq
	m.byID[a.seq] = a
}

// of is the alerts remembered on thread for person, every person's when
// person is "", oldest first.
func (m *alertedMap) of(repository, thread, person string) []alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []alert
	for _, a := range m.byID {
		if a.repository == repository && a.thread == thread && (person == "" || a.person == person) {
			out = append(out, a)
		}
	}
	sortAlerts(out)
	return out
}

func (m *alertedMap) forget(a alert) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byID, a.seq)
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

func sortAlerts(list []alert) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].seq < list[j-1].seq; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}
