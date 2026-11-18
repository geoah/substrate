package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoah/substrate/internal/push"
	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The push routes (docs/plans/push.md, section 4): what an install asks its
// server before it registers with its publisher's relay, the badge a push
// sets, and what an event id a push carried names. Each answers for the
// asking token alone, and each reads through the view tokenView builds, the
// one a push is decided through.
const (
	pushKeyRoute    = "/push/key"
	pushBadgeRoute  = "/push/badge"
	pushEventsRoute = "/push/events/{eventId}"
)

// pushUnreadableRetryAfter is the Retry-After on a relay whose authority
// document could not be read just now.
const pushUnreadableRetryAfter = time.Minute

// TokenView is the view an API request with a token gets over a
// repository's own dataset: the person's roles as they are now and, for an
// app's token, its grant, with `agents:` covering an agent's reads
// transitively. Push decides who may see an event, and counts the badge,
// through it, so a push says nothing its device's token could not read.
type TokenView struct {
	ctx context.Context
	ds  substrate.Dataset
}

// NewTokenView builds the view a request with tok gets over whole.
func NewTokenView(ctx context.Context, whole substrate.Dataset, tok substrate.TokenInfo) (*TokenView, error) {
	ctx, _, err := tokenView(ctx, whole, tok)
	if err != nil {
		return nil, err
	}
	return &TokenView{ctx: ctx, ds: DatasetFrom(ctx)}, nil
}

// Context is the request context the view's reads run on: its principal,
// its person, its scopes.
func (v *TokenView) Context() context.Context { return v.ctx }

// Dataset is the view.
func (v *TokenView) Dataset() substrate.Dataset { return v.ds }

// Reads reports whether the view reads rec, which carries at least its
// kind, its id and the properties a row rule reads (a thread's `agent`, a
// message's `thread`).
func (v *TokenView) Reads(_ context.Context, rec *substrate.Record) (bool, error) {
	return viewReads(v.ctx, v.ds, rec)
}

// viewReads walks a view's layers: each scoped one must read rec.
func viewReads(ctx context.Context, ds substrate.Dataset, rec *substrate.Record) (bool, error) {
	switch d := ds.(type) {
	case *scopedDataset:
		ok, err := d.visible(ctx, &substrate.Record{Kind: d.local(rec.Kind), ID: rec.ID, Properties: rec.Properties})
		if err != nil || !ok {
			return false, err
		}
		return viewReads(ctx, d.inner, rec)
	case *assistantDataset:
		return viewReads(ctx, d.inner, rec)
	}
	return true, nil
}

// viewDecides reports whether a view could write the decision of rec, a
// record that waits on a person. A person's layer must write the kind whole
// by their roles: the agent verbs' row path is an app's, and a viewer or a
// person whose role was taken away decides nothing. An app's layer writes
// it whole, or reaches the row through an agent verb.
func viewDecides(ctx context.Context, ds substrate.Dataset, rec *substrate.Record) (bool, error) {
	switch d := ds.(type) {
	case *scopedDataset:
		kind := d.local(rec.Kind)
		whole, err := d.writes(ctx, kind)
		if err != nil {
			return false, err
		}
		if !whole {
			if d.person || !d.rowKind(kind) {
				return false, nil
			}
			ok, err := d.rowVisible(ctx, &substrate.Record{Kind: kind, ID: rec.ID, Properties: rec.Properties})
			if err != nil || !ok {
				return false, err
			}
		}
		return viewDecides(ctx, d.inner, rec)
	case *assistantDataset:
		return viewDecides(ctx, d.inner, rec)
	}
	return true, nil
}

// pushStoreOf is the engine beneath a view, which keeps the devices and the
// keys.
func pushStoreOf(ds substrate.Dataset) (substrate.PushStore, bool) {
	for {
		switch d := ds.(type) {
		case *scopedDataset:
			ds = d.inner
		case *assistantDataset:
			ds = d.inner
		default:
			store, ok := ds.(substrate.PushStore)
			return store, ok
		}
	}
}

// pushRequest is what each push route starts from: push on, the request's
// token and the engine's push store. It writes the refusal itself and
// answers false.
func (h *handler) pushRequest(w http.ResponseWriter, r *http.Request) (substrate.TokenInfo, substrate.PushStore, bool) {
	if h.pushOff {
		writeJSON(w, http.StatusForbidden, substrate.ErrorEnvelope{Error: substrate.ErrorPayload{
			Code: codeForbidden, Reason: substrate.ReasonPushOff, Message: "this server sends no pushes: its operator turned them off",
		}})
		return substrate.TokenInfo{}, nil, false
	}
	if bad := unsupportedParam(r); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad)
		return substrate.TokenInfo{}, nil, false
	}
	tok, ok := tokenFrom(r.Context())
	store, held := pushStoreOf(DatasetFrom(r.Context()))
	if !ok || !held {
		writeError(w, http.StatusForbidden, codeForbidden, "this token takes no pushes")
		return substrate.TokenInfo{}, nil, false
	}
	return tok, store, true
}

