import Foundation
import SubstrateCore

/// The tasks package's closure this build carries (Resources/Package,
/// written by `.mise/closures.py --json`): the version the approval asks for
/// as its floor, and each kind's declared properties, which the codec is
/// held to.
public struct TasksPackage: Sendable {
    public var package: String
    public var version: Int
    /// Each kind's declared property names, by kind reference.
    public var properties: [String: [String]]
    /// Each kind's trait bindings, by kind reference, then by trait and its
    /// parameter: `temporal` → `point` → `dueAt`, a property the trait binds
    /// rather than the kind declares.
    public var bindings: [String: [String: [String: String]]] = [:]
    /// Each kind's declaration, as the app reads its own kinds before the
    /// server has said anything (and where the server says nothing of the
    /// settings kinds, which bind no status).
    public var declarations: [TaskKindDeclaration] = []

    /// The closure in this build's resources. On Android `Bundle.module`
    /// looks beside `/system/bin` and stops the process when the bundle is
    /// not there (docs/plans/app-kit.md §3.2), so there the bundle the
    /// bridge copied out is read instead (`bundled(from:)`), and this throws.
    public static func bundled() throws -> TasksPackage {
        #if os(Android)
        throw CocoaError(.fileNoSuchFile)
        #else
        try bundled(from: .module)
        #endif
    }

    /// The closure in `bundle`: the package's resources wherever they were
    /// copied to.
    public static func bundled(from bundle: Bundle) throws -> TasksPackage {
        guard let url = bundle.url(forResource: "tasks", withExtension: "json", subdirectory: "Package") else {
            throw CocoaError(.fileNoSuchFile)
        }
        return try read(Data(contentsOf: url))
    }

    public static func read(_ data: Data) throws -> TasksPackage {
        let file = try JSONDecoder().decode(JSONValue.self, from: data)
        var properties: [String: [String]] = [:]
        var bindings: [String: [String: [String: String]]] = [:]
        var declarations: [TaskKindDeclaration] = []
        for document in file["documents"]?.arrayValue ?? [] where document["kind"]?.text == "substrate.reamde.dev/core/kind" {
            guard let id = document["metadata"]?["id"]?.text else { continue }
            properties[id] = document["data"]?["properties"]?.objectValue?.keys ?? []
            declarations.append(TaskKindDeclaration(identity: id, definition: document["data"]?.object ?? [:]))
            for trait in document["data"]?["traits"]?.arrayValue ?? [] {
                guard let text = trait.text, let open = text.firstIndex(of: "("), text.hasSuffix(")") else { continue }
                let name = String(text[..<open].split(separator: "/").last ?? "")
                for pair in text[text.index(after: open) ..< text.index(before: text.endIndex)].split(separator: ",") {
                    let parts = pair.split(separator: ":", maxSplits: 1).map { $0.trimmingCharacters(in: .whitespaces) }
                    if parts.count == 2 { bindings[id, default: [:]][name, default: [:]][parts[0]] = parts[1] }
                }
            }
        }
        var package = TasksPackage(package: file["package"]?.text ?? "", version: file["version"]?.int ?? 0, properties: properties)
        package.bindings = bindings
        package.declarations = declarations
        return package
    }
}
