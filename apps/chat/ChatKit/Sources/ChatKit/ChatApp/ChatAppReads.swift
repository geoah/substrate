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
    /// Without what no part reads: the engine's own bookkeeping, and the
    /// drafts, which the tree carries from a conversation and never from
    /// here.
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
        state.watching = false
        state.baselined = false
        state.forgetOnSignOut = true
        state.approvalsKnown = false
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
        state.renameProblem = nil
        state.keyProblem = nil
        state.keySaved = Signal()
        state.answered = Signal()
        state.providers = [:]
        state.models = nil
        state.keys = nil
        state.others = [:]
        state.appActive = true
        return state
    }

    /// As one conversation's screen reads it: its thread's row and rows,
    /// the agents and what they run on, the cards and the owner's choices.
    /// Of the transcripts, its own alone.
    func conversed(_ thread: String?) -> ChatAppState {
        var state = presented
        state.transcripts = thread.flatMap { id in transcripts[id].map { [id: $0] } } ?? [:]
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
        var state = presented
        state.transcripts = [:]
        state.conversations = [:]
        state.scenes = [:]
        state.cards = [:]
        state.route = nil
        state.seen = [:]
        state.chats = []
        state.automated = []
        state.others = [:]
        state.openings = [:]
        return state
    }

    /// As the rest of the app's own part reads it: the session, the route,
    /// what is on screen, the agents offered and the providers without a
    /// key.
    var framed: ChatAppState {
        var state = presented
        state.transcripts = [:]
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
        return state
    }
}
