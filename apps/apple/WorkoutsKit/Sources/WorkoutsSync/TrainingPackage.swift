import Foundation
import WorkoutsCore

/// The training package's manifests as this build carries them
/// (`Resources/Package/training.json`, written by
/// apps/apple/scripts/workouts-package.py from SubstrateWorkouts/Package/):
/// the package under `~`, its version, and the documents one apply takes.
public struct PackageManifest: Sendable {
    /// `~/training`.
    public var package: String
    /// The version this build declares. Each release that changes a
    /// declaration ships the next one, additive only.
    public var version: Int
    public var documents: [WorkoutsCore.JSONValue]

    public init(package: String, version: Int, documents: [WorkoutsCore.JSONValue]) {
        self.package = package
        self.version = version
        self.documents = documents
    }

    /// The manifests in WorkoutsCore's resources.
    public static func bundled() throws -> PackageManifest {
        guard let url = BundledCatalog.resourceBundle.url(forResource: "training", withExtension: "json", subdirectory: "Package") else {
            throw CatalogError.missingResource("Package/training.json")
        }
        return try decode(Data(contentsOf: url))
    }

    static func decode(_ data: Data) throws -> PackageManifest {
        guard case let .object(object) = try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: data),
              case let .string(package)? = object["package"],
              case let .number(version)? = object["version"],
              case let .array(documents)? = object["documents"]
        else { throw CatalogError.missingResource("Package/training.json is not a manifest file") }
        return PackageManifest(package: package, version: NSDecimalNumber(decimal: version).intValue, documents: documents)
    }

    /// The documents in one repository's spelling.
    public func documents(for rehome: Rehome) -> [WorkoutsCore.JSONValue] { documents.map(rehome.outbound) }
}

/// Where a repository stands with the app's package.
public enum PackageState: Sendable, Hashable {
    /// The repository has no training package: the app installs it.
    case missing
    /// It holds an older version than this build declares: the app
    /// upgrades it, additively.
    case behind(held: Int)
    /// It holds this build's version.
    case current(held: Int)
    /// It holds a newer version than this build knows, from a newer build or
    /// an edit on the server. The app writes nothing until it is updated:
    /// what it writes could miss what the newer version requires.
    case newer(held: Int)

    public var held: Int? {
        switch self {
        case .missing: nil
        case let .behind(held), let .current(held), let .newer(held): held
        }
    }

    /// Whether the app may write records of the package.
    public var writable: Bool {
        if case .current = self { return true }
        return false
    }

    static func of(held: Int?, shipped: Int) -> PackageState {
        guard let held else { return .missing }
        if held < shipped { return .behind(held: held) }
        if held > shipped { return .newer(held: held) }
        return .current(held: held)
    }
}

/// Installs and upgrades the app's package in one repository, through the
/// token's `declare:~/training` scope (decision 0203). The approval set up
/// installs `substrate.reamde.dev/health`, which the package requires; the
/// app declares its own package after it.
public struct PackageInstaller: Sendable {
    public var api: WorkoutsAPI
    public var manifest: PackageManifest

    public init(api: WorkoutsAPI, manifest: PackageManifest) {
        self.api = api
        self.manifest = manifest
    }

    public var packageID: String { KindNames.packageID(authority: api.rehome.authority) }

    /// The version the repository holds, read from its package row.
    public func state() async throws -> PackageState {
        let row = try await api.packageRow(packageID)
        let held: Int? = row.flatMap { properties -> Int? in
            guard case let .number(version)? = properties["version"] else { return nil }
            return NSDecimalNumber(decimal: version).intValue
        }
        return .of(held: held, shipped: manifest.version)
    }

    /// Installs or upgrades as the state asks, and answers the state after.
    /// A newer version is left alone. A change the server would have to
    /// confirm as lossy is refused to an app's token, so an upgrade that is
    /// not additive fails here, in the server's words, and changes nothing.
    @discardableResult
    public func ensure() async throws -> PackageState {
        let before = try await state()
        switch before {
        case .current, .newer:
            return before
        case .missing, .behind:
            try await api.applyVocabulary(manifest.documents(for: api.rehome))
            return try await state()
        }
    }
}
