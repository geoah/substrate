package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/geoah/substrate/internal/substrate"
)

// automationOps is the engine's trigger-delivery seam, asserted at runtime
// like the TOTP rotator: substrate.Dataset stays frozen, a dataset without
// triggers simply has no verbs. Status is computed, a replay is a cursor
// reset, a run is one synthesized delivery, a wake is an immediate scan, and
// CallFunction is the callable invocation API (`mode: call`).
type automationOps interface {
	TriggerStatuses(ctx context.Context) ([]substrate.TriggerStatus, error)
	ReplayTrigger(ctx context.Context, id string, from int64) error
	RunTrigger(ctx context.Context, id, recordKind, recordID string) (int, error)
	WakeTrigger(ctx context.Context, id string) (int, error)
	TriggerFailures(ctx context.Context, id string) ([]substrate.TriggerFailure, error)
	RetryTriggerFailure(ctx context.Context, id string, failureID int64) (int, error)
	CallFunction(ctx context.Context, name string, args any) (any, int, error)
}

func automationFrom(ctx context.Context) (automationOps, bool) {
	ops, ok := DatasetFrom(ctx).(automationOps)
	return ops, ok
}

// mountTriggerVerbs registers the trigger delivery verbs under one authority.
// It is mounted at core.substrate.reamde.dev, where the trigger records live (ruling A8:
// a resource's operational verbs sit at the resource).
func (h *handler) mountTriggerVerbs(r chi.Router, authority string) {
	r.Get("/"+authority+"/triggers/status", h.getTriggerStatus)
	r.Post("/"+authority+"/triggers/{id}/replay", h.postTriggerReplay)
	r.Post("/"+authority+"/triggers/{id}/run", h.postTriggerRun)
	r.Post("/"+authority+"/triggers/{id}/wake", h.postTriggerWake)
	r.Get("/"+authority+"/triggers/{id}/parked", h.getTriggerParked)
	r.Post("/"+authority+"/triggers/{id}/parked/{fid}/retry", h.postTriggerRetry)
}

func writeNoAutomation(w http.ResponseWriter) {
	writeUnsupported(w, "this substrate runs no triggers")
}

// getTriggerStatus is per-trigger visibility: kind, cursor, head, lag, last
// fire and parked count, all computed — nothing is stored on the trigger
// record.
func (h *handler) getTriggerStatus(w http.ResponseWriter, r *http.Request) {
	ops, ok := automationFrom(r.Context())
	if !ok {
		writeNoAutomation(w)
		return
	}
	statuses, err := ops.TriggerStatuses(r.Context())
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"triggers": statuses})
}

type replayRequest struct {
	From int64 `json:"from"`
}

// postTriggerReplay resets an record-sourced trigger's cursor; the
// dispatcher does the rest (retrospective runs are cursor resets).
func (h *handler) postTriggerReplay(w http.ResponseWriter, r *http.Request) {
	ops, ok := automationFrom(r.Context())
	if !ok {
		writeNoAutomation(w)
		return
	}
	var req replayRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if err := ops.ReplayTrigger(r.Context(), pathParam(r, "id"), req.From); err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": req.From})
}

type runRequest struct {
	// Kind + ID are the delivered record's full reference.
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// postTriggerRun synthesizes one delivery of a record's current state
// through the trigger's callable, without moving the cursor.
func (h *handler) postTriggerRun(w http.ResponseWriter, r *http.Request) {
	ops, ok := automationFrom(r.Context())
	if !ok {
		writeNoAutomation(w)
		return
	}
	var req runRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if req.Kind == "" || req.ID == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "a kind and an id are required — records are addressed by (kind, id)")
		return
	}
	ran, err := ops.RunTrigger(r.Context(), pathParam(r, "id"), req.Kind, req.ID)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ran": ran})
}

// postTriggerWake runs a trigger's scan NOW: a webhook trigger delivers one
// fire, an record trigger drains its backlog, a schedule trigger checks its
// due occurrence. The body is empty on purpose — a webhook payload, when it
// exists someday, arrives through a signed channel, not this wake.
func (h *handler) postTriggerWake(w http.ResponseWriter, r *http.Request) {
	ops, ok := automationFrom(r.Context())
	if !ok {
		writeNoAutomation(w)
		return
	}
	ran, err := ops.WakeTrigger(r.Context(), pathParam(r, "id"))
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ran": ran})
}

// getTriggerParked lists a trigger's parked deliveries.
func (h *handler) getTriggerParked(w http.ResponseWriter, r *http.Request) {
	ops, ok := automationFrom(r.Context())
	if !ok {
		writeNoAutomation(w)
		return
	}
	failures, err := ops.TriggerFailures(r.Context(), pathParam(r, "id"))
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"parked": failures})
}

// postTriggerRetry re-runs one parked delivery; success deletes the row.
func (h *handler) postTriggerRetry(w http.ResponseWriter, r *http.Request) {
	ops, ok := automationFrom(r.Context())
	if !ok {
		writeNoAutomation(w)
		return
	}
	fid, err := strconv.ParseInt(pathParam(r, "fid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "a parked failure id is a number")
		return
	}
	ran, err := ops.RetryTriggerFailure(r.Context(), pathParam(r, "id"), fid)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ran": ran})
}

type callRequest struct {
	Input any `json:"input"`
}

// postFunctionCall is the callable invocation API: `mode: call`, arbitrary
// input validated against the manifest's `input:` schema when one is
// declared, no cursor motion, effects applied under the function's actor.
func (h *handler) postFunctionCall(w http.ResponseWriter, r *http.Request) {
	ops, ok := automationFrom(r.Context())
	if !ok {
		writeNoAutomation(w)
		return
	}
	var req callRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	output, effects, err := ops.CallFunction(r.Context(), pathParam(r, "name"), req.Input)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": output, "effects": effects})
}
