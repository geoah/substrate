package applet

import (
	"fmt"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// outlineItems is how many items of a list the outline draws, and the depth
// and the line count it stops at: a draft whose components contain
// themselves is previewed too, and must end.
const (
	outlineItems    = 3
	outlineMaxDepth = 24
	outlineMaxLines = 400
)

// Outline draws a frame's surface as text.
func Outline(d *Def, f *substrate.AppletFrame) substrate.AppletPreview {
	p := substrate.AppletPreview{Screen: f.State.Screen, Screens: d.screenNames(), Actions: map[string]string{}}
	for _, name := range sortedKeys(d.Actions) {
		var ops []string
		for _, s := range d.Actions[name] {
			ops = append(ops, s.Op)
		}
		p.Actions[name] = strings.Join(ops, " → ")
	}
	var create *substrate.A2UICreateSurface
	for _, m := range f.Messages {
		if m.CreateSurface != nil {
			create = m.CreateSurface
		}
	}
	if create == nil {
		return p
	}
	p.Counts, _ = create.DataModel["counts"].(map[string]any)
	o := &outliner{byID: map[string]map[string]any{}, model: create.DataModel, seen: map[string]bool{}}
	for _, c := range create.Components {
		id, _ := c["id"].(string)
		o.byID[id] = c
	}
	o.draw("root", "", 0)
	p.Outline = strings.TrimRight(o.b.String(), "\n")
	p.Unresolved = o.unresolved
	return p
}

type outliner struct {
	lines      int
	cut        bool
	b          strings.Builder
	byID       map[string]map[string]any
	model      map[string]any
	unresolved []string
	seen       map[string]bool
}

// drawnProps are the props worth a line, in the order they are drawn.
var drawnProps = []string{"text", "label", "url", "value", "placeholder", "variant", "options", "action", "tabs"}

func (o *outliner) draw(id, scope string, depth int) {
	c := o.byID[id]
	indent := strings.Repeat("  ", depth)
	if depth > outlineMaxDepth || o.lines >= outlineMaxLines {
		if !o.cut {
			o.cut = true
			fmt.Fprintf(&o.b, "%s… (the outline stops here: too deep or too long)\n", indent)
		}
		return
	}
	o.lines++
	if c == nil {
		fmt.Fprintf(&o.b, "%s%s ⚠ no such component\n", indent, id)
		return
	}
	typ, _ := c["component"].(string)
	var parts []string
	for _, k := range drawnProps {
		v, ok := c[k]
		if !ok {
			continue
		}
		parts = append(parts, k+"="+o.show(id, k, v, scope))
	}
	fmt.Fprintf(&o.b, "%s%s %s %s\n", indent, id, typ, strings.Join(parts, " "))
	for _, key := range []string{"child", "trigger", "content"} {
		if s, ok := c[key].(string); ok {
			o.draw(s, scope, depth+1)
		}
	}
	if tabs, ok := c["tabs"].([]any); ok {
		for _, t := range tabs {
			if m, ok := t.(map[string]any); ok {
				if s, ok := m["child"].(string); ok {
					o.draw(s, scope, depth+1)
				}
			}
		}
	}
	switch ch := c["children"].(type) {
	case []any:
		for _, e := range ch {
			if s, ok := e.(string); ok {
				o.draw(s, scope, depth+1)
			}
		}
	case map[string]any:
		path, _ := ch["path"].(string)
		tmpl, _ := ch["componentId"].(string)
		full := o.abs(path, scope)
		items, _ := o.get(full)
		list, isList := items.([]any)
		if !isList {
			fmt.Fprintf(&o.b, "%s  ⚠ template over %s, which holds no list\n", indent, full)
			o.unresolved = append(o.unresolved, fmt.Sprintf("%s.children: %s holds no list", id, full))
			return
		}
		fmt.Fprintf(&o.b, "%s  (%d items at %s)\n", indent, len(list), full)
		for i := range list {
			if i == outlineItems {
				fmt.Fprintf(&o.b, "%s  … %d more\n", indent, len(list)-outlineItems)
				break
			}
			fmt.Fprintf(&o.b, "%s  [%d]\n", indent, i)
			o.draw(tmpl, fmt.Sprintf("%s/%d", full, i), depth+2)
		}
	}
}

func (o *outliner) abs(p, scope string) string {
	if strings.HasPrefix(p, "/") {
		return p
	}
	return scope + "/" + p
}

func (o *outliner) get(p string) (any, bool) { return pointerGet(o.model, p) }

// show is a prop as the outline prints it, a resolved binding as its value.
func (o *outliner) show(id, prop string, v any, scope string) string {
	switch x := v.(type) {
	case string:
		return quote(x)
	case map[string]any:
		if p, ok := x["@path"].(string); ok {
			full := o.abs(p, scope)
			got, found := o.get(full)
			if !found || got == nil {
				o.miss(id, prop, full, scope)
				return "@" + full + "=∅"
			}
			return "@" + full + "=" + quote(text(got))
		}
		if name, ok := x["@call"].(string); ok {
			args, _ := x["args"].(map[string]any)
			if name == "formatString" {
				s, _ := args["value"].(string)
				return quote(interpolate(s, func(p string) (any, bool) {
					full := o.abs(p, scope)
					got, found := o.get(full)
					switch got.(type) {
					case nil:
						o.miss(id, prop, full, scope)
					case []any, map[string]any:
						o.whole(id, prop, full, scope)
					}
					return got, found
				}))
			}
			var shown []string
			for _, k := range sortedKeys(args) {
				shown = append(shown, k+": "+o.show(id, prop, args[k], scope))
			}
			return name + "(" + strings.Join(shown, ", ") + ")"
		}
		if ev, ok := x["event"].(map[string]any); ok {
			name, _ := ev["name"].(string)
			return "→" + name
		}
	case []any:
		return fmt.Sprintf("[%d]", len(x))
	}
	return text(v)
}

// miss records a binding that read nothing, once per component, prop and
// path: inside a template, the item's index is left out so ten rows reading
// one wrong path report it once.
func (o *outliner) miss(id, prop, full, scope string) {
	at := full
	if scope != "" {
		at = strings.TrimPrefix(full, scope)
		at = "<item>" + at
	}
	key := fmt.Sprintf("%s.%s: %s resolved to nothing", id, prop, at)
	if o.seen[key] {
		return
	}
	o.seen[key] = true
	o.unresolved = append(o.unresolved, key+o.nearest(full, scope))
	sort.Strings(o.unresolved)
}

// whole records a placeholder that reads a list or an object, which A2UI
// draws as JSON (["Frank Herbert"]): the owner wants one item or one field.
func (o *outliner) whole(id, prop, full, scope string) {
	at := full
	if scope != "" {
		at = "<item>" + strings.TrimPrefix(full, scope)
	}
	line := fmt.Sprintf("%s.%s: %s is a list or an object, drawn as JSON; name one item (%s/0) or one field", id, prop, at, at)
	if o.seen[line] {
		return
	}
	o.seen[line] = true
	o.unresolved = append(o.unresolved, line)
	sort.Strings(o.unresolved)
}

// nearest says what the deepest part of a missed path that does exist
// holds, which tells a wrong path from a value a record does not have.
func (o *outliner) nearest(full, scope string) string {
	segs := splitPointer(full)
	for i := len(segs) - 1; i > 0; i-- {
		prefix := "/" + strings.Join(segs[:i], "/")
		got, ok := o.get(prefix)
		if !ok {
			continue
		}
		at := prefix
		if scope != "" && strings.HasPrefix(prefix, scope) {
			at = "<item>" + strings.TrimPrefix(prefix, scope)
		}
		if m, isMap := got.(map[string]any); isMap {
			return fmt.Sprintf("; %s has %s", at, strings.Join(sortedKeys(m), ", "))
		}
		return fmt.Sprintf("; %s is %s, not an object", at, quote(text(got)))
	}
	return ""
}

func quote(s string) string {
	if r := []rune(s); len(r) > 80 {
		s = string(r[:77]) + "…"
	}
	return fmt.Sprintf("%q", s)
}
