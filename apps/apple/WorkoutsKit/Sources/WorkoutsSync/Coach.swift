import Foundation
import SubstrateKit
import WorkoutsCore

/// The workout coach (decision 0206, docs/plans/workouts-coach.md): an AI
/// agent the app declares in a package of its own, `~/trainingcoach`, under
/// the approval an `agent` authorization detail put on its token. The owner
/// chats with it in Substrate Chat or the console; this app only installs it
/// and keeps its instructions current. There is no coach screen yet: the
/// provider, model and daily cap are this build's defaults, and changing
/// them in the app comes later.
public enum Coach {
    public static let package = "\(KindNames.home)/trainingcoach"
    public static let agent = "\(package)/workoutCoach"
    public static let agentKind = "substrate.reamde.dev/core/agent"
    static let kindKind = "substrate.reamde.dev/core/kind"
    static let providerKind = "substrate.reamde.dev/llm/provider"
    static let interactionKind = "substrate.reamde.dev/llm/interaction"

    /// The default daily cap, in cents; the server holds every app's agent
    /// to 200.
    public static let defaultSpendCentsPerDay = 100

    static let training = KindNames.packageID(authority: KindNames.home)
    /// What the coach reads: everything the app logs, its memories, the
    /// health catalogue's workouts and sleep, every observation and what a
    /// metric read needs beside it, and the clinical conditions and
    /// medications that matter for safe training (owner, 2026-11-02).
    public static let reads = ["exercise", "setup", "routine", "session", "set", "memory"].map { "\(training)/\($0)" } + [
        "substrate.reamde.dev/health/workout",
        "substrate.reamde.dev/health/sleep",
        "substrate.reamde.dev/observations/observation",
        "substrate.reamde.dev/observations/metric",
        "substrate.reamde.dev/observations/source",
        "substrate.reamde.dev/observations/sourcelink",
        "substrate.reamde.dev/observations/trace",
        "substrate.reamde.dev/clinical/condition",
        "substrate.reamde.dev/clinical/medicationrequest",
        "substrate.reamde.dev/clinical/medicationdispense",
    ]
    /// What it writes: routines and what they reference, and its memories.
    /// Never sessions or sets, which are this app's log.
    public static let writes = ["exercise", "setup", "routine", "memory"].map { "\(training)/\($0)" }

    /// The detail the sign-in asks for. Optional: a server without agents
    /// takes the rest of the request.
    public static var detail: AuthorizationDetail {
        .agent(agent, reads: reads, writes: writes, spendCentsPerDay: defaultSpendCentsPerDay)
    }

    /// `text` with `~/` and a bare `~` authority completed for one repository.
    static func homed(_ value: WorkoutsCore.JSONValue, authority: String) -> WorkoutsCore.JSONValue {
        switch value {
        case let .string(text):
            return .string(homed(text, authority: authority))
        case let .array(items):
            return .array(items.map { homed($0, authority: authority) })
        case let .object(object):
            var out = JSONObject()
            for (key, item) in object {
                // A key names a package too (`requiresAtLeast`).
                out[homed(key, authority: authority)] = key == "authority" && item == .string(KindNames.home) ? .string(authority) : homed(item, authority: authority)
            }
            return .object(out)
        default:
            return value
        }
    }

    static func homed(_ text: String, authority: String) -> String {
        text.replacingOccurrences(of: "\(KindNames.home)/", with: "\(authority)/")
    }
}

/// The coach's package as this build carries it (`Resources/Package/coach.json`,
/// written by apps/apple/scripts/workouts-package.py): the package and bundle
/// documents, the rendered prompt and its revision.
public struct CoachManifest: Sendable {
    public var revision: Int
    /// The training version the prompt's field guide was rendered from, the
    /// one the coach needs.
    public var trainingVersion: Int
    public var prompt: String
    public var documents: [WorkoutsCore.JSONValue]

    public init(revision: Int, trainingVersion: Int, prompt: String, documents: [WorkoutsCore.JSONValue]) {
        self.revision = revision
        self.trainingVersion = trainingVersion
        self.prompt = prompt
        self.documents = documents
    }

