import Foundation
import SwiftParser
import SwiftSyntax

/// Reads the `Bridged` declarations of Swift sources into a `Model`,
/// refusing, by file and line, anything outside the restricted model
/// (`SubstrateCore`'s `Bridged`).
public struct Reader {
    /// The leaf types a field may name besides the declarations themselves.
    static let leaves: [String: BridgedType] = [
        "String": .string, "Bool": .bool, "Int": .int, "Double": .double, "JSONValue": .json,
        "Date": .date, "URL": .url, "UUID": .uuid,
    ]

    /// Names Swift and the JSON agree on but Kotlin's generated code would
    /// not: an enum entry may not be named for `Enum`'s own members.
    static let reservedCaseNames: Set<String> = ["name", "ordinal", "entries", "values", "valueOf", "Companion"]

    private var pending: [(declaration: Declaration, location: String, scope: [String], raw: RawDeclaration)] = []
    /// What extensions write that would change a type's coding, by the type
    /// they extend: refused once every file is read, for the bridged ones.
    private var extensionCoding: [(extended: String, location: String, what: String)] = []
    public private(set) var errors: [ModelError] = []
    /// Types outside the model a field may still name, crossing as their
    /// own JSON (`BridgedType.opaque`).
    let opaque: Set<String>

    public init(opaque: Set<String> = []) {
        self.opaque = opaque
    }

    /// Reads one file. `module` is its target's name, `path` how errors name
    /// it; `external` marks what another configuration generates.
    public mutating func read(source: String, module: String, path: String, external: Bool = false) {
        let tree = Parser.parse(source: source)
        let converter = SourceLocationConverter(fileName: path, tree: tree)
        let visitor = Visitor(module: module, path: path, converter: converter)
        visitor.walk(tree)
        pending += visitor.found.map { item in
            var item = item
            item.declaration.external = external
            return item
        }
        extensionCoding += visitor.extensionCoding
        errors += visitor.errors
    }

    /// The model, with every field's type resolved; nil when anything was refused.
    public mutating func model() -> Model? {
        var names: [String: String] = [:]
        for item in pending {
            if let other = names[item.declaration.name] {
                errors.append(ModelError(location: item.location, message: "\(item.declaration.name) is declared Bridged twice (also \(other))"))
            }
            names[item.declaration.name] = item.location
        }
        // An extension in any file read writes the coding of the type it
        // names; a module-qualified name is the same type.
        for item in extensionCoding {
            let parts = item.extended.split(separator: ".").map(String.init)
            let candidates = [item.extended] + (parts.count > 1 ? [parts.dropFirst().joined(separator: ".")] : [])
            guard let name = candidates.first(where: { names[$0] != nil }) else { continue }
            errors.append(ModelError(location: item.location, message: "\(name) \(item.what) in an extension; a bridged value keeps the synthesized coding"))
        }
        var arity: [String: Int] = [:]
        for item in pending { arity[item.declaration.name] = item.declaration.parameters.count }
        var declarations: [Declaration] = []
        for item in pending {
            var declaration = item.declaration
            func resolve(_ raw: RawType, _ what: String) -> BridgedType? {
                switch Self.resolve(raw, scope: item.scope, names: arity, parameters: Set(declaration.parameters), opaque: opaque) {
                case let .success(type): return type
                case let .failure(refusal):
                    errors.append(ModelError(location: item.location, message: "\(declaration.name).\(what): \(refusal.text)"))
                    return nil
                }
            }
            switch item.raw {
            case let .fields(fields):
                declaration.fields = fields.compactMap { field in resolve(field.type, field.name).map { Field(name: field.name, type: $0) } }
            case let .cases(cases):
                declaration.cases = cases.map { rawCase in
                    Case(name: rawCase.name, rawValue: rawCase.rawValue, fields: rawCase.fields.compactMap { field in
                        resolve(field.type, "\(rawCase.name).\(field.name)").map { Field(name: field.name, type: $0) }
                    })
                }
            }
            if declaration.kind == .enum || declaration.kind == .stringEnum {
                for rawCase in declaration.cases where Self.reservedCaseNames.contains(rawCase.name) {
                    errors.append(ModelError(location: item.location, message: "\(declaration.name) has a case named \(rawCase.name), which Kotlin reserves"))
                }
            }
            declarations.append(declaration)
        }
        guard errors.isEmpty else { return nil }
        return Model(declarations: declarations.sorted { $0.name < $1.name })
    }

