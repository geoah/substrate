package blobbytes

// Test-only surface. It lives in an internal test file so the s3 suite can
// create the bucket it then exercises, without the production backend growing
// a bucket-creation door it has no use for: a substrate is pointed at a bucket
// an operator already made, with the access policy they chose.

import (
	"bytes"
	"context"
	"net/http"
)

// PutRawForTest writes an arbitrary key, so a test can put something in the
// bucket that is not a blob and check the listing walks past it.
func (s *S3) PutRawForTest(ctx context.Context, key string, data []byte) error {
	req, err := s.request(ctx, http.MethodPut, key, nil, bytes.NewReader(data), hashHex(data))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(data))
	resp, err := s.do(req)
	if err != nil {
		return err
	}
	defer drain(resp)
	return s3Error(resp, "put")
}

// CreateBucketForTest creates the configured bucket.
func (s *S3) CreateBucketForTest(ctx context.Context) error {
	req, err := s.request(ctx, http.MethodPut, "", nil, nil, emptyPayloadHash)
	if err != nil {
		return err
	}
	resp, err := s.do(req)
	if err != nil {
		return err
	}
	defer drain(resp)
	// A bucket that is already there is the outcome this asks for.
	if resp.StatusCode == http.StatusConflict {
		return nil
	}
	return s3Error(resp, "create bucket")
}
