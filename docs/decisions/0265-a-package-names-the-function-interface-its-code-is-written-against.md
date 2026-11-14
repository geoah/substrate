---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0265. A package names the function interface its code is written against

## Context and Problem Statement

A package's Python functions are written against what the server hands them,
the `input`, and what they call back, the `host` object. That pair had no
version. A function names its `runtime`, `python` or `host`, and nothing
else (`internal/vocabulary/function.go`). The runner's protocol has a
version, but nothing at runtime compares it, because both ends ship in one
binary (`internal/runner/protocol.go`). The Python release was fixed only in
the image (`Dockerfile`). So an author had nothing fixed to write against,
and a package written for a newer server installed on an older one and
failed at its first delivery. The finding and the proposal are F10 in
[pull request 1313](https://github.com/geoah/substrate/pull/1313).

## Considered Options

- A version on each function.
- A minimum server release on the package header.
- A version of the interface on the package header, with the input's shape
  pinned by a test.

## Decision Outcome

Chosen: an interface version on the package header, because a package's
functions are written and published together, and an interface number says
what the code needs where a server release says only where it was tried.

What this sets:

- **The key is `interface`** on the package header, a whole number. An absent
  key stores nothing and reads as 1, the interface every package was written
  against before the key existed. The server runs interface 1
  (`vocabulary.FunctionInterfaces`).
- **A version the server does not run is refused** by the loader, naming the
  version and the ones it runs. Every door a package comes through loads it
  with the same loader, so nothing of the package lands.
- **The input only gains keys within a version.** The keys a body reads,
  their JSON types and whether each is always present are a golden, read
  off the envelopes as the dispatcher builds them. A renamed or removed key, or
  a changed host call, is a new version.
- **The interface is labelled `beta`.** It is supported, and a break is
  announced one minor release ahead. The `functions` feature, the call
  route, keeps its own label.
- **The Python release is part of the interface.** A server release supports
  the current Python and the one before. The image carries the current one;
  the host and the shipped bodies are linted for an older grammar. No test
  runs a body on the release before.

### Consequences

- Good, because an author writes `interface: 1` and knows what the input
  holds, and a server that cannot run a package says why before anything
  lands.
- Good, because a change to the input fails a test, where before a body found
  out.
- Bad, because the key is one more a server must know: an older server
  refuses a header carrying it as an unknown key.
- Bad, because "and the one before" is stated more than it is held: CI runs
  bodies on one interpreter.
- Bad, because the host object's calls are not pinned by the golden, only by
  the runner's own tests.

### Confirmation

`TestAPackageNamesItsFunctionInterface` (`internal/vocabulary`) holds the
grammar and the refusal, and
`TestAPackageNamingAnInterfaceTheServerLacksIsRefused` (`internal/testenv`)
holds the refusal at the apply door. `TestFunctionInputMatchesTheGolden`
(`internal/engine`) fails when the input moves
(`internal/engine/testdata/function_input.golden.json`).

## More Information

[Functions](../functions.md#the-interface) is the author's description, and
[stability](../stability.md#a-packages-code) the promise. Revisit when a
second interface version is needed: the server then runs both, and the
release that drops the older one says so.