    public static func bundled() throws -> CoachManifest {
        guard let url = BundledCatalog.resourceBundle.url(forResource: "coach", withExtension: "json", subdirectory: "Package") else {
            throw CatalogError.missingResource("Package/coach.json")
        }
        return try decode(Data(contentsOf: url))
    }

    static func decode(_ data: Data) throws -> CoachManifest {
        guard case let .object(object) = try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: data),
              case let .number(revision)? = object["revision"],
              case let .number(training)? = object["trainingVersion"],
              case let .string(prompt)? = object["prompt"],
              case let .array(documents)? = object["documents"]
        else { throw CatalogError.missingResource("Package/coach.json is not a coach manifest") }
        return CoachManifest(revision: NSDecimalNumber(decimal: revision).intValue, trainingVersion: NSDecimalNumber(decimal: training).intValue,
                             prompt: prompt, documents: documents)
    }

    /// The revision a stored prompt was rendered at, read from its first
    /// line; nil for a prompt no build of this app wrote.
    public static func revision(of prompt: String) -> Int? {
        let prefix = "Workout coach instructions, revision "
        guard let line = prompt.split(separator: "\n", maxSplits: 1).first, line.hasPrefix(prefix) else { return nil }
        return Int(line.dropFirst(prefix.count).trimmingCharacters(in: CharacterSet(charactersIn: ". ")))
    }
}

/// Where a repository stands with the coach, from the install's side.
public enum CoachState: Sendable, Hashable {
    /// The token carries no approval of the coach: the server takes no
    /// agents, or the owner has not approved it yet.
    case notApproved
    /// The training package is not yet at the version the coach needs.
    case waitingForTraining
    /// No AI provider on the server prices a model the coach can run on.
    case noProvider
    /// The coach on the server is this build's.
    case current
    /// The install or update just landed.
    case applied
    /// A newer build of the app wrote the coach; this one leaves it alone.
    case newer(revision: Int)
    /// The coach's package is kept by someone else (decision 0205): the
    /// owner took it over, or another app declared it. The app leaves it.
    case managedElsewhere(PackageManager)
    /// The server refused the apply, in its words: something the approval
    /// does not cover.
    case refused(String)
}

/// The provider, model and params the coach runs on.
public struct CoachModel: Sendable, Hashable {
    public var provider: String
    public var model: String
    public var params: JSONObject

    public init(provider: String, model: String, params: JSONObject = [:]) {
        self.provider = provider
        self.model = model
        self.params = params
    }

    /// The models this build prefers, by wire, with the params each needs.
    static let preferred: [(wire: String, model: String, params: JSONObject)] = [
        ("anthropic", "claude-sonnet-5-5", ["reasoningEffort": .string("medium")]),
        ("openai", "gpt-5", ["reasoningEffort": .string("low")]),
    ]

    /// The model the coach runs on, chosen from the repository's provider
    /// rows: a keyed row first, the preferred model of its wire where the row
    /// prices it, else the row's first priced model. A row with no key still
    /// serves when none has one: the coach is installed and refuses until
    /// the owner keys it. The server refuses a model the row does not price,
    /// so nothing unpriced is chosen.
    public static func choose(from providers: [FlatRecord]) -> CoachModel? {
        let keyed = providers.filter { $0.properties["apiKey"].map { $0 != .null } ?? false }
        for rows in [keyed, providers] {
            for (wire, model, params) in preferred {
                if let row = rows.first(where: { $0.properties["wire"] == .string(wire) && priced($0).contains(model) }) {
                    return CoachModel(provider: row.id, model: model, params: params)
                }
            }
            if let row = rows.first(where: { !priced($0).isEmpty }), let model = priced(row).first {
                return CoachModel(provider: row.id, model: model)
            }
        }
        return nil
    }

    /// The models a provider row prices above zero, in its order.
    static func priced(_ row: FlatRecord) -> [String] {
        guard case let .array(entries)? = row.properties["pricing"] else { return [] }
        return entries.compactMap { entry -> String? in
            guard case let .object(o) = entry, case let .string(model)? = o["model"],
                  rate(o["inputPer1M"]) > 0, rate(o["outputPer1M"]) > 0 else { return nil }
            return model
        }
    }

