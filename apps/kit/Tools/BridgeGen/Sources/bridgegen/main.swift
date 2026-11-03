import BridgeModel
import Foundation

// bridgegen [--check] <bridge.json>
//
// Run from the repository's root, which every path in the configuration is
// relative to. --check writes nothing and fails when a generated file is
// stale, naming it.
var arguments = Array(CommandLine.arguments.dropFirst())
let check = arguments.contains("--check")
arguments.removeAll { $0 == "--check" }
guard let configPath = arguments.first else {
    FileHandle.standardError.write(Data("usage: bridgegen [--check] <bridge.json>\n".utf8))
    exit(2)
}
let root = URL(fileURLWithPath: FileManager.default.currentDirectoryPath)
do {
    let configuration = try JSONDecoder().decode(Configuration.self, from: Data(contentsOf: URL(fileURLWithPath: configPath)))
    let generation = try Generation.run(configuration, root: root)
    if check {
        let stale = generation.stale(root: root)
        guard stale.isEmpty else {
            FileHandle.standardError.write(Data("bridgegen: stale, regenerate with `\(Generation.command)`: \(stale.joined(separator: ", "))\n".utf8))
            exit(1)
        }
        print("bridgegen: up to date, fingerprint \(generation.fingerprint)")
    } else {
        try generation.write(root: root)
        print("bridgegen: wrote \(generation.files.map(\.path).joined(separator: ", ")), fingerprint \(generation.fingerprint)")
    }
} catch {
    FileHandle.standardError.write(Data("bridgegen: \(error)\n".utf8))
    exit(1)
}
