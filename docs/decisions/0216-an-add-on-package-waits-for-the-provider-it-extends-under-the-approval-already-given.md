---
status: proposed
date: 2026-11-03
decision-makers: George Antoniadis
---

# 0216. An add-on package waits for the provider it extends, under the approval already given

## Context and Problem Statement

An app may offer a package that extends a provider the repository does not
hold. Installing that provider just to satisfy an optional add-on would
change the owner's choice. This amends 0202's all-or-nothing package rows.

## Considered Options

- Install the provider with every add-on.
- Require a new app approval after connecting the provider.
- Record the optional approval on the token and plan it again on install.

## Decision Outcome

Chosen: optional package rows whose publisher lists `addonFor`. The owner
checks the add-ons to approve. An absent provider gives the row action
`wait`; the token's `appAddons` records package, version floor and provider.
The catalog install path tries waiting add-ons after installing their
provider, including providers installed as requirements. It reads live
tokens anew, excludes expired tokens, and plans under each token's scopes.
The bundle writes its own closure. Failed or blocked add-ons stay waiting
and appear in the install answer and console notification. Already-held
add-ons at the approved floor are not reinstalled.

The catalog accepts an approval-policy callback to avoid a dependency cycle
with appsetup. The server wires it before serving requests. Until decision
0215's complete reach checker is integrated, deferred executable closures
under scoped tokens are refused conservatively; kinds-only closures and
full-access approvals can proceed.

### Consequences

- Connecting a provider can finish setup already approved by its owner.
- Revocation ends waiting approval; it does not remove installed packages.
- A blocked add-on does not roll back the provider installation.
- An add-on needing settings waits for the owner to fill them on its page;
  secrets are never saved on a token for a later installation.

### Confirmation

`TestOptionalAddonsRetainOnlyTheOwnersSelection` checks optional consent and
waiting token metadata. `TestProviderInstallReplansLiveWaitingAddons`
checks replanning with the live token, expiry, revocation, blocked reports,
retry, and bundle attribution.
