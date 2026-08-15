---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0009. The changelog chain hashes what Postgres stored, with value-exact numbers

## Context and Problem Statement

The changelog gained tamper evidence: every entry carries a SHA-256 hash over
its content and the previous entry's hash (`internal/engine/chain.go`). The
payload is a `jsonb` column, and Postgres re-renders what Go sends — key
order, whitespace, number lexemes — so the bytes hashed at write time and the
bytes a verifier reads later are only the same if the preimage is defined
carefully. Getting this wrong strands every historical hash on the first
Postgres upgrade, or lets two different stored texts hash identically.

## Considered Options

- Hash the bytes Go marshaled, before insert.
- Canonicalize with RFC 8785 (JCS) at both write and verify time.
- Hash the stored text (`RETURNING payload::text`) canonicalized with a
  value-exact number normal form, computed with integer string operations.
- A Merkle tree / transparency-tree design (`x/mod/sumdb/tlog`, Trillian).

## Decision Outcome

Chosen: hash the stored text, canonicalized in Go (sorted keys, one escaping
policy, numbers reduced to a value-exact normal form: `1.5`, `1.50` and
`15e-1` all canonicalize alike), because it is the only option whose two ends
provably read the same bytes and whose durability rests on what Postgres
guarantees (the VALUE of a `numeric`) rather than on how any version prints it.

Hashing Go's bytes fails immediately: `jsonb` normalization means the verifier
can never reproduce them. JCS fails on this store's actual contents:
integer-typed properties are `int64` (`internal/engine/validate.go`), reseal
preserves lexemes with `json.Number` precisely because float64 cannot
(`decodeNumberPreserving`), and `jsonb` holds arbitrary-precision decimals,
while JCS renders numbers as ES6 doubles — an out-of-domain value must be
rejected or rounded, and either breaks the guarantee. A Merkle tree earns its
weight when an untrusted party needs efficient inclusion and consistency
proofs over someone else's history (substrate-to-substrate sync, if it ever
arrives); a linear chain is the right size for one repository verified by its
own operator, and the entries stay the truth, so a tree can be computed later.

The preimage is length-framed with a domain tag (`substrate/changelog/v1`),
carries the repository id (a cross-repository splice fails even where seqs
line up) and a presence byte on `caused_by` (NULL and zero must not collide),
and hashes are stamped at settle time, after `settleFold` has merged a
transaction's late effects into its last entry's payload.

### Consequences

- Good, because verification recomputes hashes from exactly what it reads:
  no float coercion, no rendering dependence, no second implementation.
- Good, because zero new dependencies: `crypto/sha256` and ~80 lines of
  stdlib canonicalization with frozen test vectors.
- Bad, because the canonical form is value-based: a tamper that rewrites a
  number's lexeme without changing its value (`1.50` to `1.5`) is invisible.
  Accepted: the value IS the datum; display scale is jsonb bookkeeping.
- Bad, because the hash covers the stored text, so the write path pays a
  `RETURNING payload::text` round-trip and one UPDATE per entry at settle.
  Measured against a Postgres write, noise.

### Confirmation

`TestCanonicalNumberValueExact`, `TestCanonicalJSONNormalizes`,
`TestEntryHashPreimageInjective` and `TestFieldBoundariesDoNotSlide`
(`chain_internal_test.go`) freeze the format; `TestChainNamesEveryTamper` and
`TestChainBackfillStampsLegacyHistory` (`chain_db_test.go`) hold the two ends
together over real jsonb, including numbers a float64 cannot carry.

## More Information

Replaces the plan `docs/plans/changelog-integrity.md` (deleted with this
record; its threat model now lives in
[docs/changelog.md](../changelog.md#the-chain)). Revisit if
substrate-to-substrate sync needs third-party proofs — that is the Merkle
trigger — or if payloads ever stop being JSON.
