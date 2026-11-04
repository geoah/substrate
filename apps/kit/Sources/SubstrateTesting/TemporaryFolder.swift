import Foundation

/// A folder of its own under the temporary directory, for one test.
public func temporaryFolder(_ label: String = "kit") -> URL {
    let url = FileManager.default.temporaryDirectory.appendingPathComponent("\(label)-\(UUID().uuidString)", isDirectory: true)
    try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
    return url
}
