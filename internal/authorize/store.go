package authorize

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

// The lifecycle lives in the server's memory: a restart forgets every parked
// request and code, and the app starts over. One server process writes a
// repository (decision 0083), so no second process needs the state, and the
// token the flow ends in is a record in the changelog like any other.
const (
	// RequestTTL is how long a parked request waits for the owner, again
	// after a set up of it failed, and for its hand-over once one finished.
	// An approval refused before its set up ran gives the request back the
	// lapse it had, so refusing never keeps a request parked longer.
	RequestTTL = 10 * time.Minute
	// CodeTTL is how long a code may be exchanged once its app can hold it
	// (an app exchanges within seconds).
	CodeTTL = 60 * time.Second
	// maxCodeLife is OAuth 2.1 §4.1.2's cap on a code: a set up's code waits
	// for the page that reads it, and lives no longer than this from Finish
	// whenever that read comes.
	maxCodeLife = 10 * time.Minute
	// SetUpTTL is how long a request a set up reserved waits for it: the set
	// up's own bound, so a run that hangs lapses with its request.
	SetUpTTL = 15 * time.Minute
	// codeMemory is how long a redeemed code is remembered, so a replay can
	// still be recognized and revoke what the first exchange minted.
	codeMemory = 10 * time.Minute
	maxParked  = 1000
	maxCodes   = 1000
	// maxPerSource bounds the requests one source holds parked at once, so
	// a flood from one address (or one IPv6 /64) cannot take every slot of
	// maxParked from everybody else.
	maxPerSource = 32
)

var (
	// ErrNotPending is a request that does not exist, lapsed, or was
	// already answered: one answer for all three.
	ErrNotPending = errors.New("no such pending authorization request")
	// ErrFull is a store at its bound.
	ErrFull = errors.New("too many authorization requests in flight")
	// ErrBadCode is a code that does not exist or lapsed.
	ErrBadCode = errors.New("no such authorization code")
	// ErrSettingUp is a request whose set up is running: the set up answers
	// it when it ends, and nothing else answers it meanwhile.
	ErrSettingUp = errors.New("the authorization request is being set up")
	// ErrNotShown is a request the approval page has not shown, which no
	// set up may start for.
	ErrNotShown = errors.New("the approval page has not shown this authorization request")
)

// Request is a parked authorization request: what the app asked for, taken
// when GET /oauth/authorize checked it and never changed after, and where
// its answer stands (State and the fields after it).
type Request struct {
	ID          string
	Issuer      string
	Client      Client
	RedirectURI string
	// AppState is the app's opaque `state` parameter, said back on the
	// redirect.
	AppState  string
	Challenge string
	// Scopes are as the app wrote them, `~` and all; the approval resolves
	// them against the approving repository.
	Scopes []scope.Scope
	// Details are the app's checked `authorization_details` (ParseDetails):
	// what approving sets up.
	Details []substrate.AuthorizationDetail
	Created time.Time
	Expires time.Time
	// Repository is the repository that claimed the request: the first owner
	// to read it on the approval page. Every later read, approval or denial
	// must come from the same repository, so a request id that leaks does not
	// let another repository on this server answer it.
	Repository string
	// Source is who parked the request (an IPv4 address or an IPv6 /64), for
	// the per-source bound.
	Source string
	// Document is the app's document as the approval page showed it, taken
	// when the page read the request. Approval and denial check the redirect
	// against this snapshot, never a later fetch, so what the owner approves
	// is what the owner read.
	Document *Document

	// State is substrate.AuthorizationPending until an approval reserves the
	// request (Reserve), AuthorizationSettingUp while the approval is checked
	// and its set up runs, and AuthorizationApproved once the set up landed
	// and the code exists (Finish), until the page reads where to go
	// (HandOver).
	State string
	// Setup is the rows an approval was checked against, each carrying its
	// progress, from the moment its set up starts (Begin), and PlanHash their
	// digest: the page reads these while the set up runs and after it stops,
	// never a plan that moved as rows landed. They outlive a failed run, so
	// the page shows what landed, and go when the plan moved from what was
	// approved (Replan). Failure is why the last run stopped.
	Setup    []substrate.AuthorizationSetup
	PlanHash string
	Failure  *substrate.AuthorizationFailure
	// Answer is the approval page's whole view of the request once approved,
	// its redirect carrying the code among it, built as the set up finished
	// (Finish): the read that hands it over (HandOver) answers it as it is
	// and reads nothing else, so no read that fails can keep an issued code
	// from the page.
	Answer *substrate.AuthorizationRequest

	// code is the hash of the code Finish minted, whose clock HandOver
	// starts.
	code string
	// waited is when the request lapsed before Reserve moved its lapse to
	// the set up's bound, which an approval refused before its set up ran
	// gives back.
	waited time.Time
}