    static func rate(_ value: WorkoutsCore.JSONValue?) -> Decimal {
        switch value {
        case let .number(n): n
        case let .string(s): Decimal(string: s) ?? 0
        default: 0
        }
    }
}

/// Installs and updates the coach in one repository, quietly: every outcome
/// is a `CoachState`, and nothing it meets stops the app's own sync.
public struct CoachInstaller: Sendable {
    public var api: WorkoutsAPI
    public var manifest: CoachManifest
    /// The agent detail the token response approved, every authority
    /// resolved; nil when the token approves none.
    public var approval: AuthorizationDetail?
    /// This app's client id, which the server owns the coach's package by
    /// (decision 0205). Nil leaves a package another app keeps to the
    /// server's 403.
    public var clientID: String?

    public init(api: WorkoutsAPI, manifest: CoachManifest, approval: AuthorizationDetail?, clientID: String? = nil) {
        self.api = api
        self.manifest = manifest
        self.approval = approval
        self.clientID = clientID
    }

    var authority: String { api.rehome.authority }
    public var agentID: String { Coach.homed(Coach.agent, authority: authority) }

    /// The approved agent detail among a session's token response details.
    public static func approval(in details: [AuthorizationDetail], authority: String) -> AuthorizationDetail? {
        let id = Coach.homed(Coach.agent, authority: authority)
        return details.first { $0.type == AuthorizationDetail.Types.agent && $0.agent == id }
    }

    /// Installs or updates the coach once training is at the version it
    /// needs, and answers where it stands.
    public func ensure(training: PackageState) async throws -> CoachState {
        guard let approval, let approvedReads = approval.reads, let cap = approval.spendCentsPerDay else { return .notApproved }
        // Training is the app's to keep current; one the owner took over at
        // the version the coach needs serves it as well.
        switch training {
        case .current, .managedElsewhere: break
        default: return .waitingForTraining
        }
        guard let held = training.held, held >= manifest.trainingVersion else { return .waitingForTraining }
        if let row = try await api.packageRow(Coach.homed(Coach.package, authority: authority)),
           let manager = PackageManager.of(row, clientID: clientID)
        {
            return .managedElsewhere(manager)
        }
        let stored = try await api.declarationRow(Coach.agentKind, agentID)?.properties
        if let prompt = stored?["prompt"].flatMap({ if case let .string(s) = $0 { s } else { nil } }),
           let revision = CoachManifest.revision(of: prompt), revision > manifest.revision
        {
            return .newer(revision: revision)
        }
        // The stored choice stands across releases; a first install takes
        // this build's default.
        let model: CoachModel
        if let stored, let kept = Self.model(of: stored) {
            model = kept
        } else if let chosen = CoachModel.choose(from: try await api.list([Coach.providerKind])) {
            model = chosen
        } else {
            return .noProvider
        }
        var reads: [String] = []
        for kind in approvedReads where try await api.declarationRow(Coach.kindKind, kind) != nil {
            reads.append(kind)
        }
        guard reads.contains(Coach.homed("\(Coach.training)/memory", authority: authority)) else { return .waitingForTraining }
        let writes = (approval.writes ?? []).filter(reads.contains) + [Coach.interactionKind]
        let capCents = min(cap, Self.storedCap(stored) ?? Coach.defaultSpendCentsPerDay)
        let agent = agentDocument(model: model, reads: reads, writes: writes, capCents: capCents)
        if let stored, Self.same(stored, as: agent) { return .current }
        do {
            try await api.applyVocabulary(manifest.documents.map { Coach.homed($0, authority: authority) } + [agent])
        } catch let error as ServerError where PackageInstaller.notTheAppsPackage(error) {
            return .managedElsewhere(.unknown(error.message))
        } catch let error as ServerError where [403, 409, 422].contains(error.status) {
            return .refused(error.message)
        }
        return .applied
    }

