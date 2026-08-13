package commands

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/substrate"
)

// app carries everything a command needs: streams, config location, and the
// lazily built REST client. One per process invocation (one per test).
type app struct {
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	hc     *http.Client
	now    func() time.Time
	// stdin is the invocation's one buffered view of in, shared by every
	// prompt so a reader's lookahead cannot eat the next secret.
	stdin *bufio.Reader

	configPath  string
	contextName string
	serverFlag  string
	tokenFlag   string
	actorFlag   string
	// dsnFlag is the OPERATOR hat's database URL. It is a separate world from
	// the flags above: those address a substrate over HTTP with a token, this
	// one addresses the box's Postgres with no token at all.
	dsnFlag string

	version   string
	cfg       *Config
	cl        *client
	ctx       *Context
	typeCache []substrate.KindInfo
}

func newApp(version string) *app {
	return &app{
		in:      os.Stdin,
		out:     os.Stdout,
		errOut:  os.Stderr,
		now:     time.Now,
		version: version,
	}
}

func (a *app) config() (*Config, error) {
	if a.cfg != nil {
		return a.cfg, nil
	}
	if a.configPath == "" {
		p, err := configPath()
		if err != nil {
			return nil, err
		}
		a.configPath = p
	}
	cfg, err := loadConfig(a.configPath)
	if err != nil {
		return nil, err
	}
	a.cfg = cfg
	return cfg, nil
}

// saveConfig writes the config back to wherever it was read from, resolving
// the default location when nothing named one.
func (a *app) saveConfig(cfg *Config) error {
	if a.configPath == "" {
		p, err := configPath()
		if err != nil {
			return err
		}
		a.configPath = p
	}
	return saveConfig(a.configPath, cfg)
}

// resolveContext merges the config context with the environment and flag
// overrides. SUBSTRATE_SERVER / SUBSTRATE_TOKEN are the canonical variables and
// override the file; SS_* is the one accepted alias, read only when the
// canonical variable is unset. Flags override both.
//
// There is no repository here: a token implies its repository, so a client
// that holds one has nothing left to choose.
func (a *app) resolveContext() (Context, error) {
	if a.ctx != nil {
		return *a.ctx, nil
	}
	cfg, err := a.config()
	if err != nil {
		return Context{}, err
	}
	ctx, err := cfg.current(a.contextName)
	if err != nil && (!errors.Is(err, errNoContext) || a.contextName != "") {
		return Context{}, err
	}
	if ctx.Name == "" {
		ctx.Name = "default"
	}
	if v := firstEnv("SUBSTRATE_SERVER", "SS_SERVER"); v != "" {
		ctx.Server = v
	}
	if v := firstEnv("SUBSTRATE_TOKEN", "SS_TOKEN"); v != "" {
		ctx.Token = v
		// A token from the environment names no record substratectl can revoke; a
		// stale id from the file would be the WRONG one to delete.
		ctx.TokenID = ""
	}
	if a.serverFlag != "" {
		ctx.Server = a.serverFlag
	}
	if a.tokenFlag != "" {
		ctx.Token = a.tokenFlag
		ctx.TokenID = ""
	}
	a.ctx = &ctx
	return ctx, nil
}

// firstEnv returns the first non-empty environment variable among names.
func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

func (a *app) client() (*client, error) {
	if a.cl != nil {
		return a.cl, nil
	}
	ctx, err := a.resolveContext()
	if err != nil {
		return nil, err
	}
	if ctx.Server == "" {
		return nil, fmt.Errorf("no substrate server configured: run `substratectl login --server <url>` or set SUBSTRATE_SERVER")
	}
	cl := newClient(ctx.Server, ctx.Token, a.hc)
	cl.actor = a.actorFlag
	a.cl = cl
	return cl, nil
}

// defaultActor is what substratectl's writes are attributed to. An actor is
// ATTRIBUTION, not authorization: it names the door a
// write came through, and this is that door.
var defaultActor = string(substrate.ActorCLI)

// Execute runs the CLI and returns the process exit code.
func Execute(version string) int {
	a := newApp(version)
	root := a.rootCommand()
	if err := root.Execute(); err != nil {
		renderError(a.errOut, err)
		return 1
	}
	return 0
}

func (a *app) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "substratectl",
		Short: "Talk to a substrate",
		Long: `substratectl is the command line client for a substrate, and it wears two hats.

AS A USER it holds a token and talks HTTP. Everything in a repository is a
record of a declared kind, addressed by its kind reference — {authority}/{plural}
for a published kind, {plural} alone for a repository-local one — and the CLI is
a thin door onto that surface: types, get, apply, patch, delete, link, unlink,
watch. The token implies the repository; there is nothing else to point at.

  substratectl register --server https://substrate.example.com
  substratectl login --username geoah
  substratectl kinds
  substratectl get tasks -l owner/pinned=true
  substratectl apply -f task.yaml
  substratectl token create --label laptop

AS AN OPERATOR it holds a database URL and talks to nobody. There is no admin
user and no privileged endpoint: ` + "`user reset`" + `, ` + "`repository inspect`" + ` and
` + "`repository rebuild`" + ` run on the box, straight through the engine.

  DATABASE_URL=… substratectl repository list
  DATABASE_URL=… substratectl user reset geoah`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(a.in)
	root.SetOut(a.out)
	root.SetErr(a.errOut)

	pf := root.PersistentFlags()
	pf.StringVar(&a.configPath, "config", a.configPath, "config file (default ~/.config/substratectl/config.yaml)")
	pf.StringVar(&a.contextName, "context", "", "context to use from the config")
	pf.StringVar(&a.serverFlag, "server", "", "substrate base URL (overrides the context)")
	pf.StringVar(&a.tokenFlag, "token", "", "bearer token (overrides the context)")
	pf.StringVar(&a.actorFlag, "actor", defaultActor, "write as this actor (X-Substrate-Actor)")
	pf.StringVar(&a.dsnFlag, "dsn", "", "operator commands only: the substrate's Postgres URL (default $DATABASE_URL)")

	root.AddCommand(
		// The door.
		a.registerCommand(),
		a.loginCommand(),
		a.logoutCommand(),
		a.tokenCommand(),
		a.recoveryCommand(),
		// The records.
		a.kindsCommand(),
		a.getCommand(),
		a.applyCommand(),
		a.patchCommand(),
		a.deleteCommand(),
		a.linkCommand(),
		a.unlinkCommand(),
		a.editCommand(),
		a.watchCommand(),
		a.triggerCommand(),
		a.functionCommand(),
		a.bundleCommand(),
		// The box.
		a.userCommand(),
		a.repositoryCommand(),
		a.versionCommand(),
	)
	return root
}

func (a *app) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the client version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(a.out, "substratectl %s (api v1)\n", a.version)
			return nil
		},
	}
}