// snapshot is a copy of r a caller may keep: the rows are the store's to
// update while a set up runs.
func (r *Request) snapshot() Request {
	cp := *r
	cp.Setup = slices.Clone(r.Setup)
	return cp
}

// Approval is what the owner approved, bound to the code.
type Approval struct {
	Repository string
	// Dataset is the approving repository's handle: the exchange mints the
	// token through it, so the code never names a repository a later
	// request could swap.
	Dataset    substrate.Dataset
	Scopes     []string
	Lifetime   time.Duration
	ClientName string
	// Details are what the approval set up, or found in place, which the
	// token response lists.
	Details []substrate.AuthorizationDetail
}

// Code is a code's bookkeeping.
type Code struct {
	Request  Request
	Approval Approval
	Expires  time.Time
	// Redeemed is set by the first exchange that presented the code, and
	// MintedTokenID by that exchange once its token exists.
	Redeemed      bool
	MintedTokenID string
	// revokeOnMint is set when a replay that proved every binding arrives
	// before the first exchange finished minting.
	revokeOnMint bool
	forgetAt     time.Time
}

// Store holds parked requests and codes.
type Store struct {
	now func() time.Time

	mu       sync.Mutex
	requests map[string]*Request
	codes    map[string]*Code // by the code's SHA-256
}

// NewStore builds an empty store.
func NewStore(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{now: now, requests: map[string]*Request{}, codes: map[string]*Code{}}
}

// Park stores a checked request under a fresh 128-bit id.
func (s *Store) Park(r Request) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	if len(s.requests) >= maxParked {
		return Request{}, ErrFull
	}
	if r.Source != "" {
		held := 0
		for _, p := range s.requests {
			if p.Source == r.Source {
				held++
			}
		}
		if held >= maxPerSource {
			return Request{}, ErrFull
		}
	}
	r.ID = randomText(16)
	r.State = substrate.AuthorizationPending
	r.Created = s.now()
	r.Expires = r.Created.Add(RequestTTL)
	cp := r
	s.requests[r.ID] = &cp
	return r, nil
}

// Claim answers a request for repository, binding it to that repository on
// the first call. A request being set up, or approved after a set up and not
// yet handed over, is answered too, to the repository that claimed it alone,
// so the page can follow the set up it started. Reading changes nothing
// else: an approved request's Answer is handed over by HandOver alone.
func (s *Store) Claim(id, repository string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.liveLocked(id, repository)
	if err != nil {
		return Request{}, err
	}
	r.Repository = repository
	return r.snapshot(), nil
}

// Snapshot stores the app's document as the approval page first showed it,
// and answers the snapshot the request holds. The first one stands: a later
// read of the same request is shown the same document, so an approval from a
// page loaded earlier is never checked against one it did not show.
func (s *Store) Snapshot(id, repository string, doc Document) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.liveLocked(id, repository)
	if err != nil {
		return Document{}, err
	}
	r.Repository = repository
	if r.Document == nil {
		r.Document = &doc
	}
	return *r.Document, nil
}

// liveLocked is a request that has not lapsed and is not claimed by another
// repository, in whatever state.
func (s *Store) liveLocked(id, repository string) (*Request, error) {
	r, ok := s.requests[id]
	if !ok || !s.now().Before(r.Expires) || (r.Repository != "" && r.Repository != repository) {
		return nil, ErrNotPending
	}
	return r, nil
}

// pendingLocked is a live request waiting for an answer: one being set up
// answers ErrSettingUp, one already approved ErrNotPending.
func (s *Store) pendingLocked(id, repository string) (*Request, error) {
	r, err := s.liveLocked(id, repository)
	if err != nil {
		return nil, err
	}
	switch r.State {
	case substrate.AuthorizationSettingUp:
		return nil, ErrSettingUp
	case substrate.AuthorizationApproved:
		return nil, ErrNotPending
	}
	return r, nil
}

// settingUpLocked is a live request a set up reserved for repository.
func (s *Store) settingUpLocked(id, repository string) (*Request, error) {
	r, err := s.liveLocked(id, repository)
	if err != nil {
		return nil, err
	}
	if r.State != substrate.AuthorizationSettingUp || r.Repository != repository {
		return nil, ErrNotPending
	}
	return r, nil
}