    /// The agent document as the app declares it.
    func agentDocument(model: CoachModel, reads: [String], writes: [String], capCents: Int) -> WorkoutsCore.JSONValue {
        let memory = Coach.homed("\(Coach.training)/memory", authority: authority)
        let tools: [WorkoutsCore.JSONValue] = ["query", "write", "ask"].map { .object(["function": .string("substrate.reamde.dev/core/\($0)")]) }
        var data: JSONObject = [
            "authority": .string(authority),
            "package": .string("trainingcoach"),
            "purpose": .string("primary"),
            "description": .string("Your strength coach: plans your workout routines from your goals, training and health data, and remembers what you tell it. Kept by Substrate Workouts."),
            "prompt": .string(Coach.homed(manifest.prompt, authority: authority)),
            "provider": .string(model.provider),
            "model": .string(model.model),
            "tools": .array(tools),
            "context": .object([
                "pinned": .array([.object([
                    "kind": .string(memory),
                    "filter": .string(#"{"properties": {"active": {"eq": true}}}"#),
                    "orderBy": .string("createdAt:desc,id"),
                    "first": .number(60),
                    "fields": .array(["text", "topic", "exercise", "until"].map { .string($0) }),
                ])]),
                "maxBytes": .number(12288),
            ]),
            "permissions": .object([
                "reads": .object(["kinds": .array(reads.map { .string($0) }), "budgets": .object(["calls": .number(40), "rows": .number(5000)])]),
                "writes": .array(writes.map { .string($0) }),
            ]),
            "budgets": .object([
                "maxTurns": .number(16), "maxToolCalls": .number(48), "maxParallelTools": .number(4),
                "deadlineSeconds": .number(300), "maxToolResultBytes": .number(65536),
                "spendCentsPerDay": .number(Decimal(capCents)),
            ]),
        ]
        if !model.params.isEmpty { data["params"] = .object(model.params) }
        return .object([
            "kind": .string(Coach.agentKind),
            "metadata": .object(["id": .string(agentID)]),
            "data": .object(data),
        ])
    }

    /// The stored row's provider, model and params.
    static func model(of stored: JSONObject) -> CoachModel? {
        guard let provider = referenceID(stored["provider"]), case let .string(model)? = stored["model"] else { return nil }
        var params = JSONObject()
        if case let .object(p)? = stored["params"] { params = p }
        return CoachModel(provider: provider, model: model, params: params)
    }

    static func storedCap(_ stored: JSONObject?) -> Int? {
        guard case let .object(budgets)? = stored?["budgets"], case let .number(cap)? = budgets["spendCentsPerDay"] else { return nil }
        return NSDecimalNumber(decimal: cap).intValue
    }

    /// A reference as a bare record id: a string or a stored `{ref: …}`,
    /// whose last path segment is the id.
    static func referenceID(_ value: WorkoutsCore.JSONValue?) -> String? {
        switch value {
        case let .string(s): s.split(separator: "/").last.map(String.init)
        case let .object(o): referenceID(o["ref"])
        default: nil
        }
    }

    /// Whether the stored row already says what this build would declare:
    /// the prompt, the model and the reach. An unchanged coach is not
    /// applied again.
    static func same(_ stored: JSONObject, as document: WorkoutsCore.JSONValue) -> Bool {
        guard case let .object(doc) = document, case let .object(data)? = doc["data"] else { return false }
        func refs(_ v: WorkoutsCore.JSONValue?) -> Set<String> {
            guard case let .array(items)? = v else { return [] }
            return Set(items.compactMap { item in
                switch item {
                case let .string(s): s
                case let .object(o): if case let .string(s)? = o["ref"] { s.hasPrefix("substrate.reamde.dev/core/kind/") ? String(s.dropFirst(31)) : s } else { nil }
                default: nil
                }
            })
        }
        func perms(_ o: JSONObject) -> (Set<String>, Set<String>) {
            guard case let .object(p)? = o["permissions"] else { return ([], []) }
            var reads: WorkoutsCore.JSONValue?
            if case let .object(r)? = p["reads"] { reads = r["kinds"] }
            return (refs(reads), refs(p["writes"]))
        }
        return stored["prompt"] == data["prompt"] && referenceID(stored["provider"]) == referenceID(data["provider"])
            && stored["model"] == data["model"] && storedCap(stored) == storedCap(data)
            && perms(stored) == perms(data)
    }
}
