import Foundation
import SubstrateKit
import WorkoutsCore

/// The session's Live Activity as the kit's ongoing-activity service
/// requests it: its attributes made from the session's id and title, and
/// its content state read from the presenter's content the session sends.
nonisolated extension WorkoutsActivityAttributes: OngoingActivityAttributes {
    init(activityID: String, title: String) {
        self.init(sessionID: activityID, title: title)
    }

    var activityID: String { sessionID }

    /// The presenter's content as the widget's content state. The title
    /// travels in the attributes, set once at the request.
    static func contentState(_ data: Data) -> ContentState? {
        guard let payload = LiveActivityPayload.decoded(data) else { return nil }
        let content = payload.content
        return ContentState(
            clockStart: content.clockStart,
            clockPausedAt: content.clockPausedAt,
            exercise: content.exercise,
            setLabel: content.setLabel,
            isWarmup: content.isWarmup,
            values: content.values,
            next: content.next,
            nextRowID: content.nextRowID?.rawValue,
            canComplete: content.canComplete,
            // Finish ends the activity with the summary and no countdown.
            countdown: payload.finishedSummary != nil
                ? nil
                : content.countdown.map { countdown in
                    .init(kind: kind(countdown.kind), label: countdown.label, startedAt: countdown.startedAt, endsAt: countdown.endsAt)
                },
            setsDone: content.setsDone,
            setsPlanned: content.setsPlanned,
            finishedSummary: payload.finishedSummary,
            revision: payload.revision
        )
    }

    private static func kind(_ kind: LiveActivityCountdown.Kind) -> ContentState.Countdown.Kind {
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
}