// Approve answers a request its approval reserved and found nothing to set
// up for, once: check accepts the approval under the lock, the code's
// secret is the caller's to answer, shown this once, and the request is
// gone. A refused check leaves the request reserved, for the caller to Fail.
func (s *Store) Approve(id, repository string, check func(Request) (Approval, error)) (string, Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	r, err := s.settingUpLocked(id, repository)
	if err != nil {
		return "", Request{}, err
	}
	code, err := s.mintLocked(r, check)
	if err != nil {
		return "", Request{}, err
	}
	delete(s.requests, id)
	return code, r.snapshot(), nil
}

// mintLocked checks an approval of r and binds a fresh code to it.
func (s *Store) mintLocked(r *Request, check func(Request) (Approval, error)) (string, error) {
	if len(s.codes) >= maxCodes {
		return "", ErrFull
	}
	a, err := check(r.snapshot())
	if err != nil {
		return "", err
	}
	code := randomText(32)
	now := s.now()
	s.codes[HashCode(code)] = &Code{
		Request: *r, Approval: a, Expires: now.Add(CodeTTL), forgetAt: now.Add(codeMemory),
	}
	return code, nil
}

// Reserve moves a pending request to setting-up for its approval, once: the
// request must be claimed by repository and shown, and check accepts the
// approval under the lock, so a second approval or a denial meanwhile is
// refused with ErrSettingUp while the approval is checked and its set up
// runs. The request then lapses SetUpTTL from now, the set up's own bound.
// A refusal leaves it pending.
func (s *Store) Reserve(id, repository string, check func(Request) error) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	r, err := s.pendingLocked(id, repository)
	if err != nil {
		return Request{}, err
	}
	// A request is shown by the read that claims it, so one with no
	// snapshot is unshown wherever it is asked from.
	if r.Document == nil {
		return Request{}, ErrNotShown
	}
	if r.Repository != repository {
		return Request{}, ErrNotPending
	}
	if err := check(r.snapshot()); err != nil {
		return Request{}, err
	}
	r.State = substrate.AuthorizationSettingUp
	r.waited, r.Expires = r.Expires, s.now().Add(SetUpTTL)
	return r.snapshot(), nil
}

// Begin records, as a reserved request's set up starts, the rows its
// approval was checked against, each carrying its first progress, and their
// digest. A set up that starts again ends the last one's failure.
func (s *Store) Begin(id, repository string, rows []substrate.AuthorizationSetup, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.settingUpLocked(id, repository)
	if err != nil {
		return err
	}
	r.Setup, r.PlanHash, r.Failure = slices.Clone(rows), hash, nil
	return nil
}

// Progress records each row's progress as the run reaches it, in Setup's
// order.
func (s *Store) Progress(id, repository string, progress []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.settingUpLocked(id, repository)
	if err != nil {
		return err
	}
	for i, p := range progress {
		if i < len(r.Setup) {
			r.Setup[i].Progress = p
		}
	}
	return nil
}

// Fail moves a request being set up back to pending, with why the set up
// stopped; a nil failure leaves the last one as it was, for an approval
// refused before its set up ran. The rows stay with what landed: one the run
// left running stopped part way and reads as failed, one left waiting never
// ran and reads as neither. A set up that failed waits RequestTTL again for
// the owner to try again or deny; a refused approval lapses when the request
// did before Reserve, so refusals never keep a request parked.
func (s *Store) Fail(id, repository string, failure *substrate.AuthorizationFailure) error {
	return s.stop(id, repository, failure, false)
}

// Replan is Fail for a set up that stopped because what approving sets up
// moved from what was approved: the rows go with their digest, so the page
// plans the request again and the owner approves what it is now.
func (s *Store) Replan(id, repository string, failure *substrate.AuthorizationFailure) error {
	return s.stop(id, repository, failure, true)
}

func (s *Store) stop(id, repository string, failure *substrate.AuthorizationFailure, replan bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.settingUpLocked(id, repository)
	if err != nil {
		return err
	}
	r.State = substrate.AuthorizationPending
	if failure != nil {
		f := *failure
		r.Failure = &f
	}
	if replan {
		r.Setup, r.PlanHash = nil, ""
	}
	for i := range r.Setup {
		switch r.Setup[i].Progress {
		case substrate.ProgressRunning:
			r.Setup[i].Progress = substrate.ProgressFailed
		case substrate.ProgressWaiting:
			r.Setup[i].Progress = ""
		}
	}
	r.Expires = r.waited
	if failure != nil {
		r.Expires = s.now().Add(RequestTTL)
	}
	return nil
}

