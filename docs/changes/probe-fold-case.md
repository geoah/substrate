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

A probe without the key is unchanged. A folded probe reads every live row of
the target kind, so keep it to kinds of people scale.
