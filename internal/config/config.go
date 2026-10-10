// Package config loads the substrate service configuration from the
// environment; there is no settings surface by design.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config is the full service configuration.
type Config struct {
	Port string `envconfig:"PORT" default:"8080"`
	// BindAddress is the interface the server listens on. Loopback by
	// default, because the server speaks plain HTTP: a password, a TOTP code
	// and every bearer token cross this socket readable, so the port is for
	// this machine and a TLS terminator on it. Any other address is refused
	// unless InsecureAllowCleartext says the deployment accounts for that
	// (ListenAddress). Empty is every interface, and refused the same way.
	BindAddress string `envconfig:"SUBSTRATE_BIND_ADDRESS" default:"127.0.0.1"`
	// InsecureAllowCleartext admits a BindAddress that is not loopback. It
	// is the operator's statement that nothing but a TLS terminator, or a
	// port published on a host's loopback, reaches this socket: a container
	// behind its runtime's port mapping, a pod behind an ingress, a binary
	// behind a proxy on another host. Without one of those in front,
	// passwords and bearer tokens cross the network in cleartext.
	InsecureAllowCleartext bool `envconfig:"SUBSTRATE_INSECURE_ALLOW_CLEARTEXT" default:"false"`

	LogLevel string `envconfig:"LOG_LEVEL" default:"info"`
	// WebDir is the built SPA served at /; empty disables static serving
	// (dev mode, where Vite proxies the API).
	WebDir string `envconfig:"WEB_DIR" default:""`

	// DatabaseURL is the one Postgres holding every repository, in one
	// shared schema.
	DatabaseURL string `envconfig:"DATABASE_URL" required:"true"`
	// RepositoryConnections caps the Postgres connections every repository
	// of the process shares; one repository takes at most half of them, and
	// never more than eight. A process holds at most this plus twelve: four
	// for the admin pool, five for the maintenance pool, two for repository
	// migrations and one for a commit-time catch-up (engine
	// MigrationConnections, CatchUpConnections), so the default of 16 holds
	// it to 28 however many repositories it has opened. Below
	// MinRepositoryConnections is refused.
	RepositoryConnections int `envconfig:"SUBSTRATE_REPOSITORY_CONNECTIONS" default:"16"`
	// Data is the data root every repository directory lives under. Its own
	// type, because the operator hat loads it without the rest (LoadData).
	Data Data
	// InviteCode gates the ONE door into a fresh substrate: registering
	// creates a repository and its user, and with a code set the door
	// admits only a request that presents it. UNSET, THE DOOR READS NO CODE
	// and anyone who can reach the substrate may register — the right shape
	// for the laptop the README's quick start runs on, and the wrong one for
	// anything else, so the boot says so. There is no closed state: a
	// substrate that has its user keeps strangers out with a code nobody is
	// given, and discovery reports `registration.inviteRequired` either way.
	InviteCode string `envconfig:"SUBSTRATE_INVITE_CODE" default:""`

	// ConversionCeiling bounds the live records one declaration change (a
	// vocabulary apply, a provider upgrade, the boot upgrade) may rewrite in
	// its transaction: a plan whose estimated work is above it is refused and
	// the previews say so (decision 0067). In records; 0 removes the ceiling.
	ConversionCeiling int64 `envconfig:"SUBSTRATE_CONVERSION_CEILING" default:"10000"`

	// DigestBytesPerSecond caps the rate the process hashes its
	// repositories' finished changelog segments at, behind their opens, over
	// every repository together: hashing is one core busy for as long as
	// the bytes last, and unpaced it starved the function bodies beside it
	// on a four-core box. The digests also pause while any function or
	// agent runs, whatever the cap. 0 removes the cap; a value above 0 and
	// below MinDigestBytesPerSecond is refused, since it is a unit mistake
	// (8 for 8 MiB) that would make a history of gigabytes take years with
	// nothing logged.
	DigestBytesPerSecond int64 `envconfig:"SUBSTRATE_DIGEST_BYTES_PER_SECOND" default:"8388608"`

	// OrphanGrace turns the GC sweep's ORPHAN COLLECTION on and sets its
	// grace window: a mapping target whose live sources are all gone, whose
	// properties are all machine-held, and that no live record points at is
	// tombstoned once it has carried the mark for this long. UNSET OR ZERO IS
	// OFF, which is the default: the mark is derived either way and
	// `filter.orphaned` lists it, so the destructive half is something a
	// deployment asks for by naming a window. A negative value is refused.
	OrphanGrace time.Duration `envconfig:"SUBSTRATE_ORPHAN_GRACE" default:"0"`

	// TriggerInterval is the trigger dispatcher's tick: how often every
	// repository is checked for a trigger due to run, so it bounds the delay
	// between a record write and the delivery it fires. Each tick lists the
	// repositories and runs one pass per idle repository, so a busy host with
	// many repositories may want a slower tick; a test suite that waits on
	// deliveries in a loop wants a faster one. At most 5s by default; zero or
	// negative is refused.
	TriggerInterval time.Duration `envconfig:"SUBSTRATE_TRIGGER_INTERVAL" default:"5s"`
	// TriggerLaneWorkers is how many due schedule fires one repository's
	// dispatcher pass runs at once, each of a different trigger. Every
	// delivery the dispatcher runs also holds one of the process's 16
	// delivery slots, so a higher value lets more of one repository's syncs
	// start together without raising what the process runs at once. From 1,
	// which fires one schedule at a time, to MaxTriggerLaneWorkers.
	TriggerLaneWorkers int `envconfig:"SUBSTRATE_TRIGGER_LANE_WORKERS" default:"4"`

	// HealthFailingAfter is how long every delivery of a trigger must have
	// parked, since its newest ok run, before the dispatcher opens the
	// trigger's `trigger.failing/<trigger id>` alert and its status reads
	// `failing`. The next ok delivery resolves the alert whatever the window.
	// One hour by default; zero or negative is refused.
	HealthFailingAfter time.Duration `envconfig:"SUBSTRATE_HEALTH_FAILING_AFTER" default:"1h"`

	// InsecureDisableTOTP takes the SECOND FACTOR OFF the whole door: login,
	// registration and the credential changes ask for a repository and a
	// password and nothing else. It exists for a local substrate you wipe
	// every day, where enrolling an authenticator to reach a throwaway
	// repository is friction with nothing behind it. NEVER set it on a
	// deployment anybody can reach: a leaked password is then the account.
	// The seed is still minted and still stored, so turning it back off
	// restores the factor the user enrolled.
	InsecureDisableTOTP bool `envconfig:"SUBSTRATE_INSECURE_DISABLE_TOTP" default:"false"`

	// The host OAuth facility (bundles declare auth, the host runs it).
	// StateKey signs flow state; CallbackURL is the one redirect URI every
	// provider app registers. Both unset disables the facility.
	OAuthStateKey    string `envconfig:"SUBSTRATE_OAUTH_STATE_KEY" default:""`
	OAuthCallbackURL string `envconfig:"SUBSTRATE_OAUTH_CALLBACK_URL" default:""`
	// CredentialKey seals the credential store (AES-256-GCM): every
	// repository's DEK wraps under it. It is key material, not a passphrase:
	// standard-base64 of exactly 32 bytes, the AES-256 key itself, which
	// Validate holds it to (ADR 0024). A host without this key would store
	// every DEK wrap plain-marked, so it refuses to boot. There is no
	// exception.
	CredentialKey string `envconfig:"SUBSTRATE_CREDENTIAL_KEY" default:""`
	// ConsoleURL is the console origin the OAuth callback return-page posts its
	// completion message to and falls back to redirecting into. The scheme+host
	// is the postMessage targetOrigin; the full base is the fallback redirect
	// target. Empty (local dev) uses targetOrigin "*" and renders no redirect.
	ConsoleURL string `envconfig:"SUBSTRATE_CONSOLE_URL" default:""`

	// Metrics serves the Prometheus exposition at GET /metrics, unauthenticated
	// and DB-free like /healthz: request latency by route, requests in
	// flight, the pools' sql.DBStats, the trigger dispatcher's pass time and
	// deliveries, and the Go runtime. OFF by default: the path carries route
	// names and pool sizes to anyone who can reach the port, so a deployment
	// that turns it on scrapes the pod directly and keeps /metrics off its
	// ingress. The instruments record either way; this is only the door.
	Metrics bool `envconfig:"SUBSTRATE_METRICS" default:"false"`

	// There is NO LLM configuration here. Completions and embeddings alike are
	// bought through a repository's own llm/provider records, which carry the
	// wire, the endpoint, the key and (for embeddings) the model, so the
	// process holds no bearer that could reach a repository-chosen endpoint.
}

