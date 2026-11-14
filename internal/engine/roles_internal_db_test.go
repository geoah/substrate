package engine

// The one check (decision 0276) over every type of actor and every class of
// control kind, each cell a write through the path that hand's writes take
// in the engine: a person's and an app's the dataset write the record routes
// call, a function's an effect the dispatcher applies, an agent's the write
// built-in's bounded write, a package's and the system's their own hand's
// write. The persons hold the three shipped roles.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// rolesRepository is a repository whose first person, ada, is its admin, with
// bo an editor, cy a viewer and dee a viewer whose sign-in the table's
// sign-in column revokes.
type rolesRepository struct {
	r                   *restartable
	ds                  *dataset
	repo                string
	ada, bo, cy, dee    string
	adaID, boID, cyID   string
	deeToken, deeSecret string
}

func newRolesRepository(t *testing.T) *rolesRepository {
	t.Helper()
	ctx := context.Background()
	r := openRestartable(t)
	label := testdb.RepositoryLabel(t)
	rr := &rolesRepository{r: r, ada: label + "a.example.com", bo: label + "b.example.com", cy: label + "c.example.com", dee: label + "d.example.com"}
	for _, name := range []string{rr.ada, rr.bo, rr.cy, rr.dee} {
		r.register(name)
	}
	for name, role := range map[string]string{rr.bo: scope.RoleEditor, rr.cy: scope.RoleViewer, rr.dee: scope.RoleViewer} {
		if err := r.s.AddPerson(ctx, name, rr.ada, role); err != nil {
			t.Fatalf("add %s as %s: %v", name, role, err)
		}
	}
	d, err := r.s.Dataset(ctx, rr.ada)
	if err != nil {
		t.Fatal(err)
	}
	rr.ds = d.(*dataset)
	rr.repo = rr.ds.info.ID
	id := func(name string) string {
		t.Helper()
		row, err := r.s.identityByName(ctx, r.s.maint, name)
		if err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	rr.adaID, rr.boID, rr.cyID = id(rr.ada), id(rr.bo), id(rr.cy)
	minted, err := r.signIn(rr.dee, rr.ada)
	if err != nil {
		t.Fatal(err)
	}
	rr.deeToken, rr.deeSecret = minted.Token.ID, minted.Secret
	return rr
}

// as is ctx acting as the person named, as a request of their token is.
func (rr *rolesRepository) as(identity string) context.Context {
	return substrate.WithIdentity(context.Background(), identity)
}

// controlColumns are the table's columns: every class of control kind, and
// sign-in material.
func controlColumns() []string {
	out := make([]string, 0, len(vocabulary.ControlClasses)+1)
	for _, c := range vocabulary.ControlClasses {
		out = append(out, string(c))
	}
	return append(out, vocabulary.ControlSignIn)
}

// columnWrite is each column's write: a record of one kind of the class, or
// for sign-in material the revoke of another person's token.
type columnWrite struct {
	kind  string
	props map[string]any
	id    string
}

func (rr *rolesRepository) columnWrites() map[string]columnWrite {
	return map[string]columnWrite{
		string(vocabulary.ControlTriggers): {kind: typeTrigger, props: map[string]any{
			"source": map[string]any{"webhook": map[string]any{}}, "callable": vocabulary.RecordPath(kindFunction, "x.example.com/p/f"),
		}},
		string(vocabulary.ControlPolicies): {kind: vocabulary.KindRecordPatchPolicy, props: map[string]any{
			"selector": map[string]any{"kinds": []any{"x.example.com/p/k"}}, "action": "refuse",
		}},
		string(vocabulary.ControlProviders): {kind: typeProvider, props: map[string]any{"wire": "openai"}},
		string(vocabulary.ControlSettings):  {kind: kindSetting, id: timezoneSettingID, props: map[string]any{"value": "Europe/Athens"}},
		string(vocabulary.ControlApplets): {kind: vocabulary.PackageApplets + "/applet", props: map[string]any{
			"name": "a screen", "runtime": "a2ui",
		}},
		string(vocabulary.ControlDeclarations): {kind: kindKind, props: map[string]any{}},
		string(vocabulary.ControlActors):       {kind: kindActor, props: map[string]any{"type": "person", "name": "nobody"}},
		string(vocabulary.ControlRoles):        {kind: kindRole, props: map[string]any{"name": "a role of the table's"}},
		vocabulary.ControlSignIn:               {kind: kindToken, id: rr.deeToken},
	}
}

// tableRow is one hand: its actor type, and how it writes a column.
type tableRow struct {
	name   string
	typ    string
	writes string // one Y or N per column, in controlColumns order
	write  func(t *testing.T, w columnWrite, n int) error
}

func (rr *rolesRepository) rows() []tableRow {
	ds := rr.ds
	person := func(identity string) func(*testing.T, columnWrite, int) error {
		return func(_ *testing.T, w columnWrite, n int) error {
			ctx := rr.as(identity)
			if w.kind == kindToken {
				_, err := ds.Delete(ctx, substrate.ActorPerson, w.kind, w.id, substrate.DeleteInput{})
				return err
			}
			_, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: w.kind, ID: rowID(w, n), Properties: w.props})
			return err
		}
	}
	app := func(identity string, scopes []string) func(*testing.T, columnWrite, int) error {
		return func(_ *testing.T, w columnWrite, n int) error {
			ctx := substrate.WithOnBehalfOf(rr.as(identity), substrate.ActorPerson)
			if scopes != nil {
				ctx = substrate.WithScopes(ctx, scopes)
			}
			actor := substrate.Actor("app:notes.example.org")
			if w.kind == kindToken {
				_, err := ds.Delete(ctx, actor, w.kind, w.id, substrate.DeleteInput{})
				return err
			}
			_, err := ds.Put(ctx, actor, substrate.PutInput{Kind: w.kind, ID: rowID(w, n), Properties: w.props})
			return err
		}
	}
	// code applies the run's effects as the dispatcher does, its
	// declaration's writes carried, acting for behalf ("" for a trigger's).
	function := func(behalf substrate.Actor, writes func(kind string) []string) func(*testing.T, columnWrite, int) error {
		return func(_ *testing.T, w columnWrite, n int) error {
			ctx := substrate.WithOnBehalfOf(context.Background(), behalf)
			emit := writes(w.kind)
			ef := effect{Action: effectPut, Type: w.kind, ID: rowID(w, n), Properties: w.props}
			if w.kind == kindToken {
				ef = effect{Action: effectDelete, Type: w.kind, ID: w.id}
			}
			return ds.inTx(ctx, substrate.FunctionActor("x.example.com", "p", "f"), false, func(tx *txn) error {
				tx.setEffectEmit(emit)
				return tx.applyEffects(emit, []effect{ef})
			})
		}
	}
	agent := func(behalf substrate.Actor, writes func(kind string) []string) func(*testing.T, columnWrite, int) error {
		return func(_ *testing.T, w columnWrite, n int) error {
			ctx := substrate.WithOnBehalfOf(context.Background(), behalf)
			ceiling := &effectCeiling{emit: writes(w.kind)}
			actor := substrate.AgentActor("x.example.com", "p", "a")
			if w.kind == kindToken {
				_, err := ds.deleteBounded(ctx, actor, w.kind, w.id, substrate.DeleteInput{}, ceiling)
				return err
			}
			_, err := ds.putBounded(ctx, actor, substrate.PutInput{Kind: w.kind, ID: rowID(w, n), Properties: w.props}, ceiling)
			return err
		}
	}
	plain := func(actor substrate.Actor) func(*testing.T, columnWrite, int) error {
		return func(_ *testing.T, w columnWrite, n int) error {
			if w.kind == kindToken {
				_, err := ds.Delete(context.Background(), actor, w.kind, w.id, substrate.DeleteInput{})
				return err
			}
			_, err := ds.Put(context.Background(), actor, substrate.PutInput{Kind: w.kind, ID: rowID(w, n), Properties: w.props})
			return err
		}
	}
	naming := func(kind string) []string { return []string{kind} }
	glob := func(string) []string { return []string{"*"} }
	bo := substrate.PersonActor(rr.boID)
	cy := substrate.PersonActor(rr.cyID)
	// Installed code never writes a policy, whatever it names: a fixed rule
	// beside the one check (write.go refuseInstalledPolicyWrite).
	//                      triggers policies providers settings applets declarations actors roles sign-in
	return []tableRow{
		{name: "an admin", typ: substrate.ActorTypePerson, writes: "YYYYYYNNY", write: person(rr.adaID)},
		{name: "an editor", typ: substrate.ActorTypePerson, writes: "NNNNNNNNN", write: person(rr.boID)},
		{name: "a viewer", typ: substrate.ActorTypePerson, writes: "NNNNNNNNN", write: person(rr.cyID)},
		{name: "an app's token for an admin, writing everything", typ: substrate.ActorTypeApp, writes: "NNNNNNNNN", write: app(rr.adaID, []string{"write:*"})},
		{name: "an assistant for an admin", typ: substrate.ActorTypeApp, writes: "YYYYYYNNN", write: app(rr.adaID, nil)},
		{name: "an assistant for an editor", typ: substrate.ActorTypeApp, writes: "NNNNNNNNN", write: app(rr.boID, nil)},
		{name: "a function a trigger runs, naming the kind", typ: substrate.ActorTypeFunction, writes: "YNYYYNNNN", write: function("", naming)},
		{name: "a function a trigger runs, by a glob", typ: substrate.ActorTypeFunction, writes: "NNNNNNNNN", write: function("", glob)},
		{name: "a function an admin runs, naming the kind", typ: substrate.ActorTypeFunction, writes: "YNYYYNNNN", write: function(substrate.ActorPerson, naming)},
		{name: "a function an editor runs, naming the kind", typ: substrate.ActorTypeFunction, writes: "NNNNNNNNN", write: function(bo, naming)},
		{name: "an agent a trigger runs, naming the kind", typ: substrate.ActorTypeAgent, writes: "YNYYYYNNN", write: agent("", naming)},
		{name: "an agent a trigger runs, by a glob", typ: substrate.ActorTypeAgent, writes: "NNNNNNNNN", write: agent("", glob)},
		{name: "an agent an admin starts, naming the kind", typ: substrate.ActorTypeAgent, writes: "YNYYYYNNN", write: agent(substrate.ActorPerson, naming)},
		{name: "an agent an editor starts, naming the kind", typ: substrate.ActorTypeAgent, writes: "NNNNNNNNN", write: agent(bo, naming)},
		{name: "an agent a viewer starts, naming the kind", typ: substrate.ActorTypeAgent, writes: "NNNNNNNNN", write: agent(cy, naming)},
		{name: "a package's own hand", typ: substrate.ActorTypePackage, writes: "YNYYYYNNN", write: plain(substrate.BundleActor("x.example.com", "p"))},
		{name: "the system", typ: substrate.ActorTypeSystem, writes: "YYYYYYNNY", write: plain(substrate.ActorSystem)},
	}
}

