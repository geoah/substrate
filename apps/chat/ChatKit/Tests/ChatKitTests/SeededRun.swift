import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateRuntime
import SubstrateTesting

@testable import ChatKit

/// One seed's run of the app against a server that misbehaves by it: a
/// person's steps chosen from the seed (sends, stops, Try Again and
/// Continue, Send Again and Edit Message, opening and closing chats,
/// leaving and coming back to the front, kills and relaunches, scrolling to
/// a chat's end and away from it, Mark as Unread and Mark as Read), the
/// server's turns moved, another device of theirs moving a mark, and the
/// clock advanced between them, and after every step the rules a chat must
/// keep whatever the server does.
///
/// The seed fixes everything: the steps, the faults, the clock, and the
/// order the app's concurrent work runs in, because a run is made on a
/// thread of its own (`SeededThread`) that takes each job in the order it
/// was enqueued. Between steps the run waits for that thread to have
/// nothing left to run, which is exact and takes no time on any clock, so
/// a seed replays the same way whatever else the host is doing.
struct SeededRun {
    /// What went wrong, with what replays it.
    struct Violation: Error, CustomStringConvertible {
        var seed: UInt64
        var rule: String
        var what: String
        var steps: [String]
        var revision: Int
        var records: Int
        var rate: Double

        var description: String {
            let listed = steps.enumerated().map { "  \($0.offset + 1). \($0.element)" }.joined(separator: "\n")
            return """
                seed \(seed) broke a rule at step \(steps.count): \(rule)
                \(what)
                server: agents revision \(revision), records revision \(records), \(Int(rate * 100))% of requests faulted
                steps:
                \(listed)
                replay: CHATKIT_SEED=\(seed) CHATKIT_STEPS=\(steps.count) swift test --filter SeededChat
                """
        }
    }

    /// The rules, by the name a failure prints and a known failure is
    /// listed under.
    enum Rule: String, CaseIterable {
        case fold = "the transcript drawn equals the fold of the server's rows"
        case streaming = "no conversation is left waiting with no stream open"
        case doubled = "no message is written twice"
        case drawnTwice = "no row is drawn twice"
        case lost = "nothing the person sent is lost"
        case places = "no failure is drawn in two places"
        case pending = "a pending message has its card or its send"
        case resent = "without an id nothing is sent again on the app's own"
        case list = "the list holds each chat once"
        case settles = "every conversation settles once the server behaves"
        /// Read state (`ChatAppMarks.swift`), where the server keeps the
        /// person's marks. A dot that went for a message, with the server
        /// holding it read, is drawn again only once somebody marked the
        /// chat unread.
        case dot = "a dot never comes back for a message already read unless Mark as Unread was set"
        /// A read is written only of a message whose chat stood open at
        /// its end, in front, for a second, or one Mark as Read was
        /// pressed on; Mark as Unread only where it was pressed; and
        /// nothing at all to a server that keeps no marks.
        case sight = "a mark is never written for a chat whose end was not on screen in front"
        case badge = "the badge equals the number of unread chats by the rule"
        case notified = "no notification stands for a reply a mark already reads through"
        /// The run's own: work that left its thread is ordered by the
        /// scheduler again, and the seed no longer says what happens. Held
        /// where the run can look (`SeededServer.watch`): a job that left
        /// the thread and passed none of those places is not seen.
        case strayed = "the run is on its own thread wherever it is met"
        /// Broken today, in code another group is rebuilding (`known`).
        case overlay = "no run's overlay is left drawn once the run is over"
        /// Broken today, in the same code.
        case kept = "an overlay kept for a failed reload is not drawn beside rows that hold its turns"
        /// Broken today, in the same code.
        case carried = "an overlay kept for a failed reload is not carried into the next send"
        /// Broken today, in the same code.
        case attached = "an attach does not draw again a message its rows hold under its id"
    }

    /// Rules the app is known to break today, each in code another group
    /// is rebuilding, with what breaks it. A run notes one where it meets
    /// it, sets that conversation aside until it is whole again, and goes
    /// on, so the rest of the seed is still held to every other rule.
    static let known: [Rule: String] = [
        .overlay: """
        A 401 on an attach (or on the read before it) ends the run through `stopRunning`, which leaves the overlay's turns in \
        place with the conversation idle and its thread handed back: the next read of the rows draws the partial reply under \
        the stored one. For the group that makes the two models of a turn one (A4).
        """,
        .kept: """
        After a handover's read fails the overlay is kept as the only view of that run. A send from there that ends with no \
        answer is read for by the delivery check (a server without message ids), and what the check finds is published as \
        the thread's rows while the kept overlay still stands: the run is drawn twice until Try Again on the reload. Also \
        the two models of a turn (A4).
        """,
        .carried: """
        The same kept overlay, once Try Again on the reload has read the rows: its turns stay in the overlay, and the next \
        send streams under them, so the turns the rows now hold are drawn a second time above the new message until that \
        run hands over. Found by seed 1048 at 90 steps (agents 3, records 8), where t6 drew "say 4." and "say 5." twice. \
        Also the two models of a turn (A4).
        """,
        .attached: """
        A first message sent under its id, the app left and brought back to the front while its turn runs: the conversation \
        attaches with the message still in its overlay, and the list reads the thread's rows beside it (`read(thread:)` right \
        after the attach is asked for), so the message is drawn twice under one id, as a bubble and as a stored row, until \
        the run hands over. Found by seed 9059 at 16 steps (agents 3, records 8); no mark moved in that run. The rows a \
        conversation owns while it runs, and the two models of a turn (A4).
        """,
    ]

    let seed: UInt64
    let scene = "s"
    let clock: TestClock
    let ids: SequentialIDs
    let base: URL
    let server: SeededServer
    let careless: Bool
    let rate: Double
    let with401: Bool
    private let thread: SeededThread
    private var numbers: SeededNumbers
    private var rig: ChatRig
    private(set) var steps: [String] = []
    private var typed = 0
    /// Every text the person pressed Send on and did not take back: how
    /// often they pressed, whether an id ever went with it, and how many
    /// requests the server had seen for it before the first press.
    private var sent: [String: (presses: Int, ids: Bool, before: Int)] = [:]
    /// Every text ever sent, taken back or not, with how many messages the
    /// person meant by it: one, and one more each time they took it back
    /// with Edit Message, after which sending the same words is their own
    /// new message.
    private var ever: [String: Int] = [:]
    /// Texts the app handed back without knowing whether they arrived: the
    /// person looks before sending one again.
    private var suspect: Set<String> = []
    private var inFront = true
    private var signedOut = false
    /// A conversation nothing was seen to move: as it stood, since when on
    /// the clock, and at which step.
    private var stuck: [ChildKey: (mark: String, since: Date, step: Int)] = [:]
    /// The waits on the clock each conversation asked for, and the list.
    private var waits: [ChildKey: [Wait]] = [:]
    private var listWaits: [Wait] = []
    private var scanned = 0
    /// The sent texts a composer holds now, and when the person last typed
    /// each without sending it: what a kill may and may not take.
    private var composed: Set<String> = []
    private var typedAt: [String: Date] = [:]
    /// The conversation whose view is on screen, and whether its end is,
    /// as that view last said.
    private var viewed: ChildKey?
    private var atEnd = false
    /// The end the person is looking at, in front: the thread, the newest
    /// message the app holds of it, and since when on the clock.
    private var looking: (thread: String, message: String, since: Date)?
    /// The messages of each thread whose end stood so for a second: what a
    /// read by sight may name. A person's, so a kill takes none.
    private var ripe: [String: Set<String>] = [:]
    /// What Mark as Read was pressed on: each thread, with the newest
    /// message its row named then ("" where it named none), and where Mark
    /// as Unread was.
    private var pressedRead: [String: Set<String>] = [:]
    private var pressedUnread: Set<String> = []
    /// The writes of marks judged so far.
    private var judged = 0
    /// Each chat drawn with no dot while the server held it read, with the
    /// message it was read through: where a dot must not come back.
    private var cleared: [String: String] = [:]
    /// The first rule the run broke that it is not known to break.
    private(set) var broken: Violation?
    /// The known rules this run broke, each once, with where.
    private(set) var noted: [Rule: Violation] = [:]

