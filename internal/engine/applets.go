package engine

// The applets package (docs/plans/applets.md): an applet record is checked at
// its write against the A2UI catalog and this repository's kinds, and an
// agent previews one through the `preview` host function. The runtime is
// internal/applet's; this file answers what it reads of the registry.

import (
	"context"
	"fmt"

	"github.com/geoah/substrate/internal/applet"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// appletSchema answers applet's check from a registry.
type appletSchema struct{ reg *vocabulary.Registry }

func (s appletSchema) Kind(ref string) (applet.KindShape, bool) {
	k, ok := s.reg.ByIdentity(ref)
	if !ok {
		return applet.KindShape{}, false
	}
	shape := applet.KindShape{Props: make(map[string]applet.PropShape, len(k.Props))}
	for name, p := range k.Props {
		ps := applet.PropShape{Type: string(p.Datatype), Target: p.To}
		for _, v := range p.Values {
			ps.Values = append(ps.Values, v.Value)
		}
		shape.Props[name] = ps
	}
	return shape, true
}

func (s appletSchema) Function(ref string) bool {
	f, err := s.reg.ResolveFunction(ref)
	return err == nil && f.Identity() == ref
}

// admitApplet holds an applet's merged row to the check, so the agent or the
// person who wrote it reads one problem per path at the write rather than
// the owner meeting a screen that cannot draw.
func (t *txn) admitApplet(id string, props map[string]any) error {
	if problems := applet.CheckRecord(id, props, appletSchema{t.declarations()}); len(problems) > 0 {
		return &substrate.ValidationError{Problems: problems}
	}
	return nil
}

// dispatchAppletPreview runs one preview call: one screen of a stored applet or
// of a draft, rendered against the live records under the agent's reads.
// It writes nothing, and an action is never run.
func (l *agentLoop) dispatchAppletPreview(ctx context.Context, args map[string]any) (string, bool) {
	for _, k := range sortedKeys(args) {
		switch k {
		case "id", "applet", "screen", "params", "ui":
		default:
			return toolError(fmt.Sprintf("unknown argument %q; preview takes id, applet, screen, params, ui", k)), false
		}
	}
	id, _ := args["id"].(string)
	draft, _ := args["applet"].(map[string]any)
	if (id == "") == (draft == nil) {
		return toolError("name a stored applet by id, or pass a draft's properties as applet, not both"), false
	}
	var rec *substrate.Record
	if id != "" {
		if !l.readsAllow(applet.Kind) {
			return toolError(fmt.Sprintf("%s is not in agent %s's reads allowlist", applet.Kind, l.ag.Identity())), false
		}
		got, err := l.ds.Get(ctx, applet.Kind, id)
		if err != nil {
			return toolError(fmt.Sprintf("read applet %s: %v", id, err)), false
		}
		rec = got
	} else {
		rec = &substrate.Record{ID: "draft", Kind: applet.Kind, Properties: draft}
	}
	in := substrate.AppletPreviewInput{}
	in.Screen, _ = args["screen"].(string)
	in.Params, _ = args["params"].(map[string]any)
	in.UI, _ = args["ui"].(map[string]any)
	rt := applet.Runtime{Data: agentReads{ds: l.ds, allows: l.readsAllow}}
	p, err := rt.Preview(ctx, rec, in, appletSchema{l.ds.registry()}, l.readsAllow)
	if err != nil {
		return toolError(err.Error()), false
	}
	return toolJSON(p), false
}

// agentReads is the dataset a preview renders against: every record a read
// answers, an expanded referent included, is held to the agent's reads, as
// the query tool holds them, and nothing is written.
type agentReads struct {
	ds     *dataset
	allows func(kind string) bool
}

func (a agentReads) List(ctx context.Context, q substrate.Query) (*substrate.Page, error) {
	page, err := a.ds.List(ctx, q)
	if err != nil {
		return nil, err
	}
	kept := page.Records[:0:0]
	for _, r := range page.Records {
		if a.allows(r.Kind) {
			kept = append(kept, r)
		}
	}
	page.Records = kept
	for path, r := range page.Included {
		if !a.allows(r.Kind) {
			delete(page.Included, path)
		}
	}
	return page, nil
}

func (agentReads) Put(context.Context, substrate.Actor, substrate.PutInput) (*substrate.Record, error) {
	return nil, errPreviewWrites
}

func (agentReads) Patch(context.Context, substrate.Actor, string, string, substrate.PatchInput) (*substrate.Record, error) {
	return nil, errPreviewWrites
}

func (agentReads) Delete(context.Context, substrate.Actor, string, string, substrate.DeleteInput) (*substrate.Record, error) {
	return nil, errPreviewWrites
}

func (agentReads) CallFunction(context.Context, substrate.Actor, string, any) (any, int, error) {
	return nil, 0, errPreviewWrites
}

func (a agentReads) KindByRef(ctx context.Context, ref string) (substrate.KindInfo, error) {
	return a.ds.KindByRef(ctx, ref)
}

var errPreviewWrites = fmt.Errorf("%w: a preview reads and never writes or calls", substrate.ErrForbidden)
