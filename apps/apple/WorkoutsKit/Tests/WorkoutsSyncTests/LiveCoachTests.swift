import Foundation
import SubstrateKit
import Testing
import WorkoutsCore
@testable import WorkoutsSync

/// The workout coach's install against a real substrate (decision 0207),
/// skipped unless `WORKOUTS_E2E_COACH_SERVER` names one and
/// `WORKOUTS_E2E_OWNER_TOKEN` holds a full-access token of a repository with
/// no training package: the app's whole sign-in request with its `agent`
/// detail, approved through the approval API the console's page calls, the
/// code exchanged, training installed, then the coach installed by the
/// installer the app runs, applied once, and read back with the owner's
/// token. The coach is left installed, for Substrate Chat to find.
///
///     WORKOUTS_E2E_COACH_SERVER=http://localhost:8097 WORKOUTS_E2E_OWNER_TOKEN=… swift test --filter LiveCoach
@Suite("Live coach", .enabled(if: ProcessInfo.processInfo.environment["WORKOUTS_E2E_COACH_SERVER"] != nil))
struct LiveCoachTests {
    let server = URL(string: ProcessInfo.processInfo.environment["WORKOUTS_E2E_COACH_SERVER"] ?? "http://localhost")!
    let ownerToken = ProcessInfo.processInfo.environment["WORKOUTS_E2E_OWNER_TOKEN"] ?? ""
    let redirect = "http://127.0.0.1:53112/oauth/callback"

    @Test("sign in with the coach, approve, install training and the coach, read it back")
    func theCoach() async throws {
        let manifest = WorkoutsManifest.manifest(clientID: WorkoutsManifest.localClientID)
        let probe = try await ServerProbe().probe(server, requirement: manifest.requirement, app: manifest.appName)
        let metadata = try #require(probe.metadata)
        #expect(manifest.details(for: metadata).map(\.type) == ["package", "agent"])
        let oauth = OAuthClient(manifest: manifest, metadata: metadata)
        let pkce = try PKCE()
        let state = try PKCE.state()
        var request = URLRequest(url: try oauth.authorizeURL(redirect: redirect, pkce: pkce, state: state))
        request.httpMethod = "GET"
        let (_, parked) = try await URLSessionTransport.shared.data(for: request)
        let location = try #require(parked.value(forHTTPHeaderField: "Location"))
        let id = try #require(location.split(separator: "/").last.map(String.init))

        let owner = SubstrateClient(server: server, token: ownerToken)
        let page = try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: try await owner.send("GET", "api/v1/authorizations/\(id)").data)
        guard case let .object(fields) = page, case let .array(scopes)? = fields["scopes"], case let .array(setup)? = fields["setup"] else {
            Issue.record("no scopes")
            return
        }
        #expect(setup.contains { if case let .object(row) = $0 { row["type"] == .string("agent") } else { false } })
        let approved = scopes.compactMap { scope -> WorkoutsCore.JSONValue? in
            guard case let .object(row) = scope else { return nil }
            return row["scope"]
        }
        var approval: JSONObject = ["scopes": .array(approved), "lifetimeDays": .number(30)]
        if let hash = fields["planHash"] { approval["planHash"] = hash }
        let encoder = JSONEncoder()
        encoder.outputFormatting = .withoutEscapingSlashes
        var answer = try await owner.send("POST", "api/v1/authorizations/\(id)", body: .json(encoder.encode(WorkoutsCore.JSONValue.object(approval))),
                                          timeout: SubstrateClient.Timeout.serverWork).data
        var next = redirectOf(answer)
        for _ in 0 ..< 120 where next == nil {
            try await Task.sleep(for: .milliseconds(500))
            answer = try await owner.send("GET", "api/v1/authorizations/\(id)").data
            next = redirectOf(answer)
        }
        let back = try #require(next.flatMap(URLComponents.init(string:)))
        let query = Dictionary(uniqueKeysWithValues: (back.queryItems ?? []).map { ($0.name, $0.value ?? "") })
        let session = try await oauth.session(exchanging: try oauth.callback(query, state: state), redirect: redirect, pkce: pkce, server: server)
        let approvalDetail = try #require(CoachInstaller.approval(in: session.details, authority: session.repository))
        #expect(approvalDetail.spendCentsPerDay == 100)
        #expect(session.scopes?.contains { $0.contains("health/sleep") } == false, "the app's own scopes gained the coach's reach")

        let api = WorkoutsAPI(client: SubstrateClient(session), authority: session.repository)
        let training = try await PackageInstaller(api: api, manifest: try PackageManifest.bundled(), clientID: manifest.clientID).ensure()
        #expect(training == .current(held: 2))
        let coach = CoachInstaller(api: api, manifest: try CoachManifest.bundled(), approval: approvalDetail, clientID: manifest.clientID)
        #expect(try await coach.ensure(training: training) == .applied)
        #expect(try await coach.ensure(training: training) == .current)

        let row = try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: try await owner.send(
            "GET", SubstrateClient.recordPath("substrate.reamde.dev/core/agent", coach.agentID)).data)
        guard case let .object(record) = row, case let .object(properties)? = record["properties"] else {
            Issue.record("no agent row")
            return
        }
        #expect(properties["purpose"] == .string("primary"))
        print("coach installed:", coach.agentID, properties["provider"] ?? .null, properties["model"] ?? .null)
    }

    private func redirectOf(_ data: Data) -> String? {
        guard case let .object(object)? = try? JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: data) else { return nil }
        for key in ["redirect", "next"] {
            if case let .string(url)? = object[key], !url.isEmpty { return url }
        }
        return nil
    }
}
