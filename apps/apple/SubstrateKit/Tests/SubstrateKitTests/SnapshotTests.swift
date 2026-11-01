#if os(macOS)
import AppKit
import Foundation
import SwiftUI
import Testing
@testable import SubstrateKit

/// Every screen of the kit in the states an app shows it in, drawn to PNG
/// files in the directory SUBSTRATEKIT_SNAPSHOTS names, for a person to look
/// at; skipped without it. A view is hosted in an offscreen window and its
/// layer cached, since `ImageRenderer` draws a text field or a list as a
/// placeholder.
@Suite(.enabled(if: ProcessInfo.processInfo.environment["SUBSTRATEKIT_SNAPSHOTS"] != nil))
@MainActor
struct Snapshots {
    static let size = CGSize(width: 393, height: 852)
    let directory = URL(fileURLWithPath: ProcessInfo.processInfo.environment["SUBSTRATEKIT_SNAPSHOTS"] ?? NSTemporaryDirectory())
    let server = URL(string: "https://ada.example.com")!
    let now = Date()

    var manifest: AppManifest { syncManifest() }

    var metadata: AuthorizationServerMetadata {
        AuthorizationServerMetadata(issuer: server, scopesSupported: ["read:*", "write:*", "agents:*", "decide:*"],
                                    authorizationDetailsTypesSupported: AuthorizationDetail.Types.all)
    }

    var header: ConnectHeader {
        ConnectHeader(appName: "Substrate Sync", tagline: "Your iPhone's contacts, calendars, places and Health, in your substrate.")
    }

    var update: [Change] {
        [
            Change(kind: .scope, words: ScopeWords.sentence("write:providers.substrate.reamde.dev/apple/place")),
            Change(kind: .upgrade, words: "Update Apple", detail: "Version 2 to 3"),
        ]
    }

    func connect(_ phase: ConnectView<ConnectHeader, EmptyView>.Phase, address: String) -> some View {
        ConnectView(manifest: manifest, address: .constant(address), probe: ServerProbe(), header: header,
                    onSignIn: { _ in }, passwordDoor: { _ in EmptyView() }, phase: phase)
    }

    func screen(@ViewBuilder _ content: () -> some View) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                Text("Substrate Sync").font(.largeTitle.weight(.bold))
                content()
                Text("Contacts, calendars and places sync in the background.")
                    .foregroundStyle(.secondary)
            }
            .padding(20)
        }
    }

    @Test func everyScreenDraws() async throws {
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let actions = AppStatusActions(review: {}, signInAgain: {}, moveToAppAccess: {}, retry: {}, openServer: {})
        let result = ServerProbe.Result(origin: server, discovery: Discovery(server: .init(version: "v0.114.0")),
                                        metadata: metadata, verdict: .compatible)

        try await draw("connect-empty", connect(.idle, address: ""))
        try await draw("connect-checking", connect(.checking, address: "ada.example.com"))
        try await draw("connect-found", connect(.found(result), address: "https://ada.example.com"))
        try await draw("server-too-old", ScrollView {
            ServerTooOldView(appName: "Substrate Sync", host: "ada.example.com", running: "v0.113.0", needed: "0.114.0",
                             missing: [ServerRequirement.detailWords("account"), ServerRequirement.detailWords("webhook")],
                             onTryAgain: {}, onChangeServer: {})
                .padding(24)
        })
        try await draw("banner-needs-approval", screen {
            AppStatusBanner(issues: [.needsApproval(changes: update)], appName: "Substrate Sync", host: "ada.example.com",
                            actions: actions)
        })
        try await draw("banner-blocked", screen {
            AppStatusBanner(issues: [.needsApproval(changes: [
                Change(kind: .install, words: "Install Apple"),
                Change(kind: .install, words: "Install Apple Health", detail: "Open your substrate to see what is in the way.", blocked: true),
            ])], appName: "Substrate Sync", host: "ada.example.com", actions: actions)
        })
        try await draw("banner-ends-soon", screen {
            AppStatusBanner(issues: [.accessEndsSoon(now.addingTimeInterval(3 * 86400))], appName: "Substrate Sync",
                            host: "ada.example.com", actions: actions)
        })
        try await draw("banner-legacy", screen {
            AppStatusBanner(issues: [.legacyAccess], appName: "Substrate Sync", host: "ada.example.com", actions: actions)
        })
        try await draw("banner-several", screen {
            AppStatusBanner(issues: [
                .serverTooOld(running: "v0.114.0", needed: nil, missing: ["install Apple version 3 or later"]),
                .needsApproval(changes: update), .accessEndsSoon(now.addingTimeInterval(3 * 86400)),
            ], appName: "Substrate Sync", host: "ada.example.com", actions: actions)
        })
        try await draw("access-ending-banner", screen {
            AccessEndingBanner(appName: "Substrate Sync", host: "ada.example.com", date: now.addingTimeInterval(86400), onRenew: {})
        })
        try await draw("review-first-sign-in", ReviewChangesSheet(
            appName: "Substrate Sync", host: "ada.example.com", changes: manifest.plannedChanges(for: metadata), firstSignIn: true,
            onContinue: {}, onCancel: {}))
        try await draw("review-update", ReviewChangesSheet(
            appName: "Substrate Sync", host: "ada.example.com", changes: update, firstSignIn: false, onContinue: {}, onCancel: {}))
        let session = Session(server: server, repository: "ada.example.com", secret: "sub_app_x", clientID: manifest.clientID,
                              scopes: manifest.scopes + ["webhook:applehealth-webhook"],
                              expiresAt: now.addingTimeInterval(30 * 86400))
        try await draw("account-section", Form {
            AccountSection(session: session, manifest: manifest, serverVersion: "v0.114.0", onChangeAccess: {}, onSignOut: {})
        }.formStyle(.grouped))
        try await draw("account-section-password", Form {
            AccountSection(session: Session(server: server, repository: "ada.example.com", secret: "s", tokenID: "t"),
                           serverVersion: "v0.113.0", onSignOut: {})
        }.formStyle(.grouped))
        try await draw("access-ended", AccessEndedView(appName: "Substrate Sync", host: "ada.example.com", onSignInAgain: {}))
    }

    /// Hosts the view in an offscreen window at an iPhone's size, in light
    /// mode, lets it settle, and writes what it drew.
    func draw(_ name: String, _ view: some View) async throws {
        _ = NSApplication.shared
        let hosting = NSHostingView(rootView: view
            .frame(width: Self.size.width, height: Self.size.height)
            .background(Color(nsColor: .windowBackgroundColor))
            .environment(\.colorScheme, .light)
            // An offscreen window is never key, and its controls would draw
            // as a background window's.
            .environment(\.controlActiveState, .key))
        hosting.frame = CGRect(origin: .zero, size: Self.size)
        let window = NSWindow(contentRect: hosting.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        window.appearance = NSAppearance(named: .aqua)
        window.contentView = hosting
        hosting.layoutSubtreeIfNeeded()
        try await Task.sleep(for: .milliseconds(300))
        hosting.layoutSubtreeIfNeeded()
        let rep = try #require(hosting.bitmapImageRepForCachingDisplay(in: hosting.bounds))
        hosting.cacheDisplay(in: hosting.bounds, to: rep)
        let png = try #require(rep.representation(using: .png, properties: [:]))
        try png.write(to: directory.appendingPathComponent("\(name).png"))
        window.contentView = nil
    }
}
#endif