    /// - Parameters:
    ///   - revision: the server's `agents` revision; nil draws it from the
    ///     seed, mostly the one that takes a message's id.
    ///   - careless: the person sends a text the app handed back without
    ///     looking for it first, which on a server without ids can send a
    ///     message twice: the run that shows the rules catch it.
    private init(seed: UInt64, revision: Int?, careless: Bool, on thread: SeededThread) {
        self.seed = seed
        self.thread = thread
        self.careless = careless
        var numbers = SeededNumbers(seed: seed)
        let drawn = numbers.below(5) == 0 ? 2 : 3
        rate = numbers.pick([0.08, 0.2, 0.35, 0.5])
        with401 = numbers.below(3) == 0
        let clock = TestClock()
        let ids = SequentialIDs()
        let base = temporaryFolder("seeded")
        // Of its own stream, so what the seed drew before marks it draws
        // still: mostly a server that keeps them.
        var marks = SeededNumbers(seed: seed ^ SeededNumbers.hash("marks"))
        let server = SeededServer(seed: seed, clock: clock, revision: revision ?? drawn, records: marks.below(6) == 0 ? 7 : 8)
        self.clock = clock
        self.ids = ids
        self.base = base
        self.server = server
        rig = ChatRig(
            platform: .ios, base: base, transport: server.connect(), clock: clock, ids: WatchedIDs(ids: ids, watch: server.watch),
            watch: server.watch)
        self.numbers = numbers
    }

    private var link: LinkSnapshot {
        LinkSnapshot(phase: signedOut ? .accessEnded : .signedIn, server: origin, repository: repository, scopes: nil, online: true)
    }

    // MARK: The run

    /// One seed's run, made and run on a thread of its own: `count` steps,
    /// and then everything is let settle. The first rule broken ends it,
    /// and is kept in `broken`.
    static func ran(seed: UInt64, revision: Int? = nil, careless: Bool = false, steps count: Int) async throws -> SeededRun {
        try await SeededThread.run {
            // Made here and not before: the rig's first tasks are the
            // thread's too.
            var run = SeededRun(seed: seed, revision: revision, careless: careless, on: SeededThread.current!)
            run.broken = await run.ran(steps: count)
            return run
        }
    }

    /// What the run came to, as one line that differs where two runs of a
    /// seed did: its steps, what the server was asked and what it holds.
    var fingerprint: String {
        var hash = SeededNumbers.hash(steps.joined(separator: "\n"))
        for thread in server.threadIDs {
            hash =
                hash &* 1_099_511_628_211
                ^ SeededNumbers.hash(thread + server.rows(of: thread).map { "\($0.id)=\($0.role)=\($0.content)" }.joined(separator: "|"))
        }
        for (name, count) in server.tally.sorted(by: { $0.key < $1.key }) {
            hash = hash &* 1_099_511_628_211 ^ SeededNumbers.hash("\(name)=\(count)")
        }
        hash = hash &* 1_099_511_628_211 ^ UInt64(server.requests) &* 31 ^ UInt64(server.faulted)
        return String(hash, radix: 16)
    }

    private mutating func ran(steps count: Int) async -> Violation? {
        await rig.runtime.linkChanged(link)
        await rig.harness.send(.sceneAppeared(scene: scene, restoring: nil))
        // Signed in against a server that behaves, as a person is before
        // anything goes wrong.
        for _ in 0 ..< 200 {
            await drain()
            let state = await rig.harness.state
            if state.loaded, !state.refreshing, state.agentsRevision != nil { break }
        }
        server.misbehave(rate, with401: with401)
        for _ in 0 ..< count {
            forgetTrace()
            await step()
            await drain()
            if server.took401() {
                // What the kit's link does with a refused token.
                signedOut = true
                await rig.runtime.linkChanged(link)
                await drain()
            }
            await appears()
            if let broken = await confirmed(settled: false) { return broken }
        }
        steps.append("the server behaves, and everything is given time to settle")
        return await settle()
    }

    /// What the run did, by name: the server's tally and the person's.
    var tally: [String: Int] {
        var out = server.tally
        for step in steps {
            let name: String? =
                step.hasPrefix("sends ")
                ? "person: sends"
                : step.hasPrefix("presses Send Again")
                    ? "person: Send Again on an unsent message"
                    : step.hasPrefix("takes ")
                        ? "person: Edit Message"
                        : step.hasPrefix("the app is killed")
                            ? "person: kill and relaunch"
                            : step.hasPrefix("presses Try Again or Continue")
                                ? "person: Try Again or Continue"
                                : step.hasPrefix("presses Stop")
                                    ? "person: Stop"
                                    : step.hasPrefix("comes back")
                                        ? "person: back to the front"
                                        : step.hasPrefix("finds ")
                                            ? "person: finds a text in the chat and does not resend"
                                            : nil
            if let name { out[name, default: 0] += 1 }
            let marks = [
                "scrolls to the end": "person: scrolls to a chat's end", "scrolls up": "person: scrolls up from a chat's end",
                "marks chat": "person: Mark as Unread or Mark as Read", "another device": "another device moves a mark",
            ]
            for (prefix, name) in marks where step.hasPrefix(prefix) { out[name, default: 0] += 1 }
        }
        return out
    }

    /// What a run did, in a line: how much of the app a seed reached.
    var summary: String {
        let rows = server.threadIDs.reduce(0) { $0 + server.rows(of: $1).count }
        return "seed \(seed): \(steps.count) steps, revision \(server.revision), \(Int(rate * 100))% faulted, \(ever.count) messages sent, "
            + "\(server.threadIDs.count) threads and \(rows) rows at the server, \(server.faulted) of \(server.requests) requests faulted, "
            + "ran as \(fingerprint)"
    }

