package commands

// The recovery key ceremony. The age identity is generated HERE, client-side,
// and only the recipient ever rides the wire: the substrate stores the
// repository's data-encryption key wrapped to the recipient (the recoverykey
// record), and the identity in the user's hands is what opens a backup with
// no server and no host key. The identity is handed to 1Password when the
// `op` CLI is present and signed in, and printed exactly once otherwise;
// substratectl never writes it to disk.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"filippo.io/age"
	"github.com/spf13/cobra"
)

// newRecoveryIdentity mints the age pair client-side.
func newRecoveryIdentity() (identity, recipient string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", err
	}
	return id.String(), id.Recipient().String(), nil
}

// opItem is the 1Password item shape `op item create` accepts on stdin: the
// identity travels through the pipe, never through an argument a process
// list could read.
type opItem struct {
	Title    string    `json:"title"`
	Category string    `json:"category"`
	Fields   []opField `json:"fields"`
}

type opField struct {
	ID      string `json:"id,omitempty"`
	Label   string `json:"label,omitempty"`
	Type    string `json:"type"`
	Purpose string `json:"purpose,omitempty"`
	Value   string `json:"value"`
}

// saveRecoveryTo1Password tries the automatic save: present `op`, signed in,
// item created. False means the caller prints the identity instead; the
// reason lands on stderr so a signed-out `op` is diagnosable.
func (a *app) saveRecoveryTo1Password(ctx context.Context, server, username, identity, recipient string) bool {
	opPath, err := exec.LookPath("op")
	if err != nil {
		return false
	}
	item := opItem{
		Title:    fmt.Sprintf("substrate recovery key (%s @ %s)", username, server),
		Category: "PASSWORD",
		Fields: []opField{
			{ID: "password", Type: "CONCEALED", Purpose: "PASSWORD", Value: identity},
			{Label: "recipient", Type: "STRING", Value: recipient},
			{Label: "server", Type: "STRING", Value: server},
			{Label: "username", Type: "STRING", Value: username},
		},
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return false
	}
	cmd := exec.CommandContext(ctx, opPath, "item", "create", "-")
	cmd.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(a.errOut, "1password: could not save the recovery key (%v): %s\n",
			err, bytes.TrimSpace(stderr.Bytes()))
		return false
	}
	fmt.Fprintf(a.out, "  recovery key: saved to 1Password as %q\n", item.Title)
	return true
}

// printRecoveryKey is the fallback ceremony: shown once, kept by the reader.
func (a *app) printRecoveryKey(identity, recipient string) {
	fmt.Fprintln(a.out, "  recovery key (shown ONCE, never stored by the substrate):")
	fmt.Fprintf(a.out, "    %s\n", identity)
	fmt.Fprintf(a.out, "  recipient: %s\n", recipient)
	fmt.Fprintln(a.out, "  Keep the recovery key safe (a password manager). With it, a backup of")
	fmt.Fprintln(a.out, "  your repository is recoverable on any substrate; without it, only this")
	fmt.Fprintln(a.out, "  server's credential key can read your secrets.")
}

// handOverRecoveryKey runs the whole handoff: 1Password when possible, the
// printed ceremony otherwise.
func (a *app) handOverRecoveryKey(ctx context.Context, server, username, identity, recipient string) {
	if identity == "" {
		fmt.Fprintf(a.out, "  recovery recipient enrolled: %s\n", recipient)
		return
	}
	if a.saveRecoveryTo1Password(ctx, server, username, identity, recipient) {
		return
	}
	a.printRecoveryKey(identity, recipient)
}

// recoveryCommand is the user's hat: enroll a recovery key on a repository
// that predates them. Registration is the ordinary door.
func (a *app) recoveryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recovery",
		Short: "The repository's recovery key",
	}
	cmd.AddCommand(a.recoveryEnrollCommand())
	return cmd
}

func (a *app) recoveryEnrollCommand() *cobra.Command {
	var (
		username      string
		code          string
		passwordStdin bool
	)
	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Enroll a recovery key on a repository that predates them (one-time)",
		Long: `Generate the recovery key locally, enroll its public half, and keep the key.

The substrate stores your repository's data-encryption key wrapped to the
age recipient, re-keys every stored secret under that data-encryption key in
the same transaction, and the recovery key stays with you (1Password when
the op CLI is signed in, printed once otherwise): a backup plus the key is a
complete recovery with no server and no host key.

Both current factors go in the request body, exactly like a password change:
a bearer token is not evidence to claim the one recovery slot. One-time: a
repository holds one recovery key, and rotation is not yet supported. New
repositories enroll at registration; this command exists for the ones that
predate recovery keys.`,
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
			cctx, err := a.resolveContext()
			if err != nil {
				return err
			}
			identity, recipient, err := newRecoveryIdentity()
			if err != nil {
				return err
			}
			// The handoff runs BEFORE the commit: a response lost after the
			// server enrolled would otherwise take the only copy of a key
			// that can never be re-issued with it.
			fmt.Fprintln(a.out, "Keep this before enrolling; the substrate never stores it:")
			a.handOverRecoveryKey(cmd.Context(), cctx.Server, username, identity, recipient)
			res, err := cl.recoveryEnroll(cmd.Context(), recoveryEnrollRequest{
				Username: username, Password: password, TOTPCode: code,
				RecoveryPublicKey: recipient,
			})
			if err != nil {
				return authError(err)
			}
			fmt.Fprintf(a.out, "recovery key enrolled on %s (recipient %s)\n", cctx.Server, res.RecoveryPublicKey)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&username, "username", "", "username (defaults to the context's)")
	f.StringVar(&code, "totp-code", "", "current 6-digit code (prompted for when omitted)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the current password from stdin (one line)")
	return cmd
}
