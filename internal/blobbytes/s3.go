package blobbytes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
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
	// this substrate. Two substrates must never share one: each one's orphan
	// sweep and `repository blobs prune` delete what its own manifests do not
	// name.
	Prefix string
	// PathStyle addresses the bucket as a path segment
	// (endpoint/bucket/key) rather than as a subdomain
	// (bucket.endpoint/key). Self-hosted S3-compatible servers want path
	// style; AWS still accepts it.
	PathStyle bool
}

// rePrefix bounds a key prefix to the same alphabet as the rest of a key, so
// no configured string can introduce a query, an escape or a `..` segment into
// an object key.
var rePrefix = regexp.MustCompile(`^[A-Za-z0-9_.\-/]*$`)

// s3Timeout bounds one request: long enough for a 64 MiB body on a slow link,
// short enough that a dead endpoint does not hold a request goroutine forever.
// The SDK sets none of its own.
const s3Timeout = 5 * time.Minute

// S3 keeps blob bytes in an S3-compatible bucket under
// <prefix>repositories/<repository>/blobs/<digest>: the same relative path
// the fs backend uses under the data root, so a bucket prefix copied to disk
// is a data root's blob tree and `repository blobs migrate` is a copy, not a
// transform.
//
// What it gives up against the fs backend: the repository directory is no
// longer a whole backup (the bucket is the second half, and a snapshot is the
// one artifact that holds both), and row level security does not reach a
// bucket — the repository is a key prefix, and the credentials this process
// holds reach every repository under the prefix.
//
// The bytes are stored as they arrived, in the clear
// (docs/decisions/0031-blob-bytes-outside-postgres-are-stored-plaintext.md).
//
// Only a general-purpose bucket is supported: ListObjectsV2 must return keys
// in ascending order and honor StartAfter, which the orphan sweep's cursor
// and the migration's merge walk both rely on, and S3 directory buckets do
// neither.
type S3 struct {
	cfg    S3Config
	client *s3.Client
}

// NewS3 opens the s3 backend. It contacts nothing; Check is the probe a
// process runs before it serves.
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
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("blobbytes: the s3 endpoint %q is not a URL: %w", cfg.Endpoint, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("blobbytes: the s3 endpoint %q needs a scheme and a host", cfg.Endpoint)
	}
	if cfg.Prefix != "" && !strings.HasSuffix(cfg.Prefix, "/") {
		cfg.Prefix += "/"
	}
	client := s3.New(s3.Options{
		BaseEndpoint: aws.String(cfg.Endpoint),
		Region:       cfg.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		UsePathStyle: cfg.PathStyle,
		HTTPClient:   awshttp.NewBuildableClient().WithTimeout(s3Timeout),
		// WhenRequired, both ways: the SDK's default adds CRC32 trailers and
		// aws-chunked bodies that S3-compatible servers do not all accept,
		// and integrity is the digest's job here (OpenVerified, ReadAll).
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &S3{cfg: cfg, client: client}, nil
}

// Check proves the bucket is there and the credentials may list it, with one
// ListObjectsV2 of at most one key under the prefix. Listing, rather than
// HeadBucket, because a key scoped to one bucket is often refused HeadBucket
// and is always granted ListBucket: a HEAD on an absent object answers 403
// without it, which would read as an error rather than as ErrNotStored.
func (s *S3) Check(ctx context.Context) error {
	_, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.cfg.Bucket),
		Prefix:  aws.String(s.cfg.Prefix),
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return fmt.Errorf("blobbytes: s3 bucket %q at %s: %w", s.cfg.Bucket, s.cfg.Endpoint, err)
	}
	return nil
}

// String names the store for a report: the endpoint, the bucket and the
// prefix, never a credential.
func (s *S3) String() string {
	return "s3 " + s.cfg.Endpoint + "/" + s.cfg.Bucket + "/" + s.cfg.Prefix
}

// Repository binds the backend to one repository's key prefix.
func (s *S3) Repository(repository string) (Store, error) {
	if err := checkRepository(repository); err != nil {
		return nil, err
	}
	return &s3Store{s3: s, prefix: s.cfg.Prefix + repositoriesDir + "/" + repository + "/" + blobsDir + "/"}, nil
}

type s3Store struct {
	s3     *S3
	prefix string
}

// key is the one place an object key is built.
func (s *s3Store) key(digest string) (*string, error) {
	if err := checkDigest(digest); err != nil {
		return nil, err
	}
	return aws.String(s.prefix + digest), nil
}

// Put writes the bytes in one request: a blob is at most the 64 MiB upload
// cap, far under the single-PUT limit, so there is no multipart path. The
// SDK signs the payload's SHA-256, which needs a body it can seek back over;
// a reader that cannot is read into memory first, bounded by size.
func (s *s3Store) Put(ctx context.Context, digest string, size int64, r io.Reader) error {
	key, err := s.key(digest)
	if err != nil {
		return err
	}
	body, err := seekableBody(r, size)
	if err != nil {
		return err
	}
	_, err = s.s3.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.s3.cfg.Bucket),
		Key:           key,
		Body:          body,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("blobbytes: s3 put %s: %w", digest, err)
	}
	return nil
}

