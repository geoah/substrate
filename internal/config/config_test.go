package config

import (
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// The credential key is the AES-256 key that unwraps every repository's DEK, so
// its strength must be a property the code checks, not a passphrase an operator
// promises (ADR 0024). Validate accepts standard-base64 of exactly 32 bytes and
// nothing else, and every refusal names the command that generates one.
func TestValidateCredentialKey(t *testing.T) {
	t.Parallel()

	good := base64.StdEncoding.EncodeToString(mustRandom(t, 32))

	refused := map[string]string{
		"empty":              "",
		"short passphrase":   "hunter2",
		"non-base64":         "not base64 at all!!",
		"base64 of 16 bytes": base64.StdEncoding.EncodeToString(mustRandom(t, 16)),
		"base64 of 31 bytes": base64.StdEncoding.EncodeToString(mustRandom(t, 31)),
		"base64 of 33 bytes": base64.StdEncoding.EncodeToString(mustRandom(t, 33)),
		"hex of 32 bytes":    "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
	}
	for name, key := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCredentialKey(key)
			if err == nil {
				t.Fatalf("ValidateCredentialKey(%q) accepted a bad key", key)
			}
			if !strings.Contains(err.Error(), "openssl rand -base64 32") {
				t.Fatalf("refusal does not name the generator command: %v", err)
			}
			if !strings.Contains(err.Error(), "SUBSTRATE_CREDENTIAL_KEY") {
				t.Fatalf("refusal does not name the variable: %v", err)
			}
		})
	}

	if err := ValidateCredentialKey(good); err != nil {
		t.Fatalf("ValidateCredentialKey rejected base64 of 32 bytes: %v", err)
	}
	data := Data{Root: t.TempDir()}
	if err := (Config{Port: "8080", CredentialKey: good, Data: data, Archive: Archive{Interval: 10 * time.Second}, RepositoryConnections: 16, TriggerInterval: 5 * time.Second, TriggerLaneWorkers: 4, HealthFailingAfter: time.Hour}).Validate(); err != nil {
		t.Fatalf("Config.Validate rejected a good key: %v", err)
	}
	if err := (Config{Port: "8080", CredentialKey: "", Data: data}).Validate(); err == nil {
		t.Fatal("Config.Validate accepted an unset credential key")
	}
}

// The data root is where every repository directory lives, so there is no
// default and no relative form: a relative root would follow the working
// directory and lose the store on a restart from elsewhere.
func TestDataValidate(t *testing.T) {
	t.Parallel()
	abs := t.TempDir()
	refused := map[string]Data{
		"empty root":        {Root: ""},
		"relative root":     {Root: "data"},
		"dot-relative root": {Root: "./data"},
	}
	for name, d := range refused {
		err := d.Validate()
		if err == nil {
			t.Fatalf("%s: Validate accepted %+v", name, d)
		}
		if !strings.Contains(err.Error(), "SUBSTRATE_DATA_ROOT") {
			t.Fatalf("%s: refusal does not name the variable: %v", name, err)
		}
	}
	if err := (Data{Root: abs}).Validate(); err != nil {
		t.Fatalf("Validate rejected an absolute root: %v", err)
	}
	if err := (Config{Data: Data{Root: "relative"}}).Validate(); err == nil ||
		!strings.Contains(err.Error(), "SUBSTRATE_DATA_ROOT") {
		t.Fatalf("Config.Validate did not refuse a relative data root: %v", err)
	}
}

