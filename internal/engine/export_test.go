package engine

import (
	"crypto/rand"
	"encoding/base64"
)

// TestCredentialKey is a conforming credential key: standard-base64 of 32
// random bytes, the shape Open now demands (ADR 0024). It is minted once per
// test binary, so every Open in the suite shares one key and a reopen of the
// same database matches. Generated at run time and never committed, because a
// key checked into the tree is a key everyone has.
var TestCredentialKey = mintTestCredentialKey()

// TestCredentialKeyBytes is the 32-byte AES-256 key TestCredentialKey decodes
// to, for tests that unwrap a DEK the way the engine does.
var TestCredentialKeyBytes = mustDecodeTestCredentialKey(TestCredentialKey)

func mintTestCredentialKey() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func mustDecodeTestCredentialKey(key string) []byte {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		panic(err)
	}
	return raw
}
