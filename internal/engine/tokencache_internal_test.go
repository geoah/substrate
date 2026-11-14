package engine

// The cache's generations, without a database: a lookup is stored only if
// no fence or unfence of any repository came between its start and its
// store.

import (
	"testing"
	"time"
)

func TestALookupThatCrossesAFenceOrAnUnfenceIsNotKept(t *testing.T) {
	now := time.Now()
	entry := func(repository string) tokenEntry {
		e := tokenEntry{at: now}
		e.repo.ID = repository
		return e
	}
	var c tokenCache

	// Begun before the fence, stored after the unfence: the read may have
	// seen the token before the doubted commit landed.
	_, _, gen := c.get("a", now)
	c.fence("r")
	c.unfence("r")
	c.put("a", gen, entry("r"))
	if c.cached("a") {
		t.Fatal("a lookup begun before a fence was kept after its unfence")
	}

	// Begun under the fence, stored after it lifted: the same.
	c.fence("r")
	_, _, gen = c.get("b", now)
	c.unfence("r")
	c.put("b", gen, entry("r"))
	if c.cached("b") {
		t.Fatal("a lookup begun under a fence was kept after its unfence")
	}

	// Under a fence nothing of that repository is kept, and another's is.
	c.fence("r")
	_, _, gen = c.get("c", now)
	c.put("c", gen, entry("r"))
	c.put("d", gen, entry("s"))
	if c.cached("c") || !c.cached("d") {
		t.Fatalf("under r's fence: r's lookup kept %v, s's kept %v; want r's dropped and s's kept", c.cached("c"), c.cached("d"))
	}
	c.unfence("r")

	// Begun after the unfence: kept.
	_, _, gen = c.get("e", now)
	c.put("e", gen, entry("r"))
	if !c.cached("e") {
		t.Fatal("a lookup begun after the fence lifted was not kept")
	}
}
