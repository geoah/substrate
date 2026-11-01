import SubstrateKit
import SwiftUI
import UIKit
import XCTest

@testable import SubstrateSync

/// The status screen an app session gets in Set Up's place, drawn to PNG
/// files in the directory SYNC_SNAPSHOTS names, for a person to look at;
/// skipped without it (`TEST_RUNNER_SYNC_SNAPSHOTS=<dir> xcodebuild … test`).
/// Each status is what the kit's check makes of a scripted server.
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
          {"type":"webhook","action":"arm","webhook":{"trigger":"applehealth-webhook","enabled":false,"keyed":true}}]}
        """#)
        XCTAssertTrue(behind.changes.contains(where: \.isSetup), "\(behind)")
        try await draw(directory, "status-needs-approval", NavigationStack { ServerStatusForm(session: earlier, status: behind) })
        try await draw(directory, "status-review", ReviewChangesSheet(appName: SyncManifest.appName, host: "ada.example.com",
                                                                      changes: behind.changes, firstSignIn: false,
                                                                      onContinue: {}, onCancel: {}))

        let ready = await status(session(), check: #"""
        {"scopes":{"missing":[]},"setup":[
          {"type":"package","action":"none","package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":3,"shipped":3}},
          {"type":"package","action":"none","package":{"id":"providers.substrate.reamde.dev/applehealth","required":5,"held":5,"shipped":5}},
          {"type":"account","action":"none"},{"type":"webhook","action":"unknown"}]}
        """#)
        XCTAssertTrue(ready.isClear, "\(ready)")
        try await draw(directory, "status-up-to-date", NavigationStack { ServerStatusForm(session: session(), status: ready, fromSettings: true) })
        try await draw(directory, "first-sign-in-review", ReviewChangesSheet(
            appName: SyncManifest.appName, host: "ada.example.com",
            changes: manifest.plannedChanges(for: AuthorizationServerMetadata(issuer: server, scopesSupported: ["read:*", "write:*"],
                                                                              authorizationDetailsTypesSupported: AuthorizationDetail.Types.all)),
            firstSignIn: true, onContinue: {}, onCancel: {}))
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
