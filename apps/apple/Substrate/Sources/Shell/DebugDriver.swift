#if DEBUG
import Foundation
import WebKit

#if os(macOS)
import AppKit
#else
import UIKit
#endif

/// A debug build's remote control, for checking the shell end to end from a
/// terminal without a person at the window. It watches `automation/` in the
/// app's own temporary directory (inside the sandbox container on macOS, the
/// app's data container on a simulator): a `eval.js` there is run as an async
/// function body in the page and its answer written to `result.json`; a
/// `snap` there is answered with `snap.png`. Release builds carry none of it.
@MainActor
final class DebugDriver {
    private let webView: WKWebView
    private let folder = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("automation")
    private var timer: Timer?

    init(webView: WKWebView) {
        self.webView = webView
        try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        timer = Timer.scheduledTimer(withTimeInterval: 0.3, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.poll() }
        }
    }

    func stop() { timer?.invalidate() }

    private func poll() {
        let fm = FileManager.default
        let script = folder.appendingPathComponent("eval.js")
        if let source = try? String(contentsOf: script, encoding: .utf8) {
            try? fm.removeItem(at: script)
            Task { await run(source) }
        }
        let snap = folder.appendingPathComponent("snap")
        if fm.fileExists(atPath: snap.path) {
            try? fm.removeItem(at: snap)
            webView.takeSnapshot(with: nil) { [folder] image, _ in
                guard let image, let png = Self.png(image) else { return }
                try? png.write(to: folder.appendingPathComponent("snap.png"))
            }
        }
    }

    private func run(_ source: String) async {
        var out: [String: Any] = [:]
        do {
            let value = try await webView.callAsyncJavaScript(source, contentWorld: .page)
            out["value"] = value.map { "\($0)" } ?? NSNull()
        } catch {
            out["error"] = "\(error)"
        }
        let data = (try? JSONSerialization.data(withJSONObject: out)) ?? Data()
        try? data.write(to: folder.appendingPathComponent("result.json"))
    }

    #if os(macOS)
    private static func png(_ image: NSImage) -> Data? {
        guard let tiff = image.tiffRepresentation, let rep = NSBitmapImageRep(data: tiff) else { return nil }
        return rep.representation(using: .png, properties: [:])
    }
    #else
    private static func png(_ image: UIImage) -> Data? { image.pngData() }
    #endif
}
#endif
