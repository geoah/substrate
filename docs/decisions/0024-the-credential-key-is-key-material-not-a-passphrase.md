---
status: accepted
date: 2026-08-17
decision-makers: George Antoniadis
---

# 0024. `SUBSTRATE_CREDENTIAL_KEY` is key material, not a passphrase

## Context and Problem Statement

`deriveCredentialKey` (`internal/engine/credentials.go:240`) is one unsalted
SHA-256 over whatever string the operator set, with no iterations and no length
floor, and `internal/config/config.go:41` validates nothing. That key unwraps
every repository's DEK, and the wrapped DEKs sit in the same database as the
sealed rows they open. An attacker with a dump and a dictionary tries a guess
for the cost of one SHA-256 and one AES-GCM open. Found by the sealed store
review ([#99](https://github.com/geoah/substrate/issues/99), section 1.1);
filed as [#229](https://github.com/geoah/substrate/issues/229).

## Considered Options

- Demand key material: base64 of exactly 32 bytes, refuse anything else.
- Stretch a passphrase properly with argon2id and a stored salt.
- Accept both: stretch what does not decode as 32 bytes, take what does.

## Decision Outcome

Chosen: demand base64 of exactly 32 bytes and refuse anything else at boot,
naming the command that generates one.

Stretching is a real defense and is rejected for a different reason. argon2id
with a known salt still raises the cost of a guess from the few hundred
nanoseconds this key derivation costs today to a few hundred milliseconds, a
factor of roughly a million, which is the difference between an instant
dictionary crack and an infeasible one for a moderately good passphrase. The
salt living in the stolen database does not undo that; it only removes the
benefit of a per-target salt, which was never the point.

It is rejected because it leaves the operator in the loop. Under stretching the
security of a deployment is a function of a passphrase nobody can inspect from
outside, and the substrate can neither measure it nor report on it: the same
config produces a strong deployment or a weak one depending on a choice made
once and never revisited. Demanding key material removes that variable
entirely, and the strength of every deployment becomes a property the code
guarantees rather than one the operator supplies. It also keeps a tunable cost
parameter off the boot path of every process that opens a repository, operator
commands included.

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
  The old SHA-of-the-string derivation is gone, so there is no in-place
  migration: nothing re-wraps a DEK under a new host key (`ResealRepository`
  re-keys sealed payloads under the DEK, not the DEK under the host key). This
  is pre-v1, so it lands before anyone stores real secrets, which is the only
  window where "no migration" costs nothing. A dev store keyed under a
  passphrase is re-created with `mise run dev:wipe` (compose:
  `docker compose down -v`); a store holding real data must not exist yet.
- Bad, because it makes the shipped `compose.yaml` fail to start until the
  operator sets a key, which is the correct outcome and still a change in
  first-run experience
  ([#230](https://github.com/geoah/substrate/issues/230)).

### Confirmation

A config test asserting that an empty string, a short string, a non-base64
string and base64 of 16 bytes are each refused with a message naming the
generator command, and that base64 of 32 bytes is accepted. `mise run dev`
already mints a key into `.dev/credential.key`; that path must be updated to
mint a conforming one, and the dev tasks are the second gate.

## More Information

There is no host-key re-key path: `ResealRepository` re-keys sealed payloads
under a repository's DEK, and nothing re-wraps a DEK under a new host key. That
gap does not block this record, because pre-v1 no store holds real data, so the
break has nothing to migrate. A host-key rotation path is future work, wanted
the moment a deployment holds secrets under a key that must change.

Reopen this if the substrate ever grows a KMS or a sealed-secret backend, where
the host key stops being an environment variable and the question becomes which
KMS handle to name instead.