// substrateManaged reports a refusal of a kind only the engine writes as a
// record (an actor, a role, a declaration through an effect): the guard on
// system kinds, which stands before the one check.
func substrateManaged(err error) bool {
	return err != nil && strings.Contains(err.Error(), "managed by the substrate")
}

func rowID(w columnWrite, n int) string {
	if w.id != "" {
		return w.id
	}
	return fmt.Sprintf("table-%d", n)
}

// TestEveryActorTypeAgainstEveryControlKind is the table of actor types
// against control kinds: every type has rows, every class a column, and each
// cell is what the one check answers for that hand's write. A type or a
// class without its place in the table fails it. Actors and roles are no
// hand's to write but the engine's own (the fixed check), and a declaration
// passes the one check where Y says so and is then the loader's.
func TestEveryActorTypeAgainstEveryControlKind(t *testing.T) {
	t.Parallel()
	rr := newRolesRepository(t)
	columns := controlColumns()
	writes := rr.columnWrites()
	for _, c := range columns {
		if _, ok := writes[c]; !ok {
			t.Errorf("the class %s has no column: add a write of one of its kinds", c)
		}
	}
	rows := rr.rows()
	for _, typ := range substrate.ActorTypes {
		if !slices.ContainsFunc(rows, func(r tableRow) bool { return r.typ == typ }) {
			t.Errorf("the actor type %s has no row", typ)
		}
	}
	n := 0
	for _, row := range rows {
		if len(row.writes) != len(columns) {
			t.Fatalf("%s: %d cells for %d columns", row.name, len(row.writes), len(columns))
		}
		for i, c := range columns {
			n++
			// The sign-in column revokes dee's one token: a fresh one per cell.
			w := writes[c]
			if c == vocabulary.ControlSignIn {
				minted, err := rr.r.signIn(rr.dee, rr.ada)
				if err != nil {
					t.Fatal(err)
				}
				w.id = minted.Token.ID
			}
			err := row.write(t, w, n)
			allowed := row.writes[i] == 'Y'
			switch refused := errors.Is(err, substrate.ErrForbidden); {
			case allowed && refused:
				t.Errorf("%s writing %s: refused (%v), want it to pass the check", row.name, c, err)
			case !allowed && !refused:
				t.Errorf("%s writing %s: %v, want it refused", row.name, c, err)
			case !allowed && !errors.Is(err, substrate.ErrNotPermitted) && !substrateManaged(err) &&
				!strings.Contains(err.Error(), "an app's token does not revoke") &&
				!strings.Contains(err.Error(), "installed code never writes the door's own rules"):
				t.Errorf("%s writing %s: refused, but neither by a rule nor as a kind only the engine writes: %v", row.name, c, err)
			}
		}
	}
}

