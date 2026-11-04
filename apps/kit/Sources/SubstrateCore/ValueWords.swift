import Foundation

/// A value as a person reads it in a sentence.
public func valueWords(_ value: JSONValue) -> String {
    switch value {
    case .null: return "Empty"
    case let .string(s): return s.isEmpty ? "Empty" : s
    case let .bool(b): return b ? "Yes" : "No"
    case .number:
        guard let n = value.number else { return value.jsonText }
        return n.rounded() == n && abs(n) < 1e15 ? String(Int(n)) : n.formatted()
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
