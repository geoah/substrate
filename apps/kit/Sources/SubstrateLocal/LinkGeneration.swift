import Foundation

/// The number a sign-out moves, kept in the repository's folder (§5.4).
/// Background work names the generation it was scheduled under and checks
/// it before every write, so a job from before a sign-out writes nothing,
/// even in a process started cold after it.
public struct LinkGeneration: Sendable {
    public let folder: RepositoryFolder

    public init(folder: RepositoryFolder) {
        self.folder = folder
    }

    /// The file at the folder's top that keeps it: an app that empties the
    /// folder at a sign-out keeps this one, or the next sign-in would start
    /// again from 0 and a job from before would pass.
    public static let fileName = "link.json"

    private struct File: Codable {
        var generation: Int
    }

    /// The generation now; 0 for a folder that never had one.
    public var current: Int {
        guard let data = try? folder.read(Self.fileName, in: .root),
            let file = try? JSONDecoder().decode(File.self, from: data)
        else { return 0 }
        return file.generation
    }

    /// Moves the generation, kept on disk before it returns.
    @discardableResult
    public func bump() throws -> Int {
        let next = current + 1
        try folder.write(JSONEncoder().encode(File(generation: next)), to: Self.fileName, in: .root)
        return next
    }

    /// Throws unless `scheduled` is still the generation: the work was
    /// scheduled before a sign-out and must write nothing.
    public func check(_ scheduled: Int) throws {
        let now = current
        guard now == scheduled else { throw LinkGenerationMoved(scheduled: scheduled, current: now) }
    }
}

/// Work scheduled under one link generation found another: a sign-out came
/// between.
public struct LinkGenerationMoved: Error, Equatable, Sendable {
    public var scheduled: Int
    public var current: Int

    public init(scheduled: Int, current: Int) {
        self.scheduled = scheduled
        self.current = current
    }
}
