// Package contract is the bridge's half of the wire the
// providers.substrate.reamde.dev/chatbridge package reads: one delivery is a
// batch of events, each with the next sequence number of this instance's
// stream (docs/plans/chat-bridge.md, section 6). The package's README is the
// contract once it lands; where the two differ, the package is right and this
// file follows it.
package contract

import (
	"encoding/json"
	"time"
)

// Version is the batch shape this bridge writes. The function refuses a
// version it does not know, so a newer bridge waits for the package.
const Version = 1

// The bounds of one delivery (section 6.1). MaxRequest is the webhook door's
// multipart bound; the JSON part counts against it too.
const (
	MaxEvents    = 200
	MaxBatchJSON = 1 << 20
	MaxMediaPart = 5 << 20
	MaxRequest   = 32 << 20
)

// BatchPart is the multipart part that carries the batch. It has no filename
// and a text media type, so the door hands it to the function inline.
const (
	BatchPart        = "batch"
	BatchContentType = "text/plain; charset=utf-8"
)

// Batch is one delivery: the events first..last of one instance's stream, in
// order. Events are kept as written to the outbox, byte for byte.
type Batch struct {
	Version  int               `json:"version"`
	Instance string            `json:"instance"`
	First    int64             `json:"first"`
	Last     int64             `json:"last"`
	Events   []json.RawMessage `json:"events"`
}

// The event types (section 6.3).
const (
	TypeInstanceHello    = "instance.hello"
	TypeAccountUpsert    = "account.upsert"
	TypeAccountState     = "account.state"
	TypeAccountRemoved   = "account.removed"
	TypeUserUpsert       = "user.upsert"
	TypeChatUpsert       = "chat.upsert"
	TypeChatDelete       = "chat.delete"
	TypeChatRead         = "chat.read"
	TypeMessageUpsert    = "message.upsert"
	TypeMessageDelete    = "message.delete"
	TypeMessageReactions = "message.reactions"
	TypeMessageStatus    = "message.status"
	TypeActionClaimed    = "action.claimed"
	TypeActionResult     = "action.result"
	TypeBackfillProgress = "backfill.progress"
)

// Instant is RFC 3339 in UTC with milliseconds.
type Instant string

// At formats t as an Instant.
func At(t time.Time) Instant {
	return Instant(t.UTC().Format("2006-01-02T15:04:05.000Z"))
}

// AtPtr is At for an optional field: the zero time is absent.
func AtPtr(t time.Time) *Instant {
	if t.IsZero() {
		return nil
	}
	at := At(t)
	return &at
}

// Null is a field that may be absent (unchanged), null (cleared) or set:
// `T | null` in the contract's notation. The zero value is absent.
type Null[T any] struct {
	set   bool
	value *T
}

// Set returns a Null holding v.
func Set[T any](v T) Null[T] { return Null[T]{set: true, value: &v} }

// Clear returns a Null that writes null.
func Clear[T any]() Null[T] { return Null[T]{set: true} }

// SetOrClear is Set for a non-empty value and Clear otherwise, for a field
// whose emptiness means there is none.
func SetOrClear[T comparable](v T) Null[T] {
	var zero T
	if v == zero {
		return Clear[T]()
	}
	return Set(v)
}

// IsZero reports an absent field, for `omitzero`.
func (n Null[T]) IsZero() bool { return !n.set }

// Value returns the held value, or nil for null or absent.
func (n Null[T]) Value() *T { return n.value }

// MarshalJSON writes null or the value.
func (n Null[T]) MarshalJSON() ([]byte, error) {
	if n.value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*n.value)
}

