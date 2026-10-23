/** Chat messages a conversation holds until it can send them, per
 * conversation. They live outside the conversation component, which the
 * agents page remounts on every switch, and in the tab's session storage, so
 * leaving a chat and coming back (or reloading the tab) sends the message
 * instead of losing it. A store that cannot be written (private mode, a full
 * quota) falls back to memory for the page's life. */

const PREFIX = "substrate.chat.held:"
const memory = new Map<string, string>()

function storage(): Storage | undefined {
  try {
    return globalThis.sessionStorage
  } catch {
    return undefined
  }
}

/** The message held for one conversation, or null. */
export function heldMessage(key: string): string | null {
  if (!key) return null
  try {
    const stored = storage()?.getItem(PREFIX + key)
    if (stored != null) return stored
  } catch {
    // Unreadable: memory below is the only copy.
  }
  return memory.get(key) ?? null
}

/** Holds a message for one conversation, or forgets it with null. */
export function holdMessage(key: string, message: string | null): void {
  if (!key) return
  if (message === null) memory.delete(key)
  else memory.set(key, message)
  try {
    const store = storage()
    if (message === null) store?.removeItem(PREFIX + key)
    else store?.setItem(PREFIX + key, message)
  } catch {
    // Memory holds it.
  }
}
