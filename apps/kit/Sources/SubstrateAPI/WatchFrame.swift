import Foundation
import SubstrateCore

/// One line of the watch's ndjson: a frame.
public enum WatchFrame: Equatable, Sendable {
    /// The opening control frame: the head the stream starts after, and the
    /// history generation every later resume must name beside a seq.
    case bookmark(WatchCursor)
    case row(WatchRow)
    /// The reserved terminal frame: the stream failed mid-way. `compacted`
    /// means its cursor no longer addresses this history.
    case failed(code: String, message: String)

    /// A line, or nil for a blank line, the `{}` heartbeat or anything else.
    public static func parse(_ line: String) -> WatchFrame? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty, let value = try? JSONDecoder().decode(JSONValue.self, from: Data(trimmed.utf8)),
              case let .object(object) = value
        else { return nil }
        if case let .number(seq)? = object["seq"], case .string? = object["ts"] {
            return .row(WatchRow(seq: (seq.int ?? 0), kind: object["kind"]?.stringValue ?? "",
                                 recordID: object["recordId"]?.stringValue ?? "", op: object["op"]?.stringValue ?? ""))
        }
        if case let .number(seq)? = object["bookmark"] {
            return .bookmark(WatchCursor(seq: (seq.int ?? 0), generation: object["generation"]?.stringValue ?? ""))
        }
        if case let .object(error)? = object["error"] {
            return .failed(code: error["code"]?.stringValue ?? "internal", message: error["message"]?.stringValue ?? "The watch stopped.")
        }
        return nil
    }
}

private extension JSONValue {
    var stringValue: String? {
        if case let .string(text) = self { return text }
        return nil
    }
}
