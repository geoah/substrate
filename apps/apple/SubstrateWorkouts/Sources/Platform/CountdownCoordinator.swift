import Foundation
import os
import WorkoutsCore
import UIKit

/// The only code that touches the countdown notification and the Live
/// Activity (DESIGN.md §6.2, req 25). The session controller calls it after
/// every applied event, once the journal is written, and at launch and every
/// return to the foreground, so whatever happened while the app was suspended
/// or dead, the alert and the Live Activity match the journal again.
///
/// Every call runs the same steps in a fixed order (04 §2.4): remember this
/// call as the latest; replace the notification; stop if a newer call
/// started meanwhile; update the Live Activity with a timestamp, which
/// ActivityKit uses to drop a late, older update. Two quick taps on +15 s
/// interleave at every `await`; the newest wins.
///
/// V1.1 inserts AlarmKit between the notification and the Live Activity:
/// cancel the old alarm before any `await`, schedule the new one after.
@MainActor
final class CountdownCoordinator {
    static let shared = CountdownCoordinator()

    private let notifications: Notifications
    private let liveActivity: LiveActivityController
    private let logger = Logger(subsystem: "dev.reamde.substrate.workouts", category: "countdown")
    /// Bumped by every call; a call that sees a newer one after an `await` stops.
    private var latest = 0
    /// Plays the end-of-countdown haptic while the app is in the foreground.
    private var endWatch: Task<Void, Never>?

    init(notifications: Notifications = .shared, liveActivity: LiveActivityController = .shared) {
        self.notifications = notifications
        self.liveActivity = liveActivity
    }

    /// Sets the notification delegate and category. Call once from
    /// `SubstrateWorkoutsApp.init`, before the app finishes launching, so a tap on a
    /// notification that launched the app is delivered.
    static func install() {
        Notifications.shared.install()
    }

    /// After every applied event (§6.1): `revision` is the controller's
    /// effects revision, carried into the content state.
    func sync(_ state: SessionState, _ presentation: SessionPresentation, _ revision: Int) async {
        latest += 1
        let mine = latest
        // The instant of this change, to the sub-second: event instants are
        // floored to the second, so two taps within one would tie.
        let changedAt = Date.now
        let session = state.session.id

        watchEnd(of: state, revision: mine, now: changedAt)

        // The alert: one pending request, replaced in place.
        notifications.clear()
        if state.phase == .active, let notification = presentation.notification, notification.fireDate > changedAt {
            await notifications.schedule(notification, session: session, now: changedAt)
        }
        guard mine == latest else { return }

        // Finish ends the activity with its summary (`finished`), never an
        // "All sets done" update first.
        guard state.phase == .active else { return }
        await liveActivity.show(
            session: session,
            title: presentation.title,
            state: Self.contentState(presentation.liveActivity, revision: revision),
            timestamp: changedAt
        )
    }

    /// At launch and on every return to the foreground (§6.3). With no open
    /// session, clears the alert and ends any activity left behind. Also
    /// re-reads the notification settings, so the session screen can say
    /// when alerts are off.
    func restore(_ state: SessionState?, _ presentation: SessionPresentation?, _ revision: Int) async {
        // The settings are read last: an `await` before the sync would let a
        // command applied meanwhile be overwritten by this older state.
        defer { Task { await notifications.refresh() } }
        guard let state, let presentation, state.phase == .active else {
            latest += 1
            endWatch?.cancel()
            notifications.clear()
            await liveActivity.endAll(except: nil)
            return
        }
        // An expired rest ends quietly: the watch only plays for an end
        // still ahead, and the notification has already alerted.
        await sync(state, presentation, revision)
        await liveActivity.endAll(except: state.session.id)
    }

    /// Finish (§6.1): no alert, and the activity ends with "Saved: 46 min,
    /// 24 sets" for five minutes. `summary` is
    /// `SessionPresenter.finishedSummary(records)`.
    func finished(_ session: SessionID, summary: String) async {
        latest += 1
        endWatch?.cancel()
        notifications.clear()
        await liveActivity.finish(session: session, summary: summary, timestamp: .now)
    }

    /// Discard (§6.1): no alert, and the activity goes at once.
    func discarded(_ session: SessionID) async {
        latest += 1
        endWatch?.cancel()
        notifications.clear()
        await liveActivity.discard(session: session)
    }

    // MARK: - Content

    /// The presenter's Live Activity content as the widget's content state.
    /// The title travels in the attributes, set once at the request.
    static func contentState(_ content: LiveActivityContent, revision: Int) -> WorkoutsActivityAttributes.ContentState {
        WorkoutsActivityAttributes.ContentState(
            clockStart: content.clockStart,
            clockPausedAt: content.clockPausedAt,
            exercise: content.exercise,
            groupLabel: content.groupLabel,
            next: content.next,
            nextRowID: content.nextRowID?.rawValue,
            canComplete: content.canComplete,
            countdown: content.countdown.map { countdown in
                .init(kind: kind(countdown.kind), label: countdown.label, startedAt: countdown.startedAt, endsAt: countdown.endsAt)
            },
            setsDone: content.setsDone,
            setsPlanned: content.setsPlanned,
            finishedSummary: nil,
            revision: revision
        )
    }

    private static func kind(_ kind: LiveActivityCountdown.Kind) -> WorkoutsActivityAttributes.ContentState.Countdown.Kind {
        switch kind {
        case .rest: .rest
        case .roundRest: .roundRest
        case .blockRest: .blockRest
        case .transition: .transition
        case .pause: .pause
        case .sideSwitch: .sideSwitch
        case .timedSet: .timedSet
        case .manual: .manual
        }
    }

    // MARK: - The end in the foreground

    /// Plays §9.3's haptic when the running countdown reaches its end while
    /// the app is in the foreground. A countdown that notifies stays quiet
    /// unless the session cover shows: elsewhere in the app the banner alerts
    /// instead. A wake-up long after the end (the app was suspended) plays
    /// nothing: the notification already alerted.
    private func watchEnd(of state: SessionState, revision mine: Int, now: Date) {
        endWatch?.cancel()
        endWatch = nil
        guard state.phase == .active, let countdown = state.countdown, let end = countdown.endsAt, end > now else { return }
        let notifies = countdown.notifies
        let cue: HapticCue = if countdown.timedRow != nil {
            .targetReached
        } else if notifies {
            .countdownEnded
        } else {
            .shortCountdownEnded
        }
        endWatch = Task { [weak self] in
            try? await Task.sleep(for: .seconds(end.timeIntervalSinceNow))
            guard !Task.isCancelled, let self, self.latest == mine else { return }
            guard Date.now.timeIntervalSince(end) < 2,
                  UIApplication.shared.applicationState == .active
            else { return }
            if notifies, !SessionScreen.shared.isVisible { return }
            Haptics.shared.play(cue)
        }
    }
}
