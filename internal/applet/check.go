package applet

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// Schema is what Check reads of the repository: the kinds, their properties,
// and the functions an action may call. The engine answers it from its
// registry, a test from a map.
type Schema interface {
	Kind(ref string) (KindShape, bool)
	Function(ref string) bool
}

// KindShape is one kind as the check reads it.
type KindShape struct {
	Props map[string]PropShape
}

// PropShape is one declared property: its type, an enum's or a state's
// values, and a reference's target kind ("" for any).
type PropShape struct {
	Type   string
	Values []string
	Target string
}

// builtinProps are what every record carries and a filter or an order may
// name without its kind declaring them.
var builtinProps = []string{"title", "createdAt", "updatedAt", "id"}

// condOps is the filter grammar's set, as the records route takes it.
var condOps = []string{"eq", "in", "prefix", "match", "gt", "gte", "lt", "lte", "contains", "exists"}

// modelRoots are the roots of the data model a component may bind.
var modelRoots = []string{"applet", "ui", "params", "data", "counts"}

// componentID is A2UI's identifier rule (UAX #31): letters, digits and _,
// not starting with a digit. A hyphen is the commonest refusal.
var componentID = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var kindRef = regexp.MustCompile(`^(~|[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+)/[a-z][a-z0-9]*/[a-z][a-z0-9]*$`)

// Check holds a parsed applet to the A2UI catalog and to the repository's
// kinds and functions: every problem a write of it is refused with, as
// `props.<path>: message`. A nil schema checks the applet's own shape alone.
func Check(d *Def, schema Schema) []string {
	c := &checker{d: d, schema: schema, cat: basicCatalog()}
	for _, name := range sortedKeys(d.Sources) {
		c.source("sources."+name, d.Sources[name])
	}
	for _, name := range sortedKeys(d.Actions) {
		c.action("actions."+name, d.Actions[name])
	}
	for _, name := range d.screenNames() {
		c.screen("screens."+name, d.Screens[name])
	}
	if need := d.screenParams(d.Start); len(need) > 0 {
		c.add("start", fmt.Sprintf("screen %q reads /params/%s, which nothing sets when the applet opens: start on a screen that needs no params", d.Start, need[0]))
	}
	return c.problems
}

// CheckRecord is Parse and Check together, the engine's admission of a
// write: the problems a write of these properties is refused with.
func CheckRecord(id string, props map[string]any, schema Schema) []string {
	d, problems := Parse(recordOf(id, 0, props))
	if len(problems) > 0 {
		return problems
	}
	return Check(d, schema)
}

type checker struct {
	d        *Def
	schema   Schema
	cat      *catalog
	problems []string
	// resultNames are the `as` names of the steps before the one checked,
	// while an action is: what a /results path may read.
	resultNames map[string]bool
}

func (c *checker) add(path, msg string) {
	c.problems = append(c.problems, "props."+path+": "+msg)
}

func (c *checker) kind(path, ref string) (KindShape, bool) {
	if ref == "" {
		return KindShape{}, false
	}
	if !kindRef.MatchString(ref) {
		c.add(path, fmt.Sprintf("%q is not a kind named in full, <authority>/<package>/<name>", ref))
		return KindShape{}, false
	}
	if c.schema == nil {
		return KindShape{}, false
	}
	k, ok := c.schema.Kind(ref)
	if !ok {
		c.add(path, fmt.Sprintf("this repository declares no kind %s", ref))
	}
	return k, ok
}

func (c *checker) prop(path, kind string, k KindShape, name string) (PropShape, bool) {
	if p, ok := k.Props[name]; ok {
		return p, true
	}
	if slices.Contains(builtinProps, name) {
		return PropShape{Type: "string"}, true
	}
	c.add(path, fmt.Sprintf("%s declares no property %q; it has %s", kind, name, strings.Join(sortedKeys(k.Props), ", ")))
	return PropShape{}, false
}

// --- sources -------------------------------------------------------------------

