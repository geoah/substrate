// Draws the app icon: web/console/public/substrate.svg's three layers on a
// full-bleed square, which each platform masks to its own shape.
// Usage: swift scripts/icon.swift <out.png> [#rrggbb background]
import AppKit

let size = 1024.0
let unit = size / 32
let rep = NSBitmapImageRep(
    bitmapDataPlanes: nil, pixelsWide: Int(size), pixelsHigh: Int(size),
    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
    colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
let ctx = NSGraphicsContext.current!.cgContext
// SVG coordinates run downwards.
ctx.translateBy(x: 0, y: size)
ctx.scaleBy(x: 1, y: -1)

// The console's blue unless another background is named: Substrate Sync's
// icon is the same mark on its own colour.
let background = CommandLine.arguments.count > 2
    ? UInt32(CommandLine.arguments[2].trimmingCharacters(in: CharacterSet(charactersIn: "#")), radix: 16)!
    : 0x2f6ea6
func channel(_ shift: UInt32) -> CGFloat { CGFloat((background >> shift) & 0xff) / 255 }
ctx.setFillColor(NSColor(srgbRed: channel(16), green: channel(8), blue: channel(0), alpha: 1).cgColor)
ctx.fill(CGRect(x: 0, y: 0, width: size, height: size))

func point(_ x: Double, _ y: Double) -> CGPoint { CGPoint(x: x * unit, y: y * unit) }
ctx.setStrokeColor(NSColor.white.cgColor)
ctx.setLineWidth(2.2 * unit)
ctx.setLineJoin(.round)
ctx.setLineCap(.round)
let top = CGMutablePath()
top.addLines(between: [point(16, 6), point(26, 11), point(16, 16), point(6, 11)])
top.closeSubpath()
ctx.addPath(top)
for y in [16.0, 21.0] {
    let layer = CGMutablePath()
    layer.addLines(between: [point(6, y), point(16, y + 5), point(26, y)])
    ctx.addPath(layer)
}
ctx.strokePath()

NSGraphicsContext.current = nil
let png = rep.representation(using: .png, properties: [:])!
try! png.write(to: URL(fileURLWithPath: CommandLine.arguments[1]))
