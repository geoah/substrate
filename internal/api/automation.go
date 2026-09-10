package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/geoah/substrate/internal/substrate"
)

// mountTriggerVerbs registers the trigger delivery verbs under one authority.
// It is mounted at substrate.reamde.dev/core, where the trigger records live:
// a resource's operational verbs sit at the resource.
func (h *handler) mountTriggerVerbs(r chi.Router, authority string) {
	r.Get("/"+authority+"/trigger/status", h.getTriggerStatus)
	r.Post("/"+authority+"/trigger/{id}/replay", h.postTriggerReplay)
	r.Post("/"+authority+"/trigger/{id}/run", h.postTriggerRun)
	r.Post("/"+authority+"/trigger/{id}/wake", h.postTriggerWake)
	r.Get("/"+authority+"/trigger/{id}/parked", h.getTriggerParked)
	r.Post("/"+authority+"/trigger/{id}/parked/{fid}/retry", h.postTriggerRetry)
}

// getTriggerStatus is per-trigger visibility: kind, cursor, head, lag, last
// fire and parked count, all computed — nothing is stored on the trigger
// record.
func (h *handler) getTriggerStatus(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	statuses, err := ds.TriggerStatuses(r.Context())
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.Listed(statuses))
}

type replayRequest struct {
	From int64 `json:"from"`
}

// postTriggerReplay resets an record-sourced trigger's cursor; the
// dispatcher does the rest (retrospective runs are cursor resets).
func (h *handler) postTriggerReplay(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	var req replayRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if err := ds.ReplayTrigger(r.Context(), pathParam(r, "id"), req.From); err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.TriggerReplayed{From: req.From})
}

type runRequest struct {
	// Kind + ID are the delivered record's full reference.
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// postTriggerRun synthesizes one delivery of a record's current state
// through the trigger's callable, without moving the cursor.
func (h *handler) postTriggerRun(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	var req runRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if req.Kind == "" || req.ID == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "a kind and an id are required — records are addressed by (kind, id)")
		return
	}
	ran, err := ds.RunTrigger(r.Context(), pathParam(r, "id"), req.Kind, req.ID)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.TriggerRan{Ran: ran})
}

// postTriggerWake runs a trigger's scan NOW: a webhook trigger delivers one
// fire, an record trigger drains its backlog, a schedule trigger checks its
// due occurrence. The body is empty on purpose: a webhook PAYLOAD arrives
// through the public door, POST /webhooks/{authority}/{trigger} (webhooks.go),
// and a wake is the owner asking for a bare fire.
func (h *handler) postTriggerWake(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	ran, err := ds.WakeTrigger(r.Context(), pathParam(r, "id"))
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.TriggerRan{Ran: ran})
}

// getTriggerParked lists a trigger's parked deliveries.
func (h *handler) getTriggerParked(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	failures, err := ds.TriggerFailures(r.Context(), pathParam(r, "id"))
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.Listed(failures))
}

// postTriggerRetry re-runs one parked delivery; success deletes the row.
func (h *handler) postTriggerRetry(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	fid, err := strconv.ParseInt(pathParam(r, "fid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "a parked failure id is a number")
		return
	}
	ran, err := ds.RetryTriggerFailure(r.Context(), pathParam(r, "id"), fid)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.TriggerRan{Ran: ran})
}

type callRequest struct {
	Input any `json:"input"`
}

// postFunctionCall is the callable invocation API: `mode: call`, arbitrary
// input validated against the manifest's `input:` schema when one is
// declared, no cursor motion, effects applied under the function's actor.
func (h *handler) postFunctionCall(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	var req callRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	output, effects, err := ds.CallFunction(idempotentContext(r), pathParam(r, "name"), req.Input)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.FunctionCalled{Output: output, Effects: effects})
}
