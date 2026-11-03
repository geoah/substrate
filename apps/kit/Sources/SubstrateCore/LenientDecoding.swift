import Foundation

public extension KeyedDecodingContainer {
    /// A field's value, or `fallback` when it is absent, null or of another
    /// shape: one odd field never costs a whole document.
    func value<T: Decodable>(_ key: Key, or fallback: T) -> T {
        (try? decodeIfPresent(T.self, forKey: key)) ?? fallback
    }
}
