import Foundation

/// What a TXT lookup came to, each outcome kept apart: only a confirmed
/// absence lets discovery go on to a name's web host, an unconfirmed one
/// lets it try only the parent host, and a failure stops it.
public enum TXTAnswer: Hashable, Sendable {
    /// Every TXT record at the name, each one's character-strings joined.
    case records([String])
    /// The name, or its TXT record, does not exist, and the DNS servers
    /// asked again agree.
    case confirmedAbsent
    /// The system resolver said there is no record and the DNS servers could
    /// not confirm it, as a SERVFAIL, a VPN, an encrypted DNS profile or
    /// iCloud Private Relay can leave it. The words say what happened.
    case unconfirmedAbsent(String)
    /// No answer in time, or an error other than an absence. The words say
    /// what happened.
    case failed(String)
}

/// Answers TXT lookups: DNS-SD on an Apple device, a fixed table in a test.
public protocol TXTResolver: Sendable {
    /// What a lookup of `name`'s TXT records came to. Throws only when the
    /// task is cancelled.
    func txt(_ name: String) async throws -> TXTAnswer
}
