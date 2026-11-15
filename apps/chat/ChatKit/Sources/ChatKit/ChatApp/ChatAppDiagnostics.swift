import Foundation
import SubstrateDevice
import SubstrateRuntime

/// What the app says of itself in the diagnostics file (`Diagnostic`): each
/// line a pure reading of one step, the event it applied and what it asked
/// for, written by the one effect that follows (`ChatAppAsk.diagnose`). The
/// list writes for its conversations too: a conversation's lines ride its
/// report to the list, so the file has one writer and one order.
extension ChatApp {
    /// The lines of one step of the list: what the event was, and what the
    /// step asked the device to do.
    func diagnostics(of event: Event, asks: [ChatAppAsk], on state: State, from previous: State) -> [Diagnostic] {
        var lines: [Diagnostic] = []
        switch event.step {
        case let .diskOpened(disk):
            lines.append(
                Diagnostic("launch").with("platform", platform).with("folder_read", disk.state != nil).with("unsent", disk.unsent.count))
        case let .featuresRead(agents, records):
            // Said when it moves: the document is read with every refresh.
            if previous.agentsRevision != agents || previous.recordsRevision != state.recordsRevision || previous.flags != state.flags {
                lines.append(Diagnostic("features").with("agents", agents).with("records", records).with("flags", state.flags))
            }
        case let .refreshFailed(failure), let .readFailed(failure), let .keyFailed(failure):
            lines.append(Diagnostic("failure").with(failure))
        case let .rowsFailed(thread, _, failure), let .renameFailed(thread, _, failure):
            lines.append(Diagnostic("failure", chat: thread).with(failure))
        case let .cardFailed(_, failure), let .cardWriteFailed(_, failure): lines.append(Diagnostic("failure").with(failure))
        case let .pageFailed(_, _, failure, compacted): lines.append(Diagnostic("failure").with(failure).with("compacted", compacted))
        case let .searchFailed(_, _, failure, unserved): lines.append(Diagnostic("failure").with(failure).with("unserved", unserved))
        case let .guidance(.readFailed(failure)), let .guidance(.writeFailed(_, failure, _)): lines.append(Diagnostic("failure").with(failure))
        case let .feedApplied(feed):
            if let failure = feed.failure { lines.append(Diagnostic("failure").with(failure)) }
        case let .watchOpened(fresh, kinds): lines.append(Diagnostic("feed opened").with("fresh", fresh).with("kinds", kinds.count))
        case .watchRelist: lines.append(Diagnostic("feed compacted"))
        case let .watchInterrupted(dropped): lines.append(Diagnostic(dropped ? "feed dropped" : "feed back"))
        case let .watchEnded(_, failure): lines.append(Diagnostic("feed ended").with(failure))
        case let .markWritten(thread, sent, _): lines.append(Self.mark("mark written", thread, sent))
        case let .markWriteFailed(thread, sent, refusal, failure):
            lines.append(Self.mark("mark not written", thread, sent).with("then", refusal).with(failure))
        case let .unsentSaved(ids, failed): lines.append(Diagnostic("unsent saved").with("messages", ids.count).with("failed", failed != nil))
        case let .backgroundExpired(id):
            lines.append(Diagnostic("background expired").with("refresh", id == Self.refreshJob))
        case let .phase(phase): lines.append(Diagnostic("app").with("phase", phase))
        case let .followEnded(key, why): lines.append(Diagnostic("follow stopped", chat: state.conversations[key]?.thread ?? key.rawValue).with("by", why))
        case let .conversation(_, report): lines += report.notes
        default: break
        }
        for ask in asks {
            switch ask {
            case let .writeMark(thread, change): lines.append(Self.mark("mark write", thread, change))
            case let .notify(request): lines.append(Self.notification("notification posted", request))
            case let .notifyHeld(request, seconds): lines.append(Self.notification("notification held", request).with("seconds", seconds: seconds))
            case let .notifyCard(request, _): lines.append(Self.notification("notification posted", request))
            case let .cardHold(_, seconds): lines.append(Diagnostic("notification held").with("of", word: "card").with("seconds", seconds: seconds))
            case let .cancelGroup(thread): lines.append(Diagnostic("notifications taken back", chat: thread))
            case let .cancelNotification(id): lines.append(Self.taken(id))
            case .cancelAllNotifications: lines.append(Diagnostic("notifications taken back").with("all", true))
            case let .finishAction(_, cancel, notify, _):
                lines.append(Diagnostic("notification action over").with("done", cancel != nil).with("said", notify != nil))
            case let .refresh(_, held, beside): lines.append(Diagnostic("list read").with("held", held.count).with("beside", beside.count))
            case let .read(thread, first, _, _): lines.append(Diagnostic("rows read", chat: thread).with("first", first))
            case let .readOlder(thread, _, _): lines.append(Diagnostic("older rows read", chat: thread))
            case .timings: break
            default: break
            }
        }
        return lines
    }

    /// A mark's write as what it carries, never the message it reads to.
    private static func mark(_ what: StaticString, _ thread: String, _ change: MarkChange) -> Diagnostic {
        Diagnostic(what, chat: thread).with("reads", change.readThrough != nil ? true : nil).with("unread", change.markedUnread)
            .with("pinned", change.pinned).with("archived", change.archived).with("held", change.held ? true : nil)
    }

