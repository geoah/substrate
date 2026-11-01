package authorize

import (
	"errors"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// shownRequest parks a request and has ada's page show it.
func shownRequest(t *testing.T, s *Store) Request {
	t.Helper()
	r, err := s.Park(Request{Issuer: "https://s.example.com", Client: Client{ID: "https://a.example.org/c"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != substrate.AuthorizationPending {
		t.Fatalf("a parked request is %q, want pending", r.Status)
	}
	if _, err := s.Snapshot(r.ID, "ada.example.com", Document{ClientID: r.Client.ID}); err != nil {
		t.Fatal(err)
	}
	return r
}

func accept(Request) error { return nil }

func approval(Request) (Approval, error) { return Approval{Scopes: []string{"read:*"}}, nil }

func redirectWith(_ Request, code string) string { return "https://a.example.org/cb?code=" + code }

func TestASetUpReservesTheRequestForTheRepositoryThatShowedIt(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := NewStore(func() time.Time { return now })

	// A request nobody showed cannot be set up, and claiming it alone is
	// not showing it.
	unshown, _ := s.Park(Request{})
	if _, err := s.Reserve(unshown.ID, "ada.example.com", accept); !errors.Is(err, ErrNotPending) {
		t.Fatalf("reserving an unclaimed request = %v, want ErrNotPending", err)
	}
	if _, err := s.Claim(unshown.ID, "ada.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(unshown.ID, "ada.example.com", accept); !errors.Is(err, ErrNotShown) {
		t.Fatalf("reserving an unshown request = %v, want ErrNotShown", err)
	}

	r := shownRequest(t, s)
	// Another repository is refused before anything runs, and learns
	// nothing a missing request would not say.
	if _, err := s.Reserve(r.ID, "eve.example.com", func(Request) error {
		t.Fatal("another repository's check ran")
		return nil
	}); !errors.Is(err, ErrNotPending) {
		t.Fatalf("another repository's reservation = %v, want ErrNotPending", err)
	}
	refuse := errors.New("refused")
	if _, err := s.Reserve(r.ID, "ada.example.com", func(Request) error { return refuse }); !errors.Is(err, refuse) {
		t.Fatalf("a refused reservation = %v", err)
	}
	if got, _ := s.Claim(r.ID, "ada.example.com"); got.Status != substrate.AuthorizationPending {
		t.Fatalf("a refused reservation left the request %q, want pending", got.Status)
	}

	got, err := s.Reserve(r.ID, "ada.example.com", accept)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != substrate.AuthorizationSettingUp || !got.Expires.Equal(now.Add(SetUpTTL)) {
		t.Fatalf("reserved = %q lapsing %v, want setting-up lapsing at %v", got.Status, got.Expires, now.Add(SetUpTTL))
	}
	// Nothing else answers it while it runs: a second approval, a second
	// set up and a denial are each refused as setting up.
	if _, _, err := s.Approve(r.ID, "ada.example.com", approval); !errors.Is(err, ErrSettingUp) {
		t.Fatalf("an approval while setting up = %v, want ErrSettingUp", err)
	}
	if _, err := s.Reserve(r.ID, "ada.example.com", accept); !errors.Is(err, ErrSettingUp) {
		t.Fatalf("a second reservation = %v, want ErrSettingUp", err)
	}
	if _, err := s.Deny(r.ID, "ada.example.com", accept); !errors.Is(err, ErrSettingUp) {
		t.Fatalf("a denial while setting up = %v, want ErrSettingUp", err)
	}
	// Its repository still reads it; another does not, and cannot touch it.
	if read, err := s.Claim(r.ID, "ada.example.com"); err != nil || read.Status != substrate.AuthorizationSettingUp {
		t.Fatalf("reading it while it runs = %q, %v", read.Status, err)
	}
	if _, err := s.Claim(r.ID, "eve.example.com"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("another repository read it: %v", err)
	}
	if err := s.Progress(r.ID, "eve.example.com", []string{"running"}); !errors.Is(err, ErrNotPending) {
		t.Fatalf("another repository's progress = %v", err)
	}
	if err := s.Fail(r.ID, "eve.example.com", nil); !errors.Is(err, ErrNotPending) {
		t.Fatalf("another repository's failure = %v", err)
	}
	if _, _, err := s.Finish(r.ID, "eve.example.com", approval, redirectWith); !errors.Is(err, ErrNotPending) {
		t.Fatalf("another repository's finish = %v", err)
	}
}

func TestAFailedSetUpIsPendingAgainWithWhatLanded(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := NewStore(func() time.Time { return now })
	r := shownRequest(t, s)
	if _, err := s.Reserve(r.ID, "ada.example.com", accept); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress(r.ID, "ada.example.com", []string{"done", "running"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	failure := &substrate.AuthorizationFailure{Row: 1, Message: "the install was refused"}
	if err := s.Fail(r.ID, "ada.example.com", failure); err != nil {
		t.Fatal(err)
	}
	got, err := s.Claim(r.ID, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != substrate.AuthorizationPending || got.Failure == nil || *got.Failure != *failure ||
		len(got.Progress) != 2 || got.Progress[0] != "done" || !got.Expires.Equal(now.Add(RequestTTL)) {
		t.Fatalf("after a failure = %+v", got)
	}
	// Progress and failures land only while a set up runs.
	if err := s.Progress(r.ID, "ada.example.com", nil); !errors.Is(err, ErrNotPending) {
		t.Fatalf("progress on a pending request = %v", err)
	}
	if err := s.Fail(r.ID, "ada.example.com", failure); !errors.Is(err, ErrNotPending) {
		t.Fatalf("a failure of a pending request = %v", err)
	}
	// Try again: a refusal before the run keeps the last failure, and the
	// run's first progress clears it.
	if _, err := s.Reserve(r.ID, "ada.example.com", accept); err != nil {
		t.Fatal(err)
	}
	if err := s.Fail(r.ID, "ada.example.com", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Claim(r.ID, "ada.example.com"); got.Failure == nil {
		t.Fatal("a refusal before the run dropped the last failure")
	}
	if _, err := s.Reserve(r.ID, "ada.example.com", accept); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress(r.ID, "ada.example.com", []string{"done", "done"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Claim(r.ID, "ada.example.com"); got.Failure != nil {
		t.Fatalf("a run that started again kept the failure %+v", got.Failure)
	}
	// The denial is open again once the run failed.
	if err := s.Fail(r.ID, "ada.example.com", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Deny(r.ID, "ada.example.com", accept); err != nil {
		t.Fatalf("denying after a failed set up = %v", err)
	}
}

func TestAFinishedSetUpMintsOneCodeAndStaysReadableUntilItLapses(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := NewStore(func() time.Time { return now })
	r := shownRequest(t, s)
	// A request nobody reserved is not finished.
	if _, _, err := s.Finish(r.ID, "ada.example.com", approval, redirectWith); !errors.Is(err, ErrNotPending) {
		t.Fatalf("finishing a pending request = %v, want ErrNotPending", err)
	}
	if _, err := s.Reserve(r.ID, "ada.example.com", accept); err != nil {
		t.Fatal(err)
	}
	// The approval's checks still run under the lock, and a refusal leaves
	// the set up to fail.
	refuse := errors.New("refused")
	if _, _, err := s.Finish(r.ID, "ada.example.com", func(Request) (Approval, error) { return Approval{}, refuse }, redirectWith); !errors.Is(err, refuse) {
		t.Fatalf("a refused finish = %v", err)
	}
	if got, _ := s.Claim(r.ID, "ada.example.com"); got.Status != substrate.AuthorizationSettingUp {
		t.Fatalf("a refused finish left the request %q, want setting-up", got.Status)
	}
	code, got, err := s.Finish(r.ID, "ada.example.com", approval, redirectWith)
	if err != nil || len(code) != 64 {
		t.Fatalf("finish = %q, %v", code, err)
	}
	if got.Status != substrate.AuthorizationApproved || got.Next != redirectWith(got, code) {
		t.Fatalf("finished = %+v", got)
	}
	// The code redeems like an approval's.
	c, first, err := s.Redeem(code, func(Code) error { return nil })
	if err != nil || !first || c.Approval.Scopes[0] != "read:*" {
		t.Fatalf("redeeming the code = %+v, %v, %v", c, first, err)
	}
	// Answered once: the page reads where to go, nothing answers it again.
	if read, err := s.Claim(r.ID, "ada.example.com"); err != nil || read.Next == "" {
		t.Fatalf("reading the approved request = %+v, %v", read, err)
	}
	if _, err := s.Claim(r.ID, "eve.example.com"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("another repository read the approved request: %v", err)
	}
	if _, _, err := s.Approve(r.ID, "ada.example.com", approval); !errors.Is(err, ErrNotPending) {
		t.Fatalf("approving it again = %v, want ErrNotPending", err)
	}
	if _, err := s.Deny(r.ID, "ada.example.com", accept); !errors.Is(err, ErrNotPending) {
		t.Fatalf("denying it after = %v, want ErrNotPending", err)
	}
	if _, err := s.Reserve(r.ID, "ada.example.com", accept); !errors.Is(err, ErrNotPending) {
		t.Fatalf("setting it up again = %v, want ErrNotPending", err)
	}
	if _, _, err := s.Finish(r.ID, "ada.example.com", approval, redirectWith); !errors.Is(err, ErrNotPending) {
		t.Fatalf("finishing it again = %v, want ErrNotPending", err)
	}
	now = now.Add(answeredTTL)
	if _, err := s.Claim(r.ID, "ada.example.com"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("the approved request outlived its lapse: %v", err)
	}
}

func TestASetUpLapsesWithItsBound(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := NewStore(func() time.Time { return now })
	r := shownRequest(t, s)
	if _, err := s.Reserve(r.ID, "ada.example.com", accept); err != nil {
		t.Fatal(err)
	}
	// Past the request's own lapse, inside the set up's.
	now = now.Add(RequestTTL + time.Minute)
	if err := s.Progress(r.ID, "ada.example.com", []string{"running"}); err != nil {
		t.Fatalf("a set up inside its bound = %v", err)
	}
	now = now.Add(SetUpTTL)
	if _, _, err := s.Finish(r.ID, "ada.example.com", approval, redirectWith); !errors.Is(err, ErrNotPending) {
		t.Fatalf("finishing a lapsed set up = %v, want ErrNotPending", err)
	}
	// Swept, so it holds no slot.
	if len(s.requests) != 0 {
		t.Fatalf("%d requests held after the lapse", len(s.requests))
	}
}