// The four checks that were the owner's alone are one question now, may this
// hand write this control kind, which the one check answers: each refuses an
// editor and admits an admin, because the admin role reaches every control
// kind and the editor's rule reaches none. An adjusted change request's
// control kind is its target's.
func TestTheFourOwnersChecksAreTheOneCheck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newRolesRepository(t)
	ds := rr.ds
	const pkg = "acct.test.dev/acct"
	accountKind, itemKind := pkg+"/account", pkg+"/item"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "account"}, map[string]any{
			"traits": []any{vocabulary.TraitAccountConfigCore},
			"properties": map[string]any{
				"tokenRef":      map[string]any{"type": "secret", "writer": "system"},
				"tokenStatus":   map[string]any{"type": "string", "writer": "system"},
				"grantedScopes": map[string]any{"type": "string", "repeated": true, "writer": "system"},
				"address":       map[string]any{"type": "email", "writer": "person"},
				"enabledMail":   map[string]any{"type": "bool", "writer": "person"},
			},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "item"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatalf("declare an account kind: %v", err)
	}
	editor, admin := rr.as(rr.boID), rr.as(rr.adaID)
	refused := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, substrate.ErrNotPermitted) {
			t.Errorf("an editor %s: %v, want refused by a rule", what, err)
		}
	}
	admitted := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Errorf("an admin %s: %v", what, err)
		}
	}
	account := map[string]any{"address": "ada@example.com", "enabledMail": true}

	// Creating a connection.
	_, err := ds.Put(editor, substrate.ActorPerson, substrate.PutInput{Kind: accountKind, Properties: account})
	refused("creates a connection", err)
	connection, err := ds.Put(admin, substrate.ActorPerson, substrate.PutInput{Kind: accountKind, Properties: account})
	admitted("creates a connection", err)
	if connection == nil {
		t.FailNow()
	}

	// Deleting one.
	_, err = ds.Delete(editor, substrate.ActorPerson, accountKind, connection.ID, substrate.DeleteInput{})
	refused("deletes a connection", err)
	_, err = ds.Delete(admin, substrate.ActorPerson, accountKind, connection.ID, substrate.DeleteInput{})
	admitted("deletes a connection", err)

	// Revoking another person's token, where a person's own is theirs.
	_, err = ds.Delete(editor, substrate.ActorPerson, kindToken, rr.deeToken, substrate.DeleteInput{})
	refused("revokes another person's token", err)
	own, err := rr.r.signIn(rr.bo, rr.ada)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ds.Delete(editor, substrate.ActorPerson, kindToken, own.Token.ID, substrate.DeleteInput{})
	if err != nil {
		t.Errorf("an editor revoking their own token: %v", err)
	}
	_, err = ds.Delete(admin, substrate.ActorPerson, kindToken, rr.deeToken, substrate.DeleteInput{})
	admitted("revokes another person's token", err)

	// Adjusting a change request: the adjusted values are a write of its
	// target by the hand that decides (a request is no control kind), so an
	// editor is refused one whose target is a control kind and the admin is
	// not, and an editor adjusts one whose target they may write.
	item, err := ds.Put(admin, substrate.ActorPerson, substrate.PutInput{Kind: itemKind, Properties: map[string]any{"name": "draft"}})
	if err != nil {
		t.Fatal(err)
	}
	another, err := ds.Put(admin, substrate.ActorPerson, substrate.PutInput{Kind: accountKind, Properties: account})
	if err != nil {
		t.Fatal(err)
	}
	request := func(target string, diff map[string]any) *substrate.Record {
		t.Helper()
		var req *substrate.Record
		if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			var err error
			req, err = tx.put(substrate.PutInput{Kind: vocabulary.KindRecordPatchRequest, Properties: map[string]any{
				"target": target, "diff": map[string]any{"properties": diff},
			}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return req
	}
	adjust := func(ctx context.Context, req *substrate.Record, adjusted map[string]any) error {
		v := req.Version
		_, err := ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
			Properties: map[string]any{"decision": "accepted", "adjustedDiff": map[string]any{"properties": adjusted}},
			IfVersion:  &v,
		})
		return err
	}
	onConnection := request(vocabulary.RecordPath(accountKind, another.ID), map[string]any{"enabledMail": false})
	// An accept whose write is refused is answered as the accept's conflict,
	// and the request stays proposed for a hand that may decide it.
	var conflict *substrate.AcceptConflictError
	if err := adjust(editor, onConnection, map[string]any{"address": "bo@example.com"}); !errors.As(err, &conflict) ||
		!strings.Contains(conflict.Reason, "no rule of the roles they hold covers it") {
		t.Errorf("an editor adjusts a change request to a connection: %v, want the accept refused by a rule", err)
	}
	admitted("adjusts a change request to a connection", adjust(admin, onConnection, map[string]any{"address": "ada@example.org"}))
	if got, err := ds.Get(ctx, accountKind, another.ID); err != nil || got.Properties["address"] != "ada@example.org" {
		t.Fatalf("the connection after the admin's adjusted accept: %v (%v)", got, err)
	}
	onItem := request(vocabulary.RecordPath(itemKind, item.ID), map[string]any{"name": "proposed"})
	if err := adjust(editor, onItem, map[string]any{"name": "adjusted"}); err != nil {
		t.Errorf("an editor adjusting a change request to a record they may write: %v", err)
	}
	if got, err := ds.Get(ctx, itemKind, item.ID); err != nil || got.Properties["name"] != "adjusted" {
		t.Fatalf("the item after the editor's adjusted accept: %v (%v)", got, err)
	}
}