// Finish approves a request its set up reserved, once the set up landed:
// check accepts the approval under the lock and the code is minted as
// Approve mints it. The request stays readable to repository as approved,
// holding answer(request, code), the page's view of it carrying the redirect
// the page that polls it navigates to, RequestTTL from now, since a page in a
// background tab may not be polling when the set up ends. answer runs under
// the lock and reads nothing. Until HandOver nobody holds the code, so it
// lives until then, and never past maxCodeLife from now. Every later
// approval or denial answers ErrNotPending. A refused check leaves the
// request setting up, for the caller to Fail.
func (s *Store) Finish(id, repository string, check func(Request) (Approval, error), answer func(Request, string) substrate.AuthorizationRequest) (string, Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	r, err := s.settingUpLocked(id, repository)
	if err != nil {
		return "", Request{}, err
	}
	code, err := s.mintLocked(r, check)
	if err != nil {
		return "", Request{}, err
	}
	now := s.now()
	r.State = substrate.AuthorizationApproved
	r.Failure = nil
	r.Expires = now.Add(RequestTTL)
	a := answer(r.snapshot(), code)
	r.Answer = &a
	r.code = HashCode(code)
	c := s.codes[r.code]
	c.Expires = now.Add(maxCodeLife)
	c.forgetAt = c.Expires.Add(codeMemory)
	return code, r.snapshot(), nil
}

// HandOver answers an approved request once, to the repository that claimed
// it, and forgets it, as Approve does: a later read finds no request. The
// code lives CodeTTL from here, and never past the cap Finish set.
func (s *Store) HandOver(id, repository string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.liveLocked(id, repository)
	if err != nil {
		return Request{}, err
	}
	if r.State != substrate.AuthorizationApproved || r.Repository != repository {
		return Request{}, ErrNotPending
	}
	if c := s.codes[r.code]; c != nil && !c.Redeemed {
		if handed := s.now().Add(CodeTTL); handed.Before(c.Expires) {
			c.Expires = handed
		}
	}
	delete(s.requests, id)
	return r.snapshot(), nil
}

// Deny moves a pending request to denied, once, after check (which verifies
// the redirect the denial navigates to) accepts it.
func (s *Store) Deny(id, repository string, check func(Request) error) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.pendingLocked(id, repository)
	if err != nil {
		return Request{}, err
	}
	if err := check(r.snapshot()); err != nil {
		return Request{}, err
	}
	delete(s.requests, id)
	return r.snapshot(), nil
}

// Redeem presents a code, with check deciding whether the presentation
// proves every binding (the issuer, the client, the redirect and the PKCE
// verifier). A presentation that does not prove them consumes nothing, so a
// leaked code alone cannot spend it out from under the app. The first one
// that does, within the code's lifetime, redeems it and answers first=true;
// a later one that does answers first=false, which is a replay of a code
// that worked (OAuth 2.1 §4.1.3).
func (s *Store) Redeem(code string, check func(Code) error) (Code, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	c, ok := s.codes[HashCode(code)]
	if !ok {
		return Code{}, false, ErrBadCode
	}
	if !c.Redeemed && !s.now().Before(c.Expires) {
		return Code{}, false, ErrBadCode
	}
	if err := check(*c); err != nil {
		return Code{}, false, err
	}
	if c.Redeemed {
		if c.MintedTokenID == "" {
			c.revokeOnMint = true
		}
		return *c, false, nil
	}
	c.Redeemed = true
	return *c, true, nil
}

// Minted records the token the first exchange minted. It answers true when a
// verified replay already asked for that token to be revoked.
func (s *Store) Minted(code, tokenID string) (revoke bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.codes[HashCode(code)]
	if !ok {
		return false
	}
	c.MintedTokenID = tokenID
	return c.revokeOnMint
}

// sweep drops what has lapsed. The store is bounded, so a pass over it is
// cheap; it runs under the lock on every write.
func (s *Store) sweep() {
	now := s.now()
	for id, r := range s.requests {
		if !now.Before(r.Expires) {
			delete(s.requests, id)
		}
	}
	for h, c := range s.codes {
		if !now.Before(c.forgetAt) {
			delete(s.codes, h)
		}
	}
}

// HashCode is the key a code is stored under: its secret never is.
func HashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// randomText is n random bytes as lowercase hex.
func randomText(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("authorize: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
