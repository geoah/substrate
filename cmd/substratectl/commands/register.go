package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

// Registration is the ONE door into a fresh substrate: the invite code admits,
// and one transaction creates the user, their repository and the seed. It is
// two calls and one write — `/register/enroll` issues a TOTP seed and creates
// NOTHING, `/register` takes it back with one code and a password — so an
// abandoned registration leaves no row behind.
//
// Registration ENDS LOGGED IN: the commit mints the first token, and substratectl
// stores it exactly as `login` does.

func (a *app) registerCommand() *cobra.Command {
	var (
		server            string
		invite            string
		username          string
		secret            string
		code              string
		label             string
		contextName       string
		recoveryPublicKey string
		passwordStdin     bool
	)
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a user with an invite code and store the first token",
		Long: `Create a user and their repository on a substrate that is open for
registration.

Three things make a user: a username, a password, and a second factor. substratectl
asks the substrate for a TOTP enrollment, prints it once for an authenticator,
and takes back one code with the password — only that call writes anything.

  substratectl register --server https://substrate.example.com
  substratectl register --username geoah --invite-code CODE \
      --totp-secret BASE32SEED --totp-code 123456 --password-stdin < password

--totp-secret brings your own seed and skips the enrollment call, which is what
makes an unattended registration possible; without it the seed comes from the
substrate and the code is prompted for.

A substrate that verifies no second factor (SUBSTRATE_INSECURE_DISABLE_TOTP, a
local-development setting) is neither enrolled with nor asked for a code: a
username and a password make the user.`,
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
			if invite == "" {
				invite, err = a.secret(false, "Invite code: ")
				if err != nil {
					return err
				}
			}
			if invite == "" {
				return errors.New("an invite code is required: a substrate with none configured is closed to registration")
			}
			if username == "" {
				username, err = a.prompt("Username: ")
				if err != nil {
					return err
				}
			}
			if username == "" {
				return errors.New("a username is required")
			}
			password, err := a.newSecret(passwordStdin, "Password: ", "Password (again): ")
			if err != nil {
				return err
			}
			cl := newClient(server, "", a.hc)
			// A substrate that verifies no second factor is not asked for an
			// enrollment either: buying a seed nobody will hold, to prove it
			// with a code nothing checks, is ceremony. The commit sends no
			// secret, and the substrate mints the one it seals.
			//
			// Asked only when the answer changes what happens next: a caller
			// carrying both a seed and a code has already decided.
			totpRequired := true
			if secret == "" || code == "" {
				totpRequired = cl.totpRequired(cmd.Context())
			}
			if secret == "" && totpRequired {
				enrollment, err := cl.registerEnroll(cmd.Context(), registerBeginRequest{
					InviteCode: invite, Username: username,
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
			// The recovery pair is generated HERE, before the commit: only
			// the recipient rides the wire, and the identity is handed over
			// after registration lands (1Password when `op` is signed in,
			// printed once otherwise). --recovery-public-key brings your own
			// recipient and skips the handover entirely.
			recoveryIdentity := ""
			if recoveryPublicKey == "" {
				var rerr error
				if recoveryIdentity, recoveryPublicKey, rerr = newRecoveryIdentity(); rerr != nil {
					return rerr
				}
			}
			res, err := a.retryWhenPaced(cmd.Context(), func() (*registerResult, error) {
				return cl.register(cmd.Context(), registerRequest{
					InviteCode: invite, Username: username, Password: password,
					TOTPSecret: secret, TOTPCode: code, Label: label,
					RecoveryPublicKey: recoveryPublicKey,
				})
			})
			if err != nil {
				// A clean 4xx refusal created nothing. Anything else is
				// AMBIGUOUS: the server may have committed and the response
				// died, and a lost response must not take the only copy of a
				// key that can never be re-issued.
				var ae *apiError
				clean := errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500
				if recoveryIdentity != "" && !clean {
					fmt.Fprintln(a.errOut, "registration did not confirm; if it actually landed, the key below is the ONLY copy:")
					a.printRecoveryKey(recoveryIdentity, recoveryPublicKey)
				}
				return authError(err)
			}
			if contextName == "" {
				contextName = username
			}
			cfg.upsertContext(Context{
				Name: contextName, Server: server, Username: username,
				Token: res.Secret, TokenID: res.Token.ID,
			})
			if err := a.saveConfig(cfg); err != nil {
				// The registration LANDED: the one-time keys must not die
				// with a failed config write, so they are handed over before
				// the error surfaces.
				fmt.Fprintln(a.errOut, "registered, but the token could not be stored; keep the keys below:")
				a.handOverRecoveryKey(cmd.Context(), server, username, recoveryIdentity, res.RecoveryPublicKey)
				a.printSigningPublicKey(res.SigningPublicKey)
				return err
			}
			fmt.Fprintf(a.out, "registered %s on %s\n", username, server)
			fmt.Fprintf(a.out, "  token:   %s (%s)\n", dash(res.Token.Label), dash(res.Token.ID))
			fmt.Fprintf(a.out, "  context: %s -> %s\n", contextName, a.configPath)
			a.handOverRecoveryKey(cmd.Context(), server, username, recoveryIdentity, res.RecoveryPublicKey)
			a.printSigningPublicKey(res.SigningPublicKey)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&server, "server", "", "substrate base URL")
	f.StringVar(&invite, "invite-code", "", "invite code (prompted for when omitted)")
	f.StringVar(&username, "username", "", "username to claim (prompted for when omitted)")
	f.StringVar(&secret, "totp-secret", "", "base32 TOTP seed to enroll (default: ask the substrate for one)")
	f.StringVar(&code, "totp-code", "", "6-digit code from the new enrollment (prompted for when omitted)")
	f.StringVar(&label, "label", "", "label for the first token (default: substratectl@<hostname>)")
	f.StringVar(&contextName, "context", "", "name for the stored context (default: the username)")
	f.StringVar(&recoveryPublicKey, "recovery-public-key", "", "age recipient for the recovery key (default: generate the pair locally and hand you the key)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin (one line) instead of prompting")
	return cmd
}

// pacedWait bounds how long a retry will sit on a Retry-After. The door's
// spacing is seconds; anything longer is a lockout, and a lockout is news the
// caller must see rather than something to wait out silently.
const pacedWait = 15 * time.Second

// retryWhenPaced sends once more after a 429 that names a wait, and only
// then. It exists for ONE sequence — the enrollment and the commit behind it —
// where the second request is substratectl's own doing.
func (a *app) retryWhenPaced(ctx context.Context, send func() (*registerResult, error)) (*registerResult, error) {
	res, err := send()
	if err == nil {
		return res, nil
	}
	var ae *apiError
	if !errors.As(err, &ae) || ae.Status != http.StatusTooManyRequests {
		return nil, err
	}
	wait, perr := time.ParseDuration(ae.RetryAfter + "s")
	if perr != nil || wait <= 0 || wait > pacedWait {
		return nil, err
	}
	fmt.Fprintf(a.errOut, "the substrate paces this door; waiting %s and sending once more\n", wait)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(wait):
	}
	return send()
}
