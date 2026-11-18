import Foundation
import SubstrateAPI
import SubstrateDevice

/// Which session the app holds, as its link says, and how many times that
/// changed: every sign-in, sign-out and switch of server, repository or
/// token moves it, and nothing else does. The runtime stamps each effect
/// with the epoch it starts under, and what the effect asks of a server is
/// of that epoch's session alone (`ClientSource.pinned`): once the epoch has
/// moved, no request of it leaves, and what it answers is dropped.
final class SessionEpoch: @unchecked Sendable {
    /// One session, as a link names it; none while signed out. An ended
    /// access is the same session.
    struct Key: Hashable, Sendable {
        var server: URL
        var repository: String
        var tokenID: String?

        init?(_ link: LinkSnapshot?) {
            guard let link, link.phase != .signedOut, let server = link.server, let repository = link.repository else { return nil }
            self.server = server
            self.repository = repository
            tokenID = link.tokenID
        }
    }

    /// The epoch an effect starts under.
    struct Stamp: Sendable {
        var number: Int
        var key: Key?
    }

    private let lock = NSLock()
    private var number = 0
    private var key: Key?

    /// The link moved: a session other than the one held starts a new epoch.
    func observe(_ link: LinkSnapshot?) {
        let next = Key(link)
        lock.withLock {
            guard next != key else { return }
            key = next
            number += 1
        }
    }

    var stamp: Stamp { lock.withLock { Stamp(number: number, key: key) } }

    func holds(_ number: Int) -> Bool { lock.withLock { self.number == number } }
}

/// Whether an effect asked for a client: its results are of that client's
/// session, and are dropped once the epoch it started under has moved.
final class EffectUse: @unchecked Sendable {
    private let lock = NSLock()
    private var asked = false

    func mark() { lock.withLock { asked = true } }
    var marked: Bool { lock.withLock { asked } }
}

/// A transport that sends a request only while the session epoch it was
/// vended under holds, checked as each request leaves.
struct EpochTransport: HTTPTransport {
    let inner: any HTTPTransport
    let epochs: SessionEpoch
    let number: Int

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        guard epochs.holds(number) else { throw CancellationError() }
        return try await inner.data(for: request)
    }

    func lines(for request: URLRequest) async throws -> LineStream {
        guard epochs.holds(number) else { throw CancellationError() }
        return try await inner.lines(for: request)
    }
}

extension ClientSource {
    /// This source held to the session of `stamp`: a client only while that
    /// session is the one held (its epoch, and its token where the source
    /// names it), and every request the client sends checked again as it
    /// leaves. Once the session changed: none, and nothing is sent.
    func pinned(to stamp: SessionEpoch.Stamp, of epochs: SessionEpoch, use: EffectUse) -> ClientSource {
        let base = self
        @Sendable func hold(_ client: SubstrateClient) -> SubstrateClient? {
            guard epochs.holds(stamp.number) else { return nil }
            var held = client
            held.transport = EpochTransport(inner: client.transport, epochs: epochs, number: stamp.number)
            return held
        }
        return ClientSource(
            sessions: {
                use.mark()
                guard epochs.holds(stamp.number), let vended = await base.vended(), epochs.holds(stamp.number) else { return nil }
                // A source that names the token: the client is the stamped
                // session's, or none. A switch the link has not told yet
                // reads another token here.
                if let token = vended.tokenID, token != stamp.key?.tokenID { return nil }
                return hold(vended.client).map { ClientSource.Vended(client: $0, tokenID: vended.tokenID) }
            },
            holding: { client in
                use.mark()
                return hold(client)
            })
    }
}