// LoadData reads the environment: the root is required. Setenv, so not
// parallel.
func TestLoadData(t *testing.T) {
	// t.Setenv restores both at the end; the Unsetenv makes them absent
	// rather than empty, which is the shape a fresh environment has.
	t.Setenv("SUBSTRATE_DATA_ROOT", "")
	_ = os.Unsetenv("SUBSTRATE_DATA_ROOT")
	if _, err := LoadData(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_DATA_ROOT") {
		t.Fatalf("LoadData without a root: err = %v, want a refusal naming SUBSTRATE_DATA_ROOT", err)
	}
	t.Setenv("SUBSTRATE_DATA_ROOT", "relative/data")
	if _, err := LoadData(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_DATA_ROOT") {
		t.Fatalf("LoadData with a relative root: err = %v, want a refusal naming SUBSTRATE_DATA_ROOT", err)
	}
	root := t.TempDir()
	t.Setenv("SUBSTRATE_DATA_ROOT", root)
	d, err := LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if d.Root != root {
		t.Fatalf("Root = %q, want %q", d.Root, root)
	}
}

func mustRandom(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return b
}

func TestRepositoryConnectionsRefusesACapUnderTheFloor(t *testing.T) {
	data := Data{Root: "/srv/substrate"}
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	for _, n := range []int{-1, 0, 1, MinRepositoryConnections - 1} {
		if err := (Config{Port: "8080", CredentialKey: key, Data: data, Archive: Archive{Interval: 10 * time.Second}, RepositoryConnections: n}).Validate(); err == nil ||
			!strings.Contains(err.Error(), "SUBSTRATE_REPOSITORY_CONNECTIONS") {
			t.Fatalf("a cap of %d: err = %v, want a refusal naming SUBSTRATE_REPOSITORY_CONNECTIONS", n, err)
		}
	}
	if err := (Config{Port: "8080", CredentialKey: key, Data: data, Archive: Archive{Interval: 10 * time.Second}, RepositoryConnections: MinRepositoryConnections, TriggerInterval: 5 * time.Second, TriggerLaneWorkers: 4, HealthFailingAfter: time.Hour}).Validate(); err != nil {
		t.Fatalf("the floor itself was refused: %v", err)
	}
}

// The dispatcher tick feeds time.NewTicker, which panics at zero or below, so
// Validate refuses both before the boot reaches it and names the variable.
func TestTriggerIntervalRefusesZeroAndNegative(t *testing.T) {
	t.Parallel()
	data := Data{Root: "/srv/substrate"}
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	for _, d := range []time.Duration{0, -time.Second} {
		err := (Config{Port: "8080", CredentialKey: key, Data: data, Archive: Archive{Interval: 10 * time.Second}, RepositoryConnections: 16, TriggerInterval: d}).Validate()
		if err == nil || !strings.Contains(err.Error(), "SUBSTRATE_TRIGGER_INTERVAL") {
			t.Fatalf("a tick of %s: err = %v, want a refusal naming SUBSTRATE_TRIGGER_INTERVAL", d, err)
		}
	}
	if err := (Config{Port: "8080", CredentialKey: key, Data: data, Archive: Archive{Interval: 10 * time.Second}, RepositoryConnections: 16, TriggerInterval: time.Second, TriggerLaneWorkers: 4, HealthFailingAfter: time.Hour}).Validate(); err != nil {
		t.Fatalf("a one-second tick was refused: %v", err)
	}
}

// The lane cap sizes a buffered channel the schedule lane sends on, which
// blocks forever at zero, and a cap above the process's delivery slots could
// never be reached, so Validate refuses both and names the variable.
func TestTriggerLaneWorkersRefusesZeroAndAboveTheProcessCeiling(t *testing.T) {
	t.Parallel()
	data := Data{Root: "/srv/substrate"}
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	base := Config{Port: "8080", CredentialKey: key, Data: data, Archive: Archive{Interval: 10 * time.Second}, RepositoryConnections: 16, TriggerInterval: time.Second, HealthFailingAfter: time.Hour}
	for _, n := range []int{-1, 0, MaxTriggerLaneWorkers + 1} {
		refused := base
		refused.TriggerLaneWorkers = n
		if err := refused.Validate(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_TRIGGER_LANE_WORKERS") {
			t.Fatalf("%d lane workers: err = %v, want a refusal naming SUBSTRATE_TRIGGER_LANE_WORKERS", n, err)
		}
	}
	for _, n := range []int{1, 4, MaxTriggerLaneWorkers} {
		ok := base
		ok.TriggerLaneWorkers = n
		if err := ok.Validate(); err != nil {
			t.Fatalf("%d lane workers were refused: %v", n, err)
		}
	}
}

