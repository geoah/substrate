# Spotter on iOS 27: which platform features to use

> **Where this came from.** Written on 2026-10-30 in the owner's private repository, as `research/workouts/`, for
> the app then called Spotter (Substrate Workouts now); the version here is the one the
> app's second build used (the private repository's #118). It was carried into this repository on
> 2026-11-02 as written, with two changes. The links resolve inside this repository. The
> draft kind paths `mneme.example.org/training/<kind>` now read as the app-owned
> `~/training/<kind>` they became (decision record [0203](../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md)). Where the
> text names the package home it recommended, a Mneme package `mneme.example.org/training`,
> that name is left as written: the home changed later, and the reasons are in
> [decisions.md](../decisions.md). Mentions of Mneme, private repository paths or
> `apps/apple/spotter` describe the plan at the time. The package now lives in
> [`Package/`](../../package/README.md), and the app's spec is [DESIGN.md](../../DESIGN.md).

Researcher slice 04. Date: 2026-10-30.

The question: which iOS 27 platform features should Spotter use, with exact API names checked in the installed SDK. This document gives recommendations, not a survey. It covers Liquid Glass in SwiftUI, the rest timer while the phone is locked, Apple Health, keeping an in-progress session safe, the shape that lets a watch app come later, and signing through `apps/apple/release.sh`.

How to read the evidence markers:

- **[web]** Checked today on the web or in a primary artefact: Apple's documentation JSON, the Human Interface Guidelines, a WWDC session page, or Apple sample code downloaded today.
- **[sdk]** Checked today in the Xcode 27.2 SDK (iPhoneOS27.2.sdk, WatchOS27.2.sdk): the `.swiftinterface` files and the Objective-C headers.
- **[src]** Checked today in source code on this machine: the private repository's worktree, or the substrate export at `/tmp/spotter-substrate-main`.
- **[recalled]** From memory, not re-checked today.
- **[unverified]** I tried and could not confirm it, or the only source is secondary.

Every Swift snippet below passed `xcrun --sdk iphoneos swiftc -typecheck -target arm64-apple-ios27.0` with Swift 6.4.2, in both `-swift-version 5` and `-swift-version 6`. The widget file adds `-parse-as-library`. The app-side files add `-D SPOTTER_APP`, the same pattern Mneme v6 uses for its Live Activity intents (`apps/apple/mneme-v6/project.yml`, `MNEME_APP`) [src].

## Summary

1. **Liquid Glass**: use the system components and let them carry the glass: `TabView` with `Tab`, toolbars, sheets, menus. Custom glass goes on one thing only: the floating rest bar in the session screen (`GlassEffectContainer`, `glassEffect(_:in:)`, `.buttonStyle(.glass)` and `.glassProminent`). Set rows, routine cards and charts stay in the content layer with standard materials.
2. **The "workout in progress" bar** is `tabViewBottomAccessory(isEnabled:content:)` with `tabBarMinimizeBehavior(.onScrollDown)`. Hevy's bar in the owner's screenshot looks like this component; that is inferred from the screenshot alone [unverified]. Tapping it opens the session as a `fullScreenCover` with a zoom transition from the bar.
3. **Rest timer**: one Live Activity per session (countdown, next set, -15s, +15s, Skip through `LiveActivityIntent`) plus one time-sensitive local notification at each rest end. AlarmKit is an opt-in "loud rest alarm" for a phone on silent. One coordinator owns the rest state and rewrites all three on every change.
4. **Apple Health**: at Finish, write one `HKWorkout` with `HKWorkoutBuilder`, type `traditionalStrengthTraining`, indoor, with `HKMetadataKeyExternalUUID` set to the session id. Do not start an iPhone `HKWorkoutSession` in the first version. Do not write energy the phone cannot measure.
5. **Session safety**: the in-progress session is one small JSON file, rewritten atomically on every change, with a file-protection class that the Live Activity buttons can still read while the phone is locked.
6. **Watch later**: put the model, the session state machine, the suggestion logic and the substrate client in a Foundation-only Swift package now. The watch app later owns an `HKWorkoutSession` and mirrors it to the iPhone. Until then the Live Activity already appears in the watch's Smart Stack, and rest notifications reach the wrist.
7. **Signing**: HealthKit and time-sensitive notifications are entitlements; AlarmKit and Live Activities are Info.plist keys only. Substrate's release script notes that its App Store Connect key needs the Admin role to register HealthKit on a new app id [src]; Apple's documentation does not confirm this [unverified], so check the role of the key `release.sh` uses.

## 0. What this builds on

- `/tmp/spotter-substrate-main/docs/plans/health-data/02-workouts.md` §1.7 lists the HealthKit workout model and §1.9 the Hevy model. I rely on three of its points: HealthKit has no set, rep or load type; strength apps write one `traditionalStrengthTraining` or `functionalStrengthTraining` workout; the effort score is a 1 to 10 quantity linked to a workout.
- I re-checked what that document marked [recalled] and Spotter leans on:
  - `relateWorkoutEffortSample(_:with:activity:)` exists on `HKHealthStore`, iOS 18.0 [sdk].
  - `HKMetadataKeyExternalUUID`, `HKMetadataKeyWorkoutBrandName`, `HKMetadataKeyIndoorWorkout` and `HKMetadataKeyTimeZone` are iOS 8.0 [sdk].
  - `HKMetadataKeySyncIdentifier` with `HKMetadataKeySyncVersion` (iOS 11) replace an existing object that has the same identifier and a lower version. The header says this for `HKObject` in general [sdk].
- What that document lacks and this one adds: the iPhone workout session in iOS 26, Live Activities, AlarmKit, notifications, Liquid Glass, and signing.
- Two precedents on this machine:
  - Mneme v6 already ships a Live Activity with a `LiveActivityIntent` in an iOS widget extension (`apps/apple/mneme-v6/MnemeV6Widgets`, `MnemeV6/Sources/Shared/RecordingActivityShared.swift`) [src].
  - Substrate Sync already reads HealthKit workouts into substrate (`apps/apple/SubstrateSync/Sources/Sync/HealthReader.swift` in the substrate export) [src]. This matters for duplicates (§3.7).

## 1. Liquid Glass in SwiftUI

### 1.1 The rules that decide where glass goes

- Liquid Glass is a separate functional layer for controls and navigation that floats above the content layer [web, HIG Materials].
- "Don't use Liquid Glass in the content layer." Use standard materials there. The only exception is a content control with a transient interactive state, such as a slider or toggle while it is touched [web, HIG Materials].
- Use custom glass effects sparingly and only on the most important functional elements [web, HIG Materials].
- Use the `clear` variant only over visually rich backgrounds such as photos and video. Use `regular` everywhere else [web, HIG Materials].
- Avoid overcrowding or layering Liquid Glass elements on top of each other [web, Adopting Liquid Glass]. In practice: no glass control on top of another glass surface, such as the tab bar accessory or a sheet's own glass.
- Remove custom backgrounds from bars, sheets and popovers, so the system's glass and scroll edge effect work [web, Adopting Liquid Glass].
- A tab bar is for navigation, not actions [web, HIG Tab bars]. "Start workout" is therefore a button in content or a toolbar, not a tab.

### 1.2 The APIs, as declared in the 27.2 SDK

All of these were read in `SwiftUI.swiftmodule` and `SwiftUICore.swiftmodule` [sdk]. "26" means iOS 26.0 unless noted.

