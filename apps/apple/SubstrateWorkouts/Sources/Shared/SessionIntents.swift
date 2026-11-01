import AppIntents
import Foundation
import os

// The Live Activity's buttons. Compiled into the app and the widget: the
// widget needs the types to build `Button(intent:)`, but a LiveActivityIntent
// always performs in the app's process (the system launches the app in the
// background if it is not running), so only the app's copy does anything.
// WORKOUTS_APP is set for the app target alone (project.yml).

/// What a Live Activity button asks of the session. The app maps each case to
/// its `SessionCommand`, which needs WorkoutsKit, and the widget links none.
nonisolated enum LiveActivityAction: Hashable, Sendable {
    /// −15 s or +15 s on the running countdown.
    case adjustRest(seconds: Int)
    /// Ends the running countdown now.
    case skipRest
    /// Ticks the Up next row with its ghost values, only if that row is still
    /// pending and still Up next (DESIGN.md §9.1, §5.5).
    case completeUpNext(rowID: String)
    /// Stops the running timed set, which completes it.
    case stopTimedSet(rowID: String)
}

/// The seam between the intents and the session. In the app, an action goes
/// to `SessionController.shared.handle(_:)`, which is created on first use,
/// so a tap that launched the app in the background reaches it too; the
/// controller folds the journal before it applies the action (DESIGN.md
/// §6.3). `handler`, when set, replaces that path.
@MainActor
enum LiveActivityActions {
    typealias Handler = @MainActor (LiveActivityAction) async -> Void

    static var handler: Handler?

    private static let logger = Logger(subsystem: "dev.reamde.substrate.workouts", category: "live-activity")

    static func send(_ action: LiveActivityAction) async {
        logger.info("Live Activity action: \(String(describing: action), privacy: .public)")
        if let handler {
            await handler(action)
            return
        }
        #if WORKOUTS_APP
        await SessionController.shared.handle(action)
        #else
        // The widget never performs an intent: the system runs it in the app.
        logger.fault("Live Activity action performed outside the app: \(String(describing: action), privacy: .public)")
        #endif
    }
}

/// −15 s and +15 s.
struct AdjustRestIntent: LiveActivityIntent {
    static let title: LocalizedStringResource = "Adjust Rest"
    static let isDiscoverable = false

    @Parameter(title: "Seconds") var seconds: Int

    init() {}

    init(seconds: Int) {
        self.seconds = seconds
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if WORKOUTS_APP
        await LiveActivityActions.send(.adjustRest(seconds: seconds))
        #endif
        return .result()
    }
}

/// Skip.
struct SkipRestIntent: LiveActivityIntent {
    static let title: LocalizedStringResource = "Skip Rest"
    static let isDiscoverable = false

    init() {}

    @MainActor
    func perform() async throws -> some IntentResult {
        #if WORKOUTS_APP
        await LiveActivityActions.send(.skipRest)
        #endif
        return .result()
    }
}

/// Done: completes the Up next set with its ghost values.
struct CompleteSetIntent: LiveActivityIntent {
    static let title: LocalizedStringResource = "Complete Set"
    static let isDiscoverable = false

    @Parameter(title: "Row") var rowID: String

    init() {}

    init(rowID: String) {
        self.rowID = rowID
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if WORKOUTS_APP
        await LiveActivityActions.send(.completeUpNext(rowID: rowID))
        #endif
        return .result()
    }
}

/// Stop, on a running timed set.
struct StopTimedSetIntent: LiveActivityIntent {
    static let title: LocalizedStringResource = "Stop Timed Set"
    static let isDiscoverable = false

    @Parameter(title: "Row") var rowID: String

    init() {}

    init(rowID: String) {
        self.rowID = rowID
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        #if WORKOUTS_APP
        await LiveActivityActions.send(.stopTimedSet(rowID: rowID))
        #endif
        return .result()
    }
}
