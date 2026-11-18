// Package pushwire is the wire between a substrate server, an app and the
// publisher's push relay (docs/plans/push.md, section 1): the two request
// bodies and their answers, the closed set of refusal codes, the signature a
// server puts on a push, the relay's two addresses, the fingerprint an app
// matches a delivered notification by, and the Apple notification a push
// becomes. testdata/ holds a fixture of each, which the server, the relay
// and the apps all test against.
//
// It links nothing of the server's, so the relay, which faces the internet
// and holds the publisher's Apple keys, carries none of it.
package pushwire

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/recordid"
)

// The fixed numbers of version 1 of the wire.
const (
	// PathRegister and PathPush are the two routes, each under the relay's
	// address (Endpoint). A change of meaning is /v2/, never an edit here.
	PathRegister = "/v1/register"
	PathPush     = "/v1/push"
	// SignatureHeader carries a push's signature (Sign).
	SignatureHeader = "Substrate-Push-Signature"
	// SignatureWindow is how far a signature's time may be from the relay's
	// clock, before or after, and how long past its time the relay keeps it
	// to refuse a repeat.
	SignatureWindow = 300 * time.Second
	// KeyLifetime is how long after it was issued a push key is accepted.
	KeyLifetime = 90 * 24 * time.Hour
	// MaxBody is the largest request body the relay reads.
	MaxBody = 4 << 10
	// MaxPushKey bounds a push key, in characters.
	MaxPushKey = 1024
	// MinEventID and MaxEventID bound an event id, in characters. 1,024 and
	// not less, because one item's sealed reference can outgrow 512: a
	// record id runs to 128 characters and a message id is the client's.
	MinEventID = 32
	MaxEventID = 1024
	// MaxBadge is the one cap of an unread count: the server's, the relay's
	// and the app's.
	MaxBadge = 999
	// MinToken and MaxToken bound Apple's device token, in hex characters.
	MinToken = 32
	MaxToken = 400
	// MaxTopic bounds a topic, an app's bundle id, which Apple caps at 155.
	MaxTopic = 155
	// PublicKeySize and GenerationSize are the decoded sizes of a register's
	// publicKey (an Ed25519 public key) and generation.
	PublicKeySize  = 32
	GenerationSize = 16
)

// PlatformAPNs is the one platform. `fcm` is reserved for Android, and until
// a relay serves it a request naming it is refused as unknown.
const PlatformAPNs = "apns"

// The two Apple environments, each with its own key and connections.
const (
	EnvironmentProduction  = "production"
	EnvironmentDevelopment = "development"
)

// The events a push says happened.
const (
	EventReply   = "reply"
	EventFailed  = "failed"
	EventRequest = "request"
	EventRead    = "read"
)

// StatusSent is the one answer to a push the relay handed to Apple.
const StatusSent = "sent"

// The closed set of refusal codes. A server acts on the code, never on the
// message; docs/plans/push.md section 1 says what it does for each.
const (
	CodeBadRequest         = "bad_request"
	CodeStaleSignature     = "stale_signature"
	CodeSignatureRefused   = "signature_refused"
	CodeKeyRefused         = "key_refused"
	CodeKeyExpired         = "key_expired"
	CodeTopicRefused       = "topic_refused"
	CodeEnvironmentRefused = "environment_refused"
	CodeReplayed           = "replayed"
	CodeGone               = "gone"
	CodeRateLimited        = "rate_limited"
	CodeUpstream           = "upstream"
	CodeInternal           = "internal"
)

// Codes is every refusal code, in the order the plan's table lists them.
var Codes = []string{
	CodeBadRequest, CodeStaleSignature, CodeSignatureRefused, CodeKeyRefused, CodeKeyExpired,
	CodeTopicRefused, CodeEnvironmentRefused, CodeReplayed, CodeGone, CodeRateLimited,
	CodeUpstream, CodeInternal,
}

// Events and Environments are the closed sets a request is held to.
var (
	Events       = []string{EventReply, EventFailed, EventRequest, EventRead}
	Environments = []string{EnvironmentProduction, EnvironmentDevelopment}
)

// RegisterRequest is what an app posts to PathRegister, with no substrate
// token.
type RegisterRequest struct {
	Platform    string `json:"platform"`
	Token       string `json:"token"`
	Topic       string `json:"topic"`
	Environment string `json:"environment"`
	PublicKey   string `json:"publicKey"`
	Generation  string `json:"generation"`
}

// RegisterResponse carries the push key the relay sealed for the install.
type RegisterResponse struct {
	PushKey string `json:"pushKey"`
}

// PushRequest is what a server posts to PathPush, signed (Sign).
type PushRequest struct {
	PushKey string `json:"pushKey"`
	Event   string `json:"event"`
	Thread  string `json:"thread"`
	Badge   int    `json:"badge"`
	EventID string `json:"eventId"`
}

// PushResponse is the answer to a push Apple accepted.
type PushResponse struct {
	Status string `json:"status"`
}

// ErrorResponse is every refusal's body. Message is for a log, never for a
// person.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail is the refusal inside an ErrorResponse.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrBadRequest wraps every reason a body is refused as CodeBadRequest.
var ErrBadRequest = errors.New("bad request")

// b64 is the one base64 every value on this wire is written in: base64url,
// no padding, and strict, so each value has one spelling.
var b64 = base64.RawURLEncoding.Strict()

