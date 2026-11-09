// The runs of this page: one at a time per import, each under a Web Lock
// named for the import so two tabs of one browser never contend, kept here
// so a run outlives the screen that started it and every screen shows its
// progress.

import { uuidv7 } from "@substrate/client"
import {
  resumeImport,
  undoImport,
  type Clock,
  type RunOutcome,
  type RunProgress,
  type RunnerStore,
} from "./runner"

export interface RunState {
  running: boolean
  mode?: "resume" | "undo"
  progress?: RunProgress
  /** How the last run in this page ended. */
  outcome?: RunOutcome
}

const IDLE: RunState = { running: false }

const DEVICE_KEY = "healthspan.device"

/** This browser's id for the lease, kept so a reload resumes as the same
 * runner; a page that cannot keep it gets one of its own. */
export function deviceId(): string {
  try {
    const kept = localStorage.getItem(DEVICE_KEY)
    if (kept) return kept
    const id = uuidv7()
    localStorage.setItem(DEVICE_KEY, id)
    return id
  } catch {
    return pageDevice
  }
}
const pageDevice = uuidv7()

interface Entry {
  state: RunState
  controller?: AbortController
  promise?: Promise<RunOutcome>
}

export class RunRegistry {
  private readonly entries = new Map<string, Entry>()
  private readonly listeners = new Set<() => void>()
  private readonly labs = new Map<string, string>()

  constructor(
    private readonly store: RunnerStore,
    private readonly opts: { device: string; clock?: Clock; lockPrefix: string }
  ) {}

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  state = (id: string): RunState => this.entries.get(id)?.state ?? IDLE

  /** The lab's name of an import planned in this page, which its setup
   * names the lab's source with. */
  rememberLab(id: string, lab: string): void {
    this.labs.set(id, lab)
  }

  private set(id: string, state: RunState) {
    const e = this.entries.get(id) ?? { state }
    e.state = state
    this.entries.set(id, e)
    for (const fn of this.listeners) fn()
  }

  private lock<T>(
    id: string,
    wait: boolean,
    fn: () => Promise<T>,
    busy: () => T
  ): Promise<T> {
    const locks =
      typeof navigator !== "undefined"
        ? (navigator as { locks?: LockManager }).locks
        : undefined
    if (!locks) return fn()
    const name = `${this.opts.lockPrefix}${id}`
    if (wait) return locks.request(name, () => fn())
    return locks.request(name, { ifAvailable: true }, (lock) =>
      lock ? fn() : busy()
    )
  }

  private start(
    id: string,
    mode: "resume" | "undo",
    run: typeof resumeImport
  ): Promise<RunOutcome> {
    const controller = new AbortController()
    const go = () =>
      run(id, {
        store: this.store,
        device: this.opts.device,
        clock: this.opts.clock,
        signal: controller.signal,
        labName: this.labs.get(id),
        onProgress: (progress) =>
          this.set(id, { running: true, mode, progress }),
      })
    this.set(id, { running: true, mode })
    const promise = this.lock(id, mode === "undo", go, () => ({
      state: "failed" as const,
      words: "Another tab of this browser is running it.",
    })).then(
      (outcome) => {
        this.set(id, { running: false, mode, outcome })
        return outcome
      },
      (e: unknown) => {
        const outcome: RunOutcome = { state: "failed", words: String(e) }
        this.set(id, { running: false, mode, outcome })
        return outcome
      }
    )
    const e = this.entries.get(id)!
    e.controller = controller
    e.promise = promise
    return promise
  }

  /** Runs an import from where it stands; one already running here is
   * joined. */
  resume(id: string): Promise<RunOutcome> {
    const e = this.entries.get(id)
    if (e?.state.running && e.promise) return e.promise
    return this.start(id, "resume", resumeImport)
  }

  /** Undoes an import: a run of it in this page is stopped first, and the
   * undo then waits for the lock another tab holds. */
  async undo(id: string): Promise<RunOutcome> {
    const e = this.entries.get(id)
    if (e?.state.running && e.promise) {
      if (e.state.mode === "undo") return e.promise
      e.controller?.abort()
      await e.promise
    }
    return this.start(id, "undo", undoImport)
  }

  /** Stops every run of this page, as a sign-out does. */
  stopAll(): void {
    for (const e of this.entries.values()) e.controller?.abort()
  }
}
