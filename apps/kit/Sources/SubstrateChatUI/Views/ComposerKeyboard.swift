#if canImport(SwiftUI)
import SubstrateChatCore
import SwiftUI

#if os(macOS)
import AppKit
#else
import UIKit
#endif

// What the composer's key rule (`ComposerKeys`, in SubstrateChatCore) needs from the
// device: the keys a press carries, which device this is, and whether an
// input method is part way through a word.

extension ComposerKeys.Modifiers {
    /// The four keys the rule reads, of those a key press carries.
    init(_ held: EventModifiers) {
        self = []
        if held.contains(.command) { insert(.command) }
        if held.contains(.shift) { insert(.shift) }
        if held.contains(.option) { insert(.option) }
        if held.contains(.control) { insert(.control) }
    }
}

extension ComposerKeys.Device {
    @MainActor static var current: ComposerKeys.Device {
        #if os(macOS)
        .mac
        #else
        UIDevice.current.userInterfaceIdiom == .pad ? .pad : .phone
        #endif
    }
}

/// Whether an input method holds marked text in the field being typed in: a
/// Japanese or Chinese word before it is chosen. On a Mac the Return that
/// chooses it passes through the composer's key handling first. An iPad's
/// field has every Return but Command-Return to itself, which chooses
/// nothing, so there it is never asked.
@MainActor func composingMarkedText() -> Bool {
    #if os(macOS)
    (NSApp.keyWindow?.firstResponder as? NSTextInputClient)?.hasMarkedText() ?? false
    #else
    false
    #endif
}

extension View {
    /// On the Send button: its help and its hint name Command-Return where
    /// a hardware keyboard is expected, a Mac and an iPad.
    @ViewBuilder func sendShortcut(on device: ComposerKeys.Device) -> some View {
        if let help = ComposerKeys.sendHelp(on: device), let hint = ComposerKeys.sendHint(on: device) {
            self.help(help).accessibilityHint(hint)
        } else {
            self
        }
    }
}
#endif