// seekableBody hands back r when it can seek and holds exactly size bytes
// from where it stands, and otherwise reads it into memory, refusing a reader
// that is not size bytes long as the fs backend does.
func seekableBody(r io.Reader, size int64) (io.ReadSeeker, error) {
	if rs, ok := r.(io.ReadSeeker); ok {
		cur, err := rs.Seek(0, io.SeekCurrent)
		if err == nil {
			end, err := rs.Seek(0, io.SeekEnd)
			if err != nil {
				return nil, err
			}
			if _, err := rs.Seek(cur, io.SeekStart); err != nil {
				return nil, err
			}
			if n := end - cur; n != size {
				return nil, fmt.Errorf("blobbytes: got %d bytes, the caller promised %d", n, size)
			}
			return rs, nil
		}
	}
	data, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, fmt.Errorf("blobbytes: got %d bytes, the caller promised %d", len(data), size)
	}
	return bytes.NewReader(data), nil
}

// Open streams the object. The body is read under ctx, so a caller that
// cancels mid-copy ends the transfer rather than leaving it to the timeout.
func (s *s3Store) Open(ctx context.Context, digest string) (io.ReadCloser, error) {
	key, err := s.key(digest)
	if err != nil {
		return nil, err
	}
	out, err := s.s3.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.s3.cfg.Bucket), Key: key})
	if isNotFound(err) {
		return nil, fmt.Errorf("%w: %s", ErrNotStored, digest)
	}
	if err != nil {
		return nil, fmt.Errorf("blobbytes: s3 get %s: %w", digest, err)
	}
	return out.Body, nil
}

func (s *s3Store) Exists(ctx context.Context, digest string) (bool, error) {
	key, err := s.key(digest)
	if err != nil {
		return false, err
	}
	_, err = s.s3.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.s3.cfg.Bucket), Key: key})
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("blobbytes: s3 head %s: %w", digest, err)
	}
	return true, nil
}

// Delete removes the object. S3 answers success whether or not the key was
// there, which is exactly the idempotence the sweep needs.
func (s *s3Store) Delete(ctx context.Context, digest string) error {
	key, err := s.key(digest)
	if err != nil {
		return err
	}
	_, err = s.s3.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.s3.cfg.Bucket), Key: key})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("blobbytes: s3 delete %s: %w", digest, err)
	}
	return nil
}

// maxListKeys is S3's page ceiling.
const maxListKeys = 1000

// List walks ListObjectsV2 from `after`. S3 returns keys in ascending order
// and StartAfter is the cursor, so a sweep or a migration reads a big bucket
// in batches.
//
// It follows the continuation token until it has `limit` objects or the
// listing ends, because MaxKeys bounds the KEYS S3 returns and keys that are
// not digests are dropped here: the orphan sweep reads a short page as the
// end of the store, so a page of other keys must not end the walk early.
func (s *s3Store) List(ctx context.Context, after string, limit int) ([]Object, error) {
	if after != "" {
		if err := checkDigest(after); err != nil {
			return nil, err
		}
	}
	in := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.s3.cfg.Bucket),
		Prefix: aws.String(s.prefix),
	}
	if after != "" {
		in.StartAfter = aws.String(s.prefix + after)
	}
	var out []Object
	for {
		page := int32(maxListKeys)
		if limit > 0 && limit-len(out) < maxListKeys {
			page = int32(limit - len(out))
		}
		in.MaxKeys = aws.Int32(page)
		res, err := s.s3.client.ListObjectsV2(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("blobbytes: s3 list: %w", err)
		}
		for _, c := range res.Contents {
			digest := strings.TrimPrefix(aws.ToString(c.Key), s.prefix)
			// A key under this repository's prefix that is not a digest is not
			// this store's object, and deleting it is not this sweep's
			// business.
			if !reDigest.MatchString(digest) {
				continue
			}
			out = append(out, Object{Digest: digest, Size: aws.ToInt64(c.Size), At: aws.ToTime(c.LastModified).UTC()})
			if limit > 0 && len(out) == limit {
				return out, nil
			}
		}
		if !aws.ToBool(res.IsTruncated) || aws.ToString(res.NextContinuationToken) == "" {
			return out, nil
		}
		in.StartAfter = nil
		in.ContinuationToken = res.NextContinuationToken
	}
}

// isNotFound reports an absent OBJECT. An absent bucket is a configuration
// error and stays one: mapping it to ErrNotStored would read every blob as
// missing rather than refuse.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var noBucket *types.NoSuchBucket
	if errors.As(err, &noBucket) {
		return false
	}
	var noKey *types.NoSuchKey
	var notFound *types.NotFound
	if errors.As(err, &noKey) || errors.As(err, &notFound) {
		return true
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return true
		}
	}
	return false
}
