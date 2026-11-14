package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// S3Config is where an S3 store writes and what it signs with. Nothing here
// has a default that points at a service: an empty Endpoint is Amazon's own,
// which the region selects.
type S3Config struct {
	Bucket string
	// Prefix is put before every key, so one bucket can hold several
	// servers' archives. Empty, or segments ending in `/`.
	Prefix   string
	Endpoint string
	Region   string
	// PathStyle addresses the bucket in the path rather than the host name,
	// which most S3-compatible services other than Amazon's need.
	PathStyle       bool
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	// HTTPClient replaces the SDK's client; a test points it at its own
	// server.
	HTTPClient *http.Client
}

// S3 is a store in a bucket of an S3-compatible service, through the AWS SDK
// (decision 0075 removed a hand-written signer for what it cost to keep).
type S3 struct {
	client *s3.Client
	bucket string
	prefix string
	name   string
}

// NewS3 returns the store cfg names. It checks the configuration and makes no
// request: an unreachable service is a pass that fails and is retried, never a
// server that does not start.
func NewS3(cfg S3Config) (*S3, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("archive: an S3 store needs a bucket")
	}
	if cfg.Prefix != "" {
		if !strings.HasSuffix(cfg.Prefix, "/") {
			cfg.Prefix += "/"
		}
		if err := checkPrefix(cfg.Prefix); err != nil {
			return nil, fmt.Errorf("archive: the S3 prefix: %w", err)
		}
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("archive: an S3 store needs an access key id and a secret access key")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	opts := s3.Options{
		Region:       cfg.Region,
		Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)),
		UsePathStyle: cfg.PathStyle,
		// Checksums only where a request needs one: several S3-compatible
		// services refuse the headers the SDK would otherwise add to every put.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}
	if cfg.HTTPClient != nil {
		opts.HTTPClient = cfg.HTTPClient
	}
	where := "aws:" + cfg.Region
	if cfg.Endpoint != "" {
		opts.BaseEndpoint = aws.String(cfg.Endpoint)
		where = cfg.Endpoint
	}
	return &S3{
		client: s3.New(opts),
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
		name:   "s3:" + where + "/" + cfg.Bucket + "/" + cfg.Prefix,
	}, nil
}

func (s *S3) String() string { return s.name }

func (s *S3) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	if err := CheckKey(key); err != nil {
		return err
	}
	if size < 0 {
		end, err := body.Seek(0, io.SeekEnd)
		if err != nil {
			return err
		}
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			return err
		}
		size = end
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.prefix + key),
		Body: body, ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("archive: put %s: %w", key, err)
	}
	return nil
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := CheckKey(key); err != nil {
		return nil, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.prefix + key)})
	if err != nil {
		if missing(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
		}
		return nil, fmt.Errorf("archive: get %s: %w", key, err)
	}
	return out.Body, nil
}

func (s *S3) List(ctx context.Context, prefix string) ([]Object, error) {
	if err := checkPrefix(prefix); err != nil {
		return nil, err
	}
	var out []Object
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket), Prefix: aws.String(s.prefix + prefix),
	})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("archive: list %s: %w", prefix, err)
		}
		for _, o := range page.Contents {
			key := strings.TrimPrefix(aws.ToString(o.Key), s.prefix)
			if CheckKey(key) != nil {
				continue
			}
			out = append(out, Object{Key: key, Size: aws.ToInt64(o.Size)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if err := CheckKey(key); err != nil {
		return err
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.prefix + key)})
	if err != nil && !missing(err) {
		return fmt.Errorf("archive: delete %s: %w", key, err)
	}
	return nil
}

// missing reports an answer that the key holds nothing: S3's NoSuchKey, or a
// bare 404 from a service that sends no code with it.
func missing(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var re *smithyhttp.ResponseError
	return errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotFound
}
