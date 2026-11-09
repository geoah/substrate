package engine

import (
	"context"
	"database/sql"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The Gmail sync's hand: installed code, at the bundle tier, as dispatch
// stamps it (applyEffects).
const relayGmailSync = substrate.Actor(substrate.FunctionActorPrefix + "providers.substrate.reamde.dev:google:syncgmail")

const (
	relayGooglePackage = "providers.substrate.reamde.dev/google"
	relayAccount       = relayGooglePackage + "/account"
	relayCalendar      = relayGooglePackage + "/calendarevent"
)

// installRelayStandIn declares the Google provider's kinds the migration
// reads, in the shapes the shipped bundle gives them: the account, the
// `emailaddress` hub core's mapping makes contacts of, a message whose five
// header lists name hub rows, and a calendar event whose organizer does.
func installRelayStandIn(t *testing.T, ds *dataset) {
	t.Helper()
	entry := map[string]any{"type": "object", "repeated": true, "fields": map[string]any{
		"value":   map[string]any{"type": "string"},
		"address": map[string]any{"type": "reference", "kind": relayHubKind},
		"name":    map[string]any{"type": "string"},
	}}
	account := map[string]any{"type": "reference", "kind": relayAccount, "required": true, "onDelete": "cascade"}
	kind := func(name string, extra, props map[string]any) map[string]any {
		data := map[string]any{
			"authority": "providers.substrate.reamde.dev", "package": "google",
			"names": map[string]any{"singular": name}, "properties": props,
		}
		for k, v := range extra {
			data[k] = v
		}
		return map[string]any{
			"kind": vocabulary.CoreKind(vocabulary.DocKind), "metadata": map[string]any{"id": relayGooglePackage + "/" + name}, "data": data,
		}
	}
	docs := []map[string]any{
		{
			"kind": vocabulary.CoreKind(vocabulary.DocPackage), "metadata": map[string]any{"id": relayGooglePackage},
			"data": map[string]any{"authority": "providers.substrate.reamde.dev", "package": "google", "version": 1},
		},
		kind("account", map[string]any{"displayTemplate": "{email}"}, map[string]any{"email": map[string]any{"type": "email"}}),
		kind("emailaddress", map[string]any{"displayTemplate": "{address}", "purpose": "supporting"}, map[string]any{
			"account": account, "address": map[string]any{"type": "email", "required": true},
		}),
		kind("gmailmessage", map[string]any{"displayTemplate": "{subject}"}, map[string]any{
			"account": account, "subject": map[string]any{"type": "string"},
			"from": entry, "to": entry, "cc": entry, "bcc": entry, "replyTo": entry,
		}),
		kind("calendarevent", map[string]any{"displayTemplate": "{summary}"}, map[string]any{
			"account": account, "summary": map[string]any{"type": "string"},
			"organizer": map[string]any{"type": "object", "fields": map[string]any{
				"address": map[string]any{"type": "reference", "kind": relayHubKind},
			}},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorSystem, docs); err != nil {
		t.Fatalf("install the Google stand-in: %v", err)
	}
}

// relaySync applies writes the way the Gmail sync's delivery commits them.
func relaySync(t *testing.T, ds *dataset, effects ...effect) {
	t.Helper()
	emit := []string{relayAccount, relayHubKind, relayMessageKind, relayCalendar}
	if err := ds.inTx(context.Background(), relayGmailSync, false, func(tx *txn) error {
		return tx.applyEffects(emit, effects)
	}); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func relayRef(kind, id string) map[string]any {
	return map[string]any{"ref": vocabulary.RecordPath(kind, id)}
}

// relayHub is the put of one hub row, named by its address as the sync names
// it by the normal form.
func relayHub(address string, props ...map[string]any) effect {
	p := map[string]any{"account": relayRef(relayAccount, fid("acct")), "address": address}
	for _, extra := range props {
		for k, v := range extra {
			p[k] = v
		}
	}
	return effect{Action: effectPut, Type: relayHubKind, ID: fid(address), Properties: p}
}

// relayEntry is one header occurrence naming its hub row.
func relayEntry(address, name string) map[string]any {
	e := map[string]any{"value": address, "address": relayRef(relayHubKind, fid(address))}
	if name != "" {
		e["name"] = name
	}
	return e
}

func relayMessage(id string, headers map[string][]any) effect {
	p := map[string]any{"account": relayRef(relayAccount, fid("acct")), "subject": "Re: [owner/repo] " + id}
	for k, v := range headers {
		p[k] = v
	}
	return effect{Action: effectPut, Type: relayMessageKind, ID: fid(id), Properties: p}
}

// relayContactOf reads the contact a hub row's `contact` slot names.
func relayContactOf(t *testing.T, ds *dataset, address string) string {
	t.Helper()
	row := relayRow(t, ds, relayHubKind, fid(address))
	if row == nil {
		t.Fatalf("no hub row for %s", address)
	}
	_, id, ok := vocabulary.SplitRecordPath(storedReferencePath(row.Properties["contact"]))
	if !ok {
		t.Fatalf("hub row %s links no contact: %v", address, row.Properties)
	}
	return id
}

// relayRecord is one stored row as these tests read it, tombstone included.
type relayRecord struct {
	Version    int64
	Properties map[string]any
	Deleted    bool
}

// relayRow reads one record, nil when there is none.
func relayRow(t *testing.T, ds *dataset, kind, id string) *relayRecord {
	t.Helper()
	var row *relayRecord
	if err := ds.inTx(context.Background(), substrate.ActorSystem, true, func(tx *txn) error {
		r, err := tx.loadRow(eref{Kind: kind, ID: id}, false)
		if err != nil || r == nil {
			return err
		}
		row = &relayRecord{Version: r.Version, Properties: r.Props, Deleted: r.DeletedAt != nil}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return row
}

// relayOpen opens the engine over one database and data root, so a reopen
// meets the repository the last one wrote.
func relayOpen(t *testing.T, dsn, root, repo string) (*service, *dataset) {
	t.Helper()
	ctx := context.Background()
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	d, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("open the repository: %v", err)
	}
	return svc.(*service), d.(*dataset)
}

// relayRerun closes the service, forgets that migration 5 ran, and opens the
// repository again, which runs it.
func relayRerun(t *testing.T, svc *service, dsn, root, repo string) (*service, *dataset) {
	t.Helper()
	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	maint, err := OpenScopedDB(dsn, repo, RoleMaint)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = maint.Close() }()
	if _, err := maint.ExecContext(context.Background(),
		`DELETE FROM repository_migrations WHERE repository = $1 AND version = 5`, repo); err != nil {
		t.Fatalf("forget migration 5: %v", err)
	}
	return relayOpen(t, dsn, root, repo)
}

func relayHead(t *testing.T, db *sql.DB, repo string) int64 {
	t.Helper()
	var seq int64
	if err := db.QueryRowContext(context.Background(),
		`SELECT coalesce(max(seq), 0) FROM changelog WHERE repository = $1`, repo).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

// The contacts a GitHub reply relay alone made go with their hub rows, and a
// message names no hub row for a relay any more, keeping what the header
// spelled. So does one the owner only hid: hiding a relay's contact is
// calling it noise. A contact anything else holds stays, and so do the relay
// rows linking to it: a second source, an owner's edit, a promotion, a merge,
// an owner's record naming the contact, a calendar event naming the row. A
// GitHub commit address is a person and is not a relay. A second run finds
// nothing, and a rebuild of the changelog reproduces the store.
func TestRepositoryMigrationRemovesTheContactsReplyRelaysMade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := MigratedDSN(t), t.TempDir(), testdb.Repository(t)
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = svc.Close()
	s, ds := relayOpen(t, dsn, root, repo)
	installRelayStandIn(t, ds)

	const (
		notif   = "notifications@github.com"
		repoA   = "repo@noreply.github.com"
		mention = "mention@noreply.github.com"
		relayA  = "reply+AAEXAMPLEONE0000@reply.github.com"
		relayB  = "Reply+AAEXAMPLETWO0000@reply.github.com"
		commit  = "12345+ada@users.noreply.github.com"
		ada     = "ada@example.com"
		grace   = "grace@example.com"
		shared  = "reply+AAEXAMPLESHARED0@reply.github.com"
		edited  = "reply+AAEXAMPLEEDITED0@reply.github.com"
		hidden  = "reply+AAEXAMPLEHIDDEN0@reply.github.com"
		known   = "reply+AAEXAMPLEKNOWN00@reply.github.com"
		merged  = "reply+AAEXAMPLEMERGED0@reply.github.com"
		invite  = "reply+AAEXAMPLEINVITE0@reply.github.com"
		member  = "reply+AAEXAMPLEMEMBER0@reply.github.com"
	)
	relaySync(t, ds,
		effect{Action: effectPut, Type: relayAccount, ID: fid("acct"), Properties: map[string]any{"email": "owner@example.com"}},
		relayHub(notif), relayHub(repoA), relayHub(mention), relayHub(relayA), relayHub(relayB),
		relayHub(commit), relayHub(ada), relayHub(grace), relayHub(edited), relayHub(hidden), relayHub(known), relayHub(merged), relayHub(invite), relayHub(member),
		relayMessage("m1", map[string][]any{
			"from": {relayEntry(notif, "Grace")}, "to": {relayEntry(repoA, "owner/repo")},
			"replyTo": {relayEntry(relayA, "owner/repo")},
		}),
		relayMessage("m2", map[string][]any{
			"from": {relayEntry(notif, "Ada")}, "to": {relayEntry(repoA, "owner/repo")},
			"cc": {relayEntry(mention, "Mention"), relayEntry(ada, "Ada")}, "replyTo": {relayEntry(relayB, "owner/repo")},
		}),
		relayMessage("m3", map[string][]any{"from": {relayEntry(commit, "Ada")}, "to": {relayEntry(ada, "")}}),
		relayMessage("m4", map[string][]any{
			"from": {relayEntry(notif, "")}, "to": {relayEntry(repoA, "owner/repo")}, "replyTo": {relayEntry(edited, "")},
		}),
		relayMessage("m5", map[string][]any{"replyTo": {relayEntry(hidden, "")}, "bcc": {relayEntry(merged, "")}}),
		relayMessage("m6", map[string][]any{"replyTo": {relayEntry(invite, ""), relayEntry(member, "")}}),
		relayMessage("m8", map[string][]any{"replyTo": {relayEntry(known, "")}}),
		effect{Action: effectPut, Type: relayCalendar, ID: fid("e1"), Properties: map[string]any{
			"account": relayRef(relayAccount, fid("acct")), "summary": "Review",
			"organizer": map[string]any{"address": relayRef(relayHubKind, fid(invite))},
		}},
	)
	// A relay row the sync linked to Ada's own contact: one contact, a relay
	// source and a real one.
	adaContact := relayContactOf(t, ds, ada)
	relaySync(t, ds,
		relayHub(shared, map[string]any{"contact": relayRef(vocabulary.KindContact, adaContact)}),
		relayMessage("m7", map[string][]any{"replyTo": {relayEntry(shared, "")}}),
	)
	if got := relayContactOf(t, ds, shared); got != adaContact {
		t.Fatalf("the shared relay links %s, want Ada's contact %s", got, adaContact)
	}

	// The owner names one relay contact, hides another, promotes a third and
	// merges a fourth into Grace's.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindContact, relayContactOf(t, ds, edited),
		substrate.PatchInput{Properties: map[string]any{"name": "Review thread"}}); err != nil {
		t.Fatalf("name: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindContact, relayContactOf(t, ds, hidden),
		substrate.PatchInput{Properties: map[string]any{"prominence": "hidden"}}); err != nil {
		t.Fatalf("hide: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindContact, relayContactOf(t, ds, known),
		substrate.PatchInput{Properties: map[string]any{"prominence": "known"}}); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: vocabulary.KindContact, Properties: map[string]any{
		"name": "Linus", "memberOf": []any{relayRef(vocabulary.KindContact, relayContactOf(t, ds, member))},
	}}); err != nil {
		t.Fatalf("an owner's contact naming a relay contact: %v", err)
	}
	graceContact := relayContactOf(t, ds, grace)
	if _, err := ds.Merge(ctx, substrate.ActorAPI, substrate.MergeInput{
		Kind: vocabulary.KindContact, Winner: graceContact, Loser: relayContactOf(t, ds, merged),
	}); err != nil {
		t.Fatalf("merge: %v", err)
	}

	contactsBefore := map[string]string{}
	for _, a := range []string{notif, repoA, mention, relayA, relayB, commit, ada, edited, hidden, known, invite, member} {
		contactsBefore[a] = relayContactOf(t, ds, a)
	}
	versions := map[string]int64{}
	for _, m := range []string{"m3", "m6", "m7", "m8"} {
		versions[m] = relayRow(t, ds, relayMessageKind, fid(m)).Version
	}

	s, ds = relayRerun(t, s, dsn, root, repo)
	DrainIndexReprojection(t, ds)

	for _, a := range []string{repoA, mention, relayA, relayB, hidden} {
		if hub := relayRow(t, ds, relayHubKind, fid(a)); hub == nil || !hub.Deleted {
			t.Errorf("the hub row of %s is %v, want deleted", a, hub)
		}
		if c := relayRow(t, ds, vocabulary.KindContact, contactsBefore[a]); c == nil || !c.Deleted {
			t.Errorf("the contact %s made for %s is %v, want deleted", contactsBefore[a], a, c)
		}
	}
	kept := map[string]string{
		notif:  "GitHub's one sender",
		commit: "a commit address names a person",
		ada:    "a real address",
		shared: "its contact has a real address's source too",
		edited: "the owner named its contact",
		known:  "the owner promoted its contact",
		merged: "the owner merged its contact",
		invite: "a calendar event names the row",
		member: "an owner's record names its contact",
	}
	for a, why := range kept {
		if hub := relayRow(t, ds, relayHubKind, fid(a)); hub == nil || hub.Deleted {
			t.Errorf("the hub row of %s is gone, want kept: %s", a, why)
		}
	}
	for a, c := range map[string]string{
		notif: contactsBefore[notif], commit: contactsBefore[commit], ada: adaContact, edited: contactsBefore[edited],
		known: contactsBefore[known], invite: contactsBefore[invite], member: contactsBefore[member], grace: graceContact,
	} {
		if row := relayRow(t, ds, vocabulary.KindContact, c); row == nil || row.Deleted {
			t.Errorf("the contact of %s is gone, want kept", a)
		}
	}
	if emails := relayRow(t, ds, vocabulary.KindContact, adaContact).Properties["emails"]; len(emails.([]any)) != 2 {
		t.Errorf("Ada's contact holds %v, want her address and the shared relay's", emails)
	}

	// The relay entries keep what the header spelled and name no hub row;
	// every other entry still names its own.
	header := func(m, name string, i int) map[string]any {
		t.Helper()
		row := relayRow(t, ds, relayMessageKind, fid(m))
		entries, _ := row.Properties[name].([]any)
		if i >= len(entries) {
			t.Fatalf("%s.%s = %v, want an entry %d", m, name, row.Properties[name], i)
		}
		return entries[i].(map[string]any)
	}
	for _, c := range []struct {
		m, name string
		i       int
		value   string
		names   bool
	}{
		{"m1", "from", 0, notif, true},
		{"m1", "to", 0, repoA, false},
		{"m1", "replyTo", 0, relayA, false},
		{"m2", "to", 0, repoA, false},
		{"m2", "cc", 0, mention, false},
		{"m2", "cc", 1, ada, true},
		{"m2", "replyTo", 0, relayB, false},
		{"m4", "to", 0, repoA, false},
		{"m4", "replyTo", 0, edited, true},
		{"m5", "replyTo", 0, hidden, false},
		{"m5", "bcc", 0, merged, true},
	} {
		e := header(c.m, c.name, c.i)
		if e["value"] != c.value {
			t.Errorf("%s.%s[%d] value = %v, want %s", c.m, c.name, c.i, e["value"], c.value)
		}
		_, names := e["address"]
		if names != c.names {
			t.Errorf("%s.%s[%d] = %v, names its hub row %v, want %v", c.m, c.name, c.i, e, names, c.names)
		}
	}
	if e := header("m1", "replyTo", 0); e["name"] != "owner/repo" {
		t.Errorf("m1's reply-to entry = %v, want the name the header spelled", e)
	}
	for m, v := range versions {
		if got := relayRow(t, ds, relayMessageKind, fid(m)).Version; got != v {
			t.Errorf("message %s moved from version %d to %d, and names no relay this removes", m, v, got)
		}
	}
	// Nothing names a deleted hub row any more, once the pass re-derived the
	// messages' refs rows.
	db := rawDB(t, ds)
	var dangling int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM refs r JOIN records x ON x.repository = r.repository AND x.kind = r.dst_kind AND x.id = r.dst
		WHERE r.repository = $1 AND r.dst_kind = $2 AND x.deleted_at IS NOT NULL AND r.src_kind = $3`,
		repo, relayHubKind, relayMessageKind).Scan(&dangling); err != nil {
		t.Fatal(err)
	}
	if dangling != 0 {
		t.Errorf("%d message refs rows still name a deleted hub row", dangling)
	}

	// Again: nothing to do.
	head := relayHead(t, db, repo)
	s, ds = relayRerun(t, s, dsn, root, repo)
	if got := relayHead(t, db, repo); got != head {
		t.Fatalf("a second run moved the changelog from %d to %d", head, got)
	}

	live := foldSnapshotOf(t, ds)
	if _, err := s.RebuildRepository(ctx, repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuilt := foldSnapshotOf(t, ds); live != rebuilt {
		t.Fatalf("the rebuilt fold is not the live one\n%s", firstDifferenceOf([]byte(live), []byte(rebuilt)))
	}
}

// A repository without the Google provider has no hub kind, and the
// migration reads that and writes nothing.
func TestRepositoryMigrationOfReplyRelaysIsSilentWithoutGoogle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root, repo := MigratedDSN(t), t.TempDir(), testdb.Repository(t)
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = svc.Close()
	s, ds := relayOpen(t, dsn, root, repo)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindContact, Properties: map[string]any{"emails": []any{"reply+AAEXAMPLE0000@reply.github.com"}},
	}); err != nil {
		t.Fatal(err)
	}
	db := rawDB(t, ds)
	head := relayHead(t, db, repo)
	_, _ = relayRerun(t, s, dsn, root, repo)
	if got := relayHead(t, db, repo); got != head {
		t.Fatalf("the migration moved the changelog from %d to %d in a repository without Google", head, got)
	}
	var name string
	if err := db.QueryRowContext(ctx, `SELECT name FROM repository_migrations WHERE repository = $1 AND version = 5`, repo).Scan(&name); err != nil {
		t.Fatalf("the ledger does not record migration 5: %v", err)
	}
}
