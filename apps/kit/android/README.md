# The kit on Android

The kit's Swift libraries run on Android under a Kotlin UI, through one
fixed bridge ([docs/plans/app-kit.md](../../../docs/plans/app-kit.md) §5.5).
Three Gradle modules here, built from the one Gradle root in
[apps/gradle](../../gradle):

| Module | What it holds |
| --- | --- |
| `kit-core` | `libSubstrateAndroid.so` (the kit's portable libraries, `SubstrateBridge` and `SubstrateBridgeCheck`, from `apps/kit/Package.swift`) with the Swift runtime and Foundation it loads, per ABI; the Swift packages' resource bundles as assets; `Bridge`, `BridgeCore`, `BridgeHandle`, `KeyedListState`, `DeviceServices`, `Redaction`; the values generated from `bridge.golden.json` (`bridge/Protocol.kt`) |
| `kit-device` | the device services in Kotlin, the only kit module that touches an Android service: `Transport` (OkHttp), `SecureStore` (Tink and DataStore), `Dns` (`DnsResolver.rawQuery`), `Connectivity`, `KitDevice.services(context)`; the backup and device-transfer rules |
| `bridge-check` | an app that starts the bridge from its `Application` and runs `BridgeChecks` (the runtime checks) against the Swift library; never published |

## How it runs

`Bridge.start(context, KitDevice.services(context))` runs once, from
`Application.onCreate` on the main thread: it loads the library, copies the
resource bundles out to `noBackupFilesDir/swift-resources`, and hands Swift
the device's locale, time zone, folders and SDK level with the protocol
fingerprint the Kotlin was generated with. Swift binds libdispatch's main
queue to the main Looper, takes the app's roots, and answers `Started` or a
terminal `Failed` (a fingerprint from another tree, a library that did not
load); nothing is half started. Then `Bridge.open("<root>")` answers a
`BridgeHandle`: its `state` is a `StateFlow` of the newest presentation and
every keyed list with its deltas applied, `send` posts a command, `close` is
idempotent.

Every message is one envelope, `{v, op, handle, id, seq, body}`, posted by
Kotlin (`NativeBridge.post`) or delivered by Swift (`NativeBridge.deliver`,
from a thread Swift attached, in a local frame, as UTF-16). Presentations
are coalesced in Swift, keyed lists cross as `KeyedListDelta`s and are never
coalesced, a device call is answered exactly once, a stream is ordered and
bounded and ends once, closing a handle answers what it waits on
`cancelled`, a message Kotlin does not take fails what waited on it, and a
body is recorded only where what it answers is known to carry no secret.
Messages are read and written with `JSONValue`'s exact reader and writer,
so a number crosses with every digit. The host tests
(`SubstrateBridgeTests`) hold each of those rules.

A body too large for a message is a file under `noBackupFilesDir/bridge`,
deleted by whichever side holds it last and swept when kit-device's
`Transport` is made; a credential exchange's never is. A locale or time
zone change reaches every open root as `environment`.

An app's roots are Swift: its library defines
`@_cdecl("substrate_bridge_roots") func roots(_ registry: UnsafeMutableRawPointer)`
and registers each by name on `RootRegistry.from(registry)`. A controller
tree needs no glue: `registry.register("tasks") { settings, context in
(TasksRoot(), context.appEnvironment(secureStore: "session")) }` opens it as
a `Runtime`, whose commands are `RouteCommand`s. An app builds its own
library the way `kit-core` builds `SubstrateAndroid` (the `substrate.swift`
convention plugin) and passes its name to `Bridge.start`.

## The generated Kotlin

A value that crosses conforms to `Bridged` (SubstrateCore) where it is
declared, and keeps to the model that protocol's comment names.
`bridge.json` lists the sources read; `kit/Tools/BridgeGen` reads them with
swift-syntax and writes `bridge.golden.json`, `kit-core`'s
`bridge/Protocol.kt` and `SubstrateBridge/ProtocolFingerprint.swift` (the
golden's SHA-256). Nobody edits the three by hand:

```bash
mise run android:generate          # after changing a Bridged value
mise run android:generate:check    # fails on a stale one; android:test runs it
```

`fixtures/roundtrip.json` is what Swift encodes of a value of each shape
(payload enums, an absent against a null optional, Unicode, decimals and
large integers); Kotlin decodes and encodes it, and Swift reads Kotlin's
back. `SUBSTRATE_UPDATE_FIXTURES=1 swift test --filter RoundTripTests`
rewrites it after a change to a value it holds.

## Building and testing

```bash
mise run android:sdk     # once: the SDK packages, NDK r30, the Swift SDK for Android
mise run android:build   # kit-core, kit-device and bridge-check, debug and release
mise run android:test    # the generator, the bridge on this host, the Kotlin tests, the round trip
mise run android:smoke   # on a booted emulator: the release build's checks, cold starts, the APK's libraries, the contract tests
mise run android:lock    # after a change to gradle/libs.versions.toml
```

`SUBSTRATE_ABIS=arm64-v8a` builds one ABI (the default is `arm64-v8a` and
`x86_64`). Swift on Android has no breakpoints, so the bridge's rules are
debugged in the host tests, where they run.
