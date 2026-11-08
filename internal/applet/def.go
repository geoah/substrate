// Package applet is the runtime of the substrate's applets
// (docs/plans/applets.md): a record of substrate.reamde.dev/applets/applet whose
// screens are A2UI v1.0 components, rendered into A2UI messages against the
// repository and acted on by running its actions' steps as ordinary reads
// and writes.
//
// The package is a function of the record, the state the renderer sends and
// a Data the caller hands it. The API hands it the request's dataset, so an
// applet reads and writes exactly what the caller's token may; the engine hands
// it its own for the designer's preview and calls Check on every write of an
// applet. It keeps no state between calls.
package applet

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// Kind is the applet kind's reference.
const Kind = "substrate.reamde.dev/applets/applet"

// SurfaceID is the one surface an applet draws: a screen change replaces it.
const SurfaceID = "applet"

// defaultFirst is how many records a list source reads when it names no
// first, and maxFirst the most it may name: the records route's own cap.
const (
	defaultFirst = 100
	maxFirst     = 500
)

// Def is an applet record read into its parts. Parse fills it and names every
// shape problem; Check goes on to hold it to the catalog and the kinds.
type Def struct {
	ID          string
	Version     int64
	Name        string
	Description string
	Icon        string
	Start       string
	State       map[string]any
	Sources     map[string]*Source
	Actions     map[string][]*Step
	Screens     map[string]*Screen
}

// Source is one read whose result lands at /data/<name>.
type Source struct {
	Kind    string
	ID      any
	Where   map[string]any
	Search  any
	OrderBy []substrate.Order
	First   int
	Expand  []string
}

// Step is one step of an action. Which fields a step reads is its op's.
type Step struct {
	Op         string
	Kind       string
	ID         any
	Properties map[string]any
	Where      map[string]any
	Screen     string
	Params     map[string]any
	Values     map[string]any
	Function   string
	Input      any
	Into       string
	Pick       string
	Text       any
	Confirm    any
	As         string
}

// Screen is one screen: its title, the sources it reads, the /ui paths
// whose edits re-render it, the steps that run each time it is entered and
// its components.
type Screen struct {
	Title      string
	Sources    []string
	RefreshOn  []string
	Init       []*Step
	Components []map[string]any
}

// initOps are the steps a screen's init may take: it fills /ui from the
// records the screen is about, and never writes one.
var initOps = []string{"read", "set"}

// The ops a step may name, and the keys each takes beside op, as and confirm.
var stepKeys = map[string][]string{
	"create":   {"kind", "properties"},
	"patch":    {"kind", "id", "properties"},
	"delete":   {"kind", "id"},
	"ensure":   {"kind", "where", "properties"},
	"read":     {"kind", "id", "into", "pick"},
	"call":     {"function", "input", "into", "pick"},
	"navigate": {"screen", "params"},
	"set":      {"values"},
	"notify":   {"text"},
}

// stepRequired are the keys each op cannot run without.
var stepRequired = map[string][]string{
	"create":   {"kind"},
	"patch":    {"kind", "id", "properties"},
	"delete":   {"kind", "id"},
	"ensure":   {"kind", "where"},
	"read":     {"kind", "id", "into"},
	"call":     {"function"},
	"navigate": {"screen"},
	"set":      {"values"},
	"notify":   {"text"},
}

var (
	sourceKeys = []string{"kind", "id", "where", "search", "orderBy", "first", "expand"}
	screenKeys = []string{"title", "sources", "refreshOn", "init", "components"}
)

