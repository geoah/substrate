package api

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// THE PATH GRAMMAR (decisions 0033, 0042).
//
//	/{authority}/{kind}         a kind's collection
//	/{authority}/{kind}/{id}    a record
//
// Every kind carries an authority (decision 0042), so a collection is always
// two segments and a record always three, and the two shapes are told apart by
// SEGMENT COUNT, never by inspecting a segment. The collection segment is the
// kind's NAME, so everything after the version prefix is the kind reference,
// and a record's path is the record path a `reference` property stores
// (vocabulary.RecordPath) character for character. There is no separator
// segment: sub-resources hang one level below the id, and non-record endpoints
// sit at the version root, so nothing needs one.

// address is what a REST path addresses: the collection's authority, the kind
// name, and the record id ("" on a collection route).
type address struct {
	authority string
	kind      string
	id        string
}

// ref is the kind reference the collection segments spell.
func (a address) ref() string { return vocabulary.KindRef(a.authority, a.kind) }

// path is the address as a URL under the version prefix, which for a record is
// also its stored reference value (vocabulary.RecordPath).
func (a address) path() string {
	p := "/api/" + APIVersion + "/" + a.ref()
	if a.id != "" {
		p += "/" + a.id
	}
	return p
}

// reservedRecordID reports whether an id collides with a record sub-resource
// segment. `incoming` is a static route below a record (`…/{id}/incoming`), so
// an id spelled that way is refused as a record id, both read and write,
// keeping the shadow corner symmetric (decision 0033).
func reservedRecordID(id string) bool {
	return id == "incoming"
}

// addressed reads what a REST path addresses, by SEGMENT COUNT alone: three
// segments name a record ({authority}/{kind}/{id}), two a collection
// ({authority}/{kind}). Every kind carries an authority (decision 0042), so a
// one-segment path names nothing and a second return of false is a 404 rather
// than a lookup.
func addressed(r *http.Request) (address, bool) {
	s1, s2, s3 := pathParam(r, "a1"), pathParam(r, "a2"), pathParam(r, "a3")
	switch {
	case s3 != "":
		return address{authority: s1, kind: s2, id: s3}, true
	case s2 != "":
		return address{authority: s1, kind: s2}, true
	default:
		return address{}, false
	}
}

// collection resolves the addressed collection to a declared kind, refusing an
// address of the wrong shape FIRST.
//
// wantID says which shape the caller serves. A method that means one thing at a
// collection and nothing at a record (or the reverse) answers 405 naming the
// path that works, and never falls through to a write: `POST` at a record path
// used to resolve the collection, discard the id and create a record under a
// server-assigned id (#202), and `PUT` at a collection created under a random
// one.
func (h *handler) collection(w http.ResponseWriter, r *http.Request, wantID bool) (substrate.Dataset, substrate.KindInfo, address, bool) {
	addr, ok := addressed(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "no such API path: "+r.URL.Path)
		return nil, substrate.KindInfo{}, address{}, false
	}
	if wantID && addr.id == "" {
		writeError(w, http.StatusMethodNotAllowed, codeBadRequest,
			r.Method+" addresses a record, not a collection: PUT "+addr.path()+
				"/{id} writes one record, POST "+addr.path()+" creates one")
		return nil, substrate.KindInfo{}, address{}, false
	}
	if !wantID && addr.id != "" {
		coll := address{authority: addr.authority, kind: addr.kind}
		writeError(w, http.StatusMethodNotAllowed, codeBadRequest,
			r.Method+" addresses a collection, not a record: POST "+coll.path()+
				" creates one, PUT "+addr.path()+" writes this record")
		return nil, substrate.KindInfo{}, address{}, false
	}
	// A record id may not be a sub-resource word. `…/{kind}/{id}/incoming` is a
	// static route, so a published record whose
	// id is `incoming` reads through the sub-resource handler and 405s while a
	// PUT to the same path creates it — a write-only row nothing can read. The
	// reservation refuses BOTH directions, on every kind, so the word means the
	// sub-resource everywhere and the corner is symmetric (decision 0033). It
	// does not touch the sub-resource handlers themselves: those address the
	// record by its real id (the segment before the word), never by the word.
	if wantID && reservedRecordID(addr.id) {
		writeError(w, http.StatusBadRequest, codeBadRequest,
			"id "+strconv.Quote(addr.id)+" is reserved: it names a record sub-resource, so no record may take it")
		return nil, substrate.KindInfo{}, address{}, false
	}
	ctx := r.Context()
	ds := DatasetFrom(ctx)
	// The collection segment IS the kind name, so the reference the two
	// segments spell resolves the kind directly — no plural lookup.
	ti, err := ds.KindByRef(ctx, addr.ref())
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) {
			writeError(w, http.StatusNotFound, codeNotFound, "unknown collection "+addr.ref())
			return nil, substrate.KindInfo{}, address{}, false
		}
		writeSubstrateError(w, err)
		return nil, substrate.KindInfo{}, address{}, false
	}
	// There is no scope gate here any more: a token has FULL ACCESS to its
	// repository, so authentication is the whole authorization
	// story on this path. What a token cannot do is written into the kinds
	// themselves — the auth kinds refuse generic writes at the engine's one
	// chokepoint, not with a per-request capability check.
	return ds, ti, addr, true
}

