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

// REFUSED KEYS. A vendor that answers 401 or 403 refused the key the call
// carried (providersecret.KeyRefusedError), and every later call with that
// key meets the same answer until someone changes the key. The service
// remembers the newest refusal per key so the operator defaults read says
// `failing` with the vendor and the status, and so the embed drain stops
// calling the key every minute. The next call the vendor accepts forgets it.
//
// The operator's key is one fact for the whole deployment: a refusal of an
// operator entry's key is every repository's, and every repository reads it.
// A person's own key is one repository's row, and no other repository reads
// or clears it. A refusal is also bound to the stored apiKey value it was
// made with, so a key the person writes over a refused one reads ready
// before any call.
//
// The table is memory only: a restart forgets every refusal, the slots read
// ready, and the first call after it meets the refusal again and records it.

const (
	// keyDrainBackoffFirst is how long the embed drain leaves a refused key
	// alone after the first refusal; each refusal after it doubles the wait,
	// up to keyDrainBackoffMax.
	keyDrainBackoffFirst = time.Minute
	keyDrainBackoffMax   = time.Hour
)

// keyID names one key a vendor can refuse: the operator entry's, with no
// repository, or a person's own on one row of one repository, with stored the
// hash of the row's stored apiKey value. A refusal is kept under keyID with
// stored blank, so a later key on the same row replaces it rather than
// standing beside it.
type keyID struct {
	operator   string
	repository string
	row        string
	stored     string
}

// operatorKeyID is the operator entry id's key.
func operatorKeyID(entry string) keyID { return keyID{operator: entry} }

// ownKeyID is the key a row of the repository holds, by its stored apiKey
// value: a secret ref for a key written through the API, which a new key
// replaces. The value is hashed so the table never holds key material.
func ownKeyID(repository, row, stored string) keyID {
	sum := sha256.Sum256([]byte(stored))
	return keyID{repository: repository, row: row, stored: hex.EncodeToString(sum[:16])}
}

func (k keyID) slot() keyID {
	k.stored = ""
	return k
}

// keyRefusals is the service's table of refused keys.
type keyRefusals struct {
	mu      sync.Mutex
	refused map[keyID]*keyRefusal
}

// keyRefusal is the newest refusal of one key and the drain's backoff on it.
type keyRefusal struct {
	stored string
	status int
	// step is the drain's current wait and until when it ends.
	step  time.Duration
	until time.Time
}

// entry is k's refusal, or nil where none stands for the key k holds now.
// Called with mu held.
func (t *keyRefusals) entry(k keyID) *keyRefusal {
	e := t.refused[k.slot()]
	if e == nil || e.stored != k.stored {
		return nil
	}
	return e
}

// observe records what the vendor answered a call made with k: a 401 or a
// 403 is a refusal, a success forgets the refusal, and any other failure
// (a timeout, a 429, a refused input, the allowance) says nothing about the
// key.
func (t *keyRefusals) observe(k keyID, err error, now time.Time) {
	switch status := providersecret.KeyRefusal(err); {
	case status != 0:
		t.refuse(k, status, now)
	case err == nil:
		t.accept(k)
	}
}

func (t *keyRefusals) refuse(k keyID, status int, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.refused == nil {
		t.refused = map[keyID]*keyRefusal{}
	}
	step := keyDrainBackoffFirst
	if e := t.entry(k); e != nil {
		step = min(2*e.step, keyDrainBackoffMax)
	}
	t.refused[k.slot()] = &keyRefusal{stored: k.stored, status: status, step: step, until: now.Add(step)}
}

func (t *keyRefusals) accept(k keyID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.entry(k) != nil {
		delete(t.refused, k.slot())
	}
}

// status is the status the vendor refused k with, or 0 where no refusal of
// the key k holds now stands.
func (t *keyRefusals) status(k keyID) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e := t.entry(k); e != nil {
		return e.status
	}
	return 0
}

// drainMay reports whether the embed drain may call k at now: no refusal
// stands, or the backoff on it ended. A drain let through after a backoff
// takes the next step's wait at once, so the drains of other repositories
// on the same operator key wait for this one's answer rather than each
// calling the vendor.
func (t *keyRefusals) drainMay(k keyID, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entry(k)
	switch {
	case e == nil:
		return true
	case now.Before(e.until):
		return false
	}
	e.until = now.Add(e.step)
	return true
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
// ErrGuard to every caller, so the API answers it 403 `guard` as it answers a
// call the spend cap or the operator's allowance refuses.
type keyRefusedError struct{ text string }

func (e *keyRefusedError) Error() string        { return e.text }
func (e *keyRefusedError) Is(target error) bool { return target == substrate.ErrGuard }

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
