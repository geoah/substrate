import Foundation
import Testing
@testable import SubstrateKit

/// The cases of internal/scope's scope_test.go, so the app's answer is the
/// server's.
@Suite struct GrantReach {
    @Test func aGrantReachesWhatItNamesAndNeverTheMachinery() {
        let g = Grants(["read:ada.example.com/people/*", "write:ada.example.com/tasks/task", "read:substrate.reamde.dev/core/kind"])
        let cases: [(kind: String, purpose: String?, reads: Bool, writes: Bool)] = [
            ("ada.example.com/people/person", "primary", true, false),
            ("ada.example.com/tasks/task", "primary", true, true),
            ("ada.example.com/tasks/project", "primary", false, false),
            ("substrate.reamde.dev/core/kind", "internal", true, false),
            ("substrate.reamde.dev/core/token", "internal", false, false),
        ]
        for c in cases {
            #expect(g.reads(c.kind, purpose: c.purpose) == c.reads, "reads \(c.kind)")
            #expect(g.writes(c.kind, purpose: c.purpose) == c.writes, "writes \(c.kind)")
        }

        let everything = Grants(["write:*"])
        let wide: [(kind: String, purpose: String, reads: Bool, writes: Bool)] = [
            ("ada.example.com/tasks/task", "primary", true, true),
            ("substrate.reamde.dev/observations/observation", "primary", true, true),
            ("substrate.reamde.dev/core/trigger", "internal", false, false),
            ("substrate.reamde.dev/llm/provider", "internal", false, false),
            ("substrate.reamde.dev/console/view", "internal", false, false),
            ("substrate.reamde.dev/core/token", "internal", false, false),
            ("substrate.reamde.dev/core/secret", "internal", false, false),
        ]
        for c in wide {
            #expect(everything.reads(c.kind, purpose: c.purpose) == c.reads, "write:* reads \(c.kind)")
            #expect(everything.writes(c.kind, purpose: c.purpose) == c.writes, "write:* writes \(c.kind)")
        }
        // The guard is the kind, not the purpose.
        for k in ["token", "credential", "secret", "recoverykey"] {
            let kind = "substrate.reamde.dev/core/\(k)"
            #expect(!everything.reads(kind, purpose: "primary") && !everything.writes(kind, purpose: "primary"))
            #expect(!Grants(["read:\(kind)"]).reads(kind, purpose: "internal"))
        }
        // Even a non-internal kind of core or llm is never written.
        #expect(!everything.writes("substrate.reamde.dev/core/unit", purpose: "primary"))
    }

    @Test func withoutItsPurposeASeededKindCountsAsInternal() {
        let all = Grants(["agents:*", "read:*"])
        #expect(all.reads("a.example.com/people/person"))
        #expect(!all.reads("substrate.reamde.dev/core/kind"))
        #expect(!all.reads("substrate.reamde.dev/llm/provider"))
        #expect(all.reads("substrate.reamde.dev/observations/observation"))
        let exact = Grants(["read:substrate.reamde.dev/llm/provider"])
        #expect(exact.reads("substrate.reamde.dev/llm/provider"))
        // A declared purpose wins over the package.
        #expect(!all.reads("a.example.com/tasks/journal", purpose: "internal"))
    }

    @Test func anAgentScopeReachesItsAgentsAndNoKindWhole() {
        let g = Grants(["agents:ada.example.com/chat/*", "decide:ada.example.com/chat/assistant"])
        for c in [("ada.example.com/chat/assistant", true, true), ("ada.example.com/chat/writer", true, false),
                  ("ada.example.com/other/assistant", false, false), ("ada.example.com.evil/chat/assistant", false, false)]
        {
            #expect(g.talksTo(c.0) == c.1, "talks to \(c.0)")
            #expect(g.decides(c.0) == c.2, "decides \(c.0)")
        }
        #expect(g.talks && g.decidesAny)
        for kind in ["substrate.reamde.dev/core/agent", "substrate.reamde.dev/llm/thread", "substrate.reamde.dev/llm/message",
                     "substrate.reamde.dev/core/recordpatchrequest"]
        {
            #expect(!g.reads(kind, purpose: "internal") && !g.writes(kind, purpose: "internal"))
        }
        let reads = Grants(["read:*"])
        #expect(!reads.talks && !reads.talksTo("ada.example.com/chat/assistant") && !reads.decidesAny)
    }