// pathParam reads a chi URL parameter, percent-decoded. chi routes on the RAW
// path so that an id carrying a `/` — a kind reference IS an id, on every
// declaration record — stays one path segment when it is written `%2F`; the
// decoding has to happen here, once, for every handler.
func pathParam(r *http.Request, name string) string {
	raw := chi.URLParam(r, name)
	if dec, err := url.PathUnescape(raw); err == nil {
		return dec
	}
	return raw
}

// putStatus is 201 for a create, 200 for an update or replace. A
// fresh row lands at version 1 and every later write bumps it past 1, so
// POST-to-collection and PUT-at-id report which they did CONSISTENTLY — no
// more blanket 201 on an upsert that only updated (codex api finding 5).
func putStatus(e *substrate.Record) int {
	if e != nil && e.Version == 1 {
		return http.StatusCreated
	}
	return http.StatusOK
}

func (h *handler) listCollection(w http.ResponseWriter, r *http.Request) {
	ds, ti, _, ok := h.collection(w, r, false)
	if !ok {
		return
	}

	// A collection watch is the changelog tail scoped to this type. The list
	// query grammar does not apply to it — filter/orderBy/first/after/
	// withAnnotations are silently meaningless on a watch — so their presence is
	// a bad_request naming the param, never silent success. `from`
	// (the transparent resume seq) IS honored.
	if r.URL.Query().Get("watch") == "1" {
		if bad := rejectParams(r, "filter", "orderBy", "first", "after", "withAnnotations"); bad != "" {
			writeError(w, http.StatusBadRequest, codeBadRequest, bad+" is not supported with watch=1")
			return
		}
		if bad := unsupportedParam(r, watchParams...); bad != "" {
			writeError(w, http.StatusBadRequest, codeBadRequest, bad)
			return
		}
		h.streamChanges(w, r, ds, substrate.ChangeFilter{Kinds: []string{ti.Identity}}, false)
		return
	}
	if bad := unsupportedParam(r, listParams...); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad)
		return
	}

	q, err := parseQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	// The path names the kind, so the collection FORCES filter.kinds (ruling
	// A8). An explicit, conflicting filter.kinds is not silently overwritten —
	// it is a bad_request; the caller either drops it or lists a different
	// collection.
	if len(q.Filter.Kinds) > 0 {
		writeError(w, http.StatusBadRequest, codeBadRequest,
			"filter.kinds is not supported on a collection list — the path already names the kind")
		return
	}
	q.Filter.Kinds = []string{ti.Identity}

	page, err := ds.List(r.Context(), q)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *handler) createInCollection(w http.ResponseWriter, r *http.Request) {
	ds, ti, _, ok := h.collection(w, r, false)
	if !ok {
		return
	}
	var in substrate.PutInput
	if err := decodeRecordBody(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	in.Kind = ti.Identity
	ctx := r.Context()
	ent, err := ds.Put(ctx, ActorFrom(ctx), in)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, putStatus(ent), ent)
}

func (h *handler) getResource(w http.ResponseWriter, r *http.Request) {
	ds, ti, addr, ok := h.collection(w, r, true)
	if !ok {
		return
	}
	// The path carries the whole record reference — the collection names the
	// kind, {id} the id — so the read is kind-scoped by construction.
	ent, err := ds.Get(r.Context(), ti.Identity, addr.id)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ent)
}

func (h *handler) putResource(w http.ResponseWriter, r *http.Request) {
	ds, ti, addr, ok := h.collection(w, r, true)
	if !ok {
		return
	}
	var in substrate.PutInput
	if err := decodeRecordBody(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	in.Kind = ti.Identity
	in.ID = addr.id
	ctx := r.Context()
	ent, err := ds.Put(ctx, ActorFrom(ctx), in)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, putStatus(ent), ent)
}

func (h *handler) patchResource(w http.ResponseWriter, r *http.Request) {
	ds, ti, addr, ok := h.collection(w, r, true)
	if !ok {
		return
	}
	// A bundle's runtime lifecycle IS record state the substrate owns
	// (decision 0019): disable, enable, uninstall and purge are transitions of
	// the `disabled`/`uninstalled`/`purging` managed properties, not verbs. A
	// PATCH that carries one of them runs the lifecycle op behind the engine's
	// exclusive fence, which a generic property write could not.
	if ti.Identity == kindBundleIdentity {
		h.patchBundleLifecycle(w, r, addr.id)
		return
	}
	var in substrate.PatchInput
	if err := decodeRecordBody(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	ent, err := ds.Patch(ctx, ActorFrom(ctx), ti.Identity, addr.id, in)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ent)
}

