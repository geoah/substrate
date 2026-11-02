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
    /// Kinds of it were changed outside the app since it last applied it:
    /// these kinds, by their repository names. The training kinds are the
    /// app's (O1/O2, owner 2026-11-02), so it writes nothing to the
    /// repository, neither the package nor records, and sessions stay on
    /// the phone. There is no applying over the change from the app; the
    /// server is to refuse an owner's edit to an app's kinds, so this
    /// becomes rare.
    case edited(held: Int, kinds: [String])
    /// The server refused the upgrade, in its words: it would remove what the
    /// package holds, such as a kind or property the owner added. Only the
    /// owner can settle it, on the server.
    case refused(held: Int?, reason: String)
    /// The package is not this app's any more, or never was (decision 0205):
    /// the owner took it over, or another app declared it. The server refuses
    /// the app's apply with a 403, so the app stops applying, and keeps
    /// sending records, which the server still takes. Read off the package
    /// row on every check, so it is never re-applied in a loop.
    case managedElsewhere(held: Int?, by: PackageManager)

    public var held: Int? {
        switch self {
        case .missing: nil
        case let .behind(held), let .current(held), let .newer(held), let .edited(held, _): held
        case let .refused(held, _), let .managedElsewhere(held, _): held
        }
    }

    /// Whether the app may write records of the package. A package managed
    /// elsewhere still takes the app's records: only its kinds are closed to
    /// the app.
    public var writable: Bool {
        switch self {
        case .current, .managedElsewhere: true
        default: false
        }
    }

    static func of(held: Int?, shipped: Int) -> PackageState {
        guard let held else { return .missing }
        if held < shipped { return .behind(held: held) }
        if held > shipped { return .newer(held: held) }
        return .current(held: held)
    }
}

/// Who keeps a training package that is not this app's (decision 0205).
public enum PackageManager: Sendable, Hashable {
    /// The owner: they took it over (from the app named, by its client id,
    /// where the row says), or declared it themselves.
    case owner(takenOverFrom: String?)
    /// Another app declared it: its client id, or its `app:<host>` actor.
    case otherApp(String)
    /// The server refused the app's apply as not its package, in its words,
    /// and the row does not say who keeps it.
    case unknown(String)

    /// Who keeps the package, from its row's ownership stamps, or nil when
    /// the row says it is this app's or says nothing. `clientID` is this
    /// app's; without it only a takeover or a package the owner declared is
    /// told apart.
    ///
    /// The full client id in `declaredByClient` decides, as the server's own
    /// rule does (decision 0205): the `app:` actor in `declaredBy` is a
    /// display name the server may spell as the verified authority
    /// (`app:substrate.reamde.dev`), as `app:local` or as the client id's
    /// host, so it never tells two apps apart. Only a row without the client
    /// id, which an app declared before the server stamped it, falls back to
    /// the actor of the client id's host: one this app's host declared is
    /// this app's, and its apply claims it.
    static func of(_ row: JSONObject, clientID: String?) -> PackageManager? {
        func string(_ key: String) -> String? {
            if case let .string(value)? = row[key], !value.isEmpty { return value }
            return nil
        }
        if let from = string("takenOverFrom") { return .owner(takenOverFrom: from) }
        if let client = string("declaredByClient") {
            guard let clientID else { return nil }
            return client == clientID ? nil : .otherApp(client)
        }
        guard let by = string("declaredBy") else { return nil }
        guard by.hasPrefix("app:") else { return .owner(takenOverFrom: nil) }
        guard let clientID else { return nil }
        return hostActor(of: clientID) == by ? nil : .otherApp(by)
    }

    /// The `app:<host[:port]>` actor a server before verified apps derived
    /// from a client id: the fallback for a row without `declaredByClient`.
    static func hostActor(of clientID: String) -> String? {
        guard let url = URL(string: clientID), let host = url.host?.lowercased() else { return nil }
        return "app:" + host + (url.port.map { ":\($0)" } ?? "")
    }

