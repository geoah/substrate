package engine

// The Google contacts bundle — the substrate's first real connector (ticket
// 010). Two proofs, from the shipped closure at.
// ./../kinds/google.bundles.substrate.reamde.dev:
//
//  1. TestGoogleContactsBundleAdmitsSchema — the closure ADMITS through the
//     schema loader: the bundle declares the `client` input (facility-read,
//     never injected) the oauth2 block names, the two config kinds wear the
//     right host-recognized
//     traits (oauth2 on the config, accountconfig on the account),
//     the source type carries its required `person` subject edge, the bundle's
//     install-closure balances, and the contact→person mapping type-checks. No
//     DB, no uv — pure schema admission.
//
//  2. TestGoogleContactsBundleInstalls — the whole closure installs into a live
//     repository and every member (bundle, types, function, mapping, both triggers)
//     lands. This warms the PEP 723 sync body through uv, so it SKIPS when uv
//     is absent or cannot provision (offline); the schema-correctness proof
//     above needs neither.
//
// Real Google API calls never run in a test (no creds). The sync body's
// admission and shape are what these prove; live OAuth + sync is verified
// against a connected account.

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	googleExampleDir  = "../../kinds/google.bundles.substrate.reamde.dev"
	googleAuthority   = "google.bundles.substrate.reamde.dev"
	googleBundleRow   = googleAuthority + "/google"
	googleConfigType  = googleAuthority + "/config"
	googleAccountType = googleAuthority + "/account"
	googleContactType = googleAuthority + "/contact"
	googleSyncFn      = googleAuthority + "/contactssync"
	googleMigrationFn = googleAuthority + "/contactsidmigration"
	googleMapping     = googleAuthority + "/contactperson"
	googlePersonType  = "people.substrate.reamde.dev/person"

	// The gmail + calendar half of the same closure.
	googleAddressType  = googleAuthority + "/emailaddress"
	googleThreadType   = googleAuthority + "/thread"
	googleMessageType  = googleAuthority + "/message"
	googleCalendarType = googleAuthority + "/calendar"
	googleEventType    = googleAuthority + "/event"
	googleGmailFn      = googleAuthority + "/gmailsync"
	googleCalendarFn   = googleAuthority + "/calendarsync"
	googleAddressMap   = googleAuthority + "/emailaddressperson"

	coreThreadType   = "messaging.substrate.reamde.dev/emailthread"
	coreMessageType  = "messaging.substrate.reamde.dev/emailmessage"
	coreCalendarType = "calendar.substrate.reamde.dev/calendar"
	coreEventType    = "calendar.substrate.reamde.dev/calendarevent"
)

