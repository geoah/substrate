import Foundation

// The controls of the pane (the design guide's option list, button, save
// card and "ready to send" message), as values.

/// The one control for a choice: a list of rows, one option each, every row
/// always there. Round marks when one can be chosen, square when several
/// can. Tapping a row sends its command; choosing hides, moves, closes and
/// sends nothing.
public struct OptionList: Codable, Equatable, Hashable, Sendable {
    public enum Mark: String, Codable, Hashable, Sendable {
        /// One of them.
        case round
        /// Any of them.
        case square
    }

    public var mark: Mark
    public var rows: [OptionRow]

    public init(_ mark: Mark = .round, _ rows: [OptionRow]) {
        self.mark = mark
        self.rows = rows
    }

    public static let empty = OptionList(.round, [])
    public var isEmpty: Bool { rows.isEmpty }
    /// The row drawn filled, for a round list.
    public var selected: OptionRow? { rows.first { $0.selected } }
}

/// One option. `selected` is what is drawn filled: above a Save button it
/// may be the assistant's default; where a tap takes effect at once it is
/// only what the owner chose. `suggested` carries the Suggested pill and
/// `reason` the assistant's one sentence, whether or not the row is
/// selected.
public struct OptionRow: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    /// The option's name.
    public var name: String
    /// One line saying what choosing it means.
    public var line: String?
    public var selected: Bool
    /// The assistant suggests this one.
    public var suggested: Bool
    /// Why it suggests it, drawn in its hue under the line.
    public var reason: String?
    /// A message's text, where the row is a message that may be sent.
    public var quote: String?
    /// What leads the row after its mark: a flag's swatch, a project's tile.
    public var stripe: Stripe?
    public var icon: Icon?
    /// What a tap sends.
    public var command: TodayCommand

    public init(
        id: String, name: String, line: String? = nil, selected: Bool = false, suggested: Bool = false, reason: String? = nil,
        quote: String? = nil, stripe: Stripe? = nil, icon: Icon? = nil, command: TodayCommand
    ) {
        self.id = id
        self.name = name
        self.line = line
        self.selected = selected
        self.suggested = suggested
        self.reason = reason
        self.quote = quote
        self.stripe = stripe
        self.icon = icon
        self.command = command
    }
}

/// Anything that does something, drawn as a button: a filled, tinted or
/// outlined shape. A button that opens a page ends with a right chevron
/// (`opens` is set); the view adds it.
public struct ButtonPresentation: Codable, Equatable, Hashable, Sendable {
    public enum Role: String, Codable, Hashable, Sendable {
        /// Filled: Done, Taken, Join, Save, Open the plan. One on a card.
        case primary
        /// Tinted: Got it, Send, Open them again.
        case secondary
        /// Outlined: Not now.
        case quiet
        /// Remove: the view asks before it sends.
        case destructive
    }

    public var title: String
    /// The glyph before the word; alone, with no word, it is an icon button
    /// and `title` is its accessibility label.
    public var glyph: Glyph?
    public var iconOnly: Bool
    public var role: Role
    /// Across the card: the one action of a save card or the plan's row.
    public var wide: Bool
    public var enabled: Bool
    /// What a tap sends; nil when the button only opens a page or a link.
    public var command: TodayCommand?
    /// The page it opens.
    public var opens: TodayRoute?
    /// The address it opens outside the app: a meeting's join link.
    public var url: String?

    public init(
        _ title: String, glyph: Glyph? = nil, iconOnly: Bool = false, role: Role = .secondary, wide: Bool = false, enabled: Bool = true,
        command: TodayCommand? = nil, opens: TodayRoute? = nil, url: String? = nil
    ) {
        self.title = title
        self.glyph = glyph
        self.iconOnly = iconOnly
        self.role = role
        self.wide = wide
        self.enabled = enabled
        self.command = command
        self.opens = opens
        self.url = url
    }
}

/// The end of a save card: one button that says what it saves, and after it
/// one line saying what was saved and when. Nothing above the button takes
/// effect until it is pressed.
public struct SaveRow: Codable, Equatable, Hashable, Sendable {
    /// "Save 13 answers". Nil once there is nothing left to save.
    public var button: ButtonPresentation?
    /// "Saved at 08:38. Twelve made tasks and one dismissed."
    public var saved: String?

    public init(button: ButtonPresentation?, saved: String? = nil) {
        self.button = button
        self.saved = saved
    }
}

/// A message a choice needs, shown ready under its list. Choosing never
/// sends: Send is its own button.
public struct ReadyMessage: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    /// "Ready to send to Karim", or "Sent to Karim".
    public var heading: String
    public var text: String
    /// The Send button, until it is sent.
    public var send: ButtonPresentation?
    /// "Nothing is sent until you press Send.", or after it "Sent at 08:44.
    /// No answer yet."
    public var note: String?
    public var sent: Bool

    public init(id: String, heading: String, text: String, send: ButtonPresentation?, note: String?, sent: Bool = false) {
        self.id = id
        self.heading = heading
        self.text = text
        self.send = send
        self.note = note
        self.sent = sent
    }
}

/// A flag, where it can be changed: the stripe as it stands, and the three
/// options (never Urgent). On a row the control is the flag icon button at
/// its end, which opens the list under the row; in a card it is a "Flag"
/// door.
public struct FlagControl: Codable, Equatable, Hashable, Sendable {
    public var stripe: Stripe
    /// "High", "No flag yet": said only where the flag is being chosen.
    public var word: String
    public var options: OptionList
    /// Lines under the list: that choosing a flag takes urgent off, that
    /// the order follows when the list is closed.
    public var notes: [String]
    /// The list is drawn open without a tap: the assistant suggests another
    /// flag, and its three options stay open until the owner chooses.
    public var open: Bool

    public init(stripe: Stripe, word: String, options: OptionList, notes: [String] = [], open: Bool = false) {
        self.stripe = stripe
        self.word = word
        self.options = options
        self.notes = notes
        self.open = open
    }
}

/// A line of an open task that opens its option list in place: "Flag",
/// "Project", "When". Its chevron points down, and up while open.
public struct Door: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    /// "Flag", "Project", "When".
    public var label: String
    /// What it stands at: "High", "Lantern", "Today, where it is".
    public var value: String
    public var stripe: Stripe?
    public var icon: Icon?
    public var pills: [Pill]
    public var options: OptionList
    public var notes: [String]

    public init(
        id: String, label: String, value: String, stripe: Stripe? = nil, icon: Icon? = nil, pills: [Pill] = [], options: OptionList,
        notes: [String] = []
    ) {
        self.id = id
        self.label = label
        self.value = value
        self.stripe = stripe
        self.icon = icon
        self.pills = pills
        self.options = options
        self.notes = notes
    }
}

/// A long text where it stands without having been asked for: its opening
/// words, shown until all of it is asked for, and the words of the button
/// that shows all of it in place and of the one that puts it back.
public struct Brief: Codable, Equatable, Hashable, Sendable {
    public var text: String
    /// "Show all".
    public var more: String
    /// "Show less".
    public var less: String

    public init(text: String, more: String = "Show all", less: String = "Show less") {
        self.text = text
        self.more = more
        self.less = less
    }
}
