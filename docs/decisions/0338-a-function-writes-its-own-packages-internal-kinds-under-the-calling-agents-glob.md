---
status: accepted
date: 2026-11-26
decision-makers: George Antoniadis
---

# 0338. A function writes its own package's internal kinds under the calling agent's glob

## Context and Problem Statement

[0239](0239-an-agent-declares-kinds-through-the-vocabulary-tool-under-its-writes.md)
holds every hand an agent writes through, the effects of a function it runs
among them, to its named grants for every `internal` kind. A package that
caches what its function fetches in an internal kind of its own (a scraped
page, a cursor) then fails whenever an agent calls that function: the owner's
assistant, granted `*`, called a page-scraping function and every call was
refused with "is an internal kind, the substrate's own machinery", nothing
applied. The assistant ships with the binary, so the owner cannot name the
package's kind in its writes, and no other agent should have to name every
package's cache.

## Considered Options

- An effect on an internal kind the function's own package declares passes
  the named-grant check; every other internal kind is still held.
- Ask each package to declare its cache `supporting`, not `internal`.
- Keep 0239 as it stands and have the owner name each kind in the writes of
  every agent that may call the function.

## Decision Outcome

Chosen: the function's own package's internal kinds pass, because the rule
exists to keep an agent, steered by what it read, from reshaping the
substrate's machinery (the vocabulary, when code runs, what its next turn
reads as given, where a key is sent). A package's own internal kind is that
package's machinery, and the body that writes it is the writer the package
means for it. The agent chooses the call and its arguments, as it already
does for the owner's data, not what the body writes.

- The exception reads the function whose body returned the effect, a callee's
  included, and holds only where that function's package declares the kind.
- Declaration kinds, `core/trigger` and `llm/instruction` stay held whatever
  the function: they are `core`'s and `llm`'s, which ship no function body.
- The effective emit still bounds every effect, so a sub-agent whose
  ceiling does not cover the kind is refused, and the policy door still
  reads each effect.

Declaring the cache `supporting` was refused as the fix: `purpose` says what
a client lists and what search ranks lower, and a cache is not the owner's
data to list. Naming the kind in every caller's writes was refused because a
shipped agent's writes are not the owner's to edit.

### Consequences

- Good, because a package with a cache works under the owner's assistant and
  any agent granted its data, with no grant naming the cache.
- Bad, because an injected instruction can now make an agent fill a
  package's internal kinds with whatever that package's function writes for
  the arguments it chose. That is the function's contract, and a package
  that must not be driven that way gates its function with a policy or
  `confirmation: always`.
