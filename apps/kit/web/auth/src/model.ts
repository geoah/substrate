import type { SubstrateClient } from "@substrate/client"
import type { AppManifest } from "./manifest"
import type { Session } from "./session"
import { checkAppStatus, type AppStatus } from "./status"

export interface AppStatusState {
  /** The last check's answer; null until one answered. */
  status: AppStatus | null
  checking: boolean
}

/** One session's status, kept current: checked when asked, at most once per
 * `interval` unless forced, and a check that answers after a newer one
 * started, or after `close`, answers nobody (the kit's AppStatusModel). */
export class AppStatusModel {
  private readonly opts: {
    manifest: AppManifest
    session: Session
    client?: SubstrateClient
    interval: number
    now: () => number
    check: typeof checkAppStatus
  }
  private state: AppStatusState = { status: null, checking: false }
  private listeners = new Set<() => void>()
  private lastRun?: number
  private generation = 0

  constructor(opts: {
    manifest: AppManifest
    session: Session
    client?: SubstrateClient
    /** In ms; five minutes unless named. */
    interval?: number
    now?: () => number
    check?: typeof checkAppStatus
  }) {
    this.opts = {
      ...opts,
      interval: opts.interval ?? 300_000,
      now: opts.now ?? Date.now,
      check: opts.check ?? checkAppStatus,
    }
  }

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  getSnapshot = (): AppStatusState => this.state

  private set(next: Partial<AppStatusState>): void {
    this.state = { ...this.state, ...next }
    for (const fn of this.listeners) fn()
  }

  /** Checks again, unless one ran within the interval. A forced check while
   * one runs starts another, whose answer stands in place of the first. */
  async refresh(force = false): Promise<void> {
    const now = this.opts.now()
    if (!force && this.state.checking) return
    if (
      !force &&
      this.lastRun !== undefined &&
      now - this.lastRun < this.opts.interval
    )
      return
    const run = ++this.generation
    this.lastRun = now
    this.set({ checking: true })
    try {
      const status = await this.opts.check({
        manifest: this.opts.manifest,
        session: this.opts.session,
        client: this.opts.client,
        now: this.opts.now,
      })
      if (run === this.generation) this.set({ status, checking: false })
    } catch {
      if (run === this.generation) this.set({ checking: false })
    }
  }

  /** Forgets any check still running: the session is gone. */
  close(): void {
    this.generation++
    this.listeners.clear()
  }
}
