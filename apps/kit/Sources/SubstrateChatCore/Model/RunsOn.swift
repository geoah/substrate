import Foundation
import SubstrateCore

// What an agent on `runsOn` runs on (decision record 0242), as the server
// chooses it (internal/engine/llmmodels.go) and the console reads it
// (web/console/src/lib/llm-models.ts): a new chat takes the first entry whose
// model record and provider row are live and whose row holds a key of its
// own, or is served by the operator with the model on its list. Readiness
// never moves it: a key the vendor refused stops a run, it never passes an
// entry over. A continued chat stays on the row and model its thread is
// stamped with.

/// The id a reference names under its pinned kind: the stored path less the
/// kind, or a bare id as authored. Never the path's last segment: a record id
/// may hold a slash.
func referent(_ value: JSONValue?, pin: String) -> String? {
    guard let path = value?.referencePath, !path.isEmpty else { return nil }
    let prefix = pin + "/"
    return path.hasPrefix(prefix) ? String(path.dropFirst(prefix.count)) : path
}

/// An `llm/model` record: one model a provider row offers. Its id is free:
/// the row and the model are read off its properties, never out of the id.
struct ModelRecord: Hashable, Sendable, Identifiable {
    let record: Record
    var id: String { record.id }
    /// The `llm/provider` row it names, by id.
    var providerID: String { referent(record["provider"], pin: Kinds.provider) ?? "" }
    /// The model id that row's API takes.
    var model: String { record["model"]?.string ?? "" }
    /// Its name as people read it: its label, else its model id.
    var name: String { record["label"]?.text ?? model }
}

/// Why an entry cannot take a new chat.
enum RunsOnRefusal: Equatable, Sendable {
    case noRecord
    case noProvider(String)
    case noKey(String)
    /// The operator serves the row, and not this model.
    case notServed(String, served: [String])

    /// The row a key of its own would open, where one would.
    var keyable: String? {
        switch self {
        case let .noKey(id), let .notServed(id, _): id
        case .noRecord, .noProvider: nil
        }
    }
}

/// Whether a call can be sent now, and why not: nil `available` while that
/// cannot be told.
struct Availability: Equatable, Sendable {
    var available: Bool?
    var refusal: RunsOnRefusal?

    static let unknown = Availability()
    static let yes = Availability(available: true)
    static func no(_ refusal: RunsOnRefusal) -> Availability { Availability(available: false, refusal: refusal) }
}

/// One `runsOn` entry, as a new chat would take it.
struct RunsOnEntry: Equatable, Sendable {
    /// The `llm/model` record id the agent names.
    var id: String
    /// The record, where it is live.
    var model: ModelRecord?
    var availability: Availability
}

/// What a new chat with an agent on `runsOn` runs on.
struct RunsOnChoice: Equatable, Sendable {
    var entries: [RunsOnEntry]

    /// The entry a new chat runs on: the first that can, or the first that
    /// cannot be told yet ahead of it.
    var chosen: RunsOnEntry? { entries.first { $0.availability.available != false } }

    /// true when an entry can; false when none can; nil while one cannot be
    /// told and none can.
    var canStart: Bool? {
        if entries.contains(where: { $0.availability.available == true }) { return true }
        return entries.allSatisfy { $0.availability.available == false } ? false : nil
    }

    /// Where none can: the row a key of its own would open, the first entry's
    /// whose record and row are live.
    var keyProvider: String? {
        guard canStart == false else { return nil }
        return entries.lazy.compactMap { $0.availability.refusal?.keyable }.first
    }

    /// The entry a person is told it runs on: the one a new chat takes, else
    /// the one a key would open, else the first that names its row.
    var shown: RunsOnEntry? {
        chosen ?? entries.first { $0.model != nil && $0.model?.providerID == keyProvider } ?? entries.first { $0.model != nil }
    }
}
