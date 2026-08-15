package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"filippo.io/age"

	"github.com/geoah/substrate/internal/substrate"
)

// The api package is developed against this hand-written fake rather than
// the engine: the HTTP layer's contract is substrate.Service and nothing
// else, and the tests must run without a database.

type fakeService struct {
	mu       sync.Mutex
	datasets map[string]*fakeDataset
	tokens   map[string]fakeToken // bearer secret -> token
	// passwords and codes are what this fake's users know: a registration
	// records both, and login checks them. The real verifier is the engine's
	// (engine/auth.go); what the HTTP layer owes is the invite gate, the rate
	// limit and the password-factor rule, and those are what
	// these tests exercise.
	passwords map[string]string
	codes     map[string]string

	createRepositoryErr error
	registerErr         error
	loginErr            error
	registerCalls       int
	loginCalls          int
	recoveryEnrolled    map[string]bool
	// authErr, when non-nil and NOT substrate.ErrAuth, models a repository
	// that could not be opened: the API maps it to 503.
	authErr error
	// totpDisabled models the engine's dev escape hatch: the second factor is
	// not verified, so any code — including none — passes.
	totpDisabled bool
}

type fakeToken struct {
	repository string
	info       substrate.TokenInfo
}

func newFakeService() *fakeService {
	s := &fakeService{
		datasets:  map[string]*fakeDataset{},
		tokens:    map[string]fakeToken{},
		passwords: map[string]string{},
		codes:     map[string]string{},
	}
	s.addRepository("geoah")
	s.passwords["geoah"] = "correct-horse-battery-staple"
	return s
}

func (s *fakeService) addRepository(name string) *fakeDataset {
	ds := newFakeDataset(name)
	s.datasets[name] = ds
	s.codes[name] = fakeCode(name)
	return ds
}

// fakeCode stands in for the repository seed's current six-digit output.
func fakeCode(repository string) string {
	h := 1
	for _, r := range repository {
		h = (h*31 + int(r)) % 900000
	}
	return fmt.Sprintf("%06d", 100000+h)
}

// fakeEnrollment is the shape the enrollment endpoints return.
func fakeEnrollment(username string) substrate.TOTPEnrollment {
	return substrate.TOTPEnrollment{
		Secret: "JBSWY3DPEHPK3PXP",
		URI: "otpauth://totp/Substrate:" + username +
			"?secret=JBSWY3DPEHPK3PXP&issuer=Substrate&algorithm=SHA1&digits=6&period=30",
	}
}

// token registers a bearer secret for a repository. The secret carries NO
// username segment — the hash lookup is what finds the repository.
func (s *fakeService) token(repository string) string {
	secret := "substrate_tok_" + fmt.Sprint(len(s.tokens)+1)
	s.tokens[secret] = fakeToken{
		repository: repository,
		info: substrate.TokenInfo{
			ID: "tok" + fmt.Sprint(len(s.tokens)+1), Label: "test",
			Created: time.Unix(0, 0).UTC(),
		},
	}
	return secret
}

// tokenWith registers a bearer secret whose TokenInfo the test tailors — an
// expiry, for the spent-credential path.
func (s *fakeService) tokenWith(repository string, edit func(*substrate.TokenInfo)) string {
	secret := "substrate_tok_" + fmt.Sprint(len(s.tokens)+1)
	info := substrate.TokenInfo{
		ID: "tok" + fmt.Sprint(len(s.tokens)+1), Label: "test",
		Created: time.Unix(0, 0).UTC(),
	}
	edit(&info)
	s.tokens[secret] = fakeToken{repository: repository, info: info}
	return secret
}

func (s *fakeService) Repositories(context.Context) ([]substrate.RepositoryInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]substrate.RepositoryInfo, 0, len(s.datasets))
	for _, ds := range s.datasets {
		out = append(out, ds.Repository())
	}
	return out, nil
}

func (s *fakeService) Dataset(_ context.Context, repository string) (substrate.Dataset, error) {
	ds, ok := s.datasets[repository]
	if !ok {
		return nil, fmt.Errorf("%w: repository %q", substrate.ErrNotFound, repository)
	}
	return ds, nil
}

