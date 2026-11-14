package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// vocabularyDoc is one declaration document a request body carries.
func vocabularyDoc() map[string]any {
	return map[string]any{
		"kind":     corePackage + "/package",
		"metadata": map[string]any{"id": stubRepository + "/widgets"},
		"data":     map[string]any{"authority": stubRepository, "package": "widgets"},
	}
}

// The preview's body is the documents alone, so a confirmation there is the
// unknown key the strict decoder refuses before the dataset is reached.
func TestSchemaPlanRefusesAConfirmation(t *testing.T) {
	env := newStubEnv(t, nil)
	rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/plan", stubToken, map[string]any{
		"documents": []map[string]any{vocabularyDoc()},
		"confirm":   map[string]any{"planHash": "cafe", "changelogSeq": 41},
	})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	wantMessage(t, rec, "confirm")
}

// vocabularyTakeover records the takeover the route hands the dataset and
// answers err, or the package row with the taking hand as its declarer: the
// engine's refusals are reached for real in vocabulary_db_test.go, and this
// holds the route's mapping of each sentinel, one of which (a hand that is not
// the owner's) no request to the route can make the engine return.
type vocabularyTakeover struct {
	stubDataset
	asked *[]string
	err   error
}

func (d vocabularyTakeover) TakeOverPackage(_ context.Context, actor substrate.Actor, pkg, confirm string) (*substrate.Record, error) {
	*d.asked = []string{string(actor), pkg, confirm}
	if d.err != nil {
		return nil, d.err
	}
	return &substrate.Record{ID: pkg, Kind: corePackage + "/package", Version: 2, Properties: map[string]any{"declaredBy": string(actor)}}, nil
}

// THE TAKEOVER ROUTE (decision 0205) hands the dataset the package, the
// confirmation and the person's hand; a body without a package, or
// with a key the body does not declare, is refused before the dataset; and
// the dataset's refusals keep their codes.
func TestTheTakeoverRouteHandsTheEngineItsInputsAndKeepsItsCodes(t *testing.T) {
	pkg := stubRepository + "/training"
	var asked []string
	env := newStubEnv(t, vocabularyTakeover{asked: &asked})
	rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/takeover", stubToken,
		map[string]any{"package": pkg, "confirm": pkg})
	wantStatus(t, rec, http.StatusOK)
	if got := fmt.Sprint(asked); got != fmt.Sprint([]string{string(substrate.ActorPerson), pkg, pkg}) {
		t.Fatalf("the dataset was asked %s", got)
	}

	asked = nil
	wantErrorCode(t, env.do(t, http.MethodPost, "/api/v1/vocabulary/takeover", stubToken, map[string]any{"confirm": pkg}), http.StatusBadRequest, codeBadRequest)
	wantErrorCode(t, env.do(t, http.MethodPost, "/api/v1/vocabulary/takeover", stubToken, map[string]any{"package": pkg, "force": true}), http.StatusBadRequest, codeBadRequest)
	if asked != nil {
		t.Fatalf("a refused body reached the dataset: %v", asked)
	}

	for err, status := range map[error]int{
		fmt.Errorf("%w: wrong hand", substrate.ErrForbidden):       http.StatusForbidden,
		fmt.Errorf("%w: confirm differs", substrate.ErrValidation): http.StatusUnprocessableEntity,
		fmt.Errorf("%w: not an app's", substrate.ErrConflict):      http.StatusConflict,
		fmt.Errorf("%w: package %s", substrate.ErrNotFound, pkg):   http.StatusNotFound,
	} {
		env := newStubEnv(t, vocabularyTakeover{asked: &asked, err: err})
		if rec := env.do(t, http.MethodPost, "/api/v1/vocabulary/takeover", stubToken, map[string]any{"package": pkg, "confirm": pkg}); rec.Code != status {
			t.Errorf("%v: %d %s, want %d", err, rec.Code, rec.Body.String(), status)
		}
	}
}