    @Test func aWebhookScopeReachesOneTriggerAndNoKind() {
        let g = Grants(["write:providers.example.com/apple/*", "webhook:applehealth-webhook"])
        #expect(g.delivers("applehealth-webhook"))
        #expect(!g.delivers("whoop-webhook") && !g.delivers(""))
        #expect(!g.reads("substrate.reamde.dev/core/trigger", purpose: "internal"))
        #expect(!g.writes("substrate.reamde.dev/core/trigger", purpose: "internal"))
        #expect(!Grants(["webhook:applehealth-webhook"]).reads("providers.example.com/apple/contact", purpose: "primary"))
    }
}

@Suite struct FullAccess {
    @Test func fullReachesEverythingButAWebhook() {
        let g = Grants(["full"])
        #expect(g.full)
        #expect(g.reads("substrate.reamde.dev/core/token", purpose: "internal"))
        #expect(g.writes("substrate.reamde.dev/llm/provider", purpose: "internal"))
        #expect(g.talksTo("ada.example.com/llm/substrate") && g.decides("ada.example.com/llm/substrate"))
        #expect(g.talks && g.decidesAny)
        #expect(!g.delivers("applehealth-webhook"))
        #expect(!Grants(["read:*"]).full)
    }

    @Test func fullIsSaidAsTheConsoleSaysIt() {
        #expect(ScopeWords.sentence("full") == ScopeWords.fullSentence)
        #expect(ScopeWords.fullSentence.hasPrefix("Do everything you can do when you sign in"))
        #expect(ScopeWords.resolve("full", authority: "ada.example.com") == "full")
    }

    @Test func aServerThatCannotGrantFullIsTooOld() throws {
        let manifest = AppManifest(appName: "Substrate", clientID: "http://127.0.0.1/substrate-console", scopes: ["full"])
        #expect(manifest.requirement.scopeVerbs == ["full"])
        let without = try JSONDecoder().decode(AuthorizationServerMetadata.self,
                                               from: Data(metadataJSON(details: [], verbs: ["read", "write"]).utf8))
        #expect(manifest.requirement.verdict(version: "dev", metadata: without)
            == .tooOld(running: "dev", needed: nil, missing: ["let apps sign in with full access"]))
    }
}

/// The console's sentences (web/console's app-scopes.ts), word for word.
@Suite struct GrantWords {
    @Test func aKindScopeSaysEveryPowerItHolds() {
        #expect(ScopeWords.sentence("write:providers.substrate.reamde.dev/apple/place") == "See, add, change and delete your Apple places")
        #expect(ScopeWords.sentence("read:*") == "See all your records, including kinds added later")
        #expect(ScopeWords.sentence("read:providers.substrate.reamde.dev/apple/*") == "See your Apple records, including kinds added later")
        #expect(ScopeWords.sentence("read:ada.example.com/*") == "See your records from ada.example.com, including kinds added later")
        #expect(ScopeWords.sentence("read:providers.substrate.reamde.dev/applehealth/account") == "See your Apple Health accounts")
        #expect(ScopeWords.sentence("read:samples.substrate.reamde.dev/tasks/task") == "See your tasks")
    }

