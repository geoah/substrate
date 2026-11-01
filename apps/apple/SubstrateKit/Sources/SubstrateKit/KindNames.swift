import Foundation

/// A kind's display name, as the console builds it (web/console's
/// kind-names.ts, whose lists this mirrors): the compound lowercase name a
/// kind declares (`calendareventseries`) split into known words, acronyms
/// and proper names in their own casing. Only ever a label: the identifier
/// is always the full reference (decision 0101).
public enum KindNames {
    /// The words a kind name is split into. A name that does not split fully
    /// reads as the raw name, capitalised: a wrong split is worse than none.
    static let words: Set<String> = [
        "account", "actor", "address", "agent", "alert", "allergy", "analyte", "api",
        "app", "appointment", "attachment", "authority", "batch", "blob", "block", "book",
        "bot", "bridge", "bundle", "calendar", "chat", "client", "code", "comment",
        "communication", "condition", "conduct", "config", "console", "contact", "conversation", "conversion",
        "credential", "cycle", "data", "database", "digest", "dispense", "document", "drive",
        "ecg", "email", "encounter", "event", "export", "file", "function", "gmail",
        "group", "health", "heart", "html", "id", "immunization", "ink", "instruction",
        "interaction", "issue", "item", "key", "kind", "lab", "label", "license",
        "link", "list", "llm", "log", "mapping", "markup", "medication", "merge",
        "message", "metric", "milestone", "mind", "note", "notification", "oauth", "observation",
        "occurrence", "of", "organization", "package", "page", "patch", "patient", "person",
        "policy", "preference", "project", "property", "provider", "pull", "rate", "reaction",
        "reading", "record", "recording", "recovery", "referral", "reminder", "report", "repository",
        "request", "result", "review", "run", "scratchpad", "secret", "series", "setting",
        "share", "sleep", "source", "split", "state", "status", "store", "symptom",
        "sync", "task", "team", "thread", "token", "trace", "trait", "transcript",
        "trigger", "type", "unit", "url", "user", "view", "visit", "web",
        "workflow", "workout",
    ]

    static let acronyms: [String: String] = [
        "api": "API",
        "ecg": "ECG",
        "hrv": "HRV",
        "html": "HTML",
        "id": "ID",
        "iq": "IQ",
        "json": "JSON",
        "llm": "LLM",
        "oauth": "OAuth",
        "url": "URL",
    ]

    static let proper: [String: String] = [
        "applehealth": "Apple Health",
        "beeper": "Beeper",
        "drive": "Drive",
        "github": "GitHub",
        "gmail": "Gmail",
        "google": "Google",
        "linear": "Linear",
        "notion": "Notion",
        "slack": "Slack",
        "whoop": "WHOOP",
    ]

    static let irregular = ["person": "people", "child": "children", "series": "series", "data": "data"]

    private static let longest = words.map(\.count).max() ?? 0

    /// The fewest-words split of `name` into known words, the longer first
    /// word winning a tie; nil when no full split exists.
    public static func split(_ name: String) -> [String]? {
        let chars = Array(name)
        var memo: [Int: [String]?] = [:]
        func from(_ i: Int) -> [String]? {
            if i == chars.count { return [] }
            if let cached = memo[i] { return cached }
            var best: [String]?
            for len in stride(from: min(longest, chars.count - i), to: 0, by: -1) {
                let word = String(chars[i ..< i + len])
                guard words.contains(word), let rest = from(i + len) else { continue }
                if best == nil || rest.count + 1 < best!.count { best = [word] + rest }
            }
            memo[i] = .some(best)
            return best
        }
        return from(0)
    }

    /// One lowercase word as it is displayed: an acronym in its capitals, a
    /// proper name in its own casing, any other word as it came.
    public static func displayWord(_ word: String) -> String {
        let lower = word.lowercased()
        return acronyms[lower] ?? proper[lower] ?? word
    }

    /// One English word's plural.
    public static func pluralWord(_ word: String) -> String {
        let lower = word.lowercased()
        if let irregular = irregular[lower] {
            return word.first?.isUppercase == true ? irregular.capitalizedFirst : irregular
        }
        if lower.hasSuffix("y"), let before = lower.dropLast().last, !"aeiou".contains(before) {
            return String(word.dropLast()) + "ies"
        }
        if ["s", "x", "z", "ch", "sh"].contains(where: lower.hasSuffix) { return word + "es" }
        return word + "s"
    }

    static func displayWords(_ kind: String) -> [String] {
        let name = KindReference(kind)?.name ?? kind.split(separator: "/").last.map(String.init) ?? kind
        return (split(name) ?? [name]).map(displayWord)
    }
}

/// "Calendar event series", "Gmail thread", "Person": a kind reference (or a
/// bare kind name) in words.
public func kindDisplayName(_ kind: String) -> String {
    KindNames.displayWords(kind).joined(separator: " ").capitalizedFirst
}

/// "Calendar event series", "Gmail threads", "People". The last word takes
/// the plural, except in an "X of Y" name, where X does.
public func kindDisplayPlural(_ kind: String) -> String {
    var parts = KindNames.displayWords(kind)
    guard !parts.isEmpty else { return "" }
    let at = parts.firstIndex(of: "of").map { max(0, $0 - 1) } ?? parts.count - 1
    parts[at] = KindNames.pluralWord(parts[at])
    return parts.joined(separator: " ").capitalizedFirst
}

/// A package's own word as a heading: "Tasks", "LLM", "Google".
public func packageDisplayName(_ package: String) -> String {
    (KindNames.split(package) ?? [package]).map(KindNames.displayWord).joined(separator: " ").capitalizedFirst
}

/// `dueAt` → "Due", `assignee` → "Assignee": a property's name as its label.
public func propertyLabel(_ key: String) -> String {
    var words = key.splittingWords().map { $0.lowercased() }
    if words.count > 1, words.last == "at" { words.removeLast() }
    return words.map(KindNames.displayWord).joined(separator: " ").capitalizedFirst
}

/// "3 tasks", "1 task".
public func plural(_ n: Int, _ one: String, _ many: String? = nil) -> String {
    "\(n.formatted()) \(n == 1 ? one : (many ?? one + "s"))"
}

/// "Tasks", "Tasks and Notes", "Tasks, Notes and People".
public func joinWords(_ words: [String]) -> String {
    guard words.count > 1 else { return words.first ?? "" }
    return words.dropLast().joined(separator: ", ") + " and " + words.last!
}

public extension String {
    var capitalizedFirst: String { prefix(1).uppercased() + dropFirst() }

    /// A display name inside a sentence: the first letter down unless the
    /// first word keeps its casing ("API keys", "Gmail threads").
    var lowercasedFirst: String {
        let first = split(separator: " ").first.map(String.init) ?? self
        let proper = KindNames.proper.values.contains { first == $0 || first == KindNames.pluralWord($0) }
        if proper || first.dropFirst().contains(where: \.isUppercase) { return self }
        return prefix(1).lowercased() + dropFirst()
    }

    /// `dueAt` → [due, At]; `snake_case-name` → [snake, case, name].
    func splittingWords() -> [String] {
        var words: [String] = []
        var current = ""
        var previous: Character?
        for ch in self {
            if ch == "_" || ch == "-" || ch == " " {
                if !current.isEmpty { words.append(current) }
                current = ""
            } else if ch.isUppercase, let p = previous, p.isLowercase || p.isNumber {
                words.append(current)
                current = String(ch)
            } else {
                current.append(ch)
            }
            previous = ch
        }
        if !current.isEmpty { words.append(current) }
        return words
    }
}
