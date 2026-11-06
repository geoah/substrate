package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/providersecret"
	"github.com/geoah/substrate/internal/substrate"
)

// REFUSED KEYS. A vendor that refuses the key a call carried
// (providersecret.RefusesKey: a 401, or a 403 whose error names the key)
// meets every later call with that key the same way until someone changes
// the key. The service remembers the newest refusal per key so the operator
// defaults read says `failing` with the vendor and the status, and so the
// embed drain stops calling the key every minute. The next call the vendor
// accepts forgets it.
//
// The operator's key is one fact for the whole deployment: a refusal of an
// operator entry's key is every repository's, and every repository reads it.
// A person's own key is one repository's row, and no other repository reads
// or clears it. A refusal is also bound to the stored apiKey value it was
// made with, so a key the person writes over a refused one reads ready
// before any call.
//
// A row's key changes while calls on the old one are still out. The fold
// tells the table when a row's stored apiKey changes or the row goes away
// (txn.noteProviderKey), the table drops what it held for the row and moves
// the row's generation, and an answer to a call that read the key before
// that change is ignored. So a late refusal of a replaced key neither
// overwrites the new key's state nor brings back a deleted row's.
//
// The table is memory only, and holds at most keyTableCapacity rows: past
// it the row touched longest ago goes. A restart forgets every refusal, the
// slots read ready, and the first call after it meets the refusal again and
// records it.

const (
	// keyDrainBackoffFirst is how long the embed drain leaves a refused key
	// alone after the first refusal; each refusal after it doubles the wait,
	// up to keyDrainBackoffMax.
	keyDrainBackoffFirst = time.Minute
	keyDrainBackoffMax   = time.Hour
	// keyTableCapacity bounds the rows the table holds across every
	// repository. A row it drops reads ready until its next refusal.
	keyTableCapacity = 4096
)

// keySlot is where a key lives: an operator entry, with no repository, or
// one llm/provider row of one repository.
type keySlot struct {
	operator   string
	repository string
	row        string
}

// keyID names the key a call carries: its slot, the digest of the row's
// stored apiKey value (blank for an operator entry), and the table's
// generation when the call read the row (keyRefusals.generation). A read
// that only asks for a status leaves the generation 0.
type keyID struct {
	slot keySlot
	key  string
	gen  uint64
}

// operatorKeyID is the operator entry id's key. The operator's keys come
// from the defaults file, which a running server never reloads, so they have
// no generation.
func operatorKeyID(entry string) keyID { return keyID{slot: keySlot{operator: entry}} }

// ownKeyID is the key a row of the repository holds, by its stored apiKey
// value: a secret ref for a key written through the API, which a new key
// replaces. The digest covers the repository and the row too, so two
// repositories storing the same value never share one, and the table never
// holds key material.
func ownKeyID(repository, row, stored string) keyID {
	sum := sha256.Sum256([]byte(repository + "\x00" + row + "\x00" + stored))
	return keyID{slot: keySlot{repository: repository, row: row}, key: hex.EncodeToString(sum[:16])}
}

// keyRefusals is the service's table of refused keys.
type keyRefusals struct {
	mu sync.Mutex
	// gen counts the key changes the table was told of (changed).
	gen uint64
	// floor is the newest change generation of a row the table dropped at
	// capacity: an answer to a call older than it, on a row the table holds
	// nothing for, may be about a replaced key, and is ignored.
	floor uint64
	slots map[keySlot]*keySlotState
	// capacity overrides keyTableCapacity where it is above 0 (tests).
	capacity int
}

// keySlotState is what the table holds for one slot.
type keySlotState struct {
	// changed is the generation of the slot's newest key change; an answer
	// to a call that read the row before it is ignored.
	changed uint64
	// touched is the slot's newest change or refusal, which orders what
	// goes at capacity.
	touched time.Time
	// refusal is the newest refusal of the key the slot holds, nil for none.
	refusal *keyRefusal
}

// keyRefusal is the newest refusal of one key and the drain's backoff on it.
type keyRefusal struct {
	key    string
	status int
	// step is the drain's current wait and until when it ends.
	step  time.Duration
	until time.Time
	// probing is set while one drain's request asks the vendor again after
	// a wait ended (takeDrain), so no other drain asks beside it.
	probing bool
}

// generation is the table's generation now. A call takes it before it reads
// its row, so a change committed after the read moves the row's generation
// past it.
func (t *keyRefusals) generation() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gen
}

