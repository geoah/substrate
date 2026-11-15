import Foundation

/// One line of the diagnostics file a person can share (Settings, Share
/// Diagnostics): a decision the app made, or a time it measured. Which
/// stream dropped and why, how long a parse and a flush took, why a
/// transcript stopped following its end, which revision each feature read,
/// each failure as the value it is, a send's repeats, a mark's writes, a
/// notification posted, held or taken back.
///
/// It never holds a word of a message, a title, a record's name, a
/// server's address or a token, and it cannot be made to: a line is built
/// only from words written in this source (`StaticString`), numbers, and
/// words of a closed set the app names. A word the server said that is in
/// none of them (a cause a newer server sends) is written as `other`. A
/// chat is named by the order the file met it in, never by its id.
public struct Diagnostic: Codable, Hashable, Sendable {
    /// The chat the line is about, as the app names it: a thread's id, or
    /// a conversation's key before it has a thread. Used to number the
    /// chat, and never written.
    var chat: String?
    /// The name the same chat went by before it had its thread.
    var formerly: String?
    /// The line itself, without when and without its chat.
    public private(set) var text: String

    init(_ what: StaticString, chat: String? = nil, formerly: String? = nil) {
        self.chat = chat
        self.formerly = formerly
        text = "\(what)"
    }

    func with(_ name: StaticString, _ value: Int?) -> Diagnostic {
        guard let value else { return self }
        return adding(name, String(value))
    }

    func with(_ name: StaticString, _ value: Bool?) -> Diagnostic {
        guard let value else { return self }
        return adding(name, value ? "yes" : "no")
    }

    /// A time or a wait, to the millisecond.
    func with(_ name: StaticString, seconds: Double?) -> Diagnostic {
        guard let seconds, seconds.isFinite else { return self }
        return adding(name, String(format: "%.3f", seconds))
    }

    func with(_ name: StaticString, milliseconds: Double?) -> Diagnostic {
        guard let milliseconds, milliseconds.isFinite else { return self }
        return adding(name, String(format: "%.2f", milliseconds))
    }

    /// A word written in this source.
    func with(_ name: StaticString, word: StaticString) -> Diagnostic { adding(name, "\(word)") }

    /// A word of one of the app's own closed sets: a case of an enum that
    /// lists its cases, so no type that only wraps a string (a key, an
    /// id) can be handed in for one.
    func with<Word: RawRepresentable & CaseIterable>(_ name: StaticString, _ word: Word?) -> Diagnostic where Word.RawValue == String {
        guard let word else { return self }
        return adding(name, Self.plain(word.rawValue) ? word.rawValue : "other")
    }

    /// A word the server said, where it is one of `known`: any other is
    /// `other`, whatever it says.
    func with(_ name: StaticString, said word: String?, among known: [String]) -> Diagnostic {
        guard let word else { return self }
        return adding(name, known.contains(word) && Self.plain(word) ? word : "other")
    }

    private func adding(_ name: StaticString, _ value: String) -> Diagnostic {
        var copy = self
        copy.text += " \(name)=\(value)"
        return copy
    }

    /// Letters, digits and an underscore: what a word of a closed set is
    /// made of, so no set can carry a sentence in.
    private static func plain(_ word: String) -> Bool {
        !word.isEmpty && word.count <= 40
            && word.unicodeScalars.allSatisfy { $0.isASCII && ($0.properties.isAlphabetic || ("0" ... "9").contains($0) || $0 == "_") }
    }
}

extension Diagnostic {
    /// The closed sets a server's word is held to.
    enum Known {
        static let statuses = ["ok", "error", "overbudget", "stopped", "running", "pending"]
        static let causes = RunCause.allCases.map(\.rawValue)
        static let reasons = RefusalReason.allCases.map(\.rawValue)
        static let cuts = CutBy.allCases.map(\.rawValue)
        /// The API's error codes this build has a use for.
        static let codes = [
            "bad_request", "auth", "forbidden", "not_found", "conflict", "validation", "guard", "rate_limited", "unavailable", "internal",
            "compacted", "insufficient_scope", "bad_gateway", "timeout",
        ]
    }

