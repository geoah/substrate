---
type: feature
---

# A `substrate.reamde.dev/core/trigger` titles itself from its optional `label`

The `trigger` kind declares an optional `label` string, and its title reads
`{label|callable}`: a trigger with a label lists under it in the console and
in `substratectl get`, and one without keeps the callable's title as before. Several triggers that call one function no longer list as
copies of the same reference.

```yaml
kind: substrate.reamde.dev/core/trigger
metadata:
  id: mail-nightly
data:
  properties:
    label: Nightly mail sync
    source:
      schedule:
        recurrence: "FREQ=DAILY;BYHOUR=2"
        timezone: UTC
    callable: substrate.reamde.dev/core/function/example.com/mail/syncmail
```

An empty or whitespace-only `label` counts as none. The same holds for every
kind's `displayTemplate`: a value of whitespace alone now hands the next
alternative its turn, where it used to render an empty title.

A title is derived when the record is written, so the upgrade leaves every
stored trigger's title as it was; a trigger takes its label's title at the
write that sets `label`. The shipped providers' triggers carry labels such as
`Gmail first sync`, `Gmail scheduled sync` and `Gmail sync now`, and a
repository receives them when it takes the provider's upgrade.
