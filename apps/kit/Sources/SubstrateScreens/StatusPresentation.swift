import Foundation
import SubstrateCore

/// Where the app stands with its repository (§5.6's Status): a sentence,
/// why where there is a reason (the server's or the system's words, never
/// run into the sentence), each package, what the owner can do about it,
/// and the writes the server refused, each with Retry and Discard.
public struct StatusPresentation: Bridged, Hashable, Sendable {
    public var title: String
    public var symbol: ScreenSymbol
    public var tone: ScreenTone
    public var sentence: String
    public var problem: ProblemPresentation?
    /// One row per package the app needs.
    public var packages: [PackageLine]
    public var actions: [ActionButton<StatusAction>]
    /// Whether a send is running now.
    public var busy: Bool
    public var footer: String?
    public var refused: RefusedWrites?
    /// What an app's column says while a refused write waits for the owner,
    /// so it is seen outside Settings: "1 change needs you".
    public var notice: NoticeLine?

    public init(title: String, symbol: ScreenSymbol, tone: ScreenTone, sentence: String, problem: ProblemPresentation? = nil,
                packages: [PackageLine], actions: [ActionButton<StatusAction>], busy: Bool, footer: String?, refused: RefusedWrites?,
                notice: NoticeLine? = nil)
    {
        self.notice = notice
        self.title = title
        self.symbol = symbol
        self.tone = tone
        self.sentence = sentence
        self.problem = problem
        self.packages = packages
        self.actions = actions
        self.busy = busy
        self.footer = footer
        self.refused = refused
    }
}

/// What the owner can do from the Status screen.
public enum StatusAction: Bridged, Hashable, Sendable {
    case sendNow
    case review
    case signIn
    case renew
    case retry(UUID)
    case discard(UUID)
}

/// The writes the server refused, listed for the owner.
public struct RefusedWrites: Bridged, Hashable, Sendable {
    public var title: String
    public var rows: [RefusedWrite]
    public var footer: String

    public init(title: String, rows: [RefusedWrite], footer: String) {
        self.title = title
        self.rows = rows
        self.footer = footer
    }
}

/// One refused write: what it was of, what was refused, its record, the
/// server's words.
public struct RefusedWrite: Bridged, Hashable, Sendable, Identifiable {
    public var id: UUID
    /// The kind of record, in the app's words: "Training session", "Task".
    public var label: String
    /// What was refused: "Write refused", "Change refused", "Delete refused".
    public var status: String
    public var recordID: String
    public var words: String?
    public var retry: ActionButton<StatusAction>
    public var discard: ActionButton<StatusAction>

    public init(id: UUID, label: String, status: String, recordID: String, words: String?, retry: ActionButton<StatusAction>,
                discard: ActionButton<StatusAction>)
    {
        self.id = id
        self.label = label
        self.status = status
        self.recordID = recordID
        self.words = words
        self.retry = retry
        self.discard = discard
    }
}

/// A package the app needs, as the Status screen lists it: "Training
/// package", "Version 2".
public struct PackageLine: Bridged, Hashable, Sendable, Identifiable {
    /// The package's id.
    public var id: String
    public var row: LabeledValue

    public init(id: String, row: LabeledValue) {
        self.id = id
        self.row = row
    }
}