// refusal is k's refusal, or nil where none stands for the key k holds now.
// Called with mu held.
func (t *keyRefusals) refusal(k keyID) *keyRefusal {
	st := t.slots[k.slot]
	if st == nil || st.refusal == nil || st.refusal.key != k.key {
		return nil
	}
	return st.refusal
}

// current reports whether an answer to a call made with k is about the key
// the slot holds now: no change reached the slot since the call read it.
// Called with mu held.
func (t *keyRefusals) current(k keyID) bool {
	if k.slot.repository == "" {
		return true
	}
	if st := t.slots[k.slot]; st != nil {
		return k.gen >= st.changed
	}
	return k.gen >= t.floor
}

// changed tells the table that slot's stored key changed or its row went
// away: it forgets the slot's refusal, and answers to calls that read the
// row before now are ignored.
func (t *keyRefusals) changed(slot keySlot, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.gen++
	st := t.slot(slot)
	st.changed, st.touched, st.refusal = t.gen, now, nil
	t.bound()
}

// slot is slot's state, made where the table held none: it starts at the
// floor, the newest generation the table may have dropped for it. Called
// with mu held.
func (t *keyRefusals) slot(slot keySlot) *keySlotState {
	if t.slots == nil {
		t.slots = map[keySlot]*keySlotState{}
	}
	st := t.slots[slot]
	if st == nil {
		st = &keySlotState{changed: t.floor}
		t.slots[slot] = st
	}
	return st
}

// bound drops the slots touched longest ago until the table is within its
// capacity, keeping every operator entry's, and moves the floor past each
// dropped slot's generation. Called with mu held.
func (t *keyRefusals) bound() {
	capacity := t.capacity
	if capacity <= 0 {
		capacity = keyTableCapacity
	}
	for len(t.slots) > capacity {
		var oldest keySlot
		var at *keySlotState
		for slot, st := range t.slots {
			if slot.repository != "" && (at == nil || st.touched.Before(at.touched)) {
				oldest, at = slot, st
			}
		}
		if at == nil {
			return
		}
		t.floor = max(t.floor, at.changed)
		delete(t.slots, oldest)
	}
}

// observe records what the vendor answered a call made with k: a refusal of
// the key (providersecret.KeyRefusedError) is recorded, a success forgets
// the refusal, and any other failure (a timeout, a 429, a refused input, a
// 403 about the request, the allowance) says nothing about the key. An
// answer to a call that read a key since replaced is ignored.
func (t *keyRefusals) observe(k keyID, err error, now time.Time) {
	status := providersecret.KeyRefusal(err)
	if status == 0 && err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.current(k) {
		return
	}
	if status == 0 {
		t.accept(k)
		return
	}
	step := keyDrainBackoffFirst
	if e := t.refusal(k); e != nil {
		step = min(2*e.step, keyDrainBackoffMax)
	}
	st := t.slot(k.slot)
	st.touched = now
	st.refusal = &keyRefusal{key: k.key, status: status, step: step, until: now.Add(step)}
	t.bound()
}

// accept forgets k's refusal, and the slot with it where the slot carries no
// generation the floor does not. Called with mu held.
func (t *keyRefusals) accept(k keyID) {
	if t.refusal(k) == nil {
		return
	}
	st := t.slots[k.slot]
	st.refusal = nil
	if st.changed <= t.floor {
		delete(t.slots, k.slot)
	}
}

// status is the status the vendor refused k with, or 0 where no refusal of
// the key k holds now stands.
func (t *keyRefusals) status(k keyID) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e := t.refusal(k); e != nil {
		return e.status
	}
	return 0
}

// drainHeld reports whether the embed drain must leave k alone at now: a
// refusal stands and its wait has not ended, or another drain is asking the
// vendor. It takes nothing, so a drain asks it before reading its queue.
func (t *keyRefusals) drainHeld(k keyID, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.refusal(k)
	return e != nil && (e.probing || now.Before(e.until))
}

// takeDrain reports whether a drain may send a request with k at now, right
// before it sends it. Where a refusal stands and its wait ended, the drain
// takes the step's one probe (probe true) and must end it (endProbe) once
// the vendor answered, so the drains of other repositories on the same
// operator key wait for this one's answer rather than each calling the
// vendor.
func (t *keyRefusals) takeDrain(k keyID, now time.Time) (probe, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.refusal(k)
	switch {
	case e == nil:
		return false, true
	case e.probing || now.Before(e.until):
		return false, false
	}
	e.probing = true
	return true, true
}

