import Foundation

/// The order a record's properties are listed in, since neither a record
/// nor its declaration keeps the order they were written in (both reach a
/// client as maps): short values first, by name; then dates, earliest first,
/// so a start comes before its end and a check-in before its check-out;
/// then references, then prose, then what is drawn raw.
public func propertyOrder(_ a: String, _ va: JSONValue?, before b: String, _ vb: JSONValue?) -> Bool {
    let (sa, sb) = (ValueShape(va ?? .null), ValueShape(vb ?? .null))
    let (ra, rb) = (propertyRank(sa), propertyRank(sb))
    guard ra == rb else { return ra < rb }
    if case let .date(da, _) = sa, case let .date(db, _) = sb, da != db { return da < db }
    return a < b
}

private func propertyRank(_ shape: ValueShape) -> Int {
    switch shape {
    case .date: 2
    case .reference, .references: 3
    case .longText: 4
    case .raw: 5
    default: 1
    }
}
