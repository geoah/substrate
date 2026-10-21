package engine

// The unit graph cache's bounds (unitgraph.go): two revisions per repository,
// unitGraphsPerProcess graphs per process, least recently used out first, and
// a graph the cache lets go is unreachable from it, so `total` is what the
// cache keeps alive.

import (
	"fmt"
	"runtime"
	"testing"
	"weak"
)

// cachedGraph puts one graph into the cache and answers a weak pointer at
// it, so the test holds no strong reference of its own.
func cachedGraph(c *unitGraphCache, repo string, revision int64) weak.Pointer[unitGraphAt] {
	at := &unitGraphAt{revision: revision}
	c.put(repo, at)
	return weak.Make(at)
}

// collected reports whether nothing keeps the graph reachable any longer.
func collected(p weak.Pointer[unitGraphAt]) bool {
	runtime.GC()
	runtime.GC()
	return p.Value() == nil
}

// cacheHeld counts the graphs the cache's lists hold, which total must equal.
func cacheHeld(c *unitGraphCache) int {
	n := 0
	for _, list := range c.repos {
		n += len(list)
	}
	return n
}

// A repository keeps its two newest revisions, and the third newest is
// released, not left behind in the list's backing array.
func TestUnitGraphCacheKeepsTwoRevisionsPerRepository(t *testing.T) {
	var c unitGraphCache
	oldest := cachedGraph(&c, "a", 1)
	cachedGraph(&c, "a", 2)
	cachedGraph(&c, "a", 3)
	if c.get("a", 1) != nil || c.get("a", 2) == nil || c.get("a", 3) == nil {
		t.Fatalf("the cache holds revisions 1 %v, 2 %v, 3 %v; want 2 and 3", c.get("a", 1) != nil, c.get("a", 2) != nil, c.get("a", 3) != nil)
	}
	if c.total != 2 || cacheHeld(&c) != 2 {
		t.Errorf("total = %d, lists hold %d, want 2", c.total, cacheHeld(&c))
	}
	if !collected(oldest) {
		t.Error("the dropped revision is still reachable from the cache")
	}
	// The cache itself stays reachable through the collection above.
	runtime.KeepAlive(&c)
}

// Past unitGraphsPerProcess the least recently used graph goes, the final
// entry of its repository's list included, and nothing keeps it reachable.
func TestUnitGraphCacheEvictsTheLeastRecentlyUsed(t *testing.T) {
	var c unitGraphCache
	// Repository a holds revisions 2 and 1, newest first; reading 2 leaves
	// 1, the final entry of a's list, the least recently used of the process.
	cachedGraph(&c, "a", 2)
	evicted := cachedGraph(&c, "a", 1)
	if c.get("a", 2) == nil {
		t.Fatal("a at revision 2 is not cached")
	}
	for i := range unitGraphsPerProcess - 2 {
		cachedGraph(&c, fmt.Sprintf("r%d", i), 1)
	}
	if c.total != unitGraphsPerProcess {
		t.Fatalf("total = %d, want the process bound %d", c.total, unitGraphsPerProcess)
	}
	if c.get("a", 2) == nil {
		t.Fatal("a at revision 2 is not cached")
	}
	cachedGraph(&c, "over", 1)
	if c.total != unitGraphsPerProcess || cacheHeld(&c) != unitGraphsPerProcess {
		t.Errorf("total = %d, lists hold %d, want %d", c.total, cacheHeld(&c), unitGraphsPerProcess)
	}
	if c.get("a", 1) != nil {
		t.Error("the least recently used graph is still cached")
	}
	if c.get("a", 2) == nil || c.get("over", 1) == nil || c.get("r0", 1) == nil {
		t.Error("a graph used more recently was evicted")
	}
	if !collected(evicted) {
		t.Error("the evicted graph is still reachable from the cache")
	}
	runtime.KeepAlive(&c)
}