// The failing window is a duration the dispatcher compares a park's age
// against, so zero would open the alert on the first park and a negative one
// means nothing: Validate refuses both and names the variable.
func TestHealthFailingAfterRefusesZeroAndNegative(t *testing.T) {
	t.Parallel()
	data := Data{Root: "/srv/substrate"}
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	base := Config{Port: "8080", CredentialKey: key, Data: data, Archive: Archive{Interval: 10 * time.Second}, RepositoryConnections: 16, TriggerInterval: time.Second, TriggerLaneWorkers: 4}
	for _, d := range []time.Duration{0, -time.Minute} {
		refused := base
		refused.HealthFailingAfter = d
		if err := refused.Validate(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_HEALTH_FAILING_AFTER") {
			t.Fatalf("a window of %s: err = %v, want a refusal naming SUBSTRATE_HEALTH_FAILING_AFTER", d, err)
		}
	}
	ok := base
	ok.HealthFailingAfter = 15 * time.Minute
	if err := ok.Validate(); err != nil {
		t.Fatalf("a 15 minute window was refused: %v", err)
	}
}

// The digest's rate cap is bytes per second: zero removes it, and a
// negative value or one under the floor (a unit mistake) is refused naming
// the variable.
func TestArchiveBytesPerSecondRefusesANegativeOrTinyCap(t *testing.T) {
	t.Parallel()
	data := Data{Root: "/srv/substrate"}
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	base := Config{Port: "8080", CredentialKey: key, Data: data, Archive: Archive{Interval: 10 * time.Second}, RepositoryConnections: 16, TriggerInterval: time.Second, TriggerLaneWorkers: 4, HealthFailingAfter: time.Hour}
	for _, n := range []int64{-1, 8, MinArchiveBytesPerSecond - 1} {
		refused := base
		refused.ArchiveBytesPerSecond = n
		if err := refused.Validate(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_ARCHIVE_BYTES_PER_SECOND") {
			t.Fatalf("a cap of %d: err = %v, want a refusal naming SUBSTRATE_ARCHIVE_BYTES_PER_SECOND", n, err)
		}
	}
	for _, n := range []int64{0, MinArchiveBytesPerSecond, 8 << 20} {
		ok := base
		ok.ArchiveBytesPerSecond = n
		if err := ok.Validate(); err != nil {
			t.Fatalf("a cap of %d was refused: %v", n, err)
		}
	}
}

// The server speaks plain HTTP, so where it listens is the one thing it can
// check about who reads its traffic. A loopback address needs nothing; every
// other one, every interface included, is refused with a message naming the
// address and the setting that admits it, and admitted once that setting is
// on.
func TestListenAddressAdmitsLoopbackAndRefusesTheRestUnlessCleartextIsAllowed(t *testing.T) {
	t.Parallel()
	for host, want := range map[string]string{
		"127.0.0.1": "127.0.0.1:8080",
		"127.0.0.2": "127.0.0.2:8080",
		"::1":       "[::1]:8080",
		"[::1]":     "[::1]:8080",
		"localhost": "127.0.0.1:8080",
		"LocalHost": "127.0.0.1:8080",
	} {
		got, err := (Config{BindAddress: host, Port: "8080"}).ListenAddress()
		if err != nil || got != want {
			t.Errorf("ListenAddress(%q) = %q, %v; want %q with no setting", host, got, err, want)
		}
	}

	for host, want := range map[string]string{
		"0.0.0.0":               "0.0.0.0:8080",
		"":                      ":8080",
		"::":                    "[::]:8080",
		"192.0.2.10":            "192.0.2.10:8080",
		"substrate.example.com": "substrate.example.com:8080",
	} {
		_, err := (Config{BindAddress: host, Port: "8080"}).ListenAddress()
		if err == nil {
			t.Errorf("ListenAddress(%q) admitted a non-loopback bind with no setting", host)
			continue
		}
		for _, named := range []string{"SUBSTRATE_BIND_ADDRESS", "SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true", want} {
			if !strings.Contains(err.Error(), named) {
				t.Errorf("ListenAddress(%q) refusal does not name %q: %v", host, named, err)
			}
		}

		got, err := (Config{BindAddress: host, Port: "8080", InsecureAllowCleartext: true}).ListenAddress()
		if err != nil || got != want {
			t.Errorf("ListenAddress(%q) with SUBSTRATE_INSECURE_ALLOW_CLEARTEXT = %q, %v; want %q", host, got, err, want)
		}
	}
}

