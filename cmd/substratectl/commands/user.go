package commands

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// A user is a username, a password and a TOTP secret. Two of these commands
// change a user's own factors over HTTP and one is the operator's door on the
// box; they sit together because they are the same subject, and each says
// which hat it wears.
//
// THE PASSWORD-FACTOR RULE is why `password` and `totp` prompt
// for the current password and code even when a perfectly good token is
// sitting in the config: a bearer token is REFUSED as evidence by these
// endpoints, so a leaked token's blast radius is the data, never the account.

func (a *app) userCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "user",
		Short:   "Change your own factors, or reset a user's on the box",
		Aliases: []string{"users"},
	}
	cmd.AddCommand(a.userPasswordCommand(), a.userTOTPCommand(), a.userResetCommand())
	return cmd
}

func (a *app) userPasswordCommand() *cobra.Command {
	var (
		username         string
		code             string
		passwordStdin    bool
		newPasswordStdin bool
	)
	cmd := &cobra.Command{
		Use:   "password",
		Short: "Change your password (both current factors required)",
		Long: `Change the password of a user.

The current password and one current code go in the request body: a bearer
token is not accepted here and never will be, so a stolen token cannot rotate
the account it stole.

  substratectl user password --username geoah
  substratectl user password --username geoah --totp-code 123456 \
      --password-stdin --new-password-stdin <<< $'current\nnew'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			username, err := a.askUsername(username)
			if err != nil {
				return err
			}
			password, err := a.secret(passwordStdin, "Current password: ")
			if err != nil {
				return err
			}
			cl, err := a.doorClient()
			if err != nil {
				return err
			}
			code, err := a.askCodeIfRequired(cmd.Context(), cl, code, "Current TOTP code: ")
			if err != nil {
				return err
			}
			newPassword, err := a.newSecret(newPasswordStdin, "New password: ", "New password (again): ")
			if err != nil {
				return err
			}
			if err := cl.changePassword(cmd.Context(), passwordRequest{
				factors:     factors{Username: username, Password: password, TOTPCode: code},
				NewPassword: newPassword,
			}); err != nil {
				return authError(err)
			}
			fmt.Fprintf(a.out, "password changed for %s\n", username)
			fmt.Fprintln(a.out, "  existing tokens keep working — revoke them with `substratectl token revoke <id>` if the old password leaked")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&username, "username", "", "username (defaults to the context's)")
	f.StringVar(&code, "totp-code", "", "current 6-digit code (prompted for when omitted)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the current password from stdin (one line)")
	f.BoolVar(&newPasswordStdin, "new-password-stdin", false, "read the new password from stdin (the next line)")
	return cmd
}

func (a *app) userTOTPCommand() *cobra.Command {
	var (
		username      string
		code          string
		newSecret     string
		newCode       string
		passwordStdin bool
	)
	cmd := &cobra.Command{
		Use:   "totp",
		Short: "Re-enroll the second factor (both current factors required)",
		Long: `Replace a user's TOTP secret.

The current password and code prove the account; a code from the NEW enrollment
proves it landed in an authenticator before the swap. The old secret stops
working the moment the swap commits.

  substratectl user totp --username geoah
  substratectl user totp --username geoah --totp-code 123456 --password-stdin \
      --new-totp-secret BASE32SEED --new-totp-code 654321 < password`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			username, err := a.askUsername(username)
			if err != nil {
				return err
			}
			password, err := a.secret(passwordStdin, "Current password: ")
			if err != nil {
				return err
			}
			cl, err := a.doorClient()
			if err != nil {
				return err
			}
			// The CURRENT code follows the deployment; the NEW one is asked for
			// regardless, because it is what proves the seed being installed
			// landed somewhere — the substrate verifies that either way.
			code, err := a.askCodeIfRequired(cmd.Context(), cl, code, "Current TOTP code: ")
			if err != nil {
				return err
			}
			current := factors{Username: username, Password: password, TOTPCode: code}
			if newSecret == "" {
				// The enrollment call writes NOTHING: an abandoned
				// re-enrollment cannot lock anyone out of their account.
				enrollment, err := cl.totpEnroll(cmd.Context(), current)
				if err != nil {
					return authError(err)
				}
				newSecret = enrollment.Secret
				a.printEnrollment(enrollment.URI, enrollment.Secret)
				fmt.Fprintln(a.out, "  the old secret keeps working until the code below is accepted")
				fmt.Fprintln(a.out)
			}
			newCode, err = a.askCode(newCode, "TOTP code from the new enrollment: ")
			if err != nil {
				return err
			}
			if err := cl.reenrollTOTP(cmd.Context(), totpRequest{
				factors: current, NewTOTPSecret: newSecret, NewTOTPCode: newCode,
			}); err != nil {
				return authError(err)
			}
			fmt.Fprintf(a.out, "second factor replaced for %s\n", username)
			fmt.Fprintln(a.out, "  the previous secret stopped working — delete its authenticator entry")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&username, "username", "", "username (defaults to the context's)")
	f.StringVar(&code, "totp-code", "", "current 6-digit code (prompted for when omitted)")
	f.StringVar(&newSecret, "new-totp-secret", "", "base32 seed to enroll (default: ask the substrate for one)")
	f.StringVar(&newCode, "new-totp-code", "", "6-digit code from the new enrollment (prompted for when omitted)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the current password from stdin (one line)")
	return cmd
}

func (a *app) userResetCommand() *cobra.Command {
	var passwordStdin bool
	var allowUnsealed bool
	cmd := &cobra.Command{
		Use:   "reset <username>",
		Short: "Operator: give a user new factors (direct database, no HTTP)",
		Long: `Reset a user who lost both factors.

This is the operator's door and it runs ON THE BOX: it writes new sealed
material and a new credential record straight through the engine, so nothing
reachable from the network can reset an account. There is no self-serve
recovery.

You choose the new password; the substrate issues the new TOTP enrollment and
prints it exactly once. Hand both over out of band — and tell the user to
change the password once they are back in.

  DATABASE_URL=… substratectl user reset geoah`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			// The DSN and the credential key are checked BEFORE the password is
			// asked for: being told there is no database — or that the key is
			// missing and the write would land in plaintext — only after typing a
			// password twice is a small cruelty, and the answer is the same either
			// way. This is the write hat, so a missing key REFUSES here.
			if _, err := a.dsn(); err != nil {
				return err
			}
			svc, err := a.openEngineWrite(cmd.Context(), allowUnsealed)
			if err != nil {
				return err
			}
			password, err := a.newSecret(passwordStdin, "New password: ", "New password (again): ")
			if err != nil {
				_ = svc.Close()
				return err
			}
			if password == "" {
				_ = svc.Close()
				return errors.New("a new password is required")
			}
			defer func() { _ = svc.Close() }()
			r, ok := svc.(resetter)
			if !ok {
				return seamMissing("ResetUser")
			}
			enrollment, err := r.ResetUser(cmd.Context(), username, password)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "user %s reset\n", username)
			fmt.Fprintln(a.out, "  the password is the one you just typed; the old one no longer works")
			a.printEnrollment(enrollment.URI, enrollment.Secret)
			fmt.Fprintln(a.out, "  hand both over out of band; the user's tokens are untouched")
			return nil
		},
	}
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the new password from stdin (one line)")
	cmd.Flags().BoolVar(&allowUnsealed, "allow-unsealed", false,
		"DEV ONLY: write the new sealed material in plaintext when no credential key is set (never in production)")
	return cmd
}

// doorClient builds a client for the endpoints that take no bearer token: the
// credential changes carry their evidence in the body, and a context with a
// stale token must not stop a user from fixing their password.
func (a *app) doorClient() (*client, error) {
	ctx, err := a.resolveContext()
	if err != nil {
		return nil, err
	}
	server := firstNonEmpty(ctx.Server, defaultServer)
	return newClient(server, "", a.hc), nil
}
