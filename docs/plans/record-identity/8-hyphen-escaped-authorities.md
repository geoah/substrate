# Proposal 8: hyphen-escaped authorities

Spell the URL's slashes with an injective escape drawn from characters the
authority grammar already admits: `/` becomes `--` and a literal `-`
becomes `-0` (the Go module proxy escapes uppercase with `!` and IDNA
spells Unicode as `xn--`; representing a wider alphabet inside a frozen one
is well-trodden). `github.com/geoah/vocab` stores as
`github.com--geoah--vocab`, which is a legal authority today, so nothing
widens: not the Go validators, not the console, not either function SDK
regex, not the pattern shipped to Postgres, and the split stays
byte-identical everywhere. A declaration's id stays its kind reference,
since no new character enters the id alphabet, which neither proposal 2 nor
3 manages. The costs: the stored form is not the URL (the same
display-mapping tax as proposals 2 and 3, milder to read), a one-time check
must confirm no stored authority already carries `--` or `-0` (the shipped
22 carry no hyphens at all), and the escape is one more rule a human
authoring a manifest must know.

```
URL of the publisher    github.com/geoah/vocab
authority (stored)      github.com--geoah--vocab        (legal under today's grammar)
kind reference          github.com--geoah--vocab/note
a declaration's id      github.com--geoah--vocab/note   (still a kind reference)
a record's path         github.com--geoah--vocab/note/n1
a literal hyphen        my-org.example/x  ->  my-0org.example--x
```