func (s *fakeService) CreateRepository(_ context.Context, name string) (substrate.RepositoryInfo, error) {
	if s.createRepositoryErr != nil {
		return substrate.RepositoryInfo{}, s.createRepositoryErr
	}
	ds := s.addRepository(name)
	return ds.Repository(), nil
}

func (s *fakeService) BeginRegistration(_ context.Context, username string) (substrate.TOTPEnrollment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerCalls++
	if s.registerErr != nil {
		return substrate.TOTPEnrollment{}, s.registerErr
	}
	if _, taken := s.datasets[username]; taken {
		return substrate.TOTPEnrollment{}, fmt.Errorf("%w: user %q already exists", substrate.ErrValidation, username)
	}
	return fakeEnrollment(username), nil
}

func (s *fakeService) Register(_ context.Context, in substrate.RegisterInput) (substrate.RegisterResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerCalls++
	if s.registerErr != nil {
		return substrate.RegisterResult{}, s.registerErr
	}
	if _, taken := s.datasets[in.Username]; taken {
		return substrate.RegisterResult{}, fmt.Errorf("%w: user %q already exists", substrate.ErrValidation, in.Username)
	}
	if in.TOTPCode != fakeCode(in.Username) {
		return substrate.RegisterResult{}, fmt.Errorf("%w: bad code", substrate.ErrAuth)
	}
	s.addRepository(in.Username)
	s.passwords[in.Username] = in.Password
	secret := s.token(in.Username)
	info := s.tokens[secret].info
	info.Label = in.Label
	out := substrate.RegisterResult{Token: info, Secret: secret, RecoveryPublicKey: in.RecoveryPublicKey}
	if out.RecoveryPublicKey == "" {
		// The fake's stand-in for the server-minted pair: shape, not crypto.
		out.RecoveryKey = "AGE-SECRET-KEY-FAKE"
		out.RecoveryPublicKey = "age1fake"
	}
	// The signing seed's one disclosure: shape, not crypto, like the pair.
	out.SigningSeed = strings.Repeat("ab", 32)
	out.SigningPublicKey = strings.Repeat("cd", 32)
	return out, nil
}

// EnrollRecoveryKey mirrors the engine's factors-gated, one-time enrollment
// with REAL age material, so the endpoint's recipient propagation and
// one-time identity delivery are exercised, not just the route.
func (s *fakeService) EnrollRecoveryKey(_ context.Context, in substrate.LoginInput, publicKey string) (string, string, error) {
	if err := s.verify(in); err != nil {
		return "", "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recoveryEnrolled == nil {
		s.recoveryEnrolled = map[string]bool{}
	}
	if s.recoveryEnrolled[in.Username] {
		return "", "", fmt.Errorf("%w: a recovery key is already enrolled; rotation is not yet supported", substrate.ErrConflict)
	}
	identity := ""
	if publicKey == "" {
		id, err := age.GenerateX25519Identity()
		if err != nil {
			return "", "", err
		}
		identity, publicKey = id.String(), id.Recipient().String()
	} else if _, err := age.ParseX25519Recipient(publicKey); err != nil {
		return "", "", fmt.Errorf("%w: recovery public key is not an age recipient", substrate.ErrValidation)
	}
	s.recoveryEnrolled[in.Username] = true
	return identity, publicKey, nil
}

// verify is the fake's whole factor check: the recorded password and the
// username's canned code, answering ONE error for every failure exactly as
// the engine does.
func (s *fakeService) verify(in substrate.LoginInput) error {
	password, known := s.passwords[in.Username]
	if !known || password != in.Password {
		return fmt.Errorf("%w: bad username, password or code", substrate.ErrAuth)
	}
	if !s.totpDisabled && in.TOTPCode != fakeCode(in.Username) {
		return fmt.Errorf("%w: bad username, password or code", substrate.ErrAuth)
	}
	return nil
}

func (s *fakeService) Login(_ context.Context, in substrate.LoginInput) (substrate.TokenInfo, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loginCalls++
	if s.loginErr != nil {
		return substrate.TokenInfo{}, "", s.loginErr
	}
	if err := s.verify(in); err != nil {
		return substrate.TokenInfo{}, "", err
	}
	secret := s.token(in.Username)
	info := s.tokens[secret].info
	info.Label = in.Label
	return info, secret, nil
}

