package blobbytes

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// S3Config is what the s3 backend needs. Endpoint, Bucket, Region and the two
// credentials are required; the rest have defaults.
type S3Config struct {
	// Endpoint is the service URL, scheme included: an S3-compatible server
	// (http://127.0.0.1:9000) or a region endpoint
	// (https://s3.us-east-1.amazonaws.com).
	Endpoint string
	Bucket   string
	Region   string
	// AccessKeyID and SecretAccessKey sign every request. SessionToken is
	// optional and only for temporary credentials.
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	// Prefix goes in front of every key, for a bucket that holds more than
	// this substrate. Empty puts <repository>/<digest> at the bucket root.
	Prefix string
	// PathStyle addresses the bucket as a path segment
	// (endpoint/bucket/key) rather than as a subdomain
	// (bucket.endpoint/key). Every self-hosted S3-compatible server wants
	// path style; AWS still accepts it.
	PathStyle bool
	// HTTP is the client requests ride. Nil takes a client with a timeout
	// that fits the 64 MiB body cap.
	HTTP *http.Client
}

// rePrefix bounds a key prefix to the same alphabet as the rest of a key, so
// no configured string can introduce a query, an escape or a `..` segment into
// a signed path.
var rePrefix = regexp.MustCompile(`^[A-Za-z0-9_.\-/]*$`)

// S3 keeps blob bytes in an S3-compatible bucket under
// <prefix><repository>/<digest>. It is the hosted answer.
//
// What it gives up against the postgres backend, both deliberately: a database
// dump is no longer a whole backup (the bucket is the second half, and
// docs/operations.md says so), and row level security does not reach a bucket
// — the repository is a key prefix, and the credentials this process holds
// reach every repository in the bucket.
//
// The bytes are stored as they arrived, in the clear. That is a decision, not
// an oversight: see docs/decisions/0021-blob-bytes-outside-postgres-are-stored-plaintext.md.
type S3 struct {
	cfg    S3Config
	client *http.Client
	base   *url.URL
}

// NewS3 opens the s3 backend. It contacts nothing: a bucket that does not
// exist or credentials that do not sign are found at the first Put, which is
// where the error names what it was doing.
func NewS3(cfg S3Config) (*S3, error) {
	switch {
	case cfg.Endpoint == "":
		return nil, errors.New("blobbytes: the s3 backend needs an endpoint URL")
	case cfg.Bucket == "":
		return nil, errors.New("blobbytes: the s3 backend needs a bucket")
	case cfg.Region == "":
		return nil, errors.New("blobbytes: the s3 backend needs a region")
	case cfg.AccessKeyID == "" || cfg.SecretAccessKey == "":
		return nil, errors.New("blobbytes: the s3 backend needs an access key id and a secret access key")
	case !rePrefix.MatchString(cfg.Prefix):
		return nil, fmt.Errorf("blobbytes: the s3 key prefix %q may hold only letters, digits, `_`, `.`, `-` and `/`", cfg.Prefix)
	}
	base, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("blobbytes: the s3 endpoint %q is not a URL: %w", cfg.Endpoint, err)
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("blobbytes: the s3 endpoint %q needs a scheme and a host", cfg.Endpoint)
	}
	if cfg.Prefix != "" && !strings.HasSuffix(cfg.Prefix, "/") {
		cfg.Prefix += "/"
	}
	client := cfg.HTTP
	if client == nil {
		// Long enough for a 64 MiB body on a slow link, short enough that a
		// dead endpoint does not hold a request goroutine forever.
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	return &S3{cfg: cfg, client: client, base: base}, nil
}

// Name is BackendS3.
func (*S3) Name() string { return BackendS3 }

// Repository binds the backend to one repository's key prefix.
func (s *S3) Repository(repository string, _ DB) (Store, error) {
	if err := checkRepository(repository); err != nil {
		return nil, err
	}
	return &s3Store{s3: s, prefix: s.cfg.Prefix + repository + "/"}, nil
}

type s3Store struct {
	s3     *S3
	prefix string
}

func (*s3Store) Backend() string { return BackendS3 }

// key is the one place an object key is built.
func (s *s3Store) key(digest string) (string, error) {
	if err := checkDigest(digest); err != nil {
		return "", err
	}
	return s.prefix + digest, nil
}

// Put writes the bytes in one request. The payload hash SigV4 signs with is
// the digest's own hex, which is what makes the request self-checking: a body
// that is not the bytes the digest names fails the endpoint's signature check
// rather than landing under the wrong key.
func (s *s3Store) Put(ctx context.Context, digest string, size int64, r io.Reader) error {
	key, err := s.key(digest)
	if err != nil {
		return err
	}
	payloadHash := strings.TrimPrefix(digest, "blob-sha256-")
	req, err := s.s3.request(ctx, http.MethodPut, key, nil, r, payloadHash)
	if err != nil {
		return err
	}
	req.ContentLength = size
	resp, err := s.s3.do(req)
	if err != nil {
		return err
	}
	defer drain(resp)
	return s3Error(resp, "put")
}

