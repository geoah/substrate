import ChatKit
import Foundation
import SubstrateKit
import Testing
import UserNotifications

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

/// A push from the publisher's relay carries no words: its `loc-key` names
/// a string in the app's own bundle, which iOS shows whether or not any
/// code of the app runs. A key the bundle lacks would show as the key.
@Suite struct PushWords {
    @Test func everyKeyARelayNamesIsInTheAppsStrings() {
        for key in ["SUBSTRATE_PUSH_REPLY", "SUBSTRATE_PUSH_FAILED", "SUBSTRATE_PUSH_REQUEST"] {
            let words = Bundle.main.localizedString(forKey: key, value: nil, table: nil)
            #expect(words != key && !words.isEmpty, "\(key) is not in Localizable.strings")
        }
    }
}

/// What the notification service extension delivers, compiled here from
/// its own file (`PushDelivery.swift`): an extension has no test target.
@Suite struct ExtensionDelivery {
    static func pushed() -> UNMutableNotificationContent {
        let content = UNMutableNotificationContent()
        content.body = "A chat has a new reply."
        content.badge = 3
        content.categoryIdentifier = "chat.approval"
        content.userInfo = ["substrate": ["v": 1, "event": "reply", "thread": "t1", "g": "gC", "eventId": "e1"], "aps": ["badge": 3]]
        return content
    }

    @Test func whatTheItemSaysReplacesTheWordsAndKeepsThePush() throws {
        let content = Self.pushed()
        let request = NotificationRequest(
            id: "x", title: "Moss", body: "Here is the summary.", thread: "t1", category: nil, userInfo: ["thread": "t1", "card": "c1"],
            subtitle: "Weekend plans")
        PushDelivery.apply(.shown(request), to: content)
        #expect(content.title == "Moss" && content.subtitle == "Weekend plans" && content.body == "Here is the summary.")
        // No category: no action is offered.
        #expect(content.categoryIdentifier == "")
        #expect(content.threadIdentifier == "t1")
        #expect(content.userInfo["thread"] as? String == "t1" && content.userInfo["card"] as? String == "c1")
        // The relay's own values stay, for a tap, an action and a take-back.
        let substrate = try #require(content.userInfo["substrate"] as? [String: Any])
        #expect(substrate["g"] as? String == "gC" && substrate["eventId"] as? String == "e1")
        #expect(content.badge == 3)
    }

    @Test func anotherRegistrationsPushDropsItsBadgeAndAnUnreadKeychainKeepsIt() {
        let foreign = Self.pushed()
        PushDelivery.apply(.fallback(dropsBadge: true), to: foreign)
        #expect(foreign.badge == nil && foreign.body == "A chat has a new reply.")
        let unknown = Self.pushed()
        PushDelivery.apply(.fallback(dropsBadge: false), to: unknown)
        #expect(unknown.badge == 3)
    }

    @Test func thePushIsDeliveredOnceWhicheverEndsFirst() {
        final class Count: @unchecked Sendable {
            var delivered: [UNNotificationContent] = []
        }
        let count = Count()
        let once = DeliverOnce(Self.pushed()) { count.delivered.append($0) }
        // The work ends first: what it made goes.
        #expect(once.deliver(.fallback(dropsBadge: false)))
        // iOS calls time after: nothing more is delivered.
        #expect(!once.expire(PushExpiry()))
        #expect(count.delivered.count == 1 && count.delivered.first?.badge == 3)
    }

    /// iOS ends the extension while the first Keychain read is still out:
    /// the delivery is claimed first, as the plain fallback with no chat
    /// text and no badge, and the work, stopped after, delivers nothing,
    /// though the Keychain then accepts the push.
    @Test func anExpiryWhileTheKeychainIsReadClaimsTheDeliveryFirst() async throws {
        final class Delivered: @unchecked Sendable {
            private let lock = NSLock()
            private var kept: [UNNotificationContent] = []
            func append(_ content: UNNotificationContent) { lock.withLock { kept.append(content) } }
            var all: [UNNotificationContent] { lock.withLock { kept } }
        }
        actor Keychain {
            private var waiters: [CheckedContinuation<Void, Never>] = []
            private var opened = false
            var waiting: Int { waiters.count }
            func read() async -> PushAcceptance {
                if !opened { await withCheckedContinuation { waiters.append($0) } }
                return PushAcceptance(.accepted)
            }
            func open() {
                opened = true
                for waiter in waiters { waiter.resume() }
                waiters = []
            }
        }
        let delivered = Delivered()
        let keychain = Keychain()
        let run = PushRun(Self.pushed()) { delivered.append($0) }
        run.start(PushPayload.flatten(Self.pushed().userInfo), accepting: { _ in await keychain.read() }, until: .now + .seconds(60))
        while await keychain.waiting == 0 { try await Task.sleep(for: .milliseconds(5)) }
        #expect(run.expire())
        #expect(delivered.all.count == 1)
        #expect(delivered.all.first?.badge == nil && delivered.all.first?.body == "A chat has a new reply.")
        await keychain.open()
        try await Task.sleep(for: .milliseconds(300))
        #expect(delivered.all.count == 1)
        #expect(!run.expire())
    }

    /// iOS ends the extension before the last check answered: nothing says
    /// the push is still this sign-in's, so it goes as the plain fallback,
    /// with no chat text and no badge.
    @Test func anExpiryBeforeTheLastCheckShowsTheFallbackWithNoBadge() {
        final class Count: @unchecked Sendable {
            var delivered: [UNNotificationContent] = []
        }
        let count = Count()
        let once = DeliverOnce(Self.pushed()) { count.delivered.append($0) }
        #expect(once.expire(PushExpiry()))
        #expect(!once.deliver(.fallback(dropsBadge: false)))
        let delivered = count.delivered.first
        #expect(count.delivered.count == 1 && delivered?.badge == nil && delivered?.body == "A chat has a new reply.")
    }
}

/// The app and its extension read one session item: the definition both
/// compile, at the place every earlier build kept it.
@Suite struct SharedSessionStore {
    @Test func theAppAndTheExtensionReadTheItemEveryBuildKept() {
        let earlier = SessionStore(service: "dev.reamde.substrate.chat")
        #expect(SessionStore.chat.account == earlier.account && SessionStore.chat.account == "session")
        #expect(SessionStore.chat.storage.location == earlier.storage.location)
    }
}