// The last admin keeps the role: demoting them is refused until another
// person holds it.
func TestTheLastAdminKeepsTheRole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newRolesRepository(t)
	if err := rr.r.s.SetPersonRoles(ctx, rr.ada, rr.ada, scope.RoleEditor); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("demoting the last admin = %v, want ErrConflict", err)
	}
	if err := rr.r.s.SetPersonRoles(ctx, rr.bo, rr.ada, scope.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := rr.r.s.SetPersonRoles(ctx, rr.ada, rr.ada, scope.RoleEditor); err != nil {
		t.Fatalf("demoting an admin beside another: %v", err)
	}
	if err := rr.r.s.SetPersonRoles(ctx, rr.bo, rr.ada, scope.RoleViewer); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("demoting the admin left = %v, want ErrConflict", err)
	}
	if err := rr.r.s.SetPersonRoles(ctx, rr.cy, rr.ada, "no-such-role"); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a role the repository does not hold = %v, want ErrValidation", err)
	}
	if err := rr.r.s.SetPersonRoles(ctx, rr.cy, rr.ada); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("taking every role from a person = %v, want ErrValidation", err)
	}
}

// A person from before roles is an admin from the first request after the
// upgrade: repository migration 13 gives every live person whose actor names
// no role `admin`, at the repository's first open under a release that
// carries it.
func TestAPersonFromBeforeRolesIsAnAdminAtTheFirstRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openRestartable(t)
	ada := testdb.RepositoryLabel(t) + "a.example.com"
	r.register(ada)
	minted, err := r.signIn(ada, ada)
	if err != nil {
		t.Fatal(err)
	}
	admin := func() bool {
		t.Helper()
		d, err := r.s.Dataset(ctx, ada)
		if err != nil {
			t.Fatal(err)
		}
		held, err := d.PersonRoles(substrate.WithIdentity(ctx, minted.Token.Identity))
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(held, func(r substrate.RoleRules) bool { return r.ID == scope.RoleAdmin })
	}
	// The repository as a release before roles left it: the person's actor
	// names no role, and the ledger has no row for the migration.
	d, err := r.s.Dataset(ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		row, err := tx.loadRow(eref{Kind: kindActor, ID: actorID(substrate.ActorPerson)}, true)
		if err != nil {
			return err
		}
		if row == nil {
			return errors.New("the person has no actor")
		}
		delete(row.Props, "roles")
		return tx.migrateRow(eref{Kind: kindActor, ID: row.ID}, row.Props, false)
	}); err != nil {
		t.Fatal(err)
	}
	MakeMigrationPending(t, r.dsn, ds.info.ID, 13)
	if admin() {
		t.Fatal("the person still holds admin with their roles taken off: the test proves nothing")
	}
	r.restart()
	if !admin() {
		t.Fatal("the person from before roles is no admin at the first request after the upgrade")
	}
}

