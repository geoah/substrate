import ActivityKit
import Foundation
import os
import WorkoutsCore

/// The session's Live Activity (DESIGN.md §9.1): requested in the foreground
/// at Start, or at restore when missing; updated with a timestamp so a late,
/// older update is dropped; ended at Finish with the summary for five
/// minutes, and at Discard at once. When the owner dismisses it, that holds
/// for the rest of the session.
///
/// Only the countdown coordinator calls it (§6.2).
@MainActor
final class LiveActivityController {
    static let shared = LiveActivityController()

    typealias State = WorkoutsActivityAttributes.ContentState

    /// How long the finished summary stays on the Lock Screen.
    static let finishedDisplay: TimeInterval = 5 * 60

    /// `UserDefaults` key: the session ids whose activity the owner
    /// dismissed (§2.3), kept until that session ends.
    private static let dismissedKey = "workouts.liveActivity.dismissed"

    private let defaults: UserDefaults
    private let logger = Logger(subsystem: "dev.reamde.substrate.workouts", category: "live-activity")
    /// Activities this app ended, whose `.dismissed` is not the owner's.
    private var endedHere: Set<String> = []
    /// Activities whose state updates are being watched.
    private var watched: Set<String> = []
    /// The last content sent to each activity, revision aside. Compared with
    /// what was sent rather than what shows, so an update still in flight
    /// cannot make a newer, different one look redundant.
    private var lastSent: [String: Sent] = [:]

    private struct Sent: Equatable {
        var state: State
        var staleDate: Date?

        init(_ state: State) {
            var state = state
            state.revision = 0
            self.state = state
            staleDate = state.staleDate
        }
    }

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    /// The session's activity while it is on screen.
    func activity(for session: SessionID) -> Activity<WorkoutsActivityAttributes>? {
        Activity<WorkoutsActivityAttributes>.activities.first { activity in
            guard activity.attributes.sessionID == session.rawValue else { return false }
            switch activity.activityState {
            case .active, .stale, .pending: return true
            case .dismissed:
                // Dismissed while this process was not watching: remember it.
                if !endedHere.contains(activity.id) { markDismissed(session) }
                return false
            default: return false
            }
        }
    }

    /// Whether the owner dismissed this session's activity.
    func isDismissed(_ session: SessionID) -> Bool {
        dismissed.contains(session.rawValue)
    }