| API                                                                                                                              | Since    | Use in Spotter                                                                         |
| -------------------------------------------------------------------------------------------------------------------------------- | -------- | -------------------------------------------------------------------------------------- |
| `glassEffect(_ glass: Glass = .regular, in shape: some Shape = DefaultGlassEffectShape())`                                       | 26       | The rest bar's timer pill                                                              |
| `Glass.regular`, `.clear`, `.identity`, `tint(_:)`, `interactive(_:)`                                                            | 26       | `regular`, tinted for the running rest; `interactive()` on custom tappable glass       |
| `GlassEffectContainer(spacing:content:)`                                                                                         | 26       | Wraps the rest bar so its pieces render together and morph                             |
| `glassEffectID(_:in:)`                                                                                                           | 26       | Morph the rest bar between its compact and expanded forms                              |
| `glassEffectUnion(id:namespace:)`                                                                                                | 26       | Not needed                                                                             |
| `glassEffectTransition(_:)` with `.matchedGeometry`, `.materialize`, `.identity`                                                 | 26       | Default `.matchedGeometry` inside the container                                        |
| `.buttonStyle(.glass)`, `.glass(_ glass: Glass)` (the `GlassButtonStyle(_:)` init is 26.1), `.buttonStyle(.glassProminent)`      | 26       | -15, +15 (glass) and Skip, Finish (prominent)                                          |
| `Tab(_:systemImage:content:)` in `TabView`                                                                                       | 18       | The three tabs                                                                         |
| `tabBarMinimizeBehavior(_:)` with `.automatic`, `.onScrollDown`, `.onScrollUp`, `.never`                                         | 26       | `.onScrollDown`, so the accessory goes inline                                          |
| `tabViewBottomAccessory(content:)`; `tabViewBottomAccessory(isEnabled:content:)`                                                 | 26; 26.1 | The in-progress bar                                                                    |
| `EnvironmentValues.tabViewBottomAccessoryPlacement` (`.inline`, `.expanded`)                                                     | 26       | Shorter bar when inline                                                                |
| `ToolbarSpacer(_ sizing: SpacerSizing = .flexible, placement:)`                                                                  | 26       | Split toolbar items into separate glass groups                                         |
| `ToolbarContent.sharedBackgroundVisibility(_:)`                                                                                  | 26       | Not needed                                                                             |
| `scrollEdgeEffectStyle(_:for:)` with `.automatic`, `.hard`, `.soft`; `scrollEdgeEffectHidden(_:for:)`                            | 26       | `.soft` at the bottom, under the rest bar                                              |
| `safeAreaBar(edge:alignment:spacing:content:)`                                                                                   | 26       | Hosts the rest bar as a custom bar, so content scrolling under it gets the edge effect |
| `backgroundExtensionEffect()`                                                                                                    | 26       | Not needed: its main use is the detail column of a split view [web]                    |
| `matchedTransitionSource(id:in:)` and `navigationTransition(.zoom(sourceID:in:))`                                                | 18       | Accessory to full-screen session                                                       |
| `TabRole.prominent`                                                                                                              | 27       | Not used: it gives one tab "prominent visual treatment" [web]; no Spotter tab needs it |
| `toolbarMinimizationBehavior(_:for:)`, `toolbarMinimizationSafeAreaAdjustment(_:for:)`, `toolbarMinimizationRestoration(_:for:)` | 27       | Optional on the History list: the navigation bar minimizes on scroll                   |
| `ToolbarItemPlacement.topBarPinnedTrailing`                                                                                      | 27       | Optional: pin Finish so it never moves to the overflow menu                            |
| `visibilityPriority(_:)` with `.low`, `.high`; `ToolbarOverflowMenu`                                                             | 27       | Keep Finish at `.high` on small phones                                                 |
| `reorderable()` on `ForEach`, `reorderContainer(for:isEnabled:move:)`, `ReorderDifference`                                       | 27       | Drag exercises in the session list (§1.4)                                              |
| `swipeActions(...)` inside any container, with `swipeActionsContainer()`                                                         | 27       | Swipe to remove a set from a `LazyVStack`                                              |
| `navigationTransition(.crossFade)`                                                                                               | 27       | Not needed                                                                             |
| `presentationPlacement(_:)`                                                                                                      | 27       | Not needed on iPhone                                                                   |

What iOS 27 did not change: the `Glass` type and the glass modifiers have no iOS 27 additions in the 27.2 SDK [sdk]. A secondary write-up says Liquid Glass "gained a few changes here and there" in iOS 27, such as glass tint, which SwiftUI apps pick up without code; it names no new API [web, secondary]. The same write-up uses a `ReorderDifference.apply(to:)` helper; it is not in the 27.2 SDK and does not compile, so Spotter applies the difference itself (§1.4) [sdk].

### 1.3 Spotter, screen by screen

| Screen                                             | Layer                       | What to use                                                                                                                                        |
| -------------------------------------------------- | --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| Tab bar: Workout, History, Exercises               | Navigation (glass, system)  | `TabView` with three `Tab`s, `.tabBarMinimizeBehavior(.onScrollDown)`                                                                              |
| In-progress bar                                    | Navigation (glass, system)  | `tabViewBottomAccessory(isEnabled:)`, plain content inside, no `glassEffect` of its own                                                            |
| Routine list and cards                             | Content                     | `List` or cards with standard materials; "Start Routine" is `.borderedProminent`, not glass                                                        |
| "Workout in progress" conflict                     | System dialog               | `confirmationDialog(_:isPresented:titleVisibility:actions:)` anchored to the Start button: Resume, Discard and start, Cancel [sdk]                 |
| Session screen                                     | Content, with glass toolbar | `fullScreenCover` plus `navigationTransition(.zoom)`; toolbar: Minimise (leading), rest-timer default, `ToolbarSpacer`, Finish (`.glassProminent`) |
| Set rows, PREVIOUS column, warm-up badge           | Content                     | Plain rows; set type through a `Menu` (warm-up, normal, failure, drop, remove)                                                                     |
| RPE keypad                                         | Sheet (glass, system)       | `.sheet` with `.presentationDetents([.height(...)])`; no custom `presentationBackground`                                                           |
| Rest bar                                           | Controls, floating          | `safeAreaBar(edge: .bottom)` holding a `GlassEffectContainer`: -15, countdown, +15, Skip                                                           |
| Exercise menu (reorder, replace, superset, remove) | Menu (glass, system)        | `Menu` from the exercise header                                                                                                                    |
| Reorder                                            | Content                     | iOS 27 `reorderable()` in place, or a sheet with a `List` and `onMove`                                                                             |

The root view, with the accessory and the zoom into the session:

```swift
import SwiftUI

@Observable final class SessionStore {
    var active: ActiveSession?
}

struct ActiveSession: Identifiable, Codable, Hashable {
    var id: UUID
    var title: String
    var startedAt: Date
    var restEndsAt: Date?
}

struct RootView: View {
    @State private var store = SessionStore()
    @State private var showSession = false
    @Namespace private var zoom

    var body: some View {
        TabView {
            Tab("Workout", systemImage: "dumbbell") { RoutinesView() }
            Tab("History", systemImage: "clock") { Text("History") }
            Tab("Exercises", systemImage: "list.bullet") { Text("Exercises") }
        }
        .tabBarMinimizeBehavior(.onScrollDown)
        .tabViewBottomAccessory(isEnabled: store.active != nil) {
            if let session = store.active {
                InProgressBar(session: session) { showSession = true }
                    .matchedTransitionSource(id: session.id, in: zoom)
            }
        }
        .fullScreenCover(isPresented: $showSession) {
            if let session = store.active {
                Text(session.title)
                    .navigationTransition(.zoom(sourceID: session.id, in: zoom))
            }
        }
    }
}

struct InProgressBar: View {
    let session: ActiveSession
    let open: () -> Void
    @Environment(\.tabViewBottomAccessoryPlacement) private var placement

    var body: some View {
        Button(action: open) {
            HStack {
                Text(session.title).lineLimit(1)
                Spacer()
                if let end = session.restEndsAt, end > .now {
                    Text(timerInterval: Date.now...end, countsDown: true)
                        .monospacedDigit()
                } else if placement != .inline {
                    Text(session.startedAt, style: .timer).monospacedDigit()
                }
            }
            .padding(.horizontal)
        }
        .buttonStyle(.plain)
    }
}

struct RoutinesView: View {
    var body: some View {
        NavigationStack {
            List { Text("Push A") }
                .navigationTitle("Workout")
        }
    }
}
```

The session screen. The rest bar is the one piece of custom glass. It sits in a `safeAreaBar`, which registers it as a bar so the list gets the scroll edge effect beneath it. The rest state here is local only to show the layout; in the app it comes from the coordinator in §2.4.

```swift
import SwiftUI
import UIKit

struct RestState: Codable, Hashable {
    var startedAt: Date
    var endsAt: Date
}

struct RestBar: View {
    let rest: RestState
    let adjust: (TimeInterval) -> Void
    let skip: () -> Void
    @Namespace private var glass

    var body: some View {
        GlassEffectContainer(spacing: 12) {
            HStack(spacing: 12) {
                Button("-15") { adjust(-15) }
                    .buttonStyle(.glass)
                Text(timerInterval: rest.startedAt...rest.endsAt, countsDown: true)
                    .font(.title2.monospacedDigit())
                    .padding(.horizontal, 20).padding(.vertical, 10)
                    .glassEffect(.regular.tint(.orange))
                    .glassEffectID("timer", in: glass)
                Button("+15") { adjust(15) }
                    .buttonStyle(.glass)
                Button("Skip", action: skip)
                    .buttonStyle(.glassProminent)
            }
        }
    }
}

struct SessionScreen: View {
    @State private var rest: RestState?
    @State private var restFinished = 0
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        NavigationStack {
            List {
                Section("Bench Press") { Text("1  60 kg x 8") }
            }
            .scrollEdgeEffectStyle(.soft, for: .bottom)
            .safeAreaBar(edge: .bottom) {
                if let rest {
                    RestBar(rest: rest, adjust: { _ in }, skip: { self.rest = nil })
                        .padding(.bottom, 8)
                }
            }
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button("Minimise", systemImage: "chevron.down") {}
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Rest timer", systemImage: "timer") {}
                }
                ToolbarSpacer(.fixed, placement: .topBarTrailing)
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Finish") {}.buttonStyle(.glassProminent)
                }
            }
            .navigationBarTitleDisplayMode(.inline)
        }
        .sensoryFeedback(.success, trigger: restFinished)
        .onAppear { UIApplication.shared.isIdleTimerDisabled = true }
        .onDisappear { UIApplication.shared.isIdleTimerDisabled = false }
        .onChange(of: scenePhase) { _, phase in
            // Back from the lock screen: the notification already alerted,
            // so clear the expired rest without a second haptic.
            if phase == .active, let r = rest, r.endsAt <= .now { rest = nil }
        }
        .task(id: rest) {
            guard let r = rest else { return }
            try? await Task.sleep(for: .seconds(max(0, r.endsAt.timeIntervalSinceNow)))
            guard !Task.isCancelled else { return }
            rest = nil
            restFinished += 1
        }
    }
}
```

