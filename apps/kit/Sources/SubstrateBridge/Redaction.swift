import Foundation
import SubstrateCore

/// What of a message may be recorded: all of it, but the body of a message
/// of a service that carries secrets (`DeviceService.carriesSecrets`). A
/// message that cannot be read is recorded as its length alone, since what
/// it holds is unknown.
public enum Redaction {
    /// `service` is the call's, where the caller knows it (a reply knows its
    /// request's); a request names its own.
    public static func redact(_ message: String, service: DeviceService?) -> String {
        guard var envelope = try? BridgeCodec.decodeEnvelope(message) else {
            return "<a message that is not an envelope, \(message.utf8.count) bytes>"
        }
        var service = service
        if envelope.op == .request, let request = try? BridgeCodec.decode(DeviceRequest.self, from: envelope.body) {
            service = request.service
        }
        guard let service, service.carriesSecrets, envelope.body != nil else { return message }
        envelope.body = .string("<redacted: \(service.rawValue)>")
        return (try? String(decoding: BridgeCodec.encoder().encode(envelope), as: UTF8.self)) ?? "<redacted>"
    }
}
