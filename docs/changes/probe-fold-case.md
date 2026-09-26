---
type: feature
---

# A `recordmapping` probe matches in any case with `fold: case`

A probe compares exactly, so `Ada Example` and `ada example` are two people
and the second spelling mints a second subject. A probe that declares
`fold: case` lowercases and trims both the source value and the target's
stored value before comparing them (decision record 0107):

```yaml
data:
  match:
    - from: realName
      to: name
      fold: case
```

A probe without the key is unchanged. Turning the key on for an existing
mapping makes its probe ambiguous wherever case-variant duplicates already
exist, so under the default `onAmbiguous: park` new sources park until the
owner merges those duplicates. The stored side folds under the database's
locale, so a `C` locale folds only ASCII.