// getPushKey serves GET /api/v1/push/key: the asking token's public key,
// and the relay the server will send its pushes to, read from its verified
// app's publisher's document. The app compares the relay with its build and
// the document and never sends to it.
func (h *handler) getPushKey(w http.ResponseWriter, r *http.Request) {
	tok, store, ok := h.pushRequest(w, r)
	if !ok {
		return
	}
	relay, err := push.Relay(r.Context(), h.pushDocs, tok)
	switch {
	case errors.Is(err, push.ErrNoRelay):
		writeJSON(w, http.StatusForbidden, substrate.ErrorEnvelope{Error: substrate.ErrorPayload{
			Code: codeForbidden, Reason: substrate.ReasonNoRelay,
			Message: "this token's app names no push relay: it is no app's, its publisher is not verified, its access went to a local program, or its publisher's document names none for it",
		}})
		return
	case err != nil:
		writeUnavailable(w, pushUnreadableRetryAfter, "the app's publisher's document could not be read just now")
		return
	}
	signer, err := store.PushSigner(r.Context(), tok.ID)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.PushKey{PublicKey: pushwire.Encode(signer.PublicKey()), Relay: relay})
}

// getPushBadge serves GET /api/v1/push/badge: the asking token's unread
// count, the number a push to it sets.
func (h *handler) getPushBadge(w http.ResponseWriter, r *http.Request) {
	_, store, ok := h.pushRequest(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	person, err := store.PushPerson(ctx)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	view := DatasetFrom(ctx)
	badge, err := push.Badge(ctx, store, func(ctx context.Context, rec *substrate.Record) (bool, error) {
		return viewReads(ctx, view, rec)
	}, person)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.PushBadge{Badge: badge})
}

// getPushEvent serves GET /api/v1/push/events/{eventId}: what an event id
// names, for the token it was sealed for alone, through its view now. An id
// that is another token's, past its day, that does not open, or that names
// what the view no longer reads is not found. A request says whether the
// token may decide it now.
func (h *handler) getPushEvent(w http.ResponseWriter, r *http.Request) {
	tok, store, ok := h.pushRequest(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	notFound := func() { writeError(w, http.StatusNotFound, codeNotFound, "no such event") }
	signer, err := store.PushSigner(ctx, tok.ID)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	e, err := signer.OpenEvent(chi.URLParam(r, "eventId"))
	if err != nil {
		notFound()
		return
	}
	view := DatasetFrom(ctx)
	get := func(kind, id string) (*substrate.Record, bool) {
		rec, err := view.Get(ctx, kind, id)
		if err != nil {
			if !errors.Is(err, substrate.ErrNotFound) && !errors.Is(err, substrate.ErrForbidden) {
				writeSubstrateError(w, r, err)
			} else {
				notFound()
			}
			return nil, false
		}
		return rec, true
	}
	thread, ok := get(vocabulary.KindLLMThread, e.Thread)
	if !ok {
		return
	}
	out := substrate.PushEventInfo{Event: e.Event, Thread: e.Thread}
	switch e.Event {
	case substrate.PushEventReply:
		if _, ok := get(e.Item.Kind, e.Item.ID); !ok {
			return
		}
		item := e.Item
		out.Item = &item
	case substrate.PushEventFailed:
		// Later writes move the thread's version: the failure stands while
		// the thread is still settled in it.
		if status, _ := thread.Properties["status"].(string); status != "error" && status != "overbudget" {
			notFound()
			return
		}
		item := e.Item
		out.Item = &item
	case substrate.PushEventRequest:
		rec, ok := get(e.Item.Kind, e.Item.ID)
		if !ok {
			return
		}
		item := e.Item
		out.Item = &item
		// Pending at the sealed version (a newer version is a new item),
		// written whole by the token's view, and continued by this hand as
		// the decision's own write would be.
		actions, err := store.PushPending(ctx, e.Item)
		if err == nil && actions {
			actions, err = viewDecides(ctx, view, rec)
		}
		if err == nil && actions {
			actions, err = store.PushMayDecide(ctx, ActorFrom(ctx), e.Item)
		}
		if err != nil {
			writeSubstrateError(w, r, err)
			return
		}
		out.Actions = &actions
	case substrate.PushEventRead:
		out.Covers = &substrate.PushCovers{Alerts: append([]string{}, e.Alerts...)}
	default:
		notFound()
		return
	}
	writeJSON(w, http.StatusOK, out)
}
