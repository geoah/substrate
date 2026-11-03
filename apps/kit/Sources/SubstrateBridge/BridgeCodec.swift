import Foundation
import SubstrateCore

/// JSON in and out of the bridge, one configuration for every message.
public enum BridgeCodec {
    /// An envelope whose body is any `Encodable` of the body's shape, so a
    /// presentation is encoded once, straight into the message, rather than
    /// into a `JSONValue` first.
    struct Outgoing<Body: Encodable>: Encodable {
        var v = envelopeVersion
        var op: Op
        var handle: Handle?
        var id: Int?
        var seq: Int?
        var body: Body?
    }

    private struct NoBody: Encodable {}

    public static func encode(op: Op, handle: Handle? = nil, id: Int? = nil, seq: Int? = nil) -> String {
        text(Outgoing<NoBody>(op: op, handle: handle, id: id, seq: seq, body: nil))
    }

    public static func encode(op: Op, handle: Handle? = nil, id: Int? = nil, seq: Int? = nil, body: some Encodable) throws -> String {
        let data = try encoder().encode(Outgoing(op: op, handle: handle, id: id, seq: seq, body: body))
        return String(decoding: data, as: UTF8.self)
    }

    /// An error message; never fails, since a failure is what it reports.
    public static func failure(_ failure: BridgeFailure, handle: Handle? = nil, id: Int? = nil) -> String {
        (try? encode(op: .error, handle: handle, id: id, body: failure))
            ?? #"{"v":1,"op":"error","body":{"code":"swiftFailure","message":"an error could not be encoded"}}"#
    }

    public static func decodeEnvelope(_ text: String) throws(BridgeFailure) -> Envelope {
        do {
            let envelope = try JSONDecoder().decode(Envelope.self, from: Data(text.utf8))
            guard envelope.v == envelopeVersion else {
                throw BridgeFailure(.malformed, "envelope version \(envelope.v), and this bridge speaks \(envelopeVersion)")
            }
            return envelope
        } catch let failure as BridgeFailure {
            throw failure
        } catch {
            throw BridgeFailure(.malformed, "not an envelope: \(describe(error))")
        }
    }

    /// A message's body as the value its op carries.
    public static func decode<T: Decodable>(_ type: T.Type, from body: JSONValue?) throws(BridgeFailure) -> T {
        do {
            let data = try JSONEncoder().encode(body ?? .null)
            return try JSONDecoder().decode(T.self, from: data)
        } catch {
            throw BridgeFailure(.malformed, "a body that is not a \(T.self): \(describe(error))")
        }
    }

    public static func value(_ value: some Encodable) throws -> JSONValue {
        try JSONDecoder().decode(JSONValue.self, from: encoder().encode(value))
    }

    static func encoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        // Key order is not part of the contract, but a stable one makes the
        // fixtures and a recorded message comparable byte for byte.
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        return encoder
    }

    private static func text(_ value: some Encodable) -> String {
        (try? String(decoding: encoder().encode(value), as: UTF8.self)) ?? ""
    }

    /// A decoding error in words that name the field, never the value: a
    /// value may be a secret.
    static func describe(_ error: Error) -> String {
        switch error {
        case let DecodingError.keyNotFound(key, context): "\(path(context.codingPath + [key])) is missing"
        case let DecodingError.typeMismatch(_, context), let DecodingError.valueNotFound(_, context):
            "\(path(context.codingPath)) has the wrong type"
        case let DecodingError.dataCorrupted(context): "\(path(context.codingPath)) is not readable"
        default: "\(type(of: error))"
        }
    }

    private static func path(_ keys: [any CodingKey]) -> String {
        keys.isEmpty ? "the value" : keys.map { $0.intValue.map { "[\($0)]" } ?? $0.stringValue }.joined(separator: ".")
    }
}