    /// - Parameters:
    ///   - names: every declaration's qualified name, with how many type
    ///     parameters it takes.
    ///   - parameters: the type parameters of the declaration being read.
    static func resolve(_ raw: RawType, scope: [String], names: [String: Int], parameters: Set<String> = [],
                        opaque: Set<String> = []) -> Result<BridgedType, Refusal>
    {
        func again(_ raw: RawType) -> Result<BridgedType, Refusal> {
            resolve(raw, scope: scope, names: names, parameters: parameters, opaque: opaque)
        }
        // Inner scopes first, as Swift looks a name up.
        func declared(_ name: String) -> String? {
            for depth in stride(from: scope.count, through: 0, by: -1) {
                let candidate = (scope.prefix(depth) + [name]).joined(separator: ".")
                if names[candidate] != nil { return candidate }
            }
            return nil
        }
        switch raw {
        case let .optional(wrapped):
            if case .optional = wrapped { return .failure(Refusal("an optional of an optional, which JSON cannot tell from one")) }
            return again(wrapped).map(BridgedType.optional)
        case let .array(element):
            return again(element).map(BridgedType.array)
        case let .dictionary(key, value):
            // A ChildKey encodes as its string, a dictionary keyed by one as an object.
            guard case let .name(keyName) = key, keyName == "String" || keyName == "ChildKey" else {
                return .failure(Refusal("a dictionary keyed by other than String or ChildKey"))
            }
            if case .optional = value { return .failure(Refusal("a dictionary of optionals, whose nil JSON cannot tell from absent")) }
            return again(value).map(BridgedType.dictionary)
        case let .generic("KeyedList", arguments):
            guard arguments.count == 1 else { return .failure(Refusal("a KeyedList takes one row type")) }
            switch again(arguments[0]) {
            case let .success(.named(row)): return .success(.keyedList(.named(row)))
            case .success: return .failure(Refusal("a KeyedList's row is a Bridged struct with a String id"))
            case let .failure(refusal): return .failure(refusal)
            }
        case let .generic(name, arguments):
            guard let qualified = declared(name) else { return .failure(Refusal("\(name) is neither in the model nor declared Bridged")) }
            guard names[qualified] == arguments.count else {
                return .failure(Refusal("\(qualified) takes \(names[qualified] ?? 0) type arguments, not \(arguments.count)"))
            }
            var resolved: [BridgedType] = []
            for argument in arguments {
                switch again(argument) {
                case let .success(type): resolved.append(type)
                case let .failure(refusal): return .failure(refusal)
                }
            }
            return .success(.generic(qualified, resolved))
        case let .name(name):
            if parameters.contains(name) { return .success(.parameter(name)) }
            if let leaf = leaves[name] { return .success(leaf) }
            if let qualified = declared(name) {
                guard names[qualified] == 0 else { return .failure(Refusal("\(qualified) is generic; name its type arguments")) }
                return .success(.named(qualified))
            }
            if opaque.contains(name) { return .success(.opaque(name)) }
            switch name {
            case "Decimal": return .failure(Refusal("Decimal encodes as a JSON number; carry it as a String (DecimalText)"))
            case "Float", "Int8", "Int16", "Int32", "Int64", "UInt", "UInt8", "UInt16", "UInt32", "UInt64":
                return .failure(Refusal("\(name) is not in the model; use Int or Double"))
            case "Data": return .failure(Refusal("\(name) is not in the model; carry it as a String"))
            default: return .failure(Refusal("\(name) is neither in the model nor declared Bridged"))
            }
        case let .refused(spelling, why):
            return .failure(Refusal("\(spelling): \(why)"))
        }
    }
}

