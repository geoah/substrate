package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Context is one named substrate endpoint plus the token minted for it.
//
// There is no repository here and none in any URL: a token IMPLIES its
// repository, so the whole of a stored credential is the
// server, the user who logged in and the secret. TokenID is the token
// record's id, kept so `substratectl logout` can revoke the very token it forgets.
// Authority is the repository's public name, the one a webhook URL and a
// delivery envelope carry; `register` records it and `login` carries the
// stored one forward.
type Context struct {
	Name      string `yaml:"name"`
	Server    string `yaml:"server"`
	Username  string `yaml:"username,omitempty"`
	Authority string `yaml:"authority,omitempty"`
	Token     string `yaml:"token,omitempty"`
	TokenID   string `yaml:"tokenId,omitempty"`
}

// Config is the on-disk CLI configuration. The file holds bearer secrets and
// is always written 0600 inside a 0700 directory.
type Config struct {
	CurrentContext string    `yaml:"currentContext,omitempty"`
	Contexts       []Context `yaml:"contexts"`
}

// A bare CLI talks to a substrate on this box; any other deployment is named
// with --server (and remembered in the context).
const defaultServer = "http://localhost:8080"

// configPath resolves the config file location: SUBSTRATECTL_CONFIG wins, then
// XDG_CONFIG_HOME, then ~/.config/substratectl/config.yaml.
func configPath() (string, error) {
	if p := os.Getenv("SUBSTRATECTL_CONFIG"); p != "" {
		return p, nil
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "substratectl", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".config", "substratectl", "config.yaml"), nil
}

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &c, nil
}

func saveConfig(path string, c *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return fmt.Errorf("chmod config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// upsertContext replaces the context with the same name, or appends it, and
// makes it current.
func (c *Config) upsertContext(ctx Context) {
	for i := range c.Contexts {
		if c.Contexts[i].Name == ctx.Name {
			c.Contexts[i] = ctx
			c.CurrentContext = ctx.Name
			return
		}
	}
	c.Contexts = append(c.Contexts, ctx)
	c.CurrentContext = ctx.Name
}

// forgetToken drops the stored secret from a context, keeping the server and
// the username so the next `substratectl login` is one command. It reports whether
// there was a context to forget.
func (c *Config) forgetToken(name string) bool {
	for i := range c.Contexts {
		if c.Contexts[i].Name == name {
			c.Contexts[i].Token = ""
			c.Contexts[i].TokenID = ""
			return true
		}
	}
	return false
}

func (c *Config) context(name string) (Context, bool) {
	for _, ctx := range c.Contexts {
		if ctx.Name == name {
			return ctx, true
		}
	}
	return Context{}, false
}

// current resolves the effective context: the named one, else the current one,
// else the only one. Environment overrides are applied by the caller.
func (c *Config) current(name string) (Context, error) {
	switch {
	case name != "":
		ctx, ok := c.context(name)
		if !ok {
			return Context{}, fmt.Errorf("no context named %q in the config", name)
		}
		return ctx, nil
	case c.CurrentContext != "":
		ctx, ok := c.context(c.CurrentContext)
		if !ok {
			return Context{}, fmt.Errorf("current context %q is not in the config", c.CurrentContext)
		}
		return ctx, nil
	case len(c.Contexts) == 1:
		return c.Contexts[0], nil
	}
	return Context{}, errNoContext
}

var errNoContext = errors.New("no substrate context configured")
