import Foundation
import Testing
@testable import SubstrateCore

/// The one JSON value both the kit's callers and WorkoutsCore read: a
/// number keeps its digits, an object its order, and each reads the way it
/// did on its side.
@Suite struct JSONValues {
    @Test func aNumberKeepsItsDigitsAndAnObjectItsOrder() throws {
        let value = try JSONValue(data: Data(#"{"b":83.9146,"a":1e-7,"c":12345678901234567890}"#.utf8))
        var object = try #require(value.objectValue)
        object["z"] = .number(Decimal(string: "9.50")!)
        object["m"] = .bool(true)
        let text = String(decoding: JSONValue.object(object).data(pretty: false), as: UTF8.self)
        #expect(text == #"{"a":0.0000001,"b":83.9146,"c":12345678901234567890,"z":9.5,"m":true}"#)
    }

    @Test func theKitsAccessorsReadAsTheyDid() {
        let value: JSONValue = ["cost": .number(Decimal(string: "0.0125")!), "count": 3, "on": true, "name": " Ada "]
        #expect(value["cost"]?.number == 0.0125)
        #expect(value["count"]?.int == 3 && value["count"]?.number == 3)
        #expect(value["on"]?.bool == true && value["on"]?.number == nil)
        #expect(value["name"]?.text == "Ada")
        #expect(value.object?.keys.sorted() == ["cost", "count", "name", "on"])
        // Past Int's range is no integer, rather than a crash.
        #expect(JSONValue.number(Decimal(string: "1e30")!).int == nil)
    }

    @Test func aLooseBodyTellsABoolFromANumber() throws {
        let value = try #require(JSONValue.parse(#"{"flag":true,"one":1,"half":0.5}"#))
        #expect(value["flag"] == .bool(true))
        #expect(value["one"] == .number(1))
        #expect(value["half"]?.decimal == Decimal(string: "0.5"))
        // A request body writes a whole number as an integer.
        #expect(value.jsonText == #"{"flag":true,"half":0.5,"one":1}"#)
    }
}

/// One RFC 3339 reader and writer for the server's stamps and every
/// datetime property.
@Suite struct Timestamps {
    @Test func aStampReadsWithEveryFractionalDigitAndAnyOffset() throws {
        let nanos = try #require(Timestamp.parse("2026-10-31T14:12:03.123456789Z"))
        #expect(abs(nanos.timeIntervalSince1970 - 1_793_455_923.123456789) < 1e-6)
        #expect(Timestamp.parse("2026-10-31T16:12:03+02:00") == Timestamp.parse("2026-10-31T14:12:03Z"))
        for refused in ["2026-10-31T14:12:03", "2026-10-31 14:12:03Z", "2026-10-31T14:12:03+0200", "2026-02-30T00:00:00Z"] {
            #expect(Timestamp.parse(refused) == nil, "\(refused)")
        }
    }

    @Test func aStampIsWrittenToTheSecondOrTheMillisecond() {
        let date = Date(timeIntervalSince1970: 1_793_455_923.9996)
        #expect(Timestamp.format(date, precision: .seconds) == "2026-10-31T14:12:03Z")
        #expect(Timestamp.format(date) == "2026-10-31T14:12:04.000Z")
        #expect(Timestamp.format(Date(timeIntervalSince1970: 0.1234)) == "1970-01-01T00:00:00.123Z")
        #expect(Timestamp.format(Date(timeIntervalSince1970: -0.5), precision: .seconds) == "1969-12-31T23:59:59Z")
    }
}