// An agent a viewer starts writes nothing: its write tool's put of a kind
// its declaration names is refused by the viewer's rules, and the record
// never lands, and so is its proposal; the same agents an admin starts write
// and propose.
func TestAnAgentAViewerStartsWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newRolesRepository(t)
	ds := rr.ds
	importVocabulary(t, ds, "tasks")
	fake := provisionAgents(t, ds)
	widget := crewPackage + "/widget"
	for _, run := range []struct {
		who    substrate.Actor
		id     string
		lands  bool
		person string
	}{
		{substrate.PersonActor(rr.cyID), fid("w-viewer"), false, rr.cyID},
		{substrate.ActorPerson, fid("w-admin"), true, rr.adaID},
	} {
		fake.script("edit",
			fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", widget, run.id, map[string]any{"name": "wanted"})}}},
			fakeTurn{content: "done."},
		)
		started := substrate.WithOnBehalfOf(substrate.WithIdentity(ctx, run.person), run.who)
		if _, err := ds.CallAgent(started, crewPackage+"/editor", "make a widget"); err != nil {
			t.Fatalf("call the agent for %s: %v", run.who, err)
		}
		_, err := ds.Get(ctx, widget, run.id)
		if landed := err == nil; landed != run.lands {
			t.Fatalf("the widget the agent %s started wrote: landed %v, want %v", run.who, landed, run.lands)
		}
	}
	// Nor does it propose: a change request is a write like any other, held
	// to the rules of the person the run acts for.
	requests := func() int {
		t.Helper()
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.KindRecordPatchRequest}}, First: 100})
		if err != nil {
			t.Fatal(err)
		}
		return len(page.Records)
	}
	for _, run := range []struct {
		who      substrate.Actor
		person   string
		proposed int
	}{
		{substrate.PersonActor(rr.cyID), rr.cyID, 0},
		{substrate.ActorPerson, rr.adaID, 1},
	} {
		before := requests()
		fake.script("stoic",
			fakeTurn{calls: []fakeCall{{"propose", `{"op":"create","kind":"` + widget + `","diff":{"properties":{"name":"proposed"}}}`}}},
			fakeTurn{content: "proposed."},
		)
		started := substrate.WithOnBehalfOf(substrate.WithIdentity(ctx, run.person), run.who)
		if _, err := ds.CallAgent(started, crewPackage+"/stoic", "propose a widget"); err != nil {
			t.Fatalf("call the proposing agent for %s: %v", run.who, err)
		}
		if got := requests() - before; got != run.proposed {
			t.Fatalf("the agent %s started proposed %d change requests, want %d", run.who, got, run.proposed)
		}
	}
}

