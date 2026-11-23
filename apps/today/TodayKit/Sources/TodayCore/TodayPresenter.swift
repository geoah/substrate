import Foundation
import SubstrateCore
import SubstrateRuntime
import SubstrateScreens

/// The root's presenter: the day made from the state, then every word the
/// app's screens show, by the engine.
public enum TodayPresenter {
    /// The day the engine reads, from what the root holds.
    public static func day(_ state: TodayApp.State, now: Date, calendar: Calendar) -> Day {
        var day = Day(now: now, calendar: calendar)
        day.discovery = state.discovery ?? Discovery()
        day.take(state.linked.records.values)
        day.take(state.requests)
        day.take(state.contacts.values)
        day.occurrences = state.windows.occurrences.compactMap(Occurrence.init)
        // Only what is on the owner's own calendars is a meeting of theirs.
        day.meetings = Meeting.merged(state.windows.ownMeetings.compactMap(Meeting.init))
        day.stale = state.stale
        day.drafts = state.drafts
        // The day's notes are another day's until the owner looks.
        if let notes = state.notes, notes.day == day.today { day.notes = notes }
        day.exchanges = state.exchanges
        day.reachable = state.reachable && state.linked.link?.online != false
        day.server = state.linked.link?.server
        day.sources = state.sources
        return day
    }

    public static func present(_ state: TodayApp.State, linked: LinkedRecords, build: BuildFacts, now: Date, calendar: Calendar) -> TodayPresentation {
        let day = day(state, now: now, calendar: calendar)
        let engine = DayEngine(day)
        let reminds = state.permission == .on || state.permission == .notTimeSensitive
        var out = TodayPresentation(
            pane: engine.pane, pages: PageSource(maker: engine, stamp: state.revision &+ Int(now.timeIntervalSince1970 / 60)),
            notices: linked.notice(state.linked).map { [$0] } ?? [],
            problem: state.problem.map { ProblemPresentation(title: "A change was not kept", message: $0) }, settings: TodaySettings.sections(day),
            storage: SettingsPresenter.storage(state.usage ?? [], budget: nil),
            about: AboutPresentation.present(
                appName: TodayApp.appName, version: build.version, channel: build.channel, offered: state.offered,
                privacyPolicy: build.privacyPolicy), reminders: reminds ? engine.reminders : [], nextChange: engine.nextChange)
        out.moss = TodaySettings.moss(day)
        out.writing = state.exchanges.contains { $0.state == .writing }
        return out
    }

    /// Settings as one screen: the app's sections, then the kit's.
    public static func settings(_ link: LinkPresentation<TodayPresentation>, _ app: TodayPresentation) -> SettingsPresentation {
        SettingsPresenter.screen(app.settings, link: link, storage: app.storage, about: app.about)
    }
}
