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

// resetWriterHint says why a reset that must write the repository stops at a
// running server.
func resetWriterHint(err error) error {
	if !errors.Is(err, engine.ErrRepositoryHasAnotherWriter) {
		return err
	}
	return fmt.Errorf("%w\n(the repository's history took its person out, so the reset writes the repository a new person's actor: stop the server and run the reset again)", err)
}

func (a *app) userResetCommand() *cobra.Command {
	var (
		passwordStdin bool
		createAs      string
	)
	cmd := &cobra.Command{
		Use:   "reset <name>",
		Short: "Operator: give a person's sign-in new factors (direct database, no HTTP)",
		Long: `Reset a person who lost both factors.

This is the operator's door and it runs ON THE BOX: it writes the person's
sign-in straight through the engine, so nothing reachable from the network can
reset one. There is no self-serve recovery.

<name> is the name the person signs in with. A repository nobody can sign in
to yet (restored without its sign-in) is named by its name or id instead, and
gets a sign-in named after it, or --identity.

You choose the new password; the substrate issues the new TOTP enrollment and
prints it exactly once. Hand both over out of band — and tell the person to
change the password once they are back in.

The server may keep running: the reset writes the sign-in, which no
repository's history holds. The one exception is a repository whose history
took its person out: its new person gets an actor of their own, written into
the repository, and the reset asks for the server to be stopped first.

  DATABASE_URL=… substratectl user reset geoah.example.com`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			// The DSN and the credential key are checked BEFORE the password is
			// asked for: being told there is no database — or that the key is
			// missing and the write would land in plaintext — only after typing a
			// password twice is a small cruelty, and the answer is the same either
			// way. This is the write hat, so a missing key REFUSES here.
			if _, err := a.dsn(); err != nil {
				return err
			}
			svc, err := a.openEngineSignIn(cmd.Context())
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
			enrollment, err := svc.ResetUser(cmd.Context(), name, password, createAs)
			if errors.Is(err, engine.ErrResetNeedsTheWriter) {
				// Opening as the repository's writer is refused at the boot
				// while a server runs, so nothing is written beside it.
				_ = svc.Close()
				if svc, err = a.openEngineWrite(cmd.Context()); err != nil {
					return resetWriterHint(err)
				}
				enrollment, err = svc.ResetUser(cmd.Context(), name, password, createAs)
			}
			if err != nil {
				return lockHint(err)
			}
			fmt.Fprintf(a.out, "the sign-in of %s is reset\n", name)
			fmt.Fprintln(a.out, "  the password is the one you just typed; the old one no longer works")
			a.printEnrollment(enrollment.URI, enrollment.Secret)
			fmt.Fprintln(a.out, "  hand both over out of band; the person's tokens are untouched")
			return nil
		},
	}
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the new password from stdin (one line)")
	cmd.Flags().StringVar(&createAs, "identity", "", "for a repository nobody can sign in to yet: the name its new sign-in takes (default: the repository's name)")
	return cmd
}
