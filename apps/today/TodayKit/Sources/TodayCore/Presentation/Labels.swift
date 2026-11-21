import Foundation

// The fixed words of the screens: the label over each part of a card or a
// page, and the name of each control that is drawn as an icon alone. They
// are presentation, as every other word is, so a view spells none.

/// The labels over the parts of the wrap.
public struct WrapLabels: Codable, Equatable, Hashable, Sendable {
    public var done = "Done today"
    public var moved = "Moved earlier today"
    public var carry = "Carries over: where does each go?"
    public var unanswered = "Suggested, and not answered"
    public var dismissed = "Dismissed today"
    public var tomorrow = "Tomorrow"

    public init() {}
}

/// The labels over the parts of a meeting in full.
public struct MeetingLabels: Codable, Equatable, Hashable, Sendable {
    public var since = "Since you last met"
    public var said = "What they said"
    public var raise = "To raise"
    public var readFirst = "To read first"
    public var after = "After"

    public init() {}
}

/// The labels over the parts of a project's and a person's page.
public struct PageLabels: Codable, Equatable, Hashable, Sendable {
    public var tasks = "Tasks"
    public var done = "Done"
    public var notToday = "Not today"
    public var suggested = "Suggested"
    public var meetings = "Meetings today"
    public var timeline = "Timeline"

    public init() {}
}

/// What each icon button is called: said to a screen reader, and shown
/// where a pointer rests on it.
public struct ControlWords: Codable, Equatable, Hashable, Sendable {
    public var back = "Back"
    public var close = "Close"
    public var send = "Send"
    public var add = "Add"
    public var dictate = "Dictate"
    public var settings = "Settings"

    public init() {}
}
