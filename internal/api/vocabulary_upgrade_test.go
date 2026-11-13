package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// vocabularyUpgradeStub answers the boot upgrade's preview with what the test
// gave it: nil, which no engine answers (it lists every package it ships),
// and a failed read, a fault of the database under the engine that no
// request can make it produce.
type vocabularyUpgradeStub struct {
	stubDataset
	plans []substrate.ShippedUpgrade
	err   error
}

func (d vocabularyUpgradeStub) PlanShippedUpgrade(context.Context) ([]substrate.ShippedUpgrade, error) {
	return d.plans, d.err
}

// llmPackageRef is the second seeded package (record 0077): the withheld
// upgrade the preview reports is one of its kinds, because that is where the
// agent runtime's declarations live.
const llmPackageRef = coreAuthorityName + "/llm"

// A preview with nothing in it answers an empty list, not an absent one, so a
// client can tell "nothing listed" from "no answer".
func TestVocabularyUpgradeAnswersAnEmptyList(t *testing.T) {
	env := newStubEnv(t, vocabularyUpgradeStub{})
	rec := env.do(t, http.MethodGet, "/api/v1/vocabulary/upgrade", stubToken, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := rec.Body.String(); got != "{\"items\":[]}\n" {
		t.Fatalf("body = %s, want an empty items list", got)
	}
}

// A preview that fails is the ordinary substrate error, not a 200 with
// nothing in it.
func TestVocabularyUpgradeReportsAFailedPreview(t *testing.T) {
	env := newStubEnv(t, vocabularyUpgradeStub{err: errVocabularyUpgradeRead})
	rec := env.do(t, http.MethodGet, "/api/v1/vocabulary/upgrade", stubToken, nil)
	wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)
}

// errVocabularyUpgradeRead is the stub's failed preview read.
var errVocabularyUpgradeRead = errors.New("the preview read failed")
