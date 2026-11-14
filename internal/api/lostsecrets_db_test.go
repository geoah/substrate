package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/kinds"
)

const lostProviderPath = "/api/v1/substrate.reamde.dev/llm/provider/openai"

// lostSecretsBoot boots a server under key on the export extracted into the
// archive it reads, the way a host the export is restored on does, and
// serves it.
func lostSecretsBoot(t *testing.T, env *dbEnv, archive []byte, key string, opts ...engine.Option) (substrate.Service, *httptest.Server, error) {
	t.Helper()
	root := t.TempDir()
	watchExtract(t, filepath.Join(root, "archive"), archive)
	svc, err := engine.Open(context.Background(), dbTemplate.Clone(t), append([]engine.Option{
		engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(key),
		engine.WithTestTOTPClock(env.clock.now),
		engine.WithLLMProviderSeed(false),
		engine.WithUnitSeed(false),
		engine.WithRecordSeed(false),
	}, opts...)...)
	if err != nil {
		return nil, nil, err
	}
	t.Cleanup(func() { _ = svc.Close() })
	srv := httptest.NewServer(New(Config{Service: svc, Now: env.clock.now, InviteCode: testInviteCode}))
	t.Cleanup(srv.Close)
	return svc, srv, nil
}

// lostSecretsGet reads one path from a served restore with the bearer.
func lostSecretsGet(t *testing.T, srv *httptest.Server, path, token string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// lostSecretsLogin signs in at a served restore and answers the status.
func lostSecretsLogin(t *testing.T, srv *httptest.Server, body map[string]any) int {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := srv.Client().Post(srv.URL+loginPath, "application/json", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// A repository exported under one host key restores under another only as
// the operator says: refused, naming the key and the variable, until it is
// listed; then without its secrets. Every record arrives and the provider's
// key reads unset. The owner's sign-in, sealed under the other key too, is
// left out, so their password signs in nowhere and the bearer they hold acts
// for nobody here, until the operator's reset gives them back under the
// identity their actor names. The same export under the key it was written
// under arrives with its secrets and its taker.
func TestAnExportRestoresUnderAnotherHostKeyWithoutItsSecrets(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withoutRepository())
	ctx := context.Background()
	repository := testdb.Repository(t)
	// No second factor, so the password alone signs in where it traveled.
	token := authRegister(t, env, repository, false).token
	ds, err := env.svc.Dataset(ctx, repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Put(ctx, substrate.ActorSystem, substrate.PutInput{
		Kind: "substrate.reamde.dev/llm/provider", ID: "openai",
		Properties: map[string]any{"label": "OpenAI", "wire": "openai", "baseURL": "https://llm.example.com/v1", "apiKey": "sk-left-behind"},
	}); err != nil {
		t.Fatalf("put the provider: %v", err)
	}
	source := httptest.NewServer(env.h)
	t.Cleanup(source.Close)
	status, provider := lostSecretsGet(t, source, lostProviderPath, token)
	if status != http.StatusOK || provider["properties"].(map[string]any)["apiKey"] == nil {
		t.Fatalf("the provider before the export = %d %v", status, provider)
	}
	rec := env.do(t, http.MethodGet, "/api/v1/export", token, nil)
	wantStatus(t, rec, http.StatusOK)
	archive := rec.Body.Bytes()
	login := loginBody(repository, authPassword, "")

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	other := base64.StdEncoding.EncodeToString(raw)
	if _, _, err := lostSecretsBoot(t, env, archive, other); err == nil ||
		!strings.Contains(err.Error(), engine.ImportWithoutSecretsVar) || !strings.Contains(err.Error(), "is not the key the DEK wrap") {
		t.Fatalf("an unlisted restore under another key = %v, want refused naming the key and %s", err, engine.ImportWithoutSecretsVar)
	}

	moved, srv, err := lostSecretsBoot(t, env, archive, other, engine.WithImportWithoutSecrets(repository))
	if err != nil {
		t.Fatalf("a listed restore under another key: %v", err)
	}
	if status, _ := lostSecretsGet(t, srv, lostProviderPath, token); status != http.StatusUnauthorized {
		t.Fatalf("the bearer before the reset = %d, want 401: its person stayed behind", status)
	}
	if status := lostSecretsLogin(t, srv, login); status != http.StatusUnauthorized {
		t.Fatalf("a login with the password that stayed behind = %d, want 401", status)
	}
	if _, err := moved.(engine.Operator).ResetUser(ctx, repository, "a password after the move", ""); err != nil {
		t.Fatalf("give the owner back: %v", err)
	}
	status, provider = lostSecretsGet(t, srv, lostProviderPath, token)
	if status != http.StatusOK {
		t.Fatalf("the restored provider = %d %v", status, provider)
	}
	props := provider["properties"].(map[string]any)
	if _, ok := props["apiKey"]; ok || props["label"] != "OpenAI" {
		t.Fatalf("the provider restored without its secrets reads %v, want its label and no key", props)
	}

	_, same, err := lostSecretsBoot(t, env, archive, dbCredentialKey, engine.WithImportWithoutSecrets(repository))
	if err != nil {
		t.Fatalf("the restore under the key it was written under: %v", err)
	}
	status, provider = lostSecretsGet(t, same, lostProviderPath, token)
	if status != http.StatusOK || provider["properties"].(map[string]any)["apiKey"] == nil {
		t.Fatalf("the provider under its own key = %d %v, want its key", status, provider)
	}
	if status := lostSecretsLogin(t, same, login); status != http.StatusCreated {
		t.Fatalf("a login under the key the credential traveled with = %d, want 201", status)
	}
}
