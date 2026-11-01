package engine

import (
	"fmt"
	"reflect"
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
