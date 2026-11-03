import Foundation
import SubstrateCore
import SubstrateRuntime
import SubstrateTesting
import Testing

/// How a runtime's presentations cross to Android (§5.5): keyed lists apart
/// and by reference, a new locale or time zone presented in, and commands
/// read with every digit.
@Suite("The runtime's crossing")
struct CrossingTests {
    struct Row: Codable, Equatable, Sendable, Identifiable {
        var id: String
        var text: String
    }

    /// A parent with its list and a child keyed under it with one of its own,
    /// and a presenter that words the time zone.
    struct Shelf: Controller, Equatable {
        typealias Child = Shelf

        enum Command: Codable, Equatable, Sendable {
            case put([String])
            case reset
            case keep(JSONValue)
        }

        struct State: Codable, Equatable, Sendable {
            var rows = KeyedList<Row>()
            var kept: JSONValue?
        }

        struct Presentation: Codable, Equatable, Sendable {
            var rows: KeyedList<Row>
            var zone: String
            var kept: JSONValue?
            var children: [ChildKey: Shelf.Presentation]
        }

        var nested = true

        var initialState: State { State() }

        func events(for command: Command, in _: Enrichment<Shelf>) -> [Command] { [command] }

        func apply(_ event: Command, to state: State) throws -> State {
            var next = state
            switch event {
            case let .put(ids): next.rows.update(ids.map { Row(id: $0, text: $0.uppercased()) })
            // A list made again from scratch: its own revision starts over.
            case .reset: next.rows = KeyedList()
            case let .keep(value): next.kept = value
            }
            return next
        }

        func present(_ input: PresenterInput<Shelf>) -> Presentation {
            Presentation(rows: input.state.rows, zone: input.timeZone.identifier, kept: input.state.kept, children: input.children)
        }

        func children(in _: State) -> [ChildKey: Shelf] {
            nested ? [ChildKey("inner"): Shelf(nested: false)] : [:]
        }
    }

    @Test func aKeyedListCrossesAsAReferenceNamedForItsPlaceAndMovesOnlyWhenItsRowsDo() async throws {
        let harness = ControllerHarness(Shelf())
        var frames = await harness.runtime.encodedFrames().makeAsyncIterator()
        let first = try #require(await frames.next())
        let tree = try JSONValue(data: first.presentation.get())
        #expect(tree["rows"] == ["$list": "rows", "revision": 1])
        #expect(tree["children"]?["inner"]?["rows"] == ["$list": "children/inner/rows", "revision": 1])
        #expect(first.lists.map(\.name) == ["rows", "children/inner/rows"])

        await harness.runtime.send(.put(["a", "b"]))
        let second = try #require(await frames.next())
        #expect(try JSONValue(data: second.presentation.get())["rows"]?["revision"] == 2)
        // The child's list did not move, so it does not cross again.
        #expect(second.lists.map(\.name) == ["rows"])
        #expect(second.lists.map(\.revision) == [2])

        // Made again at revision 0, the list still moves forward as it crosses.
        await harness.runtime.send(.reset)
        let third = try #require(await frames.next())
        #expect(try JSONValue(data: third.presentation.get())["rows"]?["revision"] == 3)
        #expect(third.lists.map(\.revision) == [3])
    }

    @Test func aCarriedListHandsItsRowsToItsReceiver() async throws {
        final class Received: KeyedListReceiver, @unchecked Sendable {
            var got: [(String, Int, [String])] = []
            func receive<R>(list name: String, _ list: KeyedList<R>) {
                got.append((name, list.revision, list.ids))
            }
        }
        let harness = ControllerHarness(Shelf(nested: false))
        await harness.runtime.send(.put(["x"]))
        var frames = await harness.runtime.encodedFrames().makeAsyncIterator()
        let frame = try #require(await frames.next())
        let received = Received()
        for list in frame.lists { list.hand(to: received) }
        #expect(received.got.map(\.0) == ["rows"])
        #expect(received.got.map(\.1) == [1])
        #expect(received.got.map(\.2) == [["x"]])
    }

    @Test func aPresentationEncodedWholeKeepsItsRows() async throws {
        let harness = ControllerHarness(Shelf(nested: false))
        await harness.runtime.send(.put(["a"]))
        var presentations = await harness.runtime.encodedPresentations().makeAsyncIterator()
        let tree = try JSONValue(data: #require(await presentations.next()).get())
        #expect(tree["rows"] == ["revision": 1, "rows": [["id": "a", "text": "A"]]])
    }

    @Test func aNewTimeZonePresentsTheWholeTreeAgain() async throws {
        let harness = ControllerHarness(Shelf())
        let before = await harness.runtime.presentation
        #expect(before.zone == TimeZone(identifier: "UTC")!.identifier)
        await harness.runtime.environmentChanged(locale: Locale(identifier: "el_GR"), timeZone: TimeZone(identifier: "Asia/Tokyo")!)
        let after = await harness.runtime.presentation
        #expect(after.zone == "Asia/Tokyo")
        #expect(after.children[ChildKey("inner")]?.zone == "Asia/Tokyo")
    }

    @Test func anEncodedCommandKeepsEveryDigit() async throws {
        let harness = ControllerHarness(Shelf(nested: false))
        let digits = "1234567890123456789012345678901234567890.0987654321"
        let text = #"{"path":[],"command":{"keep":{"_0":{"n":\#(digits)}}}}"#
        try await harness.runtime.send(encoded: Data(text.utf8))
        #expect(await harness.runtime.state.kept?["n"] == .number(JSONNumber(digits)!))
    }
}
