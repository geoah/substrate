import Foundation
import WorkoutsCore

// Every call from the session controller into SubstrateWorkouts/Sources/Platform (DESIGN.md
// §6.2, §9) is in this file, so the loop in SessionController.swift reads
// the same whatever shape the platform types take. Previews
// (`effectsEnabled` false) touch none of it.

extension SessionController {
    // MARK: - The countdown coordinator (§6.2): notification and Live Activity

    /// Step 4 of the loop: replace the alert, update or request the Live Activity.
    func platformSync(_ state: SessionState, presentation: SessionPresentation, revision: Int, at _: Date) async {
        guard effectsEnabled else { return }
        await CountdownCoordinator.shared.sync(state, presentation, revision)
    }

    /// Launch and every return to the foreground (§6.3): the alert and the
    /// Live Activity match the journal again; with no session open, anything
    /// left behind ends.
    func platformRestore(_ state: SessionState?, presentation: SessionPresentation?, revision: Int) async {
        guard effectsEnabled else { return }
        await CountdownCoordinator.shared.restore(state, presentation, revision)
    }

    /// Asked at the first Start (§9.6); afterwards it only re-reads the settings.
    func platformRequestNotifications() async {
        guard effectsEnabled else { return }
        await Notifications.shared.requestAuthorizationIfNeeded()
    }

    /// Finish, right after the history file and before the journal goes:
    /// the Health queue entry, written with no `await` in between (§6.1).
    func platformQueueHealthSave(_ session: SessionID) {
        guard effectsEnabled else { return }
        HealthExporter.shared.enqueue(session)
    }

    /// Finish: the Live Activity keeps the summary for five minutes, the
    /// alert goes, and the Health save starts without Finish waiting (§6.1).
    /// The first Finish may show the permission sheet, so the save starts
    /// once the cover and the Finish sheet have closed; over a cover that
    /// opened again for the next session it saves without asking.
    func platformSessionFinished(_ session: SessionID, records _: SessionRecords, summary: String) async {
        guard effectsEnabled else { return }
        await CountdownCoordinator.shared.finished(session, summary: summary)
        Task { [app] in
            try? await Task.sleep(for: .milliseconds(700))
            await HealthExporter.shared.process(app.isSessionPresented ? .finishCovered : .finish)
        }
    }

    /// A session Finish saved but never queued for Health (the app died in
    /// between): queued without the permission sheet, which only Finish,
    /// Connect and Try again may show; the next foreground saves it.
    func platformRecoverHealthSave(_ session: SessionID) {
        guard effectsEnabled else { return }
        HealthExporter.shared.enqueueAndProcess(session)
    }

    /// Discard: the Live Activity ends at once and the alert goes.
    func platformSessionDiscarded(_ session: SessionID) async {
        guard effectsEnabled else { return }
        await CountdownCoordinator.shared.discarded(session)
    }

    /// Notifications are refused or shown nowhere.
    var platformAlertsOff: Bool {
        effectsEnabled && Notifications.shared.alertsOff
    }

    /// Notifications are on, but not time sensitive.
    var platformTimeSensitiveOff: Bool {
        effectsEnabled && Notifications.shared.timeSensitiveOff
    }

    /// Notifications have not been asked for yet.
    var platformAlertsNotAsked: Bool {
        effectsEnabled && Notifications.shared.notAsked
    }


    /// The latest body mass in Apple Health, for Settings; nil without one or without access.
    func latestHealthBodyMass() async -> Mass? {
        guard effectsEnabled else { return nil }
        return await BodyMassReader.shared.latest()
    }

    // MARK: - Apple Health (§9.5)

    /// At launch and on each return to the foreground; never prompts. A
    /// session History holds as pending that the queue lost is queued again.
    func platformProcessHealthQueue() async {
        guard effectsEnabled else { return }
        HealthExporter.shared.requeueLost(app.history)
        await HealthExporter.shared.process(.foreground)
    }

    /// Settings' "Connect".
    func platformConnectHealth() async {
        guard effectsEnabled else { return }
        await HealthExporter.shared.connect()
    }

    /// The session detail's "Try again" after access was given in the Health app.
    func platformRetryHealth(_ id: SessionID) async {
        guard effectsEnabled else { return }
        await HealthExporter.shared.retry(id)
    }

    func platformHealthAccess() -> HealthAccess {
        guard effectsEnabled else { return .authorized }
        return HealthExporter.shared.access
    }

    /// Whether Finish's last line can say the session goes to Apple Health too.
    func platformHealthSharingAllowed() -> Bool {
        switch platformHealthAccess() {
        case .authorized, .notDetermined: true
        case .denied, .unavailable: false
        }
    }

    /// The latest Apple Health body mass, else the typed one (§9.5). Start never prompts.
    func platformBodyMass(typed: Mass?) async -> Mass? {
        guard effectsEnabled else { return typed }
        return await BodyMassReader.shared.bodyMass(typed: typed)
    }
}
