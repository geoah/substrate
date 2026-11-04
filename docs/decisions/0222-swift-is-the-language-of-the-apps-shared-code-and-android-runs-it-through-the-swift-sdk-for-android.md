---
status: proposed
date: 2026-11-03
decision-makers: George Antoniadis
---

# 0222. Swift is the language of the apps' shared code, and Android runs it through the Swift SDK for Android

## Context and Problem Statement

The Apple apps share about 30,000 lines of Swift package code (the client,
sign-in, the outbox, Workouts' engines and presenters), and the owner wants
tens of new apps that build for Android as well, sharing their logic,
sign-in, synchronization and caching. A language for that shared code had to
be chosen before any of it moved, and every app written after inherits it.
The evaluation, a Linux build of the existing core and an Android spike are
in [the app kit plan](../plans/app-kit.md) (§3, §4).

## Considered Options

- Swift for the shared code, run on Android through the official Swift SDK
  for Android, each platform drawing its own UI.
- Kotlin Multiplatform for the shared code, with Compose Multiplatform for
  one UI on both systems.
- Rust for the shared code (UniFFI or Crux), native UIs on each system.
- Go for the shared code through gomobile.
- Swift with Skip's SwiftUI redrawn in Compose on Android.

## Decision Outcome

Chosen: Swift for the shared code, run on Android through the Swift SDK for
Android, with SwiftUI on Apple systems and Compose on Android. It is the only
option that keeps the code that exists: WorkoutsCore built unchanged for
Linux and Android, and its tests and WorkoutsSync's passed on Linux before
anything moved (plan §3). Every other option rewrites it in another language,
which pays off only if Android or the Go server became where the logic lives,
and neither is the plan. The SDK has been official since Swift 6.3, and a
large Swift core under a Compose UI ships in production elsewhere. Sharing
SwiftUI through Skip would give Android only what its subset of SwiftUI
allows, and Compose Multiplatform would give up native SwiftUI on the primary
platform.

### Consequences

- Good, because the existing packages move rather than being rewritten, and
  one test suite holds the logic on every platform.
- Good, because the Mac and future Apple platforms come for free, and Linux
  CI holds portability without a Mac runner.
- Bad, because an Android app carries the Swift runtime and Foundation: the
  arm64 release APK measured 77 MB, 40 MB of it ICU.
- Bad, because the toolchain is young: an optimizer crash had to be worked
  around, Swift on Android has no breakpoints in Android Studio, and the
  open-source toolchain must match the SDK's version exactly.
- Bad, because `Bundle.module`, the device's locale and time zone, and
  `@MainActor` do not work on Android as they do on Apple systems, so shared
  code receives all of them as inputs.

### Confirmation

`mise run apps:test:linux` builds and tests the kit and every app's core on
Linux in CI (the `apps linux` job; the template's core through
`apps:new:check`), and the `android build` job cross-compiles the kit for
Android. `lint:apps` refuses an Apple-only import in portable code.
`lint:toolchain` holds the Swift pin, the Swift SDK for Android's version and
Gradle's NDK equal.

## More Information

[The plan](../plans/app-kit.md) has the comparison, the spike's measurements
(§3.2) and the review record. Worth reopening if the APK size becomes a
constraint the owner sets, or if Android rather than Apple becomes the
primary platform.
