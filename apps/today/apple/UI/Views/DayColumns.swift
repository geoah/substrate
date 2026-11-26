import CoreGraphics

/// How many columns the day's screen is drawn in, from the room its window
/// has. The day is always the first. With room for two, the conversation
/// with the assistant stands beside it; with room for three, the projects
/// (or the page that is open) stand between them. A window changes its
/// columns as it is resized, as an iPad is turned, and as an iPhone Duo is
/// opened and closed.
enum DayColumns: Equatable, Sendable {
    /// The day alone. A page covers it, and a conversation stands over it
    /// as a sheet.
    case one
    /// The day with the projects or a page beside it, where the screen has
    /// no conversation to show in a column: a Mac, a drawing.
    case beside
    /// The day, and the conversation beside it. A page covers the day.
    case chat
    /// The day, the projects or the open page, and the conversation.
    case all

    /// The narrowest window that holds the day and the conversation side by
    /// side: an iPhone Duo opened, flat (951 points) or turned upright
    /// (669), and an iPad mini held upright (744) do; an iPhone Duo closed
    /// (466) and every other iPhone held upright do not.
    static let twoWidth: CGFloat = 660
    /// The narrowest that holds all three: an iPad on its side, from the
    /// mini (1133) up. A 13-inch iPad held upright (1032) has two.
    static let threeWidth: CGFloat = 1100
    /// The narrowest that held the day and a page side by side before the
    /// conversation had a column, and still does where there is none.
    static let besideWidth: CGFloat = 900
    /// A window lower than this has no column for the conversation however
    /// wide it is, and keeps the columns it had before there was one: an
    /// iPhone on its side, where the conversation would be a few lines tall.
    static let leastHeight: CGFloat = 500

    /// - Parameters:
    ///   - height: the window's whole height, with what the keyboard
    ///     covers: a keyboard that comes up must not change the columns
    ///     under the person typing.
    ///   - chat: whether the screen has a conversation to show in a column.
    static func fit(width: CGFloat, height: CGFloat, chat: Bool) -> DayColumns {
        guard chat, height >= leastHeight else { return width >= besideWidth ? .beside : .one }
        if width >= threeWidth { return .all }
        if width >= twoWidth { return .chat }
        return .one
    }

    /// Whether the conversation has a column of its own.
    var showsChat: Bool { self == .chat || self == .all }

    /// Whether a conversation may have a column at all on this platform.
    /// An iPhone's and an iPad's does. A Mac's stands over the day as a
    /// sheet, as it did before there were columns.
    static var chatHasColumn: Bool {
        #if os(macOS)
        false
        #else
        true
        #endif
    }

    /// The width of the conversation's column in a window `width` wide: a
    /// little under half beside the day, a little under a third of three,
    /// never narrower than a small iPhone nor wider than is read with ease.
    /// The day, and the projects with it, have the rest.
    func chatWidth(in width: CGFloat) -> CGFloat {
        switch self {
        case .one, .beside: 0
        case .chat: min(max(width * 0.46, 320), 520)
        case .all: min(max(width * 0.31, 340), 520)
        }
    }

    /// The width of the middle column of three, from what the conversation
    /// leaves (`rest`): somewhat under half of it, so the day is the widest.
    func sideWidth(in rest: CGFloat) -> CGFloat {
        self == .all ? min(max(rest * 0.46, 320), 480) : 0
    }
}
