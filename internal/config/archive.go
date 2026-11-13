package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"

	"github.com/geoah/substrate/internal/archive"
)

// ArchiveOff is the SUBSTRATE_ARCHIVE value that writes no archive.
const ArchiveOff = "off"

// Archive is where the server copies each repository after its writes
// commit (decision 0262). It is on by default, into a directory under the
// data root; an operator who backs up Postgres may turn it off.
type Archive struct {
	// Where is the store: unset is <data root>/archive, a directory; an
	// absolute path is that directory; s3://<bucket>[/<prefix>] is a bucket
	// of an S3-compatible service, reached with the S3 settings below; `off`
	// writes no archive.
	Where string `envconfig:"SUBSTRATE_ARCHIVE" default:""`
	// Interval is how often each repository's archive pass runs: the
	// longest a confirmed write waits, with the store answering, before it
	// is in the archive.
	Interval time.Duration `envconfig:"SUBSTRATE_ARCHIVE_INTERVAL" default:"10s"`
	// S3Endpoint is the service's URL, for one that is not Amazon's own.
	// Unset is Amazon's, in S3Region.
	S3Endpoint string `envconfig:"SUBSTRATE_ARCHIVE_S3_ENDPOINT" default:""`
	S3Region   string `envconfig:"SUBSTRATE_ARCHIVE_S3_REGION" default:"us-east-1"`
	// S3PathStyle names the bucket in the path rather than the host name,
	// which most services other than Amazon's need.
	S3PathStyle bool `envconfig:"SUBSTRATE_ARCHIVE_S3_PATH_STYLE" default:"false"`
	// S3AccessKeyID and S3SecretAccessKey sign the requests. Unset, they are
	// read from AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.
	S3AccessKeyID     string `envconfig:"SUBSTRATE_ARCHIVE_S3_ACCESS_KEY_ID" default:""`
	S3SecretAccessKey string `envconfig:"SUBSTRATE_ARCHIVE_S3_SECRET_ACCESS_KEY" default:""`
}

// LoadArchive reads the archive's settings alone, for the operator hat, which
// reads the archive the server writes.
func LoadArchive() (Archive, error) {
	var a Archive
	if err := envconfig.Process("", &a); err != nil {
		return a, err
	}
	return a, a.Validate()
}

// Validate refuses a setting the store would fail on only once a pass ran,
// naming the variable. It makes no request: an unreachable store is a delay
// the server reports, never a boot that fails.
func (a Archive) Validate() error {
	if a.Interval <= 0 {
		return fmt.Errorf("SUBSTRATE_ARCHIVE_INTERVAL is %s: it is how often each repository's archive pass runs, and it must be a positive duration (10s is the default)", a.Interval)
	}
	_, err := a.Store("/")
	return err
}

// Store opens the store the settings name, nil when the archive is off.
// dataRoot is where the default directory goes.
func (a Archive) Store(dataRoot string) (archive.Store, error) {
	switch where := strings.TrimSpace(a.Where); {
	case where == ArchiveOff:
		return nil, nil
	case where == "":
		return archive.NewDir(filepath.Join(dataRoot, "archive"))
	case strings.HasPrefix(where, "s3://"):
		return a.s3(where)
	case filepath.IsAbs(where):
		return archive.NewDir(where)
	default:
		return nil, fmt.Errorf("SUBSTRATE_ARCHIVE is %q: it is unset (a directory under the data root), %q, an absolute path, or s3://<bucket>[/<prefix>]", where, ArchiveOff)
	}
}

func (a Archive) s3(where string) (archive.Store, error) {
	u, err := url.Parse(where)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("SUBSTRATE_ARCHIVE is %q: an S3 archive is s3://<bucket>[/<prefix>]", where)
	}
	if a.S3Endpoint != "" {
		if e, err := url.Parse(a.S3Endpoint); err != nil || (e.Scheme != "https" && e.Scheme != "http") || e.Host == "" {
			return nil, fmt.Errorf("SUBSTRATE_ARCHIVE_S3_ENDPOINT is %q: it is the service's URL, https://s3.example.com", a.S3Endpoint)
		}
	}
	id, secret := a.S3AccessKeyID, a.S3SecretAccessKey
	if id == "" && secret == "" {
		id, secret = os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY")
	}
	if id == "" || secret == "" {
		return nil, errors.New("SUBSTRATE_ARCHIVE names an S3 bucket and no credentials are set: set SUBSTRATE_ARCHIVE_S3_ACCESS_KEY_ID and SUBSTRATE_ARCHIVE_S3_SECRET_ACCESS_KEY, or AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY")
	}
	return archive.NewS3(archive.S3Config{
		Bucket: u.Host, Prefix: strings.Trim(u.Path, "/"), Endpoint: a.S3Endpoint, Region: a.S3Region,
		PathStyle: a.S3PathStyle, AccessKeyID: id, SecretAccessKey: secret, SessionToken: os.Getenv("AWS_SESSION_TOKEN"),
	})
}
