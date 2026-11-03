import Foundation
import SubstrateCore

/// What of a message may be recorded: all of it but a body that may carry a
/// secret (docs/plans/app-kit.md §5.4). A body is kept only when what it
/// belongs to is known to carry none, so a reply that races its call's
/// cancel, a second reply, or a request that does not read is recorded
/// without its body. A message that cannot be read is recorded as its length.
public enum Redaction {
    /// What the recorder knows of the call a message answers.
    public enum Call: Sendable, Equatable {
        /// A device call Swift made of this service, still waiting.
        case device(DeviceService)
        /// A `BridgeQuery` Kotlin asked Swift, which carries no secret.
        case query
        /// Nothing waits on the id: the call was answered, cancelled, or never made.
        case unknown
    }

    public static func redact(_ message: String, call: Call) -> String {
        guard var envelope = try? BridgeCodec.decodeEnvelope(message) else {
            return "<a message that is not an envelope, \(message.utf8.count) bytes>"
        }
        guard envelope.body != nil else { return message }
        let label: String?
        switch envelope.op {
        case .request:
            // A request names its own service; one that does not read is not trusted.
            if let request = try? BridgeCodec.decode(DeviceRequest.self, from: envelope.body) {
                label = request.service.carriesSecrets ? request.service.rawValue : nil
            } else if (try? BridgeCodec.decode(BridgeQuery.self, from: envelope.body)) != nil {
                label = nil
            } else {
                label = "unreadable request"
            }
        case .reply, .chunk, .end, .error, .cancel:
            switch (envelope.id, call) {
            // An error for a handle, not a call: Swift's or Kotlin's own words.
            case (nil, _): label = nil
            case let (_, .device(service)): label = service.carriesSecrets ? service.rawValue : nil
            case (_, .query): label = nil
            case (_, .unknown): label = "unknown call"
            }
        case .open, .opened, .command, .presentation, .list, .resync, .close, .closed, .accepted, .environment:
            label = nil
        }
        guard let label else { return message }
        envelope.body = .string("<redacted: \(label)>")
        return (try? BridgeCodec.text(envelope)) ?? "<redacted>"
    }
}
