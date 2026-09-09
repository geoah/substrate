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

// saveItemTo1Password tries an automatic save: present `op`, signed in, item
// created. False means the caller prints the secret instead; the reason lands
// on stderr so a signed-out `op` is diagnosable. `what` names the secret in
// both outcomes.
func (a *app) saveItemTo1Password(ctx context.Context, what string, item opItem) bool {
	opPath, err := exec.LookPath("op")
	if err != nil {
		return false
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
		fmt.Fprintf(a.errOut, "1password: could not save the %s (%v): %s\n",
			what, err, bytes.TrimSpace(stderr.Bytes()))
		return false
	}
	fmt.Fprintf(a.out, "  %s: saved to 1Password as %q\n", what, item.Title)
	return true
}

// saveRecoveryTo1Password is the recovery key's automatic save.
func (a *app) saveRecoveryTo1Password(ctx context.Context, server, repository, identity, recipient string) bool {
	return a.saveItemTo1Password(ctx, "recovery key", opItem{
		Title:    fmt.Sprintf("substrate recovery key (%s @ %s)", repository, server),
		Category: "PASSWORD",
		Fields: []opField{
			{ID: "password", Type: "CONCEALED", Purpose: "PASSWORD", Value: identity},
			{Label: "recipient", Type: "STRING", Value: recipient},
			{Label: "server", Type: "STRING", Value: server},
			{Label: "repository", Type: "STRING", Value: repository},
		},
	})
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
func (a *app) handOverRecoveryKey(ctx context.Context, server, repository, identity, recipient string) {
	if identity == "" {
		fmt.Fprintf(a.out, "  recovery recipient enrolled: %s\n", recipient)
		return
	}
	if a.saveRecoveryTo1Password(ctx, server, repository, identity, recipient) {
		return
	}
	a.printRecoveryKey(identity, recipient)
}
