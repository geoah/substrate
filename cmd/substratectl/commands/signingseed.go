package commands

// The signing seed handoff. The repository's Ed25519 changelog-signing seed
// is minted SERVER-SIDE at registration, sealed under the host credential
// key, and disclosed exactly once in the registration response. The server
// remains the only signer; this copy is the user's safekeeping — with it they
// can verify the changelog's signatures with no server involved, and it is
// the only copy that survives a lost host credential key. Like the recovery
// key it goes to 1Password when the `op` CLI is present and signed in, and is
// printed exactly once otherwise; substratectl never writes it to disk.

import (
	"context"
	"fmt"
)

// printSigningSeed is the fallback ceremony: shown once, kept by the reader.
func (a *app) printSigningSeed(seed, publicKey string) {
	fmt.Fprintln(a.out, "  signing key (shown ONCE; the server keeps only a sealed copy):")
	fmt.Fprintf(a.out, "    %s\n", seed)
	fmt.Fprintf(a.out, "  public key: %s\n", publicKey)
	fmt.Fprintln(a.out, "  Keep it beside the recovery key. It is the Ed25519 seed your repository")
	fmt.Fprintln(a.out, "  signs its changelog with: with it you can verify the signatures without")
	fmt.Fprintln(a.out, "  the server, and it is the only copy the server cannot lose for you. The")
	fmt.Fprintln(a.out, "  server remains the only signer; it is never asked for again.")
}

// handOverSigningSeed runs the handoff: 1Password when possible, the printed
// ceremony otherwise. A substrate running unsigned (the local-testing
// insecure switch) returns no seed, and there is nothing to hand over.
func (a *app) handOverSigningSeed(ctx context.Context, server, username, seed, publicKey string) {
	if seed == "" {
		return
	}
	if a.saveItemTo1Password(ctx, "signing key", opItem{
		Title:    fmt.Sprintf("substrate signing key (%s @ %s)", username, server),
		Category: "PASSWORD",
		Fields: []opField{
			{ID: "password", Type: "CONCEALED", Purpose: "PASSWORD", Value: seed},
			{Label: "public key", Type: "STRING", Value: publicKey},
			{Label: "server", Type: "STRING", Value: server},
			{Label: "username", Type: "STRING", Value: username},
		},
	}) {
		return
	}
	a.printSigningSeed(seed, publicKey)
}
