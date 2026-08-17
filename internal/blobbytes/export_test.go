package blobbytes

// Test-only surface. It lives in an internal test file so the s3 suite can
// create the bucket it then exercises, without the production backend growing
// a bucket-creation door it has no use for: a substrate is pointed at a bucket
// an operator already made, with the access policy they chose.

import (
	"context"
	"net/http"
)

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
