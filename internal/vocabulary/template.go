package vocabulary

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Template is a parsed display_template: literal text interleaved with
// tokens. A token is a pipe-separated list of alternatives, the first
// non-empty one rendering ({name|participants}). An alternative is a
// property name, a reference property's name (renders the referents' titles), a
// dotted reference.property (renders the first referent's property), a LIST
// path ({emails[]} the first value of a repeated property, {names[].displayName}
// the first entry's field), or one of the DERIVED tokens ({snippet},
// {localName}, {id}).
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
	// Ref is the HEAD of a dotted alternative: the reference or object property
	// the token reads through, "" when the alternative names the record's own
	// property.
	Ref  string
	Prop string // property name, "" when the alternative is a bare head
	// List is the `[]` hop: the alternative reads the HEAD of a repeated
	// property rather than all of it — `{emails[]}` the first value of a
	// repeated scalar (Ref ""), `{names[].displayName}` the first entry's
	// field of a repeated object or the first referent's property of a
	// repeated reference (Ref the head). An empty list renders nothing, so the
	// token's next alternative gets its turn, which is the whole reason a
	// provider's verbatim array can title a record.
	List bool
	// Derived names a token computed from the record itself rather than read off
	// a declared property, so it needs no declaration to check against. Every
	// derived token except {snippet} yields to a REAL property of the same name
	// (Render): a kind that declares `localName` means its own property, and a
	// derived token silently shadowing it would be the worse surprise.
	Derived string
}

// The derived template tokens. {snippet} is the precedent the other two follow;
// {localName} and {id} exist because a record's identity is its id and nothing
// else, so the nine core kinds that titled themselves `{name}` have somewhere
// to point once `name` stops being a stored property. Not spelled {name}:
// kinds like blob declare a real `name`.
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
	case r.Ref != "" && r.List:
		return r.Ref + "[]." + r.Prop
	case r.Ref != "":
		return r.Ref + "." + r.Prop
	case r.List:
		return r.Prop + "[]"
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
		// The `[]` hop is a SUFFIX on the head, the one spelling the map and
		// match paths already use (ParsePath): `names[].displayName` and
		// `emails[]`. No subscript — `names[0]` would promise an order the
		// provider's array does not have.
		head, list := strings.CutSuffix(name, "[]")
		if dotted {
			if !ValidCamel(head) || !ValidCamel(prop) {
				return part, fmt.Errorf("%q is not reference.property", alt)
			}
			part.Alts = append(part.Alts, TemplateRef{Ref: head, Prop: prop, List: list})
			continue
		}
		if !ValidCamel(head) {
			return part, fmt.Errorf("%q is not a property name", alt)
		}
		part.Alts = append(part.Alts, TemplateRef{Prop: head, List: list})
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

// RefHeads lists the distinct property names the template reads THROUGH: the
// head of every dotted alternative.
func (t *Template) RefHeads() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range t.Refs() {
		if r.Ref == "" || seen[r.Ref] {
			continue
		}
		seen[r.Ref] = true
		out = append(out, r.Ref)
	}
	return out
}

// Resolver supplies the values a template renders. Missing values return
// "" so the next alternative gets its turn.
type Resolver interface {
	// Prop renders a declared property of the record itself.
	Prop(name string) string
	// Declares reports whether the kind DECLARES anything of that name — a
	// property of that name, whatever the row
	// holds for it. A derived token turns on the declaration and not on the value:
	// `{localName}` on a kind that declares an optional `localName` must render
	// that property's value — including nothing, when the row left it empty —
	// because falling back to the derived value would make an empty property look
	// like a filled one.
	Declares(name string) bool
	// Reference renders through a reference property: prop == "" asks for the
	// referents' titles, a named prop asks for that property of the first
	// referent.
	Reference(name, prop string) string
	// First renders the HEAD of a repeated property: field == "" the first
	// value of a repeated scalar, a named field the first entry's field of a
	// repeated object (or the first referent's property of a repeated
	// reference). An empty list is "", which is what lets the next alternative
	// answer.
	First(name, field string) string
	// Derived renders a derived token (DerivedSnippet, DerivedLocalName,
	// DerivedID) from the record itself.
	Derived(token string) string
}

// Render resolves the template. A token whose alternatives are all empty
// renders nothing and takes the separator joining it to its neighbor with it
// (dropSeparators), so `{decision}: {winner}` with no decision is the winner,
// not ": " and the winner. A template that resolves to nothing renders "".
func (t *Template) Render(r Resolver) string {
	out := make([]string, len(t.Parts))
	for i, p := range t.Parts {
		if len(p.Alts) == 0 {
			out[i] = p.Literal
			continue
		}
		out[i] = p.resolve(r)
	}
	t.dropSeparators(out)
	return strings.TrimSpace(strings.Join(out, ""))
}