func (h *handler) deleteResource(w http.ResponseWriter, r *http.Request) {
	ds, ti, addr, ok := h.collection(w, r, true)
	if !ok {
		return
	}
	ctx := r.Context()
	ent, err := ds.Delete(ctx, ActorFrom(ctx), ti.Identity, addr.id)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ent)
}

// The query parameters each read mode honors. A parameter outside its mode's
// set is a bad_request naming the key — never silence, because a
// silently ignored parameter returns UNFILTERED rows that look filtered.
var (
	// listParams is the list query grammar: the filter document, the order, the
	// keyset page, the heavy-data opt-in, and the mode switch itself.
	listParams = []string{"filter", "orderBy", "first", "after", "withAnnotations", "watch"}
	// incomingParams is the reverse read's grammar: the keyset page, and the
	// two narrowings a drill-down expands one group with.
	incomingParams = []string{"first", "after", "property", "fromKind"}
	// watchParams is a collection watch: the mode switch and the resume cursor.
	// The list grammar does not apply, and rejectParams names those keys with a
	// message of their own before this set is consulted.
	watchParams = []string{"watch", "from"}
	// changeParams is the cross-collection changefeed: the two modes' cursors
	// plus the change filter, whose list-valued keys are all PLURAL.
	changeParams = []string{
		"watch", "from", "before", "first",
		"recordId", "recordKind", "q",
		"kinds", "excludeKinds", "actors", "excludeActors", "ops", "excludeOps",
	}
)

// unsupportedParam names the first query parameter (sorted, so one request
// gives one deterministic message) outside `allowed`, or "" when every
// parameter is honored. A near miss — the singular `kind=`/`op=`/`actor=` of
// the changes feed, or a casing slip — is told the spelling that works, since
// that guess is exactly the one that used to return the whole unfiltered feed.
func unsupportedParam(r *http.Request, allowed ...string) string {
	ok := make(map[string]bool, len(allowed))
	for _, n := range allowed {
		ok[n] = true
	}
	var unknown []string
	for name := range r.URL.Query() {
		if !ok[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return ""
	}
	sort.Strings(unknown)
	msg := "unknown query parameter " + strconv.Quote(unknown[0])
	if alt := nearestParam(unknown[0], allowed); alt != "" {
		msg += " — did you mean " + strconv.Quote(alt) + "?"
	}
	return msg
}

// nearestParam matches a supported parameter that differs only by casing or by
// the plural `s` the filter keys carry.
func nearestParam(name string, allowed []string) string {
	for _, a := range allowed {
		if strings.EqualFold(a, name) || strings.EqualFold(a, name+"s") || strings.EqualFold(name, a+"s") {
			return a
		}
	}
	return ""
}

// rejectParams reports the first named query parameter that is present, "" if
// none are. It is how a mode names a parameter it does not honor instead of
// dropping it silently: the caller turns a non-empty return into a
// bad_request.
func rejectParams(r *http.Request, names ...string) string {
	q := r.URL.Query()
	for _, n := range names {
		if q.Has(n) {
			return n
		}
	}
	return ""
}

// parseQuery reads the list parameters: filter (URL-encoded JSON), orderBy
// ("at:desc,createdAt" or JSON), first/after, and the heavy-data opt-in.
func parseQuery(r *http.Request) (substrate.Query, error) {
	v := r.URL.Query()
	var q substrate.Query
	if raw := v.Get("filter"); raw != "" {
		// The filter document is decoded STRICTLY: a misspelled
		// filter key must never broaden the query by silently dropping a
		// narrowing predicate, so an unknown key is a bad_request naming it.
		if err := decodeJSONStrict(strings.NewReader(raw), &q.Filter); err != nil {
			return q, errors.New("filter: " + err.Error())
		}
	}
	if raw := v.Get("orderBy"); raw != "" {
		orders, err := parseOrderBy(raw)
		if err != nil {
			return q, err
		}
		q.OrderBy = orders
	}
	if raw := v.Get("first"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return q, errors.New("first: not a number")
		}
		q.First = n
	}
	q.After = v.Get("after")
	q.WithAnnotations = v.Get("withAnnotations") == "1"
	return q, nil
}

func parseOrderBy(raw string) ([]substrate.Order, error) {
	if strings.HasPrefix(strings.TrimSpace(raw), "[") {
		var orders []substrate.Order
		if err := decodeJSONStrict(strings.NewReader(raw), &orders); err != nil {
			return nil, errors.New("orderBy: " + err.Error())
		}
		return orders, nil
	}
	var orders []substrate.Order
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, dir, _ := strings.Cut(part, ":")
		o := substrate.Order{Property: strings.TrimSpace(name)}
		switch strings.ToLower(strings.TrimSpace(dir)) {
		case "", "asc":
		case "desc":
			o.Desc = true
		default:
			return nil, errors.New("orderBy: direction must be asc or desc")
		}
		orders = append(orders, o)
	}
	return orders, nil
}
