package providertest

// The Google contacts bundle — the substrate's first real connector (ticket
// 010). Two proofs, from the shipped closure at.
// ./../kinds/providers.substrate.reamde.dev/google:
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
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/vocabulary"
)

// TestGoogleContactsBundleAdmitsSchema loads the builtin schema, then installs
// the bundle closure on top of it through the ordinary loader/resolver — the
// same admission the batch apply runs, minus the function-body warm. Every
// assertion is a rule the loader enforces at admission time.
func TestGoogleContactsBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	reg := bundleRegistry(t, googleDir)

	// The bundle exists and declares the `client` input the oauth2 block
	// names: facility-read, so it must NOT inject.
	b := assertBundleInput(t, reg, googlePackage, "client", googleConfigType, "")
	if b.OAuth2 == nil || b.OAuth2.ClientInput != "client" {
		t.Fatalf("oauth2 clientInput does not name the client input: %+v", b.OAuth2)
	}

	// The config type: oauth2 (client fields), the client input's kind.
	cfg := mustKind(t, reg, googleConfigType)
	if !cfg.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("config type does not implement %s", vocabulary.TraitOAuth2Core)
	}

	// The account type: accountconfig (the OAuth facility's two hands), and NOT
	// oauth2 — the as-built facility binds client creds on the config, tokens on
	// the account.
	acct := mustKind(t, reg, googleAccountType)
	if !acct.Implements(vocabulary.TraitAccountConfigCore) {
		t.Fatalf("account type does not implement %s", vocabulary.TraitAccountConfigCore)
	}
	if acct.Implements(vocabulary.TraitOAuth2Core) {
		t.Fatalf("account type implements oauth2 — client creds belong on the config, not the account")
	}

	// The contact mirror carries an EMPTY subject slot: single, unpinned and
	// optional (record 49). The repository's own mapping pins it.
	contact := mustKind(t, reg, googleContactType)
	ed, ok := contact.Prop("person")
	if !ok {
		t.Fatalf("contact declares no `person` slot")
	}
	if ed.To != "" || ed.Required || ed.Repeated || !ed.Subject || !ed.MustExist {
		t.Fatalf("person slot shape wrong: to=%q required=%v many=%v subject=%v mustExist=%v",
			ed.To, ed.Required, ed.Repeated, ed.Subject, ed.MustExist)
	}

	// The closure ships NO mapping: this package owns no person.
	if ms := reg.MappingsFrom(googleContactType); len(ms) != 0 {
		t.Fatalf("the google closure ships %d mappings from contact; a provider ships none", len(ms))
	}
	if ms := reg.MappingsTo("samples.substrate.reamde.dev/people/person"); len(ms) != 0 {
		t.Fatalf("the google closure maps onto person: %v", ms)
	}

	// The sync function is a member of the package, and it is the ONLY
	// contacts callable: the one-shot id migration went with the core rows
	// (record 49), because no store old enough to need it can open on the
	// package grammar of record 0047.
	sync, err := reg.ResolveFunction(googleSyncFn)
	if err != nil {
		t.Fatalf("sync function %s did not register: %v", googleSyncFn, err)
	}
	for _, ident := range sync.Caps.Emit {
		if strings.HasPrefix(ident, enginetest.SampleAuthority+"/") {
			t.Fatalf("contactssync may write %s, a kind this package does not own", ident)
		}
	}
	for _, g := range reg.PackageList() {
		if g.Identity != googlePackage {
			continue
		}
		for _, name := range g.FunctionOrder {
			if strings.Contains(name, "migration") {
				t.Fatalf("the closure still ships %s", name)
			}
		}
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
	requireUV(t)
	_, ds := newDataset(t)

	// The atomic install from the shipped manifest.
	install(t, ds, googleDir, nil)

	// The bundle row and every schema member landed as its own record.
	assertMembers(t, ds, map[string]string{
		googlePackage:     typeBundle,
		googleConfigType:  typeKind,
		googleAccountType: typeKind,
		googleContactType: typeKind,
		googleSyncFn:      typeFunction,
		// The gmail and calendar closure installs from the same
		// atomic manifest: four mirror types, the shared address source and two
		// sync functions. No mapping: this package owns no person (record 49).
		googleAddressType:  typeKind,
		googleThreadType:   typeKind,
		googleMessageType:  typeKind,
		googleCalendarType: typeKind,
		googleEventType:    typeKind,
		googleGmailFn:      typeFunction,
		googleCalendarFn:   typeFunction,
	})

	// Computed status: installed, enabled, and the closure's member counts.
	assertUnresolvedInput(t, ds, googlePackage, "client", googleConfigType, 3)

	// The delivery wiring installs as ordinary data records, two triggers per
	// stream, each bound to its own sync function.
	installTriggers(t, ds, googleDir)
	assertTriggers(t, ds,
		"google-contacts-on-connect", "google-contacts-scheduled",
		"google-gmail-on-connect", "google-gmail-scheduled",
		"google-calendar-on-connect", "google-calendar-scheduled",
	)
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
	data, err := os.ReadFile(googleDir + "/bundle.yaml")
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