struct Refusal: Error {
    var text: String

    init(_ text: String) {
        self.text = text
    }
}

indirect enum RawType: Equatable {
    case name(String)
    case generic(String, [RawType])
    case optional(RawType)
    case array(RawType)
    case dictionary(RawType, RawType)
    case refused(String, String)
}

struct RawField {
    var name: String
    var type: RawType
}

struct RawCase {
    var name: String
    var rawValue: String?
    var fields: [RawField]
}

enum RawDeclaration {
    case fields([RawField])
    case cases([RawCase])
}

private final class Visitor: SyntaxVisitor {
    let module: String
    let path: String
    let converter: SourceLocationConverter
    var scope: [String] = []
    var found: [(declaration: Declaration, location: String, scope: [String], raw: RawDeclaration)] = []
    var extensionCoding: [(extended: String, location: String, what: String)] = []
    var errors: [ModelError] = []

    init(module: String, path: String, converter: SourceLocationConverter) {
        self.module = module
        self.path = path
        self.converter = converter
        super.init(viewMode: .sourceAccurate)
    }

    func location(_ node: some SyntaxProtocol) -> String {
        let place = node.startLocation(converter: converter)
        return "\(path):\(place.line)"
    }

    func refuse(_ node: some SyntaxProtocol, _ message: String) {
        errors.append(ModelError(location: location(node), message: message))
    }

    static func inherits(_ clause: InheritanceClauseSyntax?) -> [String] {
        clause?.inheritedTypes.map { $0.type.trimmedDescription } ?? []
    }

    // Types that are not declarations of their own still scope the names
    // inside them.
    override func visit(_ node: ClassDeclSyntax) -> SyntaxVisitorContinueKind {
        if Self.inherits(node.inheritanceClause).contains("Bridged") { refuse(node, "\(node.name.text) is a class; a bridged value is a struct or an enum") }
        scope.append(node.name.text)
        return .visitChildren
    }

    override func visitPost(_: ClassDeclSyntax) { scope.removeLast() }

    override func visit(_ node: ActorDeclSyntax) -> SyntaxVisitorContinueKind {
        scope.append(node.name.text)
        return .visitChildren
    }

    override func visitPost(_: ActorDeclSyntax) { scope.removeLast() }

    /// Protocols whose extension's coding would reach every bridged value.
    static let codingProtocols: Set<String> = ["Bridged", "Encodable", "Decodable", "Codable"]

    override func visit(_ node: ExtensionDeclSyntax) -> SyntaxVisitorContinueKind {
        let extended = node.extendedType.trimmedDescription
        if Self.inherits(node.inheritanceClause).contains("Bridged") {
            refuse(node, "\(extended) conforms to Bridged in an extension; conform where it is declared, where Codable is synthesized")
        }
        for (member, what) in Self.handwritten(node.memberBlock) {
            if Self.codingProtocols.contains(extended) {
                refuse(member, "an extension of \(extended) \(what), which would replace every bridged value's synthesized coding")
            } else {
                extensionCoding.append((extended, location(member), what))
            }
        }
        scope.append(extended)
        return .visitChildren
    }

    override func visitPost(_: ExtensionDeclSyntax) { scope.removeLast() }

