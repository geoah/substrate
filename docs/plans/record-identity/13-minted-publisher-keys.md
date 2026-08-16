# Proposal 13: minted publisher keys, verified URL aliases

What every URL-storing path keeps, this proposal refuses to store: a URL
is a lease (GitHub namespaces are recyclable and repojacked
at scale, domains expire and resell, accounts rename), and an append-only
hash-chained changelog freezes whatever the lease meant at write time. So a
publisher's identity is a minted, opaque, collision-proof key (a hash of
the publisher's public key, or a sealed random id), and that is what enters
kind references, reference values and the chain; the URL, or a dotted DNS
name, is a claimed alias on the publisher's record, proven at claim time (a
well-known file in the repo, an OIDC attestation, a DNS record), stamped
with when it was verified, re-checkable and replaceable without touching
history. This is AT Protocol's DID-and-handle split, and the direction Go
(the checksum database) and npm and JSR (provenance attestations) each
retrofitted after shipping name-first identity. The costs: a publisher
record becomes part of the model, every display of a kind is a lookup
instead of the stored string, and the readable name moves from the grammar
into verification state.

```
publisher key (identity)  pk7f2q9x4kd3.pub           (minted once; what the changelog stores)
claimed alias             github.com/geoah/vocab     (verified 2026-08-16; re-checkable, replaceable)
kind reference            pk7f2q9x4kd3.pub/note      (splits today: the key carries the dot)
a record's path           pk7f2q9x4kd3.pub/note/n1
the console shows         geoah/vocab: note           (alias plus verification state, like a handle)
```
