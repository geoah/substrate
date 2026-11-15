import Foundation
import SubstrateKit
import SwiftUI
import Testing

@testable import SubstrateChat

/// The rules the chat's views are held to, read from their own source (the
/// files of `Sources/App`, `Sources/Views` and `Sources/Markdown`, which the
/// test bundle carries as text). A view names what a size, a colour or a
/// motion is for; the number, the colour and the animation are a token's,
/// in `Sources/Tokens`, which is the one place the app's feel is changed.
///
/// Each rule is a pattern over a line with its comments and its string
/// literals taken out. A pattern cannot know what a number means, so the
/// few lines that hold one for another reason are listed in `allowances`,
/// each with why; an allowance no line uses fails, so the list cannot
/// outlive what it was for.
@MainActor @Suite struct SourceRules {
    struct Line: Sendable {
        var file: String
        var number: Int
        var text: String
    }

    struct Rule: Sendable {
        var name: String
        var pattern: String
        /// What to write instead.
        var instead: String
    }

    struct Allowance: Sendable {
        var file: String
        /// Text the line holds.
        var holds: String
        var rule: String
        var why: String
    }

    struct Finding: CustomStringConvertible {
        var line: Line
        var rule: Rule

        var description: String { "\(line.file):\(line.number): \(rule.name): \(line.text.trimmingCharacters(in: .whitespaces)) — \(rule.instead)" }
    }

    // MARK: The rules

    /// A number that stands alone and is not zero: not a part of a name,
    /// not a factor (`width * 0.6`, `inset / 2`), which is a proportion and
    /// no size, and not what something is counted or compared with
    /// (`level <= 2`, `count - 1`).
    static let nonZero = #"(?<![\w.]|[*/<>=+\-] )-?(?!0(?:\.0+)?(?![\d.]))\d+(?:\.\d+)?(?![\w.]| [*/])"#
    /// The same, other than zero and one: an opacity that is neither gone
    /// nor whole.
    static let fraction = #"(?<![\w.]|[*/<>=+\-] )(?!(?:0|1)(?:\.0+)?(?![\d.]))\d+(?:\.\d+)?(?![\w.]| [*/])"#

