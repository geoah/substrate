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
// process sees every token change: a transaction that folded one empties the
// cache when it ends, before its write answers, so a revoked or deleted token
// stops working at once. A lookup that began before an emptying is never
// cached, or it could put back what the emptying took away. tokenCacheTTL is
// the backstop for a change the process did not make.
type tokenCache struct {
	mu      sync.Mutex
	gen     uint64
	entries map[string]tokenEntry
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
	if gen != c.gen {
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