// endProbe ends the probe a drain took on k, after observe recorded the
// vendor's answer: a refusal or a success replaced the probed refusal
// already, and any other answer spends the step, so the next probe waits
// for it again.
func (t *keyRefusals) endProbe(k keyID, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e := t.refusal(k); e != nil && e.probing {
		e.probing, e.until = false, now.Add(e.step)
	}
}

// noteProviderKey tells the refused-key table, once the transaction commits,
// that the fold changed an llm/provider row's stored apiKey from before to
// after, or deleted the row (gone): the table forgets the row's refusal and
// ignores answers to calls that read the row before (keyRefusals.changed). A
// replay writes no new key and tells nothing.
func (t *txn) noteProviderKey(ref eref, before, after any, gone bool) {
	if ref.Kind != typeProvider || t.replaying {
		return
	}
	if !gone {
		was, _ := before.(string)
		is, _ := after.(string)
		if was == is {
			return
		}
	}
	svc, slot := t.ds.svc, keySlot{repository: t.ds.Repository().ID, row: ref.ID}
	t.afterCommit = append(t.afterCommit, func() { svc.keyRefusals.changed(slot, svc.operatorNow()) })
}

// keyWatchClient tells the service's key table what the vendor answered each
// call made with key. It wraps the wire client inside the allowance gate, so
// a call the gate refused never reaches it.
type keyWatchClient struct {
	ds    *dataset
	key   keyID
	inner llm.Client
}

func (c *keyWatchClient) Complete(ctx context.Context, req llm.Request, onDelta func(llm.Delta)) (*llm.Result, error) {
	res, err := c.inner.Complete(ctx, req, onDelta)
	c.ds.svc.keyRefusals.observe(c.key, err, c.ds.svc.operatorNow())
	return res, err
}

// keyStatus is the status a vendor refused k with, or 0.
func (ds *dataset) keyStatus(k keyID) int { return ds.svc.keyRefusals.status(k) }

// keyRefusedText is the operator defaults read's message for a slot whose key
// the vendor refused: the vendor's name and the status.
func keyRefusedText(vendor string, status int) string {
	return fmt.Sprintf("%s refused the key (%d)", vendor, status)
}

// keyRefusedError is a model call the vendor refused for its key, as the
// person reads it: the vendor, the status and what to do. Its text carries no
// sentinel prefix and none of the vendor's words, which go to the log. It is
// substrate.ErrKeyRefused to every caller, so the API answers it 503
// `unavailable` with the reason `key_refused`: the caller's own access is not
// in question, and no scope it could ask for changes the answer.
type keyRefusedError struct{ text string }

func (e *keyRefusedError) Error() string        { return e.text }
func (e *keyRefusedError) Is(target error) bool { return target == substrate.ErrKeyRefused }

// keyRefusal is the refusal a call on pc meets when the vendor answered status
// to its key.
func (ds *dataset) keyRefusal(pc *providerConfig, status int) error {
	if pc.operator != nil {
		return &keyRefusedError{text: fmt.Sprintf("%s refused %s's key (%d). Add your own key on the %s card under Providers.",
			pc.name, ds.siteName(), status, pc.name)}
	}
	return &keyRefusedError{text: fmt.Sprintf("%s refused this key (%d). Add a working key on the %s card under Providers.",
		pc.name, status, pc.name)}
}

// vendorNames are the well-known vendors as a person writes them, keyed by a
// row's label or id in lower case; the console's PROVIDER_NAMES
// (web/console/src/lib/agent-chat.ts) is the same table.
var vendorNames = map[string]string{
	"openai":     "OpenAI",
	"anthropic":  "Anthropic",
	"gemini":     "Gemini",
	"azure":      "Azure OpenAI",
	"openrouter": "OpenRouter",
	"ollama":     "Ollama",
}

// vendorName is an llm/provider row's name as its Providers card titles it:
// the vendor's spelling where its label, or with no label its id, is a
// well-known one, else its label as written, else its id capitalised.
func vendorName(label, id string) string {
	if label = strings.TrimSpace(label); label != "" {
		if v, ok := vendorNames[strings.ToLower(label)]; ok {
			return v
		}
		return label
	}
	if v, ok := vendorNames[strings.ToLower(id)]; ok || id == "" {
		return v
	}
	r, size := utf8.DecodeRuneInString(id)
	return string(unicode.ToUpper(r)) + id[size:]
}