func (s *fakeService) ChangePassword(_ context.Context, in substrate.LoginInput, newPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verify(in); err != nil {
		return err
	}
	s.passwords[in.Username] = newPassword
	return nil
}

func (s *fakeService) BeginTOTPReenrollment(_ context.Context, in substrate.LoginInput) (substrate.TOTPEnrollment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verify(in); err != nil {
		return substrate.TOTPEnrollment{}, err
	}
	return fakeEnrollment(in.Username), nil
}

func (s *fakeService) ReenrollTOTP(_ context.Context, in substrate.LoginInput, newSecret, newCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.verify(in); err != nil {
		return err
	}
	if newSecret == "" || newCode == "" {
		return fmt.Errorf("%w: the new enrollment is incomplete", substrate.ErrValidation)
	}
	return nil
}

func (s *fakeService) Authenticate(_ context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	if s.authErr != nil {
		return nil, substrate.TokenInfo{}, s.authErr
	}
	t, ok := s.tokens[secret]
	if !ok {
		return nil, substrate.TokenInfo{}, substrate.ErrAuth
	}
	return s.datasets[t.repository], t.info, nil
}

func (s *fakeService) Close() error { return nil }

// ---- dataset ------------------------------------------------------------

type fakeDataset struct {
	mu         sync.Mutex
	repository substrate.RepositoryInfo
	types      []substrate.KindInfo
	records    map[string]*substrate.Record
	// meta is per-record property provenance, attached by Get and ONLY by
	// Get — lists never carry it, exactly like the engine.
	meta map[string]map[string]substrate.PropertyMeta
	// incoming backs the separate paginated reverse-edge resource.
	incoming map[string][]substrate.IncomingEdge
	// formers maps a merged-away id to the record that now wears its data.
	formers map[string]string
	changes []substrate.Change
	// trStates is the canned answer of the change-feed seam: per seq, the
	// trigger chips the engine would compute.
	trStates map[int64][]substrate.ChangeTrigger
	signals  chan int64

	// recorded inputs
	lastQuery substrate.Query
	lastPut   substrate.PutInput
	lastPatch substrate.PatchInput
	lastActor substrate.Actor
	// lastPrincipal is what the engine reads off the write's context: the
	// token id the door resolved, never anything the caller sent.
	lastPrincipal      string
	lastSearch         substrate.SearchInput
	lastVocabularyDocs []map[string]any
	lastDeleteType     string
	lastDeleteID       string

	// error injection, keyed by method name
	errs map[string]error
}

func newFakeDataset(name string) *fakeDataset {
	ds := &fakeDataset{
		repository: substrate.RepositoryInfo{ID: "r_" + name, Name: name, State: "active"},
		types:      testTypes(),
		records:    map[string]*substrate.Record{},
		meta:       map[string]map[string]substrate.PropertyMeta{},
		incoming:   map[string][]substrate.IncomingEdge{},
		formers:    map[string]string{},
		trStates:   map[int64][]substrate.ChangeTrigger{},
		signals:    make(chan int64, 8),
		errs:       map[string]error{},
	}
	return ds
}

