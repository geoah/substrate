import Foundation
import SubstrateCore

/// The record a link names (`RecordReference(link:)`): `substrate://<kind>/<id>`
/// (decision 0137), the legacy `ref:<kind>/<id>`, or a bare record path;
/// nil for any other link.
public func linkedRecord(_ url: URL) -> RecordReference? {
    RecordReference(link: url)
}
