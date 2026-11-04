#if canImport(SwiftUI)
import Foundation
#if canImport(UIKit)
import UIKit
#endif

/// The device in the words an app says it in: "this iPhone", "this iPad",
/// "this Mac", lowercase where a sentence runs on ("Not kept on this Mac").
public enum DeviceWords {
    @MainActor
    public static var current: String {
        #if os(macOS)
        "this Mac"
        #else
        UIDevice.current.userInterfaceIdiom == .pad ? "this iPad" : "this iPhone"
        #endif
    }
}
#endif
