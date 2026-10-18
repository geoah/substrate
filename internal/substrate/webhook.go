package substrate

// WebhookRequest is one public webhook delivery as the API layer hands it to
// the service: transport facts only, no net/http types, so the seam is
// testable without a listener and the engine never parses HTTP.
type WebhookRequest struct {
	Method string
	// ContentType is the parsed media type, parameters stripped
	// ("multipart/form-data", "application/json"); the full header stays in
	// Headers.
	ContentType string
	// Headers carries lowercased names, multi-valued headers joined with
	// ", ", and none of the credential headers (authorization, cookie,
	// proxy-authorization).
	Headers map[string]string
	Query   map[string][]string
	// Body is the raw non-multipart body, byte-exact so a provider's signature
	// over it verifies in the callable; nil for a multipart request.
	Body []byte
	// Parts are a multipart/form-data request's parts, in order.
	Parts []WebhookPart
}

// WebhookPart is one multipart part: Value for an inline field, Data for a
// file part, which the engine stores content-addressed before the fire and
// hands the callable as a blob digest.
type WebhookPart struct {
	Name      string
	Filename  string
	MediaType string
	Value     string
	Data      []byte
}

// WebhookAccepted is the 202 a webhook door answers with: the fire id the
// delivery runs under. The callable's output is never a response.
type WebhookAccepted struct {
	Fire string `json:"fire"`
}

// WebhookTarget is what the door learns about a trigger from CheckWebhook,
// before it reads a byte of the request.
type WebhookTarget struct {
	// MaxBody is the inline bytes the trigger admits: WebhookInlineDefault
	// unless it declares `maxBody`.
	MaxBody int64
	// SignatureOnly is a trigger with no key and a declared signature: the
	// request's signature is its one credential, and ReceiveWebhook checks
	// it. Until then nothing has authenticated the sender, so every refusal
	// the door makes while reading the request is the 404 a missing trigger
	// gets, and a sender cannot learn the trigger exists from a 400 or 413.
	SignatureOnly bool
}

// The inline bytes one webhook delivery may carry: its body, or its
// multipart inline values together. A trigger is held to WebhookInlineDefault
// unless its `source.webhook.maxBody` declares another bound, at most
// WebhookInlineMax. The door reads the trigger's bound before it reads a
// byte of the body, so an unauthenticated sender cannot make the server
// buffer more than the trigger it names allows.
const (
	WebhookInlineDefault = 1 << 20
	WebhookInlineMax     = 8 << 20
)