### 1.4 Reordering exercises in place (iOS 27)

Hevy opens a separate reorder screen. iOS 27 can drag rows in place in a `LazyVStack`, and swipe actions now work outside `List` [sdk]. Both shown together:

```swift
import SwiftUI

struct ExerciseEntry: Identifiable, Hashable {
    var id: UUID
    var name: String
}

struct ReorderExercises: View {
    @State private var items: [ExerciseEntry] = []

    var body: some View {
        ScrollView {
            LazyVStack(spacing: 8) {
                ForEach(items) { item in
                    Text(item.name).frame(maxWidth: .infinity, alignment: .leading).padding()
                        .swipeActions {
                            Button("Remove", role: .destructive) {
                                items.removeAll { $0.id == item.id }
                            }
                        }
                }
                .reorderable()
            }
            .swipeActionsContainer()
        }
        .reorderContainer(for: ExerciseEntry.self) { difference in
            let moving = items.filter { difference.sources.contains($0.id) }
            items.removeAll { difference.sources.contains($0.id) }
            switch difference.destination.position {
            case .before(let id):
                let index = items.firstIndex { $0.id == id } ?? items.endIndex
                items.insert(contentsOf: moving, at: index)
            case .end:
                items.append(contentsOf: moving)
            }
        }
    }
}
```

If in-place dragging competes with editing a set's fields, keep Hevy's pattern: a sheet with a `List` and `.onMove`, which has worked since iOS 13 [recalled].

## 2. The rest timer with the phone locked

### 2.1 What runs when the phone is locked

- A normal app is suspended in the background. No in-app timer fires [web, Configuring background execution modes].
- The background modes do not include anything for timers. `workout-processing` is listed for watchOS only [web, Configuring background execution modes].
- iOS 26 lets an iPhone app start its own `HKWorkoutSession` (§3.1). Apple's iPhone sample updates its Live Activity once a second from a `Timer` during a session, which suggests the app keeps running [web, sample code `WorkoutManager.swift`]. Apple's documentation does not say so, and the sample declares only `UIBackgroundModes: processing` [web]. Treat background running during an iPhone workout session as [unverified]. Spotter's design must not depend on it.

So the alert at rest end must be handed to the system ahead of time.

### 2.2 The options

| Mechanism                                                                                     | Alerts at rest end with the app suspended                  | Overrides the Ring/Silent switch | Breaks through Focus                           | Permission                                                | Move the end time                                                  | On a paired Apple Watch                       |
| --------------------------------------------------------------------------------------------- | ---------------------------------------------------------- | -------------------------------- | ---------------------------------------------- | --------------------------------------------------------- | ------------------------------------------------------------------ | --------------------------------------------- |
| Local notification, `UNTimeIntervalNotificationTrigger`, `interruptionLevel = .timeSensitive` | Yes                                                        | No [web]                         | Yes, if the person allows time-sensitive [web] | Notifications prompt, plus the time-sensitive entitlement | Replace the request with the same identifier [sdk]                 | Forwarded when the phone is locked [recalled] |
| Live Activity (ActivityKit + WidgetKit)                                                       | No: it alerts only when the app or a push updates it [web] | n/a                              | n/a                                            | None; the person can turn Live Activities off             | `Activity.update(_:)` from the app or a `LiveActivityIntent` [sdk] | Smart Stack, automatically [web]              |
| AlarmKit countdown, `AlarmManager.AlarmConfiguration.timer(...)`                              | Yes                                                        | Yes [web]                        | Yes [web]                                      | Its own prompt, `NSAlarmKitUsageDescription` [web]        | No API to move it: cancel and schedule a new one [sdk]             | The alert is forwarded [web]                  |
| Live Activity scheduled with `request(...alertConfiguration:start:)` (iOS 26)                 | Yes, at its start date [web]                               | No [recalled]                    | n/a                                            | As Live Activities                                        | Only from the foreground or a `LiveActivityIntent` [web]           | Smart Stack                                   |
| In-app timer only                                                                             | No                                                         | n/a                              | n/a                                            | None                                                      | Yes                                                                | No                                            |

What the documentation adds:

- **Time-sensitive** notifications override scheduled delivery and Focus, not the Ring/Silent switch. Critical alerts do override it, but need an entitlement Apple grants for health and safety uses [web, HIG Managing notifications]. The person can turn time-sensitive interruptions off per app [web]. `UNAuthorizationOptionTimeSensitive` is deprecated in favour of the entitlement [sdk].
- **Live Activities** last up to eight hours, then stay on the Lock Screen for up to four more [web]. The static and dynamic data together are capped at 4 KB [web]. `staleDate` marks the content outdated at a given date: `activityState` becomes `.stale` and `ActivityViewContext.isStale` becomes true, with no app code running [web, sdk]. `Text(timerInterval:pauseTime:countsDown:showsHours:)` [sdk] and `ProgressView(timerInterval:countsDown:)` [sdk] count down on their own. Buttons use `Button(_:intent:)` from the AppIntents SwiftUI overlay [sdk]; a `LiveActivityIntent` runs in the app's process, which the system launches in the background if needed [web, LiveActivityIntent]. The HIG says: alert only for essential updates, and "don't use push notifications alongside Live Activities for the same updates" [web].
- **AlarmKit** (iOS 26): "Alarms are a great fit for countdowns with a specific interval, like a cooking timer", and "not a replacement for ... critical alerts or time-sensitive notifications" [web, WWDC25 session 230]. The alert "overrides both a device's focus and silent mode" [web]. An app whose alarm has a countdown must provide a Live Activity for `AlarmAttributes<Metadata>` in its widget extension, "otherwise, the system may unexpectedly dismiss alarms and fail to alert" [web]. Without `NSAlarmKitUsageDescription` the app cannot schedule alarms [web]. `AlarmManager` has `schedule(id:configuration:)`, `countdown(id:)`, `pause(id:)`, `resume(id:)`, `stop(id:)`, `cancel(id:)`, `alarms`, `alarmUpdates`, `requestAuthorization()`; errors include `AlarmError.maximumLimitReached` [sdk]. The limit's value is not documented [unverified]. iOS 27 adds an `appEntityIdentifier:` parameter to the configuration [sdk]. AlarmKit does not exist on watchOS [sdk].

### 2.3 Recommendation

Use three layers, each doing one job:

1. **The Live Activity is the display.** Start it with the session, in the foreground. It shows the exercise, "Next: set 2 of 4, 35 kg x 15", the session clock, and during rest a countdown with -15s, +15s and Skip. Each rest update sets `staleDate` to the rest's end, so at that moment the system re-renders it as "Rest over". It never alerts by itself (no `alertConfiguration`), so the notification below is the only alert. The HIG's rule is against pairing push notifications with a Live Activity for the same update; Spotter's local notification does carry the same update, and it is kept because a Live Activity cannot alert at a future time without the app running (§2.2). Hevy's Live Activity in the owner's screenshot stays at 0:00 with the buttons still showing; this fixes that. Add `supplementalActivityFamilies([.small])` for a watch layout [sdk]. The owner's Hevy watch screenshot ("0:33", "Open on iPhone", a close button) matches what the HIG describes when someone taps a Live Activity in the Smart Stack and no watchOS app is installed: a full-screen view of the compact elements with a button to open the app on iPhone [web].
2. **A time-sensitive local notification is the alert.** One pending request with a fixed identifier, replaced on every change. No push, no server. Skip it for rests under about 10 seconds (myo-rep and drop-set pauses), where the owner is holding the phone.
3. **AlarmKit is opt-in.** A setting, "Loud rest alarm", for a phone in a pocket on silent. Off by default because: it asks for a second permission, it runs a second Live Activity beside the session one, it presents a full alarm alert rather than a banner, and every -15 or +15 means cancel and reschedule. Apple's own framing (a countdown "like a cooking timer") does fit a rest timer, so it is a fair option for the owner to choose.

When the app is in the foreground the in-app countdown and a haptic do the job. A `UNUserNotificationCenterDelegate` hides the banner while the session screen is visible.

Rejected:

- Scheduling a second Live Activity to start at rest end: it can only be requested from the foreground or an intent, it counts against the Live Activity limit, and it doubles the Dynamic Island.
- Silent audio to stay alive in the background: battery cost, and it fights the owner's music.
- Push from substrate: needs a server round trip and the `aps-environment` entitlement for something the phone can schedule itself.

### 2.4 Keeping them consistent when rest is extended or skipped

