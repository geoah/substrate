import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import WorkoutsCore

/// A package as this build carries it (`Resources/Package/training.json` and
/// `coach.json`, written by apps/workouts/scripts/workouts-package.py from
/// SubstrateWorkouts/Package/): its id, the version this build was written
/// against, and the documents of its closure. The app applies none of them:
/// a repository installs the package from the publisher's authority
/// document, and the build asks for its version as the floor of a `package`
/// detail (decision record 0214).
public struct PackageManifest: Sendable {
    /// `substrate.reamde.dev/training` or `substrate.reamde.dev/trainingcoach`.
    public var package: String
    /// The version this build declares. Each release that changes a
    /// declaration ships the next one, additive only.
    public var version: Int
    public var documents: [JSONValue]

    public init(package: String, version: Int, documents: [JSONValue]) {
        self.package = package
        self.version = version
        self.documents = documents
    }

    /// The training package.
    public static func bundled() throws -> PackageManifest { try bundled("training") }

    /// The coach's package: its agent in the closure.
    public static func coach() throws -> PackageManifest { try bundled("coach") }

    static func bundled(_ name: String) throws -> PackageManifest {
        guard let url = BundledCatalog.resourceBundle.url(forResource: name, withExtension: "json", subdirectory: "Package") else {
            throw CatalogError.missingResource("Package/\(name).json")
        }
        return try decode(Data(contentsOf: url))
    }

    static func decode(_ data: Data) throws -> PackageManifest {
        guard case let .object(object) = try JSONDecoder().decode(JSONValue.self, from: data),
            case let .string(package)? = object["package"],
            case let .number(version)? = object["version"],
            case let .array(documents)? = object["documents"]
        else { throw CatalogError.missingResource("Package/*.json is not a manifest file") }
        return PackageManifest(package: package, version: version.int ?? 0, documents: documents)
    }

    /// The `data` of every document of `kind`, with its id.
    public func documents(ofKind kind: String) -> [(id: String, data: JSONObject)] {
        documents.compactMap { document in
            guard case let .object(object) = document, object["kind"] == .string(kind),
                case let .object(metadata)? = object["metadata"], case let .string(id)? = metadata["id"],
                case let .object(data)? = object["data"]
            else { return nil }
            return (id, data)
        }
    }
}

/// Where a repository stands with a package this build needs, read off the
/// package row the server keeps. The server installs and upgrades the
/// package (decisions 0212 and 0213); the app only reads where it stands.
public enum PackageState: Codable, Sendable, Hashable {
    /// The repository has no such package, or the token cannot see its row:
    /// the approval installs it, so the set up check says what to do.
    case missing
    /// It holds an older version than this build was written against. The
    /// app writes nothing until the server has upgraded it: what it writes
    /// could name a kind or property the held version lacks.
    case behind(held: Int)
    /// It holds this build's version or a later one. A later one is
    /// additive, so this build keeps writing validly and ignores what it
    /// does not know.
    case current(held: Int)

    public var held: Int? {
        switch self {
        case .missing: nil
        case let .behind(held), let .current(held): held
        }
    }

    /// Whether the app may write records of the package.
    public var writable: Bool {
        if case .current = self { return true }
        return false
    }

    static func of(held: Int?, needed: Int) -> PackageState {
        guard let held else { return .missing }
        return held < needed ? .behind(held: held) : .current(held: held)
    }
}

/// Reads where one repository stands with a package, and asks the server
/// for the upgrade a build behind it needs: the set up check names the
/// build's floor, the server applies a kinds-only additive upgrade itself
/// and holds anything else for the owner's review (decision 0213).
public struct PackageCheck: Sendable {
    public var api: WorkoutsAPI
    public var manifest: PackageManifest

    public init(api: WorkoutsAPI, manifest: PackageManifest) {
        self.api = api
        self.manifest = manifest
    }

    public var packageID: String { manifest.package }

    /// The version the repository holds, read from its package row:
    /// `shippedVersion`, the publisher's number a published install stamps,
    /// else the row's own `version`.
    public func state() async throws -> PackageState {
        guard let row = try await api.packageRow(packageID) else { return .missing }
        let held = ["shippedVersion", "version"].lazy.compactMap { key -> Int? in
            guard case let .number(version)? = row[key] else { return nil }
            return version.int
        }.first
        return .of(held: held, needed: manifest.version)
    }

    /// Where the repository stands, after asking the server for the upgrade
    /// where it is behind: `check` runs the set up check, which carries the
    /// build's floor, and the row is read again after it. A package still
    /// behind then waits for the owner, which the check's own issues say.
    /// The check is the app's status model, which lives on the main actor.
    @discardableResult
    public func ensure(asking check: @MainActor () async -> Void) async throws -> PackageState {
        let before = try await state()
        guard case .behind = before else { return before }
        await check()
        return try await state()
    }
}