    @Test func kindsReachedTodayAreNamedByTheirLabels() {
        let kinds = ["contact", "calendar", "reminder", "place", "share"].map {
            ScopeKind(identity: "providers.substrate.reamde.dev/apple/\($0)")
        } + [ScopeKind(identity: "providers.substrate.reamde.dev/apple/syncstate", purpose: "internal")]
        #expect(ScopeWords.sentence("write:providers.substrate.reamde.dev/apple/*", kinds: kinds)
            == "See, add, change and delete your Apple contacts, calendars, reminders, places and shares, and 1 more kind, including kinds added later")
    }

    @Test func anAgentScopeSaysItReadsEveryChat() {
        #expect(ScopeWords.sentence("agents:*")
            == "Talk to all your agents, including agents added later, and through a chat have them do anything they may do; read every chat with them, yours included, answer their questions, and rename or stop any of those chats")
        #expect(ScopeWords.sentence("agents:x.example.com/llm/helper")
            == "Talk to Helper, and through a chat have it do anything it may do; read every chat with it, yours included, answer its questions, and rename or stop any of those chats")
        #expect(ScopeWords.sentence("decide:*")
            == "Apply or dismiss the changes your agents suggest, including agents added later, and edit one before applying it, as you")
        #expect(ScopeWords.sentence("decide:x.example.com/llm/*")
            == "Apply or dismiss the changes the agents in LLM suggest, including agents added later, and edit one before applying it, as you")
    }

    @Test func aWebhookScopeSaysWhatItSends() {
        #expect(syncManifest().scopeWords("webhook:applehealth-webhook")
            == "Send data to your repository through the Apple Health webhook, for any Apple Health account, and see every delivery to it that could not be filed, whoever sent it")
        #expect(ScopeWords.sentence("webhook:whoop-webhook")
            == "Send data to your repository through the whoop-webhook webhook, and see every delivery to it that could not be filed, whoever sent it")
        #expect(Grants(["read:*", "webhook:x"]).words.count == 2)
    }

    /// A glob over a package the app labels names its primary kinds and
    /// counts the rest, as the approval page does from what it reaches.
    @Test func aGlobNamesTheKindsTheAppLabels() {
        var manifest = syncManifest()
        for (name, singular, plural, purpose) in [("contact", "Contact", "Contacts", nil), ("addressbook", "Address book", "Address books", "supporting"),
                                                  ("event", "Event", "Events", nil), ("calendar", "Calendar", "Calendars", "supporting")]
        {
            manifest.kindLabels["providers.substrate.reamde.dev/apple/\(name)"] = KindLabel(singular: singular, plural: plural, purpose: purpose)
        }
        #expect(manifest.scopeWords("write:providers.substrate.reamde.dev/apple/*")
            == "See, add, change and delete your Apple contacts and events, and 2 more kinds, including kinds added later")
        #expect(manifest.scopeWords("read:providers.substrate.reamde.dev/apple/event") == "See your Apple events")
    }
}

/// The console's own sentences, from the fixture its tests generate from
/// app-scopes.ts: a change to either side's words fails the other's suite.
@Suite struct ConsoleSentences {
    struct Entry: Decodable {
        struct Kind: Decodable {
            struct Label: Decodable { let plural: String }
            let identity: String
            let label: Label?
            let purpose: String?
        }

        let scope: String
        let kinds: [Kind]
        let package: String?
        let sentence: String
    }

    static let fixture = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .appendingPathComponent("web/console/src/lib/app-scopes.fixture.json")

    @Test func everySentenceIsTheConsoles() throws {
        let entries = try JSONDecoder().decode([Entry].self, from: Data(contentsOf: Self.fixture))
        #expect(Set(entries.map { ScopeWords.split($0.scope).verb }) == ["read", "write", "agents", "decide", "webhook"])
        // A manifest that names nothing itself, so a webhook's package reads
        // by its id, as the console's packageName reads it.
        let manifest = AppManifest(appName: "App", clientID: "http://localhost/client.json", scopes: [])
        for entry in entries {
            let kinds = entry.kinds.map { ScopeKind(identity: $0.identity, plural: $0.label?.plural, purpose: $0.purpose) }
            let words = kinds.isEmpty ? manifest.scopeWords(entry.scope, package: entry.package)
                : ScopeWords.sentence(entry.scope, kinds: kinds)
            #expect(words == entry.sentence, "\(entry.scope) \(entry.package ?? "")")
        }
    }

    /// Rules the fixture's well-formed scopes do not reach.
    @Test func theNamesReadAsTheConsoleReadsThem() {
        #expect(kindDisplayPlural("x.example.com/llm/agentmemory") == "Agent memories")
        #expect(kindDisplayPlural("x.example.com/x/codeofconduct") == "Codes of conduct")
        #expect(kindDisplayPlural("x.example.com/x/ofnote") == "Of notes")
        #expect(ScopeWords.sentence("read:x.example.com//*") == "See your records from x.example.com, including kinds added later")
        #expect(ScopeWords.agentWords("x.example.com/llm/") == "X.example.com/llm/")
    }
}