// UnmarshalJSON reads null or a value; it is only reached for a present key.
func (n *Null[T]) UnmarshalJSON(b []byte) error {
	n.set = true
	if string(b) == "null" {
		n.value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	n.value = &v
	return nil
}

// Event is any event of the stream. Head carries what every event has.
type Event interface {
	head() *Head
}

// Head is the part of an event every type shares. Seq is assigned by the
// outbox when the event is written. At is when it happened: on the network
// for a read marker, a deletion or a status, and when the bridge saw it for
// the rest.
type Head struct {
	Seq     int64   `json:"seq"`
	At      Instant `json:"at"`
	Type    string  `json:"type"`
	Network string  `json:"network,omitempty"`
	Account string  `json:"account,omitempty"`
}

func (h *Head) head() *Head { return h }

// HeadOf returns the head of an event, for the outbox to stamp.
func HeadOf(e Event) *Head { return e.head() }

// Media names a file part of the same request.
type Media struct {
	Part string `json:"part"`
}

// NetworkInfo names one network an instance can run.
type NetworkInfo struct {
	Network     string `json:"network"`
	DisplayName string `json:"displayName"`
}

// InstanceHello says which program and networks this instance is.
type InstanceHello struct {
	Head
	Name     string        `json:"name"`
	Version  string        `json:"version"`
	Networks []NetworkInfo `json:"networks"`
}

// AccountUpsert is one sign-in on one network.
type AccountUpsert struct {
	Head
	RemoteName  *string `json:"remoteName,omitempty"`
	Handle      *string `json:"handle,omitempty"`
	PhoneNumber *string `json:"phoneNumber,omitempty"`
	Self        *string `json:"self,omitempty"`
}

// The connection states of an account.
const (
	StateConnecting          = "connecting"
	StateConnected           = "connected"
	StateTransientDisconnect = "transient_disconnect"
	StateBadCredentials      = "bad_credentials"
	StateLoggedOut           = "logged_out"
	StateBanned              = "banned"
	StateUnknownError        = "unknown_error"
)

// The warning codes an account carries.
const (
	WarningPhoneOffline     = "phone_offline"
	WarningTemporaryBan     = "temporary_ban"
	WarningReachoutTimelock = "reachout_timelock"
	WarningClientOutdated   = "client_outdated"
	WarningFloodWait        = "flood_wait"
	WarningCheckpoint       = "checkpoint"
)

// Warning is one thing the network said about the account.
type Warning struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Until   *Instant `json:"until,omitempty"`
}

// AccountState is an account's connection and warnings. Warnings is always
// the whole set, so an empty list clears the earlier ones.
type AccountState struct {
	Head
	State    string    `json:"state"`
	Message  *string   `json:"message,omitempty"`
	Warnings []Warning `json:"warnings,omitzero"`
}

// AccountRemoved says the bridge logged the account out and forgot it.
type AccountRemoved struct {
	Head
}

// User is one person or account on a network, as one sign-in sees them.
type User struct {
	ID           string       `json:"id"`
	FullName     *string      `json:"fullName,omitempty"`
	Handle       *string      `json:"handle,omitempty"`
	PhoneNumbers []string     `json:"phoneNumbers,omitzero"`
	Emails       []string     `json:"emails,omitzero"`
	IsBot        *bool        `json:"isBot,omitempty"`
	IsSelf       *bool        `json:"isSelf,omitempty"`
	ContactName  Null[string] `json:"contactName,omitzero"`
	IsContact    *bool        `json:"isContact,omitempty"`
	Avatar       Null[Media]  `json:"avatar,omitzero"`
}

// UserUpsert writes one user.
type UserUpsert struct {
	Head
	User User `json:"user"`
}

// The chat categories.
const (
	CategoryDirect  = "direct"
	CategoryGroup   = "group"
	CategoryChannel = "channel"
	CategorySpace   = "space"
)

// Member is one user's place in a chat.
type Member struct {
	User       string  `json:"user"`
	Membership string  `json:"membership"`
	Role       *string `json:"role,omitempty"`
}

// The memberships and roles.
const (
	MembershipJoin   = "join"
	MembershipInvite = "invite"
	MembershipLeave  = "leave"
	MembershipBan    = "ban"

	RoleMember = "member"
	RoleAdmin  = "admin"
	RoleOwner  = "owner"
)

