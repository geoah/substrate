import Foundation
import SubstrateDevice

/// App folders under the temporary directory, one pair per test.
public struct TemporaryFolders: AppFolders {
    public let root: URL

    public init(root: URL = temporaryFolder("folders")) {
        self.root = root
    }

    public func data() throws -> URL {
        let url = root.appendingPathComponent("data", isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    public func cache() throws -> URL {
        let url = root.appendingPathComponent("cache", isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    public var writingOptions: Data.WritingOptions { [] }
}
