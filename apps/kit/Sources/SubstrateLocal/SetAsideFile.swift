import Foundation

/// A file that could not be read, moved beside where it was as
/// `<name>.unreadable-<seconds>.<extension>` so a new one can be written and
/// the old one is kept for a look, never written over. Settings lists them.
public struct SetAsideFile: Codable, Hashable, Sendable {
    public var area: FolderArea
    /// Its name now, in its area.
    public var name: String
    /// When it was set aside.
    public var at: Date

    public init(area: FolderArea, name: String, at: Date) {
        self.area = area
        self.name = name
        self.at = at
    }

    static let marker = ".unreadable-"

    /// The name a file takes when it is set aside at `instant`.
    static func name(for original: String, at instant: Date) -> String {
        let seconds = Int(instant.timeIntervalSince1970)
        let url = URL(fileURLWithPath: original)
        let ext = url.pathExtension
        let stem = ext.isEmpty ? original : url.deletingPathExtension().lastPathComponent
        return ext.isEmpty ? "\(stem)\(marker)\(seconds)" : "\(stem)\(marker)\(seconds).\(ext)"
    }

    /// The instant a set-aside name carries, nil for any other name.
    static func instant(in name: String) -> Date? {
        guard let range = name.range(of: marker) else { return nil }
        let digits = name[range.upperBound...].prefix { $0.isNumber }
        return Int(digits).map { Date(timeIntervalSince1970: TimeInterval($0)) }
    }
}
