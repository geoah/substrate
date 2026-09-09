package commands

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

// THE USER HAT. `login`, `logout` and `register` (register.go) talk to the
// substrate over HTTP like any other client — there is no privileged door
// here. A login MINTS A TOKEN RECORD and hands back its secret once (ruling
// RB-5): sessions ARE token records, so what substratectl stores is an ordinary
// token, `substratectl token list` shows it beside every other, and `substratectl logout`
// revokes it by deleting its record.

func (a *app) loginCommand() *cobra.Command {
	var (
		server        string
		repository    string
		code          string
		label         string
		contextName   string
		passwordStdin bool
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in with a repository name, password and TOTP code, and store the token",
		Long: `Log in to a substrate.

The repository is the name registration created — its authority
(ada.example.com), or a bare label the substrate completes under its own host.
Both factors are presented directly: the password and one code from the
authenticator holding this account — unless the substrate verifies no second
factor, in which case no code is asked for. The login mints a token record and returns
its secret exactly once; substratectl writes it to the config file (mode 0600) as the
current context. The token implies the repository — there is nothing else to
configure.

  substratectl login --server https://substrate.example.com --repository geoah
  substratectl login --repository geoah --totp-code 123456 --password-stdin < password`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			existing, _ := cfg.current(contextName)
			if server == "" {
				server = firstNonEmpty(a.serverFlag,
					firstEnv("SUBSTRATE_SERVER", "SS_SERVER"),
					existing.Server, defaultServer)
			}
			if repository == "" {
				repository = existing.Repository
			}
			repository, err = a.askRepository(repository)
			if err != nil {
				return err
			}
			password, err := a.secret(passwordStdin, "Password: ")
			if err != nil {
				return err
			}
			cl := newClient(server, "", a.hc)
			// Validated before the request: every auth attempt is rate limited
			// and a failure counts toward a lockout, so a code that cannot be
			// right must not spend one. The deployment says whether there is a
			// code to ask for at all.
			code, err = a.askCodeIfRequired(cmd.Context(), cl, code, "TOTP code: ")
			if err != nil {
				return err
			}
			if label == "" {
				label = defaultTokenLabel()
			}
			res, err := cl.login(cmd.Context(), factors{
				Repository: repository, Password: password, TOTPCode: code, Label: label,
			})
			if err != nil {
				return authError(err)
			}
			if contextName == "" {
				contextName = repository
			}
			// The door's own answer, not what was typed: a bare label reaches
			// it as one and comes back as the authority, which is what
			// `apply --as-mine` and a webhook URL need.
			cfg.upsertContext(Context{
				Name: contextName, Server: server,
				Repository: firstNonEmpty(res.Repository, repository),
				Token:      res.Secret, TokenID: res.Token.ID,
			})
			if err := a.saveConfig(cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "logged in to %s as %s\n", server, repository)
			fmt.Fprintf(a.out, "  token:   %s (%s)\n", dash(res.Token.Label), dash(res.Token.ID))
			fmt.Fprintf(a.out, "  context: %s -> %s\n", contextName, a.configPath)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&server, "server", "", "substrate base URL")
	f.StringVar(&repository, "repository", "", "repository to log in to: ada.example.com, or a bare label under the substrate's host (prompted for when omitted)")
	f.StringVar(&code, "totp-code", "", "current 6-digit code (prompted for when omitted)")
	f.StringVar(&label, "label", "", "label for the minted token (default: substratectl@<hostname>)")
	f.StringVar(&contextName, "context", "", "name for the stored context (default: the repository name)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin (one line) instead of prompting")
	return cmd
}

func (a *app) logoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Revoke the stored token and forget it",
		Long: `Revoke the token in the current context and remove it from the config.

Revoking is deleting the token record: no row means no access. The context's
server and repository stay, so logging back in is one command.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			ctx, err := a.resolveContext()
			if err != nil {
				return err
			}
			if ctx.Token == "" {
				return errors.New("no stored token: nothing to log out of")
			}
			switch {
			case ctx.TokenID == "":
				// A context stored by hand (or by SUBSTRATE_TOKEN) names no
				// record to delete. Forgetting it is still worth doing, and
				// saying which half happened is the honest report.
				fmt.Fprintln(a.errOut, "warning: this context carries no token id, so the token was forgotten but not revoked")
				fmt.Fprintln(a.errOut, "  list the repository's tokens with `substratectl token list` and revoke it by id")
			default:
				cl, err := a.client()
				if err != nil {
					return err
				}
				if err := cl.revokeToken(cmd.Context(), ctx.TokenID); err != nil {
					// A token already gone is the state we wanted; anything
					// else is a real failure and must not silently drop the
					// only copy of a live secret.
					var ae *apiError
					if !errors.As(err, &ae) || (ae.Status != http.StatusNotFound && ae.Status != http.StatusUnauthorized) {
						return err
					}
					fmt.Fprintf(a.errOut, "note: the token was already gone on the server (%s)\n", ae.Error())
				}
			}
			if !cfg.forgetToken(ctx.Name) {
				// The token came from --token or the environment, so there is
				// nothing on disk to forget — and unsetting a variable is the
				// caller's to do, not substratectl's.
				fmt.Fprintln(a.out, "the token came from the environment, not the config: unset SUBSTRATE_TOKEN to finish logging out")
				return nil
			}
			if err := a.saveConfig(cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "logged out of %s (context %s)\n", ctx.Server, ctx.Name)
			return nil
		},
	}
}

// defaultTokenLabel names the token this machine holds, so a token list reads
// as a list of places rather than of secrets.
func defaultTokenLabel() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return "substratectl@" + host
}

// authError replaces the door's deliberately uniform 401 with the reasons a
// person can act on. The substrate answers the same way for an unknown
// repository, a wrong password and a wrong code — on purpose — so the CLI must
// not invent a diagnosis it does not have.
func authError(err error) error {
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status != http.StatusUnauthorized {
		return err
	}
	ae.Message = "the substrate refused the repository, the password or the code"
	ae.Hint = fmt.Sprintf("check the password and the CURRENT %d-digit code; repeated failures lock the account out for a while", codeDigits)
	return ae
}