// Unset, the bind is loopback and the escape is off: the default is the one
// that needs nothing in front of it. Setenv, so not parallel.
func TestLoadBindsLoopbackByDefault(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/substrate")
	t.Setenv("SUBSTRATE_DATA_ROOT", t.TempDir())
	t.Setenv("PORT", "8080")
	// Present-but-empty is not unset: the Unsetenv makes both absent, the
	// shape a fresh environment has, and t.Setenv restores them afterwards.
	t.Setenv("SUBSTRATE_BIND_ADDRESS", "")
	t.Setenv("SUBSTRATE_INSECURE_ALLOW_CLEARTEXT", "")
	_ = os.Unsetenv("SUBSTRATE_BIND_ADDRESS")
	_ = os.Unsetenv("SUBSTRATE_INSECURE_ALLOW_CLEARTEXT")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.BindAddress != "127.0.0.1" || c.InsecureAllowCleartext {
		t.Fatalf("defaults: SUBSTRATE_BIND_ADDRESS = %q, SUBSTRATE_INSECURE_ALLOW_CLEARTEXT = %v; want 127.0.0.1 and false",
			c.BindAddress, c.InsecureAllowCleartext)
	}
	if addr, err := c.ListenAddress(); err != nil || addr != "127.0.0.1:8080" {
		t.Fatalf("default ListenAddress = %q, %v; want 127.0.0.1:8080", addr, err)
	}
}