func (s *s3Store) Open(ctx context.Context, digest string) (io.ReadCloser, error) {
	key, err := s.key(digest)
	if err != nil {
		return nil, err
	}
	req, err := s.s3.request(ctx, http.MethodGet, key, nil, nil, emptyPayloadHash)
	if err != nil {
		return nil, err
	}
	resp, err := s.s3.do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		drain(resp)
		return nil, fmt.Errorf("%w: %s", ErrNotStored, digest)
	}
	if err := s3Error(resp, "get"); err != nil {
		drain(resp)
		return nil, err
	}
	return resp.Body, nil
}

func (s *s3Store) Exists(ctx context.Context, digest string) (bool, error) {
	key, err := s.key(digest)
	if err != nil {
		return false, err
	}
	req, err := s.s3.request(ctx, http.MethodHead, key, nil, nil, emptyPayloadHash)
	if err != nil {
		return false, err
	}
	resp, err := s.s3.do(req)
	if err != nil {
		return false, err
	}
	defer drain(resp)
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if err := s3Error(resp, "head"); err != nil {
		return false, err
	}
	return true, nil
}

// Delete removes the object. S3 answers 204 whether or not the key was there,
// which is exactly the idempotence the sweep needs.
func (s *s3Store) Delete(ctx context.Context, digest string) error {
	key, err := s.key(digest)
	if err != nil {
		return err
	}
	req, err := s.s3.request(ctx, http.MethodDelete, key, nil, nil, emptyPayloadHash)
	if err != nil {
		return err
	}
	resp, err := s.s3.do(req)
	if err != nil {
		return err
	}
	defer drain(resp)
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return s3Error(resp, "delete")
}

// listResult is the half of a ListObjectsV2 response this reads.
type listResult struct {
	XMLName  xml.Name `xml:"ListBucketResult"`
	Contents []struct {
		Key          string    `xml:"Key"`
		Size         int64     `xml:"Size"`
		LastModified time.Time `xml:"LastModified"`
	} `xml:"Contents"`
}

// List runs one ListObjectsV2 page. S3 returns keys in ascending order and
// `start-after` is the cursor, so the sweep walks a big bucket in batches
// without paying for the whole listing at once.
func (s *s3Store) List(ctx context.Context, after string, limit int) ([]Object, error) {
	q := url.Values{}
	q.Set("list-type", "2")
	q.Set("prefix", s.prefix)
	if after != "" {
		if err := checkDigest(after); err != nil {
			return nil, err
		}
		q.Set("start-after", s.prefix+after)
	}
	if limit > 0 {
		q.Set("max-keys", strconv.Itoa(limit))
	}
	req, err := s.s3.request(ctx, http.MethodGet, "", q, nil, emptyPayloadHash)
	if err != nil {
		return nil, err
	}
	resp, err := s.s3.do(req)
	if err != nil {
		return nil, err
	}
	defer drain(resp)
	if err := s3Error(resp, "list"); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody*100))
	if err != nil {
		return nil, err
	}
	var parsed listResult
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("blobbytes: s3 list: %w", err)
	}
	out := make([]Object, 0, len(parsed.Contents))
	for _, c := range parsed.Contents {
		digest := strings.TrimPrefix(c.Key, s.prefix)
		// A key under this repository's prefix that is not a digest is not
		// this store's object, and deleting it is not this sweep's business.
		if !reDigest.MatchString(digest) {
			continue
		}
		out = append(out, Object{Digest: digest, Size: c.Size, At: c.LastModified.UTC()})
	}
	return out, nil
}

// request builds a signed S3 request. key is the object key without the
// bucket; an empty key with a query is a bucket-level call (the listing).
func (s *S3) request(ctx context.Context, method, key string, query url.Values, body io.Reader, payloadHash string) (*http.Request, error) {
	u := *s.base
	path := "/" + key
	if s.cfg.PathStyle {
		path = "/" + s.cfg.Bucket + path
	} else {
		u.Host = s.cfg.Bucket + "." + u.Host
	}
	u.Path = path
	u.RawPath = ""
	rawQuery := canonicalQuery(query)
	u.RawQuery = rawQuery
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if err := s.sign(req, path, rawQuery, payloadHash); err != nil {
		return nil, err
	}
	return req, nil
}

func (s *S3) do(req *http.Request) (*http.Response, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		// The URL an http error quotes carries the endpoint but never the
		// credentials, which travel in headers.
		return nil, fmt.Errorf("blobbytes: s3 %s: %w", strings.ToLower(req.Method), err)
	}
	return resp, nil
}

// maxErrorBody bounds how much of a failure response is read back into the
// error message.
const maxErrorBody = 2048

// s3Error turns a non-2xx response into an error carrying the status and the
// endpoint's own error code, which is what tells a misconfigured bucket from a
// misconfigured credential.
func s3Error(resp *http.Response, what string) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var parsed struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	_ = xml.Unmarshal(body, &parsed)
	if parsed.Code != "" {
		return fmt.Errorf("blobbytes: s3 %s: %s (%s: %s)", what, resp.Status, parsed.Code, parsed.Message)
	}
	return fmt.Errorf("blobbytes: s3 %s: %s", what, resp.Status)
}

// drain closes a response body, reading the little that is left so the
// connection returns to the pool.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
	_ = resp.Body.Close()
}
