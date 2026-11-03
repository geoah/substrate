import Foundation
import SubstrateCore
import SubstrateDevice

/// TXT lookups on Android: `DnsResolver.rawQuery` (API 29 and later) through
/// the bridge. Kotlin hands back the response code and each record's
/// character-strings; joining and bounding them, and telling the outcomes
/// apart, is done here, as the Apple resolver's are.
public struct BridgeTXTResolver: TXTResolver {
    public let channel: DeviceChannel
    public var timeout: TimeInterval

    /// More records, or a longer one, than any `_substrate` record needs is a
    /// failure, never a truncated answer.
    public static let maximumRecords = 16
    public static let maximumRecordBytes = 4096

    public init(channel: DeviceChannel, timeout: TimeInterval = 5) {
        self.channel = channel
        self.timeout = timeout
    }

    public func txt(_ name: String) async throws -> TXTAnswer {
        let answer: DNSAnswer
        do {
            answer = try await channel.call(.dnsTXT(name: name, timeoutMillis: Int(timeout * 1000)), as: DNSAnswer.self)
        } catch let failure as BridgeFailure where failure.code == .cancelled {
            throw CancellationError()
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            return .failed("Looking up \(name) failed: \(error.localizedDescription)")
        }
        return Self.classify(answer, name: name)
    }

    /// The four outcomes, as `DNSServiceTXTResolver` gives them: NOERROR with
    /// records is the records; NXDOMAIN, or NOERROR with none, is a confirmed
    /// absence; SERVFAIL or REFUSED leave the absence unconfirmed, which
    /// allows only the parent-host fallback; anything else fails.
    public static func classify(_ answer: DNSAnswer, name: String) -> TXTAnswer {
        switch answer {
        case let .failed(reason):
            return .failed("Looking up \(name) failed: \(reason)")
        case let .answered(rcode, records):
            switch rcode {
            case 0:
                guard !records.isEmpty else { return .confirmedAbsent }
                guard records.count <= maximumRecords else {
                    return .failed("Looking up \(name) returned \(records.count) TXT records, more than \(maximumRecords).")
                }
                var joined: [String] = []
                for segments in records {
                    let text = segments.joined()
                    guard text.utf8.count <= maximumRecordBytes else {
                        return .failed("Looking up \(name) returned a TXT record longer than \(maximumRecordBytes) bytes.")
                    }
                    joined.append(text)
                }
                return .records(joined)
            case 3:
                return .confirmedAbsent
            case 2, 5:
                return .unconfirmedAbsent("Looking up \(name) failed: the DNS servers did not answer it (\(rcode == 2 ? "SERVFAIL" : "REFUSED")).")
            default:
                return .failed("Looking up \(name) failed (DNS response code \(rcode)).")
            }
        }
    }
}
