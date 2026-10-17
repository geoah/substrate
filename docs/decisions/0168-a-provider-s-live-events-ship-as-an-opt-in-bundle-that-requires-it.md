---
status: accepted
date: 2026-10-17
decision-makers: George Antoniadis
---

# 0168. A provider's live events ship as an opt-in bundle that requires the provider, with its webhook off

## Context and Problem Statement

#951 added Slack live events to the shared `slack` bundle: a webhook trigger,
`slack-events`, shipped `enabled: true` with no key, and an events branch in
`slacksync`. The webhook door checks a key only when the trigger declares one
(`webhookTarget` in `internal/engine/webhooks.go`), so every install of the
Slack bundle exposed `POST /webhooks/<authority>/slack-events` to anyone who
could reach the server, and every POST wrote Slack rows. The batch format it
reads was produced only by a Socket Mode bridge in another, private
repository, so the shared bundle carried a feature nobody else could use.
#951 was in no release when this was written.

## Considered Options

- Keep the events branch in `slacksync` and ship the trigger disabled.
- A separate provider bundle, `slacklive`, that `requires:` the Slack bundle,
  writes its mirror rows, and ships no trigger: the operator writes the keyed
  webhook trigger. The bridge moves into this repository as
  `cmd/slack-socket`.
- An engine rule that refuses a keyless webhook trigger, or a bundle-level
  way to declare that a trigger needs a key.

## Decision Outcome

Chosen: the separate bundle. Installing the Slack bundle again exposes no
webhook, and the live path is something a deployment takes on purpose,
together with a bridge it can now build from this repository. A disabled
trigger in `slack` would still put the events code and its permissions into
every install. The engine rule was ruled out for this change: it changes
what every webhook trigger may be, which is a decision of its own.

What this sets for later code:

- A provider bundle may `require` another provider and write that
  provider's kinds. Nothing in admission scopes a function's
  `permissions.writes` or `permissions.reads` to its own package; the
  `requires:` line is what makes the dependency legible and refuses the
  install while the other package is absent.
- Another package's secrets never cross: an injected input must be the
  bundle's own kind (`resolveBundle`), and `config.accounts` carries only the
  bundle's own `accountconfig` kinds. So `slacklive` takes its own
  `userToken` secret (0076) and reads the Slack account with
  `host.records.list`.
- A webhook trigger whose callable writes records is not shipped at all,
  not even disabled: the console's Resume flips `enabled` on a record and
  would open a keyless endpoint in one click. The bundle's README gives the
  whole record, and the operator writes it with its key in one
  `substratectl apply -f`. Being the operator's record, it survives a
  reinstall or an upgrade of the bundle with its key.
- The engine checks `requires:` only when the requiring bundle is
  admitted. Uninstalling the Slack bundle leaves `slacklive` installed;
  uninstall `slacklive` first.

### Consequences

- Good, because the Slack bundle is poll-only again, and the endpoint exists
  only where somebody keyed and enabled it.
- Good, because the bridge is public, configured by environment alone, and
  built from the repository's one Dockerfile.
- Bad, because `slackevents` carries a copy of `slacksync`'s row makers (a
  bundle function is one self-contained module). The two must change
  together; `TestSlackLiveCopyMatchesSlacksync` fails when a shared
  definition differs outside its allowlist.
- Bad, because the user token is pasted twice, and a rotation must be made
  in both places.
- Bad, because turning live events on is a hand-written record, not a
  switch on the provider page.
- Bad, because a repository that ran Slack bundle 21 (on `main` and the
  `latest` image only, never released) keeps its enabled, keyless
  `slack-events` trigger record after upgrading to 22: no upgrade removes a
  trigger the closure stopped shipping, and a POST to it starts a
  `slacksync` walk. The upgrade order, in the Slack and slacklive READMEs
  and the catalog: stop the bridge, `substratectl delete
  substrate.reamde.dev/core/trigger slack-events`, upgrade the Slack
  bundle, install `slacklive` and write its keyed trigger, then point the
  bridge at the new URL.

### Confirmation

`TestSlackLiveShipsNoWebhookTrigger`, `TestSlackLiveRequiresTheSlackBundle`
and `TestSlackLiveCopyMatchesSlacksync` in
`internal/providertest/slacklive_test.go`; section 19 of the Slack provider
e2e (`internal/providere2e/providers/slack/scenario.py`), which asserts `404`
for keyless and keyed POSTs after the install, writes the keyed trigger,
asserts `404` for keyless and wrong-key POSTs, then runs the batches. The rule for future webhook triggers is held by
review only.

## More Information

[`kinds/providers.substrate.reamde.dev/slacklive/README.md`](../../kinds/providers.substrate.reamde.dev/slacklive/README.md)
and [`cmd/slack-socket/README.md`](../../cmd/slack-socket/README.md).
Revisit when the engine grows a way for a trigger to refuse keyless
deliveries, or for one bundle function to share another's code.
