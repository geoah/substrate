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

    public static func bundled() throws -> TasksPackage {
        guard let url = Bundle.module.url(forResource: "tasks", withExtension: "json", subdirectory: "Package") else {
            throw CocoaError(.fileNoSuchFile)
        }
        return try read(Data(contentsOf: url))
    }

    public static func read(_ data: Data) throws -> TasksPackage {
        let file = try JSONDecoder().decode(JSONValue.self, from: data)
        var properties: [String: [String]] = [:]
        for document in file["documents"]?.arrayValue ?? [] where document["kind"]?.text == "substrate.reamde.dev/core/kind" {
            guard let id = document["metadata"]?["id"]?.text else { continue }
            properties[id] = document["data"]?["properties"]?.objectValue?.keys ?? []
        }
        return TasksPackage(package: file["package"]?.text ?? "", version: file["version"]?.int ?? 0, properties: properties)
    }
}
