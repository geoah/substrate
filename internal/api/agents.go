package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/geoah/substrate/internal/substrate"
)

// The agent surfaces: the call API's agent half and chat — the same loop
// with a live client attached. Chat streams ndjson
// (one AgentEvent per line, the changes feed's conventions); the thread it
// writes is ordinary data any client re-reads through the record API.

// agentsFrom resolves the request's dataset to the agent seam; a dataset
// without it has no agent verbs.
func agentsFrom(ctx context.Context) (substrate.AgentOps, bool) {
	ops, ok := DatasetFrom(ctx).(substrate.AgentOps)
	return ops, ok
}

// postAgentCall is the callable invocation API's agent half: arbitrary
// input becomes the first user message, and the response carries the final
// reply plus the thread id — the durable trace.
func (h *handler) postAgentCall(w http.ResponseWriter, r *http.Request) {
	ops, ok := agentsFrom(r.Context())
	if !ok {
		writeUnsupported(w, "this substrate runs no agents")
		return
	}
	var req callRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	res, err := ops.CallAgent(r.Context(), pathParam(r, "name"), req.Input)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type chatRequest struct {
	// Thread continues an existing thread; empty opens one.
	Thread  string `json:"thread"`
	Message string `json:"message"`
}

// postAgentChat opens or continues a thread against an agent with one user
// message and streams the run: `Content-Type: application/x-ndjson`, one
// AgentEvent object per line — thread first, deltas and tool lifecycle as
// they happen, one done event carrying the settled AgentResult. Mode chat:
// no trigger, no cursor; mid-run state is the same loop machinery, and the
// transcript persists as thread/message records either way.
func (h *handler) postAgentChat(w http.ResponseWriter, r *http.Request) {
	ops, ok := agentsFrom(r.Context())
	if !ok {
		writeUnsupported(w, "this substrate runs no agents")
		return
	}
	var req chatRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, codeInternal, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	emit := func(ev substrate.AgentEvent) {
		if err := enc.Encode(ev); err != nil {
			return
		}
		flusher.Flush()
	}
	_, err := ops.ChatAgent(r.Context(), ActorFrom(r.Context()),
		pathParam(r, "name"), req.Thread, req.Message, emit)
	if err != nil {
		// The 200 status line is already gone, so the failure travels as its
		// own error event — never a done with only text, which a client cannot
		// tell from a successful (if empty) settle.
		emit(substrate.AgentEvent{Kind: substrate.AgentEventError, Error: err.Error()})
	}
}
