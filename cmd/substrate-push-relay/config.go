package main

import (
	"crypto/ecdsa"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/kelseyhightower/envconfig"

	"github.com/geoah/substrate/internal/apns"
	"github.com/geoah/substrate/internal/peer"
	"github.com/geoah/substrate/internal/pushrelay"
	"github.com/geoah/substrate/internal/pushwire"
)

// env is the relay's configuration, from the environment alone
// (docs/operations.md, "Running the push relay"). The Apple keys are files,
// so their bytes never sit in an environment a process listing shows.
type env struct {
	TeamID string `envconfig:"SUBSTRATE_PUSH_RELAY_APNS_TEAM_ID"`

	ProductionKeyFile  string `envconfig:"SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_FILE"`
	ProductionKeyID    string `envconfig:"SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_ID"`
	DevelopmentKeyFile string `envconfig:"SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_FILE"`
	DevelopmentKeyID   string `envconfig:"SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_ID"`

	Topics string `envconfig:"SUBSTRATE_PUSH_RELAY_APNS_TOPICS"`

	SealKey         string `envconfig:"SUBSTRATE_PUSH_RELAY_SEAL_KEY"`
	SealKeyPrevious string `envconfig:"SUBSTRATE_PUSH_RELAY_SEAL_KEY_PREVIOUS"`

	MetricsAddress string `envconfig:"SUBSTRATE_PUSH_RELAY_METRICS_ADDRESS"`

	LimitInstall       int `envconfig:"SUBSTRATE_PUSH_RELAY_LIMIT_INSTALL" default:"20"`
	LimitCaller        int `envconfig:"SUBSTRATE_PUSH_RELAY_LIMIT_CALLER" default:"600"`
	LimitRegister      int `envconfig:"SUBSTRATE_PUSH_RELAY_LIMIT_REGISTER" default:"30"`
	LimitTotal         int `envconfig:"SUBSTRATE_PUSH_RELAY_LIMIT_TOTAL" default:"12000"`
	LimitRegisterTotal int `envconfig:"SUBSTRATE_PUSH_RELAY_LIMIT_REGISTER_TOTAL" default:"3000"`
	LimitRefusedTotal  int `envconfig:"SUBSTRATE_PUSH_RELAY_LIMIT_REFUSED_TOTAL" default:"3000"`

	Port                   string `envconfig:"PORT" default:"8080"`
	BindAddress            string `envconfig:"SUBSTRATE_BIND_ADDRESS" default:"127.0.0.1"`
	InsecureAllowCleartext bool   `envconfig:"SUBSTRATE_INSECURE_ALLOW_CLEARTEXT" default:"false"`
	TrustedProxies         string `envconfig:"SUBSTRATE_TRUSTED_PROXIES" default:""`

	LogLevel  string `envconfig:"LOG_LEVEL" default:"info"`
	LogFormat string `envconfig:"LOG_FORMAT" default:"text"`
}

// appleKey is one environment's key, parsed.
type appleKey struct {
	environment string
	id          string
	key         *ecdsa.PrivateKey
}

// config is env read and checked: everything the relay and `check` start
// from.
type config struct {
	teamID         string
	keys           []appleKey // production first
	topics         []string
	sealKey        []byte
	sealPrevious   []byte
	metricsAddress string
	limits         pushrelay.Limits
	listen         string
	trusted        []netip.Prefix
	logLevel       slog.Level
	logJSON        bool
}