    static let sizes: [Rule] = [
        Rule(
            name: "a literal size",
            pattern:
                #"\b(?:spacing|horizontalSpacing|verticalSpacing|minLength|cornerRadius|lineWidth|width|height|minWidth|maxWidth|idealWidth|minHeight|maxHeight|idealHeight|min|max|ideal|top|leading|bottom|trailing|radius): "#
                + nonZero,
            instead: "name it in ChatMetrics (ChatSpace, ChatSize, ChatRadius, ChatLine)"),
        Rule(
            name: "a literal size",
            pattern: #"\.(?:padding|frame|offset|defaultSize)\((?:[^()]|\([^()]*\))*?"# + nonZero,
            instead: "name it in ChatMetrics (ChatSpace, ChatSize, ChatRadius, ChatLine)"),
        Rule(
            name: "a literal size behind a constant",
            pattern: #"\b(?:let|var) \w+(?:: [\w.<>?]+)? = -?(?!0(?:\.0+)?\s*$)\d+(?:\.\d+)?\s*$"#,
            instead: "a size is a token in ChatMetrics, not a constant in a view's file"),
        Rule(name: "a literal size that scales", pattern: #"@ScaledMetric[^=]*= *\d"#, instead: "read a ChatSize through @Scaled"),
        Rule(name: "a literal opacity", pattern: #"\.opacity\((?:[^()]|\([^()]*\))*?"# + fraction, instead: "name it in ChatColours"),
    ]

    static let type: [Rule] = [
        Rule(name: "a literal font", pattern: #"\.font\(\s*\."#, instead: "name its role in ChatType"),
        Rule(name: "a literal font", pattern: #"\bFont\.|\.system\((?:\.|size:)"#, instead: "name its role in ChatType"),
        Rule(name: "a literal weight", pattern: #"\.(?:fontWeight|weight)\(\s*\.|\.bold\(\)|\.italic\(\)"#, instead: "name it in ChatType"),
    ]

    static let colours: [Rule] = [
        Rule(name: "a literal colour", pattern: #"\bColor\.(?!clear\b)\w+|\bColor\(|0x[0-9A-Fa-f]{6}\b"#, instead: "name it in ChatColours"),
        Rule(
            name: "a literal colour",
            pattern:
                #"(?<![\w)])\.(?:primary|secondary|tertiary|quaternary|red|orange|yellow|green|mint|teal|cyan|blue|indigo|purple|pink|brown|white|black|gray|accentColor)\b(?!\()"#,
            instead: "name an ink and its surface (`ink(_:on:)`) or a fill of ChatColours"),
        Rule(name: "a literal colour", pattern: #"(?<![\w)])\.tint\b(?!\()"#, instead: "the accent as words is `ink(.accent, on:)`"),
        Rule(name: "a literal colour", pattern: #"(?<![\w)])\.fill\.\w+"#, instead: "a fill is one of ChatColours'"),
        Rule(
            name: "words coloured without their surface", pattern: #"\.foregroundStyle\(|\.foregroundColor\b"#,
            instead: "`ink(_:on:)` says what the words stand on, which the pairs are checked by"),
    ]

    static let motion: [Rule] = [
        Rule(
            name: "motion outside the policy",
            pattern:
                #"\bwithAnimation\b|\bwithTransaction\b|\.animation\(|\.transition\(|\.contentTransition\(|\.symbolEffect\(|\brepeatForever\b|\bAnimation\.|\.phaseAnimator\(|\.keyframeAnimator\(|\.matchedGeometryEffect\(|accessibilityReduceMotion"#,
            instead: "say what kind of motion it is through ChatMotion (`motion`, `arrives`, `changes`, `repeats`, `pulses`, `ChatMotion.animate`)"),
        Rule(
            name: "motion outside the policy",
            pattern: #"(?<![\w)])\.(?:snappy|spring|bouncy|smooth|easeIn|easeOut|easeInOut|interactiveSpring|interpolatingSpring)\b"#,
            instead: "an animation is a kind's, in ChatMotion"),
        Rule(
            name: "a literal duration", pattern: #"\bduration: \d|\.(?:milliseconds|seconds|nanoseconds)\(\s*\d|addingTimeInterval\(\s*\d"#,
            instead: "how long a motion lasts is ChatMotion's"),
    ]

    /// The lines a pattern would flag for a reason that is none of the
    /// rules'. Short, and each argued.
    static let allowances: [Allowance] = [
        Allowance(
            file: "FailureView.swift", holds: "Color(palette.", rule: "a literal colour",
            why: "a failure's colours are ChatKit's FailurePalette, opaque values of its own that FailureTests holds to 4.5:1"),
        Allowance(
            file: "FailureView.swift", holds: "Color(enabled ? palette.", rule: "a literal colour",
            why: "the same palette, for a button that can and cannot be pressed"),
        Allowance(
            file: "FailureView.swift", holds: ".foregroundStyle(Color(", rule: "words coloured without their surface",
            why: "the palette is the ink and its fill together, and FailureTests holds each pair of it"),
        Allowance(
            file: "AgentPill.swift", holds: ".foregroundStyle(colours.ink)", rule: "words coloured without their surface",
            why: "an agent's ink is made from its id for every surface it is drawn on, and held for every hue by ColourContrast"),
        Allowance(
            file: "ConversationView.swift", holds: "reach.moving = Date().addingTimeInterval(0.45)", rule: "a literal duration",
            why: "a wait of the follow rule's own (how long a move to the end is left alone), not a motion: TranscriptScroll's"),
        Allowance(
            file: "ConversationView.swift", holds: "try? await Task.sleep(for: .milliseconds(", rule: "a literal duration",
            why: "TranscriptScroll's waits before it asks the scroll view again: its rule's, and no motion"),
        Allowance(
            file: "RootView.swift", holds: "try? await Task.sleep(for: .milliseconds(80))", rule: "a literal duration",
            why: "how long a Mac window that is not the key one waits before it takes a route: no motion"),
        Allowance(
            file: "SelectTextSheet.swift", holds: ".foregroundColor: ", rule: "words coloured without their surface",
            why: "the system's text view takes attributes, not styles: its two colours are the label and the quiet ink, on the sheet's page"),
        Allowance(
            file: "CodeBlockView.swift", holds: ".foregroundColor = colours[run.token]", rule: "words coloured without their surface",
            why: "a token's colour on the code block's fill, each of which ChatColours.codePairs lists for ColourContrast"),
        Allowance(
            file: "Intents.swift", holds: "ShortcutTileColor = .purple", rule: "a literal colour",
            why: "the Shortcuts app's tile, chosen from the system's own closed set: no colour of the app's can be given"),
    ]

    // MARK: Reading

    final class InBundle {}

    /// The source as statements: a call written over several lines is
    /// read as the one call it is, at the line it starts on, so a number
    /// or an ink on a line of its own inside a call's parentheses is read
    /// with the call.
    static func lines() throws -> [Line] {
        var out: [Line] = []
        var file: [Line] = []
        for line in try physicalLines() {
            if let last = file.last, last.file != line.file {
                out += statements(file)
                file = []
            }
            file.append(line)
        }
        return out + statements(file)
    }

    /// Joins each line that ends inside a call's parentheses with the
    /// lines that close it.
    static func statements(_ lines: [Line]) -> [Line] {
        var out: [Line] = []
        var open: Line?
        var depth = 0
        for line in lines {
            let code = code(line.text)
            if var joined = open {
                joined.text += " " + code.trimmingCharacters(in: .whitespaces)
                open = joined
            } else {
                open = Line(file: line.file, number: line.number, text: code)
            }
            depth += code.count { $0 == "(" } - code.count { $0 == ")" }
            // A new file starts over, whatever the last one left open.
            if depth <= 0, let joined = open {
                out.append(joined)
                (open, depth) = (nil, 0)
            }
        }
        if let open { out.append(open) }
        return out
    }

    static func physicalLines() throws -> [Line] {
        let bundle = Bundle(for: InBundle.self)
        var out: [Line] = []
        for folder in ["App", "Views", "Markdown"] {
            let url = try #require(bundle.url(forResource: folder, withExtension: nil), "the test bundle carries Sources/\(folder)")
            let files = try FileManager.default.contentsOfDirectory(at: url, includingPropertiesForKeys: nil).filter { $0.pathExtension == "swift" }
            for file in files.sorted(by: { $0.lastPathComponent < $1.lastPathComponent }) {
                let text = try String(contentsOf: file, encoding: .utf8)
                var lines: [Line] = []
                for (index, line) in text.split(separator: "\n", omittingEmptySubsequences: false).enumerated() {
                    lines.append(Line(file: file.lastPathComponent, number: index + 1, text: String(line)))
                }
                // Joined a file at a time: a call never runs from one
                // file into the next.
                out += lines
            }
        }
        return out
    }

    /// A line as the rules read it: its string literals emptied, its
    /// comment cut.
    static func code(_ text: String) -> String {
        var out = text.replacing(/"(?:[^"\\]|\\.)*"/, with: "\"\"")
        if let comment = out.range(of: "//") { out = String(out[..<comment.lowerBound]) }
        return out
    }

    static func findings(_ rules: [Rule], in lines: [Line], used: inout Set<Int>) throws -> [Finding] {
        var out: [Finding] = []
        for rule in rules {
            for line in lines {
                guard try matches(rule, code(line.text)) else { continue }
                if let allowed = allowances.firstIndex(where: { $0.file == line.file && $0.rule == rule.name && line.text.contains($0.holds) }) {
                    used.insert(allowed)
                    continue
                }
                out.append(Finding(line: line, rule: rule))
            }
        }
        return out
    }

    /// Foundation's regular expressions, which read a lookbehind: the
    /// rules tell a number that stands alone from one inside a name.
    static func matches(_ rule: Rule, _ code: String) throws -> Bool {
        let pattern = try NSRegularExpression(pattern: rule.pattern)
        return pattern.firstMatch(in: code, range: NSRange(code.startIndex..., in: code)) != nil
    }

    static func report(_ findings: [Finding]) -> Comment {
        Comment(rawValue: "\n" + findings.map(\.description).joined(separator: "\n"))
    }

    // MARK: The tests

    @Test func theBundleCarriesTheViews() throws {
        let files = Set(try Self.lines().map(\.file))
        #expect(files.isSuperset(of: ["TurnView.swift", "ComposerBar.swift", "MarkdownView.swift", "SubstrateChatApp.swift"]))
        #expect(!files.contains("ChatMetrics.swift"), "the tokens are where the numbers are, and are not read")
    }

    @Test func noViewHoldsASizeAFontOrAColourOfItsOwn() throws {
        var used: Set<Int> = []
        let lines = try Self.lines()
        let found = try Self.findings(Self.sizes + Self.type + Self.colours, in: lines, used: &used)
        #expect(found.isEmpty, Self.report(found))
    }

    @Test func everyMotionGoesThroughThePolicy() throws {
        var used: Set<Int> = []
        let found = try Self.findings(Self.motion, in: try Self.lines(), used: &used)
        #expect(found.isEmpty, Self.report(found))
    }

    @Test func everyAllowanceIsUsedAndArgued() throws {
        var used: Set<Int> = []
        _ = try Self.findings(Self.sizes + Self.type + Self.colours + Self.motion, in: try Self.lines(), used: &used)
        for (index, allowance) in Self.allowances.enumerated() {
            #expect(used.contains(index), "no line of \(allowance.file) holds `\(allowance.holds)` any more: take the allowance out")
            #expect(allowance.why.count > 30, "\(allowance.file): an allowance says why")
        }
        #expect(Self.allowances.count <= 12, "the list is short: a thirteenth allowance is a token to make")
    }

    /// The rules catch what they are for, and leave what is a token's.
    @Test func theRulesReadWhatTheyAreFor() throws {
        let bad = [
            ".padding(12)", ".padding(.horizontal, 18)", "VStack(alignment: .leading, spacing: 6) {", ".frame(width: 22)",
            ".frame(minWidth: 40, minHeight: 40)", "Spacer(minLength: 48)", ".rect(cornerRadius: 16)", "strokeBorder(x, lineWidth: 0.5)",
            ".padding(.vertical, picker ? 0 : 3)", ".opacity(turn.pending ? 0.7 : 1)", ".opacity(0.35)", "static let inset: CGFloat = 12",
            "@ScaledMetric(relativeTo: .footnote) private var window = 58.0", ".font(.caption)", ".font(.subheadline.weight(.semibold))",
            "Text(name).fontWeight(.semibold)", "let font = Font.system(.callout, design: .monospaced)", ".foregroundStyle(.secondary)",
            ".foregroundStyle(ScreenTone.critical.textColor)", ".fill(.quaternary)", ".background(.fill.tertiary, in: .capsule)",
            ".tint(.orange)", "Color.accentColor.opacity(x)", "Colour(light: 0x6A3FE0, dark: 0xA286FF)", ".foregroundStyle(.tint)",
            "withAnimation(.snappy) { open.toggle() }", ".animation(.snappy, value: empty)", ".transition(.opacity)",
            ".contentTransition(.numericText())", ".symbolEffect(.pulse, isActive: on)", "withAnimation(.easeInOut(duration: 0.9).repeatForever()) {",
            "@Environment(\\.accessibilityReduceMotion) private var reduceMotion", "try? await Task.sleep(for: .milliseconds(320))",
        ]
        let good = [
            ".padding(ChatSpace.inset)", "VStack(alignment: .leading, spacing: 0) {", "Spacer(minLength: 0)",
            ".frame(maxWidth: .infinity, alignment: .leading)",
            ".padding(.vertical, ChatSpace.lines / 2)", ".frame(width: geo.size.width * 0.6)", ".opacity(turn.pending ? ChatColours.pending : 1)",
            ".opacity(selected ? 0 : 1)", "@State private var phase = 0", ".font(ChatType.caption)", ".ink(.quiet, on: .page)",
            ".tint(ScreenTone.caution.fillColor)", ".background(ChatColours.pillStyle, in: .capsule)", "ChatMotion.animate(.fold) { open.toggle() }",
            ".motion(.status, value: empty)", ".arrives(.grow)", "ToolbarItem(placement: .primaryAction) {", ".buttonStyle(.borderedProminent)",
            "LinearGradient(colors: [.clear, ChatColours.shimmer, .clear], startPoint: .leading, endPoint: .trailing)", ".lineLimit(1 ... 8)",
            "Text(\"Costs 0.5 and .red, .font(.caption)\")", "// .padding(12) was here",
        ]
        let rules = Self.sizes + Self.type + Self.colours + Self.motion
        func flagged(_ text: String) throws -> Bool {
            let code = Self.code(text)
            return try rules.contains { try Self.matches($0, code) }
        }
        for line in bad { #expect(try flagged(line), "not caught: \(line)") }
        for line in good { #expect(try !flagged(line), "caught, and is a token's: \(line)") }
    }

    // MARK: Words and what they stand on

    /// Every `ink(_:on:)` the views write, as the pairs it names: each ink
    /// it may draw on each surface it says. An ink and a surface chosen by
    /// the same condition (`a ? x : y` for both) pair branch by branch.
    static func uses(in lines: [Line]) throws -> (found: Set<ChatColours.Use>, unread: [Line], agents: [(Line, ChatColours.Surface)]) {
        var found: Set<ChatColours.Use> = []
        var unread: [Line] = []
        var agents: [(Line, ChatColours.Surface)] = []
        let call = /\.(ink|tinted|inAgentInk)\(((?:[^()]|\((?:[^()]|\([^()]*\))*\))*)\)/
        for line in lines {
            let code = code(line.text)
            // Every call that names an ink is read, or the line is
            // reported: one this pattern cannot take apart is no pairing
            // anybody checked.
            if code.matches(of: /\.(?:ink|tinted|inAgentInk)\(/).count != code.matches(of: call).count { unread.append(line) }
            for match in code.matches(of: call) {
                let arguments = String(match.2)
                guard let split = arguments.range(of: ", on: ") else {
                    unread.append(line)
                    continue
                }
                var surfaces = String(arguments[split.upperBound...])
                if let selected = surfaces.range(of: ", selected: ") { surfaces = String(surfaces[..<selected.lowerBound]) }
                let inks = String(arguments[..<split.lowerBound])
                if match.1 == "inAgentInk" {
                    let named = Self.surfaces(surfaces)
                    if named.isEmpty { unread.append(line) }
                    agents += named.map { (line, $0) }
                    continue
                }
                let pairs: [(String, String)]
                if let ink = ternary(inks), let surface = ternary(surfaces), ink.condition == surface.condition {
                    pairs = [(ink.then, surface.then), (ink.otherwise, surface.otherwise)]
                } else {
                    pairs = [(inks, surfaces)]
                }
                for (inks, surfaces) in pairs {
                    let (inks, surfaces) = (Self.inks(inks), Self.surfaces(surfaces))
                    if inks.isEmpty || surfaces.isEmpty { unread.append(line) }
                    for ink in inks { for surface in surfaces { found.insert(ChatColours.Use(ink, on: surface)) } }
                }
            }
        }
        return (found, unread, agents)
    }

    static func ternary(_ text: String) -> (condition: String, then: String, otherwise: String)? {
        guard let match = text.firstMatch(of: /^(.+?) \? (.+?) : ([^?]+)$/) else { return nil }
        return (String(match.1), String(match.2), String(match.3))
    }

    static func inks(_ text: String) -> [ChatColours.Ink] {
        text.matches(of: /\.(label|quiet|accent|onFill|faint)\b|\.(tone|mark)\(\.(\w+)\)/).compactMap { match in
            if let tone = match.3 { return ScreenTone(rawValue: String(tone)).map { match.2 == "mark" ? .mark($0) : .tone($0) } }
            switch match.1 {
            case "label": return .label
            case "quiet": return .quiet
            case "accent": return .accent
            case "onFill": return .onFill
            case "faint": return .faint
            default: return nil
            }
        }
    }

    static func surfaces(_ text: String) -> [ChatColours.Surface] {
        text.matches(of: /\.(\w+)\b/).compactMap { ChatColours.Surface(rawValue: String($0.1)) }
    }

    /// A view draws no ink on a surface it was not checked against, and
    /// the list holds nothing no view draws: `ChatColours.named` is the
    /// pairings the source names, exactly. `ColourContrast` holds each of
    /// them, on every fill its surface has, to WCAG's ratio.
    @Test func everyInkAViewDrawsIsOnASurfaceItIsListedFor() throws {
        let lines = try Self.lines()
        let (found, unread, agents) = try Self.uses(in: lines)
        #expect(unread.isEmpty, "an ink or a surface the rule cannot read:\n\(unread.map { "\($0.file):\($0.number): \($0.text)" }.joined(separator: "\n"))")
        #expect(found.count > 30)
        for use in found.subtracting(ChatColours.uses).sorted(by: { "\($0)" < "\($1)" }) {
            let at = lines.first { $0.text.contains(".\(use.surface.rawValue)") && Self.inks(Self.code($0.text)).contains(use.ink) }
            Issue.record("\(use.ink) is drawn on \(use.surface) and is not in ChatColours.named: \(at.map { "\($0.file):\($0.number)" } ?? "")")
        }
        for use in ChatColours.named.subtracting(found).sorted(by: { "\($0)" < "\($1)" }) {
            Issue.record("ChatColours.named lists \(use.ink) on \(use.surface), which no view draws: take it out")
        }
        // An agent's ink is made to read on four backgrounds, and no other.
        let agentSurfaces: Set<ChatColours.Surface> = [.page, .list, .field, .form]
        #expect(ChatColours.agentBackgrounds.count == agentSurfaces.count)
        for (line, surface) in agents {
            #expect(agentSurfaces.contains(surface), "\(line.file):\(line.number): an agent's ink is not made for \(surface)")
        }
    }

    /// The same rule, on views it must refuse and take.
    @Test func thePairsRuleReadsACall() throws {
        func read(_ text: String) throws -> Set<ChatColours.Use> { try Self.uses(in: [Line(file: "x", number: 1, text: text)]).found }
        #expect(try read("Text(a).ink(.quiet, on: .page)") == [ChatColours.Use(.quiet, on: .page)])
        #expect(try read(".ink(.accent, on: .page, .card, .table)").count == 3)
        #expect(
            try read(".ink(card.failed ? .tone(.critical) : .label, on: .card)") == [
                ChatColours.Use(.tone(.critical), on: .card), ChatColours.Use(.label, on: .card),
            ])
        #expect(
            try read(".ink(tint == nil ? .label : .onFill, on: tint == nil ? .field : .fill)") == [
                ChatColours.Use(.label, on: .field), ChatColours.Use(.onFill, on: .fill),
            ])
        #expect(try read(".ink(.quiet, on: .list, selected: selected)") == [ChatColours.Use(.quiet, on: .list)])
        #expect(try read(".tinted(.accent, on: .list)") == [ChatColours.Use(.accent, on: .list)])
        // White on the page is no pairing anybody listed.
        #expect(!ChatColours.uses.contains(ChatColours.Use(.onFill, on: .page)))
        #expect(try !Self.uses(in: [Line(file: "x", number: 1, text: ".ink(colour, on: .page)")]).unread.isEmpty)
        // A call over several lines is the call it is: white on the page,
        // written a word to a line, is read and is not a listed pairing.
        let spread = Self.statements(
            ["Text(words)", "    .ink(", "        .onFill, on: .page", "    )", "    .padding(", "        12)"].enumerated().map {
                Line(file: "x", number: $0.offset + 1, text: $0.element)
            })
        let read = try Self.uses(in: spread)
        #expect(read.found == [ChatColours.Use(.onFill, on: .page)] && read.unread.isEmpty)
        #expect(!ChatColours.uses.contains(ChatColours.Use(.onFill, on: .page)))
        var used: Set<Int> = []
        #expect(try Self.findings(Self.sizes, in: spread, used: &used).map(\.line.number) == [5], "a size on the line after its call is read with it")
        // And one whose parentheses never close on the line is reported.
        #expect(try !Self.uses(in: [Line(file: "x", number: 1, text: "Text(a).ink(.quiet, on: .page")]).unread.isEmpty)
    }

    // MARK: The policy itself

    @Test func nothingMovesUnderReduceMotion() {
        for kind in ChatMotion.Kind.allCases {
            #expect(ChatMotion.animation(kind, reduced: true) == nil, "\(kind)")
            #expect(ChatMotion.animation(kind, reduced: false) != nil, "\(kind)")
        }
        #expect(Set(ChatMotion.Kind.allCases.filter(\.repeats)) == [.pulse, .sweep, .beat])
    }

    /// A change is still made with less motion, at once; a loop is never
    /// started.
    @Test func aChangeIsMadeWithNoMotionAndALoopIsNotStarted() {
        let before = ChatMotion.stilled
        defer { ChatMotion.stilled = before }
        ChatMotion.stilled = true
        var folded = false
        var pulsing = false
        ChatMotion.animate(.fold) { folded = true }
        ChatMotion.animate(.pulse) { pulsing = true }
        #expect(folded)
        #expect(!pulsing)
    }
}