    override func visit(_ node: StructDeclSyntax) -> SyntaxVisitorContinueKind {
        if Self.inherits(node.inheritanceClause).contains("Bridged") {
            // A generic struct's parameters are its fields' types as Kotlin's
            // generic data class takes them (`ActionButton<Action>`).
            let parameters = node.genericParameterClause?.parameters.map(\.name.text) ?? []
            let name = (scope + [node.name.text]).joined(separator: ".")
            for (member, what) in Self.handwritten(node.memberBlock) { refuse(member, "\(name) \(what)") }
            var fields: [RawField] = []
            for member in node.memberBlock.members {
                if member.decl.is(IfConfigDeclSyntax.self) { refuse(member, "\(name) has a member under #if; a bridged value is the same everywhere") }
                guard let variable = member.decl.as(VariableDeclSyntax.self) else { continue }
                if variable.modifiers.contains(where: { $0.name.tokenKind == .keyword(.static) || $0.name.tokenKind == .keyword(.class) }) { continue }
                let isLet = variable.bindingSpecifier.tokenKind == .keyword(.let)
                // A property wrapper encodes as the wrapper does, not as the
                // type written; no attribute on a stored property is in the model.
                if let attribute = variable.attributes.first?.as(AttributeSyntax.self) {
                    let names = variable.bindings.compactMap { $0.pattern.as(IdentifierPatternSyntax.self)?.identifier.text }
                    refuse(variable, "\(name).\(names.joined(separator: ", ")) is marked \(attribute.trimmedDescription); a property wrapper changes the synthesized coding")
                    continue
                }
                for binding in variable.bindings {
                    if let accessors = binding.accessorBlock {
                        // A computed property is not encoded; observers do not change that a stored one is.
                        if case let .accessors(list) = accessors.accessors,
                           list.allSatisfy({ $0.accessorSpecifier.tokenKind == .keyword(.willSet) || $0.accessorSpecifier.tokenKind == .keyword(.didSet) })
                        {} else { continue }
                    }
                    guard let identifier = binding.pattern.as(IdentifierPatternSyntax.self) else {
                        refuse(binding, "\(name) has a property that is not a plain name")
                        continue
                    }
                    let field = identifier.identifier.text
                    if isLet, binding.initializer != nil {
                        refuse(binding, "\(name).\(field) is a let with a value, which the synthesized Decodable skips")
                        continue
                    }
                    guard let type = binding.typeAnnotation?.type else {
                        refuse(binding, "\(name).\(field) has no written type; write it, so the generator need not infer it")
                        continue
                    }
                    fields.append(RawField(name: field, type: Self.raw(type)))
                }
            }
            found.append((Declaration(name: name, module: module, kind: .struct, fields: [], cases: [], parameters: parameters), location(node),
                          scope + [node.name.text], .fields(fields)))
        }
        scope.append(node.name.text)
        return .visitChildren
    }

    override func visitPost(_: StructDeclSyntax) { scope.removeLast() }

    override func visit(_ node: EnumDeclSyntax) -> SyntaxVisitorContinueKind {
        let inherited = Self.inherits(node.inheritanceClause)
        if inherited.contains("Bridged") {
            let name = (scope + [node.name.text]).joined(separator: ".")
            if node.genericParameterClause != nil { refuse(node, "\(name) is generic; a bridged value is not") }
            for (member, what) in Self.handwritten(node.memberBlock) { refuse(member, "\(name) \(what)") }
            let rawType = inherited.first
            let stringRaw = rawType == "String"
            if let rawType, !stringRaw, ["Int", "Double", "Character", "UInt8"].contains(rawType) {
                refuse(node, "\(name) has a \(rawType) raw value; a raw value is a String")
            }
            var cases: [RawCase] = []
            for member in node.memberBlock.members {
                if member.decl.is(IfConfigDeclSyntax.self) { refuse(member, "\(name) has a member under #if; a bridged value is the same everywhere") }
                guard let caseDecl = member.decl.as(EnumCaseDeclSyntax.self) else { continue }
                for element in caseDecl.elements {
                    var rawValue: String?
                    if stringRaw {
                        if element.parameterClause != nil { refuse(element, "\(name).\(element.name.text) has a payload in a String enum") }
                        if let value = element.rawValue?.value {
                            guard let literal = value.as(StringLiteralExprSyntax.self), let text = literal.representedLiteralValue else {
                                refuse(element, "\(name).\(element.name.text)'s raw value is not a plain string")
                                continue
                            }
                            rawValue = text
                        } else {
                            rawValue = element.name.text
                        }
                    }
                    var fields: [RawField] = []
                    for (index, parameter) in (element.parameterClause?.parameters ?? []).enumerated() {
                        // An unlabelled value is keyed by its place, as the
                        // synthesized coding writes it: `_0`, `_1`.
                        let label = parameter.firstName.flatMap { $0.tokenKind == .wildcard ? nil : $0.text } ?? "_\(index)"
                        if parameter.defaultValue != nil { refuse(parameter, "\(name).\(element.name.text).\(label) has a default") }
                        fields.append(RawField(name: label, type: Self.raw(parameter.type)))
                    }
                    cases.append(RawCase(name: element.name.text, rawValue: rawValue, fields: fields))
                }
            }
            found.append((Declaration(name: name, module: module, kind: stringRaw ? .stringEnum : .enum, fields: [], cases: []),
                          location(node), scope + [node.name.text], .cases(cases)))
        }
        scope.append(node.name.text)
        return .visitChildren
    }

