import SubstrateKit
import SwiftUI
import UIKit
import XCTest

@testable import SubstrateSync

/// The sign-in an app session starts from and the status screen it gets in
/// Set Up's place, drawn to PNG files in the directory SYNC_SNAPSHOTS names,
/// for a person to look at; skipped without it
/// (`TEST_RUNNER_SYNC_SNAPSHOTS=<dir> xcodebuild … test`). Each status is
/// what the kit's check makes of a scripted server.
@MainActor
final class StatusSnapshotTests: XCTestCase {
    static let size = CGSize(width: 393, height: 852)
    let server = URL(string: "https://ada.example.com")!
    let manifest = SyncManifest.make(account: "substrate-sync-1a2b3c4d", deviceName: "Ada's iPhone", timeZone: "Europe/London")

    /// An app session approved when the server held `apple` at `appleHeld`.
    func session(appleHeld: Int = 3) -> Session {
        Session(server: server, repository: "ada.example.com", secret: "sub_app_x", clientID: SyncManifest.localClientID,
                scopes: SyncManifest.scopes + ["webhook:applehealth-webhook"],
                details: [
                    .package(AppModel.appleProvider, atLeast: appleHeld), .package(AppModel.healthProvider, atLeast: 5),
                    AuthorizationDetail(type: AuthorizationDetail.Types.account, kind: SyncEngine.healthAccountKind, id: "substrate-sync-1a2b3c4d"),
                    .webhook(trigger: SyncEngine.trigger),
                ])
    }

