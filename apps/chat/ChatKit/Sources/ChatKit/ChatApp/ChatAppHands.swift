import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateRuntime

/// Whose hand ended a card (`agents` 7, decision 0327).
///
/// A resolved card links the person who resolved it: `resolvedBy` on a
/// question, `decidedBy` on an approval and on a suggestion. The app never
/// reads actors to tell its own person's from another's: it asks the
/// server which resolved cards of a thread are `me`'s, one list read per
/// kind, and concludes from that alone. A card in the answer is the
/// person's. One with a link that a whole answer does not hold is somebody
/// else's. One with no link (resolved before the revision, or decided by a
/// policy's rule) says only how it ended, and so does every card below the
/// revision, and every card past a first page of the person's own.
public enum Hand: String, Codable, Hashable, Sendable {
    /// The person the app is signed in as.
    case you
    /// Another person of the repository.
    case other
    /// Not said: no link, or nothing read that tells.
    case unsaid
}

extension ChatAppState {
    /// Whose hand ended the card `id`, by what was read of it.
    func hand(_ id: String, record: Record?) -> Hand {
        if ownCards.contains(id) { return .you }
        guard let record, record["resolvedBy"] != nil || record["decidedBy"] != nil else { return .unsaid }
        return othersCards.contains(id) ? .other : .unsaid
    }
}

extension ChatApp {
    /// How many of the person's own resolutions one read of a kind asks
    /// for: an answer that long may not be the whole of them, and nothing
    /// is concluded from it about a card it does not hold.
    static let ownPage = 100

    /// The cards of a thread that ended, as far as the device knows: the
    /// ones its rows say ended, and the ones read as records.
    func endedCards(_ state: State, _ thread: String) -> Set<String> {
        var out: Set<String> = []
        for turn in state.transcripts[thread]?.turns ?? [] where turn.role == .system {
            if let notice = interactionNotice(turn) { out.insert(notice.interactionID) }
            if let notice = approvalNotice(turn) { out.insert(notice.approvalID) }
            if let notice = decisionNotice(turn) { out.insert(notice.requestID) }
        }
        for (id, card) in state.cards {
            guard let record = card.record, record["thread"]?.referenceID == thread, !Self.waits(record) else { continue }
            out.insert(id)
        }
        return out
    }

    /// A thread's rows or one of its cards was read: where a card of it
    /// ended that the last read of the person's own did not cover, that
    /// read is made again. Only where the server links who resolved a card.
    func askOwn(_ state: inout State, _ thread: String?) {
        guard let thread, (state.agentsRevision ?? 0) >= Conversation.wordsRevision, !state.ownReading.contains(thread) else { return }
        let ended = endedCards(state, thread)
        guard !ended.isSubset(of: state.ownAsked[thread] ?? []) else { return }
        state.ownReading.insert(thread)
        state.asks.append(.readOwn(thread: thread, cards: ended.sorted()))
    }

    /// The person's own resolved cards of a thread, as the server answered.
    ///
    /// - Parameter whole: every read was answered with less than a page:
    ///   a card asked about that is not among `mine` is not the person's.
    func ownRead(_ state: inout State, thread: String, asked: [String], mine: [String], whole: Bool) {
        state.ownReading.remove(thread)
        state.ownAsked[thread] = Set(asked)
        state.ownCards.formUnion(mine)
        if whole { state.othersCards.formUnion(Set(asked).subtracting(mine)) }
        state.othersCards.subtract(state.ownCards)
        // A card that ended while the read was out is asked about now.
        askOwn(&state, thread)
    }

    /// A card this device resolved itself, by a write the server took over
    /// the version that waited: the person's own hand, with no read needed
    /// to say so.
    func resolvedHere(_ state: inout State, _ id: String, _ record: Record) {
        guard record["resolvedBy"] != nil || record["decidedBy"] != nil, !Self.waits(record) else { return }
        state.ownCards.insert(id)
        state.othersCards.remove(id)
    }

    /// The person's own resolved cards of a thread: one list read per
    /// kind, with `me` in the filter.
    static func own(_ client: SubstrateClient, thread: String) async -> (mine: [String], whole: Bool)? {
        var mine: [String] = []
        var whole = true
        var answered = false
        for (kind, link) in [(Kinds.interaction, "resolvedBy"), (Kinds.approval, "decidedBy"), (Kinds.request, "decidedBy")] {
            do {
                let page = try await client.list(
                    ListQuery(kinds: [kind], properties: ["thread": ["eq": .string(thread)], link: ["eq": "me"]], first: ownPage))
                mine += page.records.map(\.id)
                if page.records.count >= ownPage { whole = false }
                answered = true
            } catch {
                // A kind the token does not reach, or a read that failed:
                // nothing is concluded about the cards of that kind.
                whole = false
            }
        }
        return answered ? (mine, whole) : nil
    }
}
