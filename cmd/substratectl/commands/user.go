package commands

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/engine"
)

// A user is a repository, a password and a TOTP secret. Changing either factor
// is the console's account page, which presents both current factors in the
// request body because `POST /password` and `POST /totp` refuse a bearer token
// as evidence. `reset` is the operator's door on the box, and nothing
// reachable from the network reaches it.

func (a *app) userCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "user",
		Short:   "Operator: reset a user's factors on the box (direct database, no HTTP)",
		Aliases: []string{"users"},
	}
	cmd.AddCommand(a.userResetCommand())
	return cmd
}

func (a *app) userResetCommand() *cobra.Command {
	var passwordStdin bool
	cmd := &cobra.Command{
		Use:   "reset <repository>",
		Short: "Operator: give a repository's user new factors (direct database, no HTTP)",
		Long: `Reset a user who lost both factors.

This is the operator's door and it runs ON THE BOX: it writes new sealed
material and a new credential record straight through the engine, so nothing
reachable from the network can reset an account. There is no self-serve
recovery.

You choose the new password; the substrate issues the new TOTP enrollment and
prints it exactly once. Hand both over out of band — and tell the user to
change the password once they are back in.

Stop the server first: the reset appends to the repository's changelog, so it
opens the repository as its writer, and a running server holds that lock.

  DATABASE_URL=… substratectl user reset geoah.example.com`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repository := args[0]
			// The DSN and the credential key are checked BEFORE the password is
			// asked for: being told there is no database — or that the key is
			// missing and the write would land in plaintext — only after typing a
			// password twice is a small cruelty, and the answer is the same either
			// way. This is the write hat, so a missing key REFUSES here.
			if _, err := a.dsn(); err != nil {
				return err
			}
			svc, err := a.openEngineWrite(cmd.Context())
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
			r, ok := svc.(engine.Resetter)
			if !ok {
				return seamMissing("ResetUser")
			}
			enrollment, err := r.ResetUser(cmd.Context(), repository, password)
			if err != nil {
				return lockHint(err)
			}
			fmt.Fprintf(a.out, "the user of %s is reset\n", repository)
			fmt.Fprintln(a.out, "  the password is the one you just typed; the old one no longer works")
			a.printEnrollment(enrollment.URI, enrollment.Secret)
			fmt.Fprintln(a.out, "  hand both over out of band; the user's tokens are untouched")
			return nil
		},
	}
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the new password from stdin (one line)")
	return cmd
}
