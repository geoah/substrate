import Foundation
import WorkoutsCore

/// Where the Live Activity's tap (`substrate-workouts://session`) and a tap on the rest
/// notification lead: the session cover, scrolled to Up next (DESIGN.md §9.1,
/// §9.2).
@MainActor
enum DeepLinks {
    /// Handles a URL the app was opened with; false for one it does not know.
    @discardableResult
    static func handle(_ url: URL, app: AppModel = .shared, session: SessionController = .shared) -> Bool {
        guard url.scheme == WorkoutsLinks.session.scheme else { return false }
        switch url.host() {
        case WorkoutsLinks.session.host():
            openSession(app: app, session: session)
            return true
        default:
            return false
        }
    }

    /// Opens the cover on the session in progress and brings Up next into
    /// view; with none open, the app stays where it is.
    static func openSession(app: AppModel = .shared, session: SessionController = .shared) {
        Task {
            await session.prepare()
            guard session.isActive else { return }
            app.isSessionPresented = true
            if let row = session.presentation?.upNext?.row ?? session.presentation?.timedRow {
                session.scrollTarget = ScrollTarget(row: row)
            }
        }
    }
}
