---
type: fix
---

# A `decimal` or `money` bound admits the value it names

Before this release the engine compared a `decimal` or `money` value against
the binary expansion of the bound's float64, so `min: 0.01` refused `"0.01"`
and `max: 0.3` refused `"0.3"`, both on a write and in the guard that refuses
a raised `min` or a lowered `max` over live records. Both now compare against
the number the declaration names.

The same change refuses, at its next write, a stored decimal that sat between
the bound and its float64. Under `max: 0.01`, `"0.0100000000000000001"` was
admitted and is now refused:

```
props.rate: must be <= 0.01
```

Only a decimal with 17 or more significant digits can sit there; a `money`
amount cannot. To find one, list the kind with a filter just past the bound:

```bash
substratectl get <authority>/billing/fee --filter '{"properties": {"rate": {"gt": "0.01"}}}'
```