// The architect a person starts declares nothing that person may not: its
// vocabulary tool, whose apply is the vocabulary door's own transaction, is
// refused for an editor, whose rules do not name a declaration kind, and
// nothing lands; the same call the admin starts lands.
func TestTheArchitectAnEditorStartsDeclaresNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newRolesRepository(t)
	ds := rr.ds
	fake := newFakeLLM(t)
	ds.mu.Lock()
	ds.skipThreadTitles = true
	ds.mu.Unlock()
	if _, err := ds.Put(rr.as(rr.adaID), substrate.ActorPerson, substrate.PutInput{
		Kind: typeProvider, ID: "anthropic",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-anthropic"},
	}); err != nil {
		t.Fatal(err)
	}
	plant := ds.Repository().Authority + "/garden/plant"
	for _, run := range []struct {
		who    substrate.Actor
		person string
		lands  bool
	}{
		{substrate.PersonActor(rr.boID), rr.boID, false},
		{substrate.ActorPerson, rr.adaID, true},
	} {
		fake.script(mossModel,
			fakeTurn{calls: []fakeCall{{"vocabulary", toolArgs(t, map[string]any{"documents": gardenDocs(ds.Repository().Authority)})}}},
			fakeTurn{content: "done."},
		)
		started := substrate.WithOnBehalfOf(substrate.WithIdentity(ctx, run.person), run.who)
		res, err := ds.CallAgent(started, architectID, "declare a garden")
		if err != nil {
			t.Fatalf("call the architect for %s: %v", run.who, err)
		}
		rows := toolRows(t, ds, res.Thread)
		if len(rows) != 1 {
			t.Fatalf("the architect for %s: %d tool rows, want 1", run.who, len(rows))
		}
		content, _ := rows[0]["content"].(string)
		if ok := rows[0]["ok"] == true; ok != run.lands || (!run.lands && !strings.Contains(content, "no rule of the roles they hold")) {
			t.Fatalf("the architect for %s: ok %v, %s; want ok %v", run.who, rows[0]["ok"], content, run.lands)
		}
		if _, err := ds.resolveType(plant); (err == nil) != run.lands {
			t.Fatalf("the plant kind after the architect for %s: %v, want landed %v", run.who, err, run.lands)
		}
	}
}

