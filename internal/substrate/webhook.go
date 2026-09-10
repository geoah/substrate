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