    /// Waits for the app to stand still: the run's thread has no job left,
    /// so what still runs waits on the server or on the clock, which only
    /// the next step moves. No time passes on any clock.
    private mutating func drain() async {
        await thread.idle()
        collect()
    }

    private func shown() async -> (key: ChildKey, screen: ConversationScreen, live: ConversationState)? {
        guard let key = await rig.harness.state.scenes[scene]?.current, let screen = await rig.conversation(key), let live = await rig.live(key)
        else { return nil }
        return (key, screen, live)
    }

    private func tell(_ command: Conversation.Command, _ key: ChildKey) async {
        try? await rig.runtime.send(command, to: [key])
    }

    // MARK: Steps

    private mutating func step() async {
        var choices: [(weight: Int, name: String)] = [(12, "time"), (16, "work"), (3, "kill")]
        // The person's other device, whatever this one is doing.
        if server.keepsMarks, !server.threadIDs.isEmpty { choices += [(4, "elsewhereRead"), (1, "elsewhereUnread")] }
        let current = await shown()
        if signedOut {
            choices.append((10, "signIn"))
        } else if !inFront {
            choices.append((12, "return"))
        } else {
            // A person with no chat open opens one; with one open, mostly
            // talks in it.
            let idle = current == nil
            choices += [(idle ? 24 : 4, "newChat"), (2, "refresh"), (4, "leave")]
            if !(await rig.harness.state.chats.isEmpty) { choices += [(idle ? 16 : 5, "show"), (2, "markUnread"), (2, "markRead")] }
            if let current {
                choices += [(2, "back"), (8, "scroll")]
                if current.screen.composer.canSend { choices.append((30, "send")) }
                if current.screen.streaming { choices.append((5, "stop")) }
                if !buttons(current.screen).isEmpty { choices.append((14, "button")) }
            }
        }
        var draw = numbers.below(choices.reduce(0) { $0 + $1.weight })
        let choice = choices.first { choice in
            draw -= choice.weight
            return draw < 0
        }!.name
        switch choice {
        case "time":
            let seconds = numbers.pick([0.05, 0.3, 0.7, 1, 2, 3, 5, 9, 16, 40, 130])
            steps.append("the clock moves \(seconds) s")
            clock.advance(by: seconds)
        case "work":
            let events = 1 + numbers.below(4)
            steps.append("the server's turns emit \(events) more")
            server.work(events: events)
        case "kill":
            await kill()
        case "signIn":
            steps.append("signs in again")
            signedOut = false
            await rig.runtime.linkChanged(link)
        case "return":
            steps.append("comes back to the front")
            inFront = true
            await rig.harness.send(.phase(.active))
            await says(atEnd)
        case "leave":
            steps.append("leaves the app")
            inFront = false
            // The chat's view says its end is no longer looked at, before
            // or after the app hears it left the front: either order.
            let first = numbers.chance(0.5)
            if first { await says(false) }
            await rig.harness.send(.phase(.inactive))
            if !first { await says(false) }
            await rig.harness.send(.phase(.background))
        case "scroll":
            atEnd.toggle()
            steps.append(atEnd ? "scrolls to the end of the chat" : "scrolls up from the end of the chat")
            await says(atEnd)
        case "markUnread":
            let thread = numbers.pick(await rig.harness.state.chats.map(\.id))
            steps.append("marks chat \(thread) as unread")
            pressedUnread.insert(thread)
            cleared[thread] = nil
            await rig.harness.send(.markUnread(thread))
        case "markRead":
            let state = await rig.harness.state
            let thread = numbers.pick(state.chats.map(\.id))
            steps.append("marks chat \(thread) as read")
            pressedRead[thread, default: []].insert(state.thread(thread)?.lastMessage ?? "")
            await rig.harness.send(.markRead(thread))
        case "elsewhereRead":
            let thread = numbers.pick(server.threadIDs)
            steps.append("another device reads chat \(thread) to its end")
            server.elsewhere(thread, unread: false)
        case "elsewhereUnread":
            let thread = numbers.pick(server.threadIDs)
            steps.append("another device marks chat \(thread) as unread")
            server.elsewhere(thread, unread: true)
        case "newChat":
            steps.append("opens a new chat")
            await rig.harness.send(.newChat(agent: helper, listen: false, scene: scene))
        case "show":
            let thread = numbers.pick(await rig.harness.state.chats.map(\.id))
            steps.append("opens chat \(thread)")
            await rig.harness.send(.show(thread: thread, scene: scene))
        case "back":
            steps.append("goes back to the list")
            await rig.harness.send(.back(scene: scene))
        case "refresh":
            steps.append("pulls to refresh")
            await rig.harness.send(.refresh)
        case "stop":
            steps.append("presses Stop")
            if let current { await tell(.stop, current.key) }
        case "send":
            if let current { await send(current.key, current.screen) }
        default:
            if let current { await press(numbers.pick(buttons(current.screen)), current.key, current.screen) }
        }
    }

    /// The view of the open chat says whether its end is on screen to
    /// somebody looking at it.
    private func says(_ shown: Bool) async {
        if let viewed { await rig.harness.send(.end(scene: scene, key: viewed, shown: shown)) }
    }

    /// The view of a chat that came on screen says at once whether its end
    /// is in view, as the app's does: a chat opens at its end, or at its
    /// "New" line where that is further up. One that left the screen says
    /// nothing, and the list knows by what the window shows.
    private mutating func appears() async {
        let key = signedOut ? nil : await rig.harness.state.scenes[scene]?.current
        guard key != viewed else { return }
        viewed = key
        atEnd = key != nil && numbers.chance(0.7)
        if inFront { await says(atEnd) }
        await drain()
    }

    /// The next message's text: no other message's is a part of it, so a
    /// text is found by looking for it.
    private mutating func word() -> String {
        typed += 1
        return "say \(typed)."
    }

