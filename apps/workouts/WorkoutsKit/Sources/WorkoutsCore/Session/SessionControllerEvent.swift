import Foundation
import SubstrateDevice

public extension SessionController {
    /// What the session's engine applies: an engine event (the journal's
    /// truth), the steps around it that wait on the device, and the notes
    /// that move only what the screens show.
    enum Event: Codable, Equatable, Sendable {
        /// One engine event, journaled: a Start begins a new journal.
        case engine(EventEnvelope)
        /// Discard: the engine's `discard`, and the journal removed.
        case discard(EventEnvelope, session: SessionID, then: StartRequest?)
        /// Save asked: the finish the engine accepts, waiting on its
        /// history file. Not journaled.
        case finishRequested(EventEnvelope, FinishedSession)
        /// The history file is written: the session is over and the
        /// journal removed.
        case finished(FinishedSession)
        /// The history file could not be written: the session stays open.
        case finishFailed(String)
        /// The controller opened; `replayed` says a journal was folded.
        case opened(replayed: Bool)
        /// The replayed session is already in History: Finish wrote its
        /// file and the app died before it removed the journal.
        case alreadyFinished(SessionID, recoverHealth: Bool)
        case foregrounded
        /// Start asked: the device is asked for the body mass and the
        /// notification permission first.
        case starting(StartRequest)
        /// The Start did not happen: an unknown routine.
        case startDropped
        /// The presentation's next change came: a countdown ended, or
        /// "Rest over" gives way to Up next.
        case tick(Date)
        case alertsRead(NotificationPermission)
        case setAsideFound([UnreadableRecord])
        case noted(SessionNote)
    }

    /// A change to what the screens show alone.
    enum SessionNote: Codable, Equatable, Sendable {
        /// A set the entry check questions, waiting for the owner.
        case asked(EntryCheckRequest)
        /// The question answered Edit, or dismissed.
        case answered(edit: Bool)
        case scroll(ScrollTarget?)
        /// The start conflict's Finish flow, with what starts after Save.
        case finishFlow(StartRequest)
        case finishFlowShown
        case startAfterFinish(StartRequest?)
        /// The Lock Screen asked for a set the entry check questions: left
        /// for the app.
        case leftForTheApp
        /// A command the controller refuses before the engine sees it (a
        /// tick whose values are incomplete, a Start of no timer): the
        /// engine refuses it, so it reads as any refusal.
        case refuse(EngineError)
    }
}
