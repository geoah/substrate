package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

// Registration is the ONE door into a fresh substrate: the invite code admits
// where one is configured, and one transaction creates the user, their
// repository and the seed. It is
// two calls and one write — `/register/enroll` issues a TOTP seed and creates
// NOTHING, `/register` takes it back with one code and a password — so an
// abandoned registration leaves no row behind.
//
// Registration ENDS LOGGED IN: the commit mints the first token, and substratectl
// stores it exactly as `login` does.

func (a *app) registerCommand() *cobra.Command {
	var (
		server        string
		invite        string
		repository    string
		secret        string
		code          string
		label         string
		contextName   string
		passwordStdin bool
		skipTOTP      bool
	)
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a repository and store the first token",
		Long: `Create a repository and its user.

Three things make a user: a repository name, a password, and a second factor.
substratectl asks the substrate for a TOTP enrollment, prints it once for an
authenticator, and takes back one code with the password — only that call
writes anything. A substrate with an invite code configured asks for it first;
a local one with none (the compose quick start, mise run dev) is not asked and
reads none.

  substratectl register --server https://substrate.example.com
  substratectl register --repository geoah --invite-code CODE \
      --totp-secret BASE32SEED --totp-code 123456 --password-stdin < password

--totp-secret brings your own seed and skips the enrollment call, which is what
makes an unattended registration possible; without it the seed comes from the
substrate and the code is prompted for.

--skip-totp registers without a second factor: no enrollment and no code, and
signing in asks for the password alone. The owner can turn one on later in the
console's Settings.

  substratectl register --repository geoah --skip-totp --password-stdin < password

A substrate that verifies no second factor (SUBSTRATE_INSECURE_DISABLE_TOTP, a
local-development setting) is neither enrolled with nor asked for a code: a
repository name and a password make the user.

THE REPOSITORY NAME IS ITS AUTHORITY: the home of every kind you declare, the
name you log in with, and permanent. Pass any hostname you control
(ada.example.com), or a bare label the substrate completes under its own host
(geoah -> geoah.substrate.example.com).`,
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
			cl := newClient(server, "", a.hc)
			// The door's shape is read AT MOST ONCE, and only when an answer
			// changes what happens next: a caller carrying an invite code, a
			// seed and a code has already decided, and a password that fails
			// its own confirmation spends no request. A substrate that reads
			// no invite code is not asked for one; one that verifies no
			// second factor is not asked for an enrollment either, because
			// buying a seed nobody will hold, to prove it with a code nothing
			// checks, is ceremony. The commit then sends no secret, and the
			// substrate mints the one it seals.
			var read *doorPolicy
			door := func() doorPolicy {
				if read == nil {
					d := cl.door(cmd.Context())
					read = &d
				}
				return *read
			}
			if invite == "" && door().inviteRequired {
				invite, err = a.secret(false, "Invite code: ")
				if err != nil {
					return err
				}
				if invite == "" {
					return errors.New("an invite code is required: this substrate admits nobody without the one its operator configured")
				}
			}
			if repository == "" {
				repository, err = a.prompt("Repository: ")
				if err != nil {
					return err
				}
			}
			if repository == "" {
				return errors.New("a repository name is required")
			}
			password, err := a.newSecret(passwordStdin, "Password: ", "Password (again): ")
			if err != nil {
				return err
			}
			totpRequired := !skipTOTP
			if totpRequired && (secret == "" || code == "") {
				totpRequired = door().totpRequired
			}
			if secret == "" && totpRequired {
				enrollment, err := cl.registerEnroll(cmd.Context(), registerBeginRequest{
					InviteCode: invite, Repository: repository,
				})
				if err != nil {
					return authError(err)
				}
				secret = enrollment.Secret
				a.printEnrollment(enrollment.URI, enrollment.Secret)
				fmt.Fprintln(a.out, "  nothing is stored until the code below is accepted")
				fmt.Fprintln(a.out)
			}
			if totpRequired || code != "" {
				if code, err = a.askCode(code, "TOTP code from the new enrollment: "); err != nil {
					return err
				}
			}
			if label == "" {
				label = defaultTokenLabel()
			}
			// The commit is PACED, and substratectl is what made it fast: the
			// enrollment above and this call are one gesture to a person, but
			// two requests to a door that allows one every few seconds. Rather
			// than throw away a seed the user has already typed into an
			// authenticator, wait exactly as long as the substrate asks and
			// send it once more.
			res, err := retryWhenPaced(cmd.Context(), a, func() (*registerResult, error) {
				return cl.register(cmd.Context(), registerRequest{
					InviteCode: invite, Repository: repository, Password: password,
					TOTPSecret: secret, TOTPCode: code, SkipTOTP: skipTOTP, Label: label,
				})
			})
			if err != nil {
				return authError(err)
			}
			if contextName == "" {
				contextName = repository
			}
			cfg.upsertContext(Context{
				Name: contextName, Server: server, Repository: res.Repository,
				Token: res.Secret, TokenID: res.Token.ID,
			})
			if err := a.saveConfig(cfg); err != nil {
				// The registration LANDED and another `register` of the name
				// is refused, so the way back, a login, is named first.
				fmt.Fprintf(a.errOut, "registered %s, but the token could not be stored; `substratectl login` mints another\n", dash(res.Repository))
				a.handOverOlderServersKey(res.RecoveryKey)
				return err
			}
			fmt.Fprintf(a.out, "registered %s on %s\n", dash(res.Repository), server)
			fmt.Fprintf(a.out, "  token:     %s (%s)\n", dash(res.Token.Label), dash(res.Token.ID))
			fmt.Fprintf(a.out, "  context:   %s -> %s\n", contextName, a.configPath)
			a.handOverOlderServersKey(res.RecoveryKey)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&server, "server", "", "substrate base URL")
	f.StringVar(&invite, "invite-code", "", "invite code (prompted for when omitted, where the substrate reads one)")
	f.StringVar(&repository, "repository", "", "repository to create, which becomes its authority: ada.example.com, or a bare label under the substrate's host (prompted for when omitted)")
	f.StringVar(&secret, "totp-secret", "", "base32 TOTP seed to enroll (default: ask the substrate for one)")
	f.StringVar(&code, "totp-code", "", "6-digit code from the new enrollment (prompted for when omitted)")
	f.StringVar(&label, "label", "", "label for the first token (default: substratectl@<hostname>)")
	f.StringVar(&contextName, "context", "", "name for the stored context (default: the repository name)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin (one line) instead of prompting")
	f.BoolVar(&skipTOTP, "skip-totp", false, "register without a second factor; it can be turned on later in Settings")
	cmd.MarkFlagsMutuallyExclusive("skip-totp", "totp-secret")
	cmd.MarkFlagsMutuallyExclusive("skip-totp", "totp-code")
	return cmd
}

