import BridgeModel
import Foundation
import Testing

@Suite("The restricted model and what is generated from it")
struct BridgeModelTests {
    func model(_ source: String) -> (Model?, [String]) {
        var reader = Reader()
        reader.read(source: source, module: "M", path: "M.swift")
        let model = reader.model()
        return (model, reader.errors.map(\.message))
    }

    @Test func readsStructsStringEnumsAndPayloadEnums() throws {
        let (model, errors) = model("""
        public struct Outer: Bridged, Equatable {
            public var name: String
            public let count: Int
            public var ratio: Double?
            public var tags: [String]
            public var extra: [String: JSONValue]
            public var kind: Kind
            public static let shared = 1
            public var computed: Int { 1 }
            public enum Kind: String, Bridged { case a, b = "bee" }
        }
        enum Answer: Bridged {
            case found(value: String, at: Outer.Kind)
            case missing
        }
        """)
        #expect(errors.isEmpty, "\(errors)")
        let outer = try #require(model?.declaration("Outer"))
        #expect(outer.fields.map(\.name) == ["name", "count", "ratio", "tags", "extra", "kind"])
        #expect(outer.fields.map(\.type.spelling) == ["String", "Int", "Double?", "[String]", "[String: JSON]", "Outer.Kind"])
        #expect(model?.declaration("Outer.Kind")?.cases.map(\.rawValue) == ["a", "bee"])
        let answer = try #require(model?.declaration("Answer"))
        #expect(answer.kind == .enum)
        #expect(answer.cases.map(\.name) == ["found", "missing"])
        #expect(answer.cases[0].fields.map(\.type) == [.string, .named("Outer.Kind")])
    }

    @Test func refusesWhatTheModelDoesNotHold() {
        let cases: [(String, String)] = [
            ("struct A: Bridged { var at: Date }", "Date"),
            ("struct A: Bridged { var amount: Decimal }", "Decimal"),
            ("struct A: Bridged { var n = 0 }", "no written type"),
            ("struct A: Bridged { let n: Int = 0 }", "let with a value"),
            ("struct A: Bridged { var other: Unknown }", "neither in the model"),
            ("struct A: Bridged { var map: [Int: String] }", "keyed by other than String"),
            ("struct A: Bridged { var double: String?? }", "optional of an optional"),
            ("struct A: Bridged { var a: Int; enum CodingKeys: String, CodingKey { case a } }", "CodingKeys"),
            ("struct A: Bridged { var a: Int; func encode(to encoder: Encoder) throws {} }", "encode(to:)"),
            ("struct A<T>: Bridged { var a: Int }", "generic"),
            ("enum E: Bridged { case one(String) }", "unlabelled payload"),
            ("enum E: Int, Bridged { case one }", "raw value is a String"),
            ("enum E: Bridged { case name }", "Kotlin reserves"),
            ("struct A {}\nextension A: Bridged {}", "in an extension"),
            ("final class C: Bridged {}", "is a class"),
        ]
        for (source, words) in cases {
            let (model, errors) = model(source)
            #expect(model == nil, "\(source)")
            #expect(errors.contains { $0.contains(words) }, "\(source): \(errors)")
        }
    }

    @Test func aValueNestedInAnUnbridgedTypeIsInAKotlinNamespace() throws {
        let (model, errors) = model("struct Counter { enum Command: Bridged { case add(amount: Int) }; struct Presentation: Bridged { var count: Int } }")
        #expect(errors.isEmpty, "\(errors)")
        let kotlin = try Kotlin.write(try #require(model), package: "p", fingerprint: "f", generatedBy: "test")
        #expect(kotlin.contains("public object Counter {\n    @Serializable(with = Counter.Command.Serializer::class)"))
        #expect(kotlin.contains("    public data class Presentation(\n        val count: Long,\n    )"))
    }

    @Test func theFingerprintIsTheGoldensSHA256() {
        #expect(Golden.fingerprint("") == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
        #expect(Golden.fingerprint("abc") == "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
        let long = String(repeating: "a", count: 1000)
        #expect(Golden.fingerprint(long) == "41edece42d63e8d9bf515a9ba6932e1c20cbc9f5a5d134645adb5db1b9737ea3")
    }

    @Test func theKotlinDecodesSwiftsEnumShape() throws {
        let (model, _) = model("enum E: Bridged { case one(value: String); case two }")
        let kotlin = try Kotlin.write(try #require(model), package: "p", fingerprint: "f", generatedBy: "test")
        #expect(kotlin.contains("public sealed interface E {"))
        #expect(kotlin.contains("public data class One(\n        val value: String,\n    ) : E"))
        #expect(kotlin.contains("public data object Two : E"))
        #expect(kotlin.contains(#"SwiftEnumSerializer.Case("one", One::class, One.serializer())"#))
    }

    /// The files in the tree are what the sources generate today: the
    /// golden, the Kotlin and the fingerprint. Regenerate with
    /// `mise run android:generate`; never edit them by hand.
    @Test func theGeneratedFilesAreNotStale() throws {
        let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("../../../../../..").standardizedFileURL
        let configuration = try JSONDecoder().decode(Configuration.self,
                                                     from: Data(contentsOf: root.appendingPathComponent("apps/kit/android/bridge.json")))
        let generation = try Generation.run(configuration, root: root)
        #expect(generation.stale(root: root).isEmpty, "stale: run `mise run android:generate`")
    }
}