func (c *checker) source(path string, s *Source) {
	k, known := c.kind(path+".kind", s.Kind)
	c.serverValue(path+".id", s.ID, serverRoots)
	c.serverValue(path+".search", s.Search, serverRoots)
	for _, name := range sortedKeys(s.Where) {
		at := path + ".where." + name
		if known {
			c.prop(at, s.Kind, k, name)
		}
		cond := s.Where[name]
		if m, ok := cond.(map[string]any); ok && !isRefValue(m) {
			if _, _, isBinding := binding(m); !isBinding {
				for _, op := range sortedKeys(m) {
					if !slices.Contains(condOps, op) {
						c.add(at+"."+op, fmt.Sprintf("is not a condition; a condition is one of %s", strings.Join(condOps, ", ")))
					}
					c.serverValue(at+"."+op, m[op], serverRoots)
				}
				continue
			}
		}
		c.serverValue(at, cond, serverRoots)
	}
	for i, o := range s.OrderBy {
		if known {
			c.prop(fmt.Sprintf("%s.orderBy[%d]", path, i), s.Kind, k, o.Property)
		}
	}
	if window, _ := s.Where[substrate.PropAt].(map[string]any); window != nil {
		_, from := window["gte"]
		_, after := window["gt"]
		_, to := window["lt"]
		_, through := window["lte"]
		if (from || after) && (to || through) && (len(s.OrderBy) > 1 || len(s.OrderBy) == 1 && s.OrderBy[0].Property != substrate.PropAt) {
			c.add(path+".orderBy", `a source that bounds at on both ends is a window read, ordered by "at" or "at:desc" alone`)
		}
	}
	for i, name := range s.Expand {
		at := fmt.Sprintf("%s.expand[%d]", path, i)
		if !known {
			continue
		}
		if p, ok := c.prop(at, s.Kind, k, name); ok && p.Type != "reference" {
			c.add(at, fmt.Sprintf("%q is %s; expand takes a reference property", name, p.Type))
		}
	}
}