One rule: **only the rest coordinator changes a rest**. The session screen, the Live Activity's buttons, a notification action and app launch all call it. Each change runs the same steps in order:

1. Update the in-memory rest and bump a revision number.
2. Write the in-progress file (§4.1). If the app dies after this line, the file is the truth.
3. Cancel the old AlarmKit alarm, if any, before any `await`.
4. Remove the pending and delivered notification with the fixed identifier, then add the new one if the rest has a future end. `addNotificationRequest` replaces a request with the same identifier [sdk].
5. If AlarmKit is on, schedule a new alarm with an id chosen in step 1 and stored in step 2.
6. Update the Live Activity with the new state, `staleDate` = the new end, and `timestamp:` = the time of the change. ActivityKit ignores an update whose timestamp is older than one it already applied (`update(_:alertConfiguration:timestamp:)`, iOS 17.2) [web, sdk]; the revision number alone does not order Live Activity updates.

Two taps on +15s from the Lock Screen run two intents that interleave at every `await`. The revision check after each `await` makes the newest change win for the notification and the alarm, and the timestamp does it for the Live Activity; a stale run stops early and cancels any alarm it scheduled too late. -15s never moves the end before now. On every launch and every return to the foreground, `restore(_:revision:)` re-applies what the file says and cancels any alarm the file does not name. Pending notifications, scheduled alarms and Live Activities all outlive the app's process [web for alarms; recalled for notifications and Live Activities], so a restore after a crash finds them and brings them back in line.

Timed sets (a plank, a carry) use the same coordinator with a second kind of countdown. Only one countdown runs at a time.

The types shared by the app and the widget extension:

```swift
import ActivityKit
import AlarmKit
import AppIntents
import Foundation

/// Compiled into the app and the widget extension.
struct SpotterActivityAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {
        var sessionStartedAt: Date
        var exercise: String
        /// "Set 2 of 4, 35 kg x 15"
        var next: String
        var restStartedAt: Date?
        var restEndsAt: Date?
        /// Bumped on every change. ActivityKit orders updates by the
        /// `timestamp:` the coordinator passes, not by this number.
        var revision: Int
    }
    var sessionID: UUID
    var title: String
}

/// Empty: the rest alarm needs no data beyond its presentation.
struct RestAlarmMetadata: AlarmMetadata {}

struct AdjustRestIntent: LiveActivityIntent {
    static let title: LocalizedStringResource = "Adjust Rest"
    static let isDiscoverable = false
    @Parameter(title: "Seconds") var seconds: Int

    init() {}
    init(seconds: Int) { self.seconds = seconds }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if SPOTTER_APP
        await RestCoordinator.shared.adjust(by: TimeInterval(seconds))
        #endif
        return .result()
    }
}

struct SkipRestIntent: LiveActivityIntent {
    static let title: LocalizedStringResource = "Skip Rest"
    static let isDiscoverable = false

    @MainActor
    func perform() async throws -> some IntentResult {
        #if SPOTTER_APP
        await RestCoordinator.shared.skip()
        #endif
        return .result()
    }
}
```

The coordinator (app target only). `SessionJournal` stands for the in-progress file of §4.1:

```swift
import ActivityKit
import AlarmKit
import Foundation
import SwiftUI
import UserNotifications

/// The one place that changes a rest. Every caller (the session screen,
/// the Live Activity buttons, the notification action) goes through it.
@MainActor
final class RestCoordinator {
    static let shared = RestCoordinator()

    struct Rest: Codable, Hashable {
        var startedAt: Date
        var endsAt: Date
        var alarmID: UUID?
    }

    private(set) var rest: Rest?
    var useAlarm = false
    private var revision = 0
    private let notificationID = "spotter.rest"

    func start(seconds: TimeInterval, next: String) async {
        let now = Date.now
        await apply(Rest(startedAt: now, endsAt: now + seconds), next: next)
    }

    func adjust(by seconds: TimeInterval) async {
        guard var r = rest else { return }
        r.endsAt = max(Date.now, r.endsAt + seconds)
        await apply(r, next: nil)
    }

    func skip() async { await apply(nil, next: nil) }

    /// At launch and on every return to the foreground: re-apply what the
    /// journal says, so the notification, the alarm and the Live Activity
    /// match it again whatever happened while the app was not running.
    func restore(_ saved: Rest?, revision savedRevision: Int) async {
        rest = saved
        revision = max(revision, savedRevision)
        let alarms = (try? AlarmManager.shared.alarms) ?? []
        for alarm in alarms where alarm.id != saved?.alarmID {
            try? AlarmManager.shared.cancel(id: alarm.id)
        }
        await apply(saved, next: nil)
    }

    private func apply(_ requested: Rest?, next: String?) async {
        let old = rest
        var new = requested
        // The alarm id is chosen before anything is written, so the journal
        // always names the alarm that may fire.
        new?.alarmID = useAlarm && (new?.endsAt ?? .distantPast) > .now ? UUID() : nil
        rest = new
        revision += 1
        let mine = revision
        let changedAt = Date.now
        // 1. Persist first: a crash after this line still restores the truth.
        SessionJournal.shared.save(rest: new, revision: revision)
        // AlarmKit cannot move a fire date: the old alarm goes before any await.
        if let id = old?.alarmID { try? AlarmManager.shared.cancel(id: id) }
        // 2. The alert at rest end: one pending request, replaced in place.
        let center = UNUserNotificationCenter.current()
        center.removePendingNotificationRequests(withIdentifiers: [notificationID])
        center.removeDeliveredNotifications(withIdentifiers: [notificationID])
        if let r = new, r.endsAt > .now {
            let content = UNMutableNotificationContent()
            content.title = "Rest over"
            content.body = next ?? "Next set"
            content.sound = .default
            content.interruptionLevel = .timeSensitive
            let trigger = UNTimeIntervalNotificationTrigger(
                timeInterval: max(1, r.endsAt.timeIntervalSinceNow), repeats: false)
            try? await center.add(UNNotificationRequest(
                identifier: notificationID, content: content, trigger: trigger))
        }
        // Two quick taps interleave at every await: a newer change owns the rest.
        guard mine == revision else { return }
        // 3. The optional loud alarm, scheduled afresh for the new end.
        if let r = new, let id = r.alarmID {
            let attributes = AlarmAttributes<RestAlarmMetadata>(
                presentation: AlarmPresentation(
                    alert: .init(title: "Rest over"),
                    countdown: .init(title: "Rest")),
                metadata: RestAlarmMetadata(),
                tintColor: .orange)
            let config = AlarmManager.AlarmConfiguration<RestAlarmMetadata>.timer(
                duration: r.endsAt.timeIntervalSinceNow, attributes: attributes)
            _ = try? await AlarmManager.shared.schedule(id: id, configuration: config)
            // A newer change may have cancelled this id before it existed.
            if mine != revision { try? AlarmManager.shared.cancel(id: id) }
        }
        guard mine == revision else { return }
        // 4. The Live Activity: staleDate flips it to "rest over" with no app code running.
        for activity in Activity<SpotterActivityAttributes>.activities {
            var state = activity.content.state
            state.restStartedAt = new?.startedAt
            state.restEndsAt = new?.endsAt
            if let next { state.next = next }
            state.revision = revision
            // The timestamp makes ActivityKit drop an older update that lands late.
            await activity.update(ActivityContent(state: state, staleDate: new?.endsAt),
                                  alertConfiguration: nil, timestamp: changedAt)
        }
    }
}

@MainActor
final class SessionJournal {
    static let shared = SessionJournal()
    func save(rest: RestCoordinator.Rest?, revision: Int) {}
}
```

The widget extension: the session's Live Activity, and the AlarmKit countdown view that AlarmKit requires when the opt-in alarm is on:

```swift
import ActivityKit
import AlarmKit
import AppIntents
import SwiftUI
import WidgetKit

@main
struct SpotterWidgets: WidgetBundle {
    var body: some Widget {
        SessionLiveActivity()
        RestAlarmLiveActivity()
    }
}

struct SessionLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: SpotterActivityAttributes.self) { context in
            LockScreenView(state: context.state, isStale: context.isStale)
                .padding()
        } dynamicIsland: { context in
            DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Text(context.state.exercise).font(.headline)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    RestCountdown(state: context.state, isStale: context.isStale)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(alignment: .leading) {
                        Text("Next: \(context.state.next)")
                        if context.state.restEndsAt != nil, !context.isStale {
                            RestButtons()
                        }
                    }
                }
            } compactLeading: {
                Image(systemName: "dumbbell.fill")
            } compactTrailing: {
                RestCountdown(state: context.state, isStale: context.isStale)
                    .frame(maxWidth: 52)
            } minimal: {
                RestCountdown(state: context.state, isStale: context.isStale)
            }
        }
        .supplementalActivityFamilies([.small])
    }
}

struct RestCountdown: View {
    let state: SpotterActivityAttributes.ContentState
    let isStale: Bool

    var body: some View {
        if let start = state.restStartedAt, let end = state.restEndsAt, !isStale, end > .now {
            Text(timerInterval: start...end, countsDown: true)
                .monospacedDigit()
        } else if state.restEndsAt != nil {
            Text("Go")
        } else {
            Text(timerInterval: state.sessionStartedAt...Date.distantFuture, countsDown: false)
                .monospacedDigit()
        }
    }
}

struct RestButtons: View {
    var body: some View {
        HStack {
            Button("-15s", intent: AdjustRestIntent(seconds: -15))
            Button("+15s", intent: AdjustRestIntent(seconds: 15))
            Button("Skip", intent: SkipRestIntent())
        }
    }
}

struct LockScreenView: View {
    let state: SpotterActivityAttributes.ContentState
    let isStale: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(state.exercise).font(.headline)
            Text("Next: \(state.next)").font(.subheadline)
            if let start = state.restStartedAt, let end = state.restEndsAt, !isStale {
                ProgressView(timerInterval: start...end, countsDown: true)
                RestButtons()
            } else if state.restEndsAt != nil {
                Text("Rest over").font(.title3.bold())
            }
        }
    }
}

/// AlarmKit needs this when its alarms have a countdown; without it the
/// system may dismiss the alarm and never alert.
struct RestAlarmLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: AlarmAttributes<RestAlarmMetadata>.self) { context in
            switch context.state.mode {
            case .countdown(let c):
                Text(timerInterval: c.startDate...c.fireDate, countsDown: true).monospacedDigit()
            case .paused:
                Text("Paused")
            case .alert:
                Text("Rest over")
            @unknown default:
                EmptyView()
            }
        } dynamicIsland: { context in
            DynamicIsland {
                DynamicIslandExpandedRegion(.center) { Text("Rest") }
            } compactLeading: {
                Image(systemName: "timer")
            } compactTrailing: {
                Text("Rest")
            } minimal: {
                Image(systemName: "timer")
            }
        }
    }
}
```

Starting, ending, and the foreground banner rule:

```swift
import ActivityKit
import Foundation
import UserNotifications

@MainActor
enum SessionActivity {
    /// Called once, in the foreground, when a session starts or is restored.
    static func ensureStarted(sessionID: UUID, title: String, state: SpotterActivityAttributes.ContentState) {
        guard ActivityAuthorizationInfo().areActivitiesEnabled else { return }
        if Activity<SpotterActivityAttributes>.activities.contains(where: { $0.attributes.sessionID == sessionID }) { return }
        _ = try? Activity.request(
            attributes: SpotterActivityAttributes(sessionID: sessionID, title: title),
            content: ActivityContent(state: state, staleDate: state.restEndsAt),
            pushType: nil)
    }

    /// Finish keeps a summary for a few minutes; Discard removes it at once.
    static func end(finished: Bool) async {
        for activity in Activity<SpotterActivityAttributes>.activities {
            await activity.end(nil, dismissalPolicy: finished ? .after(.now + 5 * 60) : .immediate)
        }
        UNUserNotificationCenter.current().removePendingNotificationRequests(withIdentifiers: ["spotter.rest"])
    }
}

/// Suppress the banner while the session screen is on screen: the app's own
/// haptic and countdown already say it.
final class NotificationGate: NSObject, UNUserNotificationCenterDelegate {
    @MainActor static var sessionScreenVisible = false

    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        await MainActor.run { Self.sessionScreenVisible } ? [] : [.banner, .sound]
    }
}
```

Superset rests: the coordinator receives an already-resolved duration. The core package decides it: the short rest inside a superset round, or the longer rest after the round's last exercise. A zero-second intra-superset rest starts no countdown; only "Next" changes.

## 3. Apple Health

### 3.1 HKWorkoutBuilder or an iPhone workout session

What the SDK says:

- `HKWorkoutSession` has been on iOS since 17.0, but only as the mirror of a session started on Apple Watch [sdk].
- iOS 26.0 added what an iPhone needs to run its own session: `HKWorkoutSession.init(healthStore:configuration:)`, `associatedWorkoutBuilder()`, `HKLiveWorkoutBuilder`, `HKLiveWorkoutDataSource` and `HKHealthStore.recoverActiveWorkoutSession()` [sdk]. HealthKitUI adds `UIScene.ConnectionOptions.shouldHandleActiveWorkoutRecovery` (iOS 26) for crash recovery [sdk].
- iOS 27 adds workout zones (`HKLiveWorkoutZoneUpdate`, `zoneGroupsByType`) [sdk], which strength sessions do not need.

What the documentation and WWDC25 session 322 add [web]:

- iPhone and iPad have no heart rate sensor; a session collects heart rate only from a paired Bluetooth heart rate strap or similar.
- The phone "will most likely lock while a workout is running". The first session shows a prompt that workout data will be available to the app while locked.
- The session should `prepare()` and show a three-second countdown before `startActivity(with:)`.
- Siri can start, pause, resume and end a workout from the Lock Screen through the `INStartWorkoutIntent` family.

|                    | `HKWorkoutBuilder` at Finish            | `HKWorkoutSession` + `HKLiveWorkoutBuilder` on iPhone                                  |
| ------------------ | --------------------------------------- | -------------------------------------------------------------------------------------- |
| Available          | iOS 12 [sdk]                            | iOS 26 [sdk]                                                                           |
| When it writes     | Once, with explicit start and end dates | Live, from start to finish                                                             |
| Heart rate         | None of its own                         | From a paired strap                                                                    |
| Energy             | None unless the app writes it           | System-generated where it can [web]                                                    |
| Crash recovery     | Not needed: nothing is open             | `recoverActiveWorkoutSession()`                                                        |
| Background runtime | No                                      | [unverified] (§2.1)                                                                    |
| Conflicts          | None                                    | Must be ended or discarded on every path; the watch app later wants to own the session |

**Recommendation**: write with `HKWorkoutBuilder` at Finish in the first version. It matches the owner's ask (one workout, no per-exercise detail). It has no open state to lose, and it leaves the live session to the watch app later (§5). Revisit the iPhone session only if the owner trains with a chest strap and wants heart rate in Health.

### 3.2 Activity type

- `traditionalStrengthTraining`: "Primarily machines and/or free weights" [sdk header].
- `functionalStrengthTraining`: "Primarily free weights and/or body weight and/or accessories" [sdk header].

Default to `traditionalStrengthTraining`: the owner's routines in the screenshots are machines and barbells. Let a routine override it (functional for kettlebell or bodyweight circuits; `coreTraining` or `highIntensityIntervalTraining` exist too). Health Connect's `STRENGTH_TRAINING` versus `WEIGHTLIFTING` split is in 02-workouts.md §4.5.

### 3.3 Metadata

| Key                                                        | Value                         | Why                                                                                                                                                                                             |
| ---------------------------------------------------------- | ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `HKMetadataKeyExternalUUID`                                | The substrate session id      | Links the Health copy back to the session; HealthKit does not enforce uniqueness [sdk]                                                                                                          |
| `HKMetadataKeySyncIdentifier` + `HKMetadataKeySyncVersion` | Session id + session revision | A re-export after an edit replaces the old workout instead of adding one. Documented for `HKObject` [sdk]; whether it replaces an `HKWorkout` from a builder needs one device test [unverified] |
| `HKMetadataKeyIndoorWorkout`                               | `true`                        | Gym                                                                                                                                                                                             |
| `HKMetadataKeyTimeZone`                                    | `TimeZone.current.identifier` | HealthKit dates carry no zone (02-workouts.md §4.1); Substrate Sync already reads this key [src]                                                                                                |
| `HKMetadataKeyWorkoutBrandName`                            | The routine's name            | "The brand name of a particular workout" [sdk]; where Fitness shows it is [unverified]                                                                                                          |

### 3.4 Energy, heart rate and effort

- **Energy**: write none. The phone measures nothing, and a MET guess would show up in Activity totals as if it were measured. Hevy and Strong do write energy (02-workouts.md §1.7, not re-checked).
- **Heart rate**: none from Spotter. If the owner wears an Apple Watch, its background heart rate samples cover the session's span anyway, and Substrate Sync reads heart rate over a workout's span from every source [src, `HealthReader.summary`].
- **Effort**: ask for a session RPE at Finish (CR-10, the scale 02-workouts.md §5 names for sRPE). Write it as a `workoutEffortScore` sample (`appleEffortScore` unit, iOS 18) and link it with `relateWorkoutEffortSample(_:with:activity:)` [sdk]. This puts the owner's own rating on the workout in Fitness [recalled].

The export, called once at Finish:

