---
status: accepted
date: 2026-11-09
decision-makers: George Antoniadis
---

# 0248. A webhook trigger a publisher ships is enabled only with a key or a signature

## Context and Problem Statement

[0168](0168-a-provider-s-live-events-ship-as-an-opt-in-bundle-that-requires-it.md)
ruled that a webhook trigger whose callable writes records is not shipped at
all, not even disabled, because the console's Resume flips `enabled` and the
door checks a key only when the trigger declares one
(`webhookTarget` in `internal/engine/webhooks.go`). Apple Health already
ships `applehealth-webhook` disabled and keyless, because
[0202](0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md)
has an app's approval key and enable a trigger its package ships, and the
chat bridge package will ship `chatbridge-events` the same way
([the plan](../plans/chat-bridge.md), D8). Nothing stopped an owner's door,
an agent or a function from enabling either without a key, which opens an
endpoint that runs a publisher's code for anyone who reaches the server.

## Considered Options

- Keep 0168 and ship no such trigger. An approval would then have to create
  the trigger, which 0202 does not do: it turns on one its package ships.
- Refuse every enabled webhook trigger with no key, the engine rule 0168
  ruled out for changing what every trigger may be. It also ends
  [0045](0045-a-webhook-trigger-is-a-public-endpoint-with-an-optional-key.md)'s
  open endpoint on a private network, and the Pebble sample's.
- Refuse by the callable: a trigger running a published package's function
  needs a key. The trigger the slacklive operator writes by hand is such a
  trigger, so this also takes 0045 away from triggers the owner wrote.
- Refuse by who wrote the trigger: one a builtin or published package's
  install or upgrade wrote is enabled only with a key or a signature, on
  every write, the install's included.

## Decision Outcome

Chosen: refuse by who wrote the trigger, because it closes the hole at every
door (the console's Resume, the API, an agent, a function, an install)
while the owner's own triggers keep 0045 and an approval, which writes the
key and `enabled` in one write, passes.

What this sets:

- A trigger is a publisher's when its changelog holds a write by the hand of
  a package the repository holds `builtin` or `published`
  (`bundle:<authority>:<package>`, or the seed's `bundle:core`), or when the
  write being admitted is one. Every hand that ever wrote the id counts, so
  deleting the trigger and writing it again by hand does not make it the
  owner's: the next upgrade would write the shipped one over it anyway.
- A write that leaves a publisher's webhook trigger enabled with no key and
  no signature is refused with a validation error naming the trigger and
  its package, saying to set `source.webhook.key` in the same write or to
  approve the app that sets it up. Turning it off never needs a key.
- A published package may ship its webhook trigger disabled and keyless,
  for an app's approval or its owner to key and enable. One that declares
  a signature may ship enabled, as WHOOP's does
  ([0164](0164-a-webhook-signature-is-verified-by-the-host-when-the-trigger-declares-one.md)).
  An install of a package that ships one enabled with neither is refused
  whole.
- An imported sample's trigger is the owner's: the import writes the closure
  under the repository's own authority and holds it `installed`, editable
  like one the owner wrote. Pebble's endpoint stays open as shipped.
- Of the engine's internal writes, only a publisher's own hand is held: an
  install, an upgrade, the seed. A repository migration never wedges on a
  trigger stored before this rule.
- A trigger an earlier binary let its owner enable keyless is not turned off
  when the binary upgrades. The door keeps admitting what it admitted, the
  server logs the trigger and its package each time the repository opens,
  and the next write that leaves it open is refused, whatever it changes. A
  re-install or an upgrade of its package writes it back as shipped, off,
  because the install carries over only a keyed trigger's switch
  (`keepTriggerSettings`), as it did before this record.

This amends 0168: a provider may ship a webhook trigger disabled and keyless
when an approval or its owner keys it. 0168's other rules stand, and
slacklive still ships no trigger: the one its operator writes is the
owner's, which this rule does not hold to a key.

### Consequences

- Good, because the console's Resume of a publisher's keyless trigger is
  refused with a sentence that says what to do, and so is every other door.
- Good, because a published package from any authority cannot open an
  endpoint by being installed.
- Good, because an approval, `substratectl apply -f` of a keyed record and
  the synthetic data loader each write the key with `enabled`, and pass.
- Bad, because a trigger left open by an earlier binary stays open until
  somebody writes it, and the server log is the only report: an owner who
  never reads it is not told. A repository that ran Slack bundle 21 holds
  one, `slack-events`.
- Bad, because any edit of such a trigger, its label included, is refused
  until it is keyed or turned off.
- Bad, because the console still offers Resume on a publisher's keyless
  trigger, and only the refusal says why.
- Bad, because an owner who wants a publisher's callable open on a private
  network must write a trigger of their own under another id.
- Bad, because the rule reads the trigger's changelog: a compaction would
  have to keep the entries of the hands that wrote a trigger.

### Confirmation

In `internal/engine`: `TestAShippedWebhookTriggerIsEnabledOnlyWithAKey`,
`TestAnApprovalKeysAndEnablesAShippedWebhookTrigger` (the real set up run
over the engine), `TestAnUpgradeLeavesAShippedWebhookTriggerAsItWas`,
`TestAnOpenShippedWebhookTriggerIsReportedAndKept`,
`TestAPackageShippingAnOpenWebhookTriggerIsRefused` and
`TestAnOwnersOwnWebhookTriggerMayStayOpen`. The console's
`src/pages/tool.test.tsx` shows a refused Resume's sentence.
`TestShippedBundlesInstall` (`internal/catalog`) installs every shipped
bundle under the rule.

## More Information

Amends [0168](0168-a-provider-s-live-events-ship-as-an-opt-in-bundle-that-requires-it.md)
and narrows [0045](0045-a-webhook-trigger-is-a-public-endpoint-with-an-optional-key.md)'s
open endpoint to the owner's own triggers. The rule is
`refuseOpenShippedWebhook` in `internal/engine/triggers.go`. Revisit when a
provider's sender can neither carry a key nor sign, or when the console
learns a trigger needs a key before it offers Resume.
