---
status: accepted
date: 2026-09-12
decision-makers: George Antoniadis
---

# 0076. A bundle ships its settings as core `setting` and `secret` records

## Context and Problem Statement

A bundle that needs one key and one URL for the whole repository (the
Firecrawl sample, the web sample) declares a `config` kind of its own and one
input pointing at it. The engine resolves the input to a record and injects
it, which works, but the console can only present it as "an input named
`connector` of kind `config` with no record yet", and every such bundle
declares a near-identical kind. The provider shape (an OAuth `client` input
beside per-account records) is a different problem and stays as it is.

## Considered Options

- Keep the per-bundle `config` kind and input, and fix the console copy.
- A `settings:` block on the bundle header that the import materializes.
- Two core kinds, `setting` and `secret`, that a bundle ships as ordinary
  records in its closure, empty where the user must fill them.

## Decision Outcome

Chosen: two core kinds shipped as records, because a closure already carries
data records (the catalog PUTs every non-declaration document on install, and
the `llm` sample ships provider rows with an empty key this way), so a bundle
declares a setting by writing an empty one and nothing new is parsed. A
`settings:` block would add a dialect key to say what a record already says.
The per-bundle `config` kind keeps working for anyone who wants typed
properties, but the shipped samples move off it.

- `substrate.reamde.dev/core/setting` carries `value` (string), a `type` hint
  (`string`, `url`, `int`, `bool`, `enum` with `values`) the engine validates
  `value` against on write, `displayName`, `description` and `required`.
- `substrate.reamde.dev/core/secret` carries the same fields with `value`
  typed `secret`, so it is sealed on write and never read back.
- The record id is `<bundle id>/<name>`. The bundle id is `<authority>/<package>`,
  so a sample's ids are rehomed with the rest of the closure and two bundles
  may both own an `apiKey`. Ownership is the prefix and nothing else.
- Every `setting` and `secret` under a bundle's prefix is injected into its
  functions as `config.settings.<name>`, the secret in plaintext at the runner
  boundary and held there by the invocation scrubber, exactly as an input's
  secret is today.
- A `required` setting or secret whose `value` is empty is a setup item on the
  bundle's status (`code: setting`), so the registry badge, the bundle page and
  the sidebar count it. Purging the bundle removes its settings; uninstall
  leaves them, as it leaves every record.

### Consequences

- Good, because a bundle's configuration is two records anyone can read, and
  the console shows every bundle's settings on one page as a form.
- Good, because nothing is parsed that was not parsed before: no dialect key,
  no new resolution rule.
- Bad, because a setting's type is a hint on a record, not a property type in
  the kind system, so validation is a second, smaller layer and a rich shape
  (an object, a list) is a string or a per-bundle kind.
- Bad, because ownership by id prefix is a convention the engine enforces only
  at injection, setup and purge; a hand-written record under another bundle's
  prefix is that bundle's setting.

### Confirmation

`internal/providertest/firecrawl_test.go` asserts the two records land on
import, the missing key is a setup item, and the functions read
`config.settings`. `mise run kinds:check` holds the core version bump.

## More Information

Inputs (`docs/terms.md`) stay for the provider shape: the OAuth client and the
per-account records. Revisit if a shipped bundle needs a structured setting,
which would argue for typing `value` by a declared property type instead of a
hint.
