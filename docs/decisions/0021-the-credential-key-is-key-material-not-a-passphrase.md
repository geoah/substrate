---
status: proposed
date: 2026-08-17
decision-makers: George Antoniadis
---

# 0021. `SUBSTRATE_CREDENTIAL_KEY` is key material, not a passphrase

## Context and Problem Statement

`deriveCredentialKey` (`internal/engine/credentials.go:240`) is one unsalted
SHA-256 over whatever string the operator set, with no iterations and no length
floor, and `internal/config/config.go:41` validates nothing. That key unwraps
every repository's DEK, and the wrapped DEKs sit in the same database as the
sealed rows they open. An attacker with a dump and a dictionary tries a guess
for the cost of one SHA-256 and one AES-GCM open. Found by the sealed store
review ([#99](https://github.com/geoah/substrate/issues/99), section 1.1);
filed as [#221](https://github.com/geoah/substrate/issues/221).

## Considered Options

- Demand key material: base64 of exactly 32 bytes, refuse anything else.
- Stretch a passphrase properly with argon2id and a stored salt.
- Accept both: stretch what does not decode as 32 bytes, take what does.

## Decision Outcome

Chosen: demand base64 of exactly 32 bytes and refuse anything else at boot,
naming the command that generates one.

Stretching is the wrong shape here, not merely more work. The salt has to live
somewhere, and the only somewhere is the database the attacker already stole,
so argon2id buys a work factor and nothing else; the operator still picks the
entropy, and a bad passphrase behind a good KDF is a bad key. It also puts a
tunable cost parameter on the boot path of every process that opens a
repository, including every operator command.

Accepting both is worse than either. It makes the security of a deployment
depend on whether a string happened to base64-decode to 32 bytes, which is not
something an operator can reason about or a log line can usefully report.

The refusal is at config validation, before any repository opens, and its
message carries the generator command
(`openssl rand -base64 32`) rather than describing the requirement in prose.

### Consequences

- Good, because the key's strength stops being a matter of operator judgement:
  the only accepted input is 256 bits of entropy.
- Good, because it is checkable in one line at boot, with no work factor to
  tune and nothing to store.
- Good, because the failure is loud and early rather than a property nobody can
  observe from outside.
- Bad, because it is a breaking change for any deployment running a passphrase.
  The existing key still opens the existing wraps, so the migration is: set the
  old key, run `substratectl repository reseal` per repository under a new
  32-byte key, then drop the old one. Until reseal exists as a re-key (it
  re-keys sealed payloads under the DEK, not the DEK under a new host key), the
  break has no migration and this decision cannot ship.
- Bad, because it makes the shipped `compose.yaml` fail to start until the
  operator sets a key, which is the correct outcome and still a change in
  first-run experience
  ([#222](https://github.com/geoah/substrate/issues/222)).

### Confirmation

A config test asserting that an empty string, a short string, a non-base64
string and base64 of 16 bytes are each refused with a message naming the
generator command, and that base64 of 32 bytes is accepted. `mise run dev`
already mints a key into `.dev/credential.key`; that path must be updated to
mint a conforming one, and the dev tasks are the second gate.

## More Information

This decision depends on a host-key re-key path existing. `ResealRepository`
today re-keys sealed payloads under a repository's DEK; nothing re-wraps a DEK
under a new host key. That gap is the blocker, and it should be filed before
this record moves to `accepted`.

Reopen this if the substrate ever grows a KMS or a sealed-secret backend, where
the host key stops being an environment variable and the question becomes which
KMS handle to name instead.
