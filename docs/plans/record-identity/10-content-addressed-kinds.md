# Proposal 10: content-addressed kind identity

A kind's identity is a hash of its declaration (Unison's approach to
names): the authority is the hash spelled as a dotted label under a
reserved pseudo-TLD, legal under today's grammar, registry-free to split,
impossible to squat, and independent of DNS and URLs alike; the publisher's
URL rides as provenance metadata on the bundle. The fatal cost is stated up
front: identity changes with every edit, and the whole upgrade model keys
on stable identity plus an integer version (the boot upgrade, the catalog's
upgrade preview, `kinds:check`), so this needs a stable lineage identifier
on top of the hash, which reintroduces the naming problem it was meant to
dissolve. Honest verdict: the right primitive for pinning and integrity (a
lockfile row, a verify command, the digest half of an OCI reference), the
wrong primitive for the identity records are stored under; it is in this
set so it is rejected on the record rather than re-proposed.

```
declaration             (the kind declaration's canonical bytes)
authority (identity)    mfrggzdfmzts.cas               (hash label + reserved pseudo-TLD; splits today)
kind reference          mfrggzdfmzts.cas/note
provenance              data.source: https://github.com/geoah/vocab   (a claim, not the identity)
the cost                edit the declaration, get a new hash: every stored reference names the old kind
```
