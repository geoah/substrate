#if os(macOS)
import AppKit
import SwiftUI

/// A view drawn to PNG offscreen, on the Mac, for a gallery: the kit's
/// screens under `swift test` and an app's from its UI framework's tests,
/// which run with no host app. `ImageRenderer` draws a text field or a list
/// as a placeholder, so the view is hosted in a window and that window's
/// views are cached.
@MainActor
public enum OffscreenDrawing {
    /// What the view is drawn inside.
    public enum Frame: Sendable {
        /// A phone's screen: the view alone, its controls drawn as a front
        /// window's, since an offscreen window is never key.
        case screen
        /// A Mac window: a titled window with the toolbar the view's
        /// navigation titles and items go into, drawn as a window in front
        /// of others but not focused, so a list's selection is the system's
        /// own grey rather than the accent a backdrop would blend. What the
        /// window server composites itself (a glass sidebar's rows) is not
        /// in the drawing.
        case window
    }

    /// An iPhone's screen, in points.
    public static let phone = CGSize(width: 393, height: 852)
    /// A Mac window big enough for three columns.
    public static let mac = CGSize(width: 1180, height: 760)

    /// The accent SwiftUI draws a button, a picker's choice and a link in:
    /// a test process has no app's accent and would draw them grey, as
    /// though disabled. AppKit's own controls (a switch, a sidebar's
    /// selection) read the application's activation, which an offscreen
    /// drawing does not take, and are drawn as an inactive window's.
    static let accent = Color(nsColor: .systemBlue)

    /// The PNG of `view` at `size`, in light mode, once it has settled.
    public static func png(_ view: some View, size: CGSize = phone, frame: Frame = .screen) async throws -> Data {
        _ = NSApplication.shared
        let window: NSWindow
        let drawn: NSView
        switch frame {
        case .screen:
            let hosting = NSHostingView(
                rootView:
                    view
                    .frame(width: size.width, height: size.height)
                    .background(Color(nsColor: .windowBackgroundColor))
                    .tint(Self.accent)
                    .environment(\.colorScheme, .light)
                    .environment(\.controlActiveState, .key))
            hosting.frame = CGRect(origin: .zero, size: size)
            window = NSWindow(contentRect: hosting.frame, styleMask: [.borderless], backing: .buffered, defer: false)
            window.contentView = hosting
            drawn = hosting
        case .window:
            // A hosting controller, not a hosting view: it is what hands the
            // view's toolbar and titles to the window.
            let controller = NSHostingController(rootView: view.tint(Self.accent).environment(\.colorScheme, .light))
            controller.sizingOptions = []
            window = NSWindow(
                contentRect: CGRect(origin: .zero, size: size),
                styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView], backing: .buffered,
                defer: false)
            window.contentViewController = controller
            window.setContentSize(size)
            window.toolbarStyle = .unified
            // Far off every screen: ordered in so the toolbar is laid out
            // and drawn, never seen.
            window.setFrameOrigin(NSPoint(x: -20000, y: -20000))
            window.orderFrontRegardless()
            drawn = window.contentView?.superview ?? controller.view
        }
        window.appearance = NSAppearance(named: .aqua)
        window.isReleasedWhenClosed = false
        drawn.layoutSubtreeIfNeeded()
        try await Task.sleep(for: .milliseconds(400))
        drawn.layoutSubtreeIfNeeded()
        defer {
            window.orderOut(nil)
            window.contentViewController = nil
            window.contentView = nil
        }
        guard let rep = drawn.bitmapImageRepForCachingDisplay(in: drawn.bounds) else { throw NotDrawn() }
        drawn.cacheDisplay(in: drawn.bounds, to: rep)
        guard let png = rep.representation(using: .png, properties: [:]) else { throw NotDrawn() }
        return png
    }

    /// The view drew nothing to cache.
    public struct NotDrawn: Error {}
}
#endif
