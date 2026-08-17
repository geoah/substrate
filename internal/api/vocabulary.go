package api

import (
	"net/http"

	"github.com/geoah/substrate/internal/substrate"
)

// maxVocabularyApplyBody admits a batch carrying many inline function sources
// (vocabulary.SourceMaxBytes each) with envelope headroom — the loader's own
// size cap stays the real bound.
const maxVocabularyApplyBody = 32 << 20

type vocabularyApplyRequest struct {
	Documents []map[string]any `json:"documents"`
}

type vocabularyApplyResponse struct {
	Records []*substrate.Record `json:"records"`
}

// applyVocabulary is POST /{core}/vocabulary/apply: the one verb that applies schema
// documents — authority/type/metadata/data, the same envelope everything wears.
func (h *handler) applyVocabulary(w http.ResponseWriter, r *http.Request) {
	var req vocabularyApplyRequest
	// Strict at the request wrapper: a misspelled `documents` key
	// is a bad_request naming it, never an empty batch that applies nothing.
	// The documents themselves stay open maps — the loader is their admission.
	if err := decodeJSONStrict(http.MaxBytesReader(nil, r.Body, maxVocabularyApplyBody), &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	sa, ok := DatasetFrom(ctx).(substrate.VocabularyApplier)
	if !ok {
		writeUnsupported(w, "this service cannot apply schema documents")
		return
	}
	ents, err := sa.ApplyVocabularyDocuments(ctx, ActorFrom(ctx), req.Documents)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, vocabularyApplyResponse{Records: ents})
}
