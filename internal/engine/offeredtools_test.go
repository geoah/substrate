package engine

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func offeredFn(pkg, name, tool string) *vocabulary.Function {
	return &vocabulary.Function{Package: pkg, Name: name, Offer: &vocabulary.FunctionOffer{ToolName: tool}}
}

// The cap keeps the first MaxAgentOfferedTools in identity order and names
// the rest, and two offers of one tool name keep the first by identity.
func TestOfferedToolsCapAndCollisions(t *testing.T) {
	ag := &vocabulary.Agent{ToolsFrom: []string{"*"}}
	var fns []*vocabulary.Function
	for i := range vocabulary.MaxAgentOfferedTools + 2 {
		name := fmt.Sprintf("f%02d", i)
		fns = append(fns, offeredFn("a.example.com/feeds", name, "feeds_"+name))
	}
	fns = append(fns,
		offeredFn("b.example.com/feeds", "f00", "feeds_f00"),
		&vocabulary.Function{Package: "c.example.com/c", Name: "plain"},
	)
	never := func(*vocabulary.Function) bool { return false }
	picked, shadowed, over := offeredTools(ag, fns, map[string]agentTool{}, map[string]bool{}, never)
	if len(picked) != vocabulary.MaxAgentOfferedTools || picked[0].Name != "f00" {
		t.Fatalf("picked %d, first %v", len(picked), picked[0].Identity())
	}
	if want := []string{"b.example.com/feeds/f00"}; !reflect.DeepEqual(shadowed, want) {
		t.Fatalf("shadowed = %v, want %v", shadowed, want)
	}
	if want := []string{"a.example.com/feeds/f32", "a.example.com/feeds/f33"}; !reflect.DeepEqual(over, want) {
		t.Fatalf("over = %v, want %v", over, want)
	}

	// A named tool's name and a named function both win; a blocked bundle's
	// function and an unmatched pattern take nothing.
	ag = &vocabulary.Agent{ToolsFrom: []string{"a.example.com/*"}}
	picked, shadowed, _ = offeredTools(ag, fns[:3], map[string]agentTool{"feeds_f01": {}},
		map[string]bool{"a.example.com/feeds/f00": true},
		func(fn *vocabulary.Function) bool { return fn.Name == "f02" })
	if len(picked) != 0 || !reflect.DeepEqual(shadowed, []string{"a.example.com/feeds/f01"}) {
		t.Fatalf("picked %v, shadowed %v", picked, shadowed)
	}
}

// Decision 0234's names against a copy of the retired Firecrawl sample. A
// copy offers under the names it derives, `firecrawl_websearch` and
// `firecrawl_scrapepage`, which never meet the provider's `web_search` and
// `web_scrape`. An owner's copy can still declare `offer.name: web_search`
// itself, and then 0198's order decides: of two offers of one name the first
// by function reference keeps it, and a tool the agent names under `tools:`
// beats both. So a copy under an authority that sorts before
// `providers.substrate.reamde.dev` shadows the provider's tool, and one that
// sorts after is shadowed by it.
func TestAnExplicitlyNamedFirecrawlCopyFollowsReferenceOrder(t *testing.T) {
	const provider = "providers.substrate.reamde.dev/firecrawl"
	ag := &vocabulary.Agent{ToolsFrom: []string{"*"}}
	never := func(*vocabulary.Function) bool { return false }
	shipped := []*vocabulary.Function{
		offeredFn(provider, "webscrape", "web_scrape"),
		offeredFn(provider, "websearch", "web_search"),
	}
	run := func(copies ...*vocabulary.Function) (picked, shadowed []string) {
		fns := append(slices.Clone(shipped), copies...)
		slices.SortFunc(fns, func(a, b *vocabulary.Function) int { return strings.Compare(a.Identity(), b.Identity()) })
		got, shadowed, _ := offeredTools(ag, fns, map[string]agentTool{}, map[string]bool{}, never)
		for _, fn := range got {
			picked = append(picked, fn.Identity())
		}
		return picked, shadowed
	}

	// The names a copy derives sit beside the provider's.
	picked, shadowed := run(
		offeredFn("alice.example.com/firecrawl", "scrapepage", vocabulary.OfferedToolName("alice.example.com/firecrawl", "scrapepage")),
		offeredFn("alice.example.com/firecrawl", "websearch", vocabulary.OfferedToolName("alice.example.com/firecrawl", "websearch")),
	)
	if len(picked) != 4 || len(shadowed) != 0 {
		t.Fatalf("a copy under its derived names: picked %v, shadowed %v; want all four", picked, shadowed)
	}

	// A copy that names its tool web_search, under an authority that sorts
	// first, keeps the name.
	picked, shadowed = run(offeredFn("alice.example.com/firecrawl", "websearch", "web_search"))
	if !slices.Contains(picked, "alice.example.com/firecrawl/websearch") ||
		!slices.Equal(shadowed, []string{provider + "/websearch"}) {
		t.Fatalf("a copy sorting first: picked %v, shadowed %v; want the copy's web_search", picked, shadowed)
	}

	// One that sorts after the provider is shadowed by it.
	picked, shadowed = run(offeredFn("zed.example.com/firecrawl", "websearch", "web_search"))
	if !slices.Contains(picked, provider+"/websearch") ||
		!slices.Equal(shadowed, []string{"zed.example.com/firecrawl/websearch"}) {
		t.Fatalf("a copy sorting after: picked %v, shadowed %v; want the provider's web_search", picked, shadowed)
	}

	// A tool the agent names web_search beats both offers.
	fns := slices.Clone(shipped)
	got, shadowed, _ := offeredTools(ag, fns, map[string]agentTool{"web_search": {}}, map[string]bool{}, never)
	if len(got) != 1 || got[0].Name != "webscrape" || !slices.Equal(shadowed, []string{provider + "/websearch"}) {
		t.Fatalf("a named web_search: picked %v, shadowed %v; want the provider's web_search left out", got, shadowed)
	}
}
