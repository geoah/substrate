package substrate

import (
	"context"
	"crypto/ed25519"
	"time"
)

// PUSH (docs/plans/push.md). The engine finds what happened in the
// transactions that make it and hands each happening to a PushSink it was
// given at start; it sends nothing itself. The keys a push is signed and its
// event id sealed with are derived inside the engine from the repository's
// DEK and one token's id, and what leaves the engine is a PushSigner for
// that token, never the DEK.

// The events a push carries, a closed set the relay holds too.
const (
	PushEventReply   = "reply"
	PushEventFailed  = "failed"
	PushEventRequest = "request"
	PushEventRead    = "read"
)

// The two classes of event a device record's `notifies` names: `replies`
// takes a reply and a failure, `requests` a request.
const (
	PushNotifiesReplies  = "replies"
	PushNotifiesRequests = "requests"
)

// PushItem names the exact record a push is about, at its version.
type PushItem struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

// PushEvent is what an event id seals: the event, the thread it happened on
// and when it was issued, with the item a reply, a failure or a request is
// about, or, for a read, the fingerprints of the alerts it takes back.
type PushEvent struct {
	Event    string
	Thread   string
	Item     PushItem
	Alerts   []string
	IssuedAt time.Time
}

// PushEventLifetime is how long an event id resolves after it was issued.
const PushEventLifetime = 24 * time.Hour

// PushSigner signs and seals for one token's pushes. The engine derives it
// from the repository's DEK and the token's id; the DEK never leaves it.
type PushSigner interface {
	// PublicKey is the public half of the key this token's pushes are
	// signed with, which the install hands the relay at register.
	PublicKey() ed25519.PublicKey
	// Sign is the Substrate-Push-Signature value for body at t.
	Sign(t time.Time, body []byte) string
	// SealEvent seals e into an event id only this token's signer opens.
	SealEvent(e PushEvent) (string, error)
	// OpenEvent opens an event id; ErrNotFound when it does not open, is
	// another token's, or is past PushEventLifetime.
	OpenEvent(id string) (PushEvent, error)
}

// PushSignalKind is what happened, as the engine saw it commit.
type PushSignalKind string

const (
	// PushSettled is a thread's turn settling.
	PushSettled PushSignalKind = "settled"
	// PushRaised is a record of a thread starting to wait on a person
	// (`waitingOnPerson` rose), and PushDecided one that stopped waiting
	// (it fell).
	PushRaised  PushSignalKind = "raised"
	PushDecided PushSignalKind = "decided"
	// PushMarked is a person's mark of a thread moving forward, clearing
	// `markedUnread` or setting `archived`.
	PushMarked PushSignalKind = "marked"
)

// PushSignal is one happening the engine hands its sink, after the
// transaction that made it committed.
type PushSignal struct {
	Kind PushSignalKind
	// Repository is the id of the repository it happened in.
	Repository string
	Thread     string
	// Mode is the thread's mode, and Person the id of the actor record of
	// the person it acts for (`heldTo.onBehalfOf`), "" for a thread that
	// acts for nobody. On a mark, Person is the mark's own person.
	Mode   string
	Person string
	At     time.Time
	// A settle: the status it settled to, the thread's version after it,
	// when its turn began, and the message `lastMessage` moved to when that
	// message is an assistant's ("" otherwise).
	Status        string
	ThreadVersion int64
	TurnStarted   time.Time
	Message       string
	// A raise or a decide: the record, at the version the write left it.
	Item PushItem
	// A mark or a decide: the token the write came with, whose own device
	// is never sent the `read` it causes.
	Token string
}

// PushSink receives the engine's push signals. It must not block: the
// engine calls it after a commit, on the writer's goroutine.
type PushSink interface {
	PushSignal(PushSignal)
}

// PushDevice is one live device record, as the dispatcher reads it, with
// the token it is named by.
type PushDevice struct {
	ID       string
	Person   string
	PushKey  string
	Notifies []string
	Token    TokenInfo
}

// PushCheck is one alert the dispatcher asks the engine about again, after
// its hold or when a `read` may cover it.
type PushCheck struct {
	Event  string
	Person string
	Thread string
	Item   PushItem
	// Message is the message a reply names, and TurnStarted when a
	// failure's turn began.
	Message     string
	TurnStarted time.Time
	// Request is true for a request alert a `read` asks about: the mark is
	// not consulted, only whether the record still waits.
	Request bool
}

// The answers PushRecheck gives: send, or why not.
const (
	PushSend     = ""
	PushSeen     = "seen"
	PushArchived = "archived"
)

// PushStore is what push asks of the engine beside a dataset's ordinary
// reads. The engine's dataset implements it; nothing else does.
type PushStore interface {
	// PushSigner is the signer of one token's pushes.
	PushSigner(ctx context.Context, tokenID string) (PushSigner, error)
	// PushPerson is the id of the actor record of the person a request's
	// ctx acts for, "" for none.
	PushPerson(ctx context.Context) (string, error)
	// PushDevices are the live devices of the person whose actor record is
	// person, whose token is live and not expired.
	PushDevices(ctx context.Context, person string) ([]PushDevice, error)
	// PushAdmins are the actor records of the people who hold `admin`.
	PushAdmins(ctx context.Context) ([]string, error)
	// PushRecheck answers whether an alert is still worth sending, or what
	// covers it now.
	PushRecheck(ctx context.Context, c PushCheck) (string, error)
	// PushPending reports whether item still waits on a person at its
	// version.
	PushPending(ctx context.Context, item PushItem) (bool, error)
	// PushBadgeThreads are the person's unread candidates for the badge:
	// the live `chat` threads not running, unread for the person, not
	// archived unless something waits on them. The caller keeps those the
	// token's view reads and counts to PushBadgeMax.
	PushBadgeThreads(ctx context.Context, person string) ([]*Record, error)
	// DeletePushDevice deletes the device id as the system, only while it
	// still holds pushKey, and reports whether it did.
	DeletePushDevice(ctx context.Context, id, pushKey string) (bool, error)
}

// PushBadgeMax is the one cap on the badge: the server's count, the relay's
// bound and the app's display.
const PushBadgeMax = 999

// PushKey answers GET /api/v1/push/key: the asking token's public key, and
// the relay the server will send its pushes to, which the app compares with
// its build and its publisher's document and never sends to.
type PushKey struct {
	PublicKey string `json:"publicKey"`
	Relay     string `json:"relay"`
}

// PushBadge answers GET /api/v1/push/badge: the asking token's unread count,
// the number a push sets.
type PushBadge struct {
	Badge int `json:"badge"`
}

// PushEventInfo answers GET /api/v1/push/events/{eventId}: what an event id
// names. A reply, a failure and a request carry their item; a request says
// whether the asking token may decide it now; a read says what it covers.
type PushEventInfo struct {
	Event   string      `json:"event"`
	Thread  string      `json:"thread"`
	Item    *PushItem   `json:"item,omitempty"`
	Actions *bool       `json:"actions,omitempty"`
	Covers  *PushCovers `json:"covers,omitempty"`
}

// PushCovers is what a read takes back: the fingerprints of the event ids
// of the alerts it covers (pushwire.Fingerprint).
type PushCovers struct {
	Alerts []string `json:"alerts"`
}