```swift
import HealthKit

struct FinishedSession {
    let id: UUID
    let routineName: String?
    let start: Date
    let end: Date
    let revision: Int
    /// Session RPE on the CR-10 scale, asked at Finish.
    let sessionRPE: Double?
}

enum HealthExport {
    static let store = HKHealthStore()

    static let shareTypes: Set<HKSampleType> = [
        HKObjectType.workoutType(),
        HKQuantityType(.workoutEffortScore),
    ]

    static func requestAccess() async throws {
        try await store.requestAuthorization(toShare: shareTypes, read: [])
    }

    /// Writes one strength workout. Returns nil when Health is unavailable
    /// or the owner said no; the session stays in substrate either way.
    static func save(_ s: FinishedSession) async throws -> HKWorkout? {
        guard HKHealthStore.isHealthDataAvailable(),
              store.authorizationStatus(for: .workoutType()) == .sharingAuthorized
        else { return nil }
        let config = HKWorkoutConfiguration()
        config.activityType = .traditionalStrengthTraining
        config.locationType = .indoor
        let builder = HKWorkoutBuilder(healthStore: store, configuration: config, device: .local())
        try await builder.beginCollection(at: s.start)
        var metadata: [String: Any] = [
            HKMetadataKeyExternalUUID: s.id.uuidString,
            HKMetadataKeySyncIdentifier: s.id.uuidString,
            HKMetadataKeySyncVersion: s.revision,
            HKMetadataKeyIndoorWorkout: true,
            HKMetadataKeyTimeZone: TimeZone.current.identifier,
        ]
        if let name = s.routineName { metadata[HKMetadataKeyWorkoutBrandName] = name }
        try await builder.addMetadata(metadata)
        try await builder.endCollection(at: s.end)
        guard let workout = try await builder.finishWorkout() else { return nil }
        if let rpe = s.sessionRPE,
           store.authorizationStatus(for: HKQuantityType(.workoutEffortScore)) == .sharingAuthorized {
            let effort = HKQuantitySample(
                type: HKQuantityType(.workoutEffortScore),
                quantity: HKQuantity(unit: .appleEffortScore(), doubleValue: rpe),
                start: s.start, end: s.end)
            try await store.save(effort)
            try await store.relateWorkoutEffortSample(effort, with: workout, activity: nil)
        }
        return workout
    }
}
```

### 3.5 Entitlements and Info.plist

- Entitlements: `com.apple.developer.healthkit` = true [web], and `com.apple.developer.healthkit.access` = an empty array, since Spotter needs none of the extra-sensitive types that array unlocks [web]. Substrate Sync's entitlements file has these two plus background delivery and a Keychain access group [src]. Spotter needs no background delivery.
- Info.plist: `NSHealthUpdateUsageDescription` (writing). `NSHealthShareUsageDescription` only if Spotter reads workouts to find an Apple Watch workout covering the same span (§3.7).
- Ask in context, at the first Finish, with `requestAuthorization(toShare:read:)` or HealthKitUI's `healthDataAccessRequest(store:shareTypes:readTypes:trigger:completion:)` [sdk].

### 3.6 When the owner says no

- Write denied: `authorizationStatus(for:)` returns `.sharingDenied`, and the export returns nil without trying [sdk]. The session is already in substrate; the summary shows one line saying it was not written to Health, with a retry once access is given in Settings.
- Read denied: HealthKit does not tell the app; reads come back empty [recalled]. The overlap check in §3.7 then finds nothing and Spotter writes its own workout.
- Health unavailable (`isHealthDataAvailable()` false): skip silently [sdk].
- Never block Finish on Health.

### 3.7 Duplicates

- **Apple Watch's own Strength workout**: if the owner starts one on the watch, Health gets two overlapping workouts. At Finish, with read access, query workouts that overlap the session; if one from Apple Watch covers most of it, store its UUID on the session and do not write a second one. This is an open question for the owner.
- **Substrate Sync re-imports Spotter's workout**: Substrate Sync reads every `HKWorkout` into substrate keyed by the HealthKit UUID [src], so the session appears twice in substrate. Store the returned `HKWorkout.uuid` on the session record. Substrate Sync does not read `HKMetadataKeyExternalUUID` or the source bundle today [src]; teaching it to would let substrate link the two.

### 3.8 If an iPhone session is added later

The phase-2 shape, typechecked against the iOS 26 APIs:

```swift
import HealthKit
import HealthKitUI
import SwiftUI
import UIKit

/// Phase 2 only: a live session on iPhone (iOS 26+), for a chest strap's
/// heart rate and HealthKit's crash recovery.
@MainActor
final class LiveWorkout {
    let store = HKHealthStore()
    private(set) var session: HKWorkoutSession?
    private(set) var builder: HKLiveWorkoutBuilder?

    func start(at date: Date) async throws {
        let config = HKWorkoutConfiguration()
        config.activityType = .traditionalStrengthTraining
        config.locationType = .indoor
        let session = try HKWorkoutSession(healthStore: store, configuration: config)
        let builder = session.associatedWorkoutBuilder()
        builder.dataSource = HKLiveWorkoutDataSource(healthStore: store, workoutConfiguration: config)
        session.prepare()
        session.startActivity(with: date)
        try await builder.beginCollection(at: date)
        self.session = session
        self.builder = builder
    }

    func adopt(_ recovered: HKWorkoutSession) {
        let builder = recovered.associatedWorkoutBuilder()
        builder.dataSource = HKLiveWorkoutDataSource(
            healthStore: store, workoutConfiguration: recovered.workoutConfiguration)
        session = recovered
        self.builder = builder
    }

    func discard() {
        session?.end()
        builder?.discardWorkout()
    }
}

final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(_ application: UIApplication,
                     configurationForConnecting connectingSceneSession: UISceneSession,
                     options: UIScene.ConnectionOptions) -> UISceneConfiguration {
        if options.shouldHandleActiveWorkoutRecovery {
            Task { @MainActor in
                if let recovered = try? await HKHealthStore().recoverActiveWorkoutSession() {
                    Live.shared.adopt(recovered)
                }
            }
        }
        return UISceneConfiguration(name: nil, sessionRole: connectingSceneSession.role)
    }
}

@MainActor enum Live { static let shared = LiveWorkout() }
```

## 4. Keeping a session safe

### 4.1 Persist every change

- Write the whole in-progress session to one JSON file on every change: a set logged, a weight edited, a rest started. It is a few kilobytes, so rewriting it whole is cheap, and `.atomic` means a crash never leaves half a file [recalled].
- Use `.completeFileProtectionUntilFirstUserAuthentication`, not `.complete`. The Live Activity's buttons run the app's code while the phone is locked, and with `.complete` the file is unreadable then [sdk, Foundation `NSData.h` and `NSFileManager.h`].
- Keep the session as an event log with ids plus the folded state. The log is what goes to substrate, and later what goes between the watch and the phone (§5).
- Writes to substrate go through an outbox that retries, keyed by the session id, so a retry never creates a second session.
- At Finish, wrap the upload in `UIApplication.beginBackgroundTask(withName:expirationHandler:)` so a quick lock does not cut it off; the outbox covers anything left [recalled].

```swift
import Foundation

struct InProgressSession: Codable {
    var id: UUID
    var revision: Int
    var startedAt: Date
    var restEndsAt: Date?
}

/// One file, rewritten whole on every change. Small (kilobytes), atomic,
/// and readable while the phone is locked, where the Live Activity's
/// buttons run.
@MainActor
final class InProgressFile {
    private let url = URL.applicationSupportDirectory.appending(path: "in-progress-session.json")

    func save(_ session: InProgressSession) throws {
        try FileManager.default.createDirectory(
            at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        let data = try JSONEncoder().encode(session)
        try data.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }

    func load() -> InProgressSession? {
        guard let data = try? Data(contentsOf: url) else { return nil }
        return try? JSONDecoder().decode(InProgressSession.self, from: data)
    }

    func clear() { try? FileManager.default.removeItem(at: url) }
}
```

### 4.2 Relaunch

At launch, before the first frame: read the file. If a session is open, show the in-progress bar, call `RestCoordinator.restore(_:revision:)` (§2.4), and make sure a Live Activity exists for the session (`SessionActivity.ensureStarted`). If the owner dismissed the Live Activity from the Lock Screen, `activityStateUpdates` reports `.dismissed` [sdk]; respect that for the rest of the session instead of recreating it.

### 4.3 scenePhase

- `.background`: nothing extra. The file is already written, and the alerts are already handed to the system.
- `.active`: call `restore`. Clear an expired rest without a second haptic, since the notification already alerted. Re-read notification settings (`timeSensitiveSetting`, iOS 15) [sdk], so the session screen can say when alerts are off.

### 4.4 Screen, haptics, connectivity

- Keep the screen awake with `UIApplication.shared.isIdleTimerDisabled = true` while the session screen is visible, and set it back on disappear [sdk]. Make it a setting; it costs battery.
- At rest end in the foreground, `sensoryFeedback(_:trigger:)` with `.success` or a heavy `.impact` [sdk for the modifier]. Do not play a sound by default: the owner may be listening to music.
- Show a quiet "Offline" mark when the network is gone (`NWPathMonitor`), as Hevy's "Disconnected" banner does. Logging never waits for the network [recalled API].

## 5. Apple Watch later: choices to make now

Free today, with no watch app:

- The Live Activity appears at the top of the watch's Smart Stack. With `supplementalActivityFamilies([.small])` Spotter can give it its own layout with buttons [web, HIG Live Activities; sdk].
- Rest notifications reach the wrist when the phone is locked [recalled].
- The opt-in AlarmKit alert is forwarded to the paired watch [web].

