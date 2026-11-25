#if DEBUG
import Foundation
import SubstrateChatCore
import SubstrateChatUI
import SubstrateKit

/// A run played from SubstrateChatCore's fixtures in the app itself, for a picture or
/// a recording of a state that is hard to hold against a server: a debug
/// build launched with `SUBSTRATE_CHAT_DEMO` set (a release carries none of
/// it). The session is in memory and the folder a scratch one, as a
/// measurement's are.
///
/// The value says how far the run goes: `connecting`, `thinking`, `writing`,
/// `tool`, `answering` and `done` stop there; `reconnecting` drops the
/// stream part way and keeps the next from opening; `play` writes the whole
/// run a line every 0.7 seconds; `long` shows the chat of 200 turns, with
/// nothing running, for a transcript to scroll, and `deep` a chat of 1,100
/// rows, whose older rows are read in above the newest 500 when its top is
/// reached; each read's first reply is a page taller than a window, and
/// `SUBSTRATE_CHAT_DEMO_SLOW` is the seconds a read of older rows takes,
/// for a hand to move the transcript while it is out. `left:<ordinal>` shows
/// the same chat with 2,000 rows on a server that keeps its person's
/// marks, read through the row at that ordinal and not opened since: 1200
/// is found two reads above the newest 500, and 100 is further back than
/// the app reads toward. `reply:<name>` shows a chat
/// that holds one settled reply of the fixtures: `week` (every block), `big`
/// (the same again and again to 16 KB, some forty screens of a phone),
/// `short`, `lines` (code with long lines), `tables`, `table` (a small
/// one), `links` (records and pages linked in one paragraph), or a
/// language's name for its code sample.
///
/// And how a person answers: `question` and `approval` show a card that
/// waits, `words` the same question being answered in the person's own
/// words, `folded` and `approved` each once it ended; `queued` holds a run
/// a message joined while it was written, with more typed in the field,
/// and `taken` the same run once a call read the message; `notify` posts
/// the app's three notifications that take an answer, through the
/// device's own notification service, and `notify-reply`, `notify-question`
/// and `notify-approval` post one of them, for its banner to be pulled open.
///
/// And `helpers` shows a chat whose assistant called on other assistants:
/// one that ran out of time, one that answered, one whose work in the
/// background finished, and, as it stands, two running side by side and
/// one working in the background.
///
/// `new` opens a new chat with a second assistant offered beside the
/// fixture's, for a picture of its title's picker.
@MainActor
enum Demo {
    /// From the environment, or as a launch argument (`-SUBSTRATE_CHAT_DEMO
    /// <state>`), which is what a tool that launches the app on a
    /// simulator can pass.
    static let asked = ProcessInfo.processInfo.environment["SUBSTRATE_CHAT_DEMO"] ?? UserDefaults.standard.string(forKey: "SUBSTRATE_CHAT_DEMO")
    static let scene = "demo"
    static let thread = "t-live"

    private static var server: FixtureServer?
    private static let run = ChatFixtures.run(thread: thread)
    private static let joined = ChatFixtures.queued(thread: thread)
    /// The chat a `reply:` state shows.
    static let settled = "t-reply"

    /// The reply a `reply:<name>` state shows.
    private static var reply: String? {
        guard let asked, asked.hasPrefix("reply:") else { return nil }
        switch String(asked.dropFirst("reply:".count)) {
        case "week": return ChatFixtures.reply
        case "big": return ChatFixtures.reply(bytes: 16 * 1024)
        case "short": return ChatFixtures.short
        case "lines": return ChatFixtures.longLines
        case "tables": return ChatFixtures.tables
        case "table": return ChatFixtures.smallTable
        case "links": return ChatFixtures.links
        case let language: return CodeFixtures.reply([language])
        }
    }

    /// The ordinal a `left:<ordinal>` state says the deep chat's person
    /// read through.
    private static var left: Int? {
        guard let asked, asked.hasPrefix("left:") else { return nil }
        return Int(asked.dropFirst("left:".count))
    }

    /// The states that are a card's: the chat that holds it, and whether
    /// it has ended.
    private static func card(_ state: String) -> (asked: ChatFixtures.Asked, resolved: Bool, words: Bool)? {
        switch state {
        case "question": (ChatFixtures.question(), false, false)
        case "words": (ChatFixtures.question(), false, true)
        case "folded": (ChatFixtures.question(), true, false)
        case "approval": (ChatFixtures.approval(), false, false)
        case "approved": (ChatFixtures.approval(), true, false)
        default: nil
        }
    }

