package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// typesPageSize is the server's maxPageSize: the largest page the registry
// list will serve, so the fewest round trips for the WHOLE registry.
const typesPageSize = 500

// typesPageLimit bounds the cursor walk. A server that kept handing back a
// cursor would otherwise loop forever; the registry is a few hundred rows, so
// this only ever fires on a broken server.
const typesPageLimit = 100

// fetchTypes reads the type registry, ENTIRELY. The registry is served as
// records of core.substrate.reamde.dev/kind, whose properties carry the
// projection; a bare TypeInfo array is accepted too.
//
// It pages. The collection list defaults to 50 rows and the shipped schema
// alone declares more than that before a single bundle is installed, so a
// single unpaged read sees the newest 50 types and NOTHING else — which
// resolves a bare plural against a truncated registry, and that is worse than
// slow: it reports shipped vocabulary as unknown, and it can find exactly one
// match for a name several authorities declare and silently pick it.
func (c *client) fetchTypes(ctx context.Context) ([]substrate.KindInfo, error) {
	out := make([]substrate.KindInfo, 0, typesPageSize)
	q := url.Values{"first": []string{strconv.Itoa(typesPageSize)}}
	for range typesPageLimit {
		items, cursor, err := c.fetchTypePage(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, raw := range items {
			ti, ok := decodeTypeInfo(raw)
			if !ok {
				continue
			}
			out = append(out, ti)
		}
		if cursor == "" || len(items) == 0 {
			break
		}
		q.Set("after", cursor)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity < out[j].Identity })
	return out, nil
}

