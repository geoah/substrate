/** The part of the browser's Web Locks a session change takes: the one
 * exclusive lock of its name, held while `fn` runs, then let go. */
export interface SessionLocks {
  request<T>(name: string, fn: () => Promise<T>): Promise<T>
}

const tails = new Map<string, Promise<unknown>>()

/** A lock that holds within this tab alone, for a browser without Web
 * Locks (before Safari 15.4, Firefox 96 or Chrome 69) and a test
 * environment without them: this tab's changes run one at a time, but
 * another tab's may still run between a read and a write of this one's. */
export const tabLocks: SessionLocks = {
  request<T>(name: string, fn: () => Promise<T>): Promise<T> {
    const run = (tails.get(name) ?? Promise.resolve()).then(() => fn())
    const tail = run.then(
      () => undefined,
      () => undefined
    )
    tails.set(name, tail)
    void tail.then(() => {
      if (tails.get(name) === tail) tails.delete(name)
    })
    return run
  },
}

/** The browser's Web Locks, else this tab's own. Looked up at each change,
 * so a test may put its own at `navigator.locks`. */
export function sessionLocks(): SessionLocks {
  const locks = typeof navigator !== "undefined" ? navigator.locks : undefined
  if (!locks) return tabLocks
  return {
    request: <T>(name: string, fn: () => Promise<T>) =>
      locks.request(name, () => fn()) as Promise<T>,
  }
}
