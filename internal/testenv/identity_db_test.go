package testenv_test

// A sign-in names a person, who opens a repository (decision 0274). The
// person is an identity beside the repositories: registration makes one with
// its repository, a sign-in names it and reaches the repository, a change of
// its factors writes no repository's history, and the tables and the wire
// hold one person in two repositories, which no door makes yet.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/internal/vocabulary"
)

const identityPassword = "correct-horse-battery-staple"

// doorPost sends a body to a door with no bearer and answers the status and
// the body.
func doorPost(t *testing.T, e *testenv.Env, path string, body map[string]any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	status, out, _ := e.DoRaw(http.MethodPost, path, raw, map[string]string{"Content-Type": "application/json", "Authorization": ""})
	return status, out
}

func refusalOf(t *testing.T, raw []byte) substrate.ErrorPayload {
	t.Helper()
	var env substrate.ErrorEnvelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Error.Code == "" {
		t.Fatalf("not an error envelope: %s (%v)", raw, err)
	}
	return env.Error
}

func mintedOf(t *testing.T, raw []byte) substrate.MintedToken {
	t.Helper()
	var out substrate.MintedToken
	if err := json.Unmarshal(raw, &out); err != nil || out.Secret == "" {
		t.Fatalf("not a minted token: %s (%v)", raw, err)
	}
	return out
}

// identityOver reads the person the session's token acts for.
func identityOver(t *testing.T, e *testenv.Env) substrate.IdentityInfo {
	t.Helper()
	var person substrate.IdentityInfo
	mustDecode(t, e, http.MethodGet, "/identity", nil, &person)
	return person
}

// rawDB opens the schema underneath the server.
func rawDB(t *testing.T, e *testenv.Env) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", e.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// Registration makes a person and their repository; a sign-in names the
// person and reaches the repository; a request from before identities is
// told what replaced its field; and a wrong password, a wrong code and a
// name nobody holds are one answer.
func TestASignInNamesThePersonAndReachesTheirRepository(t *testing.T) {
	clock := &engine.TestClock{}
	e := testenv.Start(t, testenv.WithClock(clock.Now))

	person := identityOver(t, e)
	if !vocabulary.ValidIdentityID(person.ID) || person.Name != e.Repository || !person.SecondFactor ||
		len(person.Repositories) != 1 || person.Repositories[0].Name != e.Repository ||
		!vocabulary.ValidRepositoryID(person.Repositories[0].ID) {
		t.Fatalf("the registration's person = %+v, want one named %s in that one repository", person, e.Repository)
	}

	clock.Advance(engine.TOTPPeriod)
	status, raw := e.Login(e.Repository, identityPassword, e.TOTPCode())
	if status != http.StatusCreated {
		t.Fatalf("sign in: %d %s", status, raw)
	}
	if minted := mintedOf(t, raw); minted.Repository != e.Repository || minted.Identity != e.Repository {
		t.Fatalf("the sign-in reached %q as %q, want %s as its person", minted.Repository, minted.Identity, e.Repository)
	}
	// The token it minted opens the repository and names the person.
	if status, raw := e.Do(http.MethodGet, "/api/v1/substrate.reamde.dev/core/repository/"+person.Repositories[0].ID, nil); status != http.StatusOK {
		t.Fatalf("the sign-in's token does not reach its repository: %d %s", status, raw)
	}
	if again := identityOver(t, e); again.ID != person.ID {
		t.Fatalf("the sign-in's token acts for %+v, want %+v", again, person)
	}

	// A body from before identities names only a repository.
	clock.Advance(engine.TOTPPeriod)
	status, raw = doorPost(t, e, "/login", map[string]any{
		"repository": e.Repository, "password": identityPassword, "totpCode": e.TOTPCode(),
	})
	if refusal := refusalOf(t, raw); status != http.StatusBadRequest || refusal.Code != "bad_request" ||
		!strings.Contains(refusal.Message, "`identity`") {
		t.Fatalf("an old sign-in: %d %+v, want 400 naming the field that replaced it", status, refusal)
	}

	var refusals []substrate.ErrorPayload
	for _, body := range []map[string]any{
		{"identity": e.Repository, "password": "not-the-password-at-all", "totpCode": e.TOTPCode()},
		{"identity": e.Repository, "password": identityPassword, "totpCode": "000000"},
		{"identity": "nobody.example.com", "password": identityPassword, "totpCode": "000000"},
	} {
		clock.Advance(engine.TOTPPeriod)
		status, raw := doorPost(t, e, "/login", body)
		if status != http.StatusUnauthorized {
			t.Fatalf("a refused sign-in %v: %d %s, want 401", body["identity"], status, raw)
		}
		refusals = append(refusals, refusalOf(t, raw))
	}
	for _, r := range refusals[1:] {
		if r.Code != refusals[0].Code || r.Reason != refusals[0].Reason || r.Message != refusals[0].Message {
			t.Fatalf("two refused sign-ins answer differently: %+v and %+v", refusals[0], r)
		}
	}
}

