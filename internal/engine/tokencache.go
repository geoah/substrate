package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// tokenCache holds the token lookups this process answered, so a request
// costs no read of the token table. One process writes a database
// (decision 0083), and every write of a token record folds here, so the
// process sees every token change.
//
// THE RULE: from the moment a transaction that deletes or changes a token is
// about to commit until the database has proved that it committed or rolled
// back, the cache neither answers from nor stores a token of that repository,
// and authentication reads the table. The transaction fences its repository
// before its COMMIT (fence) and lifts the fence once the outcome is known
// (unfence): when the COMMIT answered, when no COMMIT was sent, or, for a
// COMMIT whose answer was lost, once the changelog lock that transaction held
// is granted to a waiter (settleTokens). Elapsed time proves nothing, and
// neither does an error while looking for the proof: a fence whose proof
// does not come stands, and costs reads. A lookup that began before a fence
// or an unfence is never cached, or it could put back what the fence took
// away. tokenCacheTTL is the backstop for a change the process did not make.
type tokenCache struct {
	mu      sync.Mutex
	gen     uint64
	entries map[string]tokenEntry
	// fences counts, per repository id, the token-moving transactions whose
	// outcome is not proved yet.
	fences map[string]int
}

type tokenEntry struct {
	repo Repository
	info substrate.TokenInfo
	at   time.Time
}

const (
	// tokenCacheTTL bounds how long a lookup answers without a read.
	tokenCacheTTL = time.Minute
	// tokenCacheMax bounds the entries; past it the cache starts again.
	tokenCacheMax = 4096
)

// get answers a cached lookup still inside its TTL, and the generation a
// lookup started now must carry to be cached.
func (c *tokenCache) get(hash string, now time.Time) (tokenEntry, bool, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[hash]
	if ok && now.Sub(e.at) >= tokenCacheTTL {
		delete(c.entries, hash)
		ok = false
	}
	return e, ok, c.gen
}

// put caches a lookup that started at generation gen, unless the cache was
// emptied since.
func (c *tokenCache) put(hash string, gen uint64, e tokenEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen || c.fences[e.repo.ID] > 0 {
		return
	}
	if c.entries == nil || len(c.entries) >= tokenCacheMax {
		c.entries = map[string]tokenEntry{}
	}
	c.entries[hash] = e
}

// empty drops every entry and refuses every lookup in flight.
func (c *tokenCache) empty() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.entries = nil
}

// fence drops the repository's entries and caches none of its tokens until
// unfence: the transaction about to commit may end a token, and a lookup that
// read it before the commit would otherwise keep it for the TTL.
func (c *tokenCache) fence(repository string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fences == nil {
		c.fences = map[string]int{}
	}
	c.fences[repository]++
	c.gen++
	c.drop(repository)
}

// unfence lifts one fence of the repository once its transaction's outcome
// is proved, and drops its entries again, so no lookup that began under the
// fence is kept.
func (c *tokenCache) unfence(repository string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fences[repository]--; c.fences[repository] <= 0 {
		delete(c.fences, repository)
	}
	c.gen++
	c.drop(repository)
}

// forget drops the repository's entries and refuses every lookup in flight,
// after a replay rewrote its tokens where no fence was owed (rebuild.go,
// repodir.go refoldFromFiles).
func (c *tokenCache) forget(repository string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.drop(repository)
}

// fenced answers whether a fence of the repository stands.
func (c *tokenCache) fenced(repository string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fences[repository] > 0
}

// cached answers whether a lookup of hash is cached, for a test.
func (c *tokenCache) cached(hash string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entries[hash]
	return ok
}

// drop removes the repository's entries. The caller holds mu.
func (c *tokenCache) drop(repository string) {
	for hash, e := range c.entries {
		if e.repo.ID == repository {
			delete(c.entries, hash)
		}
	}
}

// cachedTokenByHash is tokenByHash through the cache. A cached token past its
// expiry is refused as tokenByHash refuses it.
func (s *service) cachedTokenByHash(ctx context.Context, hash string) (Repository, substrate.TokenInfo, error) {
	now := nowUTC()
	e, ok, gen := s.tokens.get(hash, now)
	if ok {
		if e.info.ExpiresAt != nil && now.After(*e.info.ExpiresAt) {
			return Repository{}, substrate.TokenInfo{}, fmt.Errorf("%w: token expired", substrate.ErrAuth)
		}
		return e.repo, e.info, nil
	}
	repo, info, err := s.tokenByHash(ctx, hash)
	if err != nil {
		return repo, info, err
	}
	s.tokens.put(hash, gen, tokenEntry{repo: repo, info: info, at: now})
	return repo, info, nil
}
