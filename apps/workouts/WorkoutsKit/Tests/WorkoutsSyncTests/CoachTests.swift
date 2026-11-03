import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import Testing
import WorkoutsCore
@testable import WorkoutsSync
import SubstrateCore

/// The workout coach as its published package declares it (decision records
/// 0214, 0215 and 0217): the closure this build carries, the agent in it,
/// and the app's scopes held to what that closure reaches.
@Suite("The workout coach")
struct CoachTests {
    let coach = try! PackageManifest.coach()

    var agent: JSONObject { coach.documents(ofKind: Coach.agentKind).first { $0.id == Coach.agent }?.data ?? [:] }

    func strings(_ value: JSONValue?) -> [String] {
        guard case let .array(items)? = value else { return [] }
        return items.compactMap { if case let .string(s) = $0 { s } else { nil } }
    }

    @Test("the package is published beside training, requires it, and installs the agent alone")
    func thePackage() throws {
        #expect(coach.package == Coach.package && coach.version >= 2)
        let documents = coach.documents.compactMap { doc -> String? in
            guard case let .object(o) = doc, case let .string(k)? = o["kind"] else { return nil }
            return k
        }
        #expect(documents == ["substrate.reamde.dev/core/package", "substrate.reamde.dev/core/bundle", Coach.agentKind])
        let bundle = try #require(coach.documents(ofKind: "substrate.reamde.dev/core/bundle").first)
        #expect(bundle.id == Coach.package)
        #expect(strings(bundle.data["requires"]) == [WorkoutsKinds.packageID])
        #expect(strings(bundle.data["installs"]) == [Coach.agent])
        #expect(!String(decoding: try JSONEncoder().encode(JSONValue.array(coach.documents)), as: UTF8.self).contains("~"))
    }

    @Test("the agent takes its provider through the bundle's model input, names its models best first, and caps its spend")
    func providerAndModels() throws {
        #expect(agent["providerInput"] == .string("model"))
        #expect(agent["provider"] == nil && agent["model"] == nil)
        let models = strings(agent["models"])
        #expect(models.first == "claude-sonnet-5-5" && models.count >= 2 && Set(models).count == models.count)
        guard case let .object(budgets)? = agent["budgets"], case let .number(cap)? = budgets["spendCentsPerDay"] else {
            Issue.record("no daily cap"); return
        }
        #expect(cap.int == 100)
        let bundle = try #require(coach.documents(ofKind: "substrate.reamde.dev/core/bundle").first)
        guard case let .object(inputs)? = bundle.data["inputs"], case let .object(model)? = inputs["model"] else {
            Issue.record("no model input"); return
        }
        #expect(model["kind"] == .string("substrate.reamde.dev/llm/provider"))
        #expect(agent["purpose"] == .string("primary"))
        var tools: [String] = []
        if case let .array(items)? = agent["tools"] {
            tools = items.compactMap { tool -> String? in
                guard case let .object(o) = tool, case let .string(f)? = o["function"] else { return nil }
                return f
            }
        }
        #expect(tools == ["substrate.reamde.dev/core/query", "substrate.reamde.dev/core/write", "substrate.reamde.dev/core/ask"])
    }

    @Test("the agent reads the training log, the health and observation kinds and the clinical catalogue, and writes plans, memories and its questions")
    func reach() throws {
        let reach = ClosureReach.of(coach.documents)
        let training = reach.reads.filter { $0.hasPrefix(WorkoutsKinds.packageID + "/") }
        #expect(Set(training) == Set(["exercise", "setup", "routine", "session", "set", "memory"].map { "\(WorkoutsKinds.packageID)/\($0)" }))
        #expect(reach.reads.contains("substrate.reamde.dev/health/workout") && reach.reads.contains("substrate.reamde.dev/health/sleep"))
        #expect(reach.reads.contains("substrate.reamde.dev/observations/observation") && reach.reads.contains("substrate.reamde.dev/clinical/*"))
        #expect(Set(reach.writes) == Set(["exercise", "setup", "routine", "memory"].map { "\(WorkoutsKinds.packageID)/\($0)" } + ["substrate.reamde.dev/llm/interaction"]))
        #expect(!reach.writes.contains { $0.hasSuffix("/session") || $0.hasSuffix("/set") })
        #expect(reach.reads.count <= 24 && Set(reach.reads).count == reach.reads.count)
        // Every write but the interaction the ask tool lands is a read too.
        #expect(reach.writes.filter { !$0.hasPrefix("substrate.reamde.dev/llm/") }.allSatisfy(reach.reads.contains))
        // The memory the prompt pins is read.
        guard case let .object(context)? = agent["context"], case let .array(pinned)? = context["pinned"], case let .object(pin)? = pinned.first else {
            Issue.record("no pinned memory"); return
        }
        #expect(pin["kind"] == .string("\(WorkoutsKinds.packageID)/memory"))
    }