// A person's factors are theirs, not a repository's: a password change and
// the operator's reset write nothing to any repository's history, and the
// reset runs beside the serving server, which signs the person in with the
// new factors at once.
func TestAFactorChangeWritesNoRepositorysHistory(t *testing.T) {
	clock := &engine.TestClock{}
	key, root := testenv.MintCredentialKey(), t.TempDir()
	e := testenv.Start(t, testenv.WithClock(clock.Now), testenv.WithCredentialKey(key), testenv.WithDataRoot(root))
	db := rawDB(t, e)
	history := func() (entries, sealed int) {
		t.Helper()
		if err := db.QueryRow(`SELECT (SELECT count(*) FROM changelog), (SELECT count(*) FROM sealed)`).Scan(&entries, &sealed); err != nil {
			t.Fatal(err)
		}
		return entries, sealed
	}
	entries, sealed := history()

	clock.Advance(engine.TOTPPeriod)
	if status, raw := doorPost(t, e, "/password", map[string]any{
		"identity": e.Repository, "password": identityPassword, "totpCode": e.TOTPCode(),
		"newPassword": "a-second-correct-horse",
	}); status != http.StatusOK {
		t.Fatalf("change the password: %d %s", status, raw)
	}
	if e2, s2 := history(); e2 != entries || s2 != sealed {
		t.Fatalf("a password change wrote history: %d entries and %d sealed rows, %d and %d before", e2, s2, entries, sealed)
	}

	op, err := engine.OpenOperator(context.Background(), e.DSN,
		engine.WithRegistry(vocabulary.NewRegistry()), engine.WithDataRoot(root),
		engine.WithCredentialKey(key), engine.WithDirectoryReadOnly())
	if err != nil {
		t.Fatalf("open the operator's hat beside the server: %v", err)
	}
	t.Cleanup(func() { _ = op.Close() })
	enrollment, err := op.ResetUser(context.Background(), e.Repository, "an-operator-issued-password", "")
	if err != nil {
		t.Fatalf("reset beside the server: %v", err)
	}
	if e2, s2 := history(); e2 != entries || s2 != sealed {
		t.Fatalf("a reset wrote history: %d entries and %d sealed rows, %d and %d before", e2, s2, entries, sealed)
	}
	e.TOTPSecret = enrollment.Secret
	clock.Advance(engine.TOTPPeriod)
	if status, raw := e.Login(e.Repository, "an-operator-issued-password", e.TOTPCode()); status != http.StatusCreated {
		t.Fatalf("the serving server does not take the reset's factors: %d %s", status, raw)
	}
}

