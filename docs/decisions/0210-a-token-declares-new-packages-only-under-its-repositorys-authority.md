---
status: accepted
date: 2026-11-02
decision-makers: George Antoniadis
---

# 0210. A token declares new packages only under its repository's authority

## Context and Problem Statement

Any token could create a package under any authority but
`substrate.reamde.dev`: `POST /api/v1/vocabulary/apply` of
`google.com/anything` was admitted, so a repository could squat names it does
not own. A hand apply of a shipped provider's files (`substratectl apply -f`)
landed the provider `installed`, editable and outside its publisher's
upgrades, which [0048](0048-providers-are-published-samples-are-copied.md)
kept open on purpose. The authority plan (`docs/plans/authorities.md`,
decision 5) needs the creation of a package to say who owns its name.

## Considered Options

- Keep refusing only the publisher's own authority, by equality
  (`authorizeNewPackage` until now).
- Refuse a new package under any authority but the repository's own, for every
  hand that is not the substrate's.
- Refuse only the shipped authorities (`*.substrate.reamde.dev`) and leave
  other names open.

## Decision Outcome

Chosen: a token creates a new package only under the repository's own
authority. Every other authority reaches a repository from the binary: the
seed and the boot upgrade, a catalog install, or a sample import, which
rehomes its closure onto the repository's authority. The rule binds every
hand that is not a substrate path (`substrate`, `bundle:<authority>:<package>`):
the owner's doors, an app, an agent and a function alike. The refusal is
`403` and names `substratectl install <package>` and
`POST /api/v1/catalog/{id}/install`. A package the repository already holds
under another authority keeps the source rules of 0047 and 0048: an
`installed` copy stays writable, and its next install promotes it. Refusing
only the shipped authorities would leave every other name open to squatting.

This amends [0046](0046-a-repository-owns-one-authority-chosen-at-registration.md):
the authority chosen at registration is also the only one a token may create
packages under. It amends 0048: the hand apply of a provider's files is no
longer a door.

`SUBSTRATE_INSECURE_ALLOW_FOREIGN_PACKAGES` lifts the rule for a local
server. The provider end-to-end suite needs it, because it applies each
provider's files rewired at a local mock, and so does `mise run test:e2e`,
whose cases declare under fixed authorities. It stays until the plan's
development document (its decision 8) replaces it.

### Consequences

- Good, because a package name under another authority now means its
  publisher's closure, arrived through the binary.
- Good, because an app's `declare:~/<package>` is unaffected: `~` is the
  repository's own authority.
- Bad, because a provider's declarations can no longer be held open to
  editing by a fresh hand apply; a copy applied before this change keeps
  that.
- Bad, because a local flag now exists that disables the rule, and a
  deployment that sets it loses the rule.

### Confirmation

`TestDeclarationAuthority` in `internal/engine` opens with the rule on and
refuses a new package under `substrate.reamde.dev`,
`providers.substrate.reamde.dev`, `samples.substrate.reamde.dev` and
`example.org` for the owner's doors, an app, an agent and a function, and
admits a bundle hand and a held package.

## More Information

Revisit when a repository can prove another domain is its own (the plan's
decision 3): a verified domain could then be a second authority a token may
create packages under.
