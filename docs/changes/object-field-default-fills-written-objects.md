---
type: feature
---

# An object field's `default:` fills the field in each object a write sends

A field inside an object property's `fields:` may now declare `default:`. The
loader refused the key before. The engine stores the default in every object
value a write sends without the field, in the row and in the changelog entry.
With this declaration:

```yaml
properties:
  contact:
    type: object
    fields:
      email: {type: email}
      locale: {type: string, default: en}
```

a create writing `contact: {email: a@example.com}` stores
`contact: {email: a@example.com, locale: en}`. Each item of a `repeated`
object and each value of a `keyed` one is filled the same way, and so is a
put or a patch that writes the object. A write that does not send `contact`
stores no `contact`, and leaves a stored one as it is. A field set to `null`
keeps no value.

A field default does not backfill stored objects. A field that becomes
required while stored objects lack it is refused even beside a default, and
the refusal says to write those objects first.

A declaration's key set is closed, so a binary from before this release
quarantines a package that declares a field default. Do not roll a server
back past this release once a package declares one.
