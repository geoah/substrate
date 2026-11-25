import Foundation
import SubstrateChatCore
import SubstrateChatUI
import SubstrateKit

extension ChatHost.Parts {
    /// The device's own: the session in the Keychain item every build has
    /// kept it in, the folder every build has kept the chats in, and the
    /// device services made at launch.
    @MainActor static var device: ChatHost.Parts {
        ChatHost.Parts(
            sessions: .chat, transport: URLSessionTransport.shared, folders: ApplicationFolders(name: "SubstrateChat"),
            preferences: DefaultsPreferenceStore(defaults: .standard), notifications: ChatServices.notifications,
            background: ChatServices.background, push: ChatServices.push)
    }
}

extension ChatHost {
    /// The process's one host: the device's, but for a debug build asked
    /// for a measurement or a demo, which runs on a fixture's parts.
    static let shared: ChatHost = {
        #if DEBUG
        if let measured = Measure.parts() { return ChatHost(measured) }
        if let demo = Demo.parts() ?? Demo.session() { return ChatHost(demo) }
        #endif
        return ChatHost(.device)
    }()
}
