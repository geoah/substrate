package applet

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// Preview renders one screen of an applet for its author: the problems a
// write of it is refused with, and the screen's outline against the records
// it reads. It writes nothing; the Runtime's Data should refuse writes, and
// canRead, when set, holds every source to the author's reads, a screen
// whose sources it may not read being answered with its problems alone. A
// screen that cannot load answers its problems rather than the start's
// outline, and a failure that is not the applet's is returned as an error.
func (rt *Runtime) Preview(ctx context.Context, rec *substrate.Record, in substrate.AppletPreviewInput, schema Schema, canRead func(kind string) bool) (substrate.AppletPreview, error) {
	problems := CheckRecord(rec.ID, rec.Properties, schema)
	d, parsed := Parse(rec)
	if len(parsed) > 0 {
		return substrate.AppletPreview{Problems: problems}, nil
	}
	screen := in.Screen
	if screen == "" {
		screen = d.Start
	}
	if d.Screens[screen] == nil {
		return substrate.AppletPreview{}, &substrate.ValidationError{Problems: []string{
			fmt.Sprintf("screen: the applet has no screen %q; it has %s", screen, strings.Join(d.screenNames(), ", ")),
		}}
	}
	if canRead != nil {
		unreadable := false
		for _, name := range sortedKeys(d.Sources) {
			if k := d.Sources[name].Kind; k != "" && !canRead(k) {
				unreadable = true
				problems = append(problems, fmt.Sprintf("props.sources.%s.kind: %s is not among the kinds you read, so the preview cannot read it", name, k))
			}
		}
		if unreadable {
			return substrate.AppletPreview{Screen: screen, Screens: d.screenNames(), Problems: problems}, nil
		}
	}
	state := &substrate.AppletState{Screen: screen, Params: in.Params, UI: in.UI}
	frame, err := rt.render(ctx, d, substrate.AppletRenderInput{State: state}, false)
	if err != nil {
		if !expected(err) {
			return substrate.AppletPreview{}, err
		}
		var ve *substrate.ValidationError
		if errors.As(err, &ve) {
			for _, p := range ve.Problems {
				problems = append(problems, "render: "+p)
			}
		} else {
			problems = append(problems, "render: "+err.Error())
		}
		return substrate.AppletPreview{Screen: screen, Screens: d.screenNames(), Problems: problems}, nil
	}
	p := Outline(d, frame)
	p.Problems = problems
	return p, nil
}

// SchemaReader is what DatasetSchema reads: the API's dataset, held to the
// caller's token.
type SchemaReader interface {
	KindByRef(ctx context.Context, ref string) (substrate.KindInfo, error)
	Get(ctx context.Context, typ, id string) (*substrate.Record, error)
}

// DatasetSchema is the Schema a dataset answers, for a check run outside the
// engine (the API's preview): kinds from their declarations, functions from
// their records.
func DatasetSchema(ctx context.Context, ds SchemaReader) Schema {
	return datasetSchema{ctx: ctx, ds: ds}
}

type datasetSchema struct {
	ctx context.Context
	ds  SchemaReader
}

const functionKind = "substrate.reamde.dev/core/function"

func (s datasetSchema) Kind(ref string) (KindShape, bool) {
	info, err := s.ds.KindByRef(s.ctx, ref)
	if err != nil {
		return KindShape{}, false
	}
	return ShapeOf(info), true
}

func (s datasetSchema) Function(ref string) bool {
	_, err := s.ds.Get(s.ctx, functionKind, ref)
	return err == nil
}

// ShapeOf reads a kind's declaration into the shape the check holds an
// applet to.
func ShapeOf(info substrate.KindInfo) KindShape {
	shape := KindShape{Props: map[string]PropShape{}}
	props, _ := info.Definition["properties"].(map[string]any)
	for name, raw := range props {
		m, _ := raw.(map[string]any)
		ps := PropShape{}
		ps.Type, _ = m["type"].(string)
		ps.Target, _ = m["kind"].(string)
		if values, ok := m["values"].([]any); ok {
			for _, v := range values {
				switch x := v.(type) {
				case map[string]any:
					if value, ok := x["value"].(string); ok {
						ps.Values = append(ps.Values, value)
					}
				case string:
					ps.Values = append(ps.Values, x)
				}
			}
		}
		shape.Props[name] = ps
	}
	return shape
}