// loadConfig reads the environment and refuses, naming the variable, what
// the relay cannot start with: no team, no environment with both a key file
// and its id, half of one, a key that is not a P-256 .p8, no topic, a topic
// that is no bundle id, no seal key or one that is not 32 bytes, a limit
// under 1, or a bind beyond loopback without SUBSTRATE_INSECURE_ALLOW_CLEARTEXT.
func loadConfig() (config, error) {
	var e env
	if err := envconfig.Process("", &e); err != nil {
		return config{}, err
	}
	var c config
	var err error
	if c.teamID = strings.TrimSpace(e.TeamID); !appleID(c.teamID) {
		return config{}, errors.New("SUBSTRATE_PUSH_RELAY_APNS_TEAM_ID is not set to the team id the Apple keys were issued under")
	}
	for _, k := range []struct{ environment, fileVar, file, idVar, id string }{
		{pushwire.EnvironmentProduction, "SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_FILE", e.ProductionKeyFile, "SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_ID", e.ProductionKeyID},
		{pushwire.EnvironmentDevelopment, "SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_FILE", e.DevelopmentKeyFile, "SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_ID", e.DevelopmentKeyID},
	} {
		file, id := strings.TrimSpace(k.file), strings.TrimSpace(k.id)
		if file == "" && id == "" {
			continue
		}
		if file == "" || !appleID(id) {
			return config{}, fmt.Errorf("%s and %s go together: the key file and the key id Apple gave it", k.fileVar, k.idVar)
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return config{}, fmt.Errorf("%s: %w", k.fileVar, err)
		}
		key, err := apns.ParseKey(b)
		if err != nil {
			return config{}, fmt.Errorf("%s: %w", k.fileVar, err)
		}
		c.keys = append(c.keys, appleKey{environment: k.environment, id: id, key: key})
	}
	if len(c.keys) == 0 {
		return config{}, errors.New("no Apple key: set SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_FILE and _KEY_ID, SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_FILE and _KEY_ID, or both")
	}
	for _, t := range strings.Split(e.Topics, ",") {
		if t = strings.TrimSpace(t); t == "" {
			continue
		}
		if !pushwire.ValidTopic(t) {
			return config{}, fmt.Errorf("SUBSTRATE_PUSH_RELAY_APNS_TOPICS: %q is not a bundle id", t)
		}
		c.topics = append(c.topics, t)
	}
	if len(c.topics) == 0 {
		return config{}, errors.New("SUBSTRATE_PUSH_RELAY_APNS_TOPICS is not set to the bundle ids the relay pushes to, comma separated")
	}
	if c.sealKey, err = sealKey("SUBSTRATE_PUSH_RELAY_SEAL_KEY", e.SealKey); err != nil {
		return config{}, err
	}
	if c.sealKey == nil {
		return config{}, errors.New("SUBSTRATE_PUSH_RELAY_SEAL_KEY is not set: it is the base64 of 32 random bytes (openssl rand -base64 32)")
	}
	if c.sealPrevious, err = sealKey("SUBSTRATE_PUSH_RELAY_SEAL_KEY_PREVIOUS", e.SealKeyPrevious); err != nil {
		return config{}, err
	}
	c.limits = pushrelay.Limits{
		Install: e.LimitInstall, Caller: e.LimitCaller, Register: e.LimitRegister,
		Total: e.LimitTotal, RegisterTotal: e.LimitRegisterTotal, RefusedTotal: e.LimitRefusedTotal,
	}
	for name, v := range map[string]int{
		"INSTALL": e.LimitInstall, "CALLER": e.LimitCaller, "REGISTER": e.LimitRegister,
		"TOTAL": e.LimitTotal, "REGISTER_TOTAL": e.LimitRegisterTotal, "REFUSED_TOTAL": e.LimitRefusedTotal,
	} {
		if v < 1 {
			return config{}, fmt.Errorf("SUBSTRATE_PUSH_RELAY_LIMIT_%s is %d: a limit is at least 1 a minute", name, v)
		}
	}
	if c.listen, err = listenAddress(e.BindAddress, e.Port, e.InsecureAllowCleartext); err != nil {
		return config{}, err
	}
	c.metricsAddress = strings.TrimSpace(e.MetricsAddress)
	if c.metricsAddress != "" {
		if _, _, err := net.SplitHostPort(c.metricsAddress); err != nil {
			return config{}, fmt.Errorf("SUBSTRATE_PUSH_RELAY_METRICS_ADDRESS is %q: it is host:port, such as :9090", c.metricsAddress)
		}
	}
	if c.trusted, err = peer.ParseTrusted(e.TrustedProxies); err != nil {
		return config{}, err
	}
	if c.logLevel, err = logLevel(e.LogLevel); err != nil {
		return config{}, err
	}
	switch strings.ToLower(e.LogFormat) {
	case "", "text":
	case "json":
		c.logJSON = true
	default:
		return config{}, fmt.Errorf("LOG_FORMAT is %q: it is text or json (text is the default)", e.LogFormat)
	}
	return c, nil
}

// appleID holds a team id or a key id to Apple's alphabet, so neither can
// break a provider token's header or a log line.
func appleID(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for i := range len(s) {
		if (s[i] < '0' || s[i] > '9') && (s[i] < 'A' || s[i] > 'Z') && (s[i] < 'a' || s[i] > 'z') {
			return false
		}
	}
	return true
}

// sealKey reads a seal key: standard base64 of exactly 32 bytes, nil when
// unset. A passphrase is refused rather than stretched.
func sealKey(name, raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(b) != pushrelay.SealKeySize {
		return nil, fmt.Errorf("%s is not the base64 of %d bytes (openssl rand -base64 32)", name, pushrelay.SealKeySize)
	}
	return b, nil
}

// listenAddress is the server's rule (internal/config's ListenAddress): a
// loopback address needs nothing more, any other needs
// SUBSTRATE_INSECURE_ALLOW_CLEARTEXT, because the relay speaks plain HTTP
// and a TLS terminator belongs in front of it.
func listenAddress(bind, port string, allowCleartext bool) (string, error) {
	host := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(bind), "["), "]")
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	if p, err := strconv.ParseUint(port, 10, 16); err != nil || p == 0 {
		return "", fmt.Errorf("PORT is %q: it is a port from 1 to 65535", port)
	}
	addr := net.JoinHostPort(host, port)
	if ip := net.ParseIP(host); (ip != nil && ip.IsLoopback()) || allowCleartext {
		return addr, nil
	}
	shown := strconv.Quote(host)
	if host == "" {
		shown += " (every interface)"
	}
	return "", fmt.Errorf("SUBSTRATE_BIND_ADDRESS is %s, so the relay would listen on %s, which is not loopback, and it speaks plain HTTP. "+
		"Bind 127.0.0.1 and put a TLS terminator in front, or, when only a TLS terminator or a loopback port mapping reaches this address, "+
		"set SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true", shown, addr)
}

func logLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return slog.LevelInfo, fmt.Errorf("LOG_LEVEL is %q: it is debug, info, warn or error (info is the default)", s)
}
