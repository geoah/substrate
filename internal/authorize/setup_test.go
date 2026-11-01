package authorize

import (
	"errors"
	"strings"
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
	if r.State != substrate.AuthorizationPending {
		t.Fatalf("a parked request is %q, want pending", r.State)
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
	if _, err := s.Reserve(unshown.ID, "ada.example.com", accept); !errors.Is(err, ErrNotShown) {
		t.Fatalf("reserving an unclaimed request = %v, want ErrNotShown", err)
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
	if got, _ := s.Claim(r.ID, "ada.example.com"); got.State != substrate.AuthorizationPending {
		t.Fatalf("a refused reservation left the request %q, want pending", got.State)
	}

	got, err := s.Reserve(r.ID, "ada.example.com", accept)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != substrate.AuthorizationSettingUp || !got.Expires.Equal(now.Add(SetUpTTL)) {
		t.Fatalf("reserved = %q lapsing %v, want setting-up lapsing at %v", got.State, got.Expires, now.Add(SetUpTTL))
	}
	// Nothing else answers it while it runs: a second approval's
	// reservation and a denial are each refused as setting up.
	if _, err := s.Reserve(r.ID, "ada.example.com", accept); !errors.Is(err, ErrSettingUp) {
		t.Fatalf("a second reservation = %v, want ErrSettingUp", err)
	}
	if _, err := s.Deny(r.ID, "ada.example.com", accept); !errors.Is(err, ErrSettingUp) {
		t.Fatalf("a denial while setting up = %v, want ErrSettingUp", err)
	}
	// Its repository still reads it; another does not, and cannot touch it.
	if read, err := s.Claim(r.ID, "ada.example.com"); err != nil || read.State != substrate.AuthorizationSettingUp {
		t.Fatalf("reading it while it runs = %q, %v", read.State, err)
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
	if err := s.Begin(r.ID, "ada.example.com", threeRows(substrate.ProgressWaiting), "hash"); err != nil {
		t.Fatal(err)
	}
	if err := s.Progress(r.ID, "ada.example.com", []string{substrate.ProgressDone, substrate.ProgressRunning, substrate.ProgressWaiting}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Claim(r.ID, "ada.example.com"); progressOf(got) != "done,running,waiting" || got.PlanHash != "hash" {
		t.Fatalf("while it runs = %q %q", progressOf(got), got.PlanHash)
	}
	now = now.Add(time.Minute)
	// The run stopped while a row ran and another waited: the one stopped
	// part way, the other never ran.
	row := 1
	failure := &substrate.AuthorizationFailure{Row: &row, Message: "the install was refused"}
	if err := s.Fail(r.ID, "ada.example.com", failure); err != nil {
		t.Fatal(err)
	}
	got, err := s.Claim(r.ID, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != substrate.AuthorizationPending || got.Failure == nil || got.Failure.Message != failure.Message ||
		*got.Failure.Row != 1 || progressOf(got) != "done,failed," || got.PlanHash != "hash" || !got.Expires.Equal(now.Add(RequestTTL)) {
		t.Fatalf("after a failure = %+v, progress %q", got, progressOf(got))
	}
	// A row a reader holds is its own: the store's rows move on without it.
	got.Setup[0].Progress = "edited"
	if again, _ := s.Claim(r.ID, "ada.example.com"); again.Setup[0].Progress != substrate.ProgressDone {
		t.Fatal("a reader's copy of the rows edited the store's")
	}
	// Progress and failures land only while a set up runs.
	if err := s.Progress(r.ID, "ada.example.com", nil); !errors.Is(err, ErrNotPending) {
		t.Fatalf("progress on a pending request = %v", err)
	}
	if err := s.Fail(r.ID, "ada.example.com", failure); !errors.Is(err, ErrNotPending) {
		t.Fatalf("a failure of a pending request = %v", err)
	}
	// Try again: a refusal before the run keeps the last failure and its
	// rows, and the run's start clears the failure.
	if _, err := s.Reserve(r.ID, "ada.example.com", accept); err != nil {
		t.Fatal(err)
	}
	if err := s.Fail(r.ID, "ada.example.com", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Claim(r.ID, "ada.example.com"); got.Failure == nil || len(got.Setup) != 3 {
		t.Fatal("a refusal before the run dropped the last failure or its rows")
	}
	if _, err := s.Reserve(r.ID, "ada.example.com", accept); err != nil {
		t.Fatal(err)
	}
	if err := s.Begin(r.ID, "ada.example.com", threeRows(substrate.ProgressDone), "hash"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Claim(r.ID, "ada.example.com"); got.Failure != nil {
		t.Fatalf("a run that started again kept the failure %+v", got.Failure)
	}
	// A set up whose plan moved from what was approved leaves no rows, so
	// the page plans the request again.
	if err := s.Replan(r.ID, "ada.example.com", &substrate.AuthorizationFailure{Message: "moved"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Claim(r.ID, "ada.example.com"); got.Setup != nil || got.PlanHash != "" || got.Failure == nil {
		t.Fatalf("after a replan = %+v", got)
	}
	// The denial is open again once the run stopped.
	if _, err := s.Deny(r.ID, "ada.example.com", accept); err != nil {
		t.Fatalf("denying after a failed set up = %v", err)
	}
}

// threeRows is three package rows carrying the progress p.
func threeRows(p string) []substrate.AuthorizationSetup {
	rows := make([]substrate.AuthorizationSetup, 3)
	for i := range rows {
		rows[i] = substrate.AuthorizationSetup{Type: substrate.DetailPackage, Action: substrate.SetupInstall, Progress: p}
	}
	return rows
}

func progressOf(r Request) string {
	out := make([]string, len(r.Setup))
	for i, row := range r.Setup {
		out[i] = row.Progress
	}
	return strings.Join(out, ",")
}

// finished reserves, starts and finishes a set up of a shown request.
func finished(t *testing.T, s *Store) Request {
	t.Helper()
	r := shownRequest(t, s)
	if _, err := s.Reserve(r.ID, "ada.example.com", accept); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Finish(r.ID, "ada.example.com", approval, redirectWith); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAFinishedSetUpMintsOneCodeAndIsAnsweredOnce(t *testing.T) {
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
	if got, _ := s.Claim(r.ID, "ada.example.com"); got.State != substrate.AuthorizationSettingUp {
		t.Fatalf("a refused finish left the request %q, want setting-up", got.State)
	}
	code, got, err := s.Finish(r.ID, "ada.example.com", approval, redirectWith)
	if err != nil || len(code) != 64 {
		t.Fatalf("finish = %q, %v", code, err)
	}
	if got.State != substrate.AuthorizationApproved || got.Next != redirectWith(got, code) {
		t.Fatalf("finished = %+v", got)
	}
	// Reading it hands nothing over: the page that reads it decides when.
	if read, err := s.Claim(r.ID, "ada.example.com"); err != nil || read.Next == "" {
		t.Fatalf("reading the approved request = %+v, %v", read, err)
	}
	if _, err := s.Claim(r.ID, "eve.example.com"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("another repository read the approved request: %v", err)
	}
	if _, err := s.HandOver(r.ID, "eve.example.com"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("another repository's hand-over = %v", err)
	}
	for name, answer := range map[string]func() error{
		"approving it again":   func() error { _, _, err := s.Approve(r.ID, "ada.example.com", approval); return err },
		"denying it":           func() error { _, err := s.Deny(r.ID, "ada.example.com", accept); return err },
		"setting it up again":  func() error { _, err := s.Reserve(r.ID, "ada.example.com", accept); return err },
		"finishing it again":   func() error { _, _, err := s.Finish(r.ID, "ada.example.com", approval, redirectWith); return err },
		"failing it after all": func() error { return s.Fail(r.ID, "ada.example.com", nil) },
	} {
		if err := answer(); !errors.Is(err, ErrNotPending) {
			t.Fatalf("%s = %v, want ErrNotPending", name, err)
		}
	}
	// Answered once: the hand-over is the last read.
	handed, err := s.HandOver(r.ID, "ada.example.com")
	if err != nil || handed.Next != got.Next {
		t.Fatalf("the hand-over = %+v, %v", handed, err)
	}
	if _, err := s.Claim(r.ID, "ada.example.com"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("reading it after the hand-over = %v, want ErrNotPending", err)
	}
	if _, err := s.HandOver(r.ID, "ada.example.com"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("a second hand-over = %v, want ErrNotPending", err)
	}
	// The code redeems like an approval's.
	c, first, err := s.Redeem(code, func(Code) error { return nil })
	if err != nil || !first || c.Approval.Scopes[0] != "read:*" {
		t.Fatalf("redeeming the code = %+v, %v, %v", c, first, err)
	}
}

// A page in a background tab may not be polling when the set up ends: the
// request waits for the read that hands it where to go, and the code's clock
// starts there, never past the cap from the set up's end.
func TestAFinishedSetUpsCodeLivesFromItsHandOver(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := NewStore(func() time.Time { return now })
	codeOf := func(r Request) string { return strings.TrimPrefix(r.Next, "https://a.example.org/cb?code=") }
	redeems := func(code string) bool {
		_, _, err := s.Redeem(code, func(Code) error { return nil })
		return err == nil
	}

	// Handed over two minutes after the set up: CodeTTL from then.
	r := finished(t, s)
	now = now.Add(2 * time.Minute)
	handed, err := s.HandOver(r.ID, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(CodeTTL)
	if redeems(codeOf(handed)) {
		t.Fatal("the code outlived CodeTTL from its hand-over")
	}
	r = finished(t, s)
	now = now.Add(2 * time.Minute)
	handed, _ = s.HandOver(r.ID, "ada.example.com")
	now = now.Add(CodeTTL - time.Second)
	if !redeems(codeOf(handed)) {
		t.Fatal("the code lapsed inside CodeTTL from its hand-over")
	}

	// Handed over just before the request lapses: the cap from the set up's
	// end, not CodeTTL from the read.
	r = finished(t, s)
	now = now.Add(RequestTTL - time.Second)
	handed, err = s.HandOver(r.ID, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if redeems(codeOf(handed)) {
		t.Fatal("a handed code outlived the cap from the set up's end")
	}

	// Nobody reads it: it lapses RequestTTL after the set up.
	r = finished(t, s)
	now = now.Add(RequestTTL)
	if _, err := s.Claim(r.ID, "ada.example.com"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("an approved request nobody read outlived RequestTTL: %v", err)
	}
	if _, err := s.HandOver(r.ID, "ada.example.com"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("handing over a lapsed request = %v", err)
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
