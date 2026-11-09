---
status: proposed
date: 2026-11-09
decision-makers: George Antoniadis
---

# 0251. One provider package carries every chat network, and the network is a property

## Context and Problem Statement

The chat bridge ([plan](../plans/chat-bridge.md), D3) signs in to WhatsApp
first, then Signal, Messenger, Instagram, Google Messages and more. Each
network could be a provider package of its own, or every network could share
one. The bridge speaks one contract for all of them, built on mautrix's
bridgev2, which already puts the networks behind one interface; the Beeper
provider made the same choice for one product ("one product, so no product
prefix").

## Considered Options

- A package per network (`providers.substrate.reamde.dev/whatsapp`, …),
  each with its own kinds.
- One package, `providers.substrate.reamde.dev/chatbridge`, whose kinds are
  the same for every network and carry the network as a property.
- The Beeper provider's kinds, written by the bridge as well.

## Decision Outcome

Chosen: one package, `providers.substrate.reamde.dev/chatbridge`.

Its kinds take the contract's nouns: `instance`, `account`, `user`, `chat`,
`message`, `messageattachment` and `chataction`. Every row carries its
`network`, and its id is composed with the network as the provider:
`host.ids.external(network, login id, …)`, so two networks never compose one
id and a sign-in moved to another instance keeps its rows. An `account` is
one sign-in on one network, not one per bridge, because each sign-in has its
own credential, connection state and settings. It binds core's `sync` trait
and not `accountconfig`: the engine lets only the owner create an
`accountconfig` record (`checkBundleOwnerGate`), and these are written by
the webhook function when the owner signs in on the bridge.

A package per network repeats seven kinds, their record views, the agent
tools and a contact mapping for every network, and makes a new network a new
package rather than a change to the bridge. Beeper's kinds mirror Beeper
Desktop's API, whose ids are Matrix ids and whose rows its own sync writes;
a second writer would break that sync's one-writer rule
([0125](0125-a-provider-write-is-a-callable-that-writes-no-record.md)).

### Consequences

- Good, because one set of kinds, views, tools and one contact mapping
  serves every network, and a search over messages spans them.
- Good, because a network the bridge adds needs no package change.
- Bad, because no grant can exclude one network's rows from a kind, which is
  what Telegram's terms on AI use would ask for (the plan, section 10).
- Bad, because a field only one network has is a package version for every
  network, or waits.
- Bad, because the Providers page lists one provider for every network, and
  the console's generic account listing, which reads `accountconfig`, does
  not list these accounts: the package's record views do.
- Bad, because a network reached through Beeper and through the bridge is two
  sets of rows, which only contacts join.

### Confirmation

`mise run kinds:check` and `TestShippedBundlesInstallOnTheSeed` hold the
kinds; `TestChatBridgeIDVectors` and `TestChatBridgeIDVectorsNameTheGoldenRows`
in `internal/providertest` hold the id rule.

## More Information

Reopen when one network's rows must be kept from an agent that may read the
others, or when the networks' shapes drift far enough apart that one kind
stops describing them.
