import Foundation

/// What a property's value is, for drawing it: decided by the declared type
/// where the kind is known, else by the value's own shape.
public enum ValueShape: Equatable, Sendable {
    case empty
    case reference(RecordReference)
    case references([RecordReference])
    case text(String)
    case longText(String)
    case link(URL)
    case date(Date, time: Bool)
    case words(String)
    case raw(String)

    public init(_ value: JSONValue, declared: PropertyDeclaration? = nil) {
        switch value {
        case .null:
            self = .empty
        case let .string(s):
            let type = declared?.type
            if s.isEmpty {
                self = .empty
            } else if type == "url" || type == nil && (s.hasPrefix("https://") || s.hasPrefix("http://")), let url = URL(string: s) {
                self = .link(url)
            } else if type == "date" || type == "datetime" || type == nil, let (date, time) = ValueShape.date(s) {
                self = .date(date, time: time)
            } else if type == "enum" {
                self = .words(declared?.label(of: s) ?? s.splittingWords().map { $0.lowercased() }.joined(separator: " ").capitalizedFirst)
            } else if type == "markdown" || s.count > 140 || s.contains("\n") {
                self = .longText(s)
            } else if type == nil, ValueShape.isIdentifier(s) {
                // Undeclared, an identifier-shaped word is an enum's value
                // far more often than prose: "utility" reads "Utility".
                self = .words(s.splittingWords().map { $0.lowercased() }.joined(separator: " ").capitalizedFirst)
            } else {
                self = .text(s)
            }
        case let .array(items):
            let refs = items.compactMap { RecordReference($0) }
            if items.isEmpty {
                self = .empty
            } else if refs.count == items.count {
                self = .references(refs)
            } else {
                self = .words(items.map(valueWords).joined(separator: ", "))
            }
        case let .object(o):
            if let ref = RecordReference(value) {
                self = .reference(ref)
            } else if o.isEmpty {
                self = .empty
            } else {
                let words = valueWords(value)
                self = words == value.jsonText ? .raw(words) : .words(words)
            }
        default:
            self = .words(valueWords(value))
        }
    }

    /// `utility`, `inProgress`: a lowercase word, camel-cased at most.
    static func isIdentifier(_ s: String) -> Bool {
        guard s.count <= 24, let first = s.first, first.isLowercase, first.isASCII else { return false }
        return s.allSatisfy { $0.isASCII && ($0.isLetter || $0.isNumber) }
    }

    /// An RFC 3339 instant, or a calendar date alone.
    static func date(_ s: String) -> (Date, Bool)? {
        guard s.count >= 10, s.count <= 40, s.first?.isNumber == true else { return nil }
        if s.count == 10 {
            let f = DateFormatter()
            f.locale = Locale(identifier: "en_US_POSIX")
            f.timeZone = TimeZone(identifier: "UTC")
            f.dateFormat = "yyyy-MM-dd"
            return f.date(from: s).map { ($0, false) }
        }
        guard s.contains("T") else { return nil }
        return Timestamp.parse(s).map { ($0, true) }
    }
}

/// A value as a person reads it in a sentence.
public func valueWords(_ value: JSONValue) -> String {
    switch value {
    case .null: return "Empty"
    case let .string(s): return s.isEmpty ? "Empty" : s
    case let .bool(b): return b ? "Yes" : "No"
    case let .number(n): return n.rounded() == n && abs(n) < 1e15 ? String(Int(n)) : n.formatted()
    case let .array(items):
        return items.isEmpty ? "None" : items.map(valueWords).joined(separator: ", ")
    case let .object(o):
        if let ref = RecordReference(value) { return "\(kindDisplayName(ref.kind)) \(ref.recordID)" }
        // A quantity or an amount: the number, then its unit or currency.
        if let n = o["value"] ?? o["amount"], let unit = (o["unit"] ?? o["currency"])?.referenceID {
            return "\(valueWords(n)) \(unit)"
        }
        return value.jsonText
    }
}
