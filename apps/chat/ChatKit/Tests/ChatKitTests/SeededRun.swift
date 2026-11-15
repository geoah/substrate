import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateTesting

@testable import ChatKit

/// One seed's run of the app against a server that misbehaves by it: a
/// person's steps chosen from the seed (sends, stops, Try Again and
/// Continue, Send Again and Edit Message, opening and closing chats,
/// leaving and coming back to the front, kills and relaunches, scrolling to
/// a chat's end and away from it, Mark as Unread and Mark as Read, Pin and
/// Archive and Undo, the archived list, the list scrolled to its end, a
/// search, a message sent while the assistant works, a question answered by
/// an option or in their own words or dismissed, a call approved or
/// declined), the server's turns moved, a chat coming to wait on them,
/// another device of theirs moving a mark or a flag or resolving a card, and
/// the clock advanced between them, and after every step the rules a chat
/// must keep whatever the server does.
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
        /// One turn, one model (`TurnFold`): the stream's turn and its
        /// stored row are one row, from the first event to the last read.
        case shorter = "the text drawn for a turn is never shorter than it was a step ago"
        case renamed = "a row keeps the id its turn was first drawn by"
        case emptied = "nothing drawn of a run goes while the run goes on, an attach least of all"
        /// The chat list (`ChatAppLists.swift`), where the server keeps a
        /// person's pins and archives (decision 0322) and cuts its lists
        /// into pages. An archived chat is drawn, counted and notified for
        /// only while something in it waits on a person.
        case archived = "an archived chat is in no list, badge or notification unless it waits"
        case pinned = "a pinned chat is always shown"
        case sections = "a chat is in at most one section"
        /// Read from the list scrolled to its end once the server behaves:
        /// every chat the server lists, once.
        case paging = "paging never shows a chat twice or loses one when the order changes under it"
        case undo = "Undo restores exactly"
        /// A pin or an archive is written only where the person pressed
        /// it, or spoke in a chat they had archived; and never to a server
        /// that keeps neither.
        case flags = "a flag is never written but where the person set it"
        case found = "a search draws a message under the chat that holds it"
        /// Known (below): a message sent under no id is found among the
        /// rows by its words, and where the rows held when it was sent were
        /// unread it may be paired with an older row of the same words.
        case mispaired = "a message sent under no id is drawn as its own row, not an older one of the same words"
        /// A message sent while the assistant works (`ConversationQueue`).
        /// It is drawn as queued only while the server holds it in a turn
        /// that may still read it, as far as the app can know: never again
        /// once it was drawn read, never in a conversation nothing runs
        /// in, and never one the server does not hold.
        case queued = "a message is never drawn queued after it was taken or its run ended"
        /// A card (`CardPresenter.fold`). One the app drew as ended, or
        /// holds the server's last word of, is never drawn waiting again.
        case resolved = "a resolved card is never drawn waiting"
        /// One that waits at the server, under a call the transcript
        /// draws, stands there: or the composer says it is being answered
        /// in the person's own words.
        case hidden = "a card that waits on the person is never hidden"
        /// A card is resolved by one write, whoever made it and however
        /// often it was pressed.
        case decidedOnce = "a card is resolved once"
        /// Where a message names the questions it answers (`agents` 7),
        /// answering in the person's own words writes no dismissal and
        /// resumes nothing: the turn that reads the message is the only
        /// one. A question is dismissed only where Dismiss was pressed.
        case words = "answering in words never costs a second turn where the message names the questions"
        /// Whose hand ended a card is the server's to say: "You" only for
        /// a card the server links to the person the app is signed in as.
        case hand = "a card never says You for another person's hand"
        /// An action of a notification (`ChatAppActions`): the device is
        /// told it is over within the time an action has, whatever the
        /// server does, and what it asked for happens at most once.
        case acted = "an action from a notification is over within its time"
        /// What stands under the last reply (`ReplyActions`): under the
        /// last reply drawn that has words, with no message after it that
        /// nothing answered and nothing is answering, or the run again
        /// alone under the chat's last turn; with the run again never
        /// offered both there and on the turn's card, never while a run
        /// goes on, and nothing to share of a reply still being written.
        /// And there where it must be: under a last turn that is a reply
        /// with words, and with the run again a card names beside its
        /// button.
        case offered = "what stands under a reply is under the last one, and offers the run again once or not at all"
    }

    /// Rules the app is known to break today, each with what breaks it. A
    /// run notes one where it meets it and goes on, so the rest of the seed
    /// is still held to every other rule. An entry that no longer fails
    /// must not stay.
    static let known: [Rule: String] = [
        .mispaired:
            "seed 6523 at 76 steps (CHATKIT_SEED=6523 CHATKIT_STEPS=76): after a relaunch, before the chat's rows and the server's document are read, "
            + "\"say 3.\" is sent under no id into a thread whose rows already hold a \"say 3.\" sent under an id in the launch before. The fold "
            + "(TurnFold, pairing a message sent under no id by its words) gives the new bubble's id to the older row. The transcript drawn is the "
            + "server's rows, each once; only which of the two equal bubbles keeps the id `sent-1` is wrong. Found by the list's new steps, not "
            + "caused by them: the pairing is the send-id group's. That seed is of the list's branch (#1464): with the answering steps "
            + "beside them (#1465) a seed's run is another, and the one that reaches it is seed 1171 at 101 steps on a server at agents 7 "
            + "(CHATKIT_REVISION=7 CHATKIT_SEED=1171 CHATKIT_STEPS=101).",
        .queued:
            "seed 9435 at 39 steps on a server at agents 7 (CHATKIT_REVISION=7 CHATKIT_SEED=9435 CHATKIT_STEPS=39): after the person signs in "
            + "again twice while a run goes on, a message a call had read, and that the chat drew as read, is drawn queued once more when the "
            + "next send joins the run. A conversation made anew by the sign-in holds none of what it knew was taken (`ConversationState.taken`), "
            + "and the run's stream, said again from its start, says `messageQueued` of it before it says `messageTaken`. It is the answering "
            + "group's (#1465, `ConversationQueue.hear`): the same seed breaks the rule on that branch's head (04537b747) with nothing of the "
            + "reply's actions in it. Found by 160,000 steps over the joined line (group A8, #1466).",
        .archived:
            "seed 30167 at 45 steps on a server at agents 7 (CHATKIT_REVISION=7 CHATKIT_SEED=30167 CHATKIT_STEPS=45), and seed 20442 at 101 "
            + "steps at its own revision (CHATKIT_SEED=20442 CHATKIT_STEPS=101): another device archives a chat, and a notification of that "
            + "chat's reply stands on this device afterwards with nothing in the chat waiting on a person. In the first the app is killed and "
            + "launched again several times between the archive and the reply; in the second the other device marks the chat unread after "
            + "archiving it. Only the notification is held as known: an archived chat drawn in the list or counted in the badge still breaks "
            + "the rule. It is the list's and the answering work's (#1464, #1465): both seeds break the rule on the answering branch's head "
            + "(04537b747) with nothing of the reply's actions in it. Found by 160,000 more steps over the joined line (group A8, #1466).",
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
    /// How many chats one read of the list asks for: few, so a handful of
    /// chats is several pages.
    let page: Int
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
    /// The window lists the archived chats, and what it searches for.
    private var showsArchived = false
    private var query = ""
    /// What the person pressed of a thread's flags, by thread: "pin",
    /// "unpin", "archive", "unarchive"; and the chats they spoke in, where
    /// an archive ends without a press.
    private var pressedFlags: [String: Set<String>] = [:]
    private var spokeIn: Set<String> = []
    /// The chat the person archived, as the list drew it the moment before:
    /// its row, where it stood among the rows, and the version of its
    /// thread. What Undo must put back.
    private var shelved: (thread: String, row: ThreadRow, at: Int, rows: [String], version: Int, step: Int, pinned: Bool?)?
    /// Undo was pressed in this step, on this chat.
    private var undid: String?
    /// The chat a message was sent in in this step.
    private var justSpoke: String?
    /// What each conversation drew at the last step: its thread, how many
    /// runs it had settled, whether one went on, and every turn's id, role
    /// and text. What the fold's rules compare a step with.
    private var drew: [ChildKey: Frame] = [:]
    /// The cards whose view asked for their record since the chat came on
    /// screen: a card's view reads it once, when it appears.
    private var asked: Set<String> = []
    /// The messages each conversation drew as queued, and the ones it then
    /// drew as read.
    private var drewQueued: [ChildKey: Set<String>] = [:]
    private var drewRead: [ChildKey: Set<String>] = [:]
    /// The cards the app drew as ended.
    private var drewEnded: Set<String> = []
    /// The questions the person pressed Dismiss on, ever, and the ones
    /// they answered in words before the app knew the server's revision,
    /// which are dismissed after their message as on an older server.
    private var pressedDismiss: Set<String> = []
    /// The actions of notifications this launch was handed, each with
    /// when: the device must be told each is over.
    private var acts: [(response: NotificationResponse, at: Date)] = []
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
        // And of its own again: half of the servers that take a message's
        // id also say where a stream's text belongs (`agents` 5).
        var positions = SeededNumbers(seed: seed ^ SeededNumbers.hash("positions"))
        // And once more: half of those also take a message into a running
        // turn (`agents` 6).
        var queue = SeededNumbers(seed: seed ^ SeededNumbers.hash("queue"))
        let positioned = drawn == 3 && positions.below(2) == 0
        // And once more: half of those also take the questions a message
        // answers with it, and link who resolved a card (`agents` 7).
        var words = SeededNumbers(seed: seed ^ SeededNumbers.hash("words"))
        let joins = positioned && queue.below(2) == 0
        let agents = revision ?? (joins ? (words.below(2) == 0 ? 7 : 6) : positioned ? 5 : drawn)
        // And of its own once more: three in four of the servers that keep
        // marks keep the flags too, and the list is read a few chats a page.
        var flags = SeededNumbers(seed: seed ^ SeededNumbers.hash("flags"))
        let kept = marks.below(6) == 0 ? 7 : 8
        let records = kept == 8 && flags.below(4) != 0 ? 9 : kept
        page = flags.pick([2, 2, 3, 4, 100])
        let server = SeededServer(seed: seed, clock: clock, revision: agents, records: records)
        self.clock = clock
        self.ids = ids
        self.base = base
        self.server = server
        rig = ChatRig(
            platform: .ios, base: base, transport: server.connect(), clock: clock, ids: WatchedIDs(ids: ids, watch: server.watch),
            watch: server.watch, page: page)
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
                "marks chat": "person: Mark as Unread or Mark as Read", "another device reads": "another device moves a mark",
                "another person": "another person resolves a card",
                "another device marks": "another device moves a mark", "another device pins": "another device moves a flag",
                "another device unpins": "another device moves a flag", "another device archives": "another device moves a flag",
                "another device unarchives": "another device moves a flag", "pins chat": "person: Pin or Unpin",
                "unpins chat": "person: Pin or Unpin", "archives chat": "person: Archive", "unarchives chat": "person: Unarchive",
                "presses Undo": "person: Undo", "opens the archived": "person: opens or closes the archived list",
                "closes the archived": "person: opens or closes the archived list", "scrolls the list": "person: scrolls a list to its end",
                "searches": "person: searches", "chat ": "a chat comes to wait on the person, or is answered",
                "taps an option": "person: taps an option",
                "dismisses i": "person: dismisses questions", "chooses to answer": "person: answers in their own words",
                "approves": "person: approves a call", "declines": "person: declines a call",
                "goes back to the question": "person: goes back to a question's options",
                "answers the notification": "person: answers from a notification",
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
        if server.keepsMarks, !server.threadIDs.isEmpty { choices += [(4, "elsewhereRead"), (1, "elsewhereUnread"), (2, "wait")] }
        if server.keepsFlags, !server.threadIDs.isEmpty { choices += [(2, "elsewherePin"), (2, "elsewhereArchive")] }
        let current = await shown()
        let list = await rig.harness.presentation.list
        let listed = list.sections.flatMap(\.rows)
        undid = nil
        justSpoke = nil
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
            // The list's own: offered where the server keeps the flags.
            choices += [(2, "search")]
            if list.more || list.archived?.more == true { choices.append((6, "end")) }
            if list.flags {
                choices.append((2, "archived"))
                if !listed.isEmpty { choices += [(3, "pin"), (4, "archive")] }
                if list.undo != nil { choices.append((8, "undo")) }
                if showsArchived, list.archived?.rows.isEmpty == false { choices.append((4, "unarchive")) }
            }
            if let current {
                choices += [(2, "back"), (8, "scroll")]
                if current.screen.composer.canSend { choices.append((30, "send")) }
                if current.screen.streaming { choices.append((5, "stop")) }
                if !buttons(current.screen).isEmpty { choices.append((14, "button")) }
                // A card that stands: an option, the person's own words, a
                // dismissal, an approval or a refusal.
                if !standing(current.screen).isEmpty { choices.append((34, "card")) }
                if current.screen.composer.answering != nil { choices.append((3, "options")) }
            }
        }
        // The person's other device answers what waits.
        if server.allCards.contains(where: \.pending) { choices.append((3, "elsewhereCard")) }
        // And the person answers a notification where it stands, in front
        // of the app or away from it.
        if !signedOut, !actionable().isEmpty { choices.append((10, "notification")) }
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
        case "elsewherePin":
            let thread = numbers.pick(server.threadIDs)
            let pinned = server.mark(of: thread)?.pinned != true
            steps.append("another device \(pinned ? "pins" : "unpins") chat \(thread)")
            server.elsewhere(thread, pinned: pinned)
        case "elsewhereArchive":
            let thread = numbers.pick(server.threadIDs)
            let archived = server.mark(of: thread)?.archived != true
            steps.append("another device \(archived ? "archives" : "unarchives") chat \(thread)")
            server.elsewhere(thread, archived: archived)
        case "wait":
            let thread = numbers.pick(server.threadIDs)
            let waits = server.waiting(thread) == 0
            steps.append("chat \(thread) \(waits ? "comes to wait on the person" : "is answered")")
            server.wait(thread, on: waits ? 1 : 0)
        case "pin":
            let row = numbers.pick(listed)
            steps.append("\(row.pinned ? "unpins" : "pins") chat \(row.id)")
            pressedFlags[row.id, default: []].insert(row.pinned ? "unpin" : "pin")
            await rig.harness.send(.pin(thread: row.id, pinned: !row.pinned))
        case "archive":
            // The row's own button: Archive, or Unarchive on one that is
            // listed, archived as it is, because it waits.
            let row = numbers.pick(listed)
            steps.append("\(row.archived ? "unarchives" : "archives") chat \(row.id)")
            pressedFlags[row.id, default: []].insert(row.archived ? "unarchive" : "archive")
            if !row.archived {
                let state = await rig.harness.state
                let version = state.thread(row.id)?.record.version ?? 0
                shelved = (row.id, row, listed.firstIndex(of: row) ?? 0, listed.map(\.id), version, steps.count, Self.flag(state, row.id, \.pinned, \.pinned))
            }
            await rig.harness.send(.archive(thread: row.id, archived: !row.archived))
        case "undo":
            steps.append("presses Undo")
            if let thread = list.undo?.thread {
                pressedFlags[thread, default: []].insert("unarchive")
                undid = thread
            }
            await rig.harness.send(.undoArchive)
        case "archived":
            showsArchived.toggle()
            steps.append(showsArchived ? "opens the archived list" : "closes the archived list")
            await rig.harness.send(.showArchived(scene: scene, shown: showsArchived))
        case "unarchive":
            let row = numbers.pick(list.archived?.rows ?? [])
            steps.append("unarchives chat \(row.id)")
            pressedFlags[row.id, default: []].insert("unarchive")
            await rig.harness.send(.archive(thread: row.id, archived: false))
        case "end":
            let archived = showsArchived && list.archived?.more == true
            steps.append("scrolls the list\(archived ? " of archived chats" : "") to its end")
            await rig.harness.send(.more(archived: archived))
        case "search":
            // A word every message of the person's holds, the start of a
            // reply, one message's own words, or nothing.
            query = numbers.pick(["say", "r0", "say \(1 + numbers.below(max(1, typed))).", ""])
            steps.append("searches the chats for \"\(query)\"")
            await rig.harness.send(.search(scene: scene, query: query))
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
        case "card":
            if let current { await decide(numbers.pick(standing(current.screen)), current.key, current.screen) }
        case "notification":
            await act(numbers.pick(actionable()))
        case "options":
            steps.append("goes back to the question's options")
            if let current { await tell(.showOptions, current.key) }
        case "elsewhereCard":
            let card = numbers.pick(server.allCards.filter(\.pending))
            let accept = numbers.chance(0.6)
            // Mostly a device of the same person; sometimes another
            // person of the repository.
            let other = numbers.chance(0.3)
            steps.append("\(other ? "another person" : "another device") \(accept ? "answers or approves" : "dismisses or declines") \(card.id)")
            server.elsewhere(card: card.id, accept: accept, by: other ? "other" : "me")
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
        if key != viewed {
            viewed = key
            asked = []
            atEnd = key != nil && numbers.chance(0.7)
            if inFront { await says(atEnd) }
            await drain()
        }
        // A card's view asks for its record when it appears, once: the
        // card that stands, and the row a card ended on when it is opened,
        // which the run always does.
        guard let key, let screen = await rig.conversation(key) else { return }
        var wanted: [CardReference] = screen.approvals.map(CardReference.approval)
        for case let .assistant(reply) in screen.turns.map(\.body) {
            for tool in reply.tools {
                if let card = tool.asked, tool.attachment != nil || tool.resolution?.loading == true { wanted.append(card) }
            }
        }
        for card in wanted where asked.insert(card.id).inserted { await rig.harness.send(.loadCard(card)) }
        await drain()
    }

    /// The notifications that are shown and offer an action.
    private func actionable() -> [NotificationRequest] {
        rig.notifications.pending.values.filter { $0.category != nil && ($0.fireAt.map { $0 <= clock.now } ?? true) }.sorted { $0.id < $1.id }
    }

    /// An action of a notification: a reply with a line of text, an
    /// approval or a refusal, an option of a question. Sometimes the
    /// device hands the same response over twice.
    private mutating func act(_ note: NotificationRequest) async {
        var action = ChatNotifications.Action.open.id
        var text: String?
        switch note.category {
        case ChatNotifications.replyCategory:
            action = ChatNotifications.Action.reply.id
            let words = word()
            text = words
            // A message of the person's like any other: sent once, under
            // an id, and never lost.
            sent[words] = (1, true, server.chatRequests(saying: words))
            ever[words, default: 1] += 0
            // And theirs to follow again, where the chat was archived.
            if let thread = note.thread { spokeIn.insert(thread) }
        case ChatNotifications.approvalCategory:
            action = numbers.chance(0.6) ? ChatNotifications.Action.approve.id : ChatNotifications.Action.decline.id
        case ChatNotifications.openCategory:
            break
        default:
            let options = rig.notifications.categories.first { $0.id == note.category }?.actions ?? []
            guard !options.isEmpty else { return }
            action = numbers.pick(options).id
        }
        let response = NotificationResponse(id: note.id, category: note.category, action: .named(action), userInfo: note.userInfo, text: text)
        let twice = numbers.chance(0.15)
        steps.append("answers the notification \(note.id) with \(action)\(text.map { " \"\($0)\"" } ?? "")\(twice ? ", handed over twice" : "")")
        acts.append((response, clock.now))
        rig.notifications.respond(response)
        if twice { rig.notifications.respond(response) }
    }

    /// The cards that stand in a chat and wait: each can be acted on.
    private func standing(_ screen: ConversationScreen) -> [CardReference] {
        var out: [CardReference] = []
        for case let .assistant(reply) in screen.turns.map(\.body) {
            for tool in reply.tools {
                guard let card = tool.asked, tool.attachment != nil, let drawn = screen.cards[card.id], drawn.waits else { continue }
                let busy = drawn.question?.busy ?? drawn.approval?.busy ?? drawn.suggestion?.busy
                if busy == nil { out.append(card) }
            }
        }
        return out
    }

    /// What a person does with a card that waits on them.
    private mutating func decide(_ card: CardReference, _ key: ChildKey, _ screen: ConversationScreen) async {
        switch card {
        case let .question(id):
            guard let drawn = screen.cards[id]?.question else { return }
            switch numbers.below(5) {
            case 0, 1:
                // An option of each question, as its card offers them.
                var picks: [String: [String]] = [:]
                for question in drawn.questions { picks[question.id] = [numbers.pick(question.options.map(\.value))] }
                steps.append("taps an option of \(id)")
                await rig.harness.send(.answer(question: id, picks: picks))
            case 2:
                steps.append("dismisses \(id)")
                pressedDismiss.insert(id)
                await rig.harness.send(.dismissQuestions(id))
            default:
                steps.append("chooses to answer \(id) in their own words")
                await tell(.answerInWords(id), key)
                // And mostly says it at once. Now and then the questions
                // are resolved elsewhere while the words are on their way.
                guard numbers.chance(0.7), let now = await shown(), now.screen.composer.canSend else { break }
                if numbers.chance(0.2) {
                    let other = numbers.chance(0.5)
                    steps.append("\(other ? "another person" : "another device") dismisses or declines \(id) as the words go")
                    server.elsewhere(card: id, accept: false, by: other ? "other" : "me")
                }
                await send(now.key, now.screen)
            }
        case let .approval(id):
            let approve = numbers.chance(0.6)
            steps.append("\(approve ? "approves" : "declines") \(id)")
            await rig.harness.send(.decideApproval(id, approve: approve))
        case .suggestion:
            break
        }
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
        // Words for questions, sent before the app had read what the server
        // takes: they go as a message alone, and the questions are
        // dismissed after it, as below `agents` 7.
        if let questions = await rig.live(key)?.answering, (await rig.harness.state.agentsRevision ?? 0) < Conversation.wordsRevision {
            pressedDismiss.insert(questions)
        }
        let before = await rig.live(key)?.sends
        await tell(.send, key)
        // The message went: where the chat was archived, its archive ends.
        if let thread = screen.thread, await rig.live(key)?.sends != before {
            spokeIn.insert(thread)
            justSpoke = thread
        }
    }

    /// Every button on the screen that can be pressed now.
    private func buttons(_ screen: ConversationScreen) -> [FailureAction] {
        var out: [FailureAction] = []
        let inCards = screen.cards.values.compactMap { $0.question?.failure ?? $0.approval?.failure ?? $0.suggestion?.failure }
        for failure in Array(screen.failures.turns.values) + [screen.failures.strip].compactMap({ $0 }) + inCards {
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
            if let thread = screen.thread { spokeIn.insert(thread) }
            await tell(.continueTurn, key)
        case .send:
            await send(key, screen)
        case .sendAgain:
            steps.append("presses Send Again on the unsent message")
            if let thread = screen.thread { spokeIn.insert(thread) }
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
        case let .retryCard(reference):
            steps.append("presses Try Again in a card")
            await rig.harness.send(.retryCard(reference))
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
        drew = [:]
        asked = []
        drewQueued = [:]
        drewRead = [:]
        drewEnded = []
        // The process is gone, and with it the time the system gave its
        // actions: one unfinished is lost with it, but for a reply, which
        // the folder keeps.
        acts = []
        // The view is gone with the process, and what the launch draws
        // first is the folder's: a dot it kept may be older than the read.
        viewed = nil
        atEnd = false
        looking = nil
        cleared = [:]
        showsArchived = false
        query = ""
        shelved = nil
        rig = ChatRig(
            platform: .ios, base: base, transport: server.connect(), clock: clock, ids: WatchedIDs(ids: ids, watch: server.watch),
            watch: server.watch, page: page)
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

    /// One conversation as a step left it drawn.
    private struct Frame {
        var thread: String?
        var settles: Int
        /// A run of the server's went on: a send the server had named, or
        /// an attach.
        var running: Bool
        var turns: [(id: String, role: String, text: String)]
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
        // A reply typed into a notification, until the app is ready to
        // send it: held by the list.
        state.deferred.forEach { home($0.response.text ?? "") }
        for entry in state.conversations.values { home(entry.reply?.text ?? "") }

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

            let turnIDs = screen.turns.map(\.id)

            // One turn, one model: against what this conversation drew a
            // step ago. A kill, a sign-out and a chat that turned out to
            // be another thread start it over.
            var streams = false
            switch live.run {
            case let .send(run)?: streams = run.named && !run.resumes || run.started
            case .attach?: streams = true
            case nil: break
            }
            let frame = Frame(thread: live.thread, settles: live.settles, running: streams && live.phase == .streaming, turns: said)
            if let before = drew[key], before.thread == live.thread || before.thread == nil, !live.deleted {
                let now = Dictionary(said.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
                let had = Set(before.turns.map(\.id))
                for turn in before.turns {
                    if let same = now[turn.id] {
                        // A piece sent twice by a server that says no
                        // position cannot be told from a word said twice:
                        // the row then holds less than was drawn.
                        let twice = server.revision < 5 && live.thread.map(server.repeated) ?? false
                        if !same.text.hasPrefix(turn.text), !twice {
                            return broke(.shorter, "\(name) drew \(turn.id) as \"\(turn.text)\" and draws it as \"\(same.text)\"")
                        }
                        continue
                    }
                    guard turn.role == "assistant", !turn.text.isEmpty else { continue }
                    if let other = said.first(where: { $0.role == turn.role && !had.contains($0.id) && $0.text.hasPrefix(turn.text) }) {
                        return broke(.renamed, "\(name) drew \"\(turn.text)\" as \(turn.id) and draws it as \(other.id)")
                    }
                    if before.running, frame.running, before.settles == live.settles {
                        return broke(.emptied, "\(name) drew \(turn.id) as \"\(turn.text)\", and it is gone while its run goes on: \(said.map(\.id))")
                    }
                }
            }
            drew[key] = frame

            if let broken = answering(key, live, screen, name, state) { return broken }

            // No row is drawn twice: not by its id, and no message of the
            // person's as a bubble and a stored row both.
            if Set(turnIDs).count != turnIDs.count { return broke(.drawnTwice, "\(name) draws a turn's id twice: \(turnIDs)") }
            for (text, meant) in ever.sorted(by: { $0.key < $1.key }) where said.filter({ $0.role == "user" && $0.text == text }).count > meant {
                return broke(.drawnTwice, "\(name) draws \"\(text)\" twice: \(said.map(\.text))")
            }

            // Nor a reply: each of the server's is its own words.
            let replies = said.filter { $0.role == "assistant" && !$0.text.isEmpty }.map(\.text)
            if Set(replies).count != replies.count { return broke(.drawnTwice, "\(name) draws a reply twice: \(said.map(\.text))") }

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
            // What stands under the last reply is offered once.
            if let actions = screen.replyActions {
                guard let at = screen.turns.firstIndex(where: { $0.id == actions.turn }) else {
                    return broke(.offered, "\(name) offers actions under \(actions.turn), which is no turn drawn")
                }
                let after = screen.turns[(at + 1)...]
                // Under the last reply that has words, and no older one.
                let later = after.contains { turn in
                    if case let .assistant(reply) = turn.body { reply.text != nil } else { false }
                }
                if later { return broke(.offered, "\(name) offers actions under \(actions.turn), with a later reply drawn") }
                // A message after it makes it an older one, but while a
                // run goes on or where the assistant did something after
                // the message: the row then stays where the last words are.
                let working = screen.streaming || screen.status.contains { $0.kind == .background }
                if let message = after.lastIndex(where: { if case .user = $0.body { true } else { false } }), !working {
                    let answered = screen.turns[(message + 1)...].contains { if case .assistant = $0.body { true } else { false } }
                    if !answered, case .assistant = screen.turns[at].body {
                        return broke(.offered, "\(name) offers actions under \(actions.turn), with a message nothing answered after it")
                    }
                }
                if case let .assistant(reply) = screen.turns[at].body {
                    if actions.copies != (reply.text != nil) || (actions.whole && reply.live) {
                        return broke(
                            .offered,
                            "\(name) offers to copy or share \(actions.turn), which has \(reply.text == nil ? "no words" : "words still being written")")
                    }
                } else if actions.copies || actions.whole || actions.resume == nil || at != screen.turns.count - 1 {
                    // Under a turn that is no reply only the run again
                    // stands, and only under the chat's last.
                    return broke(.offered, "\(name) offers more than the run again under \(actions.turn), which is no reply")
                }
                if let resume = actions.resume {
                    if screen.turns.last.flatMap({ cards[$0.id] })?.button?.action == .resume {
                        return broke(.offered, "\(name) offers \(resume.title) under the reply and on its card")
                    }
                    if !resume.busy, live.phase != .idle { return broke(.offered, "\(name) offers \(resume.title) while a run goes on") }
                }
            }
            // And it is there where it must be, said without asking what
            // made it: under a chat whose last turn is a reply with words,
            // and with the run again wherever the last turn's card names
            // one beside its button.
            if let last = screen.turns.last {
                if case let .assistant(reply) = last.body, reply.text != nil, screen.replyActions?.turn != last.id {
                    return broke(.offered, "\(name) draws nothing under \(last.id), its last turn and a reply with words")
                }
                if let named = cards[last.id]?.resume, screen.replyActions?.resume != named {
                    return broke(.offered, "\(name) names \(named.title) beside its card's button under \(last.id) and offers it nowhere")
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
            guard let thread = live.thread, !waits, !live.ownsTranscript, state.reading[thread] == nil, !server.running(thread)
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
            // Nothing is queued in a chat that has settled on its rows.
            if let waiting = screen.turns.first(where: { if case let .user(message) = $0.body { message.queued != nil } else { false } }) {
                return broke(.queued, "\(name) has settled on its rows and still draws \(waiting.id) as queued")
            }
            let want = server.spoken(in: thread).map { "\($0.role): \($0.content)" }
            let got = drawn.map { "\($0.role): \($0.text)" }
            if got != want { return broke(.fold, "\(name) draws \(got), and the server holds \(want)") }
            // Each under its row's id, but a turn that was streamed in
            // this conversation, which keeps the id the stream gave it: a
            // reply its turn's ordinal, and a message sent under no id the
            // number of its send. A message sent under an id is drawn
            // under it from the press, and so is its row.
            for (turn, row) in zip(drawn, server.spoken(in: thread)) where turn.id != row.id {
                let kept = row.role == "assistant" ? turn.id == "turn-\(row.turn)" : turn.id.hasPrefix("sent-") && row.id.hasPrefix("u")
                if !kept {
                    let what =
                        "\(name) draws the row \(row.id) (\(row.role), turn \(row.turn)) under \(turn.id): it draws \(drawn.map { "\($0.id)=\($0.text)" }) over the rows "
                        + "\(server.rows(of: thread).map { "\($0.id)=\($0.content)" })"
                    // Known: a message sent under no id whose words an older
                    // row of the thread holds too, sent before the rows
                    // were read, is paired with that row. What is drawn is
                    // the server's rows; one bubble's id is the other's.
                    let older =
                        turn.id.hasPrefix("sent-") && row.role == "user"
                        && server.rows(of: thread).contains { $0.id.hasPrefix("u") && $0.content == row.content && $0.turn > row.turn }
                    guard older else { return broke(.fold, what) }
                    if noted[.mispaired] == nil { noted[.mispaired] = broke(.mispaired, what) }
                }
            }
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
                if live.provisional.turns.contains(where: { $0.content == watched }) { places.append("the stream's rows of \(key.rawValue.suffix(2))") }
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
        for card in server.allCards where card.resolutions > 1 {
            return broke(.decidedOnce, "\(card.id) was resolved by \(card.resolutions) writes")
        }
        if server.revision >= 7 {
            for card in server.allCards {
                if card.dismissals > 0, !pressedDismiss.contains(card.id) {
                    return broke(.words, "\(card.id) was asked to be dismissed \(card.dismissals) times, and nobody pressed Dismiss on it")
                }
                if card.state == "replied", card.resumes > 0 {
                    return broke(.words, "\(card.id) was replied to in a message, and its thread was resumed for it \(card.resumes) times")
                }
            }
        }
        // Every action handed to this launch is said over to the device
        // once its time has passed, with room for the app to say so.
        let finished = rig.notifications.calls.compactMap { if case let .finished(response) = $0 { response } else { nil } }
        for act in acts where clock.now.timeIntervalSince(act.at) > ChatApp.actionSeconds + 1 && !finished.contains(act.response) {
            return broke(
                .acted,
                "the action \(act.response.action) of \(act.response.id) was handed over \(Int(clock.now.timeIntervalSince(act.at))) s ago and is not over")
        }
        if let broken = marks(state, presentation) { return broken }
        if let broken = lists(state, presentation, settled: settled) { return broken }
        // The list holds each chat once.
        let listed = state.chats.map(\.id)
        if Set(listed).count != listed.count { return broke(.list, "the list holds a chat twice: \(listed)") }
        return nil
    }

    /// Whether the app holds a thread archived, and pinned, by the person's
    /// mark of it and what this device moved since; nil where it holds no
    /// mark of it.
    private static func flag(_ state: ChatAppState, _ id: String, _ path: KeyPath<MarkChange, Bool?>, _ held: KeyPath<ThreadMark, Bool>) -> Bool? {
        for change in [state.marking[id], state.markSending[id]] {
            if let set = change?[keyPath: path] { return set }
        }
        // A mark the app made up for a thread it just started was never
        // read: the lists say which the chat is.
        return state.marks?[id].flatMap { $0.version > 0 ? $0[keyPath: held] : nil }
    }

    /// The rules of the chat list: what an archive hides and never hides,
    /// what a pin shows, one section a chat, a window that grows with no
    /// chat twice and none lost, and an Undo that puts back what was there.
    private mutating func lists(_ state: ChatAppState, _ presentation: ChatPresentation, settled: Bool) -> Violation? {
        let sections = presentation.list.sections
        let rows = sections.flatMap(\.rows)
        let ids = rows.map(\.id)
        // A chat is in at most one section, and once in it.
        if Set(ids).count != ids.count {
            return broke(.sections, "the list draws a chat twice: \(sections.map { "\($0.title): \($0.rows.map(\.id))" })")
        }
        let shelf = presentation.list.archived?.rows.map(\.id) ?? []
        if Set(shelf).count != shelf.count { return broke(.sections, "the archived list draws a chat twice: \(shelf)") }

        // A flag is written only where the person set it, and never to a
        // server that keeps none.
        for write in server.markWrites where write.pinned != nil || write.archived != nil {
            if !server.keepsFlags { return broke(.flags, "a flag of \(write.thread) was sent to a server that keeps none") }
            let pressed = pressedFlags[write.thread] ?? []
            if let pinned = write.pinned, !pressed.contains(pinned ? "pin" : "unpin") {
                return broke(.flags, "\(write.thread) was written as \(pinned ? "pinned" : "unpinned"), and nobody pressed that")
            }
            if write.archived == true, !pressed.contains("archive") {
                return broke(.flags, "\(write.thread) was written as archived, and nobody archived it")
            }
            if write.archived == false, !pressed.contains("unarchive"), !spokeIn.contains(write.thread) {
                return broke(.flags, "\(write.thread) was written as not archived, and nobody unarchived it or spoke in it")
            }
        }
        guard state.flags else {
            if presentation.list.flags || presentation.list.archived != nil || sections.contains(where: { $0.title == ChatApp.pinnedTitle }) {
                return broke(.flags, "pins or archives are offered on a server that keeps none")
            }
            return settledList(state, presentation, settled: settled)
        }

        // An archived chat is drawn only while it waits; nor counted, since
        // the count is of the rows drawn (the badge's rule); nor notified.
        for row in rows where Self.flag(state, row.id, \.archived, \.archived) == true {
            if (state.thread(row.id)?.waiting ?? 0) == 0 {
                return broke(.archived, "\(row.id) is drawn in the list, and the app holds it archived with nothing waiting")
            }
        }
        if presentation.unread != rows.filter(\.unread).count {
            return broke(.archived, "\(presentation.unread) chats are counted unread, and \(rows.filter(\.unread).count) are drawn so")
        }
        for (id, request) in rig.notifications.pending.sorted(by: { $0.key < $1.key }) where id.hasSuffix("|reply") {
            guard let name = request.thread, Self.flag(state, name, \.archived, \.archived) == true, let thread = state.thread(name),
                thread.waiting == 0
            else { continue }
            // Known, and noted so the rest of the seed is still held to
            // every other rule (`known`); a chat drawn or counted while
            // archived, above, is not.
            if noted[.archived] == nil {
                noted[.archived] = broke(
                    .archived, "a notification of \(name)'s reply stands while the app holds the chat archived with nothing waiting")
            }
        }
        // A pinned chat the app holds the row of is drawn, in the pinned
        // section; and nothing else is drawn there.
        let pinned = Set(sections.first { $0.title == ChatApp.pinnedTitle }?.rows.map(\.id) ?? [])
        for id in state.threadIDs.sorted() where Self.flag(state, id, \.pinned, \.pinned) == true {
            guard let thread = state.thread(id), !thread.automated else { continue }
            let hidden = Self.flag(state, id, \.archived, \.archived) == true && thread.waiting == 0
            if !hidden, !pinned.contains(id) { return broke(.pinned, "\(id) is pinned and not archived, and is not in the pinned section: \(pinned.sorted())") }
        }
        for id in pinned.sorted() where Self.flag(state, id, \.pinned, \.pinned) == false {
            return broke(.pinned, "\(id) is drawn as pinned, and the app holds it not pinned")
        }
        // Whoever spoke in an archived chat follows it again.
        if let spoke = justSpoke, Self.flag(state, spoke, \.archived, \.archived) == true {
            return broke(.archived, "a message was sent in \(spoke), and it is still held archived")
        }
        // Undo puts the chat back: listed again and not archived; and,
        // pressed in the step after the archive, with its thread and what
        // the app holds of its mark as they were (the answer to the
        // archive's own write may have told of a pin made elsewhere), the
        // very row at the very place.
        if let thread = undid, let was = shelved, was.thread == thread {
            guard let row = rows.first(where: { $0.id == thread }) else {
                // Unless the list was read again from its start since, and
                // the chat's place is past the pages read so far: it is
                // there once the list is scrolled to it.
                if let end = state.window.end, let held = state.thread(thread), held.activeAt < end, presentation.list.more {
                    shelved = nil
                    return settledList(state, presentation, settled: settled)
                }
                return broke(
                    .undo,
                    "Undo was pressed on \(thread), and the list does not draw it: \(ids); the window \(state.window) holds \(state.chats.map(\.id)), "
                        + "the archived rows \(state.archive.rows.map(\.id)), its mark \(String(describing: state.marks?[thread])), moved "
                        + "\(String(describing: state.marking[thread] ?? state.markSending[thread])), its row \(String(describing: state.thread(thread)?.record.updatedAt))"
                )
            }
            if row.archived {
                return broke(.undo, "Undo was pressed on \(thread), which was drawn as \(was.row) and is drawn as \(row)")
            }
            let same =
                was.step == steps.count - 1 && state.thread(thread)?.record.version == was.version
                && Self.flag(state, thread, \.pinned, \.pinned) == was.pinned && row.unread == was.row.unread
            // But for when: a row says "now" until something draws the list
            // again.
            var drawn = row
            drawn.stamp = was.row.stamp
            if same, ids == was.rows, drawn != was.row {
                return broke(.undo, "Undo was pressed on \(thread) at once, and its row is \(row), not \(was.row) as it was")
            }
            if same, Set(ids) == Set(was.rows), ids.firstIndex(of: thread) != was.at {
                return broke(
                    .undo,
                    "Undo was pressed on \(thread) at once, and it stands at \(ids.firstIndex(of: thread) ?? -1) of \(ids), not at \(was.at) of \(was.rows)")
            }
            shelved = nil
        }
        // A search's finds: each message under the chat that holds it, and
        // each chat once.
        if let groups = presentation.scenes[scene]?.messages {
            if Set(groups.map(\.id)).count != groups.count { return broke(.found, "a search draws a chat twice: \(groups.map(\.id))") }
            for group in groups {
                let held = Set(server.rows(of: group.id).map(\.id))
                if let stray = group.hits.first(where: { !held.contains($0.id) }) {
                    return broke(.found, "a search draws the message \(stray.id) under \(group.id), which does not hold it")
                }
            }
        }
        return settledList(state, presentation, settled: settled)
    }

    /// Once the server has behaved and the list was read from its start
    /// and scrolled to its end: every chat the server lists is drawn, once,
    /// and no other; the pinned ones in their section; and the app holds
    /// each chat's flags as the server does.
    private mutating func settledList(_ state: ChatAppState, _ presentation: ChatPresentation, settled: Bool) -> Violation? {
        guard settled else { return nil }
        let rows = presentation.list.sections.flatMap(\.rows).map(\.id)
        let want = server.threadIDs.filter { server.mark(of: $0)?.archived != true || (server.keepsMarks && server.waiting($0) > 0) }
        if presentation.list.more {
            return broke(.paging, "the list was scrolled to its end and still says the server's goes on: \(rows)")
        }
        if Set(rows) != Set(want) {
            let odd = Set(rows).symmetricDifference(want).sorted().map { id in
                "\(id): the app holds the mark \(String(describing: state.marks?[id])), moved \(String(describing: state.marking[id] ?? state.markSending[id])), "
                    + "and the server \(String(describing: server.mark(of: id))), waiting \(server.waiting(id))"
            }
            return broke(
                .paging,
                "the list draws \(rows.sorted()), and the server lists \(want.sorted()) (a page is \(page) chats; it holds \(server.threadIDs.count)); \(odd)")
        }
        guard server.keepsFlags else { return nil }
        for id in server.threadIDs {
            let mark = server.mark(of: id)
            if state.thread(id) != nil, state.archived(id) != (mark?.archived == true) || state.isPinned(id) != (mark?.pinned == true) {
                return broke(
                    .flags,
                    "\(id) is held as archived \(state.archived(id)) and pinned \(state.isPinned(id)), and the server holds \(String(describing: mark))")
            }
        }
        // By what the server holds, whether or not the app holds the chat's
        // row: a notification of a chat archived with nothing waiting is
        // taken back however the app came to hear of it.
        for (id, request) in rig.notifications.pending.sorted(by: { $0.key < $1.key }) where id.hasSuffix("|reply") {
            guard let name = request.thread, server.mark(of: name)?.archived == true, server.waiting(name) == 0 else { continue }
            // The same known break, seen once everything has settled.
            if noted[.archived] == nil {
                noted[.archived] = broke(
                    .archived, "a notification of \(name)'s reply stands at the end, and the server holds the chat archived with nothing waiting")
            }
        }
        let pinned = Set(presentation.list.sections.first { $0.title == ChatApp.pinnedTitle }?.rows.map(\.id) ?? [])
        let shown = Set(want.filter { server.mark(of: $0)?.pinned == true })
        if pinned != shown { return broke(.pinned, "the pinned section draws \(pinned.sorted()), and the server's pinned chats are \(shown.sorted())") }
        return nil
    }

    /// The rules of answering, on one conversation as it is drawn: the
    /// messages sent while the assistant works, and the cards.
    private mutating func answering(_ key: ChildKey, _ live: ConversationState, _ screen: ConversationScreen, _ name: String, _ state: ChatAppState)
        -> Violation?
    {
        // A message drawn as queued: one the server admitted into a turn,
        // drawn once, in a conversation a run goes on in, and never one
        // this conversation already drew as read.
        var queued: Set<String> = []
        for turn in screen.turns {
            guard case let .user(message) = turn.body, message.queued != nil else { continue }
            queued.insert(turn.id)
            if !server.wasQueued(turn.id) { return broke(.queued, "\(name) draws \(turn.id) as queued, which the server never took into a turn") }
            if live.phase == .idle || live.phase == .handingOver {
                return broke(.queued, "\(name) draws \(turn.id) as queued while no run goes on in it")
            }
            // Drawn plain between two times it is drawn queued is no
            // fault where the server still holds it unread: a chat that
            // lost its stream says nothing of a queue until it reads one
            // again (seed 215, with the list's steps beside these).
            if drewRead[key]?.contains(turn.id) == true, server.wasTaken(turn.id) {
                // Known (above): noted, and the run goes on under every
                // other rule, this rule's other breaks among them.
                if noted[.queued] == nil { noted[.queued] = broke(.queued, "\(name) draws \(turn.id) as queued again after it drew it as read") }
            }
        }
        for id in live.queued.map(\.id) where screen.turns.filter({ $0.id == id }).count != 1 {
            return broke(.queued, "\(name) holds \(id) as queued and draws it \(screen.turns.filter { $0.id == id }.count) times")
        }
        drewRead[key, default: []].formUnion((drewQueued[key] ?? []).subtracting(queued))
        drewQueued[key, default: []].formUnion(queued)

        // The cards, under the calls the transcript draws.
        for case let .assistant(reply) in screen.turns.map(\.body) {
            for tool in reply.tools {
                guard let reference = tool.asked, let truth = server.card(reference.id) else { continue }
                let id = reference.id
                let drawn = screen.cards[id]
                let stands = tool.attachment != nil && drawn?.stands == true
                let held = state.cards[id]?.record
                if let resolution = tool.resolution, resolution.label.hasPrefix("You "), truth.by != "me" {
                    let whose = truth.by.map { "the server links it to \($0)" } ?? "the server links it to nobody"
                    return broke(.hand, "\(name) says \"\(resolution.label)\" of \(id), and \(whose)")
                }
                if tool.resolution != nil {
                    drewEnded.insert(id)
                    if truth.pending { return broke(.resolved, "\(name) draws \(id) as ended, and it still waits at the server") }
                    if stands { return broke(.resolved, "\(name) draws \(id) as ended and as a card") }
                } else if drawn?.waits == true, stands {
                    if drewEnded.contains(id) { return broke(.resolved, "\(name) draws \(id) waiting again after it drew it as ended") }
                    if !truth.pending, held?.version == truth.version {
                        return broke(.resolved, "\(name) draws \(id) waiting while it holds the record that says it ended")
                    }
                }
                guard truth.pending else { continue }
                // It waits: its card stands, or it is aside for an answer
                // in the person's own words, which the composer says, or
                // which is on its way.
                let aside = drawn?.question?.aside == true
                let said = screen.composer.answering?.id == id || state.cards[id]?.busy == ChatAppState.Card.words
                if !stands, !(aside && said) {
                    return broke(.hidden, "\(name) holds the call that asked \(id), which waits, and draws no card for it (\(tool.statusLabel ?? "no label"))")
                }
            }
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
        // The person goes back to the chats, clears the search, pulls the
        // list to read it from its start and scrolls it to its end.
        showsArchived = false
        query = ""
        undid = nil
        justSpoke = nil
        await rig.harness.send(.showArchived(scene: scene, shown: false))
        await rig.harness.send(.search(scene: scene, query: ""))
        await rig.harness.send(.refresh)
        await drain()
        for _ in 0 ..< 400 {
            guard await rig.harness.presentation.list.more else { break }
            await rig.harness.send(.more(archived: false))
            await drain()
        }
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
