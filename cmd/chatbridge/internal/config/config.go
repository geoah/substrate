// Package config reads the bridge's configuration from the environment.
// Nothing in it names a deployment: the server comes from the store, which
// `chatbridge authorize` fills.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config is the bridge's configuration.
type Config struct {
	// DataDir is the bridge store: the bridge's database, one database per
	// network, and the media the outbox names. It holds the session keys
	// of every linked network, so it belongs on an encrypted disk.
	DataDir string
	// ListenAddr is where /healthz and /metrics are served.
	ListenAddr string
	// Name is the instance's name, as the console shows it.
	Name string
	// Networks are the networks this instance runs, by id; empty is all.
	Networks []string
	// DeviceName is the name a network shows in the owner's list of linked
	// devices.
	DeviceName string
	// Debug logs bridgev2 and the network libraries at debug level, which
	// includes message text.
	Debug bool
}

// The environment variables.
const (
	EnvData       = "CHATBRIDGE_DATA"
	EnvListen     = "CHATBRIDGE_LISTEN_ADDR"
	EnvName       = "CHATBRIDGE_NAME"
	EnvNetworks   = "CHATBRIDGE_NETWORKS"
	EnvDeviceName = "CHATBRIDGE_DEVICE_NAME"
	EnvDebug      = "CHATBRIDGE_DEBUG"
)

// FromEnv reads the configuration.
func FromEnv() (Config, error) {
	c := Config{
		DataDir:    env(EnvData, "data"),
		ListenAddr: env(EnvListen, ":8080"),
		Name:       os.Getenv(EnvName),
		DeviceName: env(EnvDeviceName, "Substrate"),
	}
	if c.Name == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "chatbridge"
		}
		c.Name = host
	}
	for _, n := range strings.Split(os.Getenv(EnvNetworks), ",") {
		if n = strings.TrimSpace(n); n != "" {
			c.Networks = append(c.Networks, n)
		}
	}
	switch v := strings.ToLower(os.Getenv(EnvDebug)); v {
	case "", "0", "false", "no":
	case "1", "true", "yes":
		c.Debug = true
	default:
		return c, fmt.Errorf("%s=%q is not a boolean", EnvDebug, v)
	}
	return c, nil
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