    /// The fixture's parts, where the launch asked for a demo.
    static func parts() -> ChatHost.Parts? {
        guard let asked else { return nil }
        // A message joins a run where the server's `agents` is at 6.
        let queues = asked == "queued" || asked == "taken"
        // And a card says whose hand ended it where it is at 7.
        let server = FixtureServer(reply: "", agents: queues ? 6 : card(asked) != nil ? 7 : 0, records: left != nil ? 8 : 1)
        if let left { server.left(ChatFixtures.deep, rows: 2000, through: left) }
        if queues {
            server.script(thread, lines: joined.lines, rows: joined.rows)
        } else {
            server.script(thread, lines: run.lines, rows: run.rows)
        }
        if let card = card(asked) { server.ask(card.asked, resolved: card.resolved) }
        if let reply { server.seed(settled, message: "Show me.", reply: reply) }
        if asked == "helpers" { server.rows(ChatFixtures.helpersThread, page: ChatFixtures.helpers()) }
        if asked == "new" { server.moreAgents = [ChatFixtures.gardener] }
        let slow = ProcessInfo.processInfo.environment["SUBSTRATE_CHAT_DEMO_SLOW"] ?? UserDefaults.standard.string(forKey: "SUBSTRATE_CHAT_DEMO_SLOW")
        if let seconds = slow.flatMap(Double.init) { server.olderTakes = .milliseconds(Int(seconds * 1000)) }
        self.server = server
        var parts = ChatHost.Parts.fixture(server)
        // The device's own service, for the notifications alone.
        if notified(asked) != nil { parts.notifications = ChatServices.notifications }
        return parts
    }

    /// How many lines of the run are out at each stop.
    private static func lines(_ stop: String) -> Int? {
        switch stop {
        case "connecting": 0
        case "thinking": 4
        case "writing": 6
        case "tool": 7
        case "answering", "reconnecting": 13
        case "done": run.lines.count
        default: nil
        }
    }

    /// Opens the chat, sends the message and writes the run as far as was
    /// asked, once the app's window has claimed its scene.
    static func start(_ host: ChatHost) {
        guard let asked, let server else { return }
        Task {
            guard let scene = await until({ host.chats?.scenes.keys.first }) else { return }
            _ = await until { host.chats?.list.loading == false ? true : nil }
            if let only = notified(asked) { return await notify(only) }
            if asked == "new" { return await newChat(host, scene) }
            let card = card(asked)
            if asked == "deep" || left != nil { return host.send(.show(thread: ChatFixtures.deep, scene: scene)) }
            if asked == "helpers" { return host.send(.show(thread: ChatFixtures.helpersThread, scene: scene)) }
            let shown = asked == "long" ? ChatFixtures.long : reply != nil ? settled : card?.asked.thread ?? thread
            host.send(.show(thread: shown, scene: scene))
            if asked == "long" || reply != nil { return }
            // The conversation of the chat just shown, and not the one the
            // window landed on first: a chat a launch before left open is
            // put back before this one is shown, and what was sent to its
            // key went nowhere.
            let found = await until { () -> ChildKey? in
                guard let key = host.chats?.scenes[scene]?.current, host.conversation(key)?.thread == shown else { return nil }
                return key
            }
            guard let key = found else { return }
            if let card {
                // The card's view reads its record when it appears.
                _ = await until { host.conversation(key)?.cards[card.asked.card.id]?.stands == !card.resolved ? true : nil }
                if card.words {
                    host.send(.answerInWords(card.asked.card.id), to: key)
                    host.send(.fill("Thursday, but after eleven if she can."), to: key)
                }
                return
            }
            if asked == "queued" || asked == "taken" { return await join(host, key, server, taken: asked == "taken") }
            // Time for a hand to bring the keyboard up before the run.
            let wait = Double(ProcessInfo.processInfo.environment["SUBSTRATE_CHAT_DEMO_WAIT"] ?? "") ?? 1
            try? await Task.sleep(for: .seconds(wait))
            await say(run.message, host, key)
            if asked == "play" {
                for _ in run.lines {
                    try? await Task.sleep(for: .milliseconds(700))
                    server.step(thread)
                }
                return
            }
            try? await Task.sleep(for: .milliseconds(600))
            server.step(thread, lines(asked) ?? 0)
            if asked == "reconnecting" {
                try? await Task.sleep(for: .milliseconds(600))
                server.drop(thread)
            }
        }
    }