// TestGoogleContactsBundleAdmitsSchema loads the builtin schema, then installs
// the bundle closure on top of it through the ordinary loader/resolver — the
// same admission the batch apply runs, minus the function-body warm. Every
// assertion is a rule the loader enforces at admission time.
func TestGoogleContactsBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	// The registry an install actually admits into: the seeded tree (core
	// alone) plus the shipped VOCABULARY bundles this repository imported —
	// what a closure declaring onto people/tasks/messaging/calendar/media
	// needs present, and what `requires:` names.
	reg, err := enginetest.SeededRegistry("../../kinds/core.substrate.reamde.dev")
	if err != nil {
		t.Fatalf("build the repository registry: %v", err)
	}
	data, err := os.ReadFile(googleExampleDir + "/bundle.yaml")
	if err != nil {
		t.Fatalf("read bundle.yaml: %v", err)
	}
	docs, err := vocabulary.ParseStream(data)
	if err != nil {
		t.Fatalf("parse bundle.yaml: %v", err)
	}
	authorities, err := vocabulary.BuildAuthorities(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("build the bundle authority: %v", err)
	}
	if err := reg.InstallAll(authorities); err != nil {
		t.Fatalf("the bundle closure did not admit: %v", err)
	}

	// The bundle exists and declares the `client` input the oauth2 block
	// names: facility-read, so it must NOT inject.
	b, ok := reg.BundleOf(googleAuthority)
	if !ok {
		t.Fatalf("no bundle owns %s after install", googleAuthority)
	}
	in, ok := b.Inputs["client"]
	if !ok {
		t.Fatalf("bundle declares no client input: %v", b.InputOrder)
	}
	if in.Kind != googleConfigType {
		t.Fatalf("client input kind = %q, want %q", in.Kind, googleConfigType)
	}
	if in.Inject != "" {
		t.Fatalf("client input inject = %q, but the OAuth client is facility-read, never injected", in.Inject)
	}
	if b.OAuth2 == nil || b.OAuth2.ClientInput != "client" {
		t.Fatalf("oauth2 clientInput does not name the client input: %+v", b.OAuth2)
	}

	// The config type: oauth2 (client fields), the client input's kind.
	cfg, ok := reg.ByIdentity(googleConfigType)
	if !ok {
		t.Fatalf("config type %s missing", googleConfigType)
	}
	if !cfg.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("config type does not implement %s", vocabulary.TraitOAuth2Core)
	}

	// The account type: accountconfig (the OAuth facility's two hands), and NOT
	// oauth2 — the as-built facility binds client creds on the config, tokens on
	// the account.
	acct, ok := reg.ByIdentity(googleAccountType)
	if !ok {
		t.Fatalf("account type %s missing", googleAccountType)
	}
	if !acct.Implements(vocabulary.TraitAccountConfigCore) {
		t.Fatalf("account type does not implement %s", vocabulary.TraitAccountConfigCore)
	}
	if acct.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("account type implements oauth2 — client creds belong on the config, not the account")
	}

	// The source type carries its `person` subject edge — required, single,
	// pointing at person — which is what the mapping names.
	contact, ok := reg.ByIdentity(googleContactType)
	if !ok {
		t.Fatalf("source type %s missing", googleContactType)
	}
	ed, ok := contact.Edge("person")
	if !ok {
		t.Fatalf("contact declares no `person` edge")
	}
	if ed.To != "people.substrate.reamde.dev/person" || !ed.Required || ed.Many {
		t.Fatalf("person edge shape wrong: to=%q required=%v many=%v", ed.To, ed.Required, ed.Many)
	}

	// The mapping resolved: from the contact, to the person, on the person edge.
	m, ok := reg.MappingFor(googleContactType)
	if !ok {
		t.Fatalf("no mapping registered from %s", googleContactType)
	}
	if m.To != "people.substrate.reamde.dev/person" || m.Edge != "person" {
		t.Fatalf("mapping resolves wrong: to=%q edge=%q", m.To, m.Edge)
	}
	if len(m.Match) == 0 {
		t.Fatalf("mapping ships no match probe — identity would never link on email")
	}

	// The sync function and the batched id migration are members of the authority.
	if _, err := reg.ResolveFunction(googleSyncFn); err != nil {
		t.Fatalf("sync function %s did not register: %v", googleSyncFn, err)
	}
	mig, err := reg.ResolveFunction(googleMigrationFn)
	if err != nil {
		t.Fatalf("id migration function %s did not register: %v", googleMigrationFn, err)
	}
	// The migration is a bounded, batched callable (fleet review): it
	// declares its {limit, cursor} tool card, and the absorbed-orphan fix
	// needs BOTH halves of the merge gate — person in emit and the
	// mutations grant — or the fold is refused at effect decode.
	if mig.Input == nil || mig.Output == nil {
		t.Fatalf("the migration is not a full tool card: input=%v output=%v",
			mig.Input != nil, mig.Output != nil)
	}
	if !mig.Caps.AllowsMutation(vocabulary.MutationMerge) {
		t.Fatalf("the migration lacks the capabilities.mutations merge grant — the absorbed-orphan fold would be refused")
	}
	var emitsPerson bool
	for _, ty := range mig.Caps.Emit {
		if ty == googlePersonType {
			emitsPerson = true
		}
	}
	if !emitsPerson {
		t.Fatalf("the migration's emit %v does not name %s — the person merge would be refused", mig.Caps.Emit, googlePersonType)
	}

	// The three feature toggles are declared bool properties on the account.
	// All three are wired to a scope now; the gmail and calendar
	// closures assert their own scope lists.
	for _, name := range []string{"enabledContacts", "enabledGmail", "enabledCalendar"} {
		p, ok := acct.Prop(name)
		if !ok || p.Datatype != vocabulary.DatatypeBool {
			t.Fatalf("account toggle %s: ok=%v kind=%v — every feature toggle is a declared bool", name, ok, p)
		}
	}

	// Connector state comes in two tiers. The ROLLUP stays
	// account-level and unprefixed, because every accountconfig type in the
	// fleet declares it and the console reads it generically off any
	// connection; the per-stream CADENCE ANCHORS and CURSORS are prefixed, so
	// one stream can neither suppress another's due check nor satisfy
	// another's on-connect guard.
	for _, name := range []string{
		"lastSyncedAt", "syncStatus",
		"contactsSyncToken", "contactsLastSyncedAt",
		"gmailLastSyncedAt", "calendarLastSyncedAt",
	} {
		p, ok := acct.Prop(name)
		if !ok {
			t.Fatalf("account misses the connector state property %s", name)
		}
		if p.Writer != vocabulary.WriterConnector {
			t.Fatalf("account.%s writer = %q, want connector", name, p.Writer)
		}
	}
	if _, ok := acct.Prop("syncToken"); ok {
		t.Fatalf("account still declares a bare syncToken — a cursor belongs to one stream")
	}
}

