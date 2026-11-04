---
status: accepted
date: 2026-11-03
decision-makers: George Antoniadis
---

# 0223. A screen is a controller over a pure engine and presenter, and Android receives its presentations over one fixed bridge

## Context and Problem Statement

With the shared code in Swift
([0222](0222-swift-is-the-language-of-the-apps-shared-code-and-android-runs-it-through-the-swift-sdk-for-android.md)),
two things decide whether tens of apps can share it: how a screen's logic is
shaped so a SwiftUI view and a Compose view draw the same thing, and how that
crosses from Swift to Kotlin. Chat and Workouts were built two different
ways, and a Swift-to-Kotlin bridge is where cross-platform cores get
expensive. [The app kit plan](../plans/app-kit.md) (§5.1, §5.2, §5.5) has
the design and its three rounds of review.

## Considered Options

- Workouts' shape for every screen: a command, a controller that enriches it
  into an event, a pure engine, a journal written before any suspension,
  effects decided outside the engine, and a pure presenter; values only cross
  to the UI, and Android receives them as JSON over a fixed JNI surface with
  the Kotlin generated from the Swift declarations.
- The same shape, with Swift values bridged to Kotlin as generated JNI
  objects (swift-java's jextract, or Skip's bridge).
- Each app's own `@Observable` models, as Chat had them, with a UI per
  platform written against them.
- A reducer that returns its effects (TCA's shape).

## Decision Outcome

Chosen: Workouts' shape for every screen, with Android receiving
presentations as JSON over one fixed bridge whose Kotlin is generated.

- **The engine is pure and replayable.** Effects are decided after it, on the
  live path only, so a replay runs none. A runtime could replay a reducer that
  returns its effects the same way, by dropping them. Here the engine never
  sees an effect, so that rule lives in the runtime once, not in every
  reducer.
- **One presenter writes every label,** so read and edit views and the two
  platforms cannot disagree on words.
- **The journal is written before any suspension,** so a crash replays to the
  last state the journal kept.
  - That holds where the write succeeded. After a failed write the change
    stands in memory and the runtime reports the journal's problem for the
    app to show. The next write that succeeds keeps it.
  - A journal the device cannot read is never taken for an empty one. The
    runtime refuses every event it would journal, says why, and reads it
    again before the next, so nothing is written over what it could not
    read.

Composing
screens under one root (the link) keeps one presentation tree, so a parent
and child update in one step on both systems. A fixed bridge puts every
lifetime, threading and error rule in one place instead of one per value,
never grows when an app adds a screen, and leaves Kotlin with plain data the
JVM tests and screenshots use without loading Swift. Generated JNI objects
would work for one call and grow with every app.

The bridge holds three rules for every device call:
- **A call and its cancel are ordered.** A cancel either stops a request
  before it is sent or follows it, so Kotlin never starts work a Swift
  caller has already left.
- **Each side releases what it opens, on every end.** A handle's close ends
  its calls and streams. A sign-in attempt's listener or callback is let go
  whether it was answered, cancelled, expired or replaced.
- **No device call blocks one of Swift's executors.** The device services are
  asynchronous, so a call waiting on Kotlin holds no thread of Swift's pool.

### Consequences

- Good, because a screen is tested once, by value, and both UIs draw the
  same presentations.
- Good, because the bridge is one protocol with one fingerprint over its wire
  declarations. A Kotlin build whose declarations differ from the Swift it
  loads refuses to start, and two builds with the same declarations start
  together.
- Bad, because every app writes its screens in this shape, including where a
  plain model would be shorter, and the runtime grows requirements as apps
  need them (three were added for Chat).
- Bad, because every presentation is encoded once per change; long lists
  cross as revisioned deltas to keep that bounded, which is machinery of its
  own.
- Bad, because bridged values are restricted (no hand-written coding), so a
  type whose stored format needs a custom decoder cannot cross as it is.

### Confirmation

The runtime's tests hold the order, replay, epochs, coalescing and
composition by value (the suites in `RuntimeTests.swift` and
`RuntimeCompositionTests.swift` under `apps/kit/Tests/SubstrateKitTests`).
The journal's refusals are held by the runtime's tests over a journal that
cannot be read. The bridge's tests (`apps/kit/Tests/SubstrateBridgeTests`)
hold a cancel's order against its request, a stream's cancel after its
channel is gone, and a secure-store read that holds no thread while it
waits. The instrumented `BrowserSignInContractTest`, run by
`mise run android:smoke`, holds the sign-in's release of what it opened.
`mise run android:generate:check` fails when the bridge's golden or Kotlin is
stale, and the protocol fingerprint is checked at the bridge's start. The
gallery fails a fixture that has no renderer on either platform.

## More Information

The words (command, controller, engine, effect, presenter, presentation,
journal, link, signal) are defined for apps in
[docs/terms.md](../terms.md). Worth reopening if a screen's presentation
cannot be encoded fast enough for its updates on a slow phone.