    /// Send, from the composer: a new message, or the text the app handed
    /// back. Where the app could not say whether that text arrived, the
    /// person looks first, as its strip asks, and clears the box when the
    /// chat holds it.
    private mutating func send(_ key: ChildKey, _ screen: ConversationScreen) async {
        // The run plays the composer's field, which owns its text: what it
        // holds is what the conversation was last told, not the tree's copy.
        var text = (await rig.live(key)?.draft ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if text.isEmpty {
            text = word()
            await tell(.draft(text), key)
        } else if !careless, suspect.contains(text), server.userRows(saying: text) > 0 {
            // And remembers it: a kill before the cleared box is saved
            // brings the text back, and the person knows it was sent.
            steps.append("finds \"\(text)\" in the chat and clears the box")
            await tell(.draft(""), key)
            await tell(.dismissFailure, key)
            return
        }
        steps.append("sends \"\(text)\"")
        let ids = await rig.harness.state.sendsIDs
        let held = sent[text]
        sent[text] = ((held?.presses ?? 0) + 1, ids || held?.ids == true, held?.before ?? server.chatRequests(saying: text))
        typedAt[text] = nil
        ever[text, default: 1] += 0
        await tell(.send, key)
    }

    /// Every button on the screen that can be pressed now.
    private func buttons(_ screen: ConversationScreen) -> [FailureAction] {
        var out: [FailureAction] = []
        for failure in Array(screen.failures.turns.values) + [screen.failures.strip].compactMap({ $0 }) {
            if let button = failure.button, button.enabled(at: clock.now) { out.append(button.action) }
            if let dismiss = failure.dismiss { out.append(dismiss) }
        }
        // In a fixed order: a dictionary's is not the seed's.
        return out.sorted { "\($0)" < "\($1)" }
    }

    private mutating func press(_ action: FailureAction, _ key: ChildKey, _ screen: ConversationScreen) async {
        switch action {
        case .resume:
            steps.append("presses Try Again or Continue")
            await tell(.continueTurn, key)
        case .send:
            await send(key, screen)
        case .sendAgain:
            steps.append("presses Send Again on the unsent message")
            if let text = await rig.live(key)?.pending?.message { sent[text]?.presses += 1 }
            await tell(.sendAgain, key)
        case .edit:
            // Edit Message, then other words: what goes from the composer
            // now is a new message, by the person's own hand.
            guard let text = await rig.live(key)?.pending?.message else { return }
            let other = word()
            steps.append("takes \"\(text)\" back and rewrites it as \"\(other)\"")
            sent[text] = nil
            ever[text, default: 1] += 1
            await tell(.takeBack, key)
            // Only its own words are rewritten: what else the composer
            // held stays.
            let draft = await rig.live(key)?.draft ?? text
            await tell(.draft(draft.replacingOccurrences(of: text, with: other)), key)
            typedAt[other] = clock.now
        case .stop:
            steps.append("presses Try Again on a stop")
            await tell(.stop, key)
        case .reload:
            steps.append("presses Try Again on a reload")
            await tell(.retryReload, key)
        case .reopen:
            steps.append("presses Try Again on a stream")
            await tell(.reopen, key)
        case .dismiss:
            steps.append("dismisses the strip")
            await tell(.dismissFailure, key)
        case let .reloadRows(thread):
            steps.append("presses Try Again on a chat's rows")
            await rig.harness.send(.reloadRows(thread: thread))
        case .refresh:
            steps.append("presses Try Again on the list")
            await rig.harness.send(.refresh)
        case let .newChat(agent):
            steps.append("presses New Chat on a card")
            await rig.harness.send(.newChat(agent: agent, listen: false, scene: scene))
        case .dismissRename:
            await rig.harness.send(.dismissRename)
        case .rename, .addKey, .open:
            steps.append("presses a button that leaves the chat")
        }
    }

    /// The process dies and the app is launched again over the same folder.
    /// What a kill may take: a draft younger than its coalesced save, and a
    /// send without an id still on its way, as before ids. What it may not:
    /// a message sent under an id.
    private mutating func kill() async {
        steps.append("the app is killed and launched again")
        // The kill comes with the app standing still, so the writes it
        // makes at once have been made: a message it has just been handed,
        // or has just put back into a composer, is in the folder, and a
        // sent text is no draft there any more. A process cannot be held
        // to what it had no time for; what it had time for and is missing
        // is lost, and the next check says so.
        await drain()
        let state = await rig.harness.state
        let restoring = state.scenes[scene]?.current.flatMap { state.conversations[$0]?.thread }
        var pending: Set<String> = []
        for key in state.conversations.keys {
            if let message = await rig.live(key)?.pending?.message { pending.insert(message) }
        }
        await rig.runtime.close()
        let kept = Self.onDisk(rig)
        for text in Array(sent.keys) where server.userRows(saying: text) == 0 && !kept.contains(where: { $0.contains(text) }) {
            // What the folder must hold, and so stays tracked for the next
            // check to miss: a pending message, and a text the app put
            // back into a composer, which is written at once. What a kill
            // may take, as before ids: a draft the person typed less than
            // its save's pause ago, and a send without an id still on its
            // way.
            let young = typedAt[text].map { clock.now.timeIntervalSince($0) < 1 } ?? false
            let owed = pending.contains(text) || (composed.contains(text) && !young)
            if !owed { sent[text] = nil }
        }
        waits = [:]
        listWaits = []
        scanned = 0
        stuck = [:]
        composed = []
        // The view is gone with the process, and what the launch draws
        // first is the folder's: a dot it kept may be older than the read.
        viewed = nil
        atEnd = false
        looking = nil
        cleared = [:]
        rig = ChatRig(
            platform: .ios, base: base, transport: server.connect(), clock: clock, ids: WatchedIDs(ids: ids, watch: server.watch),
            watch: server.watch)
        inFront = true
        signedOut = false
        await rig.runtime.linkChanged(link)
        await rig.harness.send(.sceneAppeared(scene: scene, restoring: restoring))
    }

    /// Every draft the folder holds.
    private static func drafts(_ rig: ChatRig) -> [String] {
        let read = (try? Data(contentsOf: rig.root.appendingPathComponent("drafts.json"))).flatMap {
            try? JSONDecoder().decode([String: String].self, from: $0)
        }
        return Array((read ?? [:]).values)
    }

    /// Every text the folder holds as a draft or as an unsent message.
    private static func onDisk(_ rig: ChatRig) -> [String] {
        let unsent = (try? Data(contentsOf: rig.root.appendingPathComponent("unsent.json"))).flatMap {
            try? JSONDecoder().decode([String: UnsentMessage].self, from: $0)
        }
        return drafts(rig) + (unsent ?? [:]).values.map(\.message)
    }

    // MARK: The rules

    private func broke(_ rule: Rule, _ what: String) -> Violation {
        Violation(seed: seed, rule: rule.rawValue, what: what, steps: steps, revision: server.revision, records: server.records, rate: rate)
    }

    /// The transcript as a screen draws it: who said what, in order.
    private static func said(_ screen: ConversationScreen) -> [(id: String, role: String, text: String)] {
        screen.turns.compactMap { turn in
            switch turn.body {
            case let .user(user): (turn.id, "user", user.text)
            case let .assistant(assistant): (turn.id, "assistant", assistant.text ?? "")
            default: nil
            }
        }
    }

    /// What a conversation waits on, in words.
    private static func waiting(_ live: ConversationState) -> String {
        var parts = [live.phase.rawValue]
        if live.finding { parts.append("reading for its message by id") }
        if live.checking { parts.append("checking a delivery") }
        switch live.run {
        case let .send(run)?:
            parts.append(run.resumes ? "a continue" : run.named ? "a send the server took" : run.unkept ? "a send not yet kept" : "a send not yet answered")
        case .attach?: parts.append("an attach")
        case nil: break
        }
        return parts.joined(separator: ", ")
    }

    /// Whether something still moves a conversation that waits: a request
    /// at the server about it, or a wait on the clock.
    private func moves(_ live: ConversationState, _ key: ChildKey) -> Bool {
        var tags = [live.thread].compactMap { $0 }
        if let pending = live.pending { tags += ["new:\(pending.message)", "message:\(pending.id)"] }
        if case let .send(run)? = live.run { tags.append("new:\(run.message)") }
        if let lost = live.lost { tags.append("new:\(lost.message)") }
        if tags.contains(where: { server.exchanges(about: $0) > 0 }) { return true }
        // A delivery check for a new chat reads other threads to find its
        // own, so any request still at the server may be its.
        if live.checking, live.thread == nil, !server.openExchanges.isEmpty { return true }
        // A wait on the clock counts only where it is this conversation's
        // own (it asked for one that ends then), or nobody's that is known:
        // the client's wait before it asks a busy server again, inside a
        // request that could be anyone's. Another conversation's retry, its
        // poll, the list's saves and a request hanging at the server about
        // something else do not keep this one alive. Waits are told apart
        // by when they end, and counted, since two often end together.
        func same(_ a: Date, _ b: Date) -> Bool { abs(a.timeIntervalSince(b)) < 0.000_001 }
        let sleepers = clock.deadlines
        if (waits[key] ?? []).contains(where: { wait in sleepers.contains { same($0, wait.end) } }) { return true }
        let known = waits.values.flatMap { $0 }.map(\.end) + listWaits.map(\.end) + server.sleepDeadlines
        var seen: [Date] = []
        for deadline in sleepers where !seen.contains(where: { same($0, deadline) }) {
            seen.append(deadline)
            if sleepers.filter({ same($0, deadline) }).count > known.filter({ same($0, deadline) }).count { return true }
        }
        return false
    }

    /// A wait on the clock an effect asked for: when it ends, and the key
    /// its effect runs under, which the next effect under it replaces.
    private struct Wait {
        var end: Date
        var key: String
        /// A poll's timer: armed again for this long each time it ends.
        var every: Double?
    }

    /// Reads the waits the app asked for since the last time out of the
    /// trace: each conversation's own, and the list's. The clock moves
    /// only between steps, so a wait asked for in this one began now. A
    /// wait is taken away as the runtime takes its effect away: when
    /// another starts under its key, when its key is cancelled, and when
    /// it ends.
    private mutating func collect() {
        let trace = rig.harness.trace
        let now = clock.now
        // A poll's timer that ended in this step was armed again from now.
        for key in Array(waits.keys) {
            waits[key] = waits[key]?.compactMap { wait in
                guard wait.end <= now else { return wait }
                return wait.every.map { Wait(end: now.addingTimeInterval($0), key: wait.key, every: $0) }
            }
        }
        listWaits.removeAll { $0.end <= now }
        for entry in trace.dropFirst(scanned) {
            guard case let .effect(path, effect) = entry else { continue }
            if let key = path.last, let ask = (effect as? Conversation.Effect)?.ask {
                var wait: Wait?
                var under: String?
                switch ask {
                case let .wait(_, seconds): wait = Wait(end: now.addingTimeInterval(seconds), key: "retry")
                case let .repeatWait(_, seconds):
                    under = "repeat"
                    if seconds > 0 { wait = Wait(end: now.addingTimeInterval(seconds), key: "repeat") }
                case .handover: wait = Wait(end: now.addingTimeInterval(15), key: "run")
                case .chat, .resume, .attach: under = "run"
                case .check: wait = Wait(end: now.addingTimeInterval(1), key: "check")
                case .stopWait: wait = Wait(end: now.addingTimeInterval(1), key: "stopWait")
                case .pollEvery: wait = Wait(end: now.addingTimeInterval(5), key: "pollTimer", every: 5)
                case let .countdown(date):
                    under = "countdown"
                    if date > now { wait = Wait(end: date, key: "countdown") }
                case let .cancel(cancelled): under = cancelled
                case .find, .poll, .stop, .advance: break
                }
                if let replaced = wait?.key ?? under { waits[key]?.removeAll { $0.key == replaced } }
                if let wait { waits[key, default: []].append(wait) }
            } else if path.isEmpty, let ask = (effect as? ChatApp.Effect)?.ask {
                var wait: Wait?
                switch ask {
                case .saveState: wait = Wait(end: now.addingTimeInterval(0.4), key: "state")
                case .saveDrafts: wait = Wait(end: now.addingTimeInterval(0.6), key: "drafts")
                case let .feedWait(seconds): wait = Wait(end: now.addingTimeInterval(seconds), key: "feedWait")
                case let .sight(thread, _): wait = Wait(end: now.addingTimeInterval(ChatApp.sightSeconds), key: "sight:\(thread)")
                case let .marksWait(until): wait = Wait(end: until, key: "marksWait")
                case let .cancel(cancelled): listWaits.removeAll { $0.key == cancelled }
                default: break
                }
                if let wait {
                    listWaits.removeAll { $0.key == wait.key }
                    listWaits.append(wait)
                }
            }
        }
        scanned = trace.count
    }

    /// Forgets the trace before a step: what the step asks for is read
    /// from what follows.
    private mutating func forgetTrace() {
        collect()
        if ProcessInfo.processInfo.environment["CHATKIT_TRACE"] == nil {
            rig.harness.clearTrace()
            scanned = 0
        }
    }

    /// Every rule, on what the app holds and draws now. `settled` adds
    /// what must be so once the server has behaved for long enough.
    private mutating func check(settled: Bool) async -> Violation? {
        server.watch("a step of the run")
        if let first = server.strayed.first {
            return broke(
                .strayed,
                "\(server.strayed.count) times the run was met off its thread, first at \(first): something in it waits on real time, a "
                    + "dispatch queue or the main actor, or the runtime did not hand the thread a job enqueued from it")
        }
        let state = await rig.harness.state
        let presentation = await rig.harness.presentation
        // A wait's effect ends with its conversation, and a handover's
        // with the read it waits beside.
        for key in Array(waits.keys) {
            let phase = await rig.live(key)?.phase
            if phase == nil { waits[key] = nil } else if phase != .handingOver { waits[key]?.removeAll { $0.key == "run" } }
            if phase != .background { waits[key]?.removeAll { $0.key == "pollTimer" } }
        }
        var homes: Set<String> = []
        var inComposer: Set<String> = []
        func home(_ text: String) { for known in sent.keys where text.contains(known) { homes.insert(known) } }
        state.drafts.values.forEach(home)
        state.unsent.values.forEach { home($0.message) }

        for key in state.conversations.keys.sorted() {
            guard let live = await rig.live(key), let screen = presentation.conversations[key] else { continue }
            // What the app handed back saying it may have arrived: the
            // person reads that, and looks before sending it again.
            if live.chatFailure?.unconfirmed == true {
                for text in sent.keys where live.draft.contains(text) { suspect.insert(text) }
            }
            let said = Self.said(screen)
            let name = live.thread ?? "a new chat"
            home(live.draft)
            for text in sent.keys where live.draft.contains(text) { inComposer.insert(text) }
            said.filter { $0.role == "user" }.forEach { home($0.text) }
            if let pending = live.pending { home(pending.message) }
            if let lost = live.lost { home(lost.message) }

            // Known: a run that is over left its overlay behind, or kept it
            // for a reload and then let the rows that hold the same turns
            // be drawn above it. Noted once each, and this conversation is
            // not held to what the leftover breaks.
            // Set aside: only what a leftover overlay breaks, the drawing
            // of its turns twice and beside the rows. Every other rule
            // still holds this conversation, the last one included.
            var leftover = false
            if live.phase == .idle, !live.ownsTranscript, !live.overlay.turns.isEmpty {
                if noted[.overlay] == nil {
                    noted[.overlay] = broke(.overlay, "\(name) is idle and still draws \(live.overlay.turns.map(\.content)) from a run that ended")
                }
                leftover = true
            }
            var keeps = live.reloadFailed != nil || live.reloading != nil
            if case let .send(run)? = live.run, !run.kept.isEmpty { keeps = true }
            if keeps {
                let stored = Set((live.thread.flatMap { state.transcripts[$0] }?.turns ?? []).filter { $0.role == .user }.map(\.content))
                let again = live.overlay.turns.filter { $0.role == .user && !$0.pending && stored.contains($0.content) }.map(\.content)
                if !again.isEmpty {
                    if noted[.kept] == nil {
                        noted[.kept] = broke(.kept, "\(name) keeps \(again) from a run whose reload failed, and draws it under the rows that hold it")
                    }
                    leftover = true
                }
            }

            if live.run != nil, !keeps {
                // A turn an attach replayed, which is how a kept overlay
                // names its turns: a message this device sent is drawn
                // under its own id, and is held by every other rule.
                let stored = Set((live.thread.flatMap { state.transcripts[$0] }?.turns ?? []).filter { $0.role == .user }.map(\.content))
                let carried = live.overlay.turns.filter { turn in
                    // Of the person's own messages: the app's words for
                    // Continue on a server without the route may stand twice.
                    guard let meant = ever[turn.content] else { return false }
                    return turn.role == .user && !turn.pending && turn.id.hasPrefix("live-") && stored.contains(turn.content)
                        && said.filter { $0.role == "user" && $0.text == turn.content }.count > meant
                }
                if !carried.isEmpty {
                    if noted[.carried] == nil {
                        noted[.carried] = broke(.carried, "\(name) sends under \(carried.map(\.content)), kept from a run before, which its rows hold")
                    }
                    leftover = true
                }
            }

            let turnIDs = screen.turns.map(\.id)
            if case .attach? = live.run {
                let rows = Set((live.thread.flatMap { state.transcripts[$0] }?.records ?? []).map(\.id))
                let again = live.overlay.turns.filter { $0.role == .user && !$0.pending && rows.contains($0.id) }
                if !again.isEmpty, Set(turnIDs).count != turnIDs.count {
                    if noted[.attached] == nil {
                        noted[.attached] = broke(.attached, "\(name) is attached and draws \(again.map(\.content)) from its overlay and from its rows")
                    }
                    leftover = true
                }
            }

            // No row is drawn twice: not by its id, and no message of the
            // person's as a bubble and a stored row both.
            if !leftover, Set(turnIDs).count != turnIDs.count { return broke(.drawnTwice, "\(name) draws a turn's id twice: \(turnIDs)") }
            for (text, meant) in ever.sorted(by: { $0.key < $1.key }) where !leftover && said.filter({ $0.role == "user" && $0.text == text }).count > meant {
                return broke(.drawnTwice, "\(name) draws \"\(text)\" twice: \(said.map(\.text))")
            }

            // No failure is drawn in two places.
            let cards = screen.failures.turns
            for turn in cards.keys.sorted() where !turnIDs.contains(turn) {
                return broke(.places, "\(name) draws a card under a turn that is not there: \(turn)")
            }
            if let strip = screen.failures.strip, !strip.working {
                let twins = cards.sorted { $0.key < $1.key }.map(\.value)
                if let twin = twins.first(where: { $0.id == strip.id || ($0.sentence == strip.sentence && $0.note == strip.note) }) {
                    return broke(.places, "\(name) says \"\(twin.sentence)\" at a turn and above the composer")
                }
            }
            let draft = live.draft
            for text in sent.keys.sorted() where draft.contains(text) {
                // The app's own words for "it may be in both": only then.
                let unsure = live.chatFailure?.unconfirmed == true || suspect.contains(text)
                if said.contains(where: { $0.role == "user" && $0.text == text }), !unsure, sent[text]?.ids == true {
                    return broke(.places, "\(name) holds \"\(text)\" in the composer and as a message in the chat")
                }
            }

            // A pending message has its send or its card.
            if let pending = live.pending {
                var carried = false
                if case let .send(run)? = live.run, run.id == pending.id { carried = true }
                if !carried, !live.finding, !live.restored, cards[pending.id] == nil {
                    return broke(.pending, "\(name) holds \"\(pending.message)\" pending with no send out and no card to press")
                }
                if live.standing, cards[pending.id]?.button == nil {
                    return broke(.pending, "\(name) draws \"\(pending.message)\" unsent with no button")
                }
            }

            // No conversation is left waiting with no stream open.
            let waits = live.phase != .idle || live.finding || live.checking
            if waits, !moves(live, key) {
                // Nothing is seen to move it. That is believed once it has
                // stood exactly so while the clock went on: the waits are
                // read from a trace, and one read a moment late must not
                // fail a run. A conversation that is dead stays as it is.
                let mark = "\(Self.waiting(live))|\(live.seq)|\(live.settles)|\(live.tries)|\(live.pending?.id ?? "")"
                if let held = stuck[key], held.mark == mark {
                    if clock.now.timeIntervalSince(held.since) >= 100 {
                        return broke(
                            .streaming,
                            "\(name) is \(Self.waiting(live)) with no request at the server and no wait of its own on the clock, since step \(held.step)")
                    }
                } else {
                    stuck[key] = (mark, clock.now, steps.count)
                }
            } else {
                stuck[key] = nil
            }
            if settled, waits || live.ownsTranscript || state.reading[live.thread ?? ""] != nil {
                let reload = live.reloadFailed == nil ? "" : ", its reload failed"
                return broke(.settles, "\(name) is still \(Self.waiting(live))\(reload) after the server behaved for minutes")
            }

            // The transcript equals the fold of the server's rows, once the
            // app holds the thread as the server does.
            guard !leftover, let thread = live.thread, !waits, !live.ownsTranscript, state.reading[thread] == nil, !server.running(thread)
            else { continue }
            guard let held = state.transcripts[thread], held.version == server.version(of: thread) else {
                if settled, server.version(of: thread) != nil {
                    return broke(
                        .settles,
                        "\(name) holds its rows at version \(state.transcripts[thread]?.version ?? -1), the server at \(server.version(of: thread) ?? -1)")
                }
                continue
            }
            var drawn = said
            if let unsent = live.pending, drawn.last?.id == unsent.id {
                drawn.removeLast()
                if server.rows(of: thread).contains(where: { $0.id == unsent.id }) {
                    return broke(.fold, "\(name) draws \"\(unsent.message)\" as unsent while its rows hold it")
                }
            }
            let want = server.rows(of: thread).map { "\($0.role): \($0.content)" }
            let got = drawn.map { "\($0.role): \($0.text)" }
            if got != want { return broke(.fold, "\(name) draws \(got), and the server holds \(want)") }
            let rowIDs = server.rows(of: thread).map(\.id)
            if drawn.map(\.id) != rowIDs { return broke(.fold, "\(name) draws its rows under \(drawn.map(\.id)), and the server holds \(rowIDs)") }
        }

        composed = inComposer
        if let watched = ProcessInfo.processInfo.environment["CHATKIT_WATCH"] {
            var places: [String] = []
            if server.userRows(saying: watched) > 0 { places.append("server") }
            for (name, draft) in state.drafts.sorted(by: { $0.key < $1.key }) where draft.contains(watched) { places.append("drafts[\(name)]") }
            for (name, message) in state.unsent.sorted(by: { $0.key < $1.key }) where message.message.contains(watched) {
                places.append("unsent[\(name)]")
            }
            for key in state.conversations.keys.sorted() {
                guard let live = await rig.live(key) else { continue }
                if live.draft.contains(watched) { places.append("composer of \(key.rawValue.suffix(2))/\(live.thread ?? "new")") }
                if live.pending?.message == watched {
                    places.append("pending in \(key.rawValue.suffix(2))/\(live.thread ?? "new") standing=\(live.standing) finding=\(live.finding)")
                }
                if live.overlay.turns.contains(where: { $0.content == watched }) { places.append("overlay of \(key.rawValue.suffix(2))") }
            }
            print("WATCH step \(steps.count) [\(steps.last ?? "")]: \(places)")
        }
        // No message is written twice.
        for (text, meant) in ever.sorted(by: { $0.key < $1.key }) where server.userRows(saying: text) > meant {
            return broke(.doubled, "the server holds \"\(text)\" \(server.userRows(saying: text)) times, and the person sent it \(meant)")
        }
        // No two rows share an id, a `messageId` least of all.
        if !server.sharedRowIDs.isEmpty { return broke(.doubled, "the server holds two rows under \(server.sharedRowIDs)") }
        // Without an id the app sends what the person pressed and no more.
        for (text, record) in sent.sorted(by: { $0.key < $1.key }) where !record.ids {
            let requests = server.chatRequests(saying: text) - record.before
            if requests > record.presses {
                return broke(.resent, "\"\(text)\" was pressed \(record.presses) times and sent \(requests) times with no id")
            }
        }
        // Nothing the person sent is lost: it is in a stored row, in a
        // composer, or on a message with its card.
        for text in sent.keys.sorted() where server.userRows(saying: text) == 0 && !server.holds(text) && !homes.contains(text) {
            return broke(.lost, "\"\(text)\" is in no stored row, no composer and on no message")
        }
        if let broken = marks(state, presentation) { return broken }
        // The list holds each chat once, and once settled every chat.
        let listed = state.chats.map(\.id)
        if Set(listed).count != listed.count { return broke(.list, "the list holds a chat twice: \(listed)") }
        if settled, Set(listed) != Set(server.threadIDs) {
            return broke(.list, "the list holds \(listed.sorted()), and the server \(server.threadIDs.sorted())")
        }
        return nil
    }

    /// Whether a chat is unread by the rule, from the row and the mark the
    /// app holds: marked unread, or its newest message is not the one read
    /// through. What this device moved and has not heard back about counts
    /// while it names that newest message.
    private static func unread(_ state: ChatAppState, _ thread: ChatThread) -> Bool {
        guard !thread.running, let mark = known(state, thread) else { return false }
        if mark.markedUnread { return true }
        // Its row is older than its mark, and is being read again.
        if state.rowsBehind.contains(thread.id), mark.ahead(of: thread) { return false }
        guard let last = thread.lastMessage else { return false }
        return mark.readThrough != last
    }

    /// What the person is looking at the end of, read again each time the
    /// clock has moved: what stood there, in front, through the second
    /// that just passed is ripe, and then what stands there now is noted.
    /// Every wait that ended in a move of the clock began on what was seen
    /// before it.
    private mutating func regard(_ state: ChatAppState) {
        if let held = looking, clock.now >= held.since.addingTimeInterval(ChatApp.sightSeconds - 0.000_001) {
            ripe[held.thread, default: []].insert(held.message)
        }
        // Looking at a message is more than standing at the end: the
        // thread has settled with it, the conversation has handed its rows
        // back, and the rows drawn hold it. Checked here, apart from the
        // app's own reading of the same.
        let entry = state.scenes[scene]?.current.flatMap { state.conversations[$0] }
        if inFront, !signedOut, atEnd, viewed == state.scenes[scene]?.current, let entry, !entry.owns, let current = entry.thread,
            let row = state.thread(current), row.status != "running", let last = row.lastMessage,
            state.transcripts[current]?.records.contains(where: { $0.id == last }) == true
        {
            if looking?.thread != current || looking?.message != last { looking = (current, last, clock.now) }
        } else {
            looking = nil
        }
    }

    /// The mark the app holds of a thread: the server's as it last heard
    /// it, with what this device moved and has not heard back about laid
    /// over it where that names the thread's newest message. Nil where it
    /// knows nothing of it.
    private static func known(_ state: ChatAppState, _ thread: ChatThread) -> ThreadMark? {
        let moved = [state.markSending[thread.id], state.marking[thread.id]].compactMap { $0 }
        guard var mark = state.marks?[thread.id] ?? (moved.isEmpty ? nil : ThreadMark.none) else { return nil }
        for change in moved {
            if let read = change.readThrough, thread.lastMessage == nil || read == thread.lastMessage {
                mark.readThrough = read
                mark.markedUnread = change.markedUnread ?? false
            } else if let flag = change.markedUnread {
                mark.markedUnread = flag
            }
        }
        return mark
    }

    /// The rules of read state, on what the app draws and what the server
    /// was sent.
    private mutating func marks(_ state: ChatAppState, _ presentation: ChatPresentation) -> Violation? {
        regard(state)
        if let watched = ProcessInfo.processInfo.environment["CHATKIT_MARK"] {
            let row = state.thread(watched).map { "\($0.status), newest \($0.lastMessage ?? "none")" } ?? "not held"
            print(
                "MARK step \(steps.count) [\(steps.last ?? "")]: row \(row); the app holds \(String(describing: state.marks?[watched])), moved "
                    + "\(String(describing: state.marking[watched] ?? state.markSending[watched])); the server \(String(describing: server.mark(of: watched))); "
                    + "notifications \(rig.notifications.pending.keys.sorted())")
        }
        let writes = server.markWrites
        for write in writes.dropFirst(judged) {
            if !server.keepsMarks { return broke(.sight, "a mark of \(write.thread) was sent to a server that keeps none") }
            if let read = write.readThrough, ripe[write.thread]?.contains(read) != true, pressedRead[write.thread]?.contains(read) != true {
                return broke(
                    .sight,
                    "\(write.thread) was written as read through \(read), which never stood at the chat's end in front for a second "
                        + "(those did: \((ripe[write.thread] ?? []).sorted())) and was not marked read by hand")
            }
            if write.markedUnread == true, !pressedUnread.contains(write.thread) {
                return broke(.sight, "\(write.thread) was written as marked unread, and nobody pressed Mark as Unread on it")
            }
            if write.readThrough == nil, write.markedUnread == false, pressedRead[write.thread]?.contains("") != true {
                return broke(.sight, "\(write.thread) had its Mark as Unread taken back with no position, and nobody pressed Mark as Read on it")
            }
        }
        judged = writes.count

        let rows = presentation.list.sections.flatMap(\.rows)
        // The badge is the dots, counted.
        var badge: Int?
        for case let .badge(count) in rig.notifications.calls { badge = count }
        let dots = rows.filter(\.unread).count
        if let badge, badge != dots { return broke(.badge, "the badge says \(badge), and \(dots) chats are drawn unread") }
        guard state.marks != nil else {
            // The device's own map says what is unread, as before marks.
            cleared = [:]
            return nil
        }
        for row in rows.sorted(by: { $0.id < $1.id }) {
            guard let thread = state.thread(row.id) else { continue }
            let rule = Self.unread(state, thread)
            if row.unread != rule {
                return broke(
                    .badge, "\(row.id) is drawn \(row.unread ? "unread" : "read"), and by its mark and its newest message it is \(rule ? "unread" : "read")")
            }
            guard !thread.running, let last = thread.lastMessage else { continue }
            let truth = server.mark(of: row.id)
            let held = server.rows(of: row.id).map(\.id)
            // The server holds the message read: at or before the position.
            let read = truth?.readThrough.flatMap { held.firstIndex(of: $0) }.map { at in held.firstIndex(of: last).map { $0 <= at } ?? false } ?? false
            let flagged = truth?.markedUnread == true
            if row.unread {
                let pressed = state.marking[row.id]?.markedUnread == true || state.markSending[row.id]?.markedUnread == true
                // The app hears of a mark's moves in their order: one it
                // holds marked unread, from before the server's newest, is
                // a Mark as Unread that was set and it has not heard taken
                // back yet.
                let behind = state.marks?[row.id].map { $0.markedUnread && $0.version < (truth?.version ?? 0) } ?? false
                if cleared[row.id] == last, !flagged, !pressed, !behind {
                    return broke(
                        .dot,
                        "\(row.id) is drawn unread again for \(last), which the server holds read (through \(truth?.readThrough ?? "nothing")) and the "
                            + "list drew read before; the app holds the mark \(String(describing: state.marks?[row.id]))")
                }
                if flagged { cleared[row.id] = nil }
            } else if read, !flagged, let mark = Self.known(state, thread), mark.readThrough == last, !mark.markedUnread {
                // Drawn read because the app holds it read, and the server
                // does: from here a dot is one that came back. A chat drawn
                // read while its row is read again is not yet that.
                cleared[row.id] = last
            }
        }
        // A reply's notification, pending or shown, for a chat the app
        // itself holds read.
        for (id, request) in rig.notifications.pending.sorted(by: { $0.key < $1.key }) where id.hasSuffix("|reply") {
            guard let name = request.thread, let thread = state.thread(name), !thread.running, state.marked(name), !Self.unread(state, thread)
            else { continue }
            // A call that failed is told whoever read the reply before it:
            // it stands until somebody has looked since it settled.
            if thread.status == "error", let settled = thread.record["finishedAt"]?.string.flatMap(Timestamp.parse),
                Self.known(state, thread)?.readAt.map({ $0 <= settled }) ?? true
            {
                continue
            }
            return broke(
                .notified,
                "a notification of \(name)'s reply stands (\(request.fireAt.map { $0 <= clock.now ? "shown" : "waiting" } ?? "shown")) while the app "
                    + "holds the chat read through \(thread.lastMessage ?? "nothing")")
        }
        return nil
    }

    /// The server behaves, the person is signed in and in front, every
    /// turn runs to its end and time passes: then everything must have
    /// settled, and every transcript must be the server's.
    private mutating func settle() async -> Violation? {
        server.misbehave(0)
        if signedOut {
            signedOut = false
            await rig.runtime.linkChanged(link)
        }
        if !inFront {
            inFront = true
            await rig.harness.send(.phase(.active))
            await says(atEnd)
        }
        await drain()
        await appears()
        for round in 0 ..< 60 {
            forgetTrace()
            server.finishTurns()
            await drain()
            regard(await rig.harness.state)
            clock.advance(by: 4)
            await drain()
            regard(await rig.harness.state)
            if round % 6 == 5 {
                // The list reads again, as it does whenever the app comes
                // to the front: what moved while it could not hear is read.
                await rig.harness.send(.refresh)
                await drain()
                // And the person presses Try Again where a read of a chat
                // failed: the app said so, and offers nothing else.
                let state = await rig.harness.state
                for thread in state.rowsFailed.keys.sorted() { await rig.harness.send(.reloadRows(thread: thread)) }
                for key in state.conversations.keys.sorted() {
                    guard let live = await rig.live(key) else { continue }
                    if live.reloadFailed != nil { await tell(.retryReload, key) }
                    if live.chatFailure?.attempt == .attach { await tell(.reopen, key) }
                }
                await drain()
            }
            if round >= 24, await quiet() { break }
        }
        await rig.harness.send(.refresh)
        await drain()
        regard(await rig.harness.state)
        clock.advance(by: 1)
        await drain()
        if let broken = await confirmed(settled: false) { return broken }
        return await confirmed(settled: true)
    }

    /// A broken rule, with the app's state where `CHATKIT_VERBOSE` asks
    /// for it. The app stands still when this looks, so what it finds is
    /// what the seed gives and is not looked for twice.
    private mutating func confirmed(settled: Bool) async -> Violation? {
        var broken = await check(settled: settled)
        if ProcessInfo.processInfo.environment["CHATKIT_VERBOSE"] != nil, let found = broken {
            var states = ""
            for key in await rig.harness.state.conversations.keys.sorted() {
                if let live = await rig.live(key) { states += "\n  \(key.rawValue): \(live)" }
            }
            let trace = rig.harness.trace.suffix(ProcessInfo.processInfo.environment["CHATKIT_TRACE"] == nil ? 40 : 4000).map {
                "\n  > " + String(describing: $0).prefix(400)
            }.joined()
            let activity = await rig.runtime.activity
            let waits = clock.deadlines.map { String(format: "%.2f", $0.timeIntervalSince(clock.now)) }
            broken?.what =
                found.what + states + trace
                + "\n  running effects: \(activity.running.sorted()) of \(activity.started), sleeps end in \(waits) s, open at the server: \(server.openExchanges)"
        }
        return broken
    }

    private func quiet() async -> Bool {
        let state = await rig.harness.state
        guard !state.refreshing, state.reading.isEmpty, !server.anyRunning else { return false }
        for key in state.conversations.keys {
            guard let live = await rig.live(key) else { continue }
            if live.phase != .idle || live.finding || live.checking { return false }
        }
        return true
    }
}
