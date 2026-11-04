import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SubstrateCore
import SubstrateDevice

/// A blob as stored: its digest, the value a `blobref` property takes, and
/// its size.
public struct StoredBlob: Decodable, Sendable, Equatable {
    public let digest: String
    public let size: Int

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        digest = try c.decode(String.self, forKey: .digest)
        size = c.value(.size, or: 0)
    }

    enum CodingKeys: String, CodingKey { case digest, size }
}

public extension SubstrateClient {
    /// Stores bytes as a blob and answers its digest. The record naming it
    /// must follow within a minute: the server collects a blob nothing
    /// references after that. An app's token stores one when its approval
    /// writes a kind with a `blobref` property.
    func putBlob(_ data: Data, mediaType: String, name: String? = nil) async throws -> StoredBlob {
        let req = try request("PUT", "api/v1/blobs", query: name.map { [("name", $0)] } ?? [],
                              body: .bytes(data, contentType: mediaType), timeout: Timeout.upload)
        return try await send(req).decode(StoredBlob.self)
    }
}
