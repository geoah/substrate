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

// applyVocabulary is POST /api/v1/vocabulary/apply: the one verb that applies
// schema documents, the same envelope everything wears. It sits at the version
// root, not under an authority, because a batch of declarations is not record
// data (decision 0033).
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

// getVocabularyUpgrade is GET /api/v1/vocabulary/upgrade: what the running
// binary's boot upgrade would do to each package it ships and seeds (the core
// package), computed against this repository's stored declarations, with the
// guard lines the boot refused on. The boot upgrade skips rather than fails
// when a guard refuses it, so without this read a withheld core upgrade is a
// server log line and nothing a repository token can see. It sits at the
// version root beside `/vocabulary/apply`, because it is about the shipped
// declarations and names no kind (decision 0033).
func (h *handler) getVocabularyUpgrade(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	planner, ok := DatasetFrom(ctx).(substrate.ShippedUpgradePlanner)
	if !ok {
		writeUnsupported(w, "this service does not preview the shipped upgrade")
		return
	}
	items, err := planner.PlanShippedUpgrade(ctx)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.Listed(items))
}
