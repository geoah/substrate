import Foundation
import SubstrateCore

/// JSON in and out of the bridge, one configuration for every message.
public enum BridgeCodec {
    /// An envelope whose body is any `Encodable` of the body's shape.
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
        (try? text(Outgoing<NoBody>(op: op, handle: handle, id: id, seq: seq, body: nil))) ?? ""
    }

    public static func encode(op: Op, handle: Handle? = nil, id: Int? = nil, seq: Int? = nil, body: some Encodable) throws -> String {
        try text(Outgoing(op: op, handle: handle, id: id, seq: seq, body: body))
    }

    /// A message whose body is JSON encoded already, spliced in as it is
    /// rather than decoded and encoded again.
    public static func encode(op: Op, handle: Handle? = nil, id: Int? = nil, raw body: Data) -> String {
        let head = encode(op: op, handle: handle, id: id)
        return String(head.dropLast()) + ",\"body\":" + String(decoding: body, as: UTF8.self) + "}"
    }

    /// An error message; never fails, since a failure is what it reports.
    public static func failure(_ failure: BridgeFailure, handle: Handle? = nil, id: Int? = nil) -> String {
        (try? encode(op: .error, handle: handle, id: id, body: failure))
            ?? #"{"v":1,"op":"error","body":{"code":"swiftFailure","message":"an error could not be encoded"}}"#
    }

    /// Read with `JSONValue(data:)` and `ValueCoding`, never `JSONDecoder`,
    /// so a body keeps every digit of every number and its keys' order.
    public static func decodeEnvelope(_ text: String) throws(BridgeFailure) -> Envelope {
        let envelope: Envelope
        do {
            envelope = try ValueCoding.decode(Envelope.self, from: JSONValue(data: Data(text.utf8)))
        } catch {
            throw BridgeFailure(.malformed, "not an envelope: \(describe(error))")
        }
        guard envelope.v == envelopeVersion else {
            throw BridgeFailure(.malformed, "envelope version \(envelope.v), and this bridge speaks \(envelopeVersion)")
        }
        return envelope
    }

    /// A message's body as the value its op carries.
    public static func decode<T: Decodable>(_ type: T.Type, from body: JSONValue?) throws(BridgeFailure) -> T {
        do {
            return try ValueCoding.decode(T.self, from: body ?? .null)
        } catch {
            throw BridgeFailure(.malformed, "a body that is not a \(T.self): \(describe(error))")
        }
    }

    public static func value(_ value: some Encodable) throws -> JSONValue {
        try ValueCoding.encode(value)
    }

    /// A value as compact JSON text, as every message is written: keyed
    /// containers' keys sorted, so a recorded message and a fixture compare
    /// byte for byte, and every digit of a `JSONValue`'s numbers.
    public static func data(_ value: some Encodable) throws -> Data {
        try ValueCoding.encode(value).data(pretty: false)
    }

    static func text(_ value: some Encodable) throws -> String {
        String(decoding: try data(value), as: UTF8.self)
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