// serverValue checks the bindings inside a source's or an action's value.
func (c *checker) serverValue(path string, v any, roots []string) {
	if key, arg, ok := binding(v); ok {
		switch key {
		case bindPath:
			p, _ := arg.(string)
			root := strings.SplitN(strings.TrimPrefix(p, "/"), "/", 2)[0]
			if !strings.HasPrefix(p, "/") || !slices.Contains(roots, root) {
				c.add(path, fmt.Sprintf("@path %q reads none of %s here; a source reads /ui and /params, an action /context and /results too", p, rootList(roots)))
			}
			if root == "results" && c.resultNames != nil {
				name := strings.SplitN(strings.TrimPrefix(p, "/results/"), "/", 2)[0]
				if !c.resultNames[name] {
					c.add(path, fmt.Sprintf("@path %q names no earlier step: a step's result is kept under its as", p))
				}
			}
		case bindNow:
			if arg != "date" && arg != "datetime" {
				c.add(path, `@now is "date" or "datetime"`)
			}
		case bindCall:
			name, _ := arg.(string)
			args, _ := v.(map[string]any)["args"].(map[string]any)
			switch name {
			case fnFormatString:
			case fnAddDays:
				c.addDaysArgs(path, args, roots)
				return
			default:
				c.add(path, fmt.Sprintf("a source or an action calls formatString or addDays, not %q", name))
				return
			}
			value, has := args["value"]
			if !has {
				c.add(path+".args.value", "is required by formatString")
				return
			}
			tmpl, literal := value.(string)
			if !literal {
				c.serverValue(path+".args.value", value, roots)
				return
			}
			// Each placeholder reads the model as a binding would, so it is
			// held to the same roots: a question asked before any step runs
			// reads no step's result.
			for _, m := range placeholder.FindAllStringSubmatch(tmpl, -1) {
				expr := strings.TrimSpace(m[1])
				if strings.HasPrefix(m[0], `\`) || strings.Contains(expr, "(") {
					continue
				}
				c.serverValue(path+".args.value", map[string]any{bindPath: expr}, roots)
			}
		}
		return
	}
	switch x := v.(type) {
	case map[string]any:
		if _, legacy := x["path"]; legacy && len(x) == 1 {
			c.add(path, `a binding is {"@path": ...} in A2UI v1.0; {"path": ...} is v0.9's spelling`)
			return
		}
		for _, k := range sortedKeys(x) {
			c.serverValue(path+"."+k, x[k], roots)
		}
	case []any:
		for i, e := range x {
			c.serverValue(fmt.Sprintf("%s[%d]", path, i), e, roots)
		}
	}
}

// addDaysArgs checks addDays's two arguments: each a binding or a literal
// the function reads (a date or an instant, a whole number of days).
func (c *checker) addDaysArgs(path string, args map[string]any, roots []string) {
	value, has := args["value"]
	if !has {
		c.add(path+".args.value", "is required by addDays: the date or instant to move")
	} else if _, _, isBinding := binding(value); isBinding {
		c.serverValue(path+".args.value", value, roots)
	} else if _, err := addDays(value, 0); err != nil || value == nil {
		c.add(path+".args.value", "is a date (2026-07-01), an instant (2026-07-01T09:00:00Z) or a binding to one")
	}
	days, has := args["days"]
	if !has {
		c.add(path+".args.days", "is required by addDays: how many days to move, negative to go back")
	} else if _, _, isBinding := binding(days); isBinding {
		c.serverValue(path+".args.days", days, roots)
	} else if _, ok := wholeDays(days); !ok {
		c.add(path+".args.days", "is a whole number of days or a binding to one")
	}
}

func rootList(roots []string) string {
	out := make([]string, len(roots))
	for i, r := range roots {
		out[i] = "/" + r
	}
	return strings.Join(out, ", ")
}

// --- actions -------------------------------------------------------------------

func (c *checker) action(path string, steps []*Step) {
	c.steps(path, steps, actionRoots, true)
}

// steps checks an action's steps, or a screen's init, which runs before any
// tap: it reads /ui and /params alone and keeps no result.
func (c *checker) steps(path string, steps []*Step, roots []string, keeps bool) {
	c.resultNames = map[string]bool{}
	asked := false
	for i, s := range steps {
		at := fmt.Sprintf("%s[%d]", path, i)
		if !keeps && s.As != "" {
			c.add(at+".as", "a screen's init keeps no result: read into a /ui path with into")
		}
		// The one question is asked before any step runs, so it reads what
		// the tap knows and never a step's result.
		if s.Confirm != nil {
			if asked {
				c.add(at+".confirm", "an action asks one question, before it runs: put it on its first step that needs one")
			}
			asked = true
			c.serverValue(at+".confirm", s.Confirm, []string{"ui", "params", "context"})
		}
		switch s.Op {
		case "create", "patch", "delete", "ensure", "read":
			k, known := c.kind(at+".kind", s.Kind)
			c.serverValue(at+".id", s.ID, roots)
			for _, name := range sortedKeys(s.Properties) {
				if known {
					c.prop(at+".properties."+name, s.Kind, k, name)
				}
				c.serverValue(at+".properties."+name, s.Properties[name], roots)
			}
			for _, name := range sortedKeys(s.Where) {
				if known {
					c.prop(at+".where."+name, s.Kind, k, name)
				}
				c.serverValue(at+".where."+name, s.Where[name], roots)
			}
		}
		switch s.Op {
		case "read", "call":
			if s.Into != "" {
				if _, ok := uiPath(s.Into); !ok {
					c.add(at+".into", "is a /ui path, such as /ui/edit")
				}
			}
			if s.Pick != "" && !strings.HasPrefix(s.Pick, "/") {
				c.add(at+".pick", "is a path into the result, such as /properties or /results")
			}
		}
		switch s.Op {
		case "call":
			if c.schema != nil && s.Function != "" && !c.schema.Function(s.Function) {
				c.add(at+".function", fmt.Sprintf("this repository has no function %s; name one in full", s.Function))
			}
			c.serverValue(at+".input", s.Input, roots)
		case "navigate":
			if s.Screen != "" && c.d.Screens[s.Screen] == nil {
				c.add(at+".screen", fmt.Sprintf("names no screen: %q is not one of %s", s.Screen, strings.Join(c.d.screenNames(), ", ")))
			}
			for _, need := range c.d.screenParams(s.Screen) {
				if _, ok := s.Params[need]; !ok {
					c.add(at+".params", fmt.Sprintf("screen %q reads /params/%s, so a navigate to it passes %s", s.Screen, need, need))
				}
			}
			for _, name := range sortedKeys(s.Params) {
				c.serverValue(at+".params."+name, s.Params[name], roots)
			}
		case "set":
			for _, p := range sortedKeys(s.Values) {
				if _, ok := uiPath(p); !ok {
					c.add(at+".values."+p, "a set writes /ui paths alone, such as /ui/form")
				}
				c.serverValue(at+".values."+p, s.Values[p], roots)
			}
		case "notify":
			c.serverValue(at+".text", s.Text, roots)
		}
		if s.As != "" {
			if c.resultNames[s.As] {
				c.add(at+".as", fmt.Sprintf("%q names an earlier step already", s.As))
			}
			c.resultNames[s.As] = true
		}
	}
	c.resultNames = nil
}

// --- screens -------------------------------------------------------------------

func (c *checker) screen(path string, s *Screen) {
	for i, name := range s.Sources {
		if c.d.Sources[name] == nil {
			c.add(fmt.Sprintf("%s.sources[%d]", path, i), fmt.Sprintf("names no source: %q is not one of %s", name, strings.Join(sortedKeys(c.d.Sources), ", ")))
		}
	}
	for i, p := range s.RefreshOn {
		if _, ok := uiPath(p); !ok {
			c.add(fmt.Sprintf("%s.refreshOn[%d]", path, i), "is a /ui path an input writes, such as /ui/q")
			continue
		}
		c.declaredUI(fmt.Sprintf("%s.refreshOn[%d]", path, i), p)
	}
	if len(s.Init) > 0 {
		c.steps(path+".init", s.Init, serverRoots, false)
	}
	byID := map[string]map[string]any{}
	for i, comp := range s.Components {
		at := fmt.Sprintf("%s.components[%d]", path, i)
		id, _ := comp["id"].(string)
		switch {
		case id == "":
			c.add(at+".id", "every component has an id")
			continue
		case !componentID.MatchString(id):
			c.add(at+".id", fmt.Sprintf("%q is not an A2UI id: letters, digits and _, not starting with a digit (no -)", id))
		case byID[id] != nil:
			c.add(at+".id", fmt.Sprintf("%q is the id of an earlier component", id))
			continue
		}
		byID[id] = comp
	}
	if byID["root"] == nil {
		c.add(path+".components", "no component has id root, the one the screen draws")
	}
	sc := &screenCheck{c: c, path: path, s: s, byID: byID, templated: map[string]bool{}}
	if cycle := sc.cycle(); cycle != "" {
		c.add(path+".components", "a component contains itself: "+cycle)
		return
	}
	sc.markTemplates()
	for i, comp := range s.Components {
		id, _ := comp["id"].(string)
		if id == "" || byID[id] == nil {
			continue
		}
		sc.component(fmt.Sprintf("%s.components[%d]", path, i), id, comp)
	}
}

type screenCheck struct {
	c         *checker
	path      string
	s         *Screen
	byID      map[string]map[string]any
	templated map[string]bool
}

// cycle names a chain of components that contains itself, which no
// renderer can draw, or "".
func (sc *screenCheck) cycle() string {
	const (
		unseen = iota
		open
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(id string) string
	visit = func(id string) string {
		switch state[id] {
		case open:
			loop := stack[slices.Index(stack, id):]
			return strings.Join(append(slices.Clone(loop), id), " → ")
		case done:
			return ""
		}
		state[id] = open
		stack = append(stack, id)
		for _, child := range childIDs(sc.byID[id]) {
			if sc.byID[child] == nil {
				continue
			}
			if c := visit(child); c != "" {
				return c
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = done
		return ""
	}
	for _, id := range sortedKeys(sc.byID) {
		if c := visit(id); c != "" {
			return c
		}
	}
	return ""
}

// markTemplates finds every component a list template draws, which reads
// paths relative to the item.
func (sc *screenCheck) markTemplates() {
	var mark func(id string)
	mark = func(id string) {
		if sc.templated[id] {
			return
		}
		sc.templated[id] = true
		for _, child := range childIDs(sc.byID[id]) {
			mark(child)
		}
	}
	for _, comp := range sc.s.Components {
		if t, ok := comp["children"].(map[string]any); ok {
			if id, _ := t["componentId"].(string); id != "" {
				mark(id)
			}
		}
	}
}

// childIDs are the component ids a component names: its child, its static
// children, a template's component, its tabs' children, a modal's two.
func childIDs(comp map[string]any) []string {
	var out []string
	for _, key := range []string{"child", "trigger", "content"} {
		if s, ok := comp[key].(string); ok {
			out = append(out, s)
		}
	}
	switch ch := comp["children"].(type) {
	case []any:
		for _, e := range ch {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	case map[string]any:
		if s, ok := ch["componentId"].(string); ok {
			out = append(out, s)
		}
	}
	if tabs, ok := comp["tabs"].([]any); ok {
		for _, t := range tabs {
			if m, ok := t.(map[string]any); ok {
				if s, ok := m["child"].(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

func (sc *screenCheck) component(path, id string, comp map[string]any) {
	c := sc.c
	typ, _ := comp["component"].(string)
	spec, ok := c.cat.components[typ]
	if !ok {
		c.add(path+".component", fmt.Sprintf("%q is not an A2UI v1.0 basic catalog component; one of %s", typ, strings.Join(sortedKeys(c.cat.components), ", ")))
		return
	}
	for _, req := range spec.required {
		if _, ok := comp[req]; !ok {
			c.add(path+"."+req, "is required by "+typ)
		}
	}
	inTemplate := sc.templated[id]
	for _, key := range sortedKeys(comp) {
		at := path + "." + key
		ps, known := spec.props[key]
		if !known {
			if _, common := commonProps[key]; common {
				continue
			}
			c.add(at, fmt.Sprintf("%s has no prop %q; it takes %s", typ, key, strings.Join(sortedKeys(spec.props), ", ")))
			continue
		}
		v := comp[key]
		switch ps.role {
		case roleEnum:
			if s, ok := v.(string); !ok || !slices.Contains(ps.values, s) {
				c.add(at, fmt.Sprintf("is one of %s", strings.Join(ps.values, ", ")))
			}
		case roleChild:
			sc.childRef(at, v)
		case roleChildList:
			sc.childList(at, v, inTemplate)
		case roleTabs:
			tabs, _ := v.([]any)
			if len(tabs) == 0 {
				c.add(at, "is a list of {title, child}")
			}
			for i, t := range tabs {
				m, _ := t.(map[string]any)
				sc.dynamic(fmt.Sprintf("%s[%d].title", at, i), m["title"], inTemplate)
				sc.childRef(fmt.Sprintf("%s[%d].child", at, i), m["child"])
			}
		case roleOptions:
			opts, ok := v.([]any)
			if !ok {
				c.add(at, "is a list of {label, value}")
			}
			for i, o := range opts {
				m, _ := o.(map[string]any)
				if _, ok := m["value"].(string); !ok {
					c.add(fmt.Sprintf("%s[%d].value", at, i), "is a string, the value the picker writes")
				}
				sc.dynamic(fmt.Sprintf("%s[%d].label", at, i), m["label"], inTemplate)
			}
		case roleAction:
			sc.action(at, v, inTemplate)
		case roleChecks:
			checks, ok := v.([]any)
			if !ok {
				c.add(at, "is a list of {condition, message}")
			}
			for i, ch := range checks {
				m, _ := ch.(map[string]any)
				if m["condition"] == nil {
					c.add(fmt.Sprintf("%s[%d].condition", at, i), "is required: a binding or a call that answers true when the input is good")
				}
				sc.dynamic(fmt.Sprintf("%s[%d].condition", at, i), m["condition"], inTemplate)
			}
		case roleDynamic:
			if key == "value" && inputValueProps[typ] {
				sc.inputValue(at, v)
				continue
			}
			sc.dynamic(at, v, inTemplate)
		}
	}
	if acc, ok := comp["accessibility"].(map[string]any); ok {
		for _, k := range sortedKeys(acc) {
			sc.dynamic(path+".accessibility."+k, acc[k], inTemplate)
		}
	}
}

func (sc *screenCheck) childRef(path string, v any) {
	id, ok := v.(string)
	if !ok {
		sc.c.add(path, "is a component id")
		return
	}
	if sc.byID[id] == nil {
		sc.c.add(path, fmt.Sprintf("names no component: %q", id))
	}
}

func (sc *screenCheck) childList(path string, v any, inTemplate bool) {
	switch x := v.(type) {
	case []any:
		for i, e := range x {
			sc.childRef(fmt.Sprintf("%s[%d]", path, i), e)
		}
	case map[string]any:
		sc.childRef(path+".componentId", x["componentId"])
		p, ok := x["path"].(string)
		if !ok || p == "" {
			sc.c.add(path+".path", "is the data model path of the list the template repeats over, such as /data/books")
			return
		}
		sc.modelPath(path+".path", p, inTemplate, true)
	default:
		sc.c.add(path, "is a list of component ids, or a template {path, componentId}")
	}
}

// inputValue holds an input's value to a /ui path: the renderer writes it
// back as the owner types, and only /ui reaches the server.
func (sc *screenCheck) inputValue(path string, v any) {
	key, arg, ok := binding(v)
	p, _ := arg.(string)
	if !ok || key != bindPath || !strings.HasPrefix(p, "/ui/") {
		sc.c.add(path, `binds a /ui path, {"@path": "/ui/..."}, so what the owner enters is kept and sent with the next action`)
		return
	}
	sc.c.declaredUI(path, p)
}

// declaredUI holds a /ui path an input or a refresh reads to the applet's
// state: a path under no key the state declares starts empty, and a patch
// from it writes nothing.
func (c *checker) declaredUI(path, p string) {
	key := strings.SplitN(strings.TrimPrefix(p, "/ui/"), "/", 2)[0]
	if _, ok := c.d.State[key]; !ok {
		c.add(path, fmt.Sprintf("%s is under no key the applet's state declares: add %q to state with its starting value", p, key))
	}
}

func (sc *screenCheck) action(path string, v any, inTemplate bool) {
	m, _ := v.(map[string]any)
	switch {
	case m["event"] != nil:
		ev, _ := m["event"].(map[string]any)
		name, _ := ev["name"].(string)
		if sc.c.d.Actions[name] == nil {
			sc.c.add(path+".event.name", fmt.Sprintf("names no action: %q is not one of %s", name, strings.Join(sortedKeys(sc.c.d.Actions), ", ")))
		}
		if ctx, ok := ev["context"].(map[string]any); ok {
			for _, k := range sortedKeys(ctx) {
				sc.dynamic(path+".event.context."+k, ctx[k], inTemplate)
			}
		}
		sc.dynamic(path+".event.userMessage", ev["userMessage"], inTemplate)
	case m["functionCall"] != nil:
		sc.dynamic(path+".functionCall", m["functionCall"], inTemplate)
	default:
		sc.c.add(path, `is {"event": {"name", "context"}}, naming one of the applet's actions, or {"functionCall": ...}`)
	}
}

// dynamic checks a component value: a literal, a binding or a call.
func (sc *screenCheck) dynamic(path string, v any, inTemplate bool) {
	switch x := v.(type) {
	case nil:
		return
	case string:
		// A2UI interpolates in formatString's value alone: anywhere else a
		// placeholder is drawn as typed, which an agent seldom means.
		if placeholder.MatchString(x) && !strings.HasPrefix(placeholder.FindString(x), `\`) {
			sc.c.add(path, `${...} is filled only inside formatString: write {"@call": "formatString", "args": {"value": ...}} here`)
		}
		return
	case map[string]any:
		if p, ok := x["@path"]; ok {
			s, _ := p.(string)
			sc.modelPath(path, s, inTemplate, false)
			return
		}
		if name, ok := x["@call"]; ok {
			n, _ := name.(string)
			if n != "@index" && !sc.c.cat.functions[n] {
				sc.c.add(path, fmt.Sprintf("%q is not an A2UI v1.0 basic catalog function; one of %s", n, strings.Join(sortedKeys(sc.c.cat.functions), ", ")))
			}
			args, _ := x["args"].(map[string]any)
			for _, req := range sc.c.cat.functionArgs[n] {
				if _, ok := args[req]; !ok {
					sc.c.add(path+".args."+req, "is required by "+n)
				}
			}
			for _, k := range sortedKeys(args) {
				if n == "formatString" && k == "value" {
					continue
				}
				sc.dynamic(path+".args."+k, args[k], inTemplate)
			}
			if n == "formatString" {
				if _, literal := args["value"].(string); !literal {
					sc.dynamic(path+".args.value", args["value"], inTemplate)
				}
				if s, ok := args["value"].(string); ok {
					for _, m := range placeholder.FindAllStringSubmatch(s, -1) {
						expr := strings.TrimSpace(m[1])
						if strings.HasPrefix(m[0], `\`) || strings.Contains(expr, "(") {
							continue
						}
						sc.modelPath(path+".args.value", expr, inTemplate, false)
					}
				}
			}
			return
		}
		if _, legacy := x["path"]; legacy && len(x) == 1 {
			sc.c.add(path, `a binding is {"@path": ...} in A2UI v1.0; {"path": ...} is v0.9's spelling`)
			return
		}
		for _, k := range sortedKeys(x) {
			sc.dynamic(path+"."+k, x[k], inTemplate)
		}
	case []any:
		for i, e := range x {
			sc.dynamic(fmt.Sprintf("%s[%d]", path, i), e, inTemplate)
		}
	}
}

// modelPath checks a data model path a component reads: an absolute one
// names a root the runtime fills, and a source the screen reads; a relative
// one is read inside a template alone.
func (sc *screenCheck) modelPath(path, p string, inTemplate, list bool) {
	if p == "" {
		sc.c.add(path, "is an empty path")
		return
	}
	if !strings.HasPrefix(p, "/") {
		if !inTemplate {
			sc.c.add(path, fmt.Sprintf("%q is relative, which reads an item of a list template; outside one, a path starts at a root: %s", p, rootList(modelRoots)))
		}
		return
	}
	segs := splitPointer(p)
	if len(segs) == 0 || !slices.Contains(modelRoots, segs[0]) {
		sc.c.add(path, fmt.Sprintf("%q reads no root the applet fills; one of %s", p, rootList(modelRoots)))
		return
	}
	if (segs[0] == "data" || segs[0] == "counts") && len(segs) > 1 {
		name := segs[1]
		src := sc.c.d.Sources[name]
		switch {
		case src == nil:
			sc.c.add(path, fmt.Sprintf("%q reads source %q, which the applet does not declare", p, name))
		case !slices.Contains(sc.s.Sources, name):
			sc.c.add(path, fmt.Sprintf("%q reads source %q, which this screen does not list in its sources", p, name))
		case list && segs[0] == "data" && len(segs) == 2 && src.ID != nil:
			sc.c.add(path, fmt.Sprintf("source %q reads one record by id, so it is not a list to repeat over", name))
		}
	}
}

// screenParams are the params a screen's sources and init read, which
// whatever opens it must pass.
func (d *Def) screenParams(name string) []string {
	screen := d.Screens[name]
	if screen == nil {
		return nil
	}
	need := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		if key, arg, ok := binding(v); ok && key == bindPath {
			if p, _ := arg.(string); strings.HasPrefix(p, "/params/") {
				need[strings.SplitN(strings.TrimPrefix(p, "/params/"), "/", 2)[0]] = true
			}
			return
		}
		switch x := v.(type) {
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	for _, src := range screen.Sources {
		if s := d.Sources[src]; s != nil {
			walk(s.ID)
			walk(s.Search)
			walk(s.Where)
		}
	}
	for _, st := range screen.Init {
		walk(st.ID)
		walk(st.Where)
		walk(st.Values)
	}
	return sortedKeys(need)
}
