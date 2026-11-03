import Foundation
import SubstrateAPI
import SubstrateCore

/// Where a repository stands with a package an app needs, read off the
/// package row the server keeps. The server installs and upgrades it
/// (decisions 0212 and 0213); the app only reads where it stands, and writes
/// records of the package only while it is current.
public enum HeldPackage: Codable, Hashable, Sendable {
    /// The repository has no such package, or the token cannot see its row:
    /// the approval installs it.
    case missing
    /// An older version than the build was written against: what the build
    /// writes could name a kind or a property it lacks.
    case behind(held: Int)
    /// The build's version or a later one, which is additive.
    case current(held: Int)

    public var held: Int? {
        switch self {
        case .missing: nil
        case let .behind(held), let .current(held): held
        }
    }

    public var writable: Bool {
        if case .current = self { return true }
        return false
    }

    public init(held: Int?, needed: Int) {
        guard let held else {
            self = .missing
            return
        }
        self = held < needed ? .behind(held: held) : .current(held: held)
    }

    /// The version the repository holds: `shippedVersion`, the publisher's
    /// number a published install stamps, else the row's own `version`.
    public static func read(_ package: String, needing needed: Int, from client: SubstrateClient) async throws -> HeldPackage {
        guard let row = try await client.record("substrate.reamde.dev/core/package", package) else { return .missing }
        let held = ["shippedVersion", "version"].lazy.compactMap { key -> Int? in
            guard case let .number(version)? = row.properties[key] else { return nil }
            return NSDecimalNumber(decimal: version).intValue
        }.first
        return HeldPackage(held: held, needed: needed)
    }
}
