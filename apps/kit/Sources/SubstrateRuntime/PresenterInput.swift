import Foundation

/// Everything a presenter reads, declared, so a test pins each: the state,
/// the transient state, the children's presentations, the instant, the
/// locale and time zone the environment gives (never the process's: on
/// Android they do not follow the device), the link's snapshot, and why the
/// journal, or one under the controller, cannot be read or written, while
/// it cannot.
public struct PresenterInput<C: Controller> {
    public var state: C.State
    public var transient: C.Transient
    public var children: [ChildKey: C.Child.Presentation]
    public var now: Date
    public var locale: Locale
    public var timeZone: TimeZone
    public var link: LinkSnapshot?
    public var journalProblem: String?
    /// The same for the controllers under this one, the first by key: what
    /// the link says as a change it could not keep.
    public var childJournalProblem: String?

    public init(
        state: C.State, transient: C.Transient, children: [ChildKey: C.Child.Presentation] = [:], now: Date,
        locale: Locale = Locale(identifier: "en_US_POSIX"), timeZone: TimeZone = TimeZone(identifier: "UTC")!,
        link: LinkSnapshot? = nil, journalProblem: String? = nil, childJournalProblem: String? = nil
    ) {
        self.state = state
        self.transient = transient
        self.children = children
        self.now = now
        self.locale = locale
        self.timeZone = timeZone
        self.link = link
        self.journalProblem = journalProblem
        self.childJournalProblem = childJournalProblem
    }

    /// A Gregorian calendar in the input's time zone and locale, for a
    /// presenter that words dates.
    public var calendar: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        calendar.locale = locale
        return calendar
    }
}

public extension PresenterInput where C.Transient == NoTransient {
    init(
        state: C.State, children: [ChildKey: C.Child.Presentation] = [:], now: Date,
        locale: Locale = Locale(identifier: "en_US_POSIX"), timeZone: TimeZone = TimeZone(identifier: "UTC")!,
        link: LinkSnapshot? = nil, journalProblem: String? = nil, childJournalProblem: String? = nil
    ) {
        self.init(
            state: state, transient: NoTransient(), children: children, now: now, locale: locale, timeZone: timeZone,
            link: link, journalProblem: journalProblem, childJournalProblem: childJournalProblem)
    }
}
