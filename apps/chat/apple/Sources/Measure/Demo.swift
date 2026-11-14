#if DEBUG
import ChatKit
import Foundation
import SubstrateKit

/// A run played from ChatKit's fixtures in the app itself, for a picture or
/// a recording of a state that is hard to hold against a server: a debug
/// build launched with `SUBSTRATE_CHAT_DEMO` set (a release carries none of
/// it). The session is in memory and the folder a scratch one, as a
/// measurement's are.
///
/// The value says how far the run goes: `connecting`, `thinking`, `writing`,
/// `tool`, `answering` and `done` stop there; `reconnecting` drops the
/// stream part way and keeps the next from opening; `play` writes the whole
/// run a line every 0.7 seconds; `long` shows the chat of 200 turns, with
/// nothing running, for a transcript to scroll.
@MainActor
enum Demo {
    static let asked = ProcessInfo.processInfo.environment["SUBSTRATE_CHAT_DEMO"]
    static let scene = "demo"
    static let thread = "t-live"

    private static var server: FixtureServer?
    private static let run = ChatFixtures.run(thread: thread)

    /// The fixture's parts, where the launch asked for a demo.
    static func parts() -> ChatHost.Parts? {
        guard asked != nil else { return nil }
        let server = FixtureServer(reply: "")
        server.script(thread, lines: run.lines, rows: run.rows)
        self.server = server
        return .fixture(server)
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
            host.send(.show(thread: asked == "long" ? ChatFixtures.long : thread, scene: scene))
            if asked == "long" { return }
            guard let key = await until({ host.chats?.scenes[scene]?.current }) else { return }
            _ = await until { host.conversation(key) != nil ? true : nil }
            // Time for a hand to bring the keyboard up before the run.
            let wait = Double(ProcessInfo.processInfo.environment["SUBSTRATE_CHAT_DEMO_WAIT"] ?? "") ?? 1
            try? await Task.sleep(for: .seconds(wait))
            host.send(.draft(run.message), to: key)
            host.send(.send, to: key)
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

    /// A session handed to the build at launch, for a run against a dev
    /// server on a simulator, where no browser can be driven to sign in:
    /// `SUBSTRATE_CHAT_SESSION` holds the server's address, the
    /// repository's name and a token, with spaces between. Kept in memory,
    /// with a scratch folder, so it shares nothing with a signed-in build.
    static func session() -> ChatHost.Parts? {
        let words = (ProcessInfo.processInfo.environment["SUBSTRATE_CHAT_SESSION"] ?? "").split(separator: " ").map(String.init)
        guard words.count == 3, let server = URL(string: words[0]) else { return nil }
        let storage = MemorySessions()
        let session = Session(server: server, repository: words[1], secret: words[2], tokenID: nil)
        if let data = try? JSONEncoder().encode(session) { try? storage.write(data, account: "session") }
        return ChatHost.Parts(
            sessions: SessionStore(storage: storage), transport: URLSessionTransport.shared, folders: ScratchFolders(),
            preferences: MemoryPreferenceStore(), notifications: nil, background: nil)
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