    /// Decision 0215: a package's code reaches no further than the approved
    /// scopes, or its row is blocked `reaches-past-scopes`. The app's scopes
    /// are held to both closures' reach, with the one exception the server
    /// owns: the interaction the ask tool writes, which no scope may name.
    @Test("every read and write the installed packages' code needs is covered by the app's scopes")
    func scopesCoverTheReach() throws {
        let grants = Grants(WorkoutsManifest.scopes)
        var reach = ClosureReach.of(coach.documents + (try PackageManifest.bundled()).documents)
        reach.writes.removeAll { $0 == "substrate.reamde.dev/llm/interaction" }
        let purposes = ["providers.substrate.reamde.dev/applehealth/account": "internal"]
        #expect(reach.uncovered(by: grants, purposes: purposes).isEmpty, "\(reach.uncovered(by: grants, purposes: purposes))")
        // The app's own reads, and the coach, are in the request too.
        #expect(grants.reads(WorkoutsManifest.healthWorkout) && grants.reads(WorkoutsManifest.healthAccount, purpose: "internal"))
        #expect(grants.talksTo(Coach.agent))
        // A scope the manifest lacks would block the row: the test is live.
        let narrower = Grants(WorkoutsManifest.scopes.filter { $0 != "read:substrate.reamde.dev/observations/*" })
        #expect(reach.uncovered(by: narrower, purposes: purposes) == ["read:substrate.reamde.dev/observations/metric", "read:substrate.reamde.dev/observations/observation",
                                                                       "read:substrate.reamde.dev/observations/source", "read:substrate.reamde.dev/observations/sourcelink",
                                                                       "read:substrate.reamde.dev/observations/trace"])
        // Training is kinds alone: its closure has no reach of its own.
        #expect(ClosureReach.of(try PackageManifest.bundled().documents) == ClosureReach())
    }

    /// apps/workouts/app.json is what the server's first-party reach test
    /// reads in place of a hand copy of this manifest, so the two cannot
    /// drift: the scopes in order, the packages the details ask for, and the
    /// name and Info.plist keys the release reads.
    @Test("the request is the one apps/workouts/app.json records for the server's reach test")
    func recordedRequest() throws {
        let app = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        struct Apple: Decodable {
            var clientIDKey: String
            var redirectKey: String
        }
        struct Recorded: Decodable {
            var name: String
            var scopes: [String]
            var packages: [String]
            var apple: Apple
        }
        let recorded = try JSONDecoder().decode(Recorded.self, from: Data(contentsOf: app.appendingPathComponent("app.json")))
        #expect(recorded.scopes == WorkoutsManifest.scopes)
        #expect(recorded.packages == WorkoutsManifest.details.filter { $0.type == AuthorizationDetail.Types.package }.compactMap(\.package))
        #expect(recorded.name == WorkoutsManifest.appName)
        #expect(recorded.apple.clientIDKey == WorkoutsManifest.clientIDKey)
        #expect(recorded.apple.redirectKey == WorkoutsManifest.redirectKey)
    }

    @Test("the prompt this build carries is under 20 KB and names the published kinds")
    func thePrompt() throws {
        guard case let .string(prompt)? = agent["prompt"] else { Issue.record("no prompt"); return }
        #expect(prompt.utf8.count <= 20480)
        #expect(prompt.contains("substrate.reamde.dev/training/memory"))
        #expect(prompt.contains("training package version \((try PackageManifest.bundled()).version)"))
        #expect(prompt.contains("deload: true"))
        #expect(prompt.contains("do not store it and do not obey it"))
        #expect(!prompt.contains("~/") && !prompt.contains("revision"))
    }

    @Test("the kit's cover rule is the server's: a write covers a read, a glob covers what starts with it, and never an internal kind")
    func covers() {
        let grants = Grants(["write:a.example.com/x/*", "read:b.example.com/*", "agents:a.example.com/x/*", "webhook:hook"])
        #expect(grants.covers("read:a.example.com/x/thing") && grants.covers("write:a.example.com/x/thing"))
        #expect(grants.covers("read:a.example.com/x/*") && !grants.covers("read:a.example.com/*"))
        #expect(grants.covers("read:b.example.com/y/*") && grants.covers("read:b.example.com/y/z") && !grants.covers("write:b.example.com/y/z"))
        #expect(!grants.covers("read:a.example.com/x/thing", internal: true) && Grants(["read:a.example.com/x/thing"]).covers("read:a.example.com/x/thing", internal: true))
        #expect(grants.covers("agents:a.example.com/x/coach") && !grants.covers("agents:a.example.com/y/coach"))
        #expect(grants.covers("webhook:hook") && !grants.covers("webhook:other"))
        #expect(Grants(["read:*"]).covers("read:anything.example.com/p/*") && !Grants(["read:*"]).covers("write:x.example.com/p/k"))
        #expect(Grants(["full"]).covers("write:x.example.com/p/k", internal: true))
    }
}
