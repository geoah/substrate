import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import Testing
import WorkoutsCore
@testable import WorkoutsSync
import SubstrateCore

/// The workout coach's install against a real substrate, skipped unless
/// `WORKOUTS_E2E_COACH_SERVER` names one and `WORKOUTS_E2E_OWNER_TOKEN` holds
/// a full-access token of a repository with no training package. The server
/// must read the publisher's authority document, live or through
/// `SUBSTRATE_INSECURE_AUTHORITY_DOCS`, and hold a provider row that prices
/// one of the coach's models: the app's whole sign-in request, approved
/// through the approval API the console's page calls with that row bound to
/// the coach's `model` input, the code exchanged, and the agent read back
/// with the owner's token as the closure declares it. The coach is left
/// installed, for Substrate Chat to find.
///
///     WORKOUTS_E2E_COACH_SERVER=http://localhost:8097 WORKOUTS_E2E_OWNER_TOKEN=… swift test --filter LiveCoach
@Suite("Live coach", .enabled(if: ProcessInfo.processInfo.environment["WORKOUTS_E2E_COACH_SERVER"] != nil))
struct LiveCoachTests {
    let server = URL(string: ProcessInfo.processInfo.environment["WORKOUTS_E2E_COACH_SERVER"] ?? "http://localhost")!
    let ownerToken = ProcessInfo.processInfo.environment["WORKOUTS_E2E_OWNER_TOKEN"] ?? ""
    let redirect = "http://127.0.0.1:53112/oauth/callback"

    @Test("sign in with the coach's package, approve, read the agent back")
    func theCoach() async throws {
        let manifest = WorkoutsManifest.manifest(clientID: WorkoutsManifest.localClientID)
        let probe = try await ServerProbe().probe(server, requirement: manifest.requirement, app: manifest.appName)
        let metadata = try #require(probe.metadata)
        #expect(manifest.details(for: metadata).map(\.package) == [WorkoutsManifest.healthPackage, WorkoutsKinds.packageID, Coach.package])
        let oauth = OAuthClient(manifest: manifest, metadata: metadata)
        let pkce = try PKCE()
        let state = try PKCE.state()
        var request = URLRequest(url: try oauth.authorizeURL(redirect: redirect, pkce: pkce, state: state))
        request.httpMethod = "GET"
        let (_, parked) = try await URLSessionTransport.shared.data(for: request)
        let location = try #require(parked.value(forHTTPHeaderField: "Location"))
        let id = try #require(location.split(separator: "/").last.map(String.init))

        let owner = SubstrateClient(server: server, token: ownerToken)
        let page = try JSONDecoder().decode(JSONValue.self, from: try await owner.send("GET", "api/v1/authorizations/\(id)").data)
        guard case let .object(fields) = page, case let .array(scopes)? = fields["scopes"], case let .array(setup)? = fields["setup"] else {
            Issue.record("no scopes")
            return
        }
        // The coach's package row, blocked by nothing: its reach is inside
        // the scopes asked for (decision 0215).
        let coachRow = setup.compactMap { row -> JSONObject? in
            guard case let .object(row) = row, case let .object(package)? = row["package"], package["id"] == .string(Coach.package) else { return nil }
            return row
        }.first
        #expect(coachRow != nil && coachRow?["blocked"] == nil, "the coach's row: \(String(describing: coachRow))")
        let approved = scopes.compactMap { scope -> JSONValue? in
            guard case let .object(row) = scope else { return nil }
            return row["scope"]
        }
        var approval: JSONObject = ["scopes": .array(approved), "lifetimeDays": .number(30)]
        if let hash = fields["planHash"] { approval["planHash"] = hash }
        if let inputs = fields["inputs"] { approval["inputs"] = inputs }
        let encoder = JSONEncoder()
        encoder.outputFormatting = .withoutEscapingSlashes
        var answer = try await owner.send("POST", "api/v1/authorizations/\(id)", body: .json(encoder.encode(JSONValue.object(approval))),
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
        #expect(session.scopes?.contains(Coach.agentsScope) == true)

        let api = WorkoutsAPI(client: SubstrateClient(session))
        let training = try await PackageCheck(api: api, manifest: try PackageManifest.bundled()).state()
        #expect(training.writable, "training: \(training)")

        let row = try JSONDecoder().decode(JSONValue.self, from: try await owner.send(
            "GET", SubstrateClient.recordPath(Coach.agentKind, Coach.agent)).data)
        guard case let .object(record) = row, case let .object(properties)? = record["properties"] else {
            Issue.record("no agent row")
            return
        }
        #expect(properties["purpose"] == .string("primary"))
        #expect(properties["providerInput"] == .string("model"))
        print("coach installed:", Coach.agent, properties["models"] ?? .null)
    }

    private func redirectOf(_ data: Data) -> String? {
        guard case let .object(object)? = try? JSONDecoder().decode(JSONValue.self, from: data) else { return nil }
        for key in ["redirect", "next"] {
            if case let .string(url)? = object[key], !url.isEmpty { return url }
        }
        return nil
    }
}