func testTypes() []substrate.KindInfo {
	return []substrate.KindInfo{
		{
			Identity: "people.substrate.reamde.dev/person", Name: "person", Authority: "people.substrate.reamde.dev",
			Version: 1, Plural: "people", Source: "builtin",
			Definition: map[string]any{
				"plural": "people",
				"properties": map[string]any{
					"name":    map[string]any{"type": "string"},
					"company": map[string]any{"type": "string"},
					"emails":  map[string]any{"kind": "[string]"},
					// A reference property: the GraphQL Reference
					// object renders the stored {authority, type, id} triple.
					"manager": map[string]any{"type": "reference", "kind": "any"},
				},
				"edges": map[string]any{"member_of": map[string]any{"to": "organization"}},
			},
		},
		{
			Identity: "tasks.substrate.reamde.dev/task", Name: "task", Authority: "tasks.substrate.reamde.dev",
			Version: 1, Plural: "tasks", Source: "builtin",
			Definition: map[string]any{
				"plural": "tasks",
				"traits": []any{"temporal(point: due_at)"},
				"properties": map[string]any{
					"note": map[string]any{"type": "markdown"},
					"status": map[string]any{
						"type":   "state",
						"states": []any{"open", "done"},
						"transitions": []any{
							map[string]any{"from": "open", "to": "done", "actor": "owner", "stamps": map[string]any{"completed_at": "now"}},
						},
					},
				},
			},
		},
		{
			Identity: "messaging.substrate.reamde.dev/conversationmessage", Name: "conversationmessage",
			Authority: "messaging.substrate.reamde.dev",
			Version:   1, Plural: "conversationmessages", Source: "builtin",
			Definition: map[string]any{
				"plural":     "conversationmessages",
				"traits":     []any{"temporal(point)"},
				"properties": map[string]any{"text": map[string]any{"type": "markdown"}},
			},
		},
		{
			Identity: "library.substrate.reamde.dev/book", Name: "book",
			Authority: "library.substrate.reamde.dev",
			Version:   1, Plural: "books", Source: "builtin",
			Definition: map[string]any{
				"plural": "books",
				"properties": map[string]any{
					"media_ref": map[string]any{"type": "url"},
					"duration":  map[string]any{"type": "duration"},
				},
			},
		},
		{
			Identity: "core.substrate.reamde.dev/repository", Name: "repository", Authority: coreAuthority,
			Version: 1, Plural: "repositories", Source: "builtin",
			Definition: map[string]any{
				"plural":     "repositories",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
		},
		{
			Identity: "core.substrate.reamde.dev/connector", Name: "connector", Authority: coreAuthority,
			Version: 1, Plural: "connectors", Source: "builtin",
			Definition: map[string]any{
				"plural":     "connectors",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
		},
		{
			Identity: "core.substrate.reamde.dev/token", Name: "token", Authority: coreAuthority,
			Version: 1, Plural: "tokens", Source: "builtin",
			Definition: map[string]any{
				"plural":     "tokens",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
		},
	}
}

func (d *fakeDataset) fail(op string) error { return d.errs[op] }

func (d *fakeDataset) Repository() substrate.RepositoryInfo { return d.repository }

func (d *fakeDataset) Kinds(context.Context) ([]substrate.KindInfo, error) {
	return d.types, d.fail("Types")
}

func (d *fakeDataset) KindByRef(_ context.Context, identity string) (substrate.KindInfo, error) {
	for _, t := range d.types {
		if t.Identity == identity {
			return t, nil
		}
	}
	return substrate.KindInfo{}, fmt.Errorf("%w: type %q", substrate.ErrNotFound, identity)
}

func (d *fakeDataset) KindByPlural(_ context.Context, authority, plural string) (substrate.KindInfo, error) {
	for _, t := range d.types {
		if t.Authority == authority && t.Plural == plural {
			return t, nil
		}
	}
	return substrate.KindInfo{}, fmt.Errorf("%w: collection %s/%s", substrate.ErrNotFound, authority, plural)
}

func (d *fakeDataset) put(e *substrate.Record) {
	d.records[e.ID] = e
	d.changes = append(d.changes, substrate.Change{
		Seq: int64(len(d.changes) + 1), TS: time.Unix(int64(len(d.changes)+1), 0).UTC(),
		Actor: substrate.ActorAPI, Op: substrate.OpPut, RecordID: e.ID, Kind: e.Kind,
	})
}

func (d *fakeDataset) Put(ctx context.Context, actor substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail("Put"); err != nil {
		return nil, err
	}
	d.lastPut, d.lastActor = in, actor
	d.lastPrincipal = substrate.PrincipalFrom(ctx)
	id := in.ID
	if id == "" {
		id = fmt.Sprintf("ent%d", len(d.records)+1)
	}
	// Faithful upsert versioning: a create lands at version 1, a
	// re-put of an existing id bumps past it, so the HTTP layer's create=201 /
	// update=200 decision is exercisable here.
	version := int64(1)
	if existing, ok := d.records[id]; ok {
		version = existing.Version + 1
	}
	e := &substrate.Record{
		ID: id, Kind: in.Kind, Properties: in.Properties, Labels: in.Labels,
		Version: version, CreatedAt: time.Unix(0, 0).UTC(), UpdatedAt: time.Unix(0, 0).UTC(),
	}
	if e.Properties == nil {
		e.Properties = map[string]any{}
	}
	// Everything authored is a property: title arrives in the map like the
	// rest.
	if title, ok := e.Properties[substrate.PropTitle].(string); ok {
		e.Title = title
	}
	if e.Labels == nil {
		e.Labels = map[string]any{}
	}
	d.put(e)
	return e, nil
}

func (d *fakeDataset) Patch(_ context.Context, actor substrate.Actor, typ, id string, in substrate.PatchInput) (*substrate.Record, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail("Patch"); err != nil {
		return nil, err
	}
	d.lastPatch, d.lastActor = in, actor
	e, ok := d.records[id]
	if !ok || (typ != "" && e.Kind != typ) {
		return nil, fmt.Errorf("%w: %s", substrate.ErrNotFound, id)
	}
	if title, ok := in.Properties[substrate.PropTitle].(string); ok {
		e.Title = title
	}
	for k, v := range in.Properties {
		e.Properties[k] = v
	}
	e.Version++
	return e, nil
}

func (d *fakeDataset) Delete(_ context.Context, _ substrate.Actor, typ, id string) (*substrate.Record, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastDeleteType, d.lastDeleteID = typ, id
	if err := d.fail("Delete"); err != nil {
		return nil, err
	}
	e, ok := d.records[id]
	if !ok || (typ != "" && e.Kind != typ) {
		return nil, fmt.Errorf("%w: %s", substrate.ErrNotFound, id)
	}
	now := time.Unix(10, 0).UTC()
	e.DeletedAt = &now
	return e, nil
}

func (d *fakeDataset) Link(_ context.Context, actor substrate.Actor, _, src, rel string, to substrate.EdgeRef, props map[string]any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail("Link"); err != nil {
		return err
	}
	d.lastActor = actor
	e, ok := d.records[src]
	if !ok {
		return fmt.Errorf("%w: %s", substrate.ErrNotFound, src)
	}
	if e.Edges == nil {
		e.Edges = map[string][]substrate.EdgeTarget{}
	}
	e.Edges[rel] = append(e.Edges[rel], substrate.EdgeTarget{ID: to.ID, Kind: to.Identity(), Properties: props})
	return nil
}

func (d *fakeDataset) Unlink(_ context.Context, actor substrate.Actor, _, src, rel string, to substrate.EdgeRef) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail("Unlink"); err != nil {
		return err
	}
	d.lastActor = actor
	e, ok := d.records[src]
	if !ok {
		return fmt.Errorf("%w: %s", substrate.ErrNotFound, src)
	}
	var kept []substrate.EdgeTarget
	for _, t := range e.Edges[rel] {
		if t.ID != to.ID {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(e.Edges, rel)
	} else {
		e.Edges[rel] = kept
	}
	return nil
}

func (d *fakeDataset) Merge(_ context.Context, _ substrate.Actor, typ, winner, loser string) (*substrate.Record, error) {
	if err := d.fail("Merge"); err != nil {
		return nil, err
	}
	// The record names both sides with EDGES (MODEL §11.5).
	return &substrate.Record{
		ID: "merge1", Kind: coreAuthority + "/recordmerge",
		Edges: map[string][]substrate.EdgeTarget{
			"winner": {{ID: winner}},
			"loser":  {{ID: loser}},
		},
	}, nil
}

func (d *fakeDataset) Split(_ context.Context, _ substrate.Actor, mergeID string) (*substrate.Record, error) {
	if err := d.fail("Split"); err != nil {
		return nil, err
	}
	return &substrate.Record{
		ID: "split1", Kind: coreAuthority + "/recordsplit",
		Edges: map[string][]substrate.EdgeTarget{"merge": {{ID: mergeID}}},
	}, nil
}

func (d *fakeDataset) Get(_ context.Context, typ, id string) (*substrate.Record, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail("Get"); err != nil {
		return nil, err
	}
	e, ok := d.records[id]
	if !ok || (typ != "" && e.Kind != typ) {
		return nil, fmt.Errorf("%w: %s", substrate.ErrNotFound, id)
	}
	// The engine answers a former id with the canonical record and says so
	// (MODEL §4.1); the fake reproduces the shape the HTTP layer must carry.
	if canonical, moved := d.formers[id]; moved {
		e, ok = d.records[canonical]
		if !ok {
			return nil, fmt.Errorf("%w: %s", substrate.ErrNotFound, canonical)
		}
		clone := *e
		clone.CanonicalID = canonical
		clone.PropertyMeta = d.meta[canonical]
		return &clone, nil
	}
	if m, mok := d.meta[id]; mok {
		clone := *e
		clone.PropertyMeta = m
		return &clone, nil
	}
	return e, nil
}

func (d *fakeDataset) Incoming(_ context.Context, typ, id string, opts substrate.IncomingOptions) (*substrate.IncomingPage, error) {
	_ = typ
	d.mu.Lock()
	defer d.mu.Unlock()
	if canonical, moved := d.formers[id]; moved {
		id = canonical
	}
	rows := d.incoming[id]
	// The narrowings a drill-down sends, honored so a handler test can assert
	// that one group's expansion asks for that group alone.
	if opts.Rel != "" || opts.FromKind != "" {
		var kept []substrate.IncomingEdge
		for _, row := range rows {
			if opts.Rel != "" && row.Rel != opts.Rel {
				continue
			}
			if opts.FromKind != "" && row.From.Kind != opts.FromKind {
				continue
			}
			kept = append(kept, row)
		}
		rows = kept
	}
	first := opts.First
	if first <= 0 {
		first = 50
	}
	start := 0
	if opts.After != "" {
		_, _ = fmt.Sscanf(opts.After, "offset:%d", &start)
	}
	start = min(start, len(rows))
	end := min(start+first, len(rows))
	page := &substrate.IncomingPage{
		Incoming: append([]substrate.IncomingEdge(nil), rows[start:end]...),
		Total:    len(rows),
	}
	if end < len(rows) {
		page.Cursor = fmt.Sprintf("offset:%d", end)
	}
	return page, nil
}

func (d *fakeDataset) List(_ context.Context, q substrate.Query) (*substrate.Page, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail("List"); err != nil {
		return nil, err
	}
	d.lastQuery = q
	var out []*substrate.Record
	for _, id := range sortedRecordIDs(d.records) {
		e := d.records[id]
		if len(q.Filter.Kinds) > 0 && !containsString(q.Filter.Kinds, e.Kind) {
			continue
		}
		out = append(out, e)
	}
	return &substrate.Page{Records: out, Cursor: ""}, nil
}

func (d *fakeDataset) Search(_ context.Context, in substrate.SearchInput) ([]substrate.Hit, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastSearch = in
	if err := d.fail("Search"); err != nil {
		return nil, err
	}
	var hits []substrate.Hit
	for _, id := range sortedRecordIDs(d.records) {
		e := d.records[id]
		if in.Q == "" || strings.Contains(strings.ToLower(e.Title), strings.ToLower(in.Q)) {
			hits = append(hits, substrate.Hit{Record: e, Lexical: 0.5})
		}
	}
	return hits, nil
}

func (d *fakeDataset) Changes(_ context.Context, after int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail("Changes"); err != nil {
		return nil, err
	}
	var out []substrate.Change
	for _, c := range d.changes {
		if c.Seq <= after || !matchesChange(c, f) {
			continue
		}
		out = append(out, c)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// matchesChange mirrors the engine's ChangeFilter semantics — Q included, a
// case-insensitive substring over type, actor, record id and payload text.
func matchesChange(c substrate.Change, f substrate.ChangeFilter) bool {
	if len(f.Kinds) > 0 && !containsString(f.Kinds, c.Kind) {
		return false
	}
	if containsString(f.ExcludeKinds, c.Kind) {
		return false
	}
	actors := make([]string, 0, len(f.Actors))
	for _, actor := range f.Actors {
		actors = append(actors, string(actor))
	}
	if len(actors) > 0 && !containsString(actors, string(c.Actor)) {
		return false
	}
	excludedActors := make([]string, 0, len(f.ExcludeActors))
	for _, actor := range f.ExcludeActors {
		excludedActors = append(excludedActors, string(actor))
	}
	if containsString(excludedActors, string(c.Actor)) {
		return false
	}
	ops := make([]string, 0, len(f.Ops))
	for _, op := range f.Ops {
		ops = append(ops, string(op))
	}
	if len(ops) > 0 && !containsString(ops, string(c.Op)) {
		return false
	}
	excludedOps := make([]string, 0, len(f.ExcludeOps))
	for _, op := range f.ExcludeOps {
		excludedOps = append(excludedOps, string(op))
	}
	if containsString(excludedOps, string(c.Op)) {
		return false
	}
	if f.RecordID != "" && f.RecordID != c.RecordID {
		return false
	}
	if f.Q != "" {
		payload, _ := json.Marshal(c.Payload)
		hay := strings.ToLower(c.Kind + " " + string(c.Actor) + " " + c.RecordID + " " + string(payload))
		if !strings.Contains(hay, strings.ToLower(f.Q)) {
			return false
		}
	}
	return true
}

// ChangesBefore and ChangeTriggers are the change-feed seam (api/changes.go)
// the handler asserts at runtime; the fake answers newest-first pages over
// its slice and the canned per-seq states.
func (d *fakeDataset) ChangesBefore(_ context.Context, before int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail("ChangesBefore"); err != nil {
		return nil, err
	}
	var out []substrate.Change
	for i := len(d.changes) - 1; i >= 0; i-- {
		c := d.changes[i]
		if (before > 0 && c.Seq >= before) || !matchesChange(c, f) {
			continue
		}
		out = append(out, c)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (d *fakeDataset) ChangeTriggers(_ context.Context, changes []substrate.Change) (map[int64][]substrate.ChangeTrigger, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail("ChangeTriggers"); err != nil {
		return nil, err
	}
	out := map[int64][]substrate.ChangeTrigger{}
	for _, c := range changes {
		if states, ok := d.trStates[c.Seq]; ok {
			out[c.Seq] = states
		}
	}
	return out, nil
}

func (d *fakeDataset) WatchSignal(ctx context.Context) <-chan int64 {
	out := make(chan int64, 8)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case v, ok := <-d.signals:
				if !ok {
					return
				}
				select {
				case out <- v:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

// commit appends a change and wakes watchers.
func (d *fakeDataset) commit(c substrate.Change) {
	d.mu.Lock()
	c.Seq = int64(len(d.changes) + 1)
	d.changes = append(d.changes, c)
	seq := c.Seq
	d.mu.Unlock()
	d.signals <- seq
}

func (d *fakeDataset) MintToken(_ context.Context, label string, expiresAt *time.Time) (substrate.TokenInfo, string, error) {
	if err := d.fail("MintToken"); err != nil {
		return substrate.TokenInfo{}, "", err
	}
	return substrate.TokenInfo{
		ID: "minted", Label: label, Created: time.Unix(0, 0).UTC(), ExpiresAt: expiresAt,
	}, "substrate_tok_minted", nil
}

func (d *fakeDataset) Tokens(context.Context) ([]substrate.TokenInfo, error) {
	if err := d.fail("Tokens"); err != nil {
		return nil, err
	}
	return []substrate.TokenInfo{
		{ID: "tok1", Label: "console", Created: time.Unix(0, 0).UTC()},
	}, nil
}

func (d *fakeDataset) RunGC(context.Context) (int, error) { return 0, nil }

func (d *fakeDataset) ProcessEmbedQueue(context.Context, substrate.Embedder, int) (int, error) {
	return 0, nil
}

var _ substrate.Service = (*fakeService)(nil)

var _ substrate.Dataset = (*fakeDataset)(nil)

func containsString(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func sortedRecordIDs(m map[string]*substrate.Record) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var errBoom = errors.New("boom")
