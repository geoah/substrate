import Foundation
import SubstrateKit
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
    /// It holds an older version, and the owner changed kinds of it since
    /// this app last applied it: these kinds, by their repository names. The
    /// upgrade would apply over the change, so the app waits for the owner
    /// to choose.
    case edited(held: Int, kinds: [String])
    /// The server refused the upgrade, in its words: it would remove what the
    /// package holds, such as a kind or property the owner added. Only the
    /// owner can settle it, on the server.
    case refused(held: Int?, reason: String)

    public var held: Int? {
        switch self {
        case .missing: nil
        case let .behind(held), let .current(held), let .newer(held), let .edited(held, _): held
        case let .refused(held, _): held
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

/// What the app last applied in one repository: the package version and the
/// row version of each of the package's kinds right after, so an owner's
/// later edit is told from the app's own. A kind edit moves the kind and not
/// the package (decision 0203), so the package version alone cannot say.
public struct PackageBaseline: Codable, Sendable, Equatable {
    public var version: Int
    /// Kind id, as the repository spells it, to its row version.
    public var kinds: [String: Int]

    public init(version: Int, kinds: [String: Int]) {
        self.version = version
        self.kinds = kinds
    }

    public static let fileName = "package.json"

    public static func load(from directory: URL) -> PackageBaseline? {
        try? JSONDecoder().decode(PackageBaseline.self, from: Data(contentsOf: directory.appendingPathComponent(fileName)))
    }

    public func save(to directory: URL, writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes, .sortedKeys]
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try encoder.encode(self).write(to: directory.appendingPathComponent(Self.fileName), options: writingOptions)
    }
}

/// Installs and upgrades the app's package in one repository, through the
/// token's `declare:~/training` scope (decision 0203). The approval set up
/// installs `substrate.reamde.dev/health`, which the package requires; the
/// app declares its own package after it.
///
/// The server refuses an apply that would remove anything the package holds
/// (a kind, property or value the owner added), so an upgrade only ever adds.
/// What it would still change is the owner's edit to a kind the app ships,
/// which the app finds by comparing each kind row's version with the one it
/// recorded after its own last apply (`baselineDirectory`); a moved kind, or
/// one it never installed, stops the upgrade at `.edited` until the owner
/// lets it apply over them.
public struct PackageInstaller: Sendable {
    public var api: WorkoutsAPI
    public var manifest: PackageManifest
    /// The folder of this server and repository, where the baseline is kept;
    /// nil keeps none, and then only the server's guard protects the package.
    public var baselineDirectory: URL?

    public init(api: WorkoutsAPI, manifest: PackageManifest, baselineDirectory: URL? = nil) {
        self.api = api
        self.manifest = manifest
        self.baselineDirectory = baselineDirectory
    }

    public var packageID: String { KindNames.packageID(authority: api.rehome.authority) }

    static let kindKind = "substrate.reamde.dev/core/kind"
    static let bundleKind = "substrate.reamde.dev/core/bundle"

    /// The version the repository holds, read from its package row.
    public func state() async throws -> PackageState {
        let row = try await api.packageRow(packageID)
        let held: Int? = row.flatMap { properties -> Int? in
            guard case let .number(version)? = properties["version"] else { return nil }
            return NSDecimalNumber(decimal: version).intValue
        }
        return .of(held: held, shipped: manifest.version)
    }

    /// The kinds this build ships, as the repository spells them.
    public var shippedKinds: [String] {
        manifest.documents(for: api.rehome).compactMap { document -> String? in
            guard case let .object(object) = document, object["kind"] == .string(Self.kindKind),
                  case let .object(metadata)? = object["metadata"], case let .string(id)? = metadata["id"] else { return nil }
            return id
        }
    }

    /// Each kind of the package the repository holds, with its row version:
    /// the kinds its bundle lists, else the ones this build ships.
    public func kindVersions() async throws -> [String: Int] {
        var ids = shippedKinds
        if let bundle = try await api.declarationRow(Self.bundleKind, packageID), case let .array(installs)? = bundle.properties["installs"] {
            ids = installs.compactMap { if case let .string(id) = $0 { id } else { nil } }
        }
        var out: [String: Int] = [:]
        for id in ids {
            if let row = try await api.declarationRow(Self.kindKind, id) { out[id] = row.version }
        }
        return out
    }

    /// Installs or upgrades as the state asks, and answers the state after.
    /// A newer version is left alone; an owner's edit stops an upgrade
    /// unless `overwritingEdits`, which the owner chose; a refusal of the
    /// server's guard comes back as `.refused`, in its words.
    @discardableResult
    public func ensure(overwritingEdits: Bool = false) async throws -> PackageState {
        let before = try await state()
        let baseline = baselineDirectory.flatMap(PackageBaseline.load(from:))
        switch before {
        case .newer, .edited, .refused:
            return before
        case let .current(held):
            if baseline == nil { try? await record(version: held) }
            return before
        case let .behind(held):
            if !overwritingEdits {
                let edited = try await edits(since: baseline)
                if !edited.isEmpty { return .edited(held: held, kinds: edited) }
            }
        case .missing:
            break
        }
        do {
            try await api.applyVocabulary(manifest.documents(for: api.rehome))
        } catch let error as ServerError where error.status == 409 {
            return .refused(held: before.held, reason: error.message)
        }
        let after = try await state()
        if let held = after.held { try? await record(version: held) }
        return after
    }

    /// The kinds the owner changed since the baseline: a row at another
    /// version, or one the baseline does not hold. Without a baseline (the
    /// package was installed by a build that kept none), only kinds this
    /// build does not ship are told apart.
    func edits(since baseline: PackageBaseline?) async throws -> [String] {
        let held = try await kindVersions()
        guard let baseline else {
            let shipped = Set(shippedKinds)
            return held.keys.filter { !shipped.contains($0) }.sorted()
        }
        return held.filter { baseline.kinds[$0.key] != $0.value }.map(\.key).sorted()
    }

    private func record(version: Int) async throws {
        guard let baselineDirectory else { return }
        try PackageBaseline(version: version, kinds: try await kindVersions()).save(to: baselineDirectory)
    }
}