Choices to make now, so a watch app is additive later:

1. **A Foundation-only Swift package** (`SpotterKit`) with the model, the session event log and its fold, rest rules, suggestions, unit handling and the substrate client with its outbox. No UIKit, ActivityKit, AlarmKit or HealthKit in it: ActivityKit and AlarmKit do not exist on watchOS [sdk]. Platform services (alerts, Health export, Live Activity) sit behind small protocols in each app target. The manifest below resolves with `swift package describe` (tools 6.2):

```swift
// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "SpotterKit",
    platforms: [.iOS("27.0"), .watchOS("27.0"), .macOS("27.0")],
    products: [
        .library(name: "SpotterCore", targets: ["SpotterCore"]),
        .library(name: "SpotterSubstrate", targets: ["SpotterSubstrate"]),
    ],
    targets: [
        // Model, session state machine, suggestions, rest maths. Foundation only.
        .target(name: "SpotterCore"),
        // The substrate client and the offline outbox. Foundation only.
        .target(name: "SpotterSubstrate", dependencies: ["SpotterCore"]),
        .testTarget(name: "SpotterCoreTests", dependencies: ["SpotterCore"]),
    ]
)
```

2. **One owner per session.** Whichever device starts the session writes it to substrate and to Health. When the watch app exists, it starts the `HKWorkoutSession` (heart rate, energy, and ring credit [recalled]) and calls `startMirroringToCompanionDevice()` (watchOS 10); the iPhone receives it in `workoutSessionMirroringStartHandler` (iOS 17), which launches the iPhone app in the background if needed [sdk, web]. The iPhone then must not write its own `HKWorkout`.
3. **Sync channel during a session**: `sendToRemoteWorkoutSession(data:)` on the mirrored pair, in both directions [web, sdk]. It exists for exactly this, works while the session runs, and carries the same events the file holds. The header caps it at 100 KB in any 10-second window and returns an error beyond that [sdk, `HKWorkoutSession.h`]; set events are far smaller.

| Channel                                                                                        | When                           | Fit                                                                                  |
| ---------------------------------------------------------------------------------------------- | ------------------------------ | ------------------------------------------------------------------------------------ |
| `HKWorkoutSession` mirroring + `sendToRemoteWorkoutSession(data:)`                             | During a watch-started session | Best: built for live workout state, wakes the phone                                  |
| WatchConnectivity (`WCSession`: `sendMessage`, `transferUserInfo`, `updateApplicationContext`) | Outside a session              | Routines cache pushed to the watch; queued delivery [recalled]                       |
| Both sides read and write substrate over the network                                           | Always                         | History and routines; not live set-by-set state, because a gym often has poor signal |

4. **Events, not snapshots.** Each set change is an event with an id and the device that made it, so two devices can merge without guessing.

## 6. Signing and release through apps/apple/release.sh

What `release.sh` does today [src]: `xcodegen generate`, then `xcodebuild ... archive -allowProvisioningUpdates` with `-authenticationKeyPath`, `-authenticationKeyID` and `-authenticationKeyIssuerID` from the owner's secret store. It exports with `method` = `debugging` (development signing) and `signingStyle` = `automatic`. It passes `MARKETING_VERSION` and `CURRENT_PROJECT_VERSION` on the command line, and checks `CFBundleVersion` in the main app's `Info.plist` only (`find ... -maxdepth 2`).

| Item                                                                                                    | Target           | Kind        | Needed for                                  |
| ------------------------------------------------------------------------------------------------------- | ---------------- | ----------- | ------------------------------------------- |
| `com.apple.developer.healthkit` = true                                                                  | App              | Entitlement | Writing the workout                         |
| `com.apple.developer.healthkit.access` = []                                                             | App              | Entitlement | Added with the HealthKit capability         |
| `com.apple.developer.usernotifications.time-sensitive` = true                                           | App              | Entitlement | `.timeSensitive` notifications              |
| `NSHealthUpdateUsageDescription` (+ `NSHealthShareUsageDescription` if reading)                         | App              | Info.plist  | Health prompt                               |
| `NSSupportsLiveActivities` = YES                                                                        | App              | Info.plist  | Live Activities [web]                       |
| `NSAlarmKitUsageDescription`                                                                            | App              | Info.plist  | Only if the opt-in alarm ships [web]        |
| `NSExtension` / `NSExtensionPointIdentifier` = `com.apple.widgetkit-extension`                          | Widget extension | Info.plist  | The Live Activity UI                        |
| `CFBundleVersion` = `$(CURRENT_PROJECT_VERSION)`, `CFBundleShortVersionString` = `$(MARKETING_VERSION)` | Both             | Info.plist  | Release stamping                            |
| `SpotterVersion`, `SpotterChannel`, `SpotterCommit`, `SpotterUpdates`, `SUPublicEDKey`                  | App              | Info.plist  | The shared updater (`apps/apple/README.md`) |

Points to check:

