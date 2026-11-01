import Foundation
import SubstrateKit

/// One committed change, as much of it as a chat reads: which record of
/// which kind moved, and its position in the history.
struct ChangeRow: Sendable, Equatable {
    var seq: Int
    var kind: String
    var recordID: String
    var op: String
}

/// One line of the change feed's tail.
enum WatchLine: Sendable, Equatable {
    /// The opening control frame: the head the stream starts after, and the
    /// history generation every later resume must name beside a seq.
    case bookmark(seq: Int, generation: String)
    case row(ChangeRow)
    /// The reserved terminal frame: a mid-stream failure. Not reconnected.
    case failed(code: String, message: String)

    /// A line, or nil for a blank, the `{}` heartbeat or garbage.
    static func parse(_ line: String) -> WatchLine? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty, let data = trimmed.data(using: .utf8),
              let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return nil }
        if let seq = object["seq"] as? Int, object["ts"] is String {
            return .row(ChangeRow(seq: seq, kind: object["kind"] as? String ?? "",
                                  recordID: object["recordId"] as? String ?? "", op: object["op"] as? String ?? ""))
        }
        if let seq = object["bookmark"] as? Int {
            return .bookmark(seq: seq, generation: object["generation"] as? String ?? "")
        }
        if let error = object["error"] as? [String: Any] {
            return .failed(code: error["code"] as? String ?? "internal",
                           message: error["message"] as? String ?? "The change feed stopped.")
        }
        return nil
    }
}

/// The resume cursor no longer addresses this history: below the retention
/// horizon, above the head, or from a generation the server does not hold.
/// Re-listing from a fresh head is the only recovery.
struct FeedCompacted: LocalizedError, Sendable {
    let text: String
    var errorDescription: String? { text }
}

extension API {
    /// One connection to the change feed's tail, filtered to `kinds`. A
    /// positive `from` must name the generation it was read under; the server
    /// refuses it otherwise.
    func watch(kinds: [String], from: Int?, generation: String?) -> AsyncThrowingStream<WatchLine, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    var query = kinds.map { ("kinds", $0) }
                    query.append(("watch", "1"))
                    if let from {
                        query.append(("from", String(from)))
                        if let generation, !generation.isEmpty { query.append(("generation", generation)) }
                    }
                    let req = try request("GET", "api/v1/changes", query: query, accept: "application/x-ndjson", timeout: 75)
                    let (bytes, response) = try await Self.streaming.bytes(for: req)
                    let status = (response as? HTTPURLResponse)?.statusCode ?? 0
                    if status != 200 {
                        var data = Data()
                        for try await byte in bytes {
                            data.append(byte)
                            if data.count > 64 * 1024 { break }
                        }
                        if status == 401 { throw SessionExpired() }
                        let envelope = ServerError(status: status, envelope: data)
                        if status == 410 { throw FeedCompacted(text: envelope?.text ?? "The change feed's cursor expired.") }
                        throw envelope ?? TransportError(text: "The change feed answered HTTP \(status).")
                    }
                    for try await line in bytes.lines {
                        guard let parsed = WatchLine.parse(line) else { continue }
                        continuation.yield(parsed)
                        if case .failed = parsed { break }
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: Task.isCancelled ? CancellationError() : error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}
