package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
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
// The defaults file is read once at start, so an entry's key never changes
// under a running server. A person's own key is one repository's row, and no
// other repository reads or clears it.
//
// A row's key changes while calls on the old one are still out, so a refusal
// is recorded against the stored apiKey value the call read (ownKeyID), and
// every reader asks about the value the row stores when it reads it. A late
// answer about a replaced value lands under that value, which the row no
// longer stores, and never reaches the new one; a refusal of the new value
// counts from the call that read it, however late the table hears of the
// change. A key written through the API is stored as a fresh secret ref, so
// a new key is a new value; the same material written again keeps the ref
// the row stored (txn.storeSecretProps), so it is the same key here too.
// The fold tells the table which value a change replaced or a delete took
// away (txn.noteProviderKey), and the table forgets that value's refusal.
// That frees the row's memory and touches no other value, so a notice that
// arrives late never drops the refusal of a different key.
//
// The table is memory only. It holds at most keyTableCapacity rows, past
// which the row whose newest refusal is oldest goes, and per row the
// refusals of at most keyRowValues stored values, past which the one
// recorded longest ago goes. A restart forgets every refusal, the slots read
// ready, and the first call after it meets the refusal again and records it.

const (
	// keyDrainBackoffFirst is how long the embed drain leaves a refused key
	// alone after the first refusal; each refusal after it doubles the wait,
	// up to keyDrainBackoffMax.
	keyDrainBackoffFirst = time.Minute
	keyDrainBackoffMax   = time.Hour
	// keyTableCapacity bounds the rows the table holds across every
	// repository. A row it drops reads ready until its next refusal.
	keyTableCapacity = 4096
	// keyRowValues bounds the stored values one row holds refusals for: the
	// value it stores now, and values it replaced whose late refusals came in
	// after the change's notice.
	keyRowValues = 4
)

// keySlot is where a key lives: an operator entry, with no repository, or
// one llm/provider row of one repository.
type keySlot struct {
	operator   string
	repository string
	row        string
}

// keyID names the key a call carries: its slot and the digest of the stored
// apiKey value the call read (blank for an operator entry).
type keyID struct {
	slot keySlot
	key  string
}

// operatorKeyID is the operator entry id's key.
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
	mu    sync.Mutex
	slots map[keySlot]*keySlotState
	// capacity overrides keyTableCapacity where it is above 0 (tests).
	capacity int
}

// keySlotState is what the table holds for one slot: the refusals of its
// stored values, by keyID.key, and when the newest of them was recorded,
// which orders what goes at capacity.
type keySlotState struct {
	touched  time.Time
	refusals map[string]*keyRefusal
}

// keyRefusal is the newest refusal of one key and the drain's backoff on it.
type keyRefusal struct {
	status int
	// at is when the refusal was recorded, which orders what goes at
	// keyRowValues.
	at time.Time
	// step is the drain's current wait and until when it ends.
	step  time.Duration
	until time.Time
	// probing is set while one drain's request asks the vendor again after
	// a wait ended (takeDrain), so no other drain asks beside it.
	probing bool
}

// refusal is k's refusal, or nil where none stands. Called with mu held.
func (t *keyRefusals) refusal(k keyID) *keyRefusal {
	if st := t.slots[k.slot]; st != nil {
		return st.refusals[k.key]
	}
	return nil
}

// drop forgets k's refusal, and its slot with it where the slot holds no
// other. Called with mu held.
func (t *keyRefusals) drop(k keyID) {
	st := t.slots[k.slot]
	if st == nil {
		return
	}
	delete(st.refusals, k.key)
	if len(st.refusals) == 0 {
		delete(t.slots, k.slot)
	}
}

// replaced tells the table that k's stored value was replaced on its row, or
// its row went away: it forgets the value's refusal.
func (t *keyRefusals) replaced(k keyID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.drop(k)
}

// bound drops refusals until the table is within its capacity: in k's slot
// the values recorded longest ago past keyRowValues, keeping k's, then the
// slots touched longest ago, keeping every operator entry's. Called with mu
// held.
func (t *keyRefusals) bound(k keyID) {
	if st := t.slots[k.slot]; st != nil {
		for len(st.refusals) > keyRowValues {
			oldest := ""
			for key, e := range st.refusals {
				if key != k.key && (oldest == "" || e.at.Before(st.refusals[oldest].at)) {
					oldest = key
				}
			}
			delete(st.refusals, oldest)
		}
	}
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
		delete(t.slots, oldest)
	}
}

// observe records what the vendor answered a call made with k: a refusal of
// the key (providersecret.KeyRefusedError) is recorded, a success forgets
// the refusal, and any other failure (a timeout, a 429, a refused input, a
// 403 about the request, the allowance) says nothing about the key.
func (t *keyRefusals) observe(k keyID, err error, now time.Time) {
	status := providersecret.KeyRefusal(err)
	if status == 0 && err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if status == 0 {
		t.drop(k)
		return
	}
	step := keyDrainBackoffFirst
	if e := t.refusal(k); e != nil {
		step = min(2*e.step, keyDrainBackoffMax)
	}
	if t.slots == nil {
		t.slots = map[keySlot]*keySlotState{}
	}
	st := t.slots[k.slot]
	if st == nil {
		st = &keySlotState{refusals: map[string]*keyRefusal{}}
		t.slots[k.slot] = st
	}
	st.touched = now
	st.refusals[k.key] = &keyRefusal{status: status, at: now, step: step, until: now.Add(step)}
	t.bound(k)
}

// status is the status the vendor refused k with, or 0 where no refusal of
// it stands.
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
// that the fold replaced an llm/provider row's stored apiKey value before
// with after, or deleted the row (after nil): the table forgets the refusal
// of before (keyRefusals.replaced). A value the row still stores, and a row
// that stored none, leave the table alone. A replay writes no new key and
// tells nothing.
func (t *txn) noteProviderKey(ref eref, before, after any) {
	if ref.Kind != typeProvider || t.replaying {
		return
	}
	was, _ := before.(string)
	is, _ := after.(string)
	if was == "" || was == is {
		return
	}
	svc, k := t.ds.svc, ownKeyID(t.ds.info.ID, ref.ID, was)
	t.afterCommit = append(t.afterCommit, func() { svc.keyRefusals.replaced(k) })
}

// providerKeyOf is the apiKey value ref stores, read in the transaction
// before a fold deletes it, so noteProviderKey can name it. It is nil for any
// other kind, and during a replay, which tells the table nothing.
func (t *txn) providerKeyOf(ref eref) (any, error) {
	if ref.Kind != typeProvider || t.replaying {
		return nil, nil
	}
	var stored sql.NullString
	err := t.row(`SELECT props->>'apiKey' FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
		ref.Kind, ref.ID).Scan(&stored)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("substrate/engine: read the apiKey of llm/provider %q: %w", ref.ID, err)
	case !stored.Valid:
		return nil, nil
	}
	return stored.String, nil
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
