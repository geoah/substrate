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
        #expect(Self.words(line) == ["F1", "↗", "BBC", "↗", " and ", "{Q4}", " and bold one", "↗", "."])
        #expect(!inlinePieces(parseInline("Only [Q4](substrate://\(kind)/q4).")).contains(.leaves))
    }

    /// A line's pieces as words: a mark as ↗, a record's pill in braces.
    static func words(_ line: String, missing: Set<String> = []) -> [String] {
        inlinePieces(parseInline(line, missing: missing)).map { piece in
            switch piece {
            case let .text(part): String(part.characters)
            case .leaves: "↗"
            case let .record(_, name): "{\(String(name.characters))}"
            }
        }
    }

    @Test func aRecordLinkIsItsPillWhereverItStandsInTheLine() {
        let q4 = "[Q4](substrate://\(kind)/q4)"
        // At the start, in the middle, against punctuation, at the end.
        #expect(Self.words("\(q4) is due") == ["{Q4}", " is due"])
        #expect(Self.words("See \(q4), then (\(q4)).") == ["See ", "{Q4}", ", then (", "{Q4}", ")."])
        #expect(Self.words("Only \(q4)") == ["Only ", "{Q4}"])
        // Two records side by side are two pills, and the same one twice is two.
        #expect(Self.words("\(q4)[Q5](ref:\(kind)/q5)\(q4)") == ["{Q4}", "{Q5}", "{Q4}"])
        // Formatting inside the link's words stays one pill.
        #expect(Self.words("[the **Q4** budget](substrate://\(kind)/q4)") == ["{the Q4 budget}"])
        // Beside a link that leaves the app, each keeps its own form.
        #expect(Self.words("\(q4)[F1](https://www.formula1.com)\(q4)") == ["{Q4}", "F1", "↗", "{Q4}"])
    }

    @Test func aPillNamesTheRecordTheLinkDoesAndAMissingOneIsNoPill() {
        let pieces = inlinePieces(parseInline("[Q4](ref:\(kind)/q4) and [Ghost](substrate://\(kind)/ghost)", missing: ["\(kind)/ghost"]))
        #expect(pieces.first == .record(RecordReference(kind: kind, id: "q4"), AttributedString("Q4")))
        #expect(Self.words("[Ghost](substrate://\(kind)/ghost)", missing: ["\(kind)/ghost"]) == ["Ghost"])
        // Half a link, as a reply streams it, is its text until it closes.
        #expect(Self.words("See [Q4](substrate://\(kind)/q") == ["See [Q4](substrate://\(kind)/q"])
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

/// What a screen reader is told of the Send button's keys, read the same
/// way: Command-Return where a hardware keyboard is expected, nothing on a
/// phone.
@MainActor @Suite struct SendButtonKeys {
    @Test func theSendButtonSaysCommandReturnOnAMacAndAnIPadAndNothingOnAPhone() throws {
        #expect(try Self.help(on: .mac) == "Command-Return sends too.")
        #expect(try Self.help(on: .pad) == "Command-Return sends too.")
        #expect((try Self.help(on: .phone) ?? "").isEmpty)
    }

    private static func help(on device: ComposerKeys.Device) throws -> String? {
        let hosting = NSHostingView(rootView: Button("Send") {}.sendShortcut(on: device))
        hosting.frame = CGRect(x: 0, y: 0, width: 120, height: 60)
        hosting.layoutSubtreeIfNeeded()
        let button = try #require(LeavingTheApp.elements(under: hosting).first { $0.accessibilityRole?() == .button })
        return button.accessibilityHelp?()
    }
}

/// What a screen reader is told of a record's pill, read the same way: its
/// kind and its name, as a link where it opens the record, the same as the
/// pill inside a line of a reply, which is a link in the text.
@MainActor @Suite struct RecordPillAccessibility {
    let task = RecordReference(kind: "ada.example.com/tasks/task", id: "q4")

    private static func host(_ view: some View) -> NSHostingView<AnyView> {
        let hosting = NSHostingView(rootView: AnyView(view))
        hosting.frame = CGRect(x: 0, y: 0, width: 320, height: 60)
        hosting.layoutSubtreeIfNeeded()
        return hosting
    }

    @Test func thePillIsALinkThatSaysItsKindAndItsName() throws {
        let hosting = Self.host(RecordPill(task, title: "Q4 budget").environment(\.openRecord, OpenRecordAction { _ in }))
        let all = LeavingTheApp.elements(under: hosting)
        let link = try #require(all.first { $0.accessibilityRole?() == .link })
        #expect(link.accessibilityLabel?() == "Task: Q4 budget")
        #expect(link.accessibilityHelp?() == RecordPillStyle.hint)
        #expect(!all.contains { $0.accessibilityRole?() == .button })
    }
}
#endif