// dropSeparators edits the rendered parts in place around every empty token.
// Only a literal's SEPARATOR edge goes — whitespace and punctuation, never a
// letter or a digit — because the words of a literal are the author's
// ("Issue {x}" stays "Issue"), and a rendered value is never edited at all,
// so a title that is "Q3: plan" or "C++" survives whatever surrounds it.
//
// Around one empty token: a bracket or quote pair enclosing it goes whole
// ("{label} ({wire})" is the label); a sigil written straight before it
// ("#{n}") is its own and goes; then the separator on its left and the one
// on its right have become one gap. With content on both sides the gap keeps
// ONE of them, the left one where there is one ("{a}/{b}/{c}" with no b is
// "a/c", "{d}: {w} + {l}" with no w is "d: l"); with content on one side
// only, the gap is at an edge and both go.
func (t *Template) dropSeparators(out []string) {
	token := func(i int) bool { return len(t.Parts[i].Alts) > 0 }
	// Content is decided on what the parts held before any edit: an edit only
	// ever removes separators, which are never content.
	content := make([]bool, len(out))
	for i, v := range out {
		content[i] = v != "" && (token(i) || strings.IndexFunc(v, isWordRune) >= 0)
	}
	for i := range out {
		if !token(i) || out[i] != "" {
			continue
		}
		left, right := -1, -1
		if i > 0 && !token(i-1) {
			left = i - 1
		}
		if i+1 < len(out) && !token(i+1) {
			right = i + 1
		}
		if left >= 0 && right >= 0 {
			if l, r, ok := stripEnclosing(out[left], out[right]); ok {
				out[left], out[right] = l, r
			}
		}
		if left >= 0 {
			out[left] = strings.TrimRightFunc(out[left], isSigil)
		}
		before := slices.Contains(content[:i], true)
		after := slices.Contains(content[i+1:], true)
		lrun, rrun := 0, 0
		if left >= 0 {
			lrun = len(out[left]) - len(strings.TrimRightFunc(out[left], isSeparatorRune))
		}
		if right >= 0 {
			rrun = len(out[right]) - len(strings.TrimLeftFunc(out[right], isSeparatorRune))
		}
		switch {
		case before && after:
			if lrun > 0 && rrun > 0 {
				out[right] = out[right][rrun:]
			}
		default:
			if left >= 0 {
				out[left] = out[left][:len(out[left])-lrun]
			}
			if right >= 0 {
				out[right] = out[right][rrun:]
			}
		}
	}
}

// enclosers are the pairs an empty token takes with it when they enclose it.
var enclosers = map[rune]rune{'(': ')', '[': ']', '"': '"', '“': '”', '«': '»'}

// stripEnclosing removes an opener ending left and its closer opening right,
// with the whitespace written before the opener.
func stripEnclosing(left, right string) (string, string, bool) {
	open, size := utf8.DecodeLastRuneInString(left)
	closer, ok := enclosers[open]
	if !ok || !strings.HasPrefix(right, string(closer)) {
		return left, right, false
	}
	return strings.TrimRightFunc(left[:len(left)-size], unicode.IsSpace), right[len(string(closer)):], true
}

// isWordRune is what makes a literal content rather than a separator.
func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// isSigil is a mark written straight before a token that belongs to it:
// "#{number}", "@{login}".
func isSigil(r rune) bool { return r == '#' || r == '@' }

// isSeparatorRune is whitespace and the punctuation that joins two values —
// ": ", " + ", " — ", "/" — and not a bracket or a quote, which enclose one
// value rather than join two, nor a sigil, which belongs to the token after
// it.
func isSeparatorRune(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	if isSigil(r) || r == '\'' || r == '"' || unicode.In(r, unicode.Ps, unicode.Pe, unicode.Pi, unicode.Pf) {
		return false
	}
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// resolve renders one token: the first alternative with a value, or "".
func (p TemplatePart) resolve(r Resolver) string {
	for _, alt := range p.Alts {
		var v string
		switch {
		case alt.Derived == DerivedSnippet:
			v = r.Derived(alt.Derived)
		case alt.Derived != "":
			// A REAL declaration of the token's name wins, by DECLARATION and
			// not by having a value: a kind that declares `localName` means its
			// own property every time it is rendered, and only a kind that
			// declares none gets the derived one. Declared, the token resolves
			// exactly as the bare identifier below does — the property's value,
			// then the referents' titles — because a derived token that skipped
			// that hop would render an id where the model says a referent's title.
			//
			// {snippet} predates the rule and keeps its old meaning: it has
			// always been derived-only, and a kind declaring `snippet` would
			// silently change what its shipped template rendered.
			if !r.Declares(alt.Derived) {
				v = r.Derived(alt.Derived)
			} else if v = r.Prop(alt.Derived); v == "" {
				v = r.Reference(alt.Derived, "")
			}
		case alt.List && alt.Ref != "":
			v = r.First(alt.Ref, alt.Prop)
		case alt.List:
			v = r.First(alt.Prop, "")
		case alt.Ref != "":
			v = r.Reference(alt.Ref, alt.Prop)
		default:
			// A bare identifier is a property's own value or, failing that,
			// the titles a reference property names ("{name|participants}").
			if v = r.Prop(alt.Prop); v == "" {
				v = r.Reference(alt.Prop, "")
			}
		}
		if v != "" {
			return v
		}
	}
	return ""
}