    /// Updates the session's activity, or requests one when there is none,
    /// the owner has not dismissed it and Live Activities are on. A request
    /// from the background fails and is only logged: the next foreground
    /// restore asks again.
    func show(session: SessionID, title: String, state: State, timestamp: Date) async {
        let content = ActivityContent(state: state, staleDate: state.staleDate)
        if let activity = activity(for: session) {
            watch(activity)
            // Nothing the widget draws changed (a keystroke elsewhere): skip
            // the redraw, so typing does not flood ActivityKit with updates.
            let sent = Sent(state)
            if lastSent[activity.id] == sent { return }
            lastSent[activity.id] = sent
            await Self.update(activity.id, content, timestamp: timestamp)
            return
        }
        guard !isDismissed(session) else { return }
        guard ActivityAuthorizationInfo().areActivitiesEnabled else {
            logger.info("Live Activities are off; the session runs without one")
            return
        }
        do {
            let activity = try Activity.request(
                attributes: WorkoutsActivityAttributes(sessionID: session.rawValue, title: title),
                content: content,
                pushType: nil
            )
            logger.info("Live Activity requested: \(activity.id, privacy: .public)")
            lastSent[activity.id] = Sent(state)
            watch(activity)
        } catch {
            logger.error("Live Activity request failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// Finish: the summary, no countdown, for five minutes (§9.1).
    func finish(session: SessionID, summary: String, timestamp: Date) async {
        for activity in activities(of: session) {
            var state = activity.content.state
            state.finishedSummary = summary
            state.countdown = nil
            endedHere.insert(activity.id)
            await Self.end(
                activity.id,
                ActivityContent(state: state, staleDate: nil),
                dismissalPolicy: .after(timestamp.addingTimeInterval(Self.finishedDisplay)),
                timestamp: timestamp
            )
        }
        forget(session)
    }

    /// Discard: gone at once.
    func discard(session: SessionID) async {
        for activity in activities(of: session) {
            endedHere.insert(activity.id)
            await Self.end(activity.id, nil, dismissalPolicy: .immediate, timestamp: .now)
        }
        forget(session)
    }

    /// Ends every activity of a session other than `session` at once: left
    /// over from a crash during Finish or Discard, or from another install.
    func endAll(except session: SessionID?) async {
        for activity in Activity<WorkoutsActivityAttributes>.activities
            where activity.attributes.sessionID != session?.rawValue
        {
            switch activity.activityState {
            case .active, .stale, .pending:
                endedHere.insert(activity.id)
                logger.info("Ending a leftover Live Activity: \(activity.id, privacy: .public)")
                await Self.end(activity.id, nil, dismissalPolicy: .immediate, timestamp: .now)
            default:
                break
            }
        }
        // A dismissed set only matters for the open session.
        let keep = session.map { isDismissed($0) ? [$0.rawValue] : [] } ?? []
        if dismissed != Set(keep) { dismissed = Set(keep) }
    }

    // MARK: - ActivityKit

    // An `Activity` is not Sendable and its `update` and `end` run off the
    // main actor, so the main actor never hands one over: these find the
    // activity again by id, off the main actor, and carry only Sendable
    // content across.

    @concurrent
    private nonisolated static func update(_ id: String, _ content: ActivityContent<State>, timestamp: Date) async {
        guard let activity = Activity<WorkoutsActivityAttributes>.activities.first(where: { $0.id == id }) else { return }
        await activity.update(content, alertConfiguration: nil, timestamp: timestamp)
    }

    @concurrent
    private nonisolated static func end(
        _ id: String,
        _ content: ActivityContent<State>?,
        dismissalPolicy: ActivityUIDismissalPolicy,
        timestamp: Date
    ) async {
        guard let activity = Activity<WorkoutsActivityAttributes>.activities.first(where: { $0.id == id }) else { return }
        await activity.end(content, dismissalPolicy: dismissalPolicy, timestamp: timestamp)
    }

    // MARK: - Private

    private func activities(of session: SessionID) -> [Activity<WorkoutsActivityAttributes>] {
        Activity<WorkoutsActivityAttributes>.activities.filter {
            $0.attributes.sessionID == session.rawValue && $0.activityState != .ended && $0.activityState != .dismissed
        }
    }

    /// Records the owner's dismissal when the activity reports it.
    private func watch(_ activity: Activity<WorkoutsActivityAttributes>) {
        let id = activity.id
        guard watched.insert(id).inserted else { return }
        let session = SessionID(activity.attributes.sessionID)
        let updates = activity.activityStateUpdates
        Task { [weak self] in
            for await state in updates {
                guard let self else { return }
                if state == .dismissed, !self.endedHere.contains(id) {
                    self.logger.info("The owner dismissed the Live Activity of \(session.rawValue, privacy: .public)")
                    self.markDismissed(session)
                }
                if state == .ended || state == .dismissed { break }
            }
            self?.watched.remove(id)
            self?.lastSent[id] = nil
        }
    }

    private func markDismissed(_ session: SessionID) {
        var set = dismissed
        if set.insert(session.rawValue).inserted { dismissed = set }
    }

    private func forget(_ session: SessionID) {
        var set = dismissed
        if set.remove(session.rawValue) != nil { dismissed = set }
    }

    private var dismissed: Set<String> {
        get { Set(defaults.stringArray(forKey: Self.dismissedKey) ?? []) }
        set { defaults.set(newValue.sorted(), forKey: Self.dismissedKey) }
    }
}
