import Foundation
import SubstrateAPI
import SubstrateCore

/// How an app that keeps conversations of its own (one for a day, one for
/// each of its things) finds one again. The app gives the conversation an
/// id of its own choosing, the same on every device of the person's, and
/// the conversation's opening message is sent under it
/// (`ChatApp.Command.newChatAs`). The server writes the message's row under
/// that id, and the row names its thread: so the thread is found by reading
/// one row, with nothing kept on the device and no record of the app's own.
///
/// The id is any legal record id: letters, digits and `. _ ~ : @ / -`,
/// starting with a letter or a digit, at most 128 characters.
public enum ChatOpening {
    public enum Found: Equatable, Sendable {
        /// The conversation was opened, in this thread.
        case thread(String)
        /// No message was sent under the id: the conversation is not open.
        case none
        /// The row could not be read (no network, a refusal): not known.
        case unknown
    }

    /// The thread the message sent under `id` is in. A message that was
    /// deleted, or is not one somebody sent, opens nothing.
    public static func thread(openedBy id: String, _ client: SubstrateClient) async -> Found {
        do {
            guard let sent = try await ChatAPI.sent(client, messageID: id) else { return .none }
            return .thread(sent.thread)
        } catch {
            return .unknown
        }
    }
}
