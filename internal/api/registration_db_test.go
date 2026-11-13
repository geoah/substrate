package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// Registration has no key step (decision 0264): the answer is the token and
// the repository, nothing to keep beside them; the new repository holds no
// recovery record and does not declare its kind; and a client that still
// sends a recipient is refused, naming the field, rather than told it holds
// a key that opens nothing.
func TestRegistrationHasNoKeyStep(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	repository := testdb.Repository(t)

	rec := authRegisterAtTheDoor(t, env, repository)
	wantStatus(t, rec, http.StatusCreated)
	var answer map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range answer {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if want := []string{"repository", "secret", "token"}; !slices.Equal(keys, want) {
		t.Fatalf("registration answered %v, want %v and nothing else", keys, want)
	}
	var token string
	if err := json.Unmarshal(answer["secret"], &token); err != nil || token == "" {
		t.Fatalf("registration minted no token: %s", answer["secret"])
	}

	ds, err := env.svc.Dataset(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.KindByRef(context.Background(), "substrate.reamde.dev/core/recoverykey"); err == nil {
		t.Fatal("a new repository declares core/recoverykey")
	}
	rec = env.do(t, http.MethodGet, "/api/v1/substrate.reamde.dev/core/recoverykey/self", token, nil)
	wantStatus(t, rec, http.StatusNotFound)

	// The old request shape is refused at the door, naming the field.
	other := testdb.RepositoryLabel(t) + "2.example.com"
	rec = env.do(t, http.MethodPost, registerEnrolPath, "", map[string]any{
		"inviteCode": testInviteCode, "repository": other,
	})
	wantStatus(t, rec, http.StatusOK)
	enrollment := decodeJSON[substrate.TOTPEnrollment](t, rec)
	rec = env.do(t, http.MethodPost, registerPath, "", map[string]any{
		"inviteCode": testInviteCode, "repository": other, "password": authPassword,
		"totpSecret": enrollment.Secret, "totpCode": env.code(t, enrollment.Secret),
		"recoveryPublicKey": "age1k8ddys8rfmyge6g525zffjvmzjgp9cth7aked27g5p0fanvme3jsfz2eky",
	})
	wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	if msg := decodeJSON[substrate.ErrorEnvelope](t, rec).Error.Message; !strings.Contains(msg, "recoveryPublicKey") {
		t.Fatalf("the refusal said %q; it must name recoveryPublicKey", msg)
	}
	if _, err := env.svc.Dataset(context.Background(), other); err == nil {
		t.Fatal("a refused registration created its repository")
	}
}
