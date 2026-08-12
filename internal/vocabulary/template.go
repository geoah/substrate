package vocabulary

import (
	"fmt"
	"strings"
)

// Template is a parsed display_template: literal text interleaved with
// tokens. A token is a pipe-separated list of alternatives, the first
// non-empty one rendering ({name|participants}). An alternative is a
// property name, an edge name (renders the targets' titles), a dotted
// edge.property (renders the first target's property), or the special
// {snippet}.
type Template struct {
	Raw   string
	Parts []TemplatePart
}

// TemplatePart is either literal text (Alts empty) or one token.
type TemplatePart struct {
	Literal string
	Alts    []TemplateRef
}

// TemplateRef is one alternative inside a token.
type TemplateRef struct {
	Edge    string // edge name, "" for own property
	Prop    string // property name, "" when the ref is a bare edge
	Snippet bool
}

func (r TemplateRef) String() string {
	switch {
	case r.Snippet:
		return "snippet"
	case r.Edge != "" && r.Prop != "":
		return r.Edge + "." + r.Prop
	case r.Edge != "":
		return r.Edge
	default:
		return r.Prop
	}
}

// ParseTemplate parses a display_template, rejecting unbalanced braces and
// identifiers that break the naming rules.
func ParseTemplate(s string) (*Template, error) {
	t := &Template{Raw: s}
	var lit strings.Builder
	for i := 0; i < len(s); {
		switch s[i] {
		case '{':
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return nil, fmt.Errorf("unclosed { at offset %d", i)
			}
			if lit.Len() > 0 {
				t.Parts = append(t.Parts, TemplatePart{Literal: lit.String()})
				lit.Reset()
			}
			body := s[i+1 : i+end]
			part, err := parseToken(body)
			if err != nil {
				return nil, err
			}
			t.Parts = append(t.Parts, part)
			i += end + 1
		case '}':
			return nil, fmt.Errorf("unexpected } at offset %d", i)
		default:
			lit.WriteByte(s[i])
			i++
		}
	}
	if lit.Len() > 0 {
		t.Parts = append(t.Parts, TemplatePart{Literal: lit.String()})
	}
	return t, nil
}

func parseToken(body string) (TemplatePart, error) {
	var part TemplatePart
	if strings.TrimSpace(body) == "" {
		return part, fmt.Errorf("empty {} token")
	}
	for _, alt := range strings.Split(body, "|") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			return part, fmt.Errorf("empty alternative in {%s}", body)
		}
		if alt == "snippet" {
			part.Alts = append(part.Alts, TemplateRef{Snippet: true})
			continue
		}
		name, prop, dotted := strings.Cut(alt, ".")
		if dotted {
			if !ValidCamel(name) || !ValidCamel(prop) {
				return part, fmt.Errorf("%q is not edge.property", alt)
			}
			part.Alts = append(part.Alts, TemplateRef{Edge: name, Prop: prop})
			continue
		}
		if !ValidCamel(alt) {
			return part, fmt.Errorf("%q is not a property name", alt)
		}
		part.Alts = append(part.Alts, TemplateRef{Prop: alt})
	}
	return part, nil
}

// Refs lists every alternative referenced by the template, in order.
func (t *Template) Refs() []TemplateRef {
	var out []TemplateRef
	for _, p := range t.Parts {
		out = append(out, p.Alts...)
	}
	return out
}

// EdgeRefs lists the distinct edge names the template traverses.
func (t *Template) EdgeRefs() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range t.Refs() {
		if r.Edge == "" || seen[r.Edge] {
			continue
		}
		seen[r.Edge] = true
		out = append(out, r.Edge)
	}
	return out
}

// Resolver supplies the values a template renders. Missing values return
// "" so the next alternative gets its turn.
type Resolver interface {
	// Prop renders a declared property of the record itself.
	Prop(name string) string
	// Edge renders an edge: prop == "" asks for the targets' titles, a
	// named prop asks for that property of the first target.
	Edge(rel, prop string) string
	// Snippet is the first 80 characters of the longest text-family
	// property.
	Snippet() string
}

// Render resolves the template, dropping tokens whose alternatives are all
// empty. A template that resolves to nothing renders "".
func (t *Template) Render(r Resolver) string {
	var b strings.Builder
	for _, p := range t.Parts {
		if len(p.Alts) == 0 {
			b.WriteString(p.Literal)
			continue
		}
		for _, alt := range p.Alts {
			var v string
			switch {
			case alt.Snippet:
				v = r.Snippet()
			case alt.Edge != "":
				v = r.Edge(alt.Edge, alt.Prop)
			default:
				// A bare identifier is a property or, failing that, an
				// edge ("{name|participants}").
				if v = r.Prop(alt.Prop); v == "" {
					v = r.Edge(alt.Prop, "")
				}
			}
			if v != "" {
				b.WriteString(v)
				break
			}
		}
	}
	return strings.TrimSpace(b.String())
}
