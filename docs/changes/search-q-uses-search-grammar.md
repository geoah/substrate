---
type: breaking
release: v0.84.0
---

# The ranked read `q` parses the search grammar and refuses a query with no word

API clients and agents calling the ranked read are hit. Before v0.84.0 the
lexical arm of `GET /api/v1/records?q=` parsed `q` with Postgres's
`websearch_to_tsquery`. From v0.84.0 it uses the search grammar: bare words
conjoin and stem, a `*` in a word marks a prefix, quotes make a phrase, `-`
excludes, `OR` in capitals disjoins, and no other character is an operator
(`a&b` is one word). The grammar applies in every `mode`, and
`substratectl search` sends its query the same way.

A query with no word left in it is now refused instead of ranked:

```
GET /api/v1/records?q=*
```

answers `422` `validation` with the message

```
q: "*" has no word to match
```

## What to do

1. Stop sending a `q` that is only stars, quotes or dashes; treat a `422`
   there as an empty query.
2. Expect `lay*` to match words starting with `lay`, where the star used to
   be ignored.
3. Quote a phrase you meant literally; other punctuation no longer splits a
   word.
