// Package server is the bridge's client of the substrate server, over its
// public HTTP API alone: the app authorization flow that gets the bridge its
// token, the webhook door it delivers batches to, the change feed it follows
// for the owner's requests, and the record reads behind both. The name
// internal/substrate is the server module's contract package; this is not
// it, and imports nothing of that module.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The package the bridge's records live in, and its webhook.
const (
	Package        = "providers.substrate.reamde.dev/chatbridge"
	PackageVersion = 1
	Trigger        = "chatbridge-events"

	KindInstance   = Package + "/instance"
	KindAccount    = Package + "/account"
	KindUser       = Package + "/user"
	KindChat       = Package + "/chat"
	KindMessage    = Package + "/message"
	KindAttachment = Package + "/messageattachment"
	KindAction     = Package + "/chataction"

	triggerKind = "substrate.reamde.dev/core/trigger"
)

// ErrUnauthorized is a token the server no longer takes: revoked, expired,
// or never this repository's.
var ErrUnauthorized = errors.New("the server refused the bridge's token: run chatbridge authorize again")

// APIError is a refusal the server explained.
type APIError struct {
	Status     int
	Code       string `json:"code"`
	Message    string `json:"message"`
	Head       *int64 `json:"head,omitempty"`
	Generation string `json:"generation,omitempty"`
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("the server answered %d", e.Status)
	}
	return fmt.Sprintf("the server answered %d %s: %s", e.Status, e.Code, e.Message)
}

// Client calls one server with the bridge's token. The server and the
// token are read for every request, so a new authorization takes effect
// without a restart.
type Client struct {
	base  func() string
	token func() string
	http  *http.Client
}

// NewClient returns a client of the server at base.
func NewClient(base string, token func() string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("the server address %q is not an absolute http(s) URL", base)
	}
	return NewDynamicClient(func() string { return base }, token, hc), nil
}

// NewDynamicClient returns a client of whichever server base names when a
// request is made; an empty base is a bridge not authorized yet.
func NewDynamicClient(base, token func() string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{base: base, token: token, http: hc}
}

// Host is the server's host, which is safe to log.
func (c *Client) Host() string {
	u, err := url.Parse(c.base())
	if err != nil {
		return ""
	}
	return u.Host
}

func (c *Client) url(path string, q url.Values) string {
	s := strings.TrimRight(c.base(), "/") + path
	if len(q) > 0 {
		s += "?" + q.Encode()
	}
	return s
}

func (c *Client) request(ctx context.Context, method, target string, body io.Reader, contentType string) (*http.Response, error) {
	if c.base() == "" {
		return nil, ErrUnauthorized
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, errors.New("building a request to the server failed")
	}
	if tok := c.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, redact(err)
	}
	return resp, nil
}

// redact strips the request URL from a transport error, whose text quotes
// it; a webhook URL is the bridge's to keep out of logs.
func redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

// decodeError reads a refusal.
func decodeError(resp *http.Response) error {
	if resp.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return ErrUnauthorized
	}
	var env struct {
		Error APIError `json:"error"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	_ = json.Unmarshal(body, &env)
	env.Error.Status = resp.StatusCode
	return &env.Error
}

// IsNotFound reports a 404.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

// Record is one record as the API serves it: the flat record.
type Record struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Properties map[string]any `json:"properties"`
	Version    int64          `json:"version"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
	DeletedAt  *time.Time     `json:"deletedAt,omitempty"`
}

// String reads a string property.
func (r *Record) String(name string) string {
	s, _ := r.Properties[name].(string)
	return s
}

// Bool reads a boolean property.
func (r *Record) Bool(name string) bool {
	b, _ := r.Properties[name].(bool)
	return b
}

// Int reads an integer property and whether it is set.
func (r *Record) Int(name string) (int64, bool) {
	switch v := r.Properties[name].(type) {
	case float64:
		return int64(v), true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	}
	return 0, false
}

// Time reads an instant property.
func (r *Record) Time(name string) (time.Time, bool) {
	s := r.String(name)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil
}

// Ref reads a reference property: the referent's kind and id.
func (r *Record) Ref(name string) (kind, id string, ok bool) {
	v, _ := r.Properties[name].(map[string]any)
	path, _ := v["ref"].(string)
	if path == "" {
		return "", "", false
	}
	return SplitPath(path)
}

