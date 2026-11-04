import Foundation
import Testing
@testable import SubstrateCore

/// The one JSON value both the kit's callers and WorkoutsCore read: a
/// number keeps its digits, an object its order, and each reads the way it
/// did on its side.
@Suite(.timeLimit(.minutes(10))) struct JSONValues {
    @Test func aNumberKeepsItsDigitsAndAnObjectItsOrder() throws {
        let value = try JSONValue(data: Data(#"{"b":83.9146,"a":1e-7,"c":12345678901234567890}"#.utf8))
        var object = try #require(value.objectValue)
        object["z"] = .number(Decimal(string: "9.50")!)
        object["m"] = .bool(true)
        let text = String(decoding: JSONValue.object(object).data(pretty: false), as: UTF8.self)
        #expect(text == #"{"b":83.9146,"a":0.0000001,"c":12345678901234567890,"z":9.5,"m":true}"#)
        #expect(JSONValue.object(object).jsonText == text)
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
        #expect(value.jsonText == #"{"flag":true,"one":1,"half":0.5}"#)
        #expect(JSONValue.parse("not json") == nil && JSONValue.parse(nil) == nil)
    }
}

/// A JSON number is kept as its digits, so any number read is written back
/// as the number it was: past a Double's digits, past a Decimal's
/// exponents, past both.
@Suite(.timeLimit(.minutes(10))) struct JSONNumbers {
    @Test(arguments: [
        ("9007199254740993", "9007199254740993"), ("1e200", "1e200"), ("-1e-200", "-1e-200"), ("1e400", "1e400"),
        ("1.5e-400", "1.5e-400"), ("2.5E+300", "2.5e300"), ("1E+2", "100"), ("-0", "0"), ("0.0000001", "0.0000001"),
        ("83.9146", "83.9146"), ("9.50", "9.5"),
        ("123456789012345678901234567890123456789012345", "123456789012345678901234567890123456789012345"),
    ])
    func anyNumberIsWrittenAsTheNumberItWas(text: String, written: String) throws {
        let value = try JSONValue(data: Data("[\(text)]".utf8))
        #expect(String(decoding: value.data(pretty: false), as: UTF8.self) == "[\(written)]")
        #expect(try JSONValue(data: value.data(pretty: true)) == value)
    }

    @Test func equalNumbersAreEqualHoweverSpelled() throws {
        let spellings = try ["1.50", "15e-1", "0.15E1", "1.5"].map { try #require(JSONNumber($0)) }
        #expect(Set(spellings).count == 1)
        #expect(JSONNumber("1e200") != JSONNumber("1e201"))
        #expect(JSONValue.number(Decimal(string: "1.5")!) == .number(spellings[0]))
    }

    @Test(arguments: ["01", "+1", ".5", "5.", "1e", "1e+", "NaN", "Infinity", "-", "0x10", " 1", "1 ", "1e1000000001", "--1"])
    func onlyJSONsGrammarIsANumber(text: String) {
        #expect(JSONNumber(text) == nil)
    }

    @Test func eachReadingSaysWhereItStops() throws {
        let far = try #require(JSONNumber("1e200"))
        #expect(far.decimal == nil && far.double == 1e200 && far.int == nil)
        let farther = try #require(JSONNumber("1e400"))
        #expect(farther.decimal == nil && farther.double == nil)
        let tiny = try #require(JSONNumber("1e-200"))
        #expect(tiny.decimal == nil && tiny.double == 1e-200 && tiny.int == 0)
        let long = try #require(JSONNumber("123456789012345678901234567890123456789012345"))
        #expect(long.decimal == nil && long.int == nil)
        #expect(JSONNumber("9007199254740993")?.decimal == Decimal(string: "9007199254740993"))
    }

    /// Read from the digits, never through a Double, so an integer past
    /// 2^53 is itself and Int's own bounds are in range.
    @Test func anIntIsTheIntegerTowardZero() throws {
        let ints: [(String, Int?)] = [
            ("9223372036854775807", .max), ("-9223372036854775808", .min), ("9223372036854775808", nil),
            ("-9223372036854775809", nil), ("9007199254740993", 9_007_199_254_740_993), ("2.9", 2), ("-2.9", -2),
            ("0.5", 0), ("-0.5", 0), ("1.23e2", 123), ("1e18", 1_000_000_000_000_000_000), ("1e19", nil), ("1e30", nil),
            ("1e-200", 0),
        ]
        for (text, int) in ints {
            #expect(try #require(JSONNumber(text)).int == int, "\(text)")
        }
        #expect(JSONValue.number(Decimal(Int.max)).int == .max)
        #expect(JSONValue.number(Decimal(Int.min)).int == .min)
        #expect(JSONNumber("3.0")?.exactInt == 3 && JSONNumber("3.5")?.exactInt == nil)
        #expect(Decimal(string: "9007199254740993")?.exactInt == 9_007_199_254_740_993 && Decimal(Int.max).exactInt == .max)
    }

    /// An Encoder and a Decoder speak in Decimals and Doubles: a number one
    /// of those holds passes, one past a Double's range is refused, and an
    /// Encoder refuses one neither holds exactly rather than round it.
    @Test func throughCodableANumberPassesWholeOrIsRefused() throws {
        let read = try JSONDecoder().decode(JSONValue.self, from: Data(#"[9007199254740993,1e200,1e-200]"#.utf8))
        #expect(read == .array([.number(JSONNumber("9007199254740993")!), .number(JSONNumber("1e200")!), .number(JSONNumber("1e-200")!)]))
        #expect(throws: DecodingError.self) { try JSONDecoder().decode(JSONValue.self, from: Data("[1e400]".utf8)) }
        #expect(try JSONDecoder().decode(JSONValue.self, from: Data("1234567890123456789012345678901234567".utf8))
            == .number(JSONNumber("1234567890123456789012345678901234567")!))
        #expect(try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(read)) == read)
        let written = String(decoding: try JSONEncoder().encode(read), as: UTF8.self)
        #expect(written.hasPrefix("[9007199254740993,"))
        for refused in ["1e400", "1e-400", "123456789012345678901234567890123456789012345"] {
            #expect(throws: EncodingError.self) { try JSONEncoder().encode(JSONValue.number(JSONNumber(refused)!)) }
        }
    }

    @Test func theReaderTakesRFC8259AndNothingElse() throws {
        #expect(try JSONValue(data: Data(#"{"s":"\ud83d\ude00 \u00e9\/\n"}"#.utf8))["s"] == "😀 é/\n")
        #expect(try JSONValue(data: Data([0xEF, 0xBB, 0xBF] + Array(" true ".utf8))) == .bool(true))
        let refused = [
            #"{"a":1,}"#, "[1 2]", #""\u12""#, "tru", #"{"a" 1}"#, "1 2", "\"a\u{01}b\"", #""\ud800""#, #""\x""#, "{1:2}", "",
            String(repeating: "[", count: 600) + String(repeating: "]", count: 600), #"{"a":01}"#,
        ]
        for text in refused {
            #expect(throws: DecodingError.self, "\(text.prefix(20))") { try JSONValue(data: Data(text.utf8)) }
        }
        #expect(throws: DecodingError.self) { try JSONValue(data: Data([0x22, 0xFF, 0x22])) }
    }
}

/// One RFC 3339 reader and writer for the server's stamps and every
/// datetime property.
@Suite(.timeLimit(.minutes(10))) struct Timestamps {
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