    /// A new chat on the fixture's assistant, once both are read.
    private static func newChat(_ host: ChatHost, _ scene: String) async {
        _ = await until { (host.chats?.offered.count ?? 0) > 1 ? true : nil }
        host.send(.newChat(agent: ChatFixtures.agent, listen: false, scene: scene))
    }

    /// Sends a message as the composer would, once the chat can send: on a
    /// slow machine its assistant is not read by the time the wait above
    /// is over, and a send before that is refused and draws nothing.
    private static func say(_ message: String, _ host: ChatHost, _ key: ChildKey) async {
        host.send(.draft(message), to: key)
        _ = await until { host.conversation(key)?.composer.canSend == true ? true : nil }
        host.send(.send, to: key)
    }

    /// A run a message joins while it is written: the first message, the
    /// reply part way, the second message sent from the composer as a
    /// person would, and then, for `taken`, the call that reads it.
    private static func join(_ host: ChatHost, _ key: ChildKey, _ server: FixtureServer, taken: Bool) async {
        try? await Task.sleep(for: .milliseconds(600))
        await say(joined.message, host, key)
        try? await Task.sleep(for: .milliseconds(600))
        server.step(thread, joined.joins)
        _ = await until { host.conversation(key)?.composer.canSend == true && host.conversation(key)?.streaming == true ? true : nil }
        host.send(.draft(joined.joining), to: key)
        host.send(.send, to: key)
        try? await Task.sleep(for: .milliseconds(600))
        server.step(thread, joined.queued - joined.joins)
        _ = await until { host.conversation(key)?.composer.queued != nil ? true : nil }
        if taken {
            server.step(thread, joined.lines.count)
        } else {
            // More in the field: Send stands beside Stop.
            host.send(.fill("And the thyme?"), to: key)
        }
    }

    /// The notifications a state posts, by their place in the fixture's:
    /// all three for `notify`, one where it names one.
    private static func notified(_ state: String) -> [Int]? {
        switch state {
        case "notify": [0, 1, 2]
        case "notify-reply": [0]
        case "notify-question": [1]
        case "notify-approval": [2]
        default: nil
        }
    }

    /// The app's notifications that take an answer, posted through the
    /// device's service once the person has allowed them.
    private static func notify(_ which: [Int]) async {
        let service = ChatServices.notifications
        guard await service.requestPermission() != .off else { return }
        let made = ChatFixtures.notifications()
        await service.register(made.categories)
        for (index, request) in made.requests.enumerated() where which.contains(index) {
            await service.schedule(request)
            try? await Task.sleep(for: .seconds(1))
        }
    }

    /// A session handed to the build at launch, for a run against a dev
    /// server on a simulator, where no browser can be driven to sign in:
    /// `SUBSTRATE_CHAT_SESSION` holds the server's address, the
    /// repository's name and a token, with spaces between. Kept in memory,
    /// with a scratch folder, so it shares nothing with a signed-in build.
    /// It notifies through the device's own service where
    /// `SUBSTRATE_CHAT_SESSION_NOTIFY` is set, for an answer given from a
    /// notification to reach that server.
    static func session() -> ChatHost.Parts? {
        let words = (ProcessInfo.processInfo.environment["SUBSTRATE_CHAT_SESSION"] ?? "").split(separator: " ").map(String.init)
        guard words.count == 3, let server = URL(string: words[0]) else { return nil }
        let storage = MemorySessions()
        let session = Session(server: server, repository: words[1], secret: words[2], tokenID: nil)
        if let data = try? JSONEncoder().encode(session) { try? storage.write(data, account: "session") }
        return ChatHost.Parts(
            sessions: SessionStore(storage: storage), transport: URLSessionTransport.shared, folders: ScratchFolders(),
            preferences: MemoryPreferenceStore(),
            notifications: ProcessInfo.processInfo.environment["SUBSTRATE_CHAT_SESSION_NOTIFY"] == nil ? nil : ChatServices.notifications,
            background: nil)
    }

    private static func until<T>(_ read: () -> T?) async -> T? {
        for _ in 0 ..< 600 {
            if let value = read() { return value }
            try? await Task.sleep(for: .milliseconds(50))
        }
        return nil
    }
}
#endif
