import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateTesting
import Testing

@testable import WorkoutsSync

/// The app's own list read (`WorkoutsAPI.list`) against a server that can no
/// longer continue it part way.
@Suite("A list read", .timeLimit(.minutes(10)))
struct ListTests {
    let exercise = "substrate.reamde.dev/training/exercise"

    func api(_ stub: StubTransport) -> WorkoutsAPI {
        var client = SubstrateClient(server: URL(string: "https://ada.example.com")!, token: "sub_x", transport: stub)
        client.pause = { _ in }
        return WorkoutsAPI(client: client)
    }

    func page(_ ids: [String], cursor: String?) -> String {
        let rows = ids.map { #"{"id":"\#($0)","kind":"\#(exercise)","properties":{"name":"\#($0)"},"version":1}"# }.joined(separator: ",")
        return #"{"records":[\#(rows)],"cursor":\#(cursor.map { "\"\($0)\"" } ?? "null")}"#
    }

    let compacted = #"{"error":{"code":"compacted","message":"the cursor's history was replaced: list again from the first page"}}"#

    /// The `after` each list request carried, nil for a first page.
    func afters(_ stub: StubTransport) -> [String?] {
        stub.sent("GET", "/api/v1/records").map { request in
            request.url.flatMap { URLComponents(url: $0, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "after" }?.value }
        }
    }

    @Test("a continuation told compacted lists again from the first page, keeping nothing read before")
    func startsOver() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/records", json: page(["a", "b"], cursor: "p1"))
        stub.on("GET", "/api/v1/records", status: 410, json: compacted)
        // The history the read starts over in: b is gone, c is new.
        stub.on("GET", "/api/v1/records", json: page(["a", "c"], cursor: "q1"))
        stub.on("GET", "/api/v1/records", json: page(["d"], cursor: nil))
        let listed = try await api(stub).list([exercise])
        #expect(listed.map(\.id) == ["a", "c", "d"])
        #expect(afters(stub) == [nil, "p1", nil, "q1"])
    }

    @Test("a list told compacted again after its restart throws it")
    func restartsOnce() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/records", json: page(["a"], cursor: "p1"))
        stub.on("GET", "/api/v1/records", status: 410, json: compacted)
        stub.on("GET", "/api/v1/records", json: page(["a"], cursor: "q1"))
        stub.on("GET", "/api/v1/records", status: 410, json: compacted)
        do {
            _ = try await api(stub).list([exercise])
            Issue.record("a list refused twice answered")
        } catch let error as ServerError {
            #expect(error.isCompacted)
        }
        #expect(afters(stub) == [nil, "p1", nil, "q1"])
    }

    @Test("any other refusal of a continuation is thrown as it is")
    func otherRefusals() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/records", json: page(["a"], cursor: "p1"))
        stub.on("GET", "/api/v1/records", status: 422, json: #"{"error":{"code":"validation","message":"the cursor was minted for another filter"}}"#)
        await #expect(throws: ServerError(status: 422, code: "validation", message: "the cursor was minted for another filter")) {
            try await api(stub).list([exercise])
        }
        #expect(afters(stub) == [nil, "p1"])
    }
}
