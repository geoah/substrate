#if os(Android)
import Foundation
import SubstrateJNI

/// The app's roots: the library the app builds names a C function
/// `substrate_bridge_roots(void *registry)` (`@_cdecl`) that registers them,
/// and `start` calls it before it answers. It is found in this library
/// itself, by `dladdr` on one of the bridge's own symbols, because Android
/// loads an app's library outside the global namespace `RTLD_DEFAULT` reads.
enum AppRoots {
    typealias Install = @convention(c) (UnsafeMutableRawPointer) -> Void

    static func install(into registry: RootRegistry) {
        var info = Dl_info()
        let marker: @convention(c) () -> Void = substrate_bridge_marker
        guard dladdr(unsafeBitCast(marker, to: UnsafeRawPointer.self), &info) != 0, let path = info.dli_fname,
              let library = dlopen(path, RTLD_NOW | RTLD_NOLOAD)
        else { return }
        defer { dlclose(library) }
        guard let symbol = dlsym(library, "substrate_bridge_roots") else { return }
        let install = unsafeBitCast(symbol, to: Install.self)
        install(Unmanaged.passUnretained(registry).toOpaque())
    }
}

@_cdecl("substrate_bridge_marker")
public func substrate_bridge_marker() {}
#endif

public extension RootRegistry {
    /// The registry `substrate_bridge_roots` is handed.
    static func from(_ pointer: UnsafeMutableRawPointer) -> RootRegistry {
        Unmanaged<RootRegistry>.fromOpaque(pointer).takeUnretainedValue()
    }
}