// `admin` is held by being named, not by its record's rules: with the record
// gone (as under an upgrade withheld, which writes none) an admin is still
// one, and a role whose record is gone holds nothing.
func TestAnAdminIsOneWithoutTheRoleRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newRolesRepository(t)
	ds := rr.ds
	for _, id := range []string{scope.RoleAdmin, scope.RoleEditor} {
		if _, err := ds.deleteWith(ctx, substrate.ActorSystem, eref{Kind: kindRole, ID: id}, true, nil, false, nil); err != nil {
			t.Fatalf("delete the %s role's record: %v", id, err)
		}
	}
	held := func(identity string) []string {
		t.Helper()
		roles, err := ds.PersonRoles(substrate.WithIdentity(ctx, identity))
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, r := range roles {
			ids = append(ids, r.ID)
		}
		return ids
	}
	if got := held(rr.adaID); !slices.Equal(got, []string{scope.RoleAdmin}) {
		t.Fatalf("the admin's roles with the record gone = %v, want [admin]", got)
	}
	if got := held(rr.boID); len(got) != 0 {
		t.Fatalf("the editor's roles with the record gone = %v, want none", got)
	}
	if _, err := ds.Put(rr.as(rr.adaID), substrate.ActorPerson, substrate.PutInput{Kind: typeProvider, ID: "p-admin", Properties: map[string]any{"wire": "openai"}}); err != nil {
		t.Fatalf("the admin writes a provider with the record gone: %v", err)
	}
}

// Deleting a repository erases every person's records, so it is an admin's:
// a viewer and an editor who prove their own factors are refused, and the
// repository stands; its admin deletes it.
func TestOnlyAnAdminDeletesTheRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newRolesRepository(t)
	for _, name := range []string{rr.cy, rr.bo} {
		err := rr.r.s.DeleteRepository(ctx, substrate.LoginInput{Identity: name, Repository: rr.ada, Password: peoplePassword})
		if !errors.Is(err, substrate.ErrNotPermitted) {
			t.Fatalf("%s deleting the repository: %v, want refused by a rule", name, err)
		}
		if _, err := rr.r.s.Dataset(ctx, rr.ada); err != nil {
			t.Fatalf("the repository after %s's refused delete: %v", name, err)
		}
	}
	if err := rr.r.s.DeleteRepository(ctx, substrate.LoginInput{Identity: rr.ada, Repository: rr.ada, Password: peoplePassword}); err != nil {
		t.Fatalf("the admin deleting the repository: %v", err)
	}
	if _, err := rr.r.s.Dataset(ctx, rr.ada); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the repository after its admin's delete: %v, want gone", err)
	}
}

