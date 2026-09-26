package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
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
	// HoldWaitingMappings asks the door to hold back each mapping whose
	// source kind neither the repository nor the batch declares, and commit
	// the rest, the way the catalog's install and import doors drop a
	// suggested mapping whose provider is absent (decision record 0106).
	// Absent, such a mapping refuses the whole batch.
	HoldWaitingMappings bool `json:"holdWaitingMappings,omitempty"`
}

type vocabularyApplyResponse struct {
	Records []*substrate.Record `json:"records"`
	// HeldMappings names each mapping the batch held back, the package it
	// waits on, and the state `waiting`. Absent when nothing was held.
	HeldMappings []substrate.SuggestedMapping `json:"heldMappings,omitempty"`
}

// vocabularyPlanRequest is the preview's body: the documents the apply would
// take and the origin it would claim, and no confirmation, because a preview
// has nothing to confirm.
type vocabularyPlanRequest struct {
	Documents []map[string]any `json:"documents"`
	Origin    string           `json:"origin,omitempty"`
	// HoldWaitingMappings previews the batch the apply would commit under the
	// same flag: the waiting mappings are left out of it.
	HoldWaitingMappings bool `json:"holdWaitingMappings,omitempty"`
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
	docs, held, err := holdWaitingMappings(ctx, ds, req.Documents, req.HoldWaitingMappings)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	if len(docs) == 0 && len(held) > 0 {
		// Every document was a held mapping: nothing to commit, and the
		// report says why.
		writeJSON(w, http.StatusOK, vocabularyApplyResponse{Records: []*substrate.Record{}, HeldMappings: held})
		return
	}
	var ents []*substrate.Record
	if req.Confirm != nil || req.Origin != "" {
		// A confirmation and an origin claim both ride the decided form: the
		// plan is what says whether a conversion is lossy and whether the
		// claimed copy was edited.
		ents, err = ds.ApplyVocabularyDocumentsWith(ctx, ActorFrom(ctx), docs, substrate.VocabularyApply{Confirm: req.Confirm, Origin: req.Origin})
	} else {
		ents, err = ds.ApplyVocabularyDocuments(ctx, ActorFrom(ctx), docs)
	}
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, vocabularyApplyResponse{Records: ents, HeldMappings: held})
}

// holdWaitingMappings takes the waiting mappings out of a batch when the
// caller asked for it: each suggested mapping (vocabulary.SuggestedMappings)
// whose source kind the batch does not declare and the repository does not
// hold, with its `installs:` entry, exactly as the catalog doors prune one. It
// answers the documents to apply and one `waiting` entry per mapping held.
// Only a source that is ABSENT is held: one that is present and shaped wrong
// still refuses the batch, naming the problem.
func holdWaitingMappings(ctx context.Context, ds substrate.Dataset, docs []map[string]any, hold bool) ([]map[string]any, []substrate.SuggestedMapping, error) {
	if !hold {
		return docs, nil, nil
	}
	waiting, err := vocabulary.WaitingMappings(docs, func(kind string) (bool, error) {
		_, err := ds.KindByRef(ctx, kind)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, substrate.ErrNotFound):
			return false, nil
		default:
			return false, err
		}
	})
	if err != nil || len(waiting) == 0 {
		return docs, nil, err
	}
	drop := make(map[string]bool, len(waiting))
	held := make([]substrate.SuggestedMapping, 0, len(waiting))
	for _, sm := range waiting {
		drop[sm.ID] = true
		held = append(held, substrate.SuggestedMapping{
			ID: sm.ID, From: sm.From, To: sm.To, Package: sm.Package,
			State: substrate.SuggestedMappingWaiting,
		})
	}
	return vocabulary.WithoutMappings(docs, drop), held, nil
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
	docs, _, err := holdWaitingMappings(ctx, ds, req.Documents, req.HoldWaitingMappings)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	plan, err := ds.PlanVocabularyApplyWith(ctx, ActorFrom(ctx), docs, substrate.VocabularyApply{Origin: req.Origin})
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