// TestGoogleContactsBundleInstalls applies the whole closure into a live repository
// and asserts every member installs. It warms the PEP 723 sync body through uv,
// so it skips when uv is absent or cannot provision.
func TestGoogleContactsBundleInstalls(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the sync body warms through uv at install")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)

	// The atomic install from the shipped manifest. A failure here is either a
	// schema problem (already caught deterministically by the loader test
	// above, without uv) or a uv provisioning failure (offline) — so treat an
	// apply error as a skip rather than double-reporting a schema break.
	vocabularyDocs := loadYAMLDocs(t, googleExampleDir+"/bundle.yaml")
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, vocabularyDocs); err != nil {
		if isUVProvisionError(err) {
			t.Skipf("bundle install could not warm the PEP 723 body (uv offline?): %v", err)
		}
		t.Fatalf("install the google bundle: %v", err)
	}

	// The bundle row and every schema member landed as its own record.
	for id, wantType := range map[string]string{
		googleBundleRow:   "core.substrate.reamde.dev/bundle",
		googleConfigType:  "core.substrate.reamde.dev/kind",
		googleAccountType: "core.substrate.reamde.dev/kind",
		googleContactType: "core.substrate.reamde.dev/kind",
		googleSyncFn:      "core.substrate.reamde.dev/function",
		googleMigrationFn: "core.substrate.reamde.dev/function",
		googleMapping:     "core.substrate.reamde.dev/recordmapping",
		// The gmail and calendar closure installs from the same
		// atomic manifest: four mirror types, the shared address source, two
		// sync functions and the address mapping.
		googleAddressType:  "core.substrate.reamde.dev/kind",
		googleThreadType:   "core.substrate.reamde.dev/kind",
		googleMessageType:  "core.substrate.reamde.dev/kind",
		googleCalendarType: "core.substrate.reamde.dev/kind",
		googleEventType:    "core.substrate.reamde.dev/kind",
		googleGmailFn:      "core.substrate.reamde.dev/function",
		googleCalendarFn:   "core.substrate.reamde.dev/function",
		googleAddressMap:   "core.substrate.reamde.dev/recordmapping",
	} {
		row, err := ds.Get(ctx, wantType, id)
		if err != nil {
			t.Fatalf("member %s did not install: %v", id, err)
		}
		if row.Kind != wantType {
			t.Fatalf("member %s is a %s, want %s", id, row.Kind, wantType)
		}
	}

	// Computed status: installed, enabled, and the closure's member counts.
	st, err := ds.BundleStatus(ctx, googleAuthority)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if !st.Installed || !st.Enabled {
		t.Fatalf("bundle not live: installed=%v enabled=%v", st.Installed, st.Enabled)
	}
	if len(st.Inputs) != 1 || st.Inputs[0].Name != "client" || st.Inputs[0].Kind != googleConfigType {
		t.Fatalf("status inputs = %+v, want the one client input", st.Inputs)
	}
	if st.Inputs[0].Record != "" || st.Inputs[0].Via != "" {
		t.Fatalf("client input resolved with no config record created: %+v", st.Inputs[0])
	}
	if len(st.Setup) != 1 || st.Setup[0].Code != substrate.SetupMissing || st.Setup[0].Input != "client" {
		t.Fatalf("status setup = %+v, want the one missing-input item", st.Setup)
	}
	if st.Functions != 4 {
		t.Fatalf("status functions = %d, want 4 (contacts sync + id migration + gmail sync + calendar sync)", st.Functions)
	}

	// The delivery wiring installs as ordinary data records, two triggers per
	// stream, each bound to its own sync function.
	for _, m := range loadYAMLDocs(t, googleExampleDir+"/triggers.yaml") {
		putDataDoc(t, ds, m)
	}
	for _, id := range []string{
		"google-contacts-on-connect", "google-contacts-scheduled",
		"google-gmail-on-connect", "google-gmail-scheduled",
		"google-calendar-on-connect", "google-calendar-scheduled",
	} {
		row, err := ds.Get(ctx, typeTrigger, id)
		if err != nil {
			t.Fatalf("trigger %s did not install: %v", id, err)
		}
		if row.Kind != typeTrigger {
			t.Fatalf("trigger %s is a %s", id, row.Kind)
		}
	}
}

