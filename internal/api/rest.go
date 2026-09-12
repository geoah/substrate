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

// THE RECORD PATH (decisions 0033, 0042, 0047).
//
//	/{authority}/{package}/{kind}/{id}    a record
//
// Every kind carries an authority and a package, so a record is always four
// segments, and the path IS the record's reference value
// (vocabulary.RecordPath) character for character under the version prefix.
// There is no collection path: every list is `GET /records` (records.go), so
// a shorter path names nothing and answers the router's 404.

// address is what a record path addresses: the kind reference and the id.
type address struct {
	kind string
	id   string
}

// path is the address as a URL under the version prefix, which is also the
// record's stored reference value (vocabulary.RecordPath).
func (a address) path() string {
	return "/api/" + APIVersion + "/" + vocabulary.RecordPath(a.kind, a.id)
}

// addressed reads the record a path addresses. The four segments are the
// kind reference's three and the id.
func addressed(r *http.Request) address {
	return address{
		kind: vocabulary.KindRef(pathParam(r, "a1"), pathParam(r, "a2"), pathParam(r, "a3")),
		id:   pathParam(r, "a4"),
	}
}

// record resolves the addressed record's kind, refusing an unknown kind as a
// 404. There is no scope gate here: a token has FULL ACCESS to its
// repository, so authentication is the whole authorization story on this
// path. What a token cannot do is written into the kinds themselves: the auth
// kinds refuse generic writes at the engine's one chokepoint, not with a
// per-request capability check.
func (h *handler) record(w http.ResponseWriter, r *http.Request) (substrate.Dataset, substrate.KindInfo, address, bool) {
	addr := addressed(r)
	ctx := r.Context()
	ds := DatasetFrom(ctx)
	ti, err := ds.KindByRef(ctx, addr.kind)
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) {
			writeError(w, http.StatusNotFound, codeNotFound, "unknown kind "+addr.kind)
			return nil, substrate.KindInfo{}, address{}, false
		}
		writeSubstrateError(w, err)
		return nil, substrate.KindInfo{}, address{}, false
	}
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

// putStatus is 201 for a create, 200 for an update or replace. A fresh row
// lands at version 1 and every later write bumps it past 1, so POST and PUT
// report which they did consistently.
func putStatus(e *substrate.Record) int {
	if e != nil && e.Version == 1 {
		return http.StatusCreated
	}
	return http.StatusOK
}

func (h *handler) getResource(w http.ResponseWriter, r *http.Request) {
	ds, ti, addr, ok := h.record(w, r)
	if !ok {
		return
	}
	// The path carries the whole record reference — the kind, then the id —
	// so the read is kind-scoped by construction.
	ent, err := ds.Get(r.Context(), ti.Identity, addr.id)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ent)
}

func (h *handler) putResource(w http.ResponseWriter, r *http.Request) {
	ds, ti, addr, ok := h.record(w, r)
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
	ds, ti, addr, ok := h.record(w, r)
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

// deleteResource tombstones one record. The version precondition travels as
// the `ifVersion` query parameter: a DELETE body is dropped by enough clients
// and proxies to be no place for a guard, and the spelling is the one `put`
// and `patch` carry in their bodies. Any other parameter is refused by name.
func (h *handler) deleteResource(w http.ResponseWriter, r *http.Request) {
	ds, ti, addr, ok := h.record(w, r)
	if !ok {
		return
	}
	if bad := unsupportedParam(r, deleteParams...); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad)
		return
	}
	// Presence is what counts, not a non-empty value: `?ifVersion=` is a
	// precondition the caller meant and the server cannot read, so it is
	// refused, never treated as omitted and deleted through.
	var in substrate.DeleteInput
	if q := r.URL.Query(); q.Has("ifVersion") {
		raw := q.Get("ifVersion")
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, "ifVersion: "+strconv.Quote(raw)+" is not an integer")
			return
		}
		in.IfVersion = &n
	}
	ctx := r.Context()
	ent, err := ds.Delete(ctx, ActorFrom(ctx), ti.Identity, addr.id, in)
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
	// deleteParams is a record delete's grammar: the version precondition
	// alone.
	deleteParams = []string{"ifVersion"}
	// changeParams is the cross-collection changefeed: the two modes' cursors
	// plus the change filter, whose list-valued keys are all PLURAL.
	changeParams = []string{
		"watch", "from", "generation", "before", "first",
		"recordId", "recordKind", "q",
		"kinds", "excludeKinds", "actors", "excludeActors", "ops", "excludeOps",
	}
)

// unsupportedParam names the first query parameter (sorted, so one request
// gives one deterministic message) outside `allowed`, or "" when every
// parameter is honored. A near miss — the singular `kind=`/`op=`/`actor=` of
// the changes feed, or a casing slip — is told the spelling that works, since
// that guess would otherwise return the whole unfiltered feed as a filtered one.
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

// parseFilter reads the `filter` parameter: a URL-encoded JSON document,
// decoded STRICTLY. A misspelled filter key must never broaden the query by
// silently dropping a narrowing predicate, so an unknown key is a bad_request
// naming it.
func parseFilter(r *http.Request) (substrate.Filter, error) {
	var f substrate.Filter
	if raw := r.URL.Query().Get("filter"); raw != "" {
		if err := decodeJSONStrict(strings.NewReader(raw), &f); err != nil {
			return f, errors.New("filter: " + err.Error())
		}
	}
	return f, nil
}

// parseFirst reads the page size, 0 when absent.
func parseFirst(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("first")
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("first: not a number")
	}
	return n, nil
}

// parseQuery reads the list parameters: filter, orderBy ("at:desc,createdAt"
// or JSON), first/after, expand (comma-separated reference properties) and
// the heavy-data opt-in.
func parseQuery(r *http.Request) (substrate.Query, error) {
	v := r.URL.Query()
	var q substrate.Query
	f, err := parseFilter(r)
	if err != nil {
		return q, err
	}
	q.Filter = f
	if raw := v.Get("orderBy"); raw != "" {
		orders, err := parseOrderBy(raw)
		if err != nil {
			return q, err
		}
		q.OrderBy = orders
	}
	if q.First, err = parseFirst(r); err != nil {
		return q, err
	}
	q.After = v.Get("after")
	q.WithAnnotations = v.Get("withAnnotations") == "1"
	if raw := v.Get("expand"); raw != "" {
		for _, name := range strings.Split(raw, ",") {
			if name = strings.TrimSpace(name); name != "" {
				q.Expand = append(q.Expand, name)
			}
		}
	}
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
