// Package apns sends notifications through Apple's push service with token
// authentication, on net/http and crypto/ecdsa alone: Go speaks HTTP/2 to
// Apple without help, and the provider token is one ES256 signature.
//
// One Client is one environment: its own key, its own provider token and
// its own pool of HTTP/2 connections, kept open between sends.
package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Apple's two hosts, port 443.
const (
	HostProduction  = "https://api.push.apple.com"
	HostDevelopment = "https://api.sandbox.push.apple.com"
)

// TokenLifetime is how long a provider token is reused. Apple refuses one
// older than an hour, and a provider that replaces its token more often than
// every 20 minutes.
const TokenLifetime = 50 * time.Minute

// Timeout bounds one send, connecting included.
const Timeout = 10 * time.Second

// The reasons this package acts on. Apple sends others; a caller reads
// Response.Reason.
const (
	ReasonExpiredProviderToken = "ExpiredProviderToken"
	ReasonInvalidProviderToken = "InvalidProviderToken"
)

// maxAnswer bounds what is read of Apple's answer body, which is one small
// JSON object.
const maxAnswer = 4 << 10

// ParseKey reads an Apple token-auth key, a `.p8`: one PEM block of a PKCS#8
// P-256 private key.
func ParseKey(p8 []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(p8)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("not a PEM private key, as Apple issues a .p8")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not a PKCS#8 private key: %w", err)
	}
	ec, ok := key.(*ecdsa.PrivateKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, errors.New("not a P-256 key, as Apple's token-auth keys are")
	}
	return ec, nil
}

// NewTransport is a transport that speaks HTTP/2 alone, as Apple requires,
// with its own pool of connections. tlsConfig is nil outside tests.
func NewTransport(tlsConfig *tls.Config) *http.Transport {
	t := &http.Transport{
		Proxy:               nil,
		TLSClientConfig:     tlsConfig,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: Timeout,
		IdleConnTimeout:     time.Hour,
		MaxIdleConnsPerHost: 4,
	}
	t.Protocols = new(http.Protocols)
	t.Protocols.SetHTTP2(true)
	return t
}

// Config is one environment's.
type Config struct {
	// BaseURL is HostProduction or HostDevelopment.
	BaseURL string
	// TeamID and KeyID name the key to Apple; Key is the key.
	TeamID, KeyID string
	Key           *ecdsa.PrivateKey
	// Transport is NewTransport(nil) when nil.
	Transport http.RoundTripper
	// Now is time.Now when nil.
	Now func() time.Time
	// OnMint is called after each provider token is minted, for a metric.
	OnMint func()
}

// Client sends to one environment.
type Client struct {
	cfg  Config
	http *http.Client

	mu     sync.Mutex
	token  string
	minted time.Time
}

// New makes a Client. It mints nothing until asked.
func New(cfg Config) (*Client, error) {
	if cfg.Key == nil || cfg.TeamID == "" || cfg.KeyID == "" || cfg.BaseURL == "" {
		return nil, errors.New("apns: a client needs a key, its key id, the team id and a host")
	}
	if cfg.Transport == nil {
		cfg.Transport = NewTransport(nil)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Client{
		cfg: cfg,
		// No redirect is followed: Apple answers, it never sends one
		// elsewhere, and the bearer would follow it.
		http: &http.Client{
			Transport:     cfg.Transport,
			Timeout:       Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// Token is the provider token to send with: the current one while it is
// younger than TokenLifetime, else a new one. Minting holds the lock, so it
// is one at a time, and the sends waiting on it take the token it mints.
func (c *Client) Token() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.cfg.Now()
	if c.token != "" && now.Sub(c.minted) < TokenLifetime {
		return c.token, nil
	}
	tok, err := c.mint(now)
	if err != nil {
		return "", err
	}
	c.token, c.minted = tok, now
	if c.cfg.OnMint != nil {
		c.cfg.OnMint()
	}
	return tok, nil
}

// expire drops the current provider token, but only if it is the one a
// failing send carried: many sends refused for one token cause one mint,
// and a refusal of an older token, answered after a new one was minted,
// causes none.
func (c *Client) expire(tok string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == tok {
		c.token = ""
	}
}

// mint signs a provider token: header {"alg":"ES256","kid":<key id>}, claims
// {"iss":<team id>,"iat":<now>}, and the ES256 signature as r and s, 32
// bytes each.
func (c *Client) mint(now time.Time) (string, error) {
	enc := base64.RawURLEncoding
	header, _ := json.Marshal(struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}{"ES256", c.cfg.KeyID})
	claims, _ := json.Marshal(struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
	}{c.cfg.TeamID, now.Unix()})
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, c.cfg.Key, sum[:])
	if err != nil {
		return "", fmt.Errorf("apns: sign a provider token: %w", err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + enc.EncodeToString(sig), nil
}

// Notification is one send.
type Notification struct {
	// DeviceToken is Apple's token for the install, in hex.
	DeviceToken string
	// Headers are the apns-* headers, the topic included.
	Headers map[string]string
	Payload []byte
}

// Response is what Apple answered.
type Response struct {
	Status int
	// Reason is Apple's reason, empty on 200.
	Reason string
	// ID is the apns-id Apple gave the notification.
	ID string
}

// Send posts n. When Apple answers 403 ExpiredProviderToken to the token the
// send carried, that token is replaced and n is sent once more. An error is
// a send that got no answer: a timeout, a refused connection, a broken
// stream.
func (c *Client) Send(ctx context.Context, n Notification) (Response, error) {
	tok, err := c.Token()
	if err != nil {
		return Response{}, err
	}
	resp, err := c.send(ctx, tok, n)
	if err != nil || resp.Status != http.StatusForbidden || resp.Reason != ReasonExpiredProviderToken {
		return resp, err
	}
	c.expire(tok)
	if tok, err = c.Token(); err != nil {
		return Response{}, err
	}
	return c.send(ctx, tok, n)
}

func (c *Client) send(ctx context.Context, tok string, n Notification) (Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/3/device/"+n.DeviceToken, bytes.NewReader(n.Payload))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Authorization", "bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range n.Headers {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer res.Body.Close()
	out := Response{Status: res.StatusCode, ID: res.Header.Get("apns-id")}
	if res.StatusCode != http.StatusOK {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, maxAnswer)).Decode(&body)
		out.Reason = body.Reason
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxAnswer))
	return out, nil
}

// String says what Apple answered, for a person reading `check`.
func (r Response) String() string {
	s := strconv.Itoa(r.Status)
	if r.Reason != "" {
		s += " " + r.Reason
	}
	return s
}