// A port, a log level or a URL the server cannot use was once taken in
// silence: a console URL without a scheme made the OAuth return page post to
// "*", PORT=abc failed at the listener only after minutes of boot, and
// LOG_LEVEL=trace ran at info. Validate refuses each before the boot, naming
// the variable, and admits every value that worked before.
func TestValidateRefusesAPortLogLevelOrURLTheServerCannotUse(t *testing.T) {
	t.Parallel()
	data := Data{Root: "/srv/substrate"}
	base := Config{
		Port: "8080", LogLevel: "info", CredentialKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", Data: data,
		Archive:               Archive{Interval: 10 * time.Second},
		RepositoryConnections: 16, TriggerInterval: time.Second, TriggerLaneWorkers: 4, HealthFailingAfter: time.Hour,
	}
	badURLs := []string{
		"substrate.example.com", "//substrate.example.com", "/console", "ftp://substrate.example.com", "https://",
		"https://:8080", "http//substrate.example.com", "https://substrate.example.com:65536", "https://substrate.example.com:0",
	}
	refused := map[string][]func(*Config){
		"PORT": {
			func(c *Config) { c.Port = "" }, func(c *Config) { c.Port = "0" }, func(c *Config) { c.Port = "65536" },
			func(c *Config) { c.Port = "abc" }, func(c *Config) { c.Port = "-1" }, func(c *Config) { c.Port = "+80" },
			func(c *Config) { c.Port = "http" },
		},
		"LOG_LEVEL": {
			func(c *Config) { c.LogLevel = "trace" }, func(c *Config) { c.LogLevel = "warning" }, func(c *Config) { c.LogLevel = " info" },
		},
		"LOG_FORMAT": {
			func(c *Config) { c.LogFormat = "logfmt" }, func(c *Config) { c.LogFormat = "jsonl" }, func(c *Config) { c.LogFormat = " json" },
		},
	}
	for _, raw := range badURLs {
		refused["SUBSTRATE_CONSOLE_URL"] = append(refused["SUBSTRATE_CONSOLE_URL"], func(c *Config) { c.ConsoleURL = raw })
		refused["SUBSTRATE_OAUTH_CALLBACK_URL"] = append(refused["SUBSTRATE_OAUTH_CALLBACK_URL"], func(c *Config) { c.OAuthCallbackURL = raw })
	}
	// The issuer is an origin: https, or http on loopback alone, and nothing
	// after the host.
	for _, raw := range append(badURLs, "http://substrate.example.com", "http://10.0.0.7:8080",
		"https://substrate.example.com/console", "https://substrate.example.com?x=1", "https://user@substrate.example.com",
		"https://substrate.example.com?", "https://substrate.example.com#", "https://substrate.example.com:",
		"HTTPS://substrate.example.com", "https://Substrate.Example.com") {
		refused["SUBSTRATE_PUBLIC_URL"] = append(refused["SUBSTRATE_PUBLIC_URL"], func(c *Config) { c.PublicURL = raw })
	}
	for name, edits := range refused {
		for _, edit := range edits {
			c := base
			edit(&c)
			if err := c.Validate(); err == nil || !strings.Contains(err.Error(), name+" is ") {
				t.Errorf("%+v: err = %v, want a refusal naming %s", c, err, name)
			}
		}
	}

	admitted := []func(*Config){
		func(c *Config) { c.Port = "1" }, func(c *Config) { c.Port = "65535" },
		func(c *Config) { c.LogLevel = "" }, func(c *Config) { c.LogLevel = "DEBUG" }, func(c *Config) { c.LogLevel = "Warn" },
		func(c *Config) { c.LogLevel = "error" },
		func(c *Config) { c.LogFormat = "" }, func(c *Config) { c.LogFormat = "JSON" }, func(c *Config) { c.LogFormat = "Text" },
		func(c *Config) { c.ConsoleURL = "http://localhost:5173" },
		func(c *Config) { c.ConsoleURL = "https://[::1]:8443" },
		func(c *Config) { c.ConsoleURL = "https://substrate.example.com/console/" },
		func(c *Config) { c.OAuthCallbackURL = "https://substrate.example.com/api/v1/oauth/callback" },
		func(c *Config) { c.OAuthCallbackURL = "HTTP://127.0.0.1:8080/api/v1/oauth/callback" },
		func(c *Config) { c.PublicURL = "https://substrate.example.com" },
		func(c *Config) { c.PublicURL = "https://substrate.example.com/" },
		func(c *Config) { c.PublicURL = "http://127.0.0.1:8080" },
		func(c *Config) { c.PublicURL = "http://localhost:8080" },
	}
	for _, edit := range admitted {
		c := base
		edit(&c)
		if err := c.Validate(); err != nil {
			t.Errorf("%+v was refused: %v", c, err)
		}
	}
}