// MaxTriggerLaneWorkers is the largest SUBSTRATE_TRIGGER_LANE_WORKERS
// accepted: the deliveries the dispatcher runs at once over the whole
// process. It is engine.TriggerDeliverySlots, which Open enforces too.
const MaxTriggerLaneWorkers = 16

// MinRepositoryConnections is the smallest SUBSTRATE_REPOSITORY_CONNECTIONS
// accepted. It is engine.MinRepositoryConnections, which Open enforces too.
const MinRepositoryConnections = 4

// Load reads the configuration from the environment.
func Load() (Config, error) {
	var c Config
	err := envconfig.Process("", &c)
	return c, err
}

// Validate refuses a configuration the service cannot run safely, before any
// repository opens.
// MinDigestBytesPerSecond is the smallest cap SUBSTRATE_DIGEST_BYTES_PER_SECOND
// admits above zero: 64 KiB per second, under which a value is a unit
// mistake rather than a rate anyone wants.
const MinDigestBytesPerSecond int64 = 64 << 10

func (c Config) Validate() error {
	if err := c.Data.Validate(); err != nil {
		return err
	}
	if c.RepositoryConnections < MinRepositoryConnections {
		return fmt.Errorf("SUBSTRATE_REPOSITORY_CONNECTIONS is %d: it is the number of Postgres connections every repository shares, one repository takes half of them, and it must be at least %d so each repository gets two", c.RepositoryConnections, MinRepositoryConnections)
	}
	if c.DigestBytesPerSecond < 0 || (c.DigestBytesPerSecond > 0 && c.DigestBytesPerSecond < MinDigestBytesPerSecond) {
		return fmt.Errorf("SUBSTRATE_DIGEST_BYTES_PER_SECOND is %d: it caps the bytes per second the process hashes of its repositories' changelogs behind the open, in bytes, so it is 0 (no cap) or at least %d (64 KiB); 8388608 (8 MiB) is the default", c.DigestBytesPerSecond, MinDigestBytesPerSecond)
	}
	if c.OrphanGrace < 0 {
		return errors.New("SUBSTRATE_ORPHAN_GRACE must not be negative: unset or 0 collects no orphans, and a positive duration (168h) is the window a marked record waits out before the sweep takes it")
	}
	if c.TriggerInterval <= 0 {
		return fmt.Errorf("SUBSTRATE_TRIGGER_INTERVAL is %s: it is how often the trigger dispatcher checks every repository for a delivery due, and it must be a positive duration (5s is the default)", c.TriggerInterval)
	}
	if c.TriggerLaneWorkers < 1 || c.TriggerLaneWorkers > MaxTriggerLaneWorkers {
		return fmt.Errorf("SUBSTRATE_TRIGGER_LANE_WORKERS is %d: it is how many schedule fires one repository's dispatcher pass runs at once, so it is at least 1 and at most %d, the deliveries the whole process runs at once (4 is the default)", c.TriggerLaneWorkers, MaxTriggerLaneWorkers)
	}
	if c.HealthFailingAfter <= 0 {
		return fmt.Errorf("SUBSTRATE_HEALTH_FAILING_AFTER is %s: it is how long every delivery of a trigger must have parked before the trigger reads failing and its alert opens, and it must be a positive duration (1h is the default)", c.HealthFailingAfter)
	}
	return ValidateCredentialKey(c.CredentialKey)
}

