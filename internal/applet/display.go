package applet

import (
	"context"
	"strings"
	"unicode"
)

// The display values a record carries beside its properties, at
// <record>/display/<property>: what a person reads where the stored value is
// a machine's. A2UI has no join, no conditional and no lookup, so a screen
// cannot turn `wanted` into "Want to read" or a list of names into one line;
// the server can, because it holds the declaration.
//
//   - an enum or a state: the value's declared label, else the value made
//     readable ("inProgress" reads "In progress");
//   - a list: its items' display values joined with ", ";
//   - an expanded reference: the record's title, and one not expanded:
//     nothing, so a screen never prints a record path;
//   - a bool: "Yes" or "No";
//   - a number or a string: itself. An object has none.
//
// A property with nothing to show has no display value, so a placeholder
// over it reads "" rather than a stray separator's worth of JSON.

type propDisplay struct {
	typ    string
	labels map[string]string
}

type kindDisplay map[string]propDisplay

// displays reads each kind's declaration once per render.
type displays struct {
	rt    *Runtime
	ctx   context.Context
	kinds map[string]kindDisplay
}

func (s *displays) kind(ref string) kindDisplay {
	if k, ok := s.kinds[ref]; ok {
		return k
	}
	info, err := s.rt.Data.KindByRef(s.ctx, ref)
	if err != nil {
		s.kinds[ref] = nil
		return nil
	}
	kd := kindDisplay{}
	props, _ := info.Definition["properties"].(map[string]any)
	for name, raw := range props {
		m, _ := raw.(map[string]any)
		pd := propDisplay{}
		pd.typ, _ = m["type"].(string)
		if values, ok := m["values"].([]any); ok {
			pd.labels = map[string]string{}
			for _, v := range values {
				switch x := v.(type) {
				case map[string]any:
					value, _ := x["value"].(string)
					label, _ := x["label"].(string)
					pd.labels[value] = label
				case string:
					pd.labels[x] = ""
				}
			}
		}
		kd[name] = pd
	}
	s.kinds[ref] = kd
	return kd
}

// decorate puts display values on a record in the data model's shape, and
// on each record an expanded reference holds.
func (s *displays) decorate(row map[string]any) {
	kindRef, _ := row["kind"].(string)
	props, _ := row["properties"].(map[string]any)
	kd := s.kind(kindRef)
	display := map[string]any{}
	for name, v := range props {
		for _, nested := range records(v) {
			s.decorate(nested)
		}
		if shown, ok := displayValue(kd[name], v); ok {
			display[name] = shown
		}
	}
	row["display"] = display
}

// records are the expanded records a property value holds.
func records(v any) []map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x["properties"].(map[string]any); ok {
			return []map[string]any{x}
		}
	case []any:
		var out []map[string]any
		for _, e := range x {
			out = append(out, records(e)...)
		}
		return out
	}
	return nil
}

func displayValue(pd propDisplay, v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case []any:
		var parts []string
		for _, e := range x {
			if shown, ok := displayValue(pd, e); ok && shown != "" {
				parts = append(parts, shown)
			}
		}
		if len(parts) == 0 {
			return "", false
		}
		return strings.Join(parts, ", "), true
	case map[string]any:
		if _, expanded := x["properties"]; expanded {
			title, _ := x["title"].(string)
			return title, title != ""
		}
		return "", false
	case bool:
		if x {
			return "Yes", true
		}
		return "No", true
	case string:
		if x == "" {
			return "", false
		}
		if pd.typ == "enum" || pd.typ == "state" {
			if label := pd.labels[x]; label != "" {
				return label, true
			}
			return readable(x), true
		}
		if pd.typ == "reference" {
			return "", false
		}
		return x, true
	}
	return text(v), true
}

// readable makes a stored word one a person reads: "inProgress" and
// "in_progress" read "In progress".
func readable(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '_' || r == '-':
			b.WriteRune(' ')
		case unicode.IsUpper(r) && i > 0:
			b.WriteRune(' ')
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return out
	}
	first := []rune(out)
	first[0] = unicode.ToUpper(first[0])
	return string(first)
}