// LOG_LEVEL reads case-insensitively, and empty is the info default.
func TestParseLogLevel(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]slog.Level{
		"": slog.LevelInfo, "info": slog.LevelInfo, "DEBUG": slog.LevelDebug, "Warn": slog.LevelWarn, "error": slog.LevelError,
	} {
		got, err := ParseLogLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLogLevel(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}

// LOG_FORMAT reads case-insensitively, empty is the text default, and a
// refusal names both accepted values and the default.
func TestParseLogFormat(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"": LogFormatText, "text": LogFormatText, "Text": LogFormatText, "json": LogFormatJSON, "JSON": LogFormatJSON,
	} {
		got, err := ParseLogFormat(in)
		if err != nil || got != want {
			t.Errorf("ParseLogFormat(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	_, err := ParseLogFormat("logfmt")
	if err == nil || err.Error() != `LOG_FORMAT is "logfmt": it is text or json (text is the default)` {
		t.Errorf("ParseLogFormat(logfmt) err = %v", err)
	}
}

// Unset is text, so a deployment that never names a format logs as it always
// has. Setenv, so not parallel.
func TestLogFormatDefaultsToText(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/substrate")
	t.Setenv("SUBSTRATE_DATA_ROOT", t.TempDir())
	t.Setenv("LOG_FORMAT", "")
	_ = os.Unsetenv("LOG_FORMAT")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.LogFormat != LogFormatText {
		t.Errorf("LogFormat = %q, want text", c.LogFormat)
	}
}

func TestInsecureAuthorityDocsIsAListOfAuthorityAndPath(t *testing.T) {
	got, err := Config{InsecureAuthorityDocs: " example.org=/tmp/a.json , dev.example.com=/tmp/b.json"}.AuthorityDocs()
	if err != nil || len(got) != 2 || got["example.org"] != "/tmp/a.json" || got["dev.example.com"] != "/tmp/b.json" {
		t.Fatalf("AuthorityDocs = %v, %v", got, err)
	}
	if got, err := (Config{}).AuthorityDocs(); err != nil || got != nil {
		t.Fatalf("unset reads %v, %v", got, err)
	}
	for _, bad := range []string{"example.org", "example.org=", "=/tmp/a.json", "Example.Org=/tmp/a.json", "org=/tmp/a.json", "example.org=/a,example.org=/b"} {
		if _, err := (Config{InsecureAuthorityDocs: bad}).AuthorityDocs(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_INSECURE_AUTHORITY_DOCS") {
			t.Errorf("%q: %v, want a refusal naming the variable", bad, err)
		}
	}
}

// SUBSTRATE_DNS_RESOLVER names a DNS server as host:port, or nothing for
// the system's resolver; anything else refuses the boot, naming the
// variable.
func TestTheDNSResolverIsHostAndPort(t *testing.T) {
	good := base64.StdEncoding.EncodeToString(make([]byte, 32))
	base := Config{
		Port: "8080", CredentialKey: good, Data: Data{Root: t.TempDir()},
		Archive:               Archive{Interval: 10 * time.Second},
		RepositoryConnections: 16, TriggerInterval: 5 * time.Second, TriggerLaneWorkers: 4, HealthFailingAfter: time.Hour,
	}
	for value, ok := range map[string]bool{"": true, "127.0.0.1:53": true, "[::1]:5353": true, "dns.example.net:53": true, "127.0.0.1": false, ":53": false, "dns.example.net:": false} {
		c := base
		c.DNSResolver = value
		err := c.Validate()
		if ok != (err == nil) || (err != nil && !strings.Contains(err.Error(), "SUBSTRATE_DNS_RESOLVER")) {
			t.Errorf("SUBSTRATE_DNS_RESOLVER=%q: Validate = %v", value, err)
		}
	}
}

func TestTrustedProxyPrefixes(t *testing.T) {
	got, err := Config{TrustedProxies: " 10.42.0.0/16, 127.0.0.1 ,,fd00::/8, ::ffff:10.0.0.9"}.TrustedProxyPrefixes()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.42.0.0/16", "127.0.0.1/32", "fd00::/8", "10.0.0.9/32"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Fatalf("entry %d = %s, want %s", i, got[i], want[i])
		}
	}
	if _, err := (Config{TrustedProxies: "10.42.0.0/16, proxy.internal"}).TrustedProxyPrefixes(); err == nil || !strings.Contains(err.Error(), `"proxy.internal"`) {
		t.Fatalf("a hostname entry = %v, want a refusal naming it", err)
	}
}
