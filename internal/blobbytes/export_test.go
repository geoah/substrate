package blobbytes

// Test-only surface. It lives in an internal test file so the s3 suite can
// create the bucket it then exercises, without the production backend growing
// a bucket-creation door it has no use for: a substrate is pointed at a bucket
// an operator already made, with the access policy they chose.

import (
	"bytes"
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// PutRawForTest writes an arbitrary key under the configured prefix, so a
// test can put something in the bucket that is not a blob and check the
// listing walks past it.
func (s *S3) PutRawForTest(ctx context.Context, key string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.cfg.Bucket), Key: aws.String(s.cfg.Prefix + key),
		Body: bytes.NewReader(data), ContentLength: aws.Int64(int64(len(data))),
	})
	return err
}

// CreateBucketForTest creates the configured bucket. One that is already
// there is the outcome this asks for.
func (s *S3) CreateBucketForTest(ctx context.Context) error {
	_, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.cfg.Bucket)})
	var owned *types.BucketAlreadyOwnedByYou
	var exists *types.BucketAlreadyExists
	if errors.As(err, &owned) || errors.As(err, &exists) {
		return nil
	}
	return err
}