    /// What a notification is of, from its id's own last word.
    private static func of(_ id: String) -> String {
        if id.hasSuffix("|reply") { return "reply" }
        if id.contains("|card|") || id.hasPrefix("card|") { return "card" }
        if id.hasSuffix("|failed") { return "failed" }
        return "other"
    }

    private static let kinds = ["reply", "card", "failed", "other"]

    private static func notification(_ what: StaticString, _ request: NotificationRequest) -> Diagnostic {
        Diagnostic(what, chat: request.thread).with("of", said: of(request.id), among: kinds).with("answers", request.category != nil)
    }

    private static func taken(_ id: String) -> Diagnostic {
        Diagnostic("notification taken back", chat: ChatNotifications.thread(of: id)).with("of", said: of(id), among: kinds)
    }

    /// What the kept points of the process's meter cost since they were
    /// last taken: how long a flush, a parse and the main thread's half of
    /// a publish took.
    static func timings(_ taken: [Meter.Entry]) -> [Diagnostic] {
        taken.filter { $0.stat.count > 0 }.map { entry in
            Diagnostic("timing").with("of", said: entry.point, among: ["flush", "parse", "republish"]).with("count", entry.stat.count)
                .with("mean_ms", milliseconds: entry.stat.milliseconds / Double(entry.stat.count))
                .with("longest_ms", milliseconds: Double(entry.stat.longest) / 1_000_000)
        }
    }
}

extension Conversation {
    /// What a step of a conversation says of itself: which stream opened,
    /// dropped and why, a send's copies and repeats, each failure as the
    /// value it is. Read from the event and from what the step asked for,
    /// which the state holds until the next.
    static func notes(of event: Event, after state: State, key: ChildKey) -> [Diagnostic] {
        func line(_ what: StaticString) -> Diagnostic { Diagnostic(what, chat: state.thread ?? key.rawValue, formerly: key.rawValue) }
        var lines: [Diagnostic] = []
        var run: Diagnostic? {
            switch state.run {
            case let .send(run)?: line("run").with("of", word: run.resumes ? "continue" : "send").with("joins", run.joins)
            case let .attach(run)?: line("run").with("of", word: "attach").with("attempt", run.attempt).with("probe", run.probe ? true : nil)
            case nil: nil
            }
        }
        switch event.step {
        case let .sent(_, _, _, baseline, fromDraft, id):
            guard case let .send(run)? = state.run else { break }
            lines.append(line("send").with("id", id != nil).with("joins", run.joins).with("typed", fromDraft).with("placed", baseline != nil))
        case .sentAgain: lines.append(line("send again"))
        case .tookBack: lines.append(line("message taken back"))
        case .repeatDue:
            if state.standing { lines.append(line("message stands").with(state.unsentFailure)) }
        case let .found(_, thread, _): lines.append(line("message looked for").with("found", thread != nil))
        case let .findFailed(_, failure, _): lines.append(line("failure").with(failure))
        case .continued: lines.append(line("continue"))
        case .named: lines.append(line("stream opened").with("phase", state.phase))
        case let .streamed(streamed, _):
            switch streamed.kind {
            case .done:
                lines.append(
                    line("stream done").with("status", said: streamed.result?.status, among: Diagnostic.Known.statuses)
                        .with("cause", said: streamed.result?.cause, among: Diagnostic.Known.causes))
            case .idle: lines.append(line("stream idle").with("status", said: streamed.status, among: Diagnostic.Known.statuses))
            default: break
            }
        case let .streamEnded(failure, _):
            lines.append(line("stream ended").with("phase", state.phase).with("standing", state.standing ? true : nil).with(failure))
        case let .attachRead(thread, rows): lines.append(line("attach read").with("rows", rows.count).with("gone", thread == nil ? true : nil))
        case let .attachReadFailed(failure): lines.append(line("failure").with(failure))
        case let .retryDue(attempt): lines.append(line("stream retry").with("attempt", attempt))
        case let .handedOver(_, rows): lines.append(line("handover").with("rows", rows.count).with("settles", state.settles))
        case let .handoverFailed(failure), let .pollFailed(failure), let .stopFailed(failure): lines.append(line("failure").with(failure))
        case .stopAsked: lines.append(line("stop").with("stopping", state.stopping))
        case let .checked(_, found, failure): lines.append(line("delivery checked").with("found", found != nil).with(failure))
        case .resumed: lines.append(line("front again").with("phase", state.phase))
        default: break
        }
        for ask in state.asks {
            switch ask {
            case let .chat(_, _, _, id, repliesTo):
                lines.append(
                    line("send out").with("copy", id == nil ? nil : state.copies).with("try", state.tries).with("answers", repliesTo != nil ? true : nil))
            case let .repeatWait(_, seconds): lines.append(line("send repeat").with("seconds", seconds: seconds).with("try", state.tries))
            case .resume: lines.append(line("continue sent"))
            case .attach: if let run { lines.append(run) }
            case let .wait(attempt, seconds): lines.append(line("stream wait").with("attempt", attempt).with("seconds", seconds: seconds))
            case .handover: lines.append(line("handover read"))
            case .stop: lines.append(line("stop sent"))
            case .check: lines.append(line("delivery check"))
            case .find: lines.append(line("message look"))
            default: break
            }
        }
        return lines
    }
}
