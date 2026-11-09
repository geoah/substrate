// The runs of this page: one at a time per import, each under a Web Lock
// named for the import so two tabs of one browser never contend, kept here
// so a run outlives the screen that started it and every screen shows its
// progress. A new import is staged here and then run; the plans this page
// staged are kept for its runs, so they need not be read back.

import { uuidv7 } from "@substrate/client"
import { KIND } from "../kinds"
import type { StagingKeep } from "./keep"
import type { StoredPlan } from "./planfile"
import { storedPlan } from "./planfile"
import {
  discardImport,
  finishStaging,
  resumeImport,
  stageImport,
  undoImport,
  type Clock,
  type RunOptions,
  type RunOutcome,
  type RunProgress,
  type RunnerStore,
  type StageRequest,
} from "./runner"

export type RunMode = "stage" | "resume" | "undo" | "discard"

export interface RunState {
  running: boolean
  mode?: RunMode
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
  private readonly plans = new Map<string, StoredPlan>()
  private kept = new Set<string>()
  private keptVersion = 0

  constructor(
    private readonly store: RunnerStore,
    private readonly opts: {
      device: string
      clock?: Clock
      lockPrefix: string
      keep?: StagingKeep
    }
  ) {
    void this.refreshKept(true)
  }

  subscribe = (fn: () => void): (() => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }

  state = (id: string): RunState => this.entries.get(id)?.state ?? IDLE

  /** The imports this browser is staging and keeps the files of: a page
   * closed during staging offers to finish them. */
  keptIds = (): ReadonlySet<string> => this.kept

  /** Moves whenever `keptIds` changes. */
  get keptRevision(): number {
    return this.keptVersion
  }

  /** Reads what this browser keeps; on the page's open (`prune`), drops
   * what it keeps for an import whose record is not there (its create never
   * landed, or another device discarded it), which nothing could finish. */
  private async refreshKept(prune = false): Promise<void> {
    try {
      let ids = (await this.opts.keep?.list()) ?? []
      if (prune && ids.length) {
        const gone = new Set<string>()
        for (const id of ids)
          if (!(await this.store.record(KIND.import, id))) gone.add(id)
        for (const id of gone) await this.opts.keep?.drop(id)
        ids = ids.filter((id) => !gone.has(id))
      }
      const next = new Set(ids)
      if (
        next.size !== this.kept.size ||
        [...next].some((id) => !this.kept.has(id))
      ) {
        this.kept = next
        this.keptVersion++
        this.emit()
      }
    } catch {
      // A browser that keeps nothing offers nothing to finish.
    }
  }

  /** The lab's name of an import planned in this page, which its setup
   * names the lab's source with. */
  rememberLab(id: string, lab: string): void {
    this.labs.set(id, lab)
  }

  /** The plan of an import this page staged or read. */
  plan(id: string): StoredPlan | undefined {
    return this.plans.get(id)
  }

  private emit() {
    for (const fn of this.listeners) fn()
  }

  private set(id: string, state: RunState) {
    const e = this.entries.get(id) ?? { state }
    e.state = state
    this.entries.set(id, e)
    this.emit()
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

  private options(
    id: string,
    mode: RunMode,
    controller: AbortController
  ): RunOptions {
    return {
      store: this.store,
      device: this.opts.device,
      clock: this.opts.clock,
      signal: controller.signal,
      labName: this.labs.get(id),
      plan: this.plans.get(id),
      keep: this.opts.keep,
      onProgress: (progress) => this.set(id, { running: true, mode, progress }),
    }
  }

  private begin(
    id: string,
    mode: RunMode,
    run: (opts: RunOptions) => Promise<RunOutcome>
  ): Promise<RunOutcome> {
    const controller = new AbortController()
    const waits = mode === "undo" || mode === "discard"
    this.set(id, { running: true, mode })
    const promise = this.lock(
      id,
      waits,
      () => run(this.options(id, mode, controller)),
      () => ({
        state: "failed" as const,
        words: "Another tab of this browser is running it.",
      })
    ).then(
      (outcome) => {
        this.set(id, { running: false, mode, outcome })
        void this.refreshKept()
        return outcome
      },
      (e: unknown) => {
        const outcome: RunOutcome = { state: "failed", words: String(e) }
        this.set(id, { running: false, mode, outcome })
        void this.refreshKept()
        return outcome
      }
    )
    const e = this.entries.get(id)!
    e.controller = controller
    e.promise = promise
    return promise
  }

  /** Stages each plan as a new import, one after another, and once each
   * is `planned` runs it: answers how each staging ended, while the runs
   * go on. */
  async start(requests: readonly StageRequest[]): Promise<RunOutcome[]> {
    const outcomes: RunOutcome[] = []
    const staged: string[] = []
    for (const req of requests) {
      if (req.lab) this.rememberLab(req.id, req.lab)
      this.plans.set(req.id, storedPlan(req.plan))
      const out = await this.begin(req.id, "stage", (opts) =>
        stageImport(req, opts)
      )
      outcomes.push(out)
      if (out.state === "planned") staged.push(req.id)
    }
    void (async () => {
      for (const id of staged) await this.resume(id)
    })()
    return outcomes
  }

  /** Finishes staging an import this browser began, then runs it. */
  async finish(id: string): Promise<RunOutcome> {
    const e = this.entries.get(id)
    if (e?.state.running && e.promise) return e.promise
    const out = await this.begin(id, "stage", (opts) => finishStaging(id, opts))
    if (out.state === "planned") return this.resume(id)
    return out
  }

  /** Runs an import from where it stands; one already running here is
   * joined. */
  resume(id: string): Promise<RunOutcome> {
    const e = this.entries.get(id)
    if (e?.state.running && e.promise) return e.promise
    return this.begin(id, "resume", (opts) => resumeImport(id, opts))
  }

  /** Stops a run of this import in this page, and waits for it. */
  private async halt(id: string, unless: RunMode): Promise<RunOutcome | void> {
    const e = this.entries.get(id)
    if (e?.state.running && e.promise) {
      if (e.state.mode === unless) return e.promise
      e.controller?.abort()
      await e.promise
    }
  }

  /** Undoes an import: a run of it in this page is stopped first, and the
   * undo then waits for the lock another tab holds. */
  async undo(id: string): Promise<RunOutcome> {
    const joined = await this.halt(id, "undo")
    if (joined) return joined
    return this.begin(id, "undo", (opts) => undoImport(id, opts))
  }

  /** Discards a staging import: its staging in this page is stopped first;
   * the discard then waits for the lock another tab holds. */
  async discard(id: string): Promise<RunOutcome> {
    const joined = await this.halt(id, "discard")
    if (joined) return joined
    return this.begin(id, "discard", (opts) => discardImport(id, opts))
  }

  /** Stops every run of this page, as a sign-out does. */
  stopAll(): void {
    for (const e of this.entries.values()) e.controller?.abort()
  }
}
