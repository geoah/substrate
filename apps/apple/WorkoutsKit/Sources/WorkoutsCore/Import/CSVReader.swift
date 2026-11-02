import Foundation

/// RFC 4180 text, as the apps that export a workout history write it: comma
/// separated, a field quoted when it holds a comma, a quote or a line break
/// (Hevy's notes and descriptions do), `""` for a quote inside one, CRLF or
/// LF line ends, and a leading byte-order mark tolerated.
public enum CSVReader {
    /// One record of the text, with the line it starts on (from 1), which a
    /// quoted line break makes differ from its index.
    public struct Line: Hashable, Sendable {
        public var number: Int
        public var fields: [String]
    }

    public static func lines(_ text: String) -> [Line] {
        var out: [Line] = []
        var fields: [String] = []
        var field = ""
        var quoted = false
        var afterQuote = false
        var line = 1
        var start = 1
        var scalars = text.unicodeScalars[...]
        if scalars.first == "\u{FEFF}" { scalars = scalars.dropFirst() }

        func endRecord() {
            fields.append(field)
            // A blank line is no record.
            if !(fields.count == 1 && fields[0].isEmpty) { out.append(Line(number: start, fields: fields)) }
            fields = []
            field = ""
        }

        var index = scalars.startIndex
        while index < scalars.endIndex {
            let scalar = scalars[index]
            index = scalars.index(after: index)
            if quoted {
                if scalar == "\"" {
                    if index < scalars.endIndex, scalars[index] == "\"" {
                        field.unicodeScalars.append("\"")
                        index = scalars.index(after: index)
                    } else {
                        quoted = false
                        afterQuote = true
                    }
                } else {
                    if scalar == "\n" { line += 1 }
                    field.unicodeScalars.append(scalar)
                }
                continue
            }
            switch scalar {
            case "\"" where field.isEmpty && !afterQuote:
                quoted = true
            case ",":
                fields.append(field)
                field = ""
                afterQuote = false
            case "\r":
                // CRLF ends the record at its LF; a lone CR ends it here.
                if index < scalars.endIndex, scalars[index] == "\n" { continue }
                endRecord()
                afterQuote = false
                line += 1
                start = line
            case "\n":
                endRecord()
                afterQuote = false
                line += 1
                start = line
            default:
                field.unicodeScalars.append(scalar)
            }
        }
        if quoted || !field.isEmpty || !fields.isEmpty { endRecord() }
        return out
    }
}
