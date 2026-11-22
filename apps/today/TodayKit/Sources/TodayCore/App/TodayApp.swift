import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// Substrate Today's root, kept by the link (`LinkController<TodayApp>`).
/// The small kinds the pane reads (lists, tasks, the series kinds, logs,
/// and the app's own) are the kit's `LinkedRecords`: read once, followed
/// through the watch, every change written through the outbox. Beside them
/// it keeps what the window reads returned (routine occurrences, meetings),
/// the open requests, the contacts its rows name, the drafts above each
/// Save, the day's notes and the exchanges with the assistant. Its
/// presentation is every screen of the app; the link's is the
/// connection's, beside it.
///
/// The repository may hold any part of what the pane reads, so the kinds
/// are discovered at sign-in and kept on the device: the next launch
/// opens over them at once, offline too.
public struct TodayApp: LinkedRecordsApp {
    public typealias Command = TodayCommand

    public enum Event: Codable, Equatable, Sendable, LinkedRecordsEvent {
        case records(LinkedRecords.Event)
        /// What the repository holds of what the pane reads.
        case discovered(Discovery)
        case discoveryFailed(String)
        /// What the device kept: the day's notes and the last window reads,
        /// with the instant, the owner's day and time zone as the device
        /// has them.
        case localRead(notes: DayNotes?, windows: Windows?, at: Date, today: CivilDay, zone: String)
        case windowsRead(Windows, requests: [Record], stale: [StaleSource])
        case windowsFailed(String)
        case contactsRead([Record], asked: [String])
        /// The owner looked at the pane.
        case looked(at: Date, today: CivilDay, zone: String)
        case drafted(Drafts)
        case noted(DayNotes)
        /// Requests decided here: hidden until they are read again.
        case decided([String])
        case asked(Exchange, message: String, context: [String])
        case answer(String, AssistantEvent)
        case answerClosed(String)
        /// The words of an exchange that failed were kept as a task: its
        /// card says so in place.
        case kept(String)
        case stopped
        case later(ReminderPlan)
        case usage([AreaUsage])
        case offered(String?)
        case problemDismissed
        case permissionWanted
        case permission(NotificationPermission)
        case scheduled([ReminderPlan])
        case ticked(Date)

        public var recordsEvent: LinkedRecords.Event? {
            if case let .records(event) = self { event } else { nil }
        }
    }

    public enum Effect: Equatable, Sendable, LinkedRecordsEffect {
        case records(LinkedRecords.Effect)
        case discover
        case saveDiscovery(Discovery)
        case loadLocal
        case readWindows(Discovery)
        case saveWindows(Windows)
        case readContacts([String])
        case saveNotes(DayNotes)
        case ask(Exchange, message: String, context: [String])
        case stopAsking
        case readUsage
        case readPermission
        case askPermission
        case registerNotifications
        case schedule([ReminderPlan])
        case remind(ReminderPlan)

        public var recordsEffect: LinkedRecords.Effect? {
            if case let .records(effect) = self { effect } else { nil }
        }
    }

    public struct State: Codable, Equatable, Sendable, LinkedRecordsHolding {
        public var linked = LinkedRecordsState()
        public var discovery: Discovery?
        public var discoveryProblem: String?
        public var windows = Windows()
        public var windowsProblem: String?
        /// The open requests, as last read.
        public var requests: [Record] = []
        /// The contacts the rows name, by path, and the paths asked for.
        public var contacts: [String: Record] = [:]
        public var contactsAsked: Set<String> = []
        public var stale: [StaleSource] = []
        public var drafts = Drafts()
        public var notes: DayNotes?
        /// The device's notes were read: a look may start a new day's.
        public var notesRead = false
        /// The owner's time zone, as the device last said.
        public var zone: String?
        public var exchanges: [Exchange] = []
        /// False once the assistant could not be reached, until it answers.
        public var reachable = true
        /// Each area of the repository's folder and its size, nil until read.
        public var usage: [AreaUsage]?
        public var problem: String?
        /// The newer build the updater offers, which About shows.
        public var offered: String?
        public var permission: NotificationPermission?
        /// The reminders the device holds.
        public var scheduled: [ReminderPlan] = []
        /// Moved each time the pane may be drawn otherwise.
        public var revision = 0

        public init() {}
    }

    public static let appName = "Substrate Today"
    static let kindsFile = SettingsFile<Discovery>(name: "today-kinds.json")
    static let notesFile = SettingsFile<DayNotes>(name: "today-day.json")
    static let windowsFile = SettingsFile<Windows>(name: "today-windows.json")

    public let linked: LinkedRecords
    public let build: BuildFacts
    /// What the repository held when the device last asked, read from its
    /// folder as the root is made.
    public let saved: Discovery?

    /// - Parameters:
    ///   - words: the Status screen's words, as the shell made them
    ///     (`AppShell.Parts.words`, from `words(device:)`).
    ///   - folder: the repository's folder (`AppShell.Parts.folder`), where
    ///     the kinds found at the last sign-in are kept, so the pane opens
    ///     over them offline.
    public init(cache: RecordCache?, outbox: Outbox?, words: StatusWords, build: BuildFacts, folder: RepositoryFolder? = nil) {
        saved = folder.flatMap { Self.kindsFile.load(from: $0) }
        linked = LinkedRecords(kinds: saved?.linked ?? [], cache: cache, outbox: outbox, words: words)
        self.build = build
    }

    /// The Status screen's words, and the linked records'.
    public static func words(device: String) -> StatusWords {
        StatusWords(app: appName, device: device, kept: "day", packageNames: [TodayKinds.packageID: "Today"])
    }

    public var initialState: State {
        var state = State()
        state.discovery = saved
        return state
    }

    /// A Gregorian calendar in a time zone, as the engine reads days.
    static func calendar(_ timeZone: TimeZone, _ locale: Locale = Locale(identifier: "en_US_POSIX")) -> Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        calendar.locale = locale
        return calendar
    }

    // MARK: The presenter

    public func present(_ input: PresenterInput<TodayApp>) -> TodayPresentation {
        TodayPresenter.present(input.state, linked: linked, build: build, now: input.now, calendar: input.calendar)
    }

    public func nextChange(of presentation: TodayPresentation) -> Date? { presentation.nextChange }

    public func event(atTick instant: Date) -> Event? { .ticked(instant) }
}
