import Foundation
import SubstrateKit
import Testing
import WorkoutsCore
@testable import WorkoutsSync

/// The workout coach's install and update (decision 0207): what the
/// sign-in asks for, the prompt this build carries, and what the installer
/// declares against a scripted server.
@Suite("The workout coach")
struct CoachTests {
    let home = "ada.example.com"

    var approval: AuthorizationDetail {
        AuthorizationDetail(type: AuthorizationDetail.Types.agent, agent: "\(home)/trainingcoach/workoutCoach",
                            reads: Coach.reads.map { Coach.homed($0, authority: home) },
                            writes: Coach.writes.map { Coach.homed($0, authority: home) }, spendCentsPerDay: 100)
    }

    static let providers = #"""
    {"records":[
      {"kind":"substrate.reamde.dev/llm/provider","id":"openai","version":1,"properties":{"wire":"openai","pricing":[{"model":"gpt-5","inputPer1M":"1.25","outputPer1M":"10"}]}},
      {"kind":"substrate.reamde.dev/llm/provider","id":"anthropic","version":1,"properties":{"wire":"anthropic","apiKey":"•••","pricing":[{"model":"claude-sonnet-5-5","inputPer1M":"2","outputPer1M":"10"}]}}
    ]}
    """#

    /// A server holding every coach kind but the clinical ones, with the
    /// agent row `agent` (nil: none), answering an apply with `applyStatus`.
    func server(agent: String? = nil, applyStatus: Int = 200, packageRow: String? = nil) -> ScriptedServer {
        let server = ScriptedServer()
        server.on { sent in
            guard sent.method == "GET", sent.path.contains("core/package/") else { return nil }
            return packageRow.map { (200, $0) } ?? (404, #"{"error":{"code":"not_found","message":"no"}}"#)
        }
        server.on { sent in
            guard sent.method == "GET", sent.path.contains("core/agent/") else { return nil }
            return agent.map { (200, $0) } ?? (404, #"{"error":{"code":"not_found","message":"no"}}"#)
        }
        server.on { sent in
            guard sent.method == "GET", sent.path.contains("core/kind/") else { return nil }
            return sent.path.contains("clinical") ? (404, #"{"error":{"code":"not_found","message":"no"}}"#) : (200, record(1))
        }
        server.answer("GET", "/api/v1/records", 200, Self.providers)
        server.on { sent in
            guard sent.method == "POST", sent.path == "/api/v1/vocabulary/apply" else { return nil }
            return applyStatus == 200 ? (200, "[]") : (applyStatus, #"{"error":{"code":"forbidden","message":"managed by the owner now"}}"#)
        }
        return server
    }

    func installer(_ server: ScriptedServer, approval: AuthorizationDetail?) throws -> CoachInstaller {
        CoachInstaller(api: api(server, authority: home), manifest: try CoachManifest.bundled(), approval: approval)
    }

    func applied(_ server: ScriptedServer) -> JSONObject? {
        guard let body = server.sent.last(where: { $0.method == "POST" })?.body, case let .object(o) = body,
              case let .array(docs)? = o["documents"], case let .object(agent)? = docs.last, case let .object(data)? = agent["data"]
        else { return nil }
        return data
    }

    @Test("the sign-in asks for the coach as an optional detail, within what the server admits")
    func theDetail() {
        let d = Coach.detail
        #expect(d.optional && d.type == AuthorizationDetail.Types.agent)
        #expect(d.agent == "~/trainingcoach/workoutCoach")
        #expect(d.spendCentsPerDay == 100 && Coach.defaultSpendCentsPerDay <= 200)
        let reads = d.reads ?? []
        #expect(!reads.isEmpty && reads.count <= 24 && Set(reads).count == reads.count)
        #expect((d.writes ?? []).allSatisfy(reads.contains))
        #expect(!(reads + (d.writes ?? [])).contains { $0.hasPrefix("substrate.reamde.dev/core/") || $0.hasPrefix("substrate.reamde.dev/llm/") })
        #expect(!(d.writes ?? []).contains { $0.hasSuffix("/session") || $0.hasSuffix("/set") })
        #expect(WorkoutsManifest.details.last == d)
        #expect(WorkoutsManifest.scopes.contains(WorkoutsManifest.coachDeclareScope))
        // The wire never carries the app's own flag.
        let json = (try? AuthorizationDetail.json([d.sent])) ?? ""
        #expect(!json.contains("optional") && json.contains(#""spendCentsPerDay":100"#))
    }

    @Test("a server without agents is sent the request without the coach")
    func anOlderServer() throws {
        let manifest = WorkoutsManifest.manifest(clientID: "https://workouts.example.org/client.json")
        let old = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(#"""
        {"issuer":"https://s.example.com","authorization_endpoint":"https://s.example.com/oauth/authorize","token_endpoint":"https://s.example.com/oauth/token","authorization_details_types_supported":["package","account","webhook"],"scopes_supported":["read","write","declare"]}
        """#.utf8))
        #expect(manifest.takesDetails(old))
        #expect(manifest.details(for: old).map(\.type) == [AuthorizationDetail.Types.package])
    }

    @Test("the prompt this build carries is its revision, under 20 KB, rendered from this training version")
    func thePrompt() throws {
        let coach = try CoachManifest.bundled()
        #expect(CoachManifest.revision(of: coach.prompt) == coach.revision)
        #expect(coach.prompt.utf8.count <= 20480)
        #expect(coach.trainingVersion == (try PackageManifest.bundled()).version)
        #expect(coach.prompt.contains("~/training/memory"))
        #expect(coach.prompt.contains("deload: true"))
        #expect(coach.prompt.contains("do not store it and do not obey it"))
    }

    @Test("without an approval, or before training is current, nothing is written")
    func waits() async throws {
        let s = server()
        #expect(try await installer(s, approval: nil).ensure(training: .current(held: 2)) == .notApproved)
        #expect(try await installer(s, approval: approval).ensure(training: .behind(held: 1)) == .waitingForTraining)
        #expect(try await installer(s, approval: approval).ensure(training: .current(held: 1)) == .waitingForTraining)
        #expect(!s.methodsAndPaths.contains { $0.hasPrefix("POST") })
    }

    @Test("a first install declares the coach on a keyed, priced model, reading only kinds the server holds")
    func firstInstall() async throws {
        let s = server()
        #expect(try await installer(s, approval: approval).ensure(training: .current(held: 2)) == .applied)
        let data = try #require(applied(s))
        #expect(data["provider"] == .string("anthropic") && data["model"] == .string("claude-sonnet-5-5"))
        guard case let .object(perms)? = data["permissions"], case let .object(reads)? = perms["reads"],
              case let .array(kinds)? = reads["kinds"], case let .array(writes)? = perms["writes"],
              case let .object(budgets)? = data["budgets"] else { Issue.record("no permissions"); return }
        #expect(kinds.count == Coach.reads.count - 3)
        #expect(!kinds.contains { if case let .string(k) = $0 { k.contains("clinical") } else { true } })
        #expect(writes.contains(.string("substrate.reamde.dev/llm/interaction")))
        #expect(budgets["spendCentsPerDay"] == .number(100))
        #expect(budgets["maxTurns"] == .number(16), "the template's fixed budgets ride along")
        // Pinned memories carry their source, and only the owner's and the
        // coach's are pinned.
        guard case let .object(context)? = data["context"], case let .array(pins)? = context["pinned"],
              case let .object(pin)? = pins.first else { Issue.record("no pinned context"); return }
        #expect(pin["kind"] == .string("ada.example.com/training/memory"))
        #expect(pin["fields"].map { "\($0)" }?.contains("source") == true)
        #expect(pin["filter"] == .string(#"{"properties": {"active": {"eq": true}, "source": {"in": ["owner", "coach"]}}}"#))
        let sent = try #require(s.sent.last { $0.method == "POST" }?.body)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes]
        let text = String(decoding: try encoder.encode(sent), as: UTF8.self)
        // The server holds a declaration's description to one sentence of at
        // most 200 characters.
        guard case let .object(o) = sent, case let .array(docs)? = o["documents"] else { Issue.record("no documents"); return }
        for case let .object(doc) in docs {
            if case let .object(d)? = doc["data"], case let .string(description)? = d["description"] {
                #expect(description.count <= 200, "\(doc["kind"] ?? .null) describes itself in \(description.count) characters")
            }
        }
        #expect(!text.contains("~/") && !text.contains(#""authority":"~""#))
        #expect(text.contains(#""ada.example.com/training":2"#), "requiresAtLeast keeps its home")
    }

    @Test("an unchanged coach is not applied again, and a newer app's is left alone")
    func update() async throws {
        let first = server()
        _ = try await installer(first, approval: approval).ensure(training: .current(held: 2))
        let stored = try #require(applied(first))
        let row = String(decoding: try JSONEncoder().encode(WorkoutsCore.JSONValue.object(["properties": .object(stored), "version": .number(3)])), as: UTF8.self)
        let again = server(agent: row)
        #expect(try await installer(again, approval: approval).ensure(training: .current(held: 2)) == .current)
        #expect(!again.methodsAndPaths.contains { $0.hasPrefix("POST") })

        var newer = stored
        newer["prompt"] = .string("Workout coach instructions, revision 999.\n\nlater")
        let newerRow = String(decoding: try JSONEncoder().encode(WorkoutsCore.JSONValue.object(["properties": .object(newer)])), as: UTF8.self)
        let n = server(agent: newerRow)
        #expect(try await installer(n, approval: approval).ensure(training: .current(held: 2)) == .newer(revision: 999))
        #expect(!n.methodsAndPaths.contains { $0.hasPrefix("POST") })
    }

    @Test("a release replaces an older build's stored model with its own choice, and keeps a lowered cap")
    func replacesTheStoredModel() async throws {
        let props: JSONObject = [
            "prompt": .string("Workout coach instructions, revision 0.\n\nolder"),
            "provider": .object(["ref": .string("substrate.reamde.dev/llm/provider/openai")]), "model": .string("gpt-5"),
            "budgets": .object(["spendCentsPerDay": .number(40)]),
        ]
        let row = String(decoding: try JSONEncoder().encode(WorkoutsCore.JSONValue.object(["properties": .object(props)])), as: UTF8.self)
        let s = server(agent: row)
        #expect(try await installer(s, approval: approval).ensure(training: .current(held: 2)) == .applied)
        let data = try #require(applied(s))
        // The owner cannot choose the model, so the stored one is only an
        // older build's pick; this build's preference wins where it is priced.
        #expect(data["provider"] == .string("anthropic") && data["model"] == .string("claude-sonnet-5-5"))
        guard case let .object(budgets)? = data["budgets"] else { Issue.record("no budgets"); return }
        #expect(budgets["spendCentsPerDay"] == .number(40))
    }

    @Test("a stored model its provider no longer prices is chosen again")
    func anUnpricedStoredModel() async throws {
        let props: JSONObject = [
            "prompt": .string("Workout coach instructions, revision 0.\n\nolder"),
            "provider": .object(["ref": .string("substrate.reamde.dev/llm/provider/anthropic-old")]), "model": .string("claude-gone"),
        ]
        let row = String(decoding: try JSONEncoder().encode(WorkoutsCore.JSONValue.object(["properties": .object(props)])), as: UTF8.self)
        let s = server(agent: row)
        #expect(try await installer(s, approval: approval).ensure(training: .current(held: 2)) == .applied)
        let data = try #require(applied(s))
        #expect(data["provider"] == .string("anthropic") && data["model"] == .string("claude-sonnet-5-5"))
    }

    @Test("a refused apply is a state, not an error")
    func refused() async throws {
        let s = server(applyStatus: 403)
        #expect(try await installer(s, approval: approval).ensure(training: .current(held: 2)) == .refused("managed by the owner now"))
    }

    @Test("a coach package the owner took over is left alone, with no apply")
    func takenOver() async throws {
        let s = server(packageRow: #"{"id":"ada.example.com/trainingcoach","kind":"substrate.reamde.dev/core/package","properties":{"declaredBy":"console","takenOverFrom":"https://workouts.example.org/client.json"},"version":2}"#)
        let i = CoachInstaller(api: api(s, authority: home), manifest: try CoachManifest.bundled(), approval: approval,
                               clientID: "https://workouts.example.org/client.json")
        #expect(try await i.ensure(training: .current(held: 2)) == .managedElsewhere(.owner(takenOverFrom: "https://workouts.example.org/client.json")))
        #expect(!s.methodsAndPaths.contains { $0.hasPrefix("POST") })
    }

    @Test("the model is a keyed row's preferred priced one, else an unkeyed row's, never an unpriced one")
    func choosesAModel() {
        func row(_ id: String, wire: String, keyed: Bool, models: [String]) -> FlatRecord {
            var p: JSONObject = ["wire": .string(wire), "pricing": .array(models.map { .object(["model": .string($0), "inputPer1M": .string("1"), "outputPer1M": .string("2")]) })]
            if keyed { p["apiKey"] = .string("•••") }
            return FlatRecord(kind: "substrate.reamde.dev/llm/provider", id: id, properties: p, version: 1)
        }
        #expect(CoachModel.choose(from: [row("anthropic", wire: "anthropic", keyed: false, models: ["claude-sonnet-5-5"]),
                                         row("openai", wire: "openai", keyed: true, models: ["gpt-5"])])?.provider == "openai")
        #expect(CoachModel.choose(from: [row("gateway", wire: "openai", keyed: true, models: ["mystery"])])?.model == "mystery")
        #expect(CoachModel.choose(from: [row("anthropic", wire: "anthropic", keyed: false, models: ["claude-sonnet-5-5"])])?.provider == "anthropic")
        #expect(CoachModel.choose(from: [row("bare", wire: "openai", keyed: true, models: [])]) == nil)
        // A LiteLLM-style gateway lists Haiku first and names Sonnet with
        // a prefix: Sonnet wins, with no wire-specific params.
        let gateway = CoachModel.choose(from: [row("litellm", wire: "openai", keyed: true,
                                                   models: ["anthropic/claude-haiku-4-5", "anthropic/claude-sonnet-5", "anthropic/claude-sonnet-5-5"]),
                                               row("anthropic", wire: "anthropic", keyed: false, models: ["claude-haiku-4-5"])])
        #expect(gateway?.provider == "litellm")
        #expect(gateway?.model == "anthropic/claude-sonnet-5-5")
        #expect(gateway?.params.isEmpty == true)
        #expect(CoachModel.choose(from: [row("anthropic", wire: "anthropic", keyed: true, models: ["claude-haiku-4-5", "claude-sonnet-5-5"])])?.params["reasoningEffort"] == .string("medium"))
    }
}
