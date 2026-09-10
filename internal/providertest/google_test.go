package providertest

// What the three google streams share: the closure they all come from
// (contacts, gmail and calendar install from ONE bundle.yaml), the kinds it
// declares, and the fixtures a stream test seeds.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

const (
	googleDir         = providersDir + "/google"
	googlePackage     = "providers.substrate.reamde.dev/google"
	googleConfigType  = googlePackage + "/config"
	googleAccountType = googlePackage + "/account"
	googleContactType = googlePackage + "/contact"
	googleSyncFn      = googlePackage + "/contactssync"
	googlePersonType  = "samples.substrate.reamde.dev/people/person"

	// The gmail + calendar half of the same closure.
	googleAddressType  = googlePackage + "/emailaddress"
	googleThreadType   = googlePackage + "/thread"
	googleMessageType  = googlePackage + "/message"
	googleCalendarType = googlePackage + "/calendar"
	googleEventType    = googlePackage + "/event"
	googleGmailFn      = googlePackage + "/gmailsync"
	googleCalendarFn   = googlePackage + "/calendarsync"

	coreThreadType   = "samples.substrate.reamde.dev/messaging/emailthread"
	coreMessageType  = "samples.substrate.reamde.dev/messaging/emailmessage"
	coreCalendarType = "samples.substrate.reamde.dev/calendar/calendar"
	coreEventType    = "samples.substrate.reamde.dev/calendar/calendarevent"
	coreSeriesType   = "samples.substrate.reamde.dev/calendar/calendareventseries"
)

// googleAccountID is the account every stream test steps: one connection, so
// the stamp assertions name a record that exists.
const googleAccountID = "acct-step"

// gmailAPIAt and calendarAPIAt point one body's API base at a loopback fake.
// Each body pins its provider origin and allows loopback as the test seam, so
// the rewritten base admits.
func gmailAPIAt(baseURL string) [2]string {
	return [2]string{
		`GMAIL_API = "https://gmail.googleapis.com"`,
		`GMAIL_API = "` + baseURL + `"`,
	}
}

func calendarAPIAt(baseURL string) [2]string {
	return [2]string{
		`CAL_API = "https://www.googleapis.com"`,
		`CAL_API = "` + baseURL + `"`,
	}
}

// googleInstall installs the shipped closure with the named body
// substitutions. The closure ships NO mapping and writes no kind it does not
// own (decision record 0049), so what a test gets is mirrors with every
// subject slot empty. The mappings onto `person` are the PEOPLE sample's,
// dropped when it was imported before this provider existed; a test that
// wants the mint, the projection or the one-hop resolution re-applies that
// closure afterwards (enginetest.DeclareMappings), which is the re-import the
// console asks a reader for.
func googleInstall(t *testing.T, ds substrate.Dataset, rewrites ...[2]string) {
	t.Helper()
	installRewired(t, ds, googleDir, rewrites...)
}

// googleSeedAccount creates the connection record the mirrors point at:
// `account` is a trait-pinned cascading reference (0034). A reference does not
// refuse a missing target at write, but the account must exist for the
// cascade to collect the rows it owns and for a read to resolve the pointer,
// so the sync seeds it. The injected config carries the properties the body
// reads.
func googleSeedAccount(t *testing.T, ds substrate.Dataset, id string) {
	t.Helper()
	mustPut(t, ds, substrate.PutInput{
		Kind: googleAccountType, ID: id,
		Properties: map[string]any{"enabledGmail": true, "enabledCalendar": true},
	})
}

// googlePersonOf reads the person a source record's subject slot resolved to.
func googlePersonOf(t *testing.T, ds substrate.Dataset, kind, id string) string {
	t.Helper()
	row, err := ds.Get(context.Background(), kind, id)
	if err != nil {
		t.Fatalf("get %s %s: %v", kind, id, err)
	}
	ids := refIDs(row, "person")
	if len(ids) != 1 {
		t.Fatalf("%s %s points at %d persons, want 1", kind, id, len(ids))
	}
	return ids[0]
}
