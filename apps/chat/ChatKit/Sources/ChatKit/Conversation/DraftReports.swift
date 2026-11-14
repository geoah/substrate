import Foundation

/// What dictation adds to a draft: after it, with a space.
public func joined(_ draft: String, _ heard: String) -> String {
    let words = heard.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !words.isEmpty else { return draft }
    let head = draft.trimmingCharacters(in: .whitespaces)
    return head.isEmpty ? words : (head.hasSuffix("\n") ? head + words : head + " " + words)
}

/// What a composer's field tells its conversation, and when. The text is
/// the field's own while it is typed: a key is no command, so it runs no
/// engine and no presenter. The conversation hears of it after a pause in
/// the typing, never more than `longest` behind it, and at once before a
/// send and when the field stops editing (it lost the focus, its chat left
/// the screen, the app left the front), which is all a send and the disk
/// need.
///
/// The tree carries the draft only as the engine wrote it (a send cleared
/// it, a refusal gave it back, dictation landed, an opener was picked, the
/// chat was shown again, a field left the screen), and says so by moving
/// its revision. The field then takes the tree's text, and what was typed
/// while the rewrite was on its way stays at its end, so a rewrite never
/// costs a key.
public struct DraftReports: Equatable, Sendable {
    /// How long the typing rests before the conversation is told.
    public static let pause: TimeInterval = 0.5
    /// The longest the conversation is left behind while the typing goes on.
    public static let longest: TimeInterval = 2

    /// The text the conversation last heard from this field, or the field
    /// last took from the tree.
    private var told = ""
    /// Typed since, and not yet said.
    private var unsaid: String?
    private var unsaidSince: Date?
    private var typedAt: Date?
    /// The tree's draft revision last taken; nil before the first.
    private var revision: Int?
    /// The field has told the conversation something since it last took the
    /// tree's text: `told` is then its own, and may still be on its way.
    private var spoken = false

    public init() {}

    /// The field's text changed to `text`.
    public mutating func typed(_ text: String, at now: Date) {
        guard text != told else {
            unsaid = nil
            unsaidSince = nil
            typedAt = nil
            return
        }
        if unsaid == nil { unsaidSince = now }
        unsaid = text
        typedAt = now
    }

    /// When the conversation is next told, while something is unsaid.
    public var due: Date? {
        guard unsaid != nil, let typedAt, let unsaidSince else { return nil }
        return min(typedAt.addingTimeInterval(Self.pause), unsaidSince.addingTimeInterval(Self.longest))
    }

    /// The pause ended: what to tell, where it is due.
    public mutating func paused(at now: Date) -> Conversation.Command? {
        guard let due, now >= due else { return nil }
        return say()
    }

    /// What is unsaid, now: before a send, which sends what the
    /// conversation holds, and when the field stops editing.
    public mutating func say() -> Conversation.Command? {
        guard let text = unsaid else { return nil }
        told = text
        spoken = true
        unsaid = nil
        unsaidSince = nil
        typedAt = nil
        return .draft(text)
    }

    /// What the field holds, said whatever was said before: before a send,
    /// which sends what the conversation holds, and that may be what
    /// another window's field told it since.
    public mutating func tell(_ text: String) -> Conversation.Command {
        told = text
        spoken = true
        unsaid = nil
        unsaidSince = nil
        typedAt = nil
        return .draft(text)
    }

    /// The field leaves the screen holding `text`: the conversation carries
    /// it in the tree from here, for the field that comes after.
    public mutating func leave(_ text: String) -> Conversation.Command {
        told = text
        spoken = true
        unsaid = nil
        unsaidSince = nil
        typedAt = nil
        return .draftLeft(text)
    }

    /// The tree's composer as it stands, and the text the field holds.
    /// Where the tree's revision moved since the field last looked, answers
    /// the text the field holds now, nil where it already holds it: the
    /// tree's, and after it whatever the field holds beyond the text the
    /// rewrite stands in place of, which was typed while the rewrite was on
    /// its way.
    ///
    /// That text is the one the field last took from the tree, while it has
    /// told the conversation nothing since. Once it has, the conversation
    /// rewrote what it had heard from the field by then, which the tree
    /// names: the draft before the one rewrite the field missed, or before
    /// the first of several. Text that does not begin with the replaced one
    /// is the field's own, typed over it or never the conversation's (told
    /// or not), and stands as it is.
    ///
    /// `say()` after it tells the conversation what the field then holds.
    /// A field that had spoken always has something to say, since what it
    /// told may have landed after the rewrite and stand in its place.
    public mutating func take(_ composer: ComposerPresentation, holding text: String) -> String? {
        guard composer.draftRevision != revision else { return nil }
        let base: String
        if !spoken {
            base = told
        } else if let revision, composer.draftRevision == revision + 1 {
            base = composer.draftBefore
        } else {
            base = composer.draftReplaced
        }
        var next = composer.draft
        if revision != nil, text != base {
            next = text.hasPrefix(base) ? composer.draft + text.dropFirst(base.count) : text
        }
        let resay = spoken || next != composer.draft
        revision = composer.draftRevision
        told = composer.draft
        spoken = false
        unsaid = nil
        unsaidSince = nil
        typedAt = nil
        if resay {
            // Due at once: the conversation may hold other than the field.
            unsaid = next
            unsaidSince = .distantPast
            typedAt = .distantPast
        }
        return next == text ? nil : next
    }
}