// Parse reads an applet record. Its problems are `path: message` lines under
// the record's properties; a Def with problems is not one to run.
func Parse(rec *substrate.Record) (*Def, []string) {
	p := &parser{}
	props := rec.Properties
	d := &Def{
		ID:          rec.ID,
		Version:     rec.Version,
		Name:        p.str(props, "name"),
		Description: p.str(props, "description"),
		Icon:        p.str(props, "icon"),
		Start:       p.str(props, "start"),
		Sources:     map[string]*Source{},
		Actions:     map[string][]*Step{},
		Screens:     map[string]*Screen{},
	}
	if v, ok := props["state"]; ok && v != nil {
		m, ok := v.(map[string]any)
		if !ok {
			p.add("state", "is an object, the initial /ui")
		}
		d.State = m
	}
	if d.State == nil {
		d.State = map[string]any{}
	}
	for name, v := range p.obj(props, "sources") {
		d.Sources[name] = p.source("sources."+name, v)
	}
	for name, v := range p.obj(props, "actions") {
		d.Actions[name] = p.action("actions."+name, v)
	}
	screens := p.obj(props, "screens")
	if len(screens) == 0 {
		p.add("screens", "an applet has at least one screen")
	}
	for name, v := range screens {
		d.Screens[name] = p.screen("screens."+name, v)
	}
	switch {
	case d.Start == "" && len(d.Screens) == 1:
		for name := range d.Screens {
			d.Start = name
		}
	case d.Start == "" && len(d.Screens) > 1:
		p.add("start", "names the screen the applet opens on: one of "+strings.Join(d.screenNames(), ", "))
	case d.Start != "" && d.Screens[d.Start] == nil && len(d.Screens) > 0:
		p.add("start", fmt.Sprintf("names no screen: %q is not one of %s", d.Start, strings.Join(d.screenNames(), ", ")))
	}
	return d, p.problems
}

func (d *Def) screenNames() []string { return sortedKeys(d.Screens) }

type parser struct {
	problems []string
}

func (p *parser) add(path, msg string) {
	p.problems = append(p.problems, "props."+path+": "+msg)
}

func (p *parser) str(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		p.add(key, "is a string")
	}
	return s
}

func (p *parser) obj(m map[string]any, key string) map[string]any {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	o, ok := v.(map[string]any)
	if !ok {
		p.add(key, "is an object keyed by name")
		return nil
	}
	return o
}

func (p *parser) keys(path string, m map[string]any, allowed []string) {
	for _, k := range sortedKeys(m) {
		if !slices.Contains(allowed, k) {
			p.add(path+"."+k, "is not a key here; this takes "+strings.Join(allowed, ", "))
		}
	}
}

func (p *parser) source(path string, v any) *Source {
	s := &Source{}
	m, ok := v.(map[string]any)
	if !ok {
		p.add(path, "is an object: {kind, id, where, search, orderBy, first, expand}")
		return s
	}
	p.keys(path, m, sourceKeys)
	s.Kind, _ = m["kind"].(string)
	if s.Kind == "" {
		p.add(path+".kind", "names the kind the source reads, in full: <authority>/<package>/<name>")
	}
	s.ID = m["id"]
	if w, ok := m["where"]; ok && w != nil {
		if s.Where, ok = w.(map[string]any); !ok {
			p.add(path+".where", "is an object, one condition per property")
		}
	}
	s.Search = m["search"]
	if o, ok := m["orderBy"]; ok && o != nil {
		orders, err := parseOrder(o)
		if err != nil {
			p.add(path+".orderBy", err.Error())
		}
		s.OrderBy = orders
	}
	s.First = defaultFirst
	if f, ok := m["first"]; ok && f != nil {
		n, ok := number(f)
		if !ok || n < 1 || n > maxFirst || n != float64(int(n)) {
			p.add(path+".first", fmt.Sprintf("is a whole number from 1 to %d", maxFirst))
		} else {
			s.First = int(n)
		}
	}
	if e, ok := m["expand"]; ok && e != nil {
		s.Expand = p.strings(path+".expand", e)
	}
	return s
}

func parseOrder(v any) ([]substrate.Order, error) {
	switch o := v.(type) {
	case string:
		return substrate.ParseOrderBy(o)
	case []any:
		var parts []string
		for _, e := range o {
			switch x := e.(type) {
			case string:
				parts = append(parts, x)
			case map[string]any:
				name, _ := x["property"].(string)
				if desc, _ := x["desc"].(bool); desc {
					name += ":desc"
				}
				parts = append(parts, name)
			default:
				return nil, fmt.Errorf("is property, property:desc, or a list of them")
			}
		}
		return substrate.ParseOrderBy(strings.Join(parts, ","))
	}
	return nil, fmt.Errorf("is property, property:desc, or a list of them")
}

func (p *parser) strings(path string, v any) []string {
	list, ok := v.([]any)
	if !ok {
		if ss, isStrings := v.([]string); isStrings {
			return ss
		}
		p.add(path, "is a list of names")
		return nil
	}
	out := make([]string, 0, len(list))
	for i, e := range list {
		s, ok := e.(string)
		if !ok {
			p.add(fmt.Sprintf("%s[%d]", path, i), "is a name")
			continue
		}
		out = append(out, s)
	}
	return out
}

