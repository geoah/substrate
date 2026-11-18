package pushwire

import "crypto/sha256"

// fingerprintPrefix separates an event id's fingerprint from any other
// SHA-256 over the same bytes.
const fingerprintPrefix = "substrate-push-event-fingerprint-v1\n"

// FingerprintSize is the decoded size of a fingerprint; Encode writes it in
// 22 characters.
const FingerprintSize = 16

// EventFingerprint names one alert by its event id: the first
// FingerprintSize bytes of SHA-256 over the prefix and the event id's
// characters, in base64url without padding. A `read` event lists the
// fingerprints of the alerts it takes back, and the app computes the same
// value from the event id in each delivered notification's payload, so it
// removes exactly those, even one whose extension never ran. No key is
// needed: an event id is already sealed under a random nonce, so its
// fingerprint is as opaque as it is and no two alerts share one.
func EventFingerprint(eventID string) string {
	sum := sha256.Sum256([]byte(fingerprintPrefix + eventID))
	return Encode(sum[:FingerprintSize])
}
