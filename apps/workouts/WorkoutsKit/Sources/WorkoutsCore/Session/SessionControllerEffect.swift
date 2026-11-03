import Foundation

public extension SessionController {
    /// What an applied event asks the device to do.
    enum Effect: Equatable, Sendable {
        /// The countdown's notification replaced, and the Live Activity
        /// updated or requested (§6.2): after every applied event, so
        /// whatever happened meanwhile they match the journal again.
        case countdown(CountdownSync)
        /// At launch and every return to the foreground: the same, and every
        /// other activity ended; with no session open, anything left behind
        /// ends.
        case restore(CountdownSync?)
        /// Finish or Discard: the alert goes, and the activity ends, with
        /// the summary for five minutes after a Finish, at once after a
        /// Discard.
        case countdownEnded(SessionID, final: LiveActivityPayload?)
        /// The body mass and the notification permission, then the Start.
        case prepareStart(StartRequest)
        /// The history file written and the Health save queued, both before
        /// the journal goes.
        case writeHistory(FinishedSession)
        /// Whether History already holds the replayed session.
        case checkHistory(SessionID)
        case readAlerts
        case listSetAside
        case health(HealthWork)
    }

    /// The countdown's alert and the Live Activity, as the presentation says.
    struct CountdownSync: Equatable, Sendable {
        public var session: SessionID
        public var active: Bool
        public var title: String
        public var notification: CountdownNotification?
        public var activity: LiveActivityPayload
    }

    enum HealthWork: Equatable, Sendable {
        /// Finish: the save starts without Finish waiting, once the sheet
        /// and the cover have closed.
        case afterFinish(HealthTrigger)
        case recover(SessionID)
        case foreground
    }
}