    /// Settings' words for it.
    public var sentence: String {
        switch self {
        case .owner(takenOverFrom: .some):
            "Your Training package is managed by you now (taken over); this app no longer updates it."
        case .owner(takenOverFrom: nil):
            "Your Training package is managed by you, not this app; this app doesn\u{2019}t update it."
        case let .otherApp(app):
            "Your Training package is managed by another app, \(app); this app doesn\u{2019}t update it."
        case let .unknown(reason):
            "Your substrate doesn\u{2019}t let this app update the Training package: \(reason)"
        }
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
/// The kinds belong to the app (O1/O2): a package changed outside it since
/// its last apply, found by comparing each kind row's version with the one
/// it recorded then (`baselineDirectory`), is `.edited`, and the app writes
/// nothing, at the same version or behind it. It never applies over such a
/// change.
public struct PackageInstaller: Sendable {
    public var api: WorkoutsAPI
    public var manifest: PackageManifest
    /// The folder of this server and repository, where the baseline is kept;
    /// nil keeps none, and then only the server's guard protects the package.
    public var baselineDirectory: URL?
    /// This app's client id, which the server owns the package by (decision
    /// 0205), so a package another app declared is told apart before an
    /// apply. Nil leaves that to the server's 403.
    public var clientID: String?

    public init(api: WorkoutsAPI, manifest: PackageManifest, baselineDirectory: URL? = nil, clientID: String? = nil) {
        self.api = api
        self.manifest = manifest
        self.baselineDirectory = baselineDirectory
        self.clientID = clientID
    }

    public var packageID: String { KindNames.packageID(authority: api.rehome.authority) }

    static let kindKind = "substrate.reamde.dev/core/kind"
    static let bundleKind = "substrate.reamde.dev/core/bundle"

    /// The version the repository holds, read from its package row, and
    /// whether the row says the package is kept by someone else.
    public func state() async throws -> PackageState {
        let row = try await api.packageRow(packageID)
        let held: Int? = row.flatMap { properties -> Int? in
            guard case let .number(version)? = properties["version"] else { return nil }
            return NSDecimalNumber(decimal: version).intValue
        }
        if let row, let manager = PackageManager.of(row, clientID: clientID) {
            return .managedElsewhere(held: held, by: manager)
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
    /// A newer version is left alone; a package changed outside the app is
    /// `.edited` and nothing is written; a refusal of the server's guard
    /// comes back as `.refused`, in its words; a package the owner took over
    /// or another app keeps is `.managedElsewhere` and is never applied.
    @discardableResult
    public func ensure() async throws -> PackageState {
        let before = try await state()
        let baseline = baselineDirectory.flatMap(PackageBaseline.load(from:))
        switch before {
        case .newer, .edited, .refused, .managedElsewhere:
            return before
        case let .current(held):
            guard let baseline else {
                try? await record(version: held)
                return before
            }
            // Another of the owner's devices upgraded it: the server keeps the
            // package this app's alone (decision 0205), so a newer version
            // under this client id is the app's own apply, not an edit.
            if baseline.version < held, try await declaredByThisApp() {
                try? await record(version: held)
                return before
            }
            let edited = try await edits(since: baseline)
            return edited.isEmpty ? before : .edited(held: held, kinds: edited)
        case let .behind(held):
            if let baseline, baseline.version < held, try await declaredByThisApp() {
                try? await record(version: held)
            } else {
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
        } catch let error as ServerError where Self.notTheAppsPackage(error) {
            // Not this app's package (decision 0205): taken over, or another
            // app's, after the row was read. The row says who from the next
            // check on, so this is one refused apply, not a loop.
            let after = try? await state()
            if case .managedElsewhere? = after { return after! }
            return .managedElsewhere(held: before.held, by: .unknown(error.message))
        }
        let after = try await state()
        if let held = after.held { try? await record(version: held) }
        return after
    }

    /// Whether the package row names this app's client id as the one that
    /// declared it. A package an owner took over, or one declared before the
    /// server recorded the client, answers false.
    func declaredByThisApp() async throws -> Bool {
        guard let clientID, let row = try await api.packageRow(packageID) else { return false }
        guard case let .string(by)? = row["declaredByClient"], case .string? = row["declaredBy"], row["takenOverFrom"] == nil
        else { return false }
        return by == clientID
    }

    /// Whether the server refused an apply because the package is not this
    /// app's (decision 0205), in the words its chokepoint answers: "managed
    /// by" an app, or "the owner's package". Every other 403, a token
    /// without the `declare:` scope among them, is the caller's to explain.
    static func notTheAppsPackage(_ error: ServerError) -> Bool {
        error.status == 403 && (error.message.contains("is managed by") || error.message.contains("is the owner's package"))
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