    func status(_ session: Session, check: String) async -> AppStatus {
        let stub = StubTransport()
        stub.on("GET", "/.well-known/substrate/server.json", json: #"{"server":{"version":"v0.114.0"},"features":[{"name":"apps","stability":"alpha"}]}"#)
        stub.on("GET", "/.well-known/oauth-authorization-server", json: #"""
        {"issuer":"https://ada.example.com","authorization_endpoint":"https://ada.example.com/oauth/authorize",
         "token_endpoint":"https://ada.example.com/oauth/token","revocation_endpoint":"https://ada.example.com/oauth/revoke",
         "scopes_supported":["read:*","write:*","agents:*","decide:*"],
         "authorization_details_types_supported":["package","account","webhook"],
         "authorization_response_iss_parameter_supported":true}
        """#)
        stub.on("POST", "/api/v1/authorizations/check", json: check)
        return await AppStatusCheck(manifest: manifest, session: session, client: SubstrateClient(session, transport: stub)).run()
    }

    func testTheStatusScreenDraws() async throws {
        guard let path = ProcessInfo.processInfo.environment["SYNC_SNAPSHOTS"] else {
            throw XCTSkip("SYNC_SNAPSHOTS names the directory to draw into")
        }
        let directory = URL(fileURLWithPath: path)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)

        // Approved at apple 2; this build writes kinds of 3.
        let earlier = session(appleHeld: 2)
        let behind = await status(earlier, check: #"""
        {"scopes":{"missing":[]},"setup":[
          {"type":"package","action":"upgrade","package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":2,"shipped":3}},
          {"type":"package","action":"none","package":{"id":"providers.substrate.reamde.dev/applehealth","required":5,"held":5,"shipped":5}},
          {"type":"account","action":"create","account":{"kind":"providers.substrate.reamde.dev/applehealth/account","id":"substrate-sync-1a2b3c4d"}},
          {"type":"webhook","action":"enable","webhook":{"trigger":"applehealth-webhook","package":"providers.substrate.reamde.dev/applehealth"}}]}
        """#)
        XCTAssertTrue(behind.changes.contains(where: \.isSetup), "\(behind)")
        try await draw(directory, "status-needs-approval", NavigationStack { ServerStatusForm(session: earlier, status: behind) })
        try await draw(directory, "status-review", ReviewChangesSheet(appName: SyncManifest.appName, host: "ada.example.com",
                                                                      changes: behind.changes, firstSignIn: false,
                                                                      onContinue: {}, onCancel: {}))
        // The owner left the deliveries out last time: Change Access offers
        // them back.
        var declined = session()
        declined.scopes = [SyncManifest.scopes[0], SyncManifest.scopes[1], "webhook:applehealth-webhook"]
        declined.requestedScopes = SyncManifest.scopes
        try await draw(directory, "status-change-access", ReviewChangesSheet(
            changingAccessFor: manifest, session: declined, changes: manifest.review(nil, session: declined),
            onContinue: {}, onCancel: {}))

        let ready = await status(session(), check: #"""
        {"scopes":{"missing":[]},"setup":[
          {"type":"package","action":"none","package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":3,"shipped":3}},
          {"type":"package","action":"none","package":{"id":"providers.substrate.reamde.dev/applehealth","required":5,"held":5,"shipped":5}},
          {"type":"account","action":"none"},{"type":"webhook","action":"unknown"}]}
        """#)
        XCTAssertTrue(ready.isClear, "\(ready)")
        try await draw(directory, "status-up-to-date", NavigationStack { ServerStatusForm(session: session(), status: ready, fromSettings: true) })
        // Apple Health is quarantined: the approval page would refuse, so the
        // screen opens the page the server names to fix it.
        let blocked = await status(session(), check: #"""
        {"scopes":{"missing":[]},"setup":[
          {"type":"package","action":"none","package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":3,"shipped":3}},
          {"type":"package","action":"upgrade","blocked":{"reason":"quarantined","problem":"Your copy of this package is quarantined, so approving cannot set it up. Its provider page says why.",
            "fix":"/catalog/providers.substrate.reamde.dev%2Fapplehealth"},
           "package":{"id":"providers.substrate.reamde.dev/applehealth","required":5,"held":4,"shipped":5}},
          {"type":"account","action":"none"},{"type":"webhook","action":"unknown"}]}
        """#)
        XCTAssertTrue(blocked.approvalBlocked, "\(blocked)")
        XCTAssertEqual(blocked.issues.fix, "/catalog/providers.substrate.reamde.dev%2Fapplehealth")
        try await draw(directory, "status-blocked", NavigationStack { ServerStatusForm(session: session(), status: blocked, fromSettings: true) })
        // Only a newer server ships what Sync needs: the approval waits for it.
        let waiting = await status(earlier, check: #"""
        {"scopes":{"missing":[]},"setup":[
          {"type":"package","action":"upgrade","blocked":{"reason":"server-update","problem":"This server ships version 2; the app needs version 3. The server needs an update first."},
           "package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":2,"shipped":2}},
          {"type":"package","action":"none","package":{"id":"providers.substrate.reamde.dev/applehealth","required":5,"held":5,"shipped":5}},
          {"type":"account","action":"create","account":{"kind":"providers.substrate.reamde.dev/applehealth/account","id":"substrate-sync-1a2b3c4d"}},
          {"type":"webhook","action":"none"}]}
        """#)
        XCTAssertTrue(waiting.issues.waitsForUpdate, "\(waiting)")
        try await draw(directory, "status-waits-for-update", NavigationStack { ServerStatusForm(session: earlier, status: waiting) })
        // Apple Health ships at the version Sync needs, but a package it
        // needs first ships too old: the server's sentence says so.
        let prerequisite = await status(session(), check: #"""
        {"scopes":{"missing":[]},"setup":[
          {"type":"package","action":"none","package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":3,"shipped":3}},
          {"type":"package","action":"upgrade","blocked":{"reason":"server-update","problem":"It needs a package first at version 2, and this server ships version 1. The server needs an update first."},
           "package":{"id":"providers.substrate.reamde.dev/applehealth","required":5,"held":4,"shipped":5}},
          {"type":"account","action":"none"},{"type":"webhook","action":"unknown"}]}
        """#)
        XCTAssertTrue(prerequisite.issues.waitsForUpdate, "\(prerequisite)")
        try await draw(directory, "status-prerequisite-waits", NavigationStack { ServerStatusForm(session: session(), status: prerequisite) })

        // The real sign-in, on a server it found.
        let metadata = AuthorizationServerMetadata(issuer: server, scopesSupported: ["read:*", "write:*", "agents:*", "decide:*"],
                                                   authorizationDetailsTypesSupported: AuthorizationDetail.Types.all)
        let found = ServerProbe.Result(origin: server, discovery: Discovery(server: .init(version: "v0.114.0")), metadata: metadata,
                                       verdict: SyncManifest.requirement.verdict(version: "v0.114.0", metadata: metadata))
        let model = AppModel()
        try await draw(directory, "first-sign-in", NavigationStack { SignInView(probe: found) }.environmentObject(model))
    }

    /// Hosts the view in a window of its own at an iPhone's size, in light
    /// mode, lets it settle, and writes what it drew.
    func draw(_ directory: URL, _ name: String, _ view: some View) async throws {
        let scene = try XCTUnwrap(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(origin: .zero, size: Self.size)
        window.overrideUserInterfaceStyle = .light
        window.rootViewController = UIHostingController(rootView: view)
        window.isHidden = false
        defer { window.isHidden = true }
        try await Task.sleep(for: .milliseconds(800))
        let image = UIGraphicsImageRenderer(bounds: window.bounds).image { _ in
            window.drawHierarchy(in: window.bounds, afterScreenUpdates: true)
        }
        try XCTUnwrap(image.pngData()).write(to: directory.appendingPathComponent("\(name).png"))
    }
}
