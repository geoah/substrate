import ChatKit
import Foundation
import SubstrateKit
import Testing

@testable import SubstrateChat

#if os(macOS)
import AppKit
import SwiftUI
#endif

/// What only the Apple app holds; the chat's rules are ChatKit's.
@Suite struct LoopbackSignIn {
    @Test(.timeLimit(.minutes(1))) func theLoopbackReceiverHandsOverTheRedirectsQuery() async throws {
        let receiver = try await LoopbackReceiver.start(appName: ChatManifest.appName, path: ChatManifest.manifest.loopbackPath)
        defer { receiver.stop() }
        #expect(receiver.port > 0)
        let url = try #require(URL(string: receiver.redirect + "?code=abc&state=xyz&iss=https%3A%2F%2Fs.example.com"))
        async let page = URLSession.shared.data(from: url)
        let query = try await receiver.next()
        #expect(query["code"] == "abc")
        #expect(query["state"] == "xyz")
        #expect(query["iss"] == "https://s.example.com")
        let (body, response) = try await page
        #expect((response as? HTTPURLResponse)?.statusCode == 200)
        #expect(String(decoding: body, as: UTF8.self).contains("Signed in. You can close this tab and go back to Substrate Chat."))
    }
}

/// The session as every build before the kit kept it, field for field.
private struct LegacySession: Codable {
    struct Grants: Codable { var scopes: [String] }
    var server: URL
    var repository: String
    var secret: String
    var tokenID: String
    var grants: Grants?
    var expiresAt: Date?
    var revocation: URL?
}

@Suite struct KeptSession {
    @Test func theKeychainItemIsWhereEveryBuildKeptIt() throws {
        let store = SessionStore.chat
        let keychain = try #require(store.storage as? KeychainStorage)
        #expect(keychain.service == "dev.reamde.substrate.chat")
        #expect(store.account == "session")
        #expect(keychain.accessGroup == nil)
        #expect(keychain.accessibility == .afterFirstUnlockThisDeviceOnly)
        #expect(!keychain.dataProtection)

        // The bytes an earlier build wrote, read back through the kit's store
        // under the same service.
        let ends = Date(timeIntervalSinceReferenceDate: 815_000_000)
        let app = LegacySession(
            server: URL(string: "https://s.example.com")!, repository: "r.example.com", secret: "x", tokenID: "",
            grants: .init(scopes: ChatManifest.scopes), expiresAt: ends,
            revocation: URL(string: "https://s.example.com/oauth/revoke"))
        let scratch = SessionStore(account: "test-\(UUID().uuidString)", storage: store.storage)
        defer { try? keychain.delete(account: scratch.account) }
        try keychain.write(JSONEncoder().encode(app), account: scratch.account)
        guard case let .found(session) = scratch.read() else {
            Issue.record("the kept session did not read")
            return
        }
        #expect(session.scopes == ChatManifest.scopes)
        #expect(session.expiresAt == ends)
    }
}

@MainActor @Suite struct InlineMarkdown {
    let kind = "ada.example.com/tasks/task"

    @Test func aRecordLinkStaysALinkAndAMissingOneIsText() {
        let text = "[Q4](ref:\(kind)/q4) and [Ghost](substrate://\(kind)/ghost) and [x](javascript:alert)"
        let out = parseInline(text, missing: ["\(kind)/ghost"])
        let links = out.runs.compactMap(\.link)
        #expect(links == [URL(string: "substrate://\(kind)/q4")!])
        #expect(String(out.characters) == "Q4 and Ghost and x")
    }

    @Test func aLinkThatLeavesTheAppIsMarkedOnceAfterItsTextAndARecordLinkIsNot() {
        let line = "[F1](https://www.formula1.com)[BBC](https://www.bbc.co.uk) and [Q4](ref:\(kind)/q4) and [**bold** one](mailto:a@example.com)."
        let words = inlinePieces(parseInline(line)).map { piece -> String in
            switch piece {
            case let .text(part): String(part.characters)
            case .leaves: "↗"
            }
        }
        #expect(words == ["F1", "↗", "BBC", "↗", " and Q4 and bold one", "↗", "."])
        #expect(!inlinePieces(parseInline("Only [Q4](substrate://\(kind)/q4).")).contains(.leaves))
    }

    @Test func aConsolePathStaysALinkThatLeavesTheAppAndAHostlessOneIsText() {
        let out = parseInline("[Books](/applets/books) and [Away](//evil.example.com/x) and [Rel](applets/books)")
        #expect(out.runs.compactMap(\.link).map(\.relativeString) == ["/applets/books"])
        #expect(inlinePieces(parseInline("Open [Books](/applets/books).")).contains(.leaves))
        #expect(consolePath(URL(string: "/applets/books")!) == "/applets/books")
        #expect(consolePath(URL(string: "https://ada.example.com/applets/books")!) == nil)
    }
}

#if os(macOS)
/// What a screen reader is told of a link that leaves the app, read from the
/// accessibility tree the system is handed for the view.
@MainActor @Suite struct LeavingTheApp {
    @Test func aLinkToTheConsoleSaysItOpensInTheBrowser() throws {
        let hosting = NSHostingView(rootView: ConsoleLink(title: "Open in the Console", destination: URL(string: "https://s.example.com/")!))
        hosting.frame = CGRect(x: 0, y: 0, width: 320, height: 60)
        hosting.layoutSubtreeIfNeeded()
        let link = try #require(Self.elements(under: hosting).first { $0.accessibilityRole?() == .link })
        #expect(link.accessibilityLabel?() == "Open in the Console")
        #expect(link.accessibilityHelp?() == ExternalMark.words)
    }

    /// Every element under `root`, depth first.
    static func elements(under root: AnyObject) -> [AnyObject] {
        let children = (root.accessibilityChildren?() ?? nil) ?? []
        return children.flatMap { child in [child as AnyObject] + elements(under: child as AnyObject) }
    }
}
#endif
