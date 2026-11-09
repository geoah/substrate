import Foundation
import Testing

@testable import SubstrateCore

/// A kind's status as `lifecycle` reads it (decisions 0226 and 0247).
@Suite(.timeLimit(.minutes(10))) struct KindStatusTests {
    static func declaration(_ json: String) throws -> Record {
        try JSONDecoder().decode(
            Record.self, from: Data(#"{"id":"ada.example.com/work/chore","kind":"substrate.reamde.dev/core/kind","properties":"#.utf8 + Data(json.utf8) + Data("}".utf8)))
    }

    @Test func aMachineReadsItsStandingsAndMoves() throws {
        let record = try Self.declaration(
            #"""
            {"traits":["substrate.reamde.dev/core/temporal(point: dueAt)","substrate.reamde.dev/core/lifecycle"],
             "properties":{"status":{"type":"state","states":["proposed","open","done","abandoned"],"initial":"open",
               "transitions":[{"from":"proposed","to":"open"},{"from":"proposed","to":"abandoned"},{"from":"open","to":"done"},
                              {"from":"open","to":"abandoned"},{"from":"done","to":"open"}],
               "done":["done"],"dropped":["abandoned"],"proposed":["proposed"]}}}
            """#)
        let status = try #require(KindStatus(declaration: record))
        #expect(status.property == "status" && status.initial == "open")
        #expect(status.standing(of: "proposed") == .proposed && status.standing(of: "abandoned") == .dropped)
        #expect(status.standing(of: nil) == .open && status.standing(of: "open") == .open)
        #expect(status.move(from: "proposed", to: .open) == "open")
        #expect(status.move(from: "proposed", to: .dropped) == "abandoned")
        #expect(status.move(from: "proposed", to: .done) == nil)
        #expect(status.moves(from: "done") == ["open"])
    }

    @Test func aRenamedEnumTakesAnyOfItsValues() throws {
        let record = try Self.declaration(
            #"""
            {"traits":["substrate.reamde.dev/core/lifecycle(status: state)"],
             "properties":{"state":{"type":"enum","values":["todo",{"value":"closed","label":"Closed"}],"done":["closed"]}}}
            """#)
        let status = try #require(KindStatus(declaration: record))
        #expect(status.property == "state" && status.values == ["todo", "closed"] && status.moves == nil)
        #expect(status.moves(from: "todo") == ["closed"] && status.move(from: "todo", to: .done) == "closed")
    }

    @Test func aKindWithoutTheTraitHasNoStatus() throws {
        #expect(KindStatus(declaration: try Self.declaration(#"{"properties":{"status":{"type":"enum","values":["a"]}}}"#)) == nil)
        #expect(KindDeclaration.renames(in: "substrate.reamde.dev/core/temporal(point: dueAt)") == ["point": "dueAt"])
        #expect(KindDeclaration.renames(in: "substrate.reamde.dev/core/lifecycle").isEmpty)
    }
}
