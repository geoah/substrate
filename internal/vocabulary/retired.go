package vocabulary

// The `retired:` block (decision 0055): a declaration's spent names. A package
// header retires kind names; a kind retires its own property names, enum
// values by property and states by property. Retirement is an act the author
// performs by writing the name here, never inferred from a prune, and it is
// permanent: the engine carries a stored list into every later document of the
// same package or kind (engine carryRetirements), and refuses a candidate that
// declares a retired name on every admission door (engine retirementGuards).
//
// What this file holds is the DOCUMENT's own consistency: the block's shape,
// each entry's spelling, and that no entry is also declared in the same
// document. A reservation needs no live subject, so an entry naming a property
// the kind no longer declares is not an error; it is the ordinary case once the
// property is gone.

import "fmt"

// retiredDeclaredAgain is the one sentence every door says about a name a
// retirement covers, so the author reads the same refusal from the loader, the
// apply verb, the boot upgrade and `kinds:check`.
const retiredDeclaredAgain = "a retired name is never declared again"

// parsePackageRetired reads the package header's `retired:` block into
// g.RetiredKinds. Called after the kinds are parsed, so a name both retired and
// declared refuses the header.
func (l *loader) parsePackageRetired(where string, d map[string]any, g *Package) {
	raw, present := d["retired"]
	if !present {
		return
	}
	m := asMapOrNil(raw)
	if m == nil {
		l.errf("%s: data.retired: a block of retired names ({kinds: [...]})", where)
		return
	}
	bwhere := where + ": data.retired"
	l.checkKeys(bwhere, m, packageRetiredKeys)
	seen := map[string]bool{}
	for _, v := range l.retiredList(bwhere+".kinds", m, "kinds") {
		switch {
		case !ValidName(v):
			l.errf("%s.kinds: %q must be one lowercase word ([a-z][a-z0-9]*)", bwhere, v)
		case seen[v]:
			l.errf("%s.kinds: %q is listed twice", bwhere, v)
		case g.Kinds[v] != nil:
			l.errf("%s.kinds: kind %s/%s is retired and declared; %s", bwhere, g.Identity, v, retiredDeclaredAgain)
		default:
			seen[v] = true
			g.RetiredKinds = append(g.RetiredKinds, v)
		}
	}
}

// parseKindRetirement reads a kind's `retired:` block into t.Retired. Called
// after the properties are parsed, so a name both retired and declared refuses
// the kind.
func (l *loader) parseKindRetirement(where string, d map[string]any, t *Kind) {
	raw, present := d["retired"]
	if !present {
		return
	}
	m := asMapOrNil(raw)
	if m == nil {
		l.errf("%s: data.retired: a block of retired names ({properties: [...], values: {...}, states: {...}})", where)
		return
	}
	bwhere := where + ": data.retired"
	l.checkKeys(bwhere, m, kindRetiredKeys)

	seen := map[string]bool{}
	for _, v := range l.retiredList(bwhere+".properties", m, "properties") {
		switch {
		case !ValidCamel(v):
			l.errf("%s.properties: %q must be %s", bwhere, v, camelRule)
		case seen[v]:
			l.errf("%s.properties: %q is listed twice", bwhere, v)
		case t.Props[v] != nil && t.Props[v].Implicit:
			// A stamp target the kind never spelled out is still a property
			// the machine writes on every transition, so retiring the name
			// while a transition stamps it is the same contradiction.
			l.errf("%s.properties: property %q is retired and a transition stamps it; %s", bwhere, v, retiredDeclaredAgain)
		case t.Props[v] != nil:
			l.errf("%s.properties: property %q is retired and declared; %s", bwhere, v, retiredDeclaredAgain)
		default:
			if _, builtin := reservedProps[v]; builtin {
				l.errf("%s.properties: %q is a built-in property; it is never declared, so it never retires", bwhere, v)
				continue
			}
			seen[v] = true
			t.Retired.Properties = append(t.Retired.Properties, v)
		}
	}

	// Values and states are keyed by property: the key holds to the property
	// rule, each entry to the value rule, and an entry the kind still declares
	// live under that property is the same contradiction as a declared name.
	t.Retired.Values = l.retiredByProperty(bwhere+".values", m, "values")
	t.Retired.States = l.retiredByProperty(bwhere+".states", m, "states")
	// Only the kind's own properties are consulted, and only in the shape the
	// entry reserves: a retired value bites while the property is an enum, a
	// retired state while it is a machine. Under any other datatype, or with the
	// property gone or retired, the entry lies dormant and the declaration is
	// free to move; it bites again the moment the property becomes an enum or a
	// machine that declares the name.
	for _, key := range []string{"values", "states"} {
		byProp := t.Retired.Values
		if key == "states" {
			byProp = t.Retired.States
		}
		for _, pname := range sortedKeys(mapOfAny(byProp)) {
			p := t.Props[pname]
			if p == nil {
				continue
			}
			live := map[string]bool{}
			switch {
			case key == "values" && p.Datatype == DatatypeEnum:
				for _, ev := range p.Values {
					live[ev.Value] = true
				}
			case key == "states" && p.Machine != nil:
				for _, s := range p.Machine.States {
					live[s] = true
				}
			default:
				continue
			}
			for _, v := range byProp[pname] {
				if live[v] {
					l.errf("%s.%s.%s: %q is retired and declared; %s", bwhere, key, pname, v, retiredDeclaredAgain)
				}
			}
		}
	}
}

// retiredList reads one list of names off the block. A key that is present
// and not a list is an error rather than an empty reservation, because a
// scalar there is an author who meant one name.
func (l *loader) retiredList(where string, m map[string]any, key string) []string {
	raw, present := m[key]
	if !present {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		l.errf("%s: a list of names", where)
		return nil
	}
	out := make([]string, 0, len(items))
	for _, v := range items {
		out = append(out, fmt.Sprint(v))
	}
	return out
}

// retiredByProperty reads a `{property: [value, ...]}` map off the block:
// property keys held to the camel rule, entries to the value rule, duplicates
// refused. The live check against the kind's own declaration is the caller's,
// once the map is parsed.
func (l *loader) retiredByProperty(where string, m map[string]any, key string) map[string][]string {
	raw, present := m[key]
	if !present {
		return nil
	}
	byProp := asMapOrNil(raw)
	if byProp == nil {
		l.errf("%s: a map of property name to a list of names", where)
		return nil
	}
	out := map[string][]string{}
	for _, pname := range sortedKeys(byProp) {
		if !ValidCamel(pname) {
			l.errf("%s: property %q must be %s", where, pname, camelRule)
			continue
		}
		seen := map[string]bool{}
		for _, v := range l.retiredList(where+"."+pname, byProp, pname) {
			switch {
			case !ValidValue(v):
				l.errf("%s.%s: %q must be %s", where, pname, v, valueRule)
			case seen[v]:
				l.errf("%s.%s: %q is listed twice", where, pname, v)
			default:
				seen[v] = true
				out[pname] = append(out[pname], v)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
