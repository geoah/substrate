import SwiftUI
import WorkoutsCore

/// The body, front and back, each region shaded by its working sets in the
/// period (`StatsPresentation.regions`): the more sets, the deeper the
/// accent; a region with none stays grey. The bars below it carry the
/// numbers, so the figure is one accessibility element that names the
/// regions worked most.
struct BodyMapView: View {
    let regions: [BodyRegion: Double]

    var body: some View {
        let most = regions.values.max() ?? 0
        VStack(spacing: 12) {
            HStack(alignment: .top, spacing: 16) {
                figure(.front, title: "Front", most: most)
                figure(.back, title: "Back", most: most)
            }
            legend
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Body map of working sets")
        .accessibilityValue(summary)
    }

    private func figure(_ figure: BodyRegion.Figure, title: String, most: Double) -> some View {
        VStack(spacing: 6) {
            Canvas { context, size in
                let scale = min(size.width / BodyMapShapes.size.width, size.height / BodyMapShapes.size.height)
                let offset = CGPoint(x: (size.width - BodyMapShapes.size.width * scale) / 2, y: 0)
                let transform = CGAffineTransform(translationX: offset.x, y: offset.y).scaledBy(x: scale, y: scale)
                let shading = Self.shading(figure, regions: regions, most: most)
                for (slug, paths) in Self.paths(figure) {
                    let color =
                        shading[slug].map { Color.accentColor.opacity(0.22 + 0.78 * $0) }
                        ?? Color.secondary.opacity(Self.outline.contains(slug) ? 0.16 : 0.24)
                    for path in paths {
                        context.fill(path.applying(transform), with: .color(color))
                    }
                }
            }
            .aspectRatio(BodyMapShapes.size.width / BodyMapShapes.size.height, contentMode: .fit)
            Text(title)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
    }

    private var legend: some View {
        HStack(spacing: 8) {
            Text("Fewer sets")
            LinearGradient(colors: [Color.accentColor.opacity(0.22), Color.accentColor], startPoint: .leading, endPoint: .trailing)
                .frame(height: 8)
                .clipShape(Capsule())
            Text("More sets")
        }
        .font(.caption2)
        .foregroundStyle(.secondary)
    }

    private var summary: String {
        let worked = regions.filter { $0.value > 0 }.sorted { $0.value > $1.value }
        guard !worked.isEmpty else { return "No working sets in this period" }
        return "Most worked: " + worked.prefix(4).map(\.key.label).joined(separator: ", ")
    }

    /// Each shape's depth from 0 to 1, by the largest of the regions drawn
    /// with it (a figure's deltoid or trapezius is one region's).
    static func shading(_ figure: BodyRegion.Figure, regions: [BodyRegion: Double], most: Double) -> [String: Double] {
        guard most > 0 else { return [:] }
        var out: [String: Double] = [:]
        for (region, sets) in regions where sets > 0 {
            for shape in region.shapes where shape.figure == figure {
                out[shape.slug] = max(out[shape.slug] ?? 0, sets / most)
            }
        }
        return out
    }

    /// Head, hands and feet: drawn lighter, since nothing shades them.
    static let outline: Set<String> = ["head", "hair", "hands", "feet", "ankles", "knees"]

    private static let front = parse(BodyMapShapes.front)
    private static let back = parse(BodyMapShapes.back)

    static func paths(_ figure: BodyRegion.Figure) -> [(String, [Path])] {
        figure == .front ? front : back
    }

    private static func parse(_ shapes: [String: [String]]) -> [(String, [Path])] {
        shapes.sorted { $0.key < $1.key }.map { slug, data in (slug, data.map(path)) }
    }

    /// Reads the generator's absolute M, L, C and Z (`BodyMapShapes`).
    static func path(_ data: String) -> Path {
        var path = Path()
        var numbers: [CGFloat] = []
        var command: Character = "M"
        func flush() {
            switch command {
            case "M" where numbers.count >= 2: path.move(to: CGPoint(x: numbers[0], y: numbers[1]))
            case "L" where numbers.count >= 2: path.addLine(to: CGPoint(x: numbers[0], y: numbers[1]))
            case "C" where numbers.count >= 6:
                path.addCurve(
                    to: CGPoint(x: numbers[4], y: numbers[5]), control1: CGPoint(x: numbers[0], y: numbers[1]),
                    control2: CGPoint(x: numbers[2], y: numbers[3]))
            case "Z": path.closeSubpath()
            default: break
            }
            numbers = []
        }
        var token = ""
        for character in data {
            if character.isLetter {
                if !token.isEmpty {
                    numbers.append(CGFloat(Double(token) ?? 0))
                    token = ""
                }
                flush()
                command = character
                if command == "Z" { flush() }
            } else if character == " " {
                if !token.isEmpty {
                    numbers.append(CGFloat(Double(token) ?? 0))
                    token = ""
                }
            } else {
                token.append(character)
            }
        }
        if !token.isEmpty { numbers.append(CGFloat(Double(token) ?? 0)) }
        flush()
        return path
    }
}

#if DEBUG
#Preview {
    BodyMapView(regions: [.chest: 24, .frontDelts: 14, .triceps: 12, .quadriceps: 20, .glutes: 16, .lats: 18, .abs: 6])
        .padding()
}
#endif
