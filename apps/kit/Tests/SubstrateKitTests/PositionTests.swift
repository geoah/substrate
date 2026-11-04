import Foundation
import Testing

@testable import SubstrateCore

@Suite("Positions", .timeLimit(.minutes(10)))
struct PositionTests {
    @Test func aPositionIsTheShortestNumberBetweenItsNeighbours() {
        #expect(Position.between(1, 2) == Decimal(string: "1.5"))
        #expect(Position.between(1, Decimal(string: "1.5")) == Decimal(string: "1.3"))
        #expect(Position.between(nil, 1) == 0)
        #expect(Position.between(3, nil) == 4)
        #expect(Position.between(nil, nil) == 1)
    }

    @Test func halvingTheSameGapStaysStrictlyBetween() {
        var high: Decimal = 2
        for step in 0 ..< 50 {
            let next = Position.between(1, high)
            #expect(next > 1 && next < high, "step \(step): \(next) between 1 and \(high)")
            #expect(Position.digits(next) <= Position.digitLimit, "step \(step): \(next)")
            high = next
        }
    }

    @Test func movingIntoTheSameGapAgainAndAgainKeepsTheOrderAndTheDigits() {
        // Each move puts the last record second, into the gap the move
        // before it made smaller: a gap that runs out is spread out.
        var order = ["a", "b", "c", "d"]
        var positions: [String: Decimal] = ["a": 1, "b": 2, "c": 3, "d": 4]
        for step in 0 ..< 300 {
            let moved = order.removeLast()
            order.insert(moved, at: 1)
            for (id, position) in Position.moves(moved, to: 1, in: order, positions: positions) { positions[id] = position }
            let drawn = order.map { positions[$0]! }
            #expect(drawn == drawn.sorted() && Set(drawn).count == drawn.count, "step \(step): \(drawn)")
            #expect(drawn.allSatisfy { Position.digits($0) <= Position.digitLimit }, "step \(step): \(drawn)")
        }
    }

    @Test func aGapWithNoRoomLeftHasItsNeighboursSpreadOut() {
        // Neighbours a hair apart at the limit's last digit: no number fits
        // between them, so the move writes them too.
        let a = Decimal(string: "1.0000000000000000001")!
        let b = Decimal(string: "1.0000000000000000002")!
        #expect(Position.fit(a, b) == nil)
        let positions: [String: Decimal] = ["z": 0, "a": a, "b": b, "c": 5]
        let moves = Position.moves("m", to: 2, in: ["z", "a", "b", "c", "m"], positions: positions)
        var placed = positions
        for (id, position) in moves { placed[id] = position }
        let order = ["z", "a", "m", "b", "c"].map { placed[$0]! }
        #expect(order == order.sorted() && Set(order).count == order.count, "\(order)")
        #expect(moves.values.allSatisfy { Position.digits($0) <= Position.digitLimit })
        #expect(moves["z"] == nil && moves["c"] == nil, "only the gap's neighbours move: \(moves)")
    }

    @Test func twoDevicesAppendingAtOnceAreToldApartByTheNextMove() {
        // Both appended after 1 and wrote 2: drawn a, b by id. Moving x
        // between them writes b above x too.
        let positions: [String: Decimal] = ["a": 2, "b": 2, "c": 3]
        let moves = Position.moves("x", to: 1, in: ["a", "b", "c", "x"], positions: positions)
        var placed = positions
        for (id, position) in moves { placed[id] = position }
        #expect(placed["a"]! < placed["x"]!)
        #expect(placed["x"]! < placed["b"]!)
        #expect(placed["b"]! < placed["c"]!)
    }

    @Test func aMoveWritesTheOneRecordMoved() {
        let positions: [String: Decimal] = ["a": 1, "b": 2, "c": 3]
        #expect(Position.moves("c", to: 0, in: ["a", "b", "c"], positions: positions) == ["c": 0])
        #expect(Position.moves("a", to: 1, in: ["a", "b", "c"], positions: positions) == ["a": Decimal(string: "2.5")!])
        #expect(Position.moves("a", to: 2, in: ["a", "b", "c"], positions: positions) == ["a": 4])
    }

    @Test func aMoveAmongRecordsWithoutPositionsNumbersTheOnesBeforeIt() {
        let moves = Position.moves("a", to: 2, in: ["a", "x", "y", "z"], positions: ["a": 1])
        #expect(moves == ["x": 1, "y": 2, "a": 3])
    }
}