// Members is a chat's member list: the whole list when Complete, else the
// listed changes.
type Members struct {
	Complete bool     `json:"complete"`
	Items    []Member `json:"items"`
}

// Features is what a chat allows, read from bridgev2's room features.
type Features struct {
	Edit                bool     `json:"edit"`
	EditMaxAgeSeconds   *int64   `json:"editMaxAgeSeconds,omitempty"`
	DeleteForEveryone   bool     `json:"deleteForEveryone"`
	DeleteMaxAgeSeconds *int64   `json:"deleteMaxAgeSeconds,omitempty"`
	Reactions           bool     `json:"reactions"`
	AllowedReactions    []string `json:"allowedReactions,omitzero"`
	Replies             bool     `json:"replies"`
	Threads             bool     `json:"threads"`
	ReadReceipts        bool     `json:"readReceipts"`
	Typing              bool     `json:"typing"`
	MaxTextLength       *int     `json:"maxTextLength,omitempty"`
}

// Chat is one conversation.
type Chat struct {
	ID                    string        `json:"id"`
	Category              *string       `json:"category,omitempty"`
	ChatName              Null[string]  `json:"chatName,omitzero"`
	Topic                 Null[string]  `json:"topic,omitzero"`
	Avatar                Null[Media]   `json:"avatar,omitzero"`
	OtherUser             Null[string]  `json:"otherUser,omitzero"`
	Members               *Members      `json:"members,omitempty"`
	MutedUntil            Null[Instant] `json:"mutedUntil,omitzero"`
	Archived              *bool         `json:"archived,omitempty"`
	Pinned                *bool         `json:"pinned,omitempty"`
	MarkedUnread          *bool         `json:"markedUnread,omitempty"`
	ReadOnly              *bool         `json:"readOnly,omitempty"`
	MessageRequest        *bool         `json:"messageRequest,omitempty"`
	DisappearAfterSeconds Null[int64]   `json:"disappearAfterSeconds,omitzero"`
	Parent                Null[string]  `json:"parent,omitzero"`
	Features              *Features     `json:"features,omitempty"`
	LastActivityAt        *Instant      `json:"lastActivityAt,omitempty"`
}

// ChatUpsert writes one chat.
type ChatUpsert struct {
	Head
	Chat Chat `json:"chat"`
}

// ChatDelete deletes one chat's row.
type ChatDelete struct {
	Head
	Chat string `json:"chat"`
}

// ChatRead is the owner's own read marker, from any device. Head.At is when
// it was read.
type ChatRead struct {
	Head
	Chat string `json:"chat"`
	UpTo string `json:"upTo"`
}

// The content kinds of a message and of an attachment.
const (
	ContentText        = "text"
	ContentNotice      = "notice"
	ContentEmote       = "emote"
	ContentImage       = "image"
	ContentVideo       = "video"
	ContentAudio       = "audio"
	ContentVoice       = "voice"
	ContentFile        = "file"
	ContentSticker     = "sticker"
	ContentLocation    = "location"
	ContentPoll        = "poll"
	ContentUnsupported = "unsupported"
)

// The states of an attachment's bytes.
const (
	MediaMetadata = "metadata"
	MediaStored   = "stored"
	MediaTooLarge = "too_large"
	MediaFailed   = "failed"
	MediaExpired  = "expired"
)

// Attachment is one file sent with a message. Part is bridgev2's part id;
// File names the multipart part carrying its bytes.
type Attachment struct {
	Part       string  `json:"part"`
	Content    string  `json:"content"`
	MimeType   *string `json:"mimeType,omitempty"`
	FileName   *string `json:"fileName,omitempty"`
	Size       *int64  `json:"size,omitempty"`
	Width      *int    `json:"width,omitempty"`
	Height     *int    `json:"height,omitempty"`
	DurationMs *int64  `json:"durationMs,omitempty"`
	Caption    *string `json:"caption,omitempty"`
	File       *Media  `json:"file,omitempty"`
	MediaState string  `json:"mediaState"`
}