- **HealthKit on a new app id**: the substrate release script notes that the App Store Connect key "needs the Admin role to register an app id's capabilities (HealthKit)" [src, `/tmp/spotter-substrate-main/.mise/apple.sh`]. Confirm the role of the owner's key before the first release, or the archive fails at provisioning.
- **Time-sensitive capability**: it is not among the App Store Connect API's public `CapabilityType` values [web], and Apple's entitlement index has no page for it [web]. Xcode's automatic signing enables it on the app id [recalled]; whether it does so with the API key and no Apple ID signed in is [unverified]. Run `APPS_DRY_RUN=1 mise run apple:release apps/apple/spotter` once to find out. If it fails, ship with `.active` notifications and add the capability once by hand in the developer portal.
- **AlarmKit and ActivityKit** need no entitlement [web: neither appears in Apple's entitlement index]. No `aps-environment`: Spotter sends no pushes.
- **The widget extension** gets its own bundle id (`org.example.spotter.widgets`), registered by `-allowProvisioningUpdates` like the app's. Substrate releases an app with an embedded extension through the same flags today [src, `SubstrateShare` in `apps/apple/project.yml`]. Its version keys must use the same two variables as the app: Xcode warns when an extension's version differs from its parent's, and Apple's developer support says the two must match [web, Apple Developer Forums thread 730234]; whether a mismatch blocks a development install is [unverified]. `release.sh` does not check the extension's plist, so a hardcoded value there would go unnoticed.
- **No App Group needed.** The widget reads everything from the Live Activity's content state, and its buttons run in the app's process. This saves registering a group.
- **Development signing**: all of the above work in a development profile; the installing device must be registered and in Developer Mode (`apps/apple/README.md`) [src]. A watch app later needs the watch registered too [recalled].
- **release.env**: `PLATFORMS="iOS"`, `MIN_IOS=27.0`, `BUNDLE_ID=org.example.spotter`, and an entry in `apps/apple/apps.json`, as `AGENTS.md` requires [src].

## Design implications for Spotter

- Two targets from day one: the app and an iOS widget extension (`SpotterWidgets`) holding the session Live Activity and the AlarmKit countdown view. Share `SpotterActivityAttributes` and the intents through a folder compiled into both, as Mneme v6 does.
- A `SpotterKit` package, Foundation only, holds everything a watch app would also need. The app targets hold only platform glue.
- A single `RestCoordinator` (later `CountdownCoordinator`, for timed sets too) is the only writer of rest state. It writes the file, the notification, the optional alarm and the Live Activity in a fixed order, with a revision guard and timestamped Live Activity updates.
- The Live Activity's content state carries what the Lock Screen shows: exercise, next set label, session start, rest start and end, revision. Keep it far under 4 KB. Use `staleDate` = rest end for the "Rest over" state.
- Rest-end notification: fixed identifier, `.timeSensitive`, no notification for rests under about 10 seconds, banner hidden while the session screen is visible.
- AlarmKit behind a setting, off by default.
- Health: `HKWorkoutBuilder` at Finish, `traditionalStrengthTraining` by default with a per-routine override, indoor, external UUID = session id, sync identifier = session id with the revision as version, no energy, optional session RPE as the effort score. Store the returned workout UUID on the session record.
- The data model needs: a session revision, a per-session `healthWorkoutUUID`, a per-routine Health activity type, and a session RPE. These are inputs to the kind design.
- The in-progress file uses `.completeFileProtectionUntilFirstUserAuthentication`.
- UI: three tabs; the in-progress bar as `tabViewBottomAccessory`; the session as a full-screen cover zoomed from the bar; glass only on system bars and the rest bar.
- To test on a device before relying on them: whether an `HKWorkout` saved with a sync identifier is replaced on re-export; whether the time-sensitive capability provisions through the API key; how the Live Activity's buttons behave in the watch Smart Stack; and whether a Spotter workout earns Exercise ring minutes.

## Open questions for the owner

1. Do you wear an Apple Watch at the gym, and do you start Apple's Strength workout on it? If yes, Spotter should link to that workout instead of writing its own.
2. Is your phone usually on silent at the gym? If yes, do you want the "loud rest alarm" (AlarmKit) to be on by default rather than opt-in?
3. Do you use a chest strap? Only then is an iPhone `HKWorkoutSession` worth adding for heart rate.
4. Should Spotter write any energy estimate to Health, knowing the phone measures none?
5. Should Spotter ask for a session RPE at Finish to fill Apple's effort score?
6. Keep the screen awake during a session by default, accepting the battery cost?
7. Should a timed set complete itself when its countdown ends, or wait for a tap?
8. Should Spotter reuse Substrate Sync's sign-in (a shared Keychain access group, as Substrate Sync and its share extension already do), or sign in on its own?

## Sources

Installed SDK (Xcode 27.2, build 27B52), read today:

- `iPhoneOS27.2.sdk/System/Library/Frameworks/SwiftUI.framework/Modules/SwiftUI.swiftmodule/arm64e-apple-ios.swiftinterface`
- `iPhoneOS27.2.sdk/System/Library/Frameworks/SwiftUICore.framework/Modules/SwiftUICore.swiftmodule/arm64e-apple-ios.swiftinterface`
- `iPhoneOS27.2.sdk/System/Library/Frameworks/ActivityKit.framework/Modules/ActivityKit.swiftmodule/arm64e-apple-ios.swiftinterface`
- `iPhoneOS27.2.sdk/System/Library/Frameworks/AlarmKit.framework/Modules/AlarmKit.swiftmodule/arm64e-apple-ios.swiftinterface`
- `iPhoneOS27.2.sdk/System/Library/Frameworks/WidgetKit.framework/Modules/WidgetKit.swiftmodule/arm64e-apple-ios.swiftinterface`
- `iPhoneOS27.2.sdk/System/Library/Frameworks/AppIntents.framework/Modules/AppIntents.swiftmodule/arm64e-apple-ios.swiftinterface` and `_AppIntents_SwiftUI.framework`, `_AppIntents_HealthKit.framework`
- `iPhoneOS27.2.sdk/System/Library/Frameworks/HealthKit.framework/Headers/` (`HKWorkoutSession.h`, `HKLiveWorkoutBuilder.h`, `HKLiveWorkoutDataSource.h`, `HKHealthStore.h`, `HKMetadata.h`, `HKWorkoutActivityType.h`, `HKTypeIdentifiers.h`, `HKUnit.h`) and `HealthKitUI.swiftmodule`
- `iPhoneOS27.2.sdk/System/Library/Frameworks/UserNotifications.framework/Headers/` (`UNNotificationContent.h`, `UNUserNotificationCenter.h`, `UNNotificationSettings.h`)
- `iPhoneOS27.2.sdk/System/Library/Frameworks/Foundation.framework/Headers/` (`NSData.h`, `NSFileManager.h`)
- `WatchOS27.2.sdk/System/Library/Frameworks/` (framework list, `HKWorkoutSession.h`, `HKHealthStore.h`)

Apple documentation and guidelines, read today:

- [HIG: Materials](https://developer.apple.com/design/human-interface-guidelines/materials)
- [HIG: Tab bars](https://developer.apple.com/design/human-interface-guidelines/tab-bars)
- [HIG: Live Activities](https://developer.apple.com/design/human-interface-guidelines/live-activities)
- [HIG: Managing notifications](https://developer.apple.com/design/human-interface-guidelines/managing-notifications)
- [HIG: Workouts](https://developer.apple.com/design/human-interface-guidelines/workouts)
- [Adopting Liquid Glass](https://developer.apple.com/documentation/technologyoverviews/adopting-liquid-glass)
- [Applying Liquid Glass to custom views](https://developer.apple.com/documentation/swiftui/applying-liquid-glass-to-custom-views)
- [tabViewBottomAccessory(content:)](https://developer.apple.com/documentation/swiftui/view/tabviewbottomaccessory(content:))
- [tabBarMinimizeBehavior(_:)](https://developer.apple.com/documentation/swiftui/view/tabbarminimizebehavior(_:))
- [toolbarMinimizationBehavior(_:for:)](https://developer.apple.com/documentation/swiftui/view/toolbarminimizationbehavior(_:for:))
- [TabRole.prominent](https://developer.apple.com/documentation/swiftui/tabrole/prominent)
- [backgroundExtensionEffect()](https://developer.apple.com/documentation/swiftui/view/backgroundextensioneffect())
- [Displaying live data with Live Activities](https://developer.apple.com/documentation/activitykit/displaying-live-data-with-live-activities)
- [Activity.request(attributes:content:pushType:style:alertConfiguration:start:)](https://developer.apple.com/documentation/activitykit/activity/request(attributes:content:pushtype:style:alertconfiguration:start:))
- [Activity.update(_:alertConfiguration:timestamp:)](https://developer.apple.com/documentation/activitykit/activity/update(_:alertconfiguration:timestamp:))
- [LiveActivityIntent](https://developer.apple.com/documentation/appintents/liveactivityintent)
- [UNNotificationInterruptionLevel.timeSensitive](https://developer.apple.com/documentation/usernotifications/unnotificationinterruptionlevel/timesensitive)
- [AlarmKit](https://developer.apple.com/documentation/alarmkit)
- [Scheduling an alarm with AlarmKit](https://developer.apple.com/documentation/alarmkit/scheduling-an-alarm-with-alarmkit)
- [AlarmManager](https://developer.apple.com/documentation/alarmkit/alarmmanager)
- [NSAlarmKitUsageDescription](https://developer.apple.com/documentation/bundleresources/information-property-list/nsalarmkitusagedescription)
- [WWDC25 session 230: AlarmKit](https://developer.apple.com/videos/play/wwdc2025/230/)
- [WWDC25 session 322: Track workouts with HealthKit on iOS and iPadOS](https://developer.apple.com/videos/play/wwdc2025/322/)
- [Building a workout app for iPhone and iPad](https://developer.apple.com/documentation/healthkit/building-a-workout-app-for-iphone-and-ipad) (sample code downloaded and read: `Info.plist`, entitlements, `WorkoutManager.swift`, the app delegate)
- [Running workout sessions](https://developer.apple.com/documentation/healthkit/running-workout-sessions)
- [HKWorkoutSession](https://developer.apple.com/documentation/healthkit/hkworkoutsession)
- [HKWorkoutSession.init(healthStore:configuration:)](https://developer.apple.com/documentation/healthkit/hkworkoutsession/init(healthstore:configuration:))
- [recoverActiveWorkoutSession(completion:)](https://developer.apple.com/documentation/healthkit/hkhealthstore/recoveractiveworkoutsession(completion:))
- [workoutSessionMirroringStartHandler](https://developer.apple.com/documentation/healthkit/hkhealthstore/workoutsessionmirroringstarthandler)
- [sendToRemoteWorkoutSession(data:completion:)](https://developer.apple.com/documentation/healthkit/hkworkoutsession/sendtoremoteworkoutsession(data:completion:))
- [HKWorkoutActivityType.traditionalStrengthTraining](https://developer.apple.com/documentation/healthkit/hkworkoutactivitytype/traditionalstrengthtraining)
- [HKWorkoutActivityType.functionalStrengthTraining](https://developer.apple.com/documentation/healthkit/hkworkoutactivitytype/functionalstrengthtraining)
- [HealthKit entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.developer.healthkit)
- [HealthKit capabilities entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.developer.healthkit.access)
- [Entitlements index](https://developer.apple.com/documentation/bundleresources/entitlements)
- [Configuring background execution modes](https://developer.apple.com/documentation/xcode/configuring-background-execution-modes)
- [App Store Connect API: CapabilityType](https://developer.apple.com/documentation/appstoreconnectapi/capabilitytype)

Secondary, read today:

- [Swift with Majid: What is new in SwiftUI after WWDC26](https://swiftwithmajid.com/2026/06/08/what-is-new-in-swiftui-after-wwdc26/)
- [Apple Developer Forums thread 837430: Track workouts with HealthKit on iOS + GPS tracking](https://developer.apple.com/forums/thread/837430) (no Apple reply)
- [Apple Developer Forums thread 730234: CFBundleShortVersionString of an app extension must match that of its containing parent app](https://developer.apple.com/forums/thread/730234)

Local sources:

- `/tmp/spotter-substrate-main/docs/plans/health-data/02-workouts.md`
- `/tmp/spotter-substrate-main/apps/apple/` (`project.yml`, `Support/SubstrateSync.entitlements`, `SubstrateSync/Sources/Sync/HealthReader.swift`, `SubstrateSync/Sources/Sync/HealthEncoder.swift`) and `/tmp/spotter-substrate-main/.mise/apple.sh`
- `apps/apple/release.sh`, `apps/apple/README.md`, `apps/apple/satchel/project.yml`, `apps/apple/mneme-v6/project.yml`, `apps/apple/mneme-v6/MnemeV6/Sources/Shared/RecordingActivityShared.swift` in this worktree
- The owner's Hevy screenshots (in-progress bar above the tab bar, the Live Activity with a time-sensitive "Rest time is over!" notification, the watch Smart Stack view)
