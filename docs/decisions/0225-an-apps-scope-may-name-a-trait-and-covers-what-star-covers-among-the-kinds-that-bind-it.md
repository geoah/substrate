---
status: accepted
date: 2026-11-04
decision-makers: George Antoniadis
---

# 0225. An app's scope may name a trait, and covers what `*` covers among the kinds that bind it

## Context and Problem Statement

An app's scope names kinds
([0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md),
over the [0080](0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md)
grammar): one kind, or a glob over an authority or a package. That fits an
app that brings its own kinds, and not one that works over a meaning the
owner's kinds share, such as a to-do app over everything that can be
completed, whoever published the kind. Such an app cannot know the kinds'
names in advance, and a glob wide enough to reach them reaches far more than
the meaning. Traits already name such meanings, and the records read
already narrows by one ([`filter.implements`](../traits.md)); a scope has no
way to.

## Considered Options

- A scope may name a trait, `read:implements(<trait>)` or
  `write:implements(<trait>)`, covering every kind that binds it when a
  request is checked.
- The same, resolved once at approval into exact scopes over the kinds that
  bind it then.
- The app asks for `full` access
  ([0204](0204-an-app-may-ask-for-full-access-and-its-owner-approves-it-whole.md))
  and filters by trait itself.
- The app asks for `write:*` and filters by trait itself.

## Decision Outcome

Chosen: a scope may name a trait, `read:implements(<trait>)` or
`write:implements(<trait>)`, and it covers exactly what `*` would, narrowed to
the kinds whose declaration binds that trait when the request is checked. It
beat resolving at approval because the point is to keep working as the owner
installs kinds that share the meaning, which is what 0080 decided for a glob,
at the same cost. It beat `full` and `write:*` because those hand the app
everything to get one meaning, and an owner approving a to-do app should be
approving to-dos.

The rules this binds:

- **The trait is named in full**, as
  [0101](0101-a-kind-trait-or-callable-is-named-in-full-on-every-surface.md)
  holds on every surface, spelled as the records filter spells it, and is
  matched in that canonical spelling alone. `~` stands for the repository's
  authority as it does in a kind pattern, and the token stores the resolved
  form. A binding's variant and arguments are no part of the match: a kind
  that binds `temporal(point: dueAt)` implements
  `substrate.reamde.dev/core/temporal`.
- **Membership is the declaration's, read once per request.** A kind declared
  or upgraded later that binds the trait is covered from the next request,
  and one that stops binding it is not. A feed stream keeps the membership it
  opened with until it reconnects, as it keeps a glob's kinds, and still reads
  the token again before every batch, so revoking the token ends it. At
  approval, a declaration the approval installs decides membership before the
  one the repository holds.
- **It reaches no further than `*`.** The auth kinds are never reached, nor is
  an `internal` kind. No trait scope writes the `core` or `llm` package, and no
  app's token writes an account or a bundle input, whatever binds the trait.
  A trait scope writes no account, a kind that binds `accountconfig`, so it
  covers no code that would either:
  `write:implements(substrate.reamde.dev/core/accountconfig)` writes nothing.
- **Installed code keeps its grants.** A binding removed later narrows the
  token, not the code an approval installed while the binding held, which
  keeps its own grants as 0215 accepts for any code that runs on its own.
- **A trait nothing binds yet is admitted**, as a glob that matches nothing is.
  The approval page lists the kinds that bind it at approval, or says none do
  yet, and says that kinds that bind it later are covered too.
- **Coverage stays conservative.** `*` covers a trait scope of the same verb
  (write covering read), and a trait scope covers only itself. For code an
  approval installs
  ([0215](0215-code-an-approval-installs-reaches-no-further-than-the-scopes-it-approves.md),
  [0219](0219-an-apps-token-reads-windows-aggregates-semantic-search-blobs-and-the-feeds-history-over-the-kinds-it-reads-and-calls-the-functions-its-call-scope-names.md)),
  it covers the reach of an exact kind that binds the trait, never of a glob.

### Consequences

- Good, because an app asks for a meaning rather than names it cannot know,
  and the owner approves the meaning in words.
- Good, because every scoped surface follows from the one per-kind check: the
  reads, writes and searches, and the windows and feed 0219 scopes over the
  kinds a token reads, take a trait scope with no route of their own.
- Bad, because what a token reaches depends on declarations the owner may not
  have read: a publisher that binds the trait on a new kind widens every token
  holding that trait's scope, as an imported package widens a glob.
- Bad, because the check needs the kind's declaration and not only its name,
  so every caller hands the matcher the registry's kind.
- Bad, because a binary from before this change refuses such a token, since a
  stored scope it cannot parse fails closed (0194), so a rollback past it
  signs those apps out.
- Bad, because 0080's lattice gains a member that is no prefix: a package glob
  never covers a trait scope, so an app that already holds one may be asked
  again for what it reaches in practice.
- Bad, because an open feed stream does not follow a binding added or removed
  while it runs: it sees the change when it reconnects.
- Bad, because the scope layer cannot tell a bundle input from any other kind:
  the engine refuses an app's token every write to one, but an approval holds
  code's reach to one as it holds any kind's, for a trait scope as for every
  other, a gap 0215 leaves open.

### Confirmation

`internal/scope`'s tests hold the grammar, the resolution of `~` and that a
trait scope never reaches an auth, `internal`, `core` or `llm` kind or writes
an account, directly or as code's reach. `internal/api`'s scoped tests hold reads, writes and search
through a trait scope, including for a kind that binds the trait after the
token was minted, and the console's scope fixture holds the approval page's
words.

## More Information

Prompted by Substrate Tasks working over every kind that can be completed,
which needs a trait for that meaning; that trait is a decision of its own.
Agents' and functions' `permissions` grants (0080) do not take a trait here.
Reopen if one needs to, or if a trait scope needs to exclude a kind that
binds it.
