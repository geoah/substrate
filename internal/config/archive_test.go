package config

import (
	"strings"
	"testing"
	"time"
)

// The archive's setting names a directory, a bucket or nothing, and a value
// that is none of them is refused at the boot, naming the variable. Nothing
// here reaches a service.
func TestTheArchiveSettingNamesItsStore(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	creds := Archive{Interval: time.Second, S3AccessKeyID: "test-access-key", S3SecretAccessKey: "test-secret-key"}
	cases := map[string]struct {
		set  func(*Archive)
		want string // the store's name, "" for none, or the refusal's variable
		err  bool
	}{
		"unset is the data root's":   {set: func(a *Archive) {}, want: "dir:/srv/substrate/archive"},
		"off is none":                {set: func(a *Archive) { a.Where = "off" }},
		"an absolute directory":      {set: func(a *Archive) { a.Where = "/mnt/backup" }, want: "dir:/mnt/backup"},
		"a relative directory":       {set: func(a *Archive) { a.Where = "backup" }, want: "SUBSTRATE_ARCHIVE", err: true},
		"a bucket":                   {set: func(a *Archive) { a.Where = "s3://archive-bucket/substrate" }, want: "s3:aws:us-east-1/archive-bucket/substrate/"},
		"a bucket at an endpoint":    {set: func(a *Archive) { a.Where, a.S3Endpoint = "s3://b", "https://s3.example.com" }, want: "s3:https://s3.example.com/b/"},
		"an endpoint with no scheme": {set: func(a *Archive) { a.Where, a.S3Endpoint = "s3://b", "s3.example.com" }, want: "SUBSTRATE_ARCHIVE_S3_ENDPOINT", err: true},
		"a bucket with no name":      {set: func(a *Archive) { a.Where = "s3:///prefix" }, want: "SUBSTRATE_ARCHIVE", err: true},
		"a bucket with no keys": {set: func(a *Archive) {
			a.Where, a.S3AccessKeyID, a.S3SecretAccessKey = "s3://b", "", ""
		}, want: "SUBSTRATE_ARCHIVE_S3_ACCESS_KEY_ID", err: true},
		"no interval": {set: func(a *Archive) { a.Interval = 0 }, want: "SUBSTRATE_ARCHIVE_INTERVAL", err: true},
	}
	for name, c := range cases {
		a := creds
		a.S3Region = "us-east-1"
		c.set(&a)
		if err := a.Validate(); c.err {
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: %v, want a refusal naming %s", name, err, c.want)
			}
			continue
		} else if err != nil {
			t.Errorf("%s: refused: %v", name, err)
			continue
		}
		store, err := a.Store("/srv/substrate")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got := ""
		if store != nil {
			got = store.String()
		}
		if got != c.want {
			t.Errorf("%s: the store is %q, want %q", name, got, c.want)
		}
		if strings.Contains(got, "test-secret-key") {
			t.Errorf("%s: the store's name carries the secret", name)
		}
	}
}
