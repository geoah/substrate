package commands

// The signing pin. A repository's Ed25519 changelog-signing key is minted
// SERVER-SIDE at registration and its seed is sealed under the host credential
// key, where the only signer keeps it. The PUBLIC half comes back on the
// registration response, and it is printed here because a pin is only worth
// something read from outside the database it checks: `repository verify
// --expect-public-key <pin>` compares the store's own claim against this copy,
// and a store rewritten to claim a different key is what that catches.
//
// It is not a secret, so there is no ceremony around it: no 1Password item, no
// "shown once", nothing substratectl declines to write down.

import "fmt"

// printSigningPublicKey prints the pin registration handed back.
func (a *app) printSigningPublicKey(publicKey string) {
	if publicKey == "" {
		return
	}
	fmt.Fprintf(a.out, "  signing public key: %s\n", publicKey)
	fmt.Fprintln(a.out, "  Keep it somewhere outside this substrate. It is what")
	fmt.Fprintln(a.out, "  `repository verify --expect-public-key` holds the store to.")
}
