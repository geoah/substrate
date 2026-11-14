package api

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The stub a test of the handler alone runs on: routing, decoding, headers,
// the envelope for a sentinel, the auth middleware. It implements almost
// nothing, so what such a test asserts cannot be an answer of the engine's.
// A test that needs a method embeds stubDataset (or stubService) in a type
// of its own and writes that one method; a handler path that reaches a
// method nobody wrote calls through a nil interface and panics, which the
// recovery middleware answers 500, and the test fails on its status. In an
// agent stream's handler (a chat, a thread's events) the same panic ends the
// test binary instead (`fatal error: unknown caller pc`), so a test whose
// handler opens a stream writes every method the stream calls, WatchSignal
// among them, on its type. What a write stores or refuses is the engine's,
// and is tested on a dbEnv (engine_db_test.go).

const (
	// stubToken is the one bearer a stubService accepts.
	stubToken = "substrate_tok_stub"
	// stubRepository is the repository the stub's token opens.
	stubRepository = "stub.example.com"
	// stubInstanceID is the instance id the stub answers.
	stubInstanceID = "stubinstance0000000000000a"
)

// stubService authenticates stubToken to ds and answers the instance id.
type stubService struct {
	substrate.Service
	ds   substrate.Dataset
	info substrate.TokenInfo
}

func (s *stubService) Authenticate(_ context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	if secret != stubToken {
		return nil, substrate.TokenInfo{}, substrate.ErrAuth
	}
	return s.ds, s.info, nil
}

func (s *stubService) InstanceID() string { return stubInstanceID }

// RepositoryIDOf answers a name as its own id: the stub's repository is
// stubRepository by both.
func (s *stubService) RepositoryIDOf(_ context.Context, nameOrID string) (string, error) {
	return nameOrID, nil
}

// stubDataset is a repository that answers who it is and nothing else.
type stubDataset struct{ substrate.Dataset }

// PersonRoles answers every token's person as an admin: the handler's own
// tests hold nobody to roles (the role tests run on the engine).
func (stubDataset) PersonRoles(context.Context) ([]substrate.RoleRules, error) {
	return []substrate.RoleRules{{ID: "admin"}}, nil
}

func (stubDataset) Repository() substrate.RepositoryInfo {
	return substrate.RepositoryInfo{ID: stubRepository, Authority: stubRepository, State: "active"}
}

// newStubEnv serves the handler on a stubService whose token opens ds; a nil
// ds is a bare stubDataset. cfg edits the Config after it is filled.
func newStubEnv(t *testing.T, ds substrate.Dataset, cfg ...func(*Config)) *testEnv {
	t.Helper()
	if ds == nil {
		ds = stubDataset{}
	}
	svc := &stubService{ds: ds, info: substrate.TokenInfo{ID: "tok1", Label: "test", Created: time.Unix(0, 0).UTC()}}
	return newStubEnvOn(t, svc, cfg...)
}

// newStubEnvOn serves the handler on a service the test built, usually a
// type embedding *stubService.
func newStubEnvOn(t *testing.T, svc substrate.Service, cfg ...func(*Config)) *testEnv {
	t.Helper()
	clock := &testClock{t: time.Unix(1_700_000_000, 0).UTC()}
	c := Config{Service: svc, Now: clock.now, InviteCode: testInviteCode}
	for _, fn := range cfg {
		fn(&c)
	}
	return &testEnv{h: New(c), clock: clock}
}

// stubKinds is a stubDataset that also resolves the kinds it was given: the
// one read a record route makes before it decodes the rest of the request.
type stubKinds struct {
	stubDataset
	refs []string
}

func newStubKinds(refs ...string) stubKinds { return stubKinds{refs: refs} }

func (d stubKinds) KindByRef(_ context.Context, ref string) (substrate.KindInfo, error) {
	for _, r := range d.refs {
		if r == ref {
			authority, pkg, name := vocabulary.SplitKindRef(r)
			return substrate.KindInfo{Identity: r, Authority: authority, Package: pkg, Name: name, Version: 1, Source: "installed"}, nil
		}
	}
	return substrate.KindInfo{}, substrate.ErrNotFound
}