// SplitPath splits a record path, <authority>/<package>/<kind>/<id>, into
// its kind and id. The id is everything past the kind's three segments.
func SplitPath(path string) (kind, id string, ok bool) {
	parts := strings.SplitN(path, "/", 4)
	if len(parts) != 4 || parts[3] == "" {
		return "", "", false
	}
	return strings.Join(parts[:3], "/"), parts[3], true
}

// recordPath is the API path of a record; the id is one segment.
func recordPath(kind, id string) string {
	return "/api/v1/" + kind + "/" + url.PathEscape(id)
}

// GetRecord reads one record; a record the server holds no row for is nil.
// A deleted record answers with DeletedAt set.
func (c *Client) GetRecord(ctx context.Context, kind, id string) (*Record, error) {
	resp, err := c.request(ctx, http.MethodGet, c.url(recordPath(kind, id), nil), nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, decodeError(resp)
	}
	var r Record
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("reading a record: %w", err)
	}
	return &r, nil
}

// Filter is the records route's filter, the arms the bridge uses.
type Filter struct {
	Kinds      []string                  `json:"kinds"`
	Properties map[string]map[string]any `json:"properties,omitempty"`
}

// Page is one page of a list.
type Page struct {
	Records    []Record `json:"records"`
	Cursor     string   `json:"cursor,omitempty"`
	Head       int64    `json:"head"`
	Generation string   `json:"generation"`
}

// List reads every record a filter admits, page by page.
func (c *Client) List(ctx context.Context, f Filter) ([]Record, error) {
	raw, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	var out []Record
	after := ""
	for {
		q := url.Values{"filter": {string(raw)}, "first": {"200"}}
		if after != "" {
			q.Set("after", after)
		}
		resp, err := c.request(ctx, http.MethodGet, c.url("/api/v1/records", q), nil, "")
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusNotFound {
			// A kind the repository does not declare yet: the package is
			// not installed, so there is nothing to list.
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return out, nil
		}
		if resp.StatusCode != http.StatusOK {
			err := decodeError(resp)
			resp.Body.Close()
			return nil, err
		}
		var page Page
		dec := json.NewDecoder(resp.Body)
		dec.UseNumber()
		err = dec.Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading a page: %w", err)
		}
		out = append(out, page.Records...)
		if page.Cursor == "" {
			return out, nil
		}
		after = page.Cursor
	}
}

// Parked is one delivery the trigger holds: parked after its retries, or
// still filing.
type Parked struct {
	ID        int64     `json:"id"`
	FireID    string    `json:"fireId,omitempty"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"lastError"`
	ParkedAt  time.Time `json:"parkedAt"`
	Pending   bool      `json:"pending,omitempty"`
	Running   bool      `json:"running,omitempty"`
}

// ParkedList reads the webhook trigger's parked deliveries, which the
// bridge's webhook scope reaches.
func (c *Client) ParkedList(ctx context.Context) ([]Parked, error) {
	path := "/api/v1/" + triggerKind + "/" + url.PathEscape(Trigger) + "/parked"
	resp, err := c.request(ctx, http.MethodGet, c.url(path, nil), nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeError(resp)
	}
	var list struct {
		Items []Parked `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("reading the parked list: %w", err)
	}
	return list.Items, nil
}

// MediaPart is one file a delivery carries.
type MediaPart struct {
	Name     string
	FileName string
	MimeType string
	Open     func() (io.ReadCloser, error)
}

// Deliver posts one batch to the webhook door: the batch as an inline text
// part, each file as a part of its own. It returns the fire id the door
// answered 202 with.
func (c *Client) Deliver(ctx context.Context, webhookURL string, batch []byte, media []MediaPart) (string, error) {
	body, contentType, err := multipartBody(batch, media)
	if err != nil {
		return "", err
	}
	resp, err := c.request(ctx, http.MethodPost, webhookURL, body, contentType)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return "", decodeError(resp)
	}
	var ack struct {
		Fire string `json:"fire"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&ack); err != nil || ack.Fire == "" {
		return "", errors.New("the webhook door accepted the batch without a fire id")
	}
	return ack.Fire, nil
}

// Discover reads the server's authorization server metadata.
func (c *Client) Discover(ctx context.Context) (*Metadata, error) {
	resp, err := c.request(ctx, http.MethodGet, c.url("/.well-known/oauth-authorization-server", nil), nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeError(resp)
	}
	var m Metadata
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		return nil, fmt.Errorf("reading the server's metadata: %w", err)
	}
	return &m, nil
}