// DecodeRegister reads a register body. A key it does not know is ignored,
// so a newer app can talk to an older relay; a key it knows with a value of
// the wrong type, null, or out of bounds, and a value it does not know, are
// refused, wrapping ErrBadRequest. Whether the relay serves the topic and
// the environment is the relay's to say.
func DecodeRegister(body []byte) (RegisterRequest, error) {
	fields, err := object(body)
	if err != nil {
		return RegisterRequest{}, err
	}
	var r RegisterRequest
	for _, f := range []struct {
		name string
		into *string
	}{
		{"platform", &r.Platform},
		{"token", &r.Token},
		{"topic", &r.Topic},
		{"environment", &r.Environment},
		{"publicKey", &r.PublicKey},
		{"generation", &r.Generation},
	} {
		if err := field(fields, f.name, f.into); err != nil {
			return RegisterRequest{}, err
		}
	}
	return r, r.Check()
}

// Check holds a register request to the bounds of version 1.
func (r RegisterRequest) Check() error {
	switch {
	case r.Platform != PlatformAPNs:
		return refuse("platform is not apns")
	case len(r.Token) < MinToken || len(r.Token) > MaxToken || !isHex(r.Token):
		return refuse("token is not 32 to 400 hex characters")
	case !validTopic(r.Topic):
		return refuse("topic is not a bundle id of at most 155 characters")
	case !oneOf(r.Environment, Environments):
		return refuse("environment is not production or development")
	case decodedLen(r.PublicKey) != PublicKeySize:
		return refuse("publicKey is not the unpadded base64url of 32 bytes")
	case decodedLen(r.Generation) != GenerationSize:
		return refuse("generation is not the unpadded base64url of 16 bytes")
	}
	return nil
}

// DecodePush reads a push body by DecodeRegister's rules.
func DecodePush(body []byte) (PushRequest, error) {
	fields, err := object(body)
	if err != nil {
		return PushRequest{}, err
	}
	var p PushRequest
	for _, f := range []struct {
		name string
		into any
	}{
		{"pushKey", &p.PushKey},
		{"event", &p.Event},
		{"thread", &p.Thread},
		{"badge", &p.Badge},
		{"eventId", &p.EventID},
	} {
		if err := field(fields, f.name, f.into); err != nil {
			return PushRequest{}, err
		}
	}
	return p, p.Check()
}

// Check holds a push request to the bounds of version 1. What the push key
// holds is the relay's to read.
func (p PushRequest) Check() error {
	switch {
	case p.PushKey == "" || len(p.PushKey) > MaxPushKey:
		return refuse("pushKey is empty or over 1024 characters")
	case !oneOf(p.Event, Events):
		return refuse("event is not reply, failed, request or read")
	case !recordid.Valid(p.Thread):
		return refuse("thread is not a record id")
	case p.Badge < 0 || p.Badge > MaxBadge:
		return refuse("badge is not 0 to 999")
	case len(p.EventID) < MinEventID || len(p.EventID) > MaxEventID || decodedLen(p.EventID) < 0:
		return refuse("eventId is not 32 to 1024 characters of unpadded base64url")
	}
	return nil
}

// object reads body as one JSON object and nothing after it, keeping each
// key's raw value. Keys match exactly: a key that differs from a known one
// only by case is an unknown key, ignored, never read as the known one.
func object(body []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, refuse("the body is not one JSON object")
	}
	return fields, nil
}

// field decodes the named key into v, refusing it missing, null or of
// another type.
func field(fields map[string]json.RawMessage, name string, v any) error {
	raw, ok := fields[name]
	if !ok || string(raw) == "null" {
		return refuse(name + " is missing")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return refuse(name + " is not of its type")
	}
	return nil
}

// refuse wraps a fixed message: it is logged, so it never repeats a value
// the caller wrote.
func refuse(message string) error {
	return fmt.Errorf("%w: %s", ErrBadRequest, message)
}

func oneOf(s string, set []string) bool {
	for _, v := range set {
		if s == v {
			return true
		}
	}
	return false
}

func isHex(s string) bool {
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// validTopic holds a topic to a bundle id's alphabet (letters, digits, "."
// and "-") and Apple's length, so it is safe in a header and a log line.
func validTopic(s string) bool {
	if s == "" || len(s) > MaxTopic {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

// ValidTopic reports whether s may be a topic: a bundle id's alphabet and
// length. The relay holds its configured topics to it.
func ValidTopic(s string) bool { return validTopic(s) }

// decodedLen is how many bytes s decodes to as strict unpadded base64url,
// or -1 when it does not decode.
func decodedLen(s string) int {
	b, err := Decode(s)
	if err != nil {
		return -1
	}
	return len(b)
}

// Encode is base64url without padding, the encoding of every binary value on
// this wire.
func Encode(b []byte) string { return b64.EncodeToString(b) }

// Decode reads a value Encode wrote, refusing any other spelling. The
// standard decoder skips CR and LF even in strict mode, so a value broken
// over lines would decode as a second spelling of the same bytes: anything
// outside the alphabet is refused before it is decoded.
func Decode(s string) ([]byte, error) {
	for i := range len(s) {
		c := s[i]
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return nil, errNotBase64URL
		}
	}
	return b64.DecodeString(s)
}

var errNotBase64URL = errors.New("not unpadded base64url")