// A role's record changed or gone holds from the next request of a person
// who holds it, as every request reads the roles: the binding stands, and
// its rules are the record's as they are now.
func TestARoleRecordChangedOrGoneHoldsFromTheNextRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newRolesRepository(t)
	ds := rr.ds
	pkg := ds.Repository().Authority + "/notes"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatal(err)
	}
	bo := rr.as(rr.boID)
	write := func() error {
		_, err := ds.Put(bo, substrate.ActorPerson, substrate.PutInput{Kind: pkg + "/note", Properties: map[string]any{"name": "x"}})
		return err
	}
	if err := write(); err != nil {
		t.Fatalf("the editor's write: %v", err)
	}
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		_, err := tx.put(substrate.PutInput{Kind: kindRole, ID: scope.RoleEditor, Properties: map[string]any{
			"name": "Editor", "rules": []any{map[string]any{"verbs": []any{"read"}, "kinds": []any{"*"}}},
		}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := write(); !errors.Is(err, substrate.ErrNotPermitted) {
		t.Fatalf("the editor's write with the role's rules made read-only: %v, want refused by a rule", err)
	}
	if _, err := ds.deleteWith(ctx, substrate.ActorSystem, eref{Kind: kindRole, ID: scope.RoleEditor}, true, nil, false, nil); err != nil {
		t.Fatal(err)
	}
	if held, err := ds.PersonRoles(bo); err != nil || len(held) != 0 {
		t.Fatalf("the editor's roles with the role's record gone = %v (%v), want none", held, err)
	}
}

// A run's own conversation is the loop's write, not held to the person it
// acts for; a write the run's code directs at those kinds carries the run's
// ceiling and is held like any other. An agent whose declaration names
// threads and messages, acting for a viewer, renames no thread and deletes
// no message; acting for the admin, it renames one.
func TestCodeWritingARunsKindsIsHeldToThePerson(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rr := newRolesRepository(t)
	ds := rr.ds
	importVocabulary(t, ds, "tasks")
	fake := provisionAgents(t, ds)
	fake.script("edit", fakeTurn{content: "hello."})
	res, err := ds.CallAgent(substrate.WithIdentity(ctx, rr.adaID), crewPackage+"/editor", "say hello")
	if err != nil {
		t.Fatal(err)
	}
	var message string
	if err := ds.db.QueryRowContext(ctx, `SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id LIMIT 1`, typeMessage).Scan(&message); err != nil {
		t.Fatalf("a message of the run: %v", err)
	}
	agent := substrate.AgentActor("x.example.com", "p", "a")
	ceiling := &effectCeiling{emit: []string{typeThread, typeMessage}}
	viewer := substrate.WithOnBehalfOf(rr.as(rr.cyID), substrate.PersonActor(rr.cyID))
	rename := func(ctx context.Context) error {
		_, err := ds.patchBounded(ctx, agent, typeThread, res.Thread, substrate.PatchInput{Properties: map[string]any{"subject": "renamed"}}, ceiling)
		return err
	}
	if err := rename(viewer); !errors.Is(err, substrate.ErrNotPermitted) {
		t.Fatalf("an agent acting for a viewer renaming a thread: %v, want refused by a rule", err)
	}
	if _, err := ds.deleteBounded(viewer, agent, typeMessage, message, substrate.DeleteInput{}, ceiling); !errors.Is(err, substrate.ErrNotPermitted) {
		t.Fatalf("an agent acting for a viewer deleting a message: %v, want refused by a rule", err)
	}
	if err := rename(substrate.WithOnBehalfOf(ctx, substrate.ActorPerson)); err != nil {
		t.Fatalf("an agent acting for the admin renaming a thread: %v", err)
	}
}
