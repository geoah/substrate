package pushwire

import (
	"encoding/json"
	"strconv"
	"time"
)

// The keys of the words a pushed alert shows, in the app's own strings, so
// they are the app's, in the person's language, even when no extension
// runs. No field of a push becomes text a person reads.
const (
	LocKeyReply   = "SUBSTRATE_PUSH_REPLY"
	LocKeyFailed  = "SUBSTRATE_PUSH_FAILED"
	LocKeyRequest = "SUBSTRATE_PUSH_REQUEST"
)

// PayloadVersion is the `substrate.v` of every payload this wire builds.
const PayloadVersion = 1

// MaxCollapseID is the longest apns-collapse-id Apple accepts, in bytes.
const MaxCollapseID = 64

// How long Apple holds an undelivered alert, and a `read`.
const (
	AlertLifetime = 6 * time.Hour
	ReadLifetime  = time.Hour
)

// AppleNotification is what one push becomes at Apple: its headers but the
// device token and the topic, which the relay reads from the push key, and
// its payload.
type AppleNotification struct {
	PushType   string
	Priority   int
	Expiration time.Time
	// CollapseID is empty for none.
	CollapseID string
	Payload    []byte
}

// Headers are the apns-* headers Apple reads, the topic included.
func (n AppleNotification) Headers(topic string) map[string]string {
	h := map[string]string{
		"apns-push-type":  n.PushType,
		"apns-priority":   strconv.Itoa(n.Priority),
		"apns-expiration": strconv.FormatInt(n.Expiration.Unix(), 10),
		"apns-topic":      topic,
	}
	if n.CollapseID != "" {
		h["apns-collapse-id"] = n.CollapseID
	}
	return h
}

// SubstratePayload is the payload's `substrate` object, which the app reads:
// what happened, on which thread, the registration generation the push key
// sealed (`g`), and the id the owner's server resolves to the item.
type SubstratePayload struct {
	V       int    `json:"v"`
	Event   string `json:"event"`
	Thread  string `json:"thread"`
	G       string `json:"g"`
	EventID string `json:"eventId"`
}

type applePayload struct {
	APS       any              `json:"aps"`
	Substrate SubstratePayload `json:"substrate"`
}

type alertAPS struct {
	Alert          appleAlert `json:"alert"`
	Badge          int        `json:"badge"`
	Sound          string     `json:"sound"`
	MutableContent int        `json:"mutable-content"`
	ThreadID       string     `json:"thread-id"`
}

type appleAlert struct {
	LocKey string `json:"loc-key"`
}

// readAPS keeps the badge, which iOS applies when no code of the app runs:
// without it a closed app's badge could not go down.
type readAPS struct {
	Badge            int `json:"badge"`
	ContentAvailable int `json:"content-available"`
}

// ApplePush builds the notification a checked push becomes, from its event,
// thread, badge and event id and the generation its push key sealed, at now.
// `reply`, `failed` and `request` alert with the app's own words and let the
// extension rewrite them (mutable-content); `read` sets the badge and wakes
// the app to take its notifications back. A `reply` or `failed` collapses by
// its thread when the thread id fits a collapse id; a `request` never does,
// because two cards waiting on one thread are two alerts, and collapsing
// them at Apple would drop one before the extension saw its event id.
func ApplePush(p PushRequest, generation string, now time.Time) AppleNotification {
	sub := SubstratePayload{V: PayloadVersion, Event: p.Event, Thread: p.Thread, G: generation, EventID: p.EventID}
	if p.Event == EventRead {
		payload, _ := json.Marshal(applePayload{APS: readAPS{Badge: p.Badge, ContentAvailable: 1}, Substrate: sub})
		return AppleNotification{PushType: "alert", Priority: 5, Expiration: now.Add(ReadLifetime), Payload: payload}
	}
	loc := map[string]string{EventReply: LocKeyReply, EventFailed: LocKeyFailed, EventRequest: LocKeyRequest}[p.Event]
	aps := alertAPS{Alert: appleAlert{LocKey: loc}, Badge: p.Badge, Sound: "default", MutableContent: 1, ThreadID: p.Thread}
	payload, _ := json.Marshal(applePayload{APS: aps, Substrate: sub})
	n := AppleNotification{PushType: "alert", Priority: 10, Expiration: now.Add(AlertLifetime), Payload: payload}
	if p.Event != EventRequest && len(p.Thread) <= MaxCollapseID {
		n.CollapseID = p.Thread
	}
	return n
}
