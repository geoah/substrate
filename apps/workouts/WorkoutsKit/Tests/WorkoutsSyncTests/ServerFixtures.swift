import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import WorkoutsCore
@testable import WorkoutsSync
import SubstrateCore

enum Fixtures {
    static func data(_ name: String) throws -> Data {
        guard let url = Bundle.module.url(forResource: name, withExtension: "json", subdirectory: "Fixtures") else {
            throw CocoaError(.fileNoSuchFile)
        }
        return try Data(contentsOf: url)
    }
}

/// The fixtures were recorded from a throwaway server built from this tree
/// (the repository probe.example.com), its training package respelled as the
/// published `substrate.reamde.dev/training` every repository now holds.
func api(_ server: ScriptedServer) -> WorkoutsAPI {
    var client = SubstrateClient(server: URL(string: "http://localhost:8080")!, token: "substrate_tok_test", transport: server)
    client.pause = { _ in }
    return WorkoutsAPI(client: client)
}

/// The published package's row, as the server answers it: `shippedVersion`
/// is the publisher's number a published install stamps.
func packageRow(_ id: String, shipped: Int) -> String {
    #"{"id":"\#(id)","kind":"substrate.reamde.dev/core/package","properties":{"authority":"substrate.reamde.dev","package":"\#(id.split(separator: "/").last!)","source":"published","shippedVersion":\#(shipped),"version":\#(shipped + 2)},"version":3}"#
}

let packagePath = "/api/v1/substrate.reamde.dev/core/package/substrate.reamde.dev%2Ftraining"
let notFoundBody = #"{"error":{"code":"not_found","message":"not found"}}"#

func record(_ version: Int) -> String {
    #"{"id":"x","kind":"k","properties":{},"version":\#(version)}"#
}