    /// A failure as the value it is: what was attempted and why, with the
    /// status, the code, the reason and the cause where they are of a
    /// closed set. Never what the server said (`said`) or what the device
    /// saw (`seen`), which are sentences.
    func with(_ failure: Failure?) -> Diagnostic {
        guard let failure else { return self }
        var line = with("attempt", failure.attempt)
        switch failure.why {
        case let .run(status, cause):
            line = line.with("why", word: "run").with("status", said: status, among: Known.statuses).with("cause", said: cause, among: Known.causes)
        case let .cut(by): line = line.with("why", word: "cut").with("by", said: by, among: Known.cuts)
        case .unanswered: line = line.with("why", word: "unanswered")
        case let .http(status, code, reason):
            line = line.with("why", word: "http").with("status", status).with("code", said: code, among: Known.codes)
                .with("reason", said: reason, among: Known.reasons)
        case .offline: line = line.with("why", word: "offline")
        case .unreachable: line = line.with("why", word: "unreachable")
        case .timeout: line = line.with("why", word: "timeout")
        case .streamEnded: line = line.with("why", word: "stream_ended")
        case .sessionEnded: line = line.with("why", word: "session_ended")
        case .needsApproval: line = line.with("why", word: "needs_approval")
        case let .notTheAPI(status): line = line.with("why", word: "not_the_api").with("status", status)
        case .ended: line = line.with("why", word: "ended")
        case .closed: line = line.with("why", word: "closed")
        case .interrupted: line = line.with("why", word: "interrupted")
        case .disk: line = line.with("why", word: "disk")
        }
        return line.with("retry_after", failure.retryAfter).with("unconfirmed", failure.unconfirmed ? true : nil)
    }
}

/// The diagnostics file: a bounded ring of lines in the repository's
/// folder, kept across launches and gone with the folder at a sign-out.
///
/// Two files make the ring: lines are appended to `diagnostics.log`, and
/// when it is full it becomes `diagnostics.old.log`, in place of the one
/// before. So a line costs an append, nothing is rewritten, and the two
/// together hold between one and two files' worth of the newest lines.
struct DiagnosticsFile {
    static let name = "diagnostics.log"
    static let older = "diagnostics.old.log"
    /// One file's size, past which it becomes the older one.
    static let limit = 128 * 1024

    let root: URL
    let writingOptions: Data.WritingOptions
    var limit = DiagnosticsFile.limit

    private var current: URL { root.appendingPathComponent(Self.name) }
    private var before: URL { root.appendingPathComponent(Self.older) }

    /// Appends `lines`, each ending its own line.
    func append(_ lines: [String]) throws {
        guard !lines.isEmpty else { return }
        let data = Data((lines.joined(separator: "\n") + "\n").utf8)
        let manager = FileManager.default
        let size = ((try? manager.attributesOfItem(atPath: current.path))?[.size] as? Int) ?? 0
        if size > 0, size + data.count > limit {
            try? manager.removeItem(at: before)
            try manager.moveItem(at: current, to: before)
        }
        guard manager.fileExists(atPath: current.path) else {
            // Made with the folder's own protection, as its other files are.
            try data.write(to: current, options: writingOptions)
            return
        }
        let handle = try FileHandle(forWritingTo: current)
        defer { try? handle.close() }
        try handle.seekToEnd()
        try handle.write(contentsOf: data)
    }

    /// Every line held, oldest first.
    func lines() -> [String] {
        [before, current].flatMap { url in
            (try? String(contentsOf: url, encoding: .utf8))?.split(separator: "\n", omittingEmptySubsequences: true).map(String.init) ?? []
        }
    }
}

/// The diagnostics as a person is shown them before they share them, and
/// as they are shared: the same text.
public struct DiagnosticsReport: Codable, Equatable, Sendable {
    public var title: String
    /// What the text holds and what it never holds.
    public var note: String
    /// The lines, oldest first, one to a line.
    public var text: String
    /// Said in place of the lines where there are none.
    public var empty: String?
    public var share: String
    public var done: String

    /// The text as it is shown and shared, with the build that wrote it
    /// under its heading: the build's own version, which the device's
    /// side of the app knows.
    public func shared(version: String) -> String {
        var lines = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        lines.insert("version \(version)", at: min(1, lines.count))
        return lines.joined(separator: "\n")
    }

    static let heading = "Substrate Chat diagnostics"
    static let promise =
        "What the app decided and how long things took: streams that dropped, failures by their kind, writes and their retries. "
        + "No message, title, name, address or token is in it."

    init(lines: [String], platform: ChatPlatform, agents: Int?, records: Int?) {
        title = "Diagnostics"
        note = Self.promise
        let head = Diagnostic("report").with("platform", platform).with("agents", agents).with("records", records).with("lines", lines.count)
        text = ([Self.heading, head.text] + lines).joined(separator: "\n")
        empty = lines.isEmpty ? "Nothing has been written yet." : nil
        share = "Share"
        done = "Done"
    }
}
