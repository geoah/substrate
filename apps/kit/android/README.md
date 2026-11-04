# The kit on Android

The kit's Swift libraries run on Android under a Kotlin UI, through one
fixed bridge ([docs/plans/app-kit.md](../../../docs/plans/app-kit.md) §5.5).
Three Gradle modules here, built from the one Gradle root in
[apps/gradle](../../gradle):

| Module | What it holds |
| --- | --- |
| `kit-core` | `libSubstrateAndroid.so` (the kit's portable libraries, `SubstrateBridge` and `SubstrateBridgeCheck`, from `apps/kit/Package.swift`) with the Swift runtime and Foundation it loads, per ABI; the Swift packages' resource bundles as assets; `Bridge`, `BridgeCore`, `BridgeHandle`, `KeyedListState`, `DeviceServices`, `Redaction`; the values generated from `bridge.golden.json` (`bridge/Protocol.kt`) |
| `kit-device` | the device services in Kotlin, the only kit module that touches an Android service: `Transport` (OkHttp), `SecureStore` (Tink and DataStore), `Dns` (`DnsResolver.rawQuery`), `Connectivity`, `KitDevice.services(context)`; the backup and device-transfer rules |
| `kit-ui` | the shared screens in Compose and Material 3, one composable per SubstrateUI view (`ConnectScreen`, `ServerTooOldScreen`, `AccessBannerView`, `AccessEndedScreen`, `ReviewScreen`, `RepositoryScreen`, `StatusScreen`, `SettingsScreen`, `AboutScreen`, `ProblemView`, `ChangeLineView`, `UnrevokedNotices`, `AppMenu`), the building blocks an app draws its own screens with (`KitTheme`, `KitCard`, `KitSection`, `KitRow`, `KitValueRow`, `KitNavigationRow`, `KitDestructiveRow`, `StatusBadge`, `ProblemBox`), the design tokens Swift writes (`KitTokens.kt`), and `KitGallery`, each screen's composable |
| `bridge-check` | an app that starts the bridge from its `Application` and runs `BridgeChecks` (the runtime checks) against the Swift library; never published |
| `gallery` | the Gallery app: every shared screen in every state, live, from the fixtures the `gallery` root presents through the bridge, drawn by kit-ui; never published |

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
coalesced, a device call is answered exactly once, a chunked answer is
ordered and bounded and ends once, closing a handle answers what it waits on
`cancelled`, a message Kotlin does not take fails what waited on it, and a
body is recorded only where what it answers is known to carry no secret.
Messages are read and written with `JSONValue`'s exact reader and writer,
so a number crosses with every digit. The host tests
(`SubstrateBridgeTests`) hold each of those rules.

A runtime root's presentation crosses without its keyed lists' rows. The
runtime encodes each tree (`Runtime.encodedFrames()`) with every
`KeyedList` in it written as `{"$list": "app/tasks", "revision": 4}`, the
list named by where it sits in the tree, and hands over beside it the lists
whose rows moved, at a revision of the crossing's own that only moves
forward. `RuntimeRoot` publishes those first, whole the first time and as
deltas after, then the tree, so a tree always follows the rows it names.
Kotlin applies the lists to the handle's state, and `HandleSnapshot.tree`
is the presentation with each reference replaced by its list's
`{"revision", "rows"}` (`KeyedListTree`), made again only when every list
it names is at the revision it names; `BridgeHandle.presentations` decodes
that, so the generated `KeyedList<Row>` reads it as Swift presented it. A
command is read with the exact reader and decoded by `ValueCoding`, so a
`JSONValue` in it keeps its digits, and a new locale or time zone has
`Runtime.environmentChanged` present the whole tree again.

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

## The shared screens in Compose

kit-ui draws the presentations SubstrateScreens makes, decoded by the
generated Kotlin, and sends the commands SwiftUI sends: the same facts,
labels, order and states, in Material's own look (docs/plans/app-kit.md
§4, §7.4). Colours, marks, the type roles, spacing and radii come from
SubstrateUI's `DesignTokens`, which SubstrateKitTests' `DesignTokensKotlinTests`
writes as `kit-ui/…/KitTokens.kt`; a tone is the same colour on both
systems, a mark an SF Symbol there and a Material Symbol here. A screen's
composable takes its presentation and a `send`; an app's own screens are
made of the same blocks (`KitSection` of `KitRow`s, `KitCard`,
`ProblemBox`), and its root's presentation is decoded through its handle
(`BridgeHandle.presentations`). The App menu (`AppMenu`) is a navigation
bar under 600 dp, a rail under 1200 dp, and a drawer of its sections
above.

The gallery is a check. `fixtures/screens.json` is `ScreenFixtures.all` as
Swift writes it (`ScreenFixturesFileTests` fails while it is stale;
`SUBSTRATE_UPDATE_FIXTURES=1` writes it again). kit-core's
`ScreenFixturesTest` decodes and encodes every fixture, which Swift reads
back; kit-ui's `KitGalleryTest` draws each with `KitGallery` under
Robolectric and fails on a screen with no composable. `mise run
apps:gallery` keeps those pictures beside SwiftUI's. The Gallery app shows
the same fixtures live: the `gallery` root (SubstrateBridgeCheck) makes
them on the device and sends them as any root's presentation, so it shows
the generated Kotlin reading what Swift wrote; `substrate-gallery://fixture/<name>`
opens one.

## Building and testing

```bash
mise run android:sdk     # once: the SDK packages, NDK r30, the Swift SDK for Android
mise run android:build   # every module, the Gallery app included, debug and release
mise run android:test    # the generator, the bridge on this host, the Kotlin tests, the round trip
mise run android:smoke   # on a booted emulator: the release build's checks, cold starts, the APK's libraries, the contract tests
mise run android:lock    # after a change to gradle/libs.versions.toml
```

`SUBSTRATE_ABIS=arm64-v8a` builds one ABI (the default is `arm64-v8a` and
`x86_64`). Swift on Android has no breakpoints, so the bridge's rules are
debugged in the host tests, where they run.