// ListenAddress is the host:port the server binds, or a refusal naming the
// setting and the address. A loopback BindAddress needs nothing more. Any
// other, every interface included, needs SUBSTRATE_INSECURE_ALLOW_CLEARTEXT,
// because the server never terminates TLS and the bind is the one point where
// it can tell that the socket is for more than this machine.
//
// `localhost` binds 127.0.0.1 itself rather than whatever the resolver
// answers for the name, so the loopback check and the socket cannot disagree.
func (c Config) ListenAddress() (string, error) {
	host := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(c.BindAddress), "["), "]")
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, c.Port)
	if loopbackIP(host) || c.InsecureAllowCleartext {
		return addr, nil
	}
	shown := strconv.Quote(host)
	if host == "" {
		shown += " (every interface)"
	}
	return "", fmt.Errorf("SUBSTRATE_BIND_ADDRESS is %s, so the server would listen on %s, which is not loopback, and it speaks plain HTTP: "+
		"passwords, TOTP codes and bearer tokens would reach the network unencrypted. Bind 127.0.0.1 and put a TLS terminator in front, "+
		"or, when only a TLS terminator or a loopback port mapping reaches this address, set SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true "+
		"(docs/operations.md, \"TLS and the reverse proxy\")", shown, addr)
}

// loopbackIP reports whether host is a loopback IP literal (127.0.0.0/8,
// ::1). A name is never loopback here, even one that resolves there today,
// because the answer must not depend on a resolver; ListenAddress spells
// `localhost` as 127.0.0.1 before it asks.
func loopbackIP(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ValidateCredentialKey holds SUBSTRATE_CREDENTIAL_KEY to key material: the
// standard-base64 of exactly 32 bytes the sealed store uses as its AES-256
// key. A passphrase is refused rather than stretched, because a single hash
// over one turns a dictionary word into the key that unwraps every
// repository's DEK from a stolen database, and the strength of the deployment
// stops being an operator promise the code cannot inspect
// ([0024](../../docs/decisions/0024-the-credential-key-is-key-material-not-a-passphrase.md)).
// The message carries the generator command rather than describing the shape
// in prose.
func ValidateCredentialKey(key string) error {
	if key == "" {
		return errors.New("SUBSTRATE_CREDENTIAL_KEY is unset: every repository's DEK wraps under this key, so a host without it would store its secrets under a plain-marked wrap. Set it to base64 of 32 bytes (generate one with: openssl rand -base64 32)")
	}
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(raw) != 32 {
		return errors.New("SUBSTRATE_CREDENTIAL_KEY must be key material, not a passphrase: base64 of exactly 32 bytes (generate one with: openssl rand -base64 32)")
	}
	return nil
}
