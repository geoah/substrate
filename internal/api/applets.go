package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/geoah/substrate/internal/applet"
	"github.com/geoah/substrate/internal/substrate"
)

// The applet runtime (docs/plans/applets.md): render a record of
// substrate.reamde.dev/applets/applet into A2UI messages, and run one of its
// actions. Both take the request's dataset, so an applet reads and writes what
// the caller's token may and nothing else. Five segments, the applet kind's
// three, the id and the verb, so the record path never shadows them.
const (
	appletRenderRoute  = "/" + applet.Kind + "/{id}/render"
	appletActRoute     = "/" + applet.Kind + "/{id}/act"
	appletPreviewRoute = "/" + applet.Kind + "/{id}/preview"
)

func (h *handler) postAppletRender(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	var in substrate.AppletRenderInput
	if !readJSON(w, r, &in, jsonBody) {
		return
	}
	rec, ok := appletRecord(w, r, ds)
	if !ok {
		return
	}
	rt := applet.Runtime{Data: ds}
	frame, err := rt.Render(r.Context(), rec, in)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, frame)
}

func (h *handler) postAppletAct(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	var in substrate.AppletActInput
	if !readJSON(w, r, &in, jsonBody) {
		return
	}
	if in.Action.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, codeValidation, "action.name is required: the name of the action the button's event names")
		return
	}
	rec, ok := appletRecord(w, r, ds)
	if !ok {
		return
	}
	// No Idempotency-Key is taken: an action is not replayed (#1265).
	ctx := r.Context()
	rt := applet.Runtime{Data: ds}
	frame, err := rt.Act(ctx, ActorFrom(ctx), rec, in)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, frame)
}

func appletRecord(w http.ResponseWriter, r *http.Request, ds substrate.Dataset) (*substrate.Record, bool) {
	id := pathParam(r, "id")
	rec, err := ds.Get(r.Context(), applet.Kind, id)
	if errors.Is(err, substrate.ErrNotFound) {
		writeError(w, http.StatusNotFound, codeNotFound, "no applet "+id)
		return nil, false
	}
	if err != nil {
		writeSubstrateError(w, r, err)
		return nil, false
	}
	return rec, true
}

// postAppletPreview answers an author's view of one screen: the problems a
// write of the applet is refused with and the screen's outline. The applet is
// the stored one, or the draft the body carries under the id it would be
// written at. It reads under the caller's token and writes nothing.
func (h *handler) postAppletPreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ds := DatasetFrom(ctx)
	var in substrate.AppletPreviewInput
	if !readJSON(w, r, &in, jsonBody) {
		return
	}
	rec := &substrate.Record{ID: pathParam(r, "id"), Kind: applet.Kind, Properties: in.Applet}
	if in.Applet == nil {
		stored, ok := appletRecord(w, r, ds)
		if !ok {
			return
		}
		rec = stored
	}
	rt := applet.Runtime{Data: readsOnly{ds}}
	p, err := rt.Preview(ctx, rec, in, applet.DatasetSchema(ctx, ds), nil)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// readsOnly is a dataset a preview renders against: its reads, and no write
// or call, whatever an applet's init asks for.
type readsOnly struct{ substrate.Dataset }

var errPreviewWrites = fmt.Errorf("%w: a preview reads and never writes or calls", substrate.ErrForbidden)

func (readsOnly) Put(context.Context, substrate.Actor, substrate.PutInput) (*substrate.Record, error) {
	return nil, errPreviewWrites
}

func (readsOnly) Patch(context.Context, substrate.Actor, string, string, substrate.PatchInput) (*substrate.Record, error) {
	return nil, errPreviewWrites
}

func (readsOnly) Delete(context.Context, substrate.Actor, string, string, substrate.DeleteInput) (*substrate.Record, error) {
	return nil, errPreviewWrites
}

func (readsOnly) CallFunction(context.Context, substrate.Actor, string, any) (any, int, error) {
	return nil, 0, errPreviewWrites
}