// action reads one action: a list of steps, or one step standing alone.
func (p *parser) action(path string, v any) []*Step {
	var raw []any
	switch a := v.(type) {
	case []any:
		raw = a
	case map[string]any:
		raw = []any{a}
	default:
		p.add(path, "is a list of steps, each {op, ...}")
		return nil
	}
	if len(raw) == 0 {
		p.add(path, "has no steps")
	}
	steps := make([]*Step, 0, len(raw))
	for i, e := range raw {
		steps = append(steps, p.step(fmt.Sprintf("%s[%d]", path, i), e))
	}
	return steps
}

func (p *parser) step(path string, v any) *Step {
	s := &Step{}
	m, ok := v.(map[string]any)
	if !ok {
		p.add(path, "is a step: {op, ...}")
		return s
	}
	s.Op, _ = m["op"].(string)
	allowed, known := stepKeys[s.Op]
	if !known {
		p.add(path+".op", fmt.Sprintf("%q is not an op; a step is one of %s", s.Op, strings.Join(sortedKeys(stepKeys), ", ")))
		return s
	}
	p.keys(path, m, append([]string{"op", "as", "confirm"}, allowed...))
	for _, k := range stepRequired[s.Op] {
		if empty(m[k]) {
			p.add(path+"."+k, "is required by "+s.Op)
		}
	}
	s.Kind = p.name(path+".kind", m["kind"])
	s.ID = m["id"]
	s.Properties = p.objAt(path+".properties", m["properties"])
	s.Where = p.objAt(path+".where", m["where"])
	s.Screen = p.name(path+".screen", m["screen"])
	s.Params = p.objAt(path+".params", m["params"])
	s.Values = p.objAt(path+".values", m["values"])
	s.Function = p.name(path+".function", m["function"])
	s.Input = m["input"]
	s.Into = p.name(path+".into", m["into"])
	s.Pick = p.name(path+".pick", m["pick"])
	s.Text = m["text"]
	s.Confirm = m["confirm"]
	s.As = p.name(path+".as", m["as"])
	return s
}

// name reads a key that names something (a kind, a screen, a function, a
// path): a string, or absent. Anything else is a problem rather than an
// empty name a step would run with.
func (p *parser) name(path string, v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		p.add(path, "is a string")
	}
	return s
}

func (p *parser) objAt(path string, v any) map[string]any {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		p.add(path, "is an object")
	}
	return m
}

func (p *parser) screen(path string, v any) *Screen {
	s := &Screen{}
	m, ok := v.(map[string]any)
	if !ok {
		p.add(path, "is an object: {title, sources, refreshOn, init, components}")
		return s
	}
	p.keys(path, m, screenKeys)
	s.Title, _ = m["title"].(string)
	if src, ok := m["sources"]; ok && src != nil {
		s.Sources = p.strings(path+".sources", src)
	}
	if r, ok := m["refreshOn"]; ok && r != nil {
		s.RefreshOn = p.strings(path+".refreshOn", r)
	}
	if in, ok := m["init"]; ok && in != nil {
		s.Init = p.action(path+".init", in)
		for i, st := range s.Init {
			if st.Op != "" && !slices.Contains(initOps, st.Op) {
				p.add(fmt.Sprintf("%s.init[%d].op", path, i), "a screen's init reads and sets /ui alone: read or set")
			}
		}
	}
	comps, ok := m["components"].([]any)
	if !ok || len(comps) == 0 {
		p.add(path+".components", "is a list of A2UI components, one of them with id root")
		return s
	}
	for i, c := range comps {
		cm, ok := c.(map[string]any)
		if !ok {
			p.add(fmt.Sprintf("%s.components[%d]", path, i), "is a component: {id, component, ...}")
			continue
		}
		s.Components = append(s.Components, cm)
	}
	return s
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// clone deep-copies a JSON value, so a state the runtime changes is never the
// record's or the caller's.
func clone(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = clone(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = clone(e)
		}
		return out
	}
	return v
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return clone(m).(map[string]any)
}

// normalize round-trips a value through JSON, so a value built in Go (a
// []string, a struct) reads like one decoded from a request.
func normalize(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if json.Unmarshal(b, &out) != nil {
		return v
	}
	return out
}

// Guide is how an assistant builds an applet, served at the MCP door by the
// applets tool: the record's format, the loop, and what a good one holds.
//
//go:embed guide.md
var Guide string
