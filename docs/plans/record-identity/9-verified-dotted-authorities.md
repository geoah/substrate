# Proposal 9: verified dotted authorities

Identity stays the dotted string and the grammar does not move; what
changes is that an authority must be verified when it is claimed. The
publisher proves control of the DNS name with a DNS record or a well-known
file served over HTTPS (ACME's DNS-01 and HTTP-01 are the two shapes), the
installer checks the proof, and the bundle records what was checked and
when. Publishing without owning a zone still works through any DNS name
the publisher controls in fact (`geoah.github.io` counts). The costs:
grouping stays one subdomain per group, so the path-grouping want stays
unmet; the proof is a dated fact, not a permanent one (a domain can change
hands after its verification); and an unverifiable dotted name, today's
status quo, stops being installable, which is the point.

```
authority (identity)    vocab.geoah.dev
the proof (DNS)         TXT _substrate.vocab.geoah.dev = "substrate-authority=vocab.geoah.dev"
the proof (well-known)  https://vocab.geoah.dev/.well-known/substrate.json
                        { "authority": "vocab.geoah.dev" }
recorded at install     verified 2026-08-16, via dns
kind reference          vocab.geoah.dev/note       (splits today, byte-identical)
```
