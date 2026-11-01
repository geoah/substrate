import Foundation
import SubstrateKit

/// The record views' reader over this app's own client, so a pill's read
/// shares the chat's session and timeouts, and a refusal is said in the
/// app's words.
struct ChatRecordReader: RecordReader {
    let api: API
    let server: URL
    /// What an app token was approved for; nil for a password login's,
    /// which reaches everything.
    let grants: Grants?

    func record(_ reference: RecordReference) async throws -> Record? {
        if let grants, !grants.reads(reference.kind) { throw RecordReadError.notAllowed }
        do {
            return try await api.get(reference.kind, reference.recordID)
        } catch let error as ServerError where error.status == 403 {
            throw RecordReadError.notAllowed
        } catch is InsufficientScope {
            throw RecordReadError.notAllowed
        }
    }

    func declaration(of kind: String) async throws -> KindDeclaration? {
        try await record(RecordReference(kind: KindDeclaration.kind, id: kind)).flatMap(KindDeclaration.init(record:))
    }

    func webURL(for reference: RecordReference) -> URL? {
        var base = server.absoluteString
        while base.hasSuffix("/") { base.removeLast() }
        let parts = reference.kind.split(separator: "/").map(String.init) + [reference.recordID]
        return URL(string: base + "/data/" + parts.map(segment).joined(separator: "/"))
    }

    var deniedHint: String? {
        grants != nil ? "Substrate Chat wasn't approved to read these. Allow it with Settings → Change Access…" : nil
    }
}