// pacedWait bounds how long a retry will sit on a Retry-After. The door's
// spacing is seconds; anything longer is a lockout, and a lockout is news the
// caller must see rather than something to wait out silently.
const pacedWait = 15 * time.Second

// retryWhenPaced sends once more after a 429 that names a wait, and only
// then. It exists for the sequences where the second request is substratectl's
// own doing: the enrollment and the commit behind it, and a login and the one
// that carries the code it asked for.
func retryWhenPaced[T any](ctx context.Context, a *app, send func() (T, error)) (T, error) {
	var zero T
	res, err := send()
	if err == nil {
		return res, nil
	}
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status != http.StatusTooManyRequests {
		return zero, err
	}
	wait, perr := time.ParseDuration(ae.RetryAfter + "s")
	if perr != nil || wait <= 0 || wait > pacedWait {
		return zero, err
	}
	fmt.Fprintf(a.errOut, "the substrate paces this door; waiting %s and sending once more\n", wait)
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-time.After(wait):
	}
	return send()
}

// handOverOlderServersKey prints the recovery key an older server minted,
// once: nothing can ask for it again, and the registration has landed whatever
// happens to the rest of the command.
func (a *app) handOverOlderServersKey(key string) {
	if key == "" {
		return
	}
	fmt.Fprintln(a.out, "this server is older than this substratectl and made a recovery key for the repository;")
	fmt.Fprintln(a.out, "it is shown once and never again, so put it in your password manager now:")
	fmt.Fprintf(a.out, "  recovery key: %s\n", key)
}
