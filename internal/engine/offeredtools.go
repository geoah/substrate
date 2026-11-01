package engine

import (
	"context"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/vocabulary"
)

// addOfferedTools appends the offered functions the agent's `toolsFrom` takes
// (record 0198) after every tool and sub-agent it names, so the named block
// keeps its bytes whatever bundles come and go. A disabled bundle's functions
// are left out; an uninstalled one's no longer resolve at all.
func (l *agentLoop) addOfferedTools(ctx context.Context) error {
	if len(l.ag.ToolsFrom) == 0 {
		return nil
	}
	states, err := l.ds.bundleStates(ctx)
	if err != nil {
		return err
	}
	reg := l.ds.registry()
	blocked := func(fn *vocabulary.Function) bool {
		b, ok := reg.BundleOf(fn.Package)
		return ok && states[b.Package].blocked()
	}
	named := map[string]bool{}
	for _, t := range l.byName {
		if t.fn != nil {
			named[t.fn.Identity()] = true
		}
	}
	picked, shadowed, over := offeredTools(l.ag, reg.Functions(), l.byName, named, blocked)
	if len(shadowed) > 0 {
		l.ds.svc.log.Info("substrate: offered functions left out, their tool names taken",
			"agent", l.ag.Identity(), "functions", shadowed)
	}
	if len(over) > 0 {
		l.ds.svc.log.Warn("substrate: an agent's toolsFrom matched more offered functions than it may take; the rest are left out",
			"agent", l.ag.Identity(), "max", vocabulary.MaxAgentOfferedTools, "functions", over)
	}
	for _, fn := range picked {
		var params any = map[string]any{"type": "object"}
		if fn.Input != nil {
			params = fn.Input
		}
		name := fn.Offer.ToolName
		l.byName[name] = agentTool{fn: fn}
		l.defs = append(l.defs, llm.Tool{Name: name, Description: fn.Description, Parameters: params})
	}
	return nil
}

// offeredTools picks, from fns in identity order, the offered functions ag
// takes. A function the agent already names under `tools:`, or one whose
// tool name a named tool, a sub-agent or an earlier offered function holds,
// is shadowed; past MaxAgentOfferedTools the rest are over. The order is the
// registry's, so one set of installed bundles gives one tool block, byte for
// byte, which is what a cached prompt prefix needs.
func offeredTools(ag *vocabulary.Agent, fns []*vocabulary.Function, names map[string]agentTool, named map[string]bool,
	blocked func(*vocabulary.Function) bool,
) (picked []*vocabulary.Function, shadowed, over []string) {
	taken := map[string]bool{}
	for name := range names {
		taken[name] = true
	}
	for _, fn := range fns {
		if fn.Offer == nil || !ag.TakesOffered(fn.Identity()) || blocked(fn) {
			continue
		}
		if named[fn.Identity()] {
			continue
		}
		if taken[fn.Offer.ToolName] {
			shadowed = append(shadowed, fn.Identity())
			continue
		}
		if len(picked) == vocabulary.MaxAgentOfferedTools {
			over = append(over, fn.Identity())
			continue
		}
		taken[fn.Offer.ToolName] = true
		picked = append(picked, fn)
	}
	return picked, shadowed, over
}
