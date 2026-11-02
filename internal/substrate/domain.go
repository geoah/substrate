package substrate

import "time"

// DomainState is a repository's domain verification as its repository record
// holds it (decision 0208): the one-time proof a Verify domain request
// minted, while it is pending, and when the name was last verified.
type DomainState struct {
	Proof      string     `json:"proof,omitempty"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
}

// DomainProof is the POST /api/v1/repository/domain answer: the TXT record
// the owner adds to the repository name's DNS.
type DomainProof struct {
	// Name is the DNS name the record lives at: `_substrate.<repository>`.
	Name string `json:"name"`
	// Record is the TXT value: `v=substrate1 server=<origin> proof=<code>`.
	Record string `json:"record"`
	// Proof is the one-time code inside Record.
	Proof string `json:"proof"`
	// VerifiedAt is when the name was last verified, absent while it never
	// was.
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
}

// DomainCheck is the POST /api/v1/repository/domain/verify answer.
type DomainCheck struct {
	Verified bool `json:"verified"`
	// VerifiedAt is the stamp the check wrote, or the earlier one a failed
	// check leaves standing.
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
	// Reason says what the owner changes for a failed check to pass. Empty
	// on success.
	Reason string `json:"reason,omitempty"`
}
