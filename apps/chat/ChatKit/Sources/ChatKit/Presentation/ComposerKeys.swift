/// What Return does in the composer, by the keys held with it and where the
/// app runs, and what the Send button says of it. The rule is here, away
/// from the view, so a test holds it: the view asks and does what it is told.
///
/// Return makes a new line everywhere, so a message of several lines is
/// typed the same way on every keyboard, and Command-Return sends. A phone's
/// and a tablet's keyboard on the screen has no Command key, so there the
/// button sends.
public enum ComposerKeys {
    /// Where the composer runs, by the keyboard it is typed on.
    public enum Device: String, CaseIterable, Sendable {
        /// A Mac: a hardware keyboard, always.
        case mac
        /// An iPad: its keyboard on the screen, or a hardware one.
        case pad
        /// An iPhone: its keyboard on the screen, a hardware one rarely.
        case phone
    }

    /// The keys held with Return.
    public struct Modifiers: OptionSet, Hashable, Sendable {
        public let rawValue: Int

        public init(rawValue: Int) {
            self.rawValue = rawValue
        }

        public static let command = Modifiers(rawValue: 1 << 0)
        public static let shift = Modifiers(rawValue: 1 << 1)
        public static let option = Modifiers(rawValue: 1 << 2)
        public static let control = Modifiers(rawValue: 1 << 3)
    }

    public enum Action: Equatable, Sendable {
        /// The message goes.
        case send
        /// A new line at the cursor, written by the composer: a Mac's field
        /// takes Return as the end of the edit and makes none.
        case newline
        /// The press is the text field's own. An iPhone's and an iPad's
        /// field makes the new line itself, a Mac's makes one on
        /// Option-Return, and an input method confirming its marked text
        /// takes the Return that confirms it.
        case fieldsOwn
    }

    /// What a press of Return does. `composing` is an input method holding
    /// marked text in the field, which the press confirms.
    ///
    /// Only Command alone sends: a send cannot be taken back and a new line
    /// can, so a chord nobody named (Command-Shift-Return) makes a new line.
    public static func onReturn(_ modifiers: Modifiers, on device: Device, composing: Bool = false) -> Action {
        if composing { return .fieldsOwn }
        if modifiers == .command { return .send }
        switch device {
        case .mac: return modifiers.contains(.option) ? .fieldsOwn : .newline
        case .pad, .phone: return .fieldsOwn
        }
    }

    /// The draft with a new line written at the cursor, and where the
    /// cursor then stands, both counted in characters. `selection` is what
    /// the new line replaces: an empty range is a cursor, and none is the
    /// end of the draft. The cursor is counted in the new text and never
    /// taken as one past where it was: a new line after a carriage return
    /// joins it into one character, and the draft grows by none.
    public static func newline(in draft: String, replacing selection: Range<Int>? = nil) -> (text: String, cursor: Int) {
        let count = draft.count
        let lower = min(max(selection?.lowerBound ?? count, 0), count)
        let upper = min(max(selection?.upperBound ?? count, lower), count)
        let head = String(draft.prefix(lower)) + "\n"
        return (head + draft.dropFirst(upper), head.count)
    }

    /// The Send button's help where a hardware keyboard is expected: what it
    /// does and the keys that do the same. A phone's says nothing, since its
    /// keyboard has no Command key.
    public static func sendHelp(on device: Device) -> String? {
        switch device {
        case .mac, .pad: "Send (⌘Return)"
        case .phone: nil
        }
    }

    /// The same for a screen reader, in words it can say.
    public static func sendHint(on device: Device) -> String? {
        switch device {
        case .mac, .pad: "Command-Return sends too."
        case .phone: nil
        }
    }
}