// Message is one network message, its parts folded.
type Message struct {
	ID               string       `json:"id"`
	Sender           string       `json:"sender"`
	FromOwner        bool         `json:"fromOwner"`
	At               Instant      `json:"at"`
	Content          string       `json:"content"`
	Text             *string      `json:"text,omitempty"`
	EditedAt         *Instant     `json:"editedAt,omitempty"`
	EditCount        *int         `json:"editCount,omitempty"`
	ReplyTo          *string      `json:"replyTo,omitempty"`
	ThreadRoot       *string      `json:"threadRoot,omitempty"`
	Mentions         []string     `json:"mentions,omitzero"`
	MentionsEveryone *bool        `json:"mentionsEveryone,omitempty"`
	Forwarded        *bool        `json:"forwarded,omitempty"`
	ExpiresAt        *Instant     `json:"expiresAt,omitempty"`
	Attachments      []Attachment `json:"attachments,omitzero"`
	Action           *string      `json:"action,omitempty"`
}

// MessageUpsert writes one message.
type MessageUpsert struct {
	Head
	Chat     string  `json:"chat"`
	Backfill bool    `json:"backfill"`
	Message  Message `json:"message"`
}

// The scopes of a deletion.
const (
	ScopeEveryone = "everyone"
	ScopeOwner    = "owner"
)

// MessageDelete marks one message deleted. Head.At is when.
type MessageDelete struct {
	Head
	Chat    string  `json:"chat"`
	Message string  `json:"message"`
	Scope   string  `json:"scope"`
	By      *string `json:"by,omitempty"`
}

// Reaction is one user's reaction.
type Reaction struct {
	Key  string  `json:"key"`
	User string  `json:"user"`
	At   Instant `json:"at"`
}

// MessageReactions is the whole set of reactions the bridge holds for one
// message, so a redelivery converges.
type MessageReactions struct {
	Head
	Chat      string     `json:"chat"`
	Message   string     `json:"message"`
	Reactions []Reaction `json:"reactions"`
}

// The statuses of the owner's own messages.
const (
	StatusSent      = "sent"
	StatusDelivered = "delivered"
	StatusRead      = "read"
	StatusFailed    = "failed"
)

// MessageStatus is how far one of the owner's messages got. Head.At is when.
type MessageStatus struct {
	Head
	Chat    string `json:"chat"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// ActionClaimed says this bridge claimed an action at a version.
type ActionClaimed struct {
	Head
	Action  string `json:"action"`
	Attempt string `json:"attempt"`
	Version int64  `json:"version"`
}

// The outcomes of an action. Each is final.
const (
	OutcomeDone      = "done"
	OutcomeFailed    = "failed"
	OutcomeUncertain = "uncertain"
	OutcomeExpired   = "expired"
	OutcomeCancelled = "cancelled"
)

// The error codes of a failed action.
const (
	ErrUnsupported = "unsupported"
	ErrNotFound    = "not_found"
	ErrRateLimited = "rate_limited"
	ErrBlocked     = "blocked"
	ErrRejected    = "rejected"
	ErrNetwork     = "network"
	ErrExpired     = "expired"
	ErrCancelled   = "cancelled"
	ErrInvalid     = "invalid"
)

// ActionError says why an action failed.
type ActionError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ActionResult is an action's outcome.
type ActionResult struct {
	Head
	Action  string       `json:"action"`
	Attempt string       `json:"attempt"`
	Outcome string       `json:"outcome"`
	Message *string      `json:"message,omitempty"`
	Error   *ActionError `json:"error,omitempty"`
}

// BackfillProgress says how far history has come, for one chat or the whole
// account.
type BackfillProgress struct {
	Head
	Chat         *string  `json:"chat,omitempty"`
	OldestAt     *Instant `json:"oldestAt,omitempty"`
	Complete     bool     `json:"complete"`
	PendingChats *int     `json:"pendingChats,omitempty"`
}

// Ptr returns a pointer to v, for the optional fields.
func Ptr[T any](v T) *T { return &v }