    override func visitPost(_: EnumDeclSyntax) { scope.removeLast() }

    /// Hand-written coding is outside the model: the Kotlin is generated
    /// from what the synthesized coding writes. Each member that writes
    /// some, and what it does, in words that follow the type's name.
    static func handwritten(_ members: MemberBlockSyntax) -> [(MemberBlockItemSyntax, String)] {
        members.members.compactMap { member in
            let decl = member.decl
            let named = decl.as(EnumDeclSyntax.self)?.name.text ?? decl.as(StructDeclSyntax.self)?.name.text
                ?? decl.as(TypeAliasDeclSyntax.self)?.name.text ?? decl.as(ClassDeclSyntax.self)?.name.text
            if named == "CodingKeys" { return (member, "declares CodingKeys") }
            if let function = decl.as(FunctionDeclSyntax.self), function.name.text == "encode",
               function.signature.parameterClause.parameters.first?.firstName.text == "to"
            {
                return (member, "writes its own encode(to:)")
            }
            if let initializer = decl.as(InitializerDeclSyntax.self),
               initializer.signature.parameterClause.parameters.first?.firstName.text == "from"
            {
                return (member, "writes its own init(from:)")
            }
            return nil
        }
    }

    static func raw(_ type: TypeSyntax) -> RawType {
        if let optional = type.as(OptionalTypeSyntax.self) { return .optional(raw(optional.wrappedType)) }
        if let array = type.as(ArrayTypeSyntax.self) { return .array(raw(array.element)) }
        if let dictionary = type.as(DictionaryTypeSyntax.self) { return .dictionary(raw(dictionary.key), raw(dictionary.value)) }
        if type.is(ImplicitlyUnwrappedOptionalTypeSyntax.self) { return .refused(type.trimmedDescription, "an implicitly unwrapped optional") }
        if let member = type.as(MemberTypeSyntax.self) {
            if member.genericArgumentClause != nil { return .refused(type.trimmedDescription, "a generic type") }
            return .name(member.trimmedDescription)
        }
        if let identifier = type.as(IdentifierTypeSyntax.self) {
            let name = identifier.name.text
            let arguments = identifier.genericArgumentClause?.arguments.compactMap { argument -> TypeSyntax? in
                if case let .type(type) = argument.argument { return type }
                return nil
            } ?? []
            switch (name, arguments.count) {
            case ("Optional", 1): return .optional(raw(arguments[0]))
            case ("Array", 1): return .array(raw(arguments[0]))
            case ("Dictionary", 2): return .dictionary(raw(arguments[0]), raw(arguments[1]))
            case (_, 0): return .name(name)
            default: return .generic(name, arguments.map(raw))
            }
        }
        return .refused(type.trimmedDescription, "a type the model does not hold")
    }
}
