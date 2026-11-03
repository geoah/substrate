import Foundation

/// The golden: every bridged value, its fields with their types, and its
/// cases, as JSON with a fixed layout, so the file is a reviewable diff and
/// its SHA-256 is the protocol fingerprint.
public enum Golden {
    public static let version = 1

    public static func write(_ model: Model) -> String {
        var out = "{\n  \"version\": \(version),\n  \"types\": ["
        for (index, declaration) in model.declarations.enumerated() {
            out += index == 0 ? "\n" : ",\n"
            out += "    {\n"
            out += "      \"name\": \(quote(declaration.name)),\n"
            out += "      \"module\": \(quote(declaration.module)),\n"
            out += "      \"kind\": \(quote(declaration.kind.rawValue))"
            switch declaration.kind {
            case .struct:
                out += ",\n      \"fields\": \(fields(declaration.fields, indent: 6))\n"
            case .stringEnum:
                out += ",\n      \"cases\": [" + declaration.cases.map { "\n        {\"name\": \(quote($0.name)), \"value\": \(quote($0.rawValue ?? $0.name))}" }
                    .joined(separator: ",") + (declaration.cases.isEmpty ? "]\n" : "\n      ]\n")
            case .enum:
                out += ",\n      \"cases\": ["
                out += declaration.cases.map { item in
                    "\n        {\n          \"name\": \(quote(item.name)),\n          \"fields\": \(fields(item.fields, indent: 10))\n        }"
                }.joined(separator: ",")
                out += declaration.cases.isEmpty ? "]\n" : "\n      ]\n"
            }
            out += "    }"
        }
        out += model.declarations.isEmpty ? "]\n}\n" : "\n  ]\n}\n"
        return out
    }

    private static func fields(_ fields: [Field], indent: Int) -> String {
        guard !fields.isEmpty else { return "[]" }
        let pad = String(repeating: " ", count: indent + 2)
        return "[" + fields.map { "\n\(pad){\"name\": \(quote($0.name)), \"type\": \(quote($0.type.spelling))}" }.joined(separator: ",")
            + "\n" + String(repeating: " ", count: indent) + "]"
    }

    static func quote(_ text: String) -> String {
        var out = "\""
        for scalar in text.unicodeScalars {
            switch scalar {
            case "\"": out += "\\\""
            case "\\": out += "\\\\"
            case "\n": out += "\\n"
            default:
                if scalar.value < 0x20 { out += String(format: "\\u%04x", scalar.value) } else { out.unicodeScalars.append(scalar) }
            }
        }
        return out + "\""
    }

    /// The protocol fingerprint: the golden's SHA-256, in hex.
    public static func fingerprint(_ golden: String) -> String {
        SHA256.hex(Array(golden.utf8))
    }
}
