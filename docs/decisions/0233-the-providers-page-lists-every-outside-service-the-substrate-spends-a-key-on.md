---
status: accepted
date: 2026-11-06
decision-makers: George Antoniadis
---

# 0233. The Providers page lists every outside service the substrate spends a key on

## Context and Problem Statement

A deployment's operator can now supply keys and OAuth clients a repository
uses where it holds none of its own ([the plan](../plans/host-defaults.md),
and decision 0232 in PR 1213). The console had nowhere that showed,
in one place, every service the substrate spends a key on and whose key each
one runs on: Providers held only the catalog providers that bring data in
([0130](0130-the-console-serves-four-things-and-its-navigation-follows-them.md)),
and the AI model rows (`llm/provider`) were reached only through their
records and an agent's "Add your key" dialog. A new person on a deployment
that pays for chat could not see that it did, or swap in their own key.

## Considered Options

- A separate **Services** page beside Providers, listing every service by
  where its key comes from (built first in PR 1212).
- Keep Providers as the data-bringing providers, and leave the
  `llm/provider` rows on their records.
- One **Providers** page listing every outside service the substrate spends
  a key on, in three groups.

## Decision Outcome

Chosen: one Providers page, in three groups in this order: **Accounts you
connect** (the catalog providers that bring data in), **Tools for your
agents** (catalog providers whose package declares no account kind and
offers functions to agents,
[0198](0198-a-bundle-offers-its-functions-and-an-agent-takes-them-by-pattern.md)),
and **AI models** (one card per `llm/provider` row, seeded or made by the
person, titled by its label). This amends what 0130 and
[docs/console.md](../console.md#providers) say Providers holds: it is every
outside service with a key, not only the ones that bring data in.

A Services page split one question, "what does my substrate connect to",
across two pages that both listed Google and its sign-in, and needed its own
sidebar row, unread dot and seen list to be found at all; the owner rejected
it on 2026-11-06. Leaving the AI model rows off Providers kept the one key
most people must set, the one an agent needs to answer, out of the place
that lists keys.

The rules the page keeps:

- Every card says what it gives you and whether it works (On, Set up, Needs
  attention, Paused; an AI model card is never Paused, since a row has no
  off switch). A "whose key" line ("Using _Site_'s key", "Using your key",
  "Through _Site_'s Google sign-in") appears only where someone other than
  the person could supply one.
- An AI model card is its own component with the key form in place, not a
  bundle card; the row's record page is a Technical details link.
- The operator's daily allowance sits on the AI models heading, and as one
  line on the Tools heading, never at the top of the page or over the
  accounts.
- Where the deployment loads an operator defaults file, registration lands
  on Providers with a welcome the person dismisses. Home is unchanged
  ([0148](0148-an-ongoing-problem-is-one-alert-record-written-under-the-callable-it-is-about.md)).
- The sidebar's Providers count and Home's Providers card count what the page
  shows as set up: installed provider packages and AI model rows whose key
  resolves.

### Consequences

- Good, because one page answers "what does my substrate connect to, and
  whose key does it use", and an agent's missing key links to the card that
  fixes it.
- Good, because nothing new enters the navigation: no sixth place, no unread
  dot, and one preference (`welcomeDismissed`) instead of a seen list.
- Bad, because "provider" now names two things on one page: the catalog tier
  in the first two groups and an `llm/provider` row in the third. The groups'
  headings carry the difference, and the code keeps both words as they were.
- Bad, because before a package is installed the catalog does not say which
  kinds carry the accountconfig trait or which functions are offered, so the
  Tools group reads an uninstalled package by its closure (no kinds, some
  functions) and may place one differently once it is installed.
- Bad, because the page reads three more things (the operator's defaults, the
  `llm/provider` rows and the function records) on top of the catalog.

### Confirmation

`web/console/src/pages/providers-models.test.tsx` holds the group order and
placement, the AI model card's states and key actions, the search line, the
allowance's placement and the welcome; `pages/register.test.tsx` the landing;
`router-providers.test.ts` sends `/services` to Providers.

## More Information

[docs/console.md, Providers](../console.md#providers) is the page as built.
Revisit if a third kind of outside key appears that is neither a provider
package nor an `llm/provider` row.