// fetchTypePage reads one page of the registry: its rows and the continuation
// cursor ("" when exhausted, and always "" for the bare-array shape, which
// does not page).
func (c *client) fetchTypePage(ctx context.Context, q url.Values) ([]json.RawMessage, string, error) {
	resp, err := c.send(ctx, http.MethodGet, collectionPath(coreAuthority, kindCollection), q, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	var body json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, "", fmt.Errorf("decode types response: %w", err)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err == nil {
		return items, "", nil
	}
	var page struct {
		Records []json.RawMessage `json:"records"`
		Cursor  string            `json:"cursor"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", fmt.Errorf("decode types response: %w", err)
	}
	return page.Records, page.Cursor, nil
}

func decodeTypeInfo(raw json.RawMessage) (substrate.KindInfo, bool) {
	// "version" is the declared version in the bare-TypeInfo shape but the
	// CAS version in the record shape (whose declared version rides in
	// properties) — both numbers now, still two different numbers, so it
	// cannot decode into a typed field.
	var r struct {
		Identity    string         `json:"identity"`
		ID          string         `json:"id"`
		Name        string         `json:"name"`
		Authority   string         `json:"authority"`
		Version     any            `json:"version"`
		Plural      string         `json:"plural"`
		Source      string         `json:"source"`
		Description string         `json:"description"`
		Definition  map[string]any `json:"definition"`
		Properties  map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return substrate.KindInfo{}, false
	}
	definition := r.Definition
	if definition == nil {
		definition, _ = r.Properties["definition"].(map[string]any)
	}
	// THE PROPERTIES ARE THE DECLARATION: a typed row carries description and
	// the names object directly, and `definition` survives only for a row an
	// older substrate wrote. The bare-TypeInfo shape keeps its own fields.
	if definition == nil {
		definition = r.Properties
	}
	description, _ := definition["description"].(string)
	names, _ := definition["names"].(map[string]any)
	// The record shape's own `version` is the CAS counter; its DECLARED
	// version rides in properties. The bare-TypeInfo shape has no
	// properties, so its `version` field is the declared one.
	declaredVersion, ok := vocabulary.VersionValue(r.Properties["version"])
	if !ok || declaredVersion == 0 {
		declaredVersion, _ = vocabulary.VersionValue(r.Version)
	}
	ti := substrate.KindInfo{
		Identity:    r.Identity,
		Name:        firstNonEmpty(r.Name, propString(names, "singular"), propString(r.Properties, "name")),
		Authority:   firstNonEmpty(r.Authority, propString(r.Properties, "authority")),
		Version:     declaredVersion,
		Plural:      firstNonEmpty(r.Plural, propString(names, "plural"), propString(r.Properties, "plural")),
		Source:      firstNonEmpty(r.Source, propString(r.Properties, "source")),
		Description: firstNonEmpty(r.Description, description),
		Definition:  definition,
	}
	if ti.Identity == "" {
		ti.Identity = r.ID
	}
	if ti.Identity == "" && ti.Name != "" && ti.Authority != "" {
		ti.Identity = vocabulary.KindRef(ti.Authority, ti.Name)
	}
	// Fill only the missing half: a typed row authors `authority` and derives
	// its name from the id, and the derivation must never clobber the
	// authored value.
	if name, authority, ok := splitIdentity(ti.Identity); ok {
		if ti.Name == "" {
			ti.Name = name
		}
		if ti.Authority == "" {
			ti.Authority = authority
		}
	}
	if ti.Identity == "" {
		return substrate.KindInfo{}, false
	}
	return ti, true
}

func propString(properties map[string]any, key string) string {
	if properties == nil {
		return ""
	}
	if s, ok := properties[key].(string); ok {
		return s
	}
	return ""
}

// stateProperties names a type's state properties, sorted. A state is a
// property whose declared type is `state`, so its current value sits in an
// record's properties with everything else — which makes the declaration the
// only thing that can say which of those values is a state, and the only
// source the STATE column has.
func stateProperties(ti substrate.KindInfo) []string {
	declared, _ := ti.Definition["properties"].(map[string]any)
	names := make([]string, 0, len(declared))
	for name, raw := range declared {
		decl, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if declaredType, _ := decl["type"].(string); declaredType == "state" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// statesFor resolves the state properties of a collection's type, best-effort:
// a registry the CLI cannot reach costs an empty STATE column, never the read
// itself. A qualified plural resolves without a round trip, so this is where
// that collection's registry lookup happens — and only for the output formats
// that have a STATE column to fill.
func (a *app) statesFor(ctx context.Context, col collection) []string {
	types, err := a.types(ctx)
	if err != nil {
		return nil
	}
	for _, ti := range types {
		if ti.Authority == col.Authority && collectionOf(ti) == col.Name {
			return stateProperties(ti)
		}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// splitIdentity splits "<name>.<authority>" at the first dot.
// splitIdentity splits a kind identity into its name and authority. The
// reference is `{authority}/{name}` and splits on its one slash; a bare
// repository-local name has no authority to derive.
func splitIdentity(identity string) (name, authority string, ok bool) {
	authority, name = vocabulary.SplitKindRef(identity)
	if authority == "" || name == "" {
		return "", "", false
	}
	return name, authority, true
}

// collection is a resolved REST collection. Name is the collection SEGMENT,
// which is the kind's name (decision 0028), so Authority and Name together
// spell the kind reference the path carries.
type collection struct {
	Authority string
	Name      string
	// Identity is the type identity when known ("" when resolved purely
	// syntactically from a qualified argument).
	Identity string
}

// ref renders a RECORD reference: the kind reference, then the id.
func (c collection) ref(id string) string {
	name := c.Identity
	if name == "" {
		name = vocabulary.KindRef(c.Authority, c.Name)
	}
	return name + "/" + id
}

// resolveCollection turns a CLI argument into a collection. The qualified form
// "<authority>/<kind>" IS the collection path, so it wins outright and needs no
// round trip; a bare argument with -g is taken literally under that authority;
// a bare argument otherwise resolves against the kind registry, where the
// kind's declared plural still matches so `get tasks` keeps working.
func (a *app) resolveCollection(ctx context.Context, arg, authority string) (collection, error) {
	if g, name := vocabulary.SplitKindRef(arg); g != "" {
		col := collection{Authority: g, Name: name}
		if ti, found := a.lookupCached(name, g); found {
			col.Name, col.Identity = collectionOf(ti), ti.Identity
		}
		return col, nil
	}
	if authority != "" {
		return collection{Authority: authority, Name: arg}, nil
	}
	types, err := a.types(ctx)
	if err != nil {
		return collection{}, err
	}
	var matches []substrate.KindInfo
	for _, ti := range types {
		if ti.Plural == arg || ti.Name == arg {
			matches = append(matches, ti)
		}
	}
	switch len(matches) {
	case 0:
		return collection{}, fmt.Errorf("no kind named %q; run `substratectl kinds` to list them", arg)
	case 1:
		ti := matches[0]
		return collection{Authority: ti.Authority, Name: collectionOf(ti), Identity: ti.Identity}, nil
	}
	names := make([]string, 0, len(matches))
	for _, ti := range matches {
		names = append(names, vocabulary.KindRef(ti.Authority, collectionOf(ti)))
	}
	return collection{}, fmt.Errorf("%q is ambiguous across authorities: %s (qualify it as authority/kind or pass -g)",
		arg, strings.Join(names, ", "))
}

// collectionOf is a kind's collection segment: its NAME. A KindInfo whose name
// a server did not fill falls back to the declared plural, which is the only
// place the CLI still reads one — an old server's answer, never a path it
// builds.
func collectionOf(ti substrate.KindInfo) string {
	if ti.Name != "" {
		return ti.Name
	}
	return ti.Plural
}

// types fetches and caches the registry for the life of one command.
func (a *app) types(ctx context.Context) ([]substrate.KindInfo, error) {
	if a.typeCache != nil {
		return a.typeCache, nil
	}
	cl, err := a.client()
	if err != nil {
		return nil, err
	}
	types, err := cl.fetchTypes(ctx)
	if err != nil {
		return nil, err
	}
	a.typeCache = types
	return types, nil
}

func (a *app) lookupCached(nameOrPlural, authority string) (substrate.KindInfo, bool) {
	for _, ti := range a.typeCache {
		if ti.Authority == authority && (ti.Plural == nameOrPlural || ti.Name == nameOrPlural) {
			return ti, true
		}
	}
	return substrate.KindInfo{}, false
}

// collectionForKind resolves a manifest's `kind` — a kind reference — to its
// REST collection. A bare reference resolves the way a bare plural does, and
// errors the same way when ambiguous.
func (a *app) collectionForKind(ctx context.Context, ref string) (collection, error) {
	authority, name := vocabulary.SplitKindRef(ref)
	if authority == "" {
		return a.resolveCollection(ctx, name, "")
	}
	types, err := a.types(ctx)
	if err != nil {
		return collection{}, err
	}
	var elsewhere []string
	var pluralOnly string
	for _, ti := range types {
		if ti.Authority == authority && ti.Plural == name && ti.Name != name {
			pluralOnly = ti.Name
		}
		if ti.Name != name {
			continue
		}
		if ti.Authority == authority {
			return collection{Authority: ti.Authority, Name: collectionOf(ti), Identity: ti.Identity}, nil
		}
		elsewhere = append(elsewhere, ti.Authority)
	}
	if pluralOnly != "" {
		return collection{}, fmt.Errorf("`kind` names the singular (%q), not the plural %q", pluralOnly, name)
	}
	if len(elsewhere) > 0 {
		return collection{}, fmt.Errorf("no kind %q under authority %q; it is published by %s (fix the manifest's kind)",
			name, authority, strings.Join(elsewhere, ", "))
	}
	return collection{}, fmt.Errorf("unknown kind %q; run `substratectl kinds` to list them", ref)
}