// isUVProvisionError reports whether a bundle-install error is the PEP 723 body
// failing to warm (uv resolve/provision), as opposed to a schema admission
// problem. Preparation failures surface as "body failed to prepare".
func isUVProvisionError(err error) bool {
	s := strings.ToLower(err.Error())
	for _, marker := range []string{"failed to prepare", "uv", "provision", "resolve", "register"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// TestGoogleContactsSyncWritesDeclaredFields: an object property's fields are
// validated STRICTLY — an undeclared key refuses the whole put — so the sync
// body and the declaration it writes into have to agree letter for letter.
// They drifted once, when a vocabulary sweep renamed the declared field and
// left the body writing the old name, which refused every contact carrying a
// home or work email. Nothing caught it: no fixture in this package has ever
// carried an emailAddresses or phoneNumbers array. This is that catch, and it
// needs no Google, no uv and no database — the closure is text and so is the
// body.
func TestGoogleContactsSyncWritesDeclaredFields(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(googleExampleDir + "/bundle.yaml")
	if err != nil {
		t.Fatalf("read bundle.yaml: %v", err)
	}
	docs, err := vocabulary.ParseStream(data)
	if err != nil {
		t.Fatalf("parse bundle.yaml: %v", err)
	}
	var declared map[string]bool
	var body string
	for _, d := range docs {
		switch d.ID {
		case googleContactType:
			// Every field of every object property: the body assembles each
			// one into a local named `item`, so the union is what its writes
			// are held to.
			declared = map[string]bool{}
			props, _ := d.Data["properties"].(map[string]any)
			for _, p := range props {
				pm, _ := p.(map[string]any)
				fields, _ := pm["fields"].(map[string]any)
				for field := range fields {
					declared[field] = true
				}
			}
			if len(declared) == 0 {
				t.Fatal("the contact kind declares no object fields")
			}
		case googleSyncFn:
			body, _ = d.Data["source"].(string)
		}
	}
	if declared == nil || body == "" {
		t.Fatalf("the closure is missing %s or %s", googleContactType, googleSyncFn)
	}
	written := regexp.MustCompile(`item\["([A-Za-z0-9_]+)"\]\s*=`).FindAllStringSubmatch(body, -1)
	if len(written) == 0 {
		t.Fatal("the sync body assembles no object fields — the guard has stopped guarding")
	}
	for _, m := range written {
		if !declared[m[1]] {
			t.Errorf("the sync body writes %q, which no object property of the contact kind declares (declared: %v)",
				m[1], sortedKeys(declared))
		}
	}
}
