package vocabulary

import (
	"fmt"
	"strings"
)

// Template is a parsed display_template: literal text interleaved with
// tokens. A token is a pipe-separated list of alternatives, the first
// non-empty one rendering ({name|participants}). An alternative is a
// property name, an edge name (renders the targets' titles), a dotted
// edge.property (renders the first target's property), or one of the DERIVED
// tokens ({snippet}, {localName}, {id}).
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
	Edge string // edge name, "" for own property
	Prop string // property name, "" when the ref is a bare edge
	// Derived names a token computed from the record itself rather than read off
	// a declared property, so it needs no declaration to check against. Every
	// derived token except {snippet} yields to a REAL property of the same name
	// (Render): a kind that declares `localName` means its own property, and a
	// derived token silently shadowing it would be the worse surprise.
	Derived string
}

// The derived template tokens. {snippet} is the precedent the other two follow;
// {localName} and {id} exist because a record's identity is its id and nothing
// else, so the eight core kinds that titled themselves `{name}` have somewhere
// to point once `name` stops being a stored property. Not spelled {name}:
// llmprovider declares a real `name`.
const (
	DerivedSnippet   = "snippet"
	DerivedLocalName = "localName"
	DerivedID        = "id"
)

// derivedTokens is the closed set parseToken recognizes.
var derivedTokens = map[string]bool{
	DerivedSnippet: true, DerivedLocalName: true, DerivedID: true,
}

func (r TemplateRef) String() string {
	switch {
	case r.Derived != "":
		return r.Derived
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
		if derivedTokens[alt] {
			part.Alts = append(part.Alts, TemplateRef{Derived: alt})
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
	// Declares reports whether the kind DECLARES a property of that name,
	// whatever the row holds for it. A derived token turns on the declaration and
	// not on the value: `{localName}` on a kind that declares an optional
	// `localName` must render that property's value — including nothing, when the
	// row left it empty — because falling back to the derived value would make an
	// empty property look like a filled one.
	Declares(name string) bool
	// Edge renders an edge: prop == "" asks for the targets' titles, a
	// named prop asks for that property of the first target.
	Edge(rel, prop string) string
	// Derived renders a derived token (DerivedSnippet, DerivedLocalName,
	// DerivedID) from the record itself.
	Derived(token string) string
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
			case alt.Derived == DerivedSnippet:
				v = r.Derived(alt.Derived)
			case alt.Derived != "":
				// A REAL property of the token's name wins, by DECLARATION and not
				// by having a value: a kind that declares `localName` means its own
				// property every time it is rendered, and only a kind with no such
				// property gets the derived one. {snippet} predates the rule and
				// keeps its old meaning — it has always been derived-only, and a
				// kind declaring `snippet` would silently change what its shipped
				// template rendered.
				if r.Declares(alt.Derived) {
					v = r.Prop(alt.Derived)
				} else {
					v = r.Derived(alt.Derived)
				}
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
