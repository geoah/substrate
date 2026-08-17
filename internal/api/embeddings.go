package api

import "net/http"

// The re-embed verb. A repository's vectors are bought from the llmprovider
// row that declares `embedModel`, and each stored vector names the row and the
// model that produced it; change either and the vectors already stored came
// from a different model, which is not a distance away from the new ones. This
// verb enqueues them for replacement. It buys nothing itself: the drain loop
// does, so a re-embed of a large repository is spread over passes and resumes
// after a restart.
//
// It addresses the repository rather than one row, which is why it is not
// mounted under the llmprovider resource: the row is where the model is
// chosen, but every embeddable property is what gets requeued. The operator's
// hat runs the same verb over the DSN
// (`substratectl --dsn … repository reembed <username>`).

type reembedRequest struct {
	// All ignores the stored provenance and enqueues every embeddable
	// property. It is the answer to a gateway swapped behind an unchanged
	// provider row and model name, which nothing stored can detect.
	All bool `json:"all"`
}

func (h *handler) postReembed(w http.ResponseWriter, r *http.Request) {
	var req reembedRequest
	// An empty body means the default scan, so a decode failure is only ever
	// a malformed one.
	if r.ContentLength > 0 {
		if err := decodeBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
			return
		}
	}
	ctx := r.Context()
	report, err := DatasetFrom(ctx).Reembed(ctx, req.All)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
