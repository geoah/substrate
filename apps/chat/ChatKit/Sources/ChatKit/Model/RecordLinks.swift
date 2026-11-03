import Foundation
import SubstrateCore

/// The record a `substrate://<kind>/<id>` link names (decision 0137), or
/// one in the legacy `ref:<kind>/<id>` spelling; nil for any other link.
public func linkedRecord(_ url: URL) -> RecordReference? {
    let raw = url.absoluteString
    for scheme in ["substrate://", "ref:"] where raw.lowercased().hasPrefix(scheme) {
        let path = String(raw.dropFirst(scheme.count))
        return RecordReference(path: path.removingPercentEncoding ?? path)
    }
    return nil
}
