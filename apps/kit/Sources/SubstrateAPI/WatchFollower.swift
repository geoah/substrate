import Foundation
import SubstrateCore
import SubstrateDevice

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// What following the watch reports.
public enum WatchEvent: Equatable, Sendable {
    /// The stream opened after this point: the cursor to keep.
    case opened(WatchCursor)
    /// One change, after which the cursor is the row's seq.
    case row(WatchRow)
    /// The cursor addresses no history the server holds (a 410, or a
    /// `compacted` frame): read everything again and follow from the new
    /// head. The follow ends here.
    case relist
    /// The connection ended; it opens again from the last cursor after a
    /// pause, with these words for a status line meanwhile.
    case interrupted(String)
    /// The follow ended for good: the server's terminal frame, or a refusal
    /// no retry fixes.
    case ended(WatchEnd)
}

/// Why a follow ended for good.
public enum WatchEnd: Equatable, Sendable {
    /// The token was refused: sign in again.
    case signedOut
    /// The token does not reach the kinds asked for.
    case needsApproval
    /// The server's words for anything else (a terminal frame, a 4xx, a
    /// redirect it would not follow).
    case refused(String)
}

/// Follows the watch (`GET /api/v1/changes?watch=1`) over a set of kinds:
/// from a cursor and its generation, or from the live head; through
/// heartbeats; again from the last cursor when the connection drops; and to
/// a relist when the cursor no longer addresses the history. Generalised
/// from Chat's feed.
public struct WatchFollower: Sendable {
    public var client: SubstrateClient
    public var kinds: [String]
    /// The app's name, as the words for an interruption say it.
    public var app: String
    /// How long to wait before opening again after an interruption, by
    /// attempt; the last repeats.
    public var pauses: [Duration]
    /// How a pause waits; a test replaces it so nothing sleeps.
    public var pause: @Sendable (Duration) async throws -> Void

    public init(
        client: SubstrateClient, kinds: [String], app: String, pauses: [Duration] = [.seconds(1), .seconds(3), .seconds(8), .seconds(15)],
        pause: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }
    ) {
        self.client = client
        self.kinds = kinds
        self.app = app
        self.pauses = pauses
        self.pause = pause
    }

    /// One connection's request: a positive `from` must name the generation
    /// it was read under, which the server holds it to.
    public func request(from cursor: WatchCursor?) throws -> URLRequest {
        var query = kinds.map { ("kinds", $0) }
        query.append(("watch", "1"))
        if let cursor {
            query.append(("from", String(cursor.seq)))
            if !cursor.generation.isEmpty { query.append(("generation", cursor.generation)) }
        }
        return try client.request("GET", "api/v1/changes", query: query, accept: "application/x-ndjson", timeout: 75)
    }

    /// The watch from `cursor` (nil: the live head) until it ends, relists
    /// or the reader stops reading.
    public func follow(from cursor: WatchCursor?) -> AsyncStream<WatchEvent> {
        AsyncStream { continuation in
            let task = Task {
                var cursor = cursor
                var failures = 0
                while !Task.isCancelled {
                    let outcome = await connect(from: &cursor, continuation)
                    switch outcome {
                    case .finished:
                        continuation.finish()
                        return
                    case let .interrupted(words, progressed):
                        if progressed { failures = 0 }
                        continuation.yield(.interrupted(words))
                        let wait = pauses.isEmpty ? .seconds(3) : pauses[min(failures, pauses.count - 1)]
                        failures += 1
                        do { try await pause(wait) } catch { break }
                    }
                }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private enum Outcome {
        case finished
        /// The connection ended without a verdict; `progressed` when it
        /// delivered something first.
        case interrupted(String, progressed: Bool)
    }

    private func connect(from cursor: inout WatchCursor?, _ continuation: AsyncStream<WatchEvent>.Continuation) async -> Outcome {
        var progressed = false
        do {
            let stream = try await client.transport.lines(for: request(from: cursor))
            let status = stream.head.statusCode
            if status != 200 {
                let body = try await stream.body(limit: 64 * 1024)
                let refusal = SubstrateClient.failure(SubstrateClient.Response(data: body, response: stream.head))
                if status == 410 {
                    continuation.yield(.relist)
                    return .finished
                }
                if let end = Self.end(for: refusal, app: app) {
                    continuation.yield(.ended(end))
                    return .finished
                }
                return .interrupted(explain(refusal, app: app, server: client.server), progressed: false)
            }
            for try await line in stream.lines {
                guard let parsed = WatchFrame.parse(line) else { continue }
                progressed = true
                switch parsed {
                case let .bookmark(opened):
                    // The bookmark's generation is the one to resume under,
                    // and the live head where nothing was read before.
                    let kept = cursor.map { WatchCursor(seq: $0.seq, generation: opened.generation.isEmpty ? $0.generation : opened.generation) }
                    cursor = kept ?? opened
                    continuation.yield(.opened(cursor ?? opened))
                case let .row(row):
                    cursor = WatchCursor(seq: row.seq, generation: cursor?.generation ?? "")
                    continuation.yield(.row(row))
                case let .failed(code, message):
                    // `auth` is the stream's token ending (revoked, or the
                    // person signed out everywhere): sign in again.
                    switch code {
                    case "compacted": continuation.yield(.relist)
                    case "auth": continuation.yield(.ended(.signedOut))
                    default: continuation.yield(.ended(.refused(message)))
                    }
                    return .finished
                }
            }
            return .interrupted("The watch closed; it opens again shortly.", progressed: progressed)
        } catch {
            if Task.isCancelled || isCancellation(error) { return .finished }
            return .interrupted(explain(error, app: app, server: client.server), progressed: progressed)
        }
    }

    /// How far apart two recoveries from `compacted` (a relist, and the watch
    /// opened again from it) must start: one recovery a minute. A refusal
    /// within this of the start of the last recovery is a failure, shown as
    /// one, so a server that keeps refusing never makes a loop. Only the
    /// minute passing makes the allowance whole again: a bookmark, a list
    /// that lands or a row the watch delivers does not.
    public static let recoveryInterval: TimeInterval = 60

    /// Whether a `compacted` refusal met at `now` starts a recovery, the last
    /// one having started at `last` (nil: none yet). `now` is the app's
    /// clock (`AppEnvironment.clock`), so a test moves it.
    public static func recovers(at now: Date, after last: Date?) -> Bool {
        last.map { now.timeIntervalSince($0) >= recoveryInterval } ?? true
    }

    /// A refusal no retry fixes, as the end it means; nil for one worth
    /// trying again (a busy or failing server).
    static func end(for refusal: any Error, app: String) -> WatchEnd? {
        switch refusal {
        case is SessionExpired: return .signedOut
        case is InsufficientScope: return .needsApproval
        case let redirect as Redirected: return .refused(explain(redirect, app: app, server: nil))
        case let server as ServerError where server.status < 500 && server.status != 429: return .refused(server.text)
        default: return nil
        }
    }
}
