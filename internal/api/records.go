package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/geoah/substrate/internal/strictjson"
	"github.com/geoah/substrate/internal/substrate"
)

// THE RECORDS ROUTE. Every "read some records" question is one route,
// `GET /api/v1/records`, in three modes told apart by their parameters:
//
//	?filter&orderBy&first&after&expand&withAnnotations   the list
//	?q&mode&filter&first                                  the ranked read
//	?watch=1&filter&from&generation                       the tail
//
// The three share `filter`, and each names which of its arms it honors and
// refuses the rest by name: a silently ignored parameter returns unfiltered
// rows that look filtered. The list carries the whole grammar; the ranked
// read pushes `filter.kinds` alone into the ranking, because both arms cap
// candidates BEFORE hydration and a predicate applied to the top-k afterwards
// would not produce the filtered top-k; the tail carries `filter.kinds`
// alone because the change filter has no property arms.
//
// `POST /api/v1/records` is the one body-addressed write: a create under a
// server-assigned id, the body naming its kind. A chosen id is a PUT at the
// record path, which fixes (kind, id) in the URL.

// recordsRoute is the route under the version prefix. The router mounts it
// and discovery advertises it from this one spelling.
const recordsRoute = "/records"

var (
	// recordsListParams is the list grammar.
	recordsListParams = []string{"filter", "orderBy", "first", "after", "expand", "withAnnotations"}
	// recordsRankedParams is the ranked read's grammar: the query, its mode,
	// the kind narrowing and the hit count.
	recordsRankedParams = []string{"q", "mode", "filter", "first"}
	// recordsWatchParams is the tail: the mode switch, the kind narrowing and
	// the resume cursor.
	recordsWatchParams = []string{"watch", "filter", "from", "generation"}
)

func (h *handler) getRecords(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ds := DatasetFrom(ctx)
	v := r.URL.Query()
	switch {
	case v.Get("watch") == "1":
		if bad := unsupportedParam(r, recordsWatchParams...); bad != "" {
			writeError(w, http.StatusBadRequest, codeBadRequest, bad+" is not supported with watch=1")
			return
		}
		f, err := parseFilter(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
			return
		}
		if arm := filterArmBeyondKinds(f); arm != "" {
			writeError(w, http.StatusBadRequest, codeBadRequest,
				"filter."+arm+" is not supported with watch=1: the tail narrows by filter.kinds alone")
			return
		}
		kinds, ok := h.resolveKinds(w, r, ds, f.Kinds)
		if !ok {
			return
		}
		h.streamChanges(w, r, ds, substrate.ChangeFilter{Kinds: kinds}, false)
	case v.Has("q"):
		if bad := unsupportedParam(r, recordsRankedParams...); bad != "" {
			writeError(w, http.StatusBadRequest, codeBadRequest, bad+" is not supported with q")
			return
		}
		f, err := parseFilter(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
			return
		}
		if arm := filterArmBeyondKinds(f); arm != "" {
			writeError(w, http.StatusBadRequest, codeBadRequest,
				"filter."+arm+" is not supported with q: a ranked read narrows by filter.kinds alone")
			return
		}
		kinds, ok := h.resolveKinds(w, r, ds, f.Kinds)
		if !ok {
			return
		}
		first, err := parseFirst(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
			return
		}
		res, err := ds.Search(ctx, substrate.SearchInput{
			Q:     v.Get("q"),
			Mode:  substrate.SearchMode(strings.ToLower(v.Get("mode"))),
			Kinds: kinds,
			K:     first,
		})
		if err != nil {
			writeSubstrateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, substrate.Ranked(res))
	default:
		if bad := unsupportedParam(r, recordsListParams...); bad != "" {
			writeError(w, http.StatusBadRequest, codeBadRequest, bad)
			return
		}
		q, err := parseQuery(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
			return
		}
		if len(q.Filter.Kinds) > 0 {
			kinds, ok := h.resolveKinds(w, r, ds, q.Filter.Kinds)
			if !ok {
				return
			}
			q.Filter.Kinds = kinds
		}
		page, err := ds.List(ctx, q)
		if errors.Is(err, substrate.ErrStaleHistory) {
			// The same signal the changefeed gives a cursor from another
			// history: 410 with the head to start over from, never a 422 a
			// client would read as its own mistake.
			head, herr := ds.Head(ctx)
			if herr != nil {
				writeSubstrateError(w, herr)
				return
			}
			writeCompacted(w, head, err.Error())
			return
		}
		if err != nil {
			writeSubstrateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	}
}

// resolveKinds turns filter.kinds into kind identities, answering 404 for a
// kind this repository never declared, exactly as the record path does.
func (h *handler) resolveKinds(w http.ResponseWriter, r *http.Request, ds substrate.Dataset, refs []string) ([]string, bool) {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		ti, err := ds.KindByRef(r.Context(), ref)
		if errors.Is(err, substrate.ErrNotFound) {
			writeError(w, http.StatusNotFound, codeNotFound, "unknown kind "+ref)
			return nil, false
		}
		if err != nil {
			writeSubstrateError(w, err)
			return nil, false
		}
		out = append(out, ti.Identity)
	}
	return out, true
}

// filterArmBeyondKinds names the first filter arm set beside `kinds`, or "".
// The ranked read and the tail admit `kinds` alone, and the arm is named so
// the refusal says what to drop.
func filterArmBeyondKinds(f substrate.Filter) string {
	switch {
	case f.Implements != "":
		return "implements"
	case len(f.IDs) > 0:
		return "ids"
	case len(f.Properties) > 0:
		return "properties"
	case len(f.Labels) > 0:
		return "labels"
	case f.Deleted != nil:
		return "deleted"
	case f.Referencing != nil:
		return "referencing"
	}
	return ""
}

// postRecords creates one record under a server-assigned id. The body is the
// put input and `kind` names where it lands. It is the one write that does
// not fix (kind, id) in the URL, so an id in the body is refused rather than
// honored: a chosen id is `PUT /api/v1/{authority}/{package}/{kind}/{id}`,
// and letting POST upsert under one would make two doors to one write with
// two different answers about what a repeat does.
func (h *handler) postRecords(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ds := DatasetFrom(ctx)
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxRequestBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	var in substrate.PutInput
	if err := strictjson.NameRetiredKeys(decodeStrictBytes(raw, &in, false)); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if in.Kind == "" {
		writeError(w, http.StatusUnprocessableEntity, codeValidation,
			"kind is required — the body names the kind the record is created in")
		return
	}
	// PRESENCE is what is refused, not a non-empty value: `"id": ""` or
	// `"id": null` is a caller that meant to name the record and did not,
	// and minting one under it would be a silent second answer.
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(raw, &keys)
	if _, named := keys["id"]; named {
		id := in.ID
		if id == "" {
			id = "{id}"
		}
		writeError(w, http.StatusUnprocessableEntity, codeValidation,
			"id is not accepted here: POST creates under a server-assigned id; PUT "+
				address{kind: in.Kind, id: id}.path()+" writes the record you named")
		return
	}
	ti, err := ds.KindByRef(ctx, in.Kind)
	if errors.Is(err, substrate.ErrNotFound) {
		writeError(w, http.StatusNotFound, codeNotFound, "unknown kind "+in.Kind)
		return
	}
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	in.Kind = ti.Identity
	ctx = idempotentContext(r)
	ent, err := ds.Put(ctx, ActorFrom(ctx), in)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, putStatus(ent), ent)
}
