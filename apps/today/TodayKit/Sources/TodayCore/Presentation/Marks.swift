import Foundation

// The marks every screen of the pane is made of (the design guide's tile,
// badge, glyph, stripe and pill), as values a view draws and never decides.

/// A flag the owner can set: High, Medium or Low. Urgent is not one of them:
/// it is reserved, and is drawn as a pill (`Pill.Kind.urgent`), never a
/// stripe.
public enum Flag: String, Codable, Hashable, Sendable, CaseIterable {
    case high, medium, low

    /// The word shown only where a flag is chosen: "High".
    public var word: String {
        switch self {
        case .high: "High"
        case .medium: "Medium"
        case .low: "Low"
        }
    }

    /// Its place in the order of the day: High first. No flag sorts after Low.
    public var rank: Int {
        switch self {
        case .high: 0
        case .medium: 1
        case .low: 2
        }
    }
}

/// The stripe on the leading edge of whatever carries a flag: a row, an
/// entry of the strip, a group of what is happening, an item of the plan.
/// No flag is no stripe. `suggested` draws it hollow: a flag the assistant
/// suggests and the owner has not chosen.
public struct Stripe: Codable, Hashable, Sendable {
    public var flag: Flag?
    public var suggested: Bool

    public init(_ flag: Flag?, suggested: Bool = false) {
        self.flag = flag
        self.suggested = suggested && flag != nil
    }

    public static let none = Stripe(nil)
}

/// The bare glyph that says what a thing is, or what a button does. A view
/// maps each to its symbol (the design guide's "glyph" component).
public enum Glyph: String, Codable, Hashable, Sendable, CaseIterable {
    // Kinds.
    case task, meeting, routine, night, moss, urgent, plan
    // States and controls a presentation may ask for.
    case done, edit, add, dictate, send, join, document, link, clock
}

/// A project, wherever it is named: a square with two initials. `suggested`
/// draws it outlined and not filled, for a project the assistant suggests
/// adding. `hue` is 0..<360 and is the same for one project everywhere.
public struct Tile: Codable, Hashable, Sendable {
    public var initials: String
    public var hue: Int
    public var suggested: Bool

    public init(initials: String, hue: Int, suggested: Bool = false) {
        self.initials = initials
        self.hue = hue
        self.suggested = suggested
    }
}

/// A person, wherever one is named: a circle with two initials.
public struct Badge: Codable, Hashable, Sendable {
    public var initials: String
    public var hue: Int

    public init(initials: String, hue: Int) {
        self.initials = initials
        self.hue = hue
    }
}

/// What leads a row, a chip or a page's title: shape says the kind.
public enum Icon: Codable, Hashable, Sendable {
    case tile(Tile)
    case badge(Badge)
    case glyph(Glyph)
}

/// One state word in a capsule, one size everywhere, never tapped. The kind
/// gives its tone and its glyph: sparkles before "Suggested", a flame before
/// "Urgent", a clock before "Stale".
public struct Pill: Codable, Hashable, Sendable {
    public enum Kind: String, Codable, Hashable, Sendable {
        /// Saved, Chosen, Sent, New, a source's name.
        case neutral
        /// What the assistant suggests, in its one hue.
        case suggested
        /// The reserved fourth step.
        case urgent
        /// Something not updated.
        case stale
        /// "To choose", "Yours?", a late task.
        case caution
    }

    public var kind: Kind
    public var word: String

    public init(_ kind: Kind, _ word: String) {
        self.kind = kind
        self.word = word
    }

    public static let suggested = Pill(.suggested, "Suggested")
    public static let urgent = Pill(.urgent, "Urgent")
    public static let stale = Pill(.stale, "Stale")
    public static let new = Pill(.neutral, "New")
}

/// The colour behind the pane: what the moment is, not what the clock says.
public enum Ground: String, Codable, Hashable, Sendable, CaseIterable {
    /// The routine and your health, and nothing from work.
    case waking
    /// The night's one thing, the suggested tasks, the plan, an overlap.
    case planning
    /// Work in hand.
    case working
    /// In a meeting, or one starts within ten minutes.
    case meeting
    /// Something cannot wait.
    case urgent
    /// The day is over.
    case closing
}

/// A project named as a chip: its tile and its name. It opens the project's
/// page. `suggested`: the project the assistant suggests for a task that
/// has none, drawn hollow with the Suggested pill.
public struct ProjectChip: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var tile: Tile
    public var suggested: Bool

    public init(id: String, name: String, tile: Tile, suggested: Bool = false) {
        self.id = id
        self.name = name
        self.tile = tile
        self.suggested = suggested
    }

    public var route: TodayRoute { .project(id) }
}

/// A person named as a chip: the badge and what the chip says ("for Tomas",
/// "Wei asked", or the name alone). It opens the person's page.
public struct PersonChip: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var label: String
    public var badge: Badge

    public init(id: String, label: String, badge: Badge) {
        self.id = id
        self.label = label
        self.badge = badge
    }

    public var route: TodayRoute { .person(id) }
}

/// The chips under a row's title or in a card: its project, who it is for,
/// and the state words that apply.
public struct Chips: Codable, Hashable, Sendable {
    public var project: ProjectChip?
    public var people: [PersonChip]
    public var pills: [Pill]

    public init(project: ProjectChip? = nil, people: [PersonChip] = [], pills: [Pill] = []) {
        self.project = project
        self.people = people
        self.pills = pills
    }

    public static let none = Chips()
    public var isEmpty: Bool { project == nil && people.isEmpty && pills.isEmpty }
}

/// One pill and one sentence, where something has not been updated: a
/// routine nothing has logged for a long time, a source that has not synced.
public struct StaleNote: Codable, Hashable, Sendable {
    public var pill: Pill
    public var text: String

    public init(_ text: String, word: String = "Stale") {
        pill = Pill(.stale, word)
        self.text = text
    }
}
