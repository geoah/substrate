---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0218. The approval page writes a package's settings and secrets as the owner

## Context and Problem Statement

A package's empty configuration records need owner input before its setup
can finish. The app must never receive the credentials the owner types.
This amends 0202's approval setup and 0076's configuration form.

## Considered Options

- Give the app credentials to configure its packages.
- Always send the owner to a separate settings page.
- Collect values on approval and write them through the owner's door.

## Decision Outcome

Chosen: the plan lists empty core `setting` and `secret` records belonging
to the package and its requirements, with id, description, secret flag and
required flag. Existing nonempty records are left alone. A secret field is
a password input and never starts with a stored value. Identity includes
both kind and id, so a secret cannot become a readable setting of the same
name.

Values travel only in the approval POST and transient running setup. The
server validates them against the plan, rejects unknown or repeated fields,
and refuses a missing required value with `needs-setting`. Once packages
land it writes values as the approving human actor through the ordinary
record path, which seals secret properties. Underlying write errors are
replaced with fixed wording because validation errors may quote input.
Values never enter plan hashes, parked views, tokens, app responses or logs.
Blank optional values are not written. Waiting add-ons collect no values.

### Consequences

- An app never handles the owner's package credentials.
- A failed setup may leave packages installed and some settings saved,
  following 0202's existing partial-progress rule.
- Settings use the engine's existing validation and sealed storage.

### Confirmation

`TestApprovalSettingsAreMetadataAndOwnerWrites` checks required values,
kind-sensitive field admission, writing after installation, unchanged plan
hashes and redaction of failures and captured logging.
`package-choices.test.tsx` checks password inputs and optional add-on choices.
