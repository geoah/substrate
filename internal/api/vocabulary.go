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
	// Confirm is the caller's consent to a lossy conversion plan, bound to the
	// preview `POST /vocabulary/plan` answered (decision 0067). Absent, a
	// lossy batch is refused with the `lossy` code; a lossless one runs
	// either way.
	Confirm *substrate.ConversionConfirm `json:"confirm,omitempty"`
	// Origin is the shipped bundle id the batch is a hand-rehomed copy of
	// (decision record 0070), which the engine records on the landed package
	// row as the import door records its own. Absent, the apply stamps
	// nothing.
	Origin string `json:"origin,omitempty"`
}

type vocabularyApplyResponse struct {
	Records []*substrate.Record `json:"records"`
}

// vocabularyPlanRequest is the preview's body: the documents the apply would
// take and the origin it would claim, and no confirmation, because a preview
// has nothing to confirm.
type vocabularyPlanRequest struct {
	Documents []map[string]any `json:"documents"`
	Origin    string           `json:"origin,omitempty"`
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
	ds := DatasetFrom(ctx)
	var ents []*substrate.Record
	var err error
	if req.Confirm != nil || req.Origin != "" {
		// A confirmation and an origin claim both ride the decided form: the
		// plan is what says whether a conversion is lossy and whether the
		// claimed copy was edited.
		ents, err = ds.ApplyVocabularyDocumentsWith(ctx, ActorFrom(ctx), req.Documents, substrate.VocabularyApply{Confirm: req.Confirm, Origin: req.Origin})
	} else {
		ents, err = ds.ApplyVocabularyDocuments(ctx, ActorFrom(ctx), req.Documents)
	}
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, vocabularyApplyResponse{Records: ents})
}

// planVocabulary is POST /api/v1/vocabulary/plan: what applying the batch
// would refuse and what it would rewrite, with the plan hash and changelog
// head a confirmation names (decision 0067). It writes nothing. A POST because
// the documents are the input, and beside `/vocabulary/apply` because it is
// that verb's preview.
func (h *handler) planVocabulary(w http.ResponseWriter, r *http.Request) {
	var req vocabularyPlanRequest
	if err := decodeJSONStrict(http.MaxBytesReader(nil, r.Body, maxVocabularyApplyBody), &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	ds := DatasetFrom(ctx)
	plan, err := ds.PlanVocabularyApplyWith(ctx, ActorFrom(ctx), req.Documents, substrate.VocabularyApply{Origin: req.Origin})
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
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
	ds := DatasetFrom(ctx)
	items, err := ds.PlanShippedUpgrade(ctx)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.Listed(items))
}
