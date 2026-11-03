import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import WorkoutsCore
import SubstrateCore

/// The workout coach (docs/plans/workouts-coach.md): an AI agent shipped in
/// `substrate.reamde.dev/trainingcoach`, a published package the app's
/// approval installs beside training (decision record 0214). The owner
/// chats with it in Substrate Chat or the console, and picks the provider it
/// runs on when the package is installed (the bundle's `model` input,
/// decision record 0217). This app only asks for the package and talks to
/// the agent through its `agents:` scope.
public enum Coach {
    public static let package = "\(WorkoutsKinds.authority)/trainingcoach"
    public static let agent = "\(package)/workoutCoach"
    public static let agentKind = "substrate.reamde.dev/core/agent"
    /// The scope that reaches the coach, its chats and its questions.
    public static let agentsScope = "agents:\(package)/*"
}

/// What a closure's code reaches (decision 0215): its functions' and
/// agents' reads and writes, its triggers' selector kinds (read) and its
/// mappings' sources (read) and targets (write), each a kind or a glob.
/// Every package an approval installs must reach only what the approved
/// scopes cover, or the server blocks the row `reaches-past-scopes`, so an
/// app's scopes are held to its packages' reach before a request is sent.
public struct ClosureReach: Sendable, Hashable {
    public var reads: [String]
    public var writes: [String]

    public init(reads: [String] = [], writes: [String] = []) {
        self.reads = reads
        self.writes = writes
    }

    static let functionKind = "substrate.reamde.dev/core/function"
    static let triggerKind = "substrate.reamde.dev/core/trigger"
    static let mappingKind = "substrate.reamde.dev/core/recordmapping"

    /// The reach of `documents`, a closure as `PackageManifest` carries it,
    /// each entry once and sorted.
    public static func of(_ documents: [JSONValue]) -> ClosureReach {
        var reads: Set<String> = []
        var writes: Set<String> = []
        for document in documents {
            guard case let .object(object) = document, case let .string(kind)? = object["kind"],
                  case let .object(data)? = object["data"] else { continue }
            switch kind {
            case functionKind, Coach.agentKind:
                guard case let .object(permissions)? = data["permissions"] else { continue }
                if case let .object(read)? = permissions["reads"] { reads.formUnion(references(read["kinds"])) }
                writes.formUnion(references(permissions["writes"]))
            case triggerKind:
                guard case let .object(properties)? = data["properties"], case let .object(source)? = properties["source"],
                      case let .object(record)? = source["record"] else { continue }
                reads.formUnion(references(record["kinds"]))
            case mappingKind:
                reads.formUnion(references(data["from"]))
                writes.formUnion(references(data["to"]))
            default:
                continue
            }
        }
        return ClosureReach(reads: reads.sorted(), writes: writes.sorted())
    }

    /// The scopes the reach needs, in the grant's form: `write:` for each
    /// write and `read:` for each read no write already covers.
    public var scopes: [String] {
        writes.map { "write:\($0)" } + reads.filter { !writes.contains($0) }.map { "read:\($0)" }
    }

    /// The scopes of the reach that `grants` does not cover (as the server
    /// decides cover, `Grants.covers`): empty when the approval would admit
    /// the closure. `purposes` is the declared purpose of each kind the
    /// caller knows, since a glob never covers an `internal` kind.
    public func uncovered(by grants: Grants, purposes: [String: String] = [:]) -> [String] {
        scopes.filter { scope in
            let kind = String(scope.drop { $0 != ":" }.dropFirst())
            return !grants.covers(scope, internal: purposes[kind] == "internal")
        }
    }

    /// Kind references as a grant lists them: strings, or stored `{ref}`
    /// objects whose path ends in the kind.
    static func references(_ value: JSONValue?) -> [String] {
        guard case let .array(items)? = value else { return [] }
        return items.compactMap { item in
            switch item {
            case let .string(text): return text
            case let .object(object):
                guard case let .string(ref)? = object["ref"] else { return nil }
                let prefix = "substrate.reamde.dev/core/kind/"
                return ref.hasPrefix(prefix) ? String(ref.dropFirst(prefix.count)) : ref
            default: return nil
            }
        }
    }
}
