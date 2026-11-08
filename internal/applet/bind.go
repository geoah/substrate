package applet

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A binding in a source or an action is one of two objects, each with that
// one key: {"@path": "/ui/..."} reads the server-side model (/ui, /params,
// and in an action /context, the tapped button's context, and /results/<as>,
// what an earlier step answered), and {"@now": "date"|"datetime"} is today
// or this instant in UTC. Components use A2UI's own {"@path"} against the
// renderer's data model, which the server never reads.
const (
	bindPath = "@path"
	bindNow  = "@now"
	bindCall = "@call"
)

// serverRoots are the roots a source's binding may read, and actionRoots an
// action's.
var (
	serverRoots = []string{"ui", "params"}
	actionRoots = []string{"ui", "params", "context", "results"}
)

// binding reports whether v is a binding object, and which. A path or a
// now is the object's one key; a call carries its args beside it.
func binding(v any) (key string, arg any, ok bool) {
	m, isMap := v.(map[string]any)
	if !isMap {
		return "", nil, false
	}
	if name, isCall := m[bindCall]; isCall {
		return bindCall, name, true
	}
	if len(m) != 1 {
		return "", nil, false
	}
	for k, a := range m {
		switch k {
		case bindPath, bindNow:
			return k, a, true
		}
	}
	return "", nil, false
}

// resolver resolves a source's or an action's values against the model.
type resolver struct {
	model map[string]any
	now   time.Time
}

func (r *resolver) resolve(v any) (any, error) {
	if key, arg, ok := binding(v); ok {
		switch key {
		case bindPath:
			p, _ := arg.(string)
			got, _ := pointerGet(r.model, p)
			return clone(got), nil
		case bindNow:
			switch arg {
			case "date":
				return r.now.UTC().Format(time.DateOnly), nil
			case "datetime", nil, true:
				return r.now.UTC().Format(time.RFC3339), nil
			}
			return nil, fmt.Errorf(`@now is "date" or "datetime", got %v`, arg)
		case bindCall:
			return r.call(v.(map[string]any))
		}
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			got, err := r.resolve(e)
			if err != nil {
				return nil, err
			}
			out[k] = got
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			got, err := r.resolve(e)
			if err != nil {
				return nil, err
			}
			out[i] = got
		}
		return out, nil
	}
	return v, nil
}

// present leaves out of a patch every property whose binding reads a path
// the model does not hold: an input that was never loaded is not the owner
// clearing a value, and writing its null would erase what is saved. A path
// that holds null or "" is kept, because that is a value someone cleared.
func (r *resolver) present(props map[string]any) map[string]any {
	out := make(map[string]any, len(props))
	for k, v := range props {
		if key, arg, ok := binding(v); ok && key == bindPath {
			p, _ := arg.(string)
			if _, found := pointerGet(r.model, p); !found {
				continue
			}
		}
		out[k] = v
	}
	return out
}

// call evaluates the one function a server-side value may call,
// formatString, so a notice or a confirmation can name what it is about.
func (r *resolver) call(m map[string]any) (any, error) {
	name, _ := m[bindCall].(string)
	if name != "formatString" {
		return nil, fmt.Errorf("a source or an action calls formatString alone, got %q", name)
	}
	args, _ := m["args"].(map[string]any)
	tmpl, err := r.resolve(args["value"])
	if err != nil {
		return nil, err
	}
	s, _ := tmpl.(string)
	return interpolate(s, func(path string) (any, bool) { return pointerGet(r.model, path) }), nil
}

// placeholder is one `${...}` of a formatString value; `\${` escapes it.
var placeholder = regexp.MustCompile(`\\?\$\{([^{}]*)\}`)

// interpolate fills a formatString value's path placeholders. A function
// call inside one is the renderer's, and stays as written.
func interpolate(s string, get func(path string) (any, bool)) string {
	return placeholder.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(m, `\`) {
			return m[1:]
		}
		expr := strings.TrimSpace(m[2 : len(m)-1])
		if strings.Contains(expr, "(") {
			return m
		}
		v, ok := get(expr)
		if !ok || v == nil {
			return ""
		}
		return text(v)
	})
}

// text is a value as a renderer would print it.
func text(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// empty is what a condition's binding resolving to nothing looks like: the
// condition is dropped, so an empty filter is every record.
func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// --- JSON Pointer (RFC 6901) ---------------------------------------------------

func splitPointer(p string) []string {
	if p == "" || p == "/" {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
	}
	return parts
}

// pointerGet reads a path; a relative one reads from the root as well.
func pointerGet(root any, p string) (any, bool) {
	cur := root
	for _, seg := range splitPointer(p) {
		switch x := cur.(type) {
		case map[string]any:
			v, ok := x[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			cur = x[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// pointerSet writes a path in a map, making the objects on the way; a nil
// value removes the key, as A2UI's updateDataModel does.
func pointerSet(root map[string]any, p string, v any) error {
	segs := splitPointer(p)
	if len(segs) == 0 {
		return fmt.Errorf("%q names no key", p)
	}
	cur := root
	for _, seg := range segs[:len(segs)-1] {
		next, ok := cur[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[seg] = next
		}
		cur = next
	}
	last := segs[len(segs)-1]
	if v == nil {
		delete(cur, last)
		return nil
	}
	cur[last] = v
	return nil
}

// uiPath is a path under /ui, without the root, or false.
func uiPath(p string) (string, bool) {
	if !strings.HasPrefix(p, "/ui/") || len(p) == len("/ui/") {
		return "", false
	}
	return strings.TrimPrefix(p, "/ui"), true
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}
