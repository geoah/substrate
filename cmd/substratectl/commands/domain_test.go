package commands

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func (f *fakeSubstrate) handleDomain(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	writeJSON(w, http.StatusOK, substrate.DomainProof{
		Name:   "_substrate.ada.example.org",
		Record: "v=substrate1 server=https://substrate.example.com proof=k3j9x2abcd",
		Proof:  "k3j9x2abcd",
	})
}

func (f *fakeSubstrate) handleDomainVerify(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	f.mu.Lock()
	published := f.domainRecordPublished
	f.mu.Unlock()
	if !published {
		writeJSON(w, http.StatusOK, substrate.DomainCheck{Reason: "no TXT record opening with \"v=substrate1\" at _substrate.ada.example.org"})
		return
	}
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	writeJSON(w, http.StatusOK, substrate.DomainCheck{Verified: true, VerifiedAt: &at})
}

func TestDomainShowPrintsTheRecordToAdd(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _ := h.mustRun("domain", "show")
	for _, want := range []string{
		"_substrate.ada.example.org", "TXT",
		`"v=substrate1 server=https://substrate.example.com proof=k3j9x2abcd"`,
		"substratectl domain verify",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("domain show lacks %q:\n%s", want, out)
		}
	}
}

func TestDomainVerifyPrintsTheReasonAndFailsUntilTheRecordPasses(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	out, _, err := h.run("domain", "verify")
	if !errors.Is(err, errDomainUnverified) {
		t.Fatalf("domain verify before the record: err %v, want errDomainUnverified", err)
	}
	if !strings.Contains(out, "not verified: no TXT record") {
		t.Errorf("domain verify did not print the reason:\n%s", out)
	}

	h.fake.mu.Lock()
	h.fake.domainRecordPublished = true
	h.fake.mu.Unlock()
	out, _ = h.mustRun("domain", "verify")
	if !strings.Contains(out, "verified 2026-11-02T12:00:00Z") {
		t.Errorf("domain verify after the record:\n%s", out)
	}
}
