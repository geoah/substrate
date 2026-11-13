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
		Short: "Log in with a repository name, password and, where the account has one, a TOTP code, and store the token",
		Long: `Log in to a substrate.

The repository is the name registration created — its authority
(ada.example.com), or a bare label the substrate completes under its own host.
The factors are presented directly: the password, then one code from the
authenticator holding this account, asked for only when the substrate answers
that the account has a second factor. An account registered without one, and
any account on a substrate that verifies no code, signs in with the password
alone. --totp-code sends the code up front. The login mints a token record and returns
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
			// A code in hand is validated before the request: every auth
			// attempt is paced, so a code that cannot be right must not spend
			// one.
			if code != "" {
				if code, err = normalizeCode(code); err != nil {
					return err
				}
			}
			if label == "" {
				label = defaultTokenLabel()
			}
			in := factors{Repository: repository, Password: password, TOTPCode: code, Label: label}
			res, err := cl.login(cmd.Context(), in)
			// The two-step sign-in (decision 0237): the substrate asks for a
			// code only once the password is right, and only of an account
			// that has a second factor. The second attempt follows the first
			// inside the door's interval, so it waits out the pacing.
			if code == "" && isCodeRequired(err) {
				if in.TOTPCode, err = a.askCode("", "TOTP code: "); err != nil {
					return err
				}
				res, err = retryWhenPaced(cmd.Context(), a, func() (*tokenResult, error) {
					return cl.login(cmd.Context(), in)
				})
			}
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
	f.StringVar(&code, "totp-code", "", "current 6-digit code (prompted for when omitted and the account has a second factor)")
	f.StringVar(&label, "label", "", "label for the minted token (default: substratectl@<hostname>)")
	f.StringVar(&contextName, "context", "", "name for the stored context (default: the repository name)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin (one line) instead of prompting")
	return cmd
}

func (a *app) logoutCommand() *cobra.Command {
	var (
		everywhere    bool
		code          string
		passwordStdin bool
	)
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Revoke the stored token and forget it, or sign out everywhere",
		Long: `Revoke the token in the current context and remove it from the config.

Revoking is deleting the token record: no row means no access. The context's
server and repository stay, so logging back in is one command.

--everywhere ends every token the person holds instead: every browser and
script signed in, every token minted from one, and every app's and
assistant's access, this one included. Use it when a token may have leaked.
It asks for the password, and a TOTP code where the account has a second
factor, because a token is no evidence for it.

  substratectl logout --everywhere
  substratectl logout --everywhere --totp-code 123456 --password-stdin < password`,
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
			if everywhere {
				return a.signOutEverywhere(cmd, cfg, ctx, code, passwordStdin)
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
	f := cmd.Flags()
	f.BoolVar(&everywhere, "everywhere", false, "end every token the person holds, this one included (asks for the password)")
	f.StringVar(&code, "totp-code", "", "with --everywhere: current 6-digit code (prompted for when omitted and the account has a second factor)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "with --everywhere: read the password from stdin (one line) instead of prompting")
	return cmd
}

// signOutEverywhere is `logout --everywhere`: both factors, the same
// two-step code prompt a login uses, then the context forgets its token,
// which the sign-out ended with the rest.
func (a *app) signOutEverywhere(cmd *cobra.Command, cfg *Config, ctx Context, code string, passwordStdin bool) error {
	repository, err := a.askRepository(ctx.Repository)
	if err != nil {
		return err
	}
	password, err := a.secret(passwordStdin, "Password: ")
	if err != nil {
		return err
	}
	if code != "" {
		if code, err = normalizeCode(code); err != nil {
			return err
		}
	}
	cl := newClient(ctx.Server, "", a.hc)
	in := factors{Repository: repository, Password: password, TOTPCode: code}
	ended, err := cl.signOutEverywhere(cmd.Context(), in)
	if code == "" && isCodeRequired(err) {
		if in.TOTPCode, err = a.askCode("", "TOTP code: "); err != nil {
			return err
		}
		ended, err = retryWhenPaced(cmd.Context(), a, func() (int, error) {
			return cl.signOutEverywhere(cmd.Context(), in)
		})
	}
	if err != nil {
		return authError(err)
	}
	if cfg.forgetToken(ctx.Name) {
		if err := a.saveConfig(cfg); err != nil {
			return err
		}
	}
	noun := "tokens"
	if ended == 1 {
		noun = "token"
	}
	fmt.Fprintf(a.out, "signed out everywhere on %s: %d %s ended\n", ctx.Server, ended, noun)
	return nil
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

// isCodeRequired reports the door's one other 401: the password was right and
// the account has a second factor, so the same login with a code succeeds.
func isCodeRequired(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == http.StatusUnauthorized && ae.Code == "totp_required"
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
