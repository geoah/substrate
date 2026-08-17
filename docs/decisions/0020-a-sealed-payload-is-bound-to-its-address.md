---
status: proposed
date: 2026-08-17
decision-makers: George Antoniadis
---

# 0020. A sealed payload is bound to the address it was written at

## Context and Problem Statement

`sealWith` (`internal/engine/credentials.go:177`) passes nil additional data to
AES-GCM, so a ciphertext is bound to its key and to nothing else. The read side
does not compensate: `openSecretValue` resolves a ref with
`SELECT payload FROM sealed WHERE ref = $1` and no owner predicate, and the
only ownership check in the system is `sealedRefOf` on the write path. An
attacker who can write the `sealed` table without holding the key therefore
copies one row's payload into a row they can read through the runner's secret
injection, and the substrate decrypts it for them. Found by the sealed store
review ([#99](https://github.com/geoah/substrate/issues/99), section 2.1);
filed as [#231](https://github.com/geoah/substrate/issues/231).

## Considered Options

- Bind `ref || record_kind || record_id` as AES-GCM additional data, behind a
  new framing byte.
- Add the owner predicate to every read query and leave the sealing alone.
- Store a MAC over the row's addressing columns in a second column.

## Decision Outcome

Chosen: bind `ref || record_kind || record_id` as additional data, under a new
framing byte `'a'` beside the existing `'p'` and `'s'`.

The owner predicate alone is weaker: it moves the check back into SQL, where
the attacker already has a hand, and it does not protect the DEK wrap in
`repositories.dek`, which has no owning record. A second MAC column is the same
guarantee at the cost of a second key and a second thing to keep in sync, and
GCM already offers the binding for free.

The framing byte is required rather than optional. The reseal migration and the
recovery tooling both open payloads written by older releases, and a reader
cannot tell an AAD-bound ciphertext from an unbound one by looking at it: it
would simply fail to authenticate, which is indistinguishable from a wrong key
or a corrupt row. `'a'` says which construction to use, so the failure stays
diagnosable.

The binding string is the three fields joined by a byte that cannot appear in
any of them. Refs are `secret:`/`auth:` prefixed hex, a record kind is
`{authority}/{name}` and a record id draws from a frozen alphabet, so a NUL
separator is unambiguous. The DEK wrap has no owner, so it binds the literal
string `dek` and the repository id instead.

### Consequences

- Good, because a `sealed` row moved, copied or swapped by anyone without the
  key stops decrypting, which turns a confidentiality break into an availability
  one.
- Good, because it costs nothing at runtime: AAD is hashed by the same GCM pass
  that already runs.
- Good, because the DEK wrap gains a binding it never had, so a wrap lifted
  from one repository's row into another's stops opening.
- Bad, because rotation now has to re-seal rather than copy, and any future
  "move this secret to another record" verb has to re-seal too. Nothing does
  either today.
- Bad, because it is a third framing byte, and the open path grows a branch
  that never goes away: `'p'` and `'s'` payloads must keep opening until every
  repository is resealed, which nothing currently records
  ([#133](https://github.com/geoah/substrate/issues/133)).
- Bad, because a repository whose sealed rows are `'a'`-framed cannot be read
  by an older binary at all. That is correct behavior and it is still a
  one-way door.

### Confirmation

A test that seals a payload at one `(ref, kind, id)`, writes the bytes into a
second row at a different address, and asserts the open fails. A second test
that `rekeySealedStore` leaves an `'a'` payload byte-identical when the DEK
already opens it, which is the migration's idempotency.

## More Information

Scope check done while reviewing: nothing in the tree breaks under the binding.
`rekeySealedStore` (`internal/engine/reseal.go:456`) re-seals in place under the
same ref and owner, rotation mints a fresh ref and deletes the old row,
`RebuildRepository` never touches `sealed`, and `OpenPayloadWithKey` takes the
address from the row it read.

Reopen this if a verb that moves a secret between records is ever wanted: the
binding is what would make that expensive, and re-sealing under the new address
is the answer rather than weakening the binding.
