import Foundation
import SubstrateRuntime

/// What each part of the presentation reads of the list's state, as the
/// state with the rest emptied. A part is kept while its reads compare equal
/// (`PresenterParts`), so a field left in only costs a part made again,
/// and a field emptied that the part does read is a screen that stays as it
/// was. So every field is read by every part unless it is emptied here, and
/// each one emptied is one the part's presenter never reads. A test rig
/// makes every kept part again and compares, which holds these lists to
/// what the presenters read.
extension ChatAppState {
    /// Without what no part reads: the engine's own bookkeeping, the
    /// drafts and the unsent messages, which the tree carries from a
    /// conversation and never from here, and the list's own failure, its
    /// feed's reconnecting and the offer to notify, which the tree's top
    /// reads at every presentation and no kept part does.
    private var presented: ChatAppState {
        var state = self
        state.asks = []
        state.restarted = false
        state.drafts = [:]
        state.readEpochs = [:]
        state.reading = [:]
        state.feedBatch = []
        state.feedApplying = nil
        state.notified = []
        state.backgroundRun = nil
        state.waitingRun = nil
        state.disk = nil
        state.refreshing = false
        state.refreshed = false
        state.diagnostics = nil
        state.watching = false
        state.baselined = false
        state.forgetOnSignOut = true
        state.approvalsKnown = false
        state.watchGains = []
        state.leaving = false
        state.recoveryStarted = nil
        state.listFailure = nil
        state.feedReconnecting = false
        state.renaming = [:]
        state.agentsRevision = nil
        state.continueUnserved = false
        state.idsUnserved = false
        state.unsent = [:]
        state.keptUnsent = []
        state.unsaved = nil
        state.marksKnown = false
        state.undo = nil
        state.flagRefused = nil
        state.searches = [:]
        state.searchUnserved = false
        state.seeking = [:]
        state.marksDue = nil
        state.sights = [:]
        state.notificationOffer = nil
        state.announced = []
        state.acting = [:]
        state.acted = []
        state.deferred = []
        state.unkept = [:]
        state.ownReading = []
        state.ownAsked = [:]
        state.wordsSent = [:]
        state.heldCards = [:]
        state.attention = [:]
        state.failedActions = []
        state.cardSaid = [:]
        state.replySaid = []
        return state
    }

    /// Without the person's marks, which the list alone reads: what is
    /// unread is a row's, and a conversation's "New" line is decided when
    /// it is opened and kept on its entry.
    private var unmarked: ChatAppState {
        var state = presented
        state.marks = nil
        state.marking = [:]
        state.markSending = [:]
        state.rowsBehind = []
        return state
    }

    /// As the chat list reads it: the threads, their opening lines, what
    /// was seen and what waits on the owner. Of the owner's choices, whether
    /// automated runs are listed.
    var listed: ChatAppState {
        var state = presented
        state.preferences = ChatPreferences()
        state.preferences.showAutomated = preferences.showAutomated
        state.agents = []
        state.lastAgent = nil
        state.transcripts = [:]
        state.conversations = [:]
        state.scenes = [:]
        state.cards = [:]
        state.guidance = Guidance()
        state.usage = []
        state.permission = nil
        state.route = nil
        state.renameFailure = nil
        state.rowsFailed = [:]
        state.keyProblem = nil
        state.keySaved = Signal()
        state.answered = Signal()
        state.providers = [:]
        state.models = nil
        state.keys = nil
        state.others = [:]
        state.appActive = true
        state.paces = false
        state.olderReading = []
        return state
    }

    /// As one conversation's screen reads it: its thread's row and rows,
    /// the agents and what they run on, the cards and the owner's choices.
    /// Of the transcripts, its own alone, and of the failures, a rename of
    /// its thread and a read of its rows.
    func conversed(_ thread: String?) -> ChatAppState {
        var state = unmarked
        state.transcripts = thread.flatMap { id in transcripts[id].map { [id: $0] } } ?? [:]
        state.renameFailure = renameFailure.flatMap { $0.thread == thread ? $0 : nil }
        state.rowsFailed = thread.flatMap { id in rowsFailed[id].map { [id: $0] } } ?? [:]
        state.olderReading = olderReading.filter { $0 == thread }
        state.conversations = [:]
        state.scenes = [:]
        state.route = nil
        state.guidance = Guidance()
        state.usage = []
        state.seen = [:]
        return state
    }

    /// As Settings reads it: the agents and what they run on, the owner's
    /// choices, their memories and instructions, the storage and the
    /// permission.
    var settled: ChatAppState {
        var state = unmarked
        state.transcripts = [:]
        state.conversations = [:]
        state.scenes = [:]
        state.cards = [:]
        state.route = nil
        state.renameFailure = nil
        state.rowsFailed = [:]
        state.seen = [:]
        state.chats = []
        state.automated = []
        state.others = [:]
        state.openings = [:]
        state.paces = false
        state.shelve()
        return state
    }

    /// As the rest of the app's own part reads it: the session, the route,
    /// what is on screen, the agents offered and the providers without a
    /// key.
    var framed: ChatAppState {
        var state = unmarked
        state.transcripts = [:]
        state.renameFailure = nil
        state.rowsFailed = [:]
        state.cards = [:]
        state.guidance = Guidance()
        state.usage = []
        state.chats = []
        state.automated = []
        state.others = [:]
        state.openings = [:]
        state.seen = [:]
        state.interactions = []
        state.requests = []
        state.approvals = []
        state.paces = false
        state.shelve()
        return state
    }

    /// Without the lists read beside the chats, their windows and the rows
    /// being read above a transcript: what only the list and a conversation
    /// read.
    private mutating func shelve() {
        pinned = []
        waiting = []
        archive = Archive()
        window = ListWindow()
        olderReading = []
    }
}
