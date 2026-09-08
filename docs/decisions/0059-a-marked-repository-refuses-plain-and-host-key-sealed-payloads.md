---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
---

# 0059. A repository marked DEK-only refuses plain and host-key-sealed payloads

## Context and Problem Statement

[0023](0023-a-sealed-payload-is-bound-to-its-address.md) bound every sealed
payload to its row and left one consequence open: the plain (`'p'`) and
host-key-sealed forms "must keep opening until every repository is resealed,
which nothing currently records". `openWithFallback` (`internal/engine/dek.go`)
therefore tried the DEK and then the host key on every read, a `'p'` payload
opened with no key at all, and a wrapped DEK (`repositories.dek`, the
manifest's `dek`) named no host key, so a wrong `SUBSTRATE_CREDENTIAL_KEY`
failed as "sealed payload but no key opens it". A payload the re-key missed
was found at recovery, when the age identity could not open it, and a `'p'`
row planted after the re-key was indistinguishable from legacy
([#133](https://github.com/geoah/substrate/issues/133); the sealed store
review [#99](https://github.com/geoah/substrate/issues/99), sections 2.3 and
2.4). This record amends that one consequence of 0023; the binding and the
framing byte stand.

## Considered Options

- A per-repository marker, set by a one-shot re-key at the repository's first
  open, after which the two legacy forms are refused; a host-key id beside
  each DEK wrap
- Keep the fallback open forever, as 0023's consequence reads, and count the
  opens that took it
- Refuse the legacy forms unconditionally in this release and require an
  operator command to re-key first
- A key id on every sealed payload, not only on the DEK wraps

## Decision Outcome

Chosen: the marker and the host-key id. `repositories.sealed_dek_only`
(manifest `sealedDekOnly`) records that every payload in the repository's
sealed store is bound-framed under its DEK. Creation sets it, because a
repository is born with a DEK and seals nothing any other way. The first open
of a repository not yet marked runs `rekeySealedStore` once
(`retireLegacySealed`), which rewrites every `'p'`, host-key-sealed and
unbound `'s'` payload as `'a'` under the DEK, then marks the row and rewrites
the manifest; recovery enrollment requires the marker before it writes and
re-keys again in its own transaction. It writes nothing to the row: the
scoped transaction cannot reach `repositories`, and a control-plane write
after the commit could fail once the record, and a server-minted identity
returned exactly once, are already out. A boot import carries the manifest's
marker into the row, beside a DEK only. Once marked, `openRepoPayload`
refuses a `'p'` payload and never tries the host key, and its error names the
framing found and the key expected. `repositories.dek_key_id` (manifest
`dekKeyId`) names the host key each wrap is under: 16 hex digits of a
domain-separated SHA-256 over the key material (`hostKeyID`), written by
creation, adoption, the legacy-directory move and the offline rewrap of
[0054](0054-a-repository-moves-between-host-keys-through-an-offline-rewrap.md).
A wrap that names no key is re-wrapped under the host's key at the first open
on a keyed host, and only then named: the old wrap may be plain-marked, from a
keyless release, and an id must never name a key that protects nothing. The
id explains a failure and never causes one: the wrap is always tried, and only
its failure is reported in terms of the two ids.

What readers keep opening, on every repository: the unbound `'s'` framing
under the DEK. It is ciphertext under the repository's own key, so it costs no
confidentiality against the store's threat model; the re-key rebinds it to
`'a'`, so a marked repository holds none; and `rekeySealedStore` itself must
read it on an unmarked store. The retired inline-sealed property form (a
`sealed:` value on a record) keeps opening under the DEK and then the host
key, marked or not: it lives in record properties, which the re-key never
touches, so the store's marker cannot speak for it. A `'p'` DEK wrap on a
keyless host is the control plane's business, not the store's, and is
unchanged. `OpenPayloadWithKey`
refuses `'p'` whatever the marker says: it exists to prove a key opens a
payload, and a plain payload proves nothing.

The fallback-forever option leaves the downgrade invisible: without state, a
legacy store and a planted `'p'` row look the same. Unconditional refusal
makes every repository from before the marker unopenable until an operator
acts, for a re-key the engine can run itself in one transaction. A
per-payload key id is the wrong unit: a repository has one DEK, and the
framing byte already says whether a payload is plain, under the DEK or under
the host key; the identifier that was missing is the host key's, on the two
DEK wraps.

### Consequences

- Good, because a `'p'` or host-key-sealed row planted on a marked repository
  is refused with a named error instead of being decrypted or read, and
  `sealed_dek_only` is the operator's answer to which repositories the
  recovery key opens completely.
- Good, because a wrong `SUBSTRATE_CREDENTIAL_KEY` is reported as the wrong
  key, with the id the wrap names and the id the host holds, rather than as
  damage.
- Good, because nothing changes for a repository created under this release:
  it is born marked, and the open pays one row read.
- Bad, because the first open of an unmarked repository rewrites every legacy
  payload in one transaction holding every sealed row `FOR UPDATE`; on a
  large store that is a long first open.
- Bad, because the marker lives in the row and in the manifest. The row is
  the truth and `ensureManifest` rewrites the manifest from it, which is the
  same arrangement the DEK wrap already has.
- Bad, because an `'s'` payload copied from a backup taken before the re-key
  still opens unbound at any address, the 0023 move by way of an old
  ciphertext. Refusing `'s'` on a marked repository would close it and is the
  trigger for revisiting this record.
- Bad, because the marker says nothing about plaintext secrets written into
  changelog payloads before secrets moved into the store, which the change
  feed's `?q=` match still reads; that is a different repository state.
- Bad, because a read-only open (`repository verify`) neither re-keys nor
  marks, so that process keeps the fallback for an unmarked repository.
- Bad, because one sealed row that opens under neither key now refuses the
  whole repository's open, where it used to fail one read. The material is
  unrecoverable either way; the operator deletes the row and its file and
  the user re-enters the secret (`docs/operations.md`, what happens at boot).
- Bad, because `repository rebuild` without `SUBSTRATE_CREDENTIAL_KEY` on an
  unmarked repository holding host-key-sealed rows fails at the open's
  re-key, where it used to skip sealed material. Set the key; a keyless
  rebuild of a marked repository is unchanged.
- Bad, because a marked manifest is proven at import by opening every sealed
  file under the DEK, so a copy whose files disagree with its marker is
  refused rather than imported; the refusal names the file.

### Confirmation

`TestOpenRepoPayloadRefusesLegacyFormsOnceMarked` and
`TestHostKeyIDNamesAKeyWithoutRevealingIt` (`internal/engine/dek_internal_test.go`),
`TestMarkedRepositoryRefusesLegacyPayloads`,
`TestFirstOpenRekeysAndMarksAnUnmarkedRepository`,
`TestFirstOpenRewrapsAWrapThatNamesNoKey`,
`TestBootNamesADamagedWrapUnderTheSameKey` and
`TestWrongHostKeyIsNamedById` (`internal/engine/sealed_dekonly_db_test.go`),
`TestEnrollRecoveryKeyWrapsADEKThatOpensMigratedPayloads` and
`TestEnrollRecoveryKeyRefusesAnUnmarkedDatasetBeforeWriting`
(`internal/engine/recovery_db_test.go`),
`TestFirstLoginOfAPreDEKRepositoryMirrorsTheReKeyedStep` and
`TestImportRefusesAMarkedDirectoryWhoseFilesDoNotOpen`
(`internal/engine/sealed_dekonly_login_db_test.go`),
`TestManifestRoundTrip` and `TestManifestJSONOmitsNothing`
(`internal/changelogfile/manifest_test.go`), and migration
`0020_repository_dek_key_id.up.sql`.

## More Information

0023 stays `accepted`: this record replaces its fifth consequence and nothing
else. DEK rotation is [#237](https://github.com/geoah/substrate/issues/237);
host-key rotation of a live repository, re-wrapping every DEK under a new
key, is the future work [0024](0024-the-credential-key-is-key-material-not-a-passphrase.md)
names and has no ticket yet. Whether the keyless read-only operator open
should stay is left open here.