// One person in two repositories, which no door makes before a second person
// may be added (decision 0274): the tables hold it as one more row, and the
// wire carries it. A sign-in naming no repository is answered with both and
// spends no code; naming one, by name or id, opens it; the person lists both;
// signing out everywhere ends their tokens in both and nobody else's; and
// deleting one repository leaves the person in the other.
func TestOnePersonInTwoRepositories(t *testing.T) {
	clock := &engine.TestClock{}
	e := testenv.Start(t, testenv.WithClock(clock.Now))
	other := e.RegisterUser("second", identityPassword)
	// A person is in a repository while a person actor there names them:
	// the operator's door writes the first person's actor in the second
	// repository, then the server's index of it.
	if err := e.Service.(engine.Operator).AddPerson(context.Background(), e.Repository, other.Repository, scope.RoleAdmin); err != nil {
		t.Fatalf("give the first person the second repository: %v", err)
	}

	clock.Advance(engine.TOTPPeriod)
	code := e.TOTPCode()
	status, raw := doorPost(t, e, "/login", map[string]any{"identity": e.Repository, "password": identityPassword, "totpCode": code})
	refusal := refusalOf(t, raw)
	if status != http.StatusConflict || refusal.Reason != substrate.ReasonRepositoryRequired || len(refusal.Repositories) != 2 {
		t.Fatalf("a sign-in into one of two naming none: %d %+v, want 409 %s with both", status, refusal, substrate.ReasonRepositoryRequired)
	}
	names := map[string]string{}
	for _, c := range refusal.Repositories {
		names[c.Name] = c.ID
	}
	if names[e.Repository] == "" || names[other.Repository] == "" {
		t.Fatalf("the choices %+v, want %s and %s", refusal.Repositories, e.Repository, other.Repository)
	}

	// The same code, naming the second repository: the 409 spent nothing. The
	// door's pacing spaces two sign-ins of one person; the verifier takes the
	// code a step either side.
	clock.Advance(6 * time.Second)
	status, raw = doorPost(t, e, "/login", map[string]any{
		"identity": e.Repository, "repository": other.Repository, "password": identityPassword, "totpCode": code,
	})
	if status != http.StatusCreated {
		t.Fatalf("the sign-in naming the second repository: %d %s", status, raw)
	}
	inSecond := mintedOf(t, raw)
	if inSecond.Repository != other.Repository || inSecond.Identity != e.Repository {
		t.Fatalf("the sign-in reached %q as %q", inSecond.Repository, inSecond.Identity)
	}
	if person := identityOver(t, e.WithToken(inSecond.Secret)); len(person.Repositories) != 2 {
		t.Fatalf("the person lists %+v, want both repositories", person.Repositories)
	}
	if status, raw := e.WithToken(inSecond.Secret).Do(http.MethodGet, "/api/v1/substrate.reamde.dev/core/repository/"+names[other.Repository], nil); status != http.StatusOK {
		t.Fatalf("the token does not reach the second repository: %d %s", status, raw)
	}
	// Their write there links their own person actor, not that repository's
	// first person's.
	if status, raw := e.WithToken(inSecond.Secret).Do(http.MethodPost, "/api/v1/records", map[string]any{
		"kind": "substrate.reamde.dev/core/contact", "properties": map[string]any{"name": "Ada"},
	}); status != http.StatusCreated {
		t.Fatalf("a write by the person in the second repository: %d %s", status, raw)
	}
	var page struct {
		Changes []substrate.ChangeRow `json:"changes"`
	}
	mustDecode(t, e.WithToken(inSecond.Secret), http.MethodGet, "/api/v1/changes?before=0&first=1", nil, &page)
	person := identityOver(t, e.WithToken(inSecond.Secret))
	if len(page.Changes) != 1 || page.Changes[0].Actor != vocabulary.ActorID(substrate.PersonActor(person.ID)) {
		t.Fatalf("the person's write in the second repository links %+v, want their own actor %s", page.Changes, vocabulary.ActorID(substrate.PersonActor(person.ID)))
	}

	// By id, the first.
	clock.Advance(engine.TOTPPeriod)
	status, raw = doorPost(t, e, "/login", map[string]any{
		"identity": e.Repository, "repository": names[e.Repository], "password": identityPassword, "totpCode": e.TOTPCode(),
	})
	if status != http.StatusCreated || mintedOf(t, raw).Repository != e.Repository {
		t.Fatalf("the sign-in naming the first repository by id: %d %s", status, raw)
	}
	inFirst := mintedOf(t, raw)

	// A repository the person does not open is refused, whether it exists.
	for _, named := range []string{"nosuch.example.com", "third.example.com"} {
		clock.Advance(engine.TOTPPeriod)
		status, raw = doorPost(t, e, "/login", map[string]any{
			"identity": e.Repository, "repository": named, "password": identityPassword, "totpCode": e.TOTPCode(),
		})
		if status != http.StatusForbidden {
			t.Fatalf("a sign-in naming %s: %d %s, want 403", named, status, raw)
		}
	}

	// The second repository's own person is untouched by the first's sign-out.
	clock.Advance(engine.TOTPPeriod)
	if status, raw := doorPost(t, e, "/signout/everywhere", map[string]any{
		"identity": e.Repository, "password": identityPassword, "totpCode": e.TOTPCode(),
	}); status != http.StatusOK {
		t.Fatalf("sign out everywhere: %d %s", status, raw)
	}
	for what, secret := range map[string]string{"first": inFirst.Secret, "second": inSecond.Secret} {
		if status, _ := e.WithToken(secret).Do(http.MethodGet, "/tokens", nil); status != http.StatusUnauthorized {
			t.Fatalf("the person's token in the %s repository answered %d after the sign-out, want 401", what, status)
		}
	}
	if status, raw := other.Do(http.MethodGet, "/tokens", nil); status != http.StatusOK {
		t.Fatalf("the other person's token answered %d %s after somebody else's sign-out", status, raw)
	}

	// Deleting the second repository leaves the first person in the first.
	clock.Advance(engine.TOTPPeriod)
	if status, raw := doorPost(t, e, "/repository/delete", map[string]any{
		"identity": e.Repository, "repository": other.Repository, "password": identityPassword, "totpCode": e.TOTPCode(),
	}); status/100 != 2 {
		t.Fatalf("delete the second repository: %d %s", status, raw)
	}
	clock.Advance(engine.TOTPPeriod)
	status, raw = doorPost(t, e, "/login", map[string]any{"identity": e.Repository, "password": identityPassword, "totpCode": e.TOTPCode()})
	if status != http.StatusCreated || mintedOf(t, raw).Repository != e.Repository {
		t.Fatalf("the person after one of their repositories went: %d %s, want a sign-in to the other", status, raw)
	}
}
