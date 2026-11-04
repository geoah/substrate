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
        let (model, errors) = model(
            """
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
            ("struct A: Bridged { var amount: Decimal }", "Decimal"),
            ("struct A: Bridged { var n = 0 }", "no written type"),
            ("struct A: Bridged { let n: Int = 0 }", "let with a value"),
            ("struct A: Bridged { var other: Unknown }", "neither in the model"),
            ("struct A: Bridged { var map: [Int: String] }", "keyed by other than String"),
            ("struct A: Bridged { var double: String?? }", "optional of an optional"),
            ("struct A: Bridged { var a: Int; enum CodingKeys: String, CodingKey { case a } }", "CodingKeys"),
            ("struct A: Bridged { var a: Int; func encode(to encoder: Encoder) throws {} }", "encode(to:)"),
            ("enum E: Int, Bridged { case one }", "raw value is a String"),
            ("enum E: Bridged { case name }", "Kotlin reserves"),
            ("struct A {}\nextension A: Bridged {}", "in an extension"),
            ("final class C: Bridged {}", "is a class"),
            // Coding written in an extension changes the wire as surely as in the declaration.
            ("struct A: Bridged { var a: Int }\nextension A { func encode(to encoder: Encoder) throws {} }", "encode(to:) in an extension"),
            ("struct A: Bridged { var a: Int }\nextension A { init(from decoder: Decoder) throws { a = 1 } }", "init(from:) in an extension"),
            ("struct A: Bridged { var a: Int }\nextension A { enum CodingKeys: String, CodingKey { case a = \"b\" } }", "CodingKeys in an extension"),
            (
                "struct O { enum E: Bridged { case one } }\nextension O.E { init(from decoder: Decoder) throws { self = .one } }",
                "O.E writes its own init(from:)"
            ),
            ("struct A: Bridged { var a: Int }\nextension M.A { func encode(to encoder: Encoder) throws {} }", "A writes its own encode(to:)"),
            ("extension Bridged { func encode(to encoder: Encoder) throws {} }", "every bridged value"),
            ("struct A: Bridged { typealias CodingKeys = K; var a: Int }", "CodingKeys"),
            ("struct A: Bridged { @Clamped var a: Int }", "property wrapper"),
            ("enum E<T>: Bridged { case one(value: T) }", "generic"),
            ("struct B<T>: Bridged { var t: T }\nstruct A: Bridged { var b: B }", "name its type arguments"),
            ("struct B<T>: Bridged { var t: T }\nstruct A: Bridged { var b: B<Int, Int> }", "takes 1 type arguments"),
            ("struct A: Bridged { var rows: KeyedList<Int> }", "KeyedList's row"),
            ("struct A: Bridged { var probe: Probe }", "neither in the model"),
        ]
        for (source, words) in cases {
            let (model, errors) = model(source)
            #expect(model == nil, "\(source)")
            #expect(errors.contains { $0.contains(words) }, "\(source): \(errors)")
        }
    }

    @Test func codingInAnExtensionOfAnUnbridgedTypeIsNotTheModelsConcern() {
        var reader = Reader()
        reader.read(source: "struct A: Bridged { var a: Int }", module: "M", path: "a.swift")
        reader.read(source: "struct Other {}\nextension Other { func encode(to encoder: Encoder) throws {} }", module: "M", path: "b.swift")
        #expect(reader.model() != nil, "\(reader.errors.map(\.message))")
        // Across files: an extension in one file of a value declared in another.
        var across = Reader()
        across.read(source: "extension A { enum CodingKeys: String, CodingKey { case a } }", module: "M", path: "b.swift")
        across.read(source: "struct A: Bridged { var a: Int }", module: "M", path: "a.swift")
        #expect(across.model() == nil)
        #expect(across.errors.contains { $0.location == "b.swift:1" })
    }

    @Test func aValueNestedInAnUnbridgedTypeIsInAKotlinNamespace() throws {
        let (model, errors) = model("struct Counter { enum Command: Bridged { case add(amount: Int) }; struct Presentation: Bridged { var count: Int } }")
        #expect(errors.isEmpty, "\(errors)")
        let kotlin = try Kotlin.write(try #require(model), package: "p", fingerprint: "f", generatedBy: "test")
        #expect(kotlin.contains("public object Counter {\n    @Serializable(with = Counter.Command.Serializer::class)"))
        #expect(kotlin.contains("    public data class Presentation(\n        val count: Long,\n    )"))
    }

    @Test func instantsAddressesAndIdsCrossAsStrings() throws {
        let (keyed, keyedErrors) = model("struct A: Bridged { var editors: [ChildKey: String] }")
        #expect(keyedErrors.isEmpty, "\(keyedErrors)")
        #expect(keyed?.declaration("A")?.fields.map(\.type) == [.dictionary(.string)])
        let (model, errors) = model("struct A: Bridged { var at: Date; var url: URL?; var id: UUID }")
        #expect(errors.isEmpty, "\(errors)")
        #expect(model?.declaration("A")?.fields.map(\.type) == [.date, .optional(.url), .uuid])
        let kotlin = try Kotlin.write(try #require(model), package: "p", fingerprint: "f", generatedBy: "test")
        #expect(kotlin.contains("    val at: String,\n    val url: String? = null,\n    val id: String,"))
    }

    @Test func anUnlabelledPayloadIsKeyedByItsPlaceAndNamedValueInKotlin() throws {
        let (model, errors) = model("enum E: Bridged { case one(String); case two(String, verbatim: Bool); case three(Int, Int) }")
        #expect(errors.isEmpty, "\(errors)")
        #expect(model?.declaration("E")?.cases.map { $0.fields.map(\.name) } == [["_0"], ["_0", "verbatim"], ["_0", "_1"]])
        let kotlin = try Kotlin.write(try #require(model), package: "p", fingerprint: "f", generatedBy: "test")
        #expect(kotlin.contains("public data class One(\n        @SerialName(\"_0\") val value: String,\n    ) : E"))
        #expect(kotlin.contains("@SerialName(\"_0\") val value: String,\n        val verbatim: Boolean,"))
        #expect(kotlin.contains("@SerialName(\"_0\") val value0: Long,\n        @SerialName(\"_1\") val value1: Long,"))
    }

    @Test func aGenericStructIsAGenericDataClass() throws {
        let (model, errors) = model(
            """
            struct Button<Action: Codable & Sendable>: Bridged { var title: String; var action: Action }
            enum Go: String, Bridged { case on }
            struct Screen<App: Codable>: Bridged { var go: Button<Go>?; var app: App?; var all: [Button<Go>] }
            """)
        #expect(errors.isEmpty, "\(errors)")
        #expect(model?.declaration("Button")?.parameters == ["Action"])
        #expect(model?.declaration("Screen")?.fields.map(\.type.spelling) == ["Button<Go>?", "App?", "[Button<Go>]"])
        let kotlin = try Kotlin.write(try #require(model), package: "p", fingerprint: "f", generatedBy: "test")
        #expect(kotlin.contains("public data class Button<Action>(\n    val title: String,\n    val action: Action,\n)"))
        #expect(kotlin.contains("val go: Button<Go>? = null,\n    val app: App? = null,"))
        // A generic one takes its arguments' serializers, so the round trip's map leaves it out.
        #expect(!kotlin.contains("\"Button\" to"))
        #expect(Golden.write(try #require(model)).contains("\"parameters\": [\"Action\"]"))
    }

    @Test func aKeyedListAndAnOpaqueTypeAreKnownByName() throws {
        var reader = Reader(opaque: ["Probe.Result"])
        reader.read(
            source: """
                struct Row: Bridged { var id: String }
                struct A: Bridged { var rows: KeyedList<Row>; var found: Probe.Result? }
                """, module: "M", path: "M.swift")
        let read = reader.model()
        let model = try #require(read, "\(reader.errors)")
        #expect(model.declaration("A")?.fields.map(\.type.spelling) == ["KeyedList<Row>", "JSON(Probe.Result)?"])
        let kotlin = try Kotlin.write(model, package: "p", fingerprint: "f", generatedBy: "test")
        #expect(kotlin.contains("val rows: KeyedList<Row>,\n    val found: @Serializable(with = ExactJson::class) JsonElement? = null,"))
    }

    @Test func aConfigurationThatUsesAnotherGeneratesOnlyItsOwnAndImportsTheRest() throws {
        var reader = Reader()
        reader.read(
            source: "struct Kit { enum Tone: String, Bridged { case calm } }\nstruct Button<A: Codable>: Bridged { var action: A }",
            module: "Kit", path: "Kit.swift", external: true)
        reader.read(source: "struct Mine: Bridged { var tone: Kit.Tone; var button: Button<Kit.Tone> }", module: "App", path: "App.swift")
        let read = reader.model()
        let model = try #require(read, "\(reader.errors)")
        let kotlin = try Kotlin.write(model, package: "app", fingerprint: "f", generatedBy: "test", uses: ["kit"])
        #expect(kotlin.contains("import kit.*"))
        #expect(kotlin.contains("public data class Mine(\n    val tone: Kit.Tone,\n    val button: Button<Kit.Tone>,\n)"))
        #expect(!kotlin.contains("enum class Tone"))
        #expect(!kotlin.contains("object Kit"))
        #expect(!kotlin.contains("object ExactJson"))
        #expect(Golden.write(model.own).contains("\"Mine\""))
        #expect(!Golden.write(model.own).contains("\"name\": \"Kit.Tone\""))
    }

    @Test func aTypeACaseClassWouldShadowIsWrittenWithItsPackage() throws {
        let (model, errors) = model("struct Banner: Bridged { var lines: [String] }\nenum Shown: Bridged { case banner(Banner); case other(lines: [Banner]) }")
        #expect(errors.isEmpty, "\(errors)")
        let kotlin = try Kotlin.write(try #require(model), package: "p", fingerprint: "f", generatedBy: "test")
        #expect(kotlin.contains("@SerialName(\"_0\") val value: p.Banner,"))
        #expect(kotlin.contains("val lines: List<p.Banner>,"))
        #expect(kotlin.contains("public data class Banner(\n    val lines: List<String>,"))
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
        let configuration = try JSONDecoder().decode(
            Configuration.self,
            from: Data(contentsOf: root.appendingPathComponent("apps/kit/android/bridge.json")))
        let generation = try Generation.run(configuration, root: root)
        #expect(generation.stale(root: root).isEmpty, "stale: run `mise run android:generate`")
    }
}
