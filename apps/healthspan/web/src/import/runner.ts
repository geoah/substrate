// The runner (docs/plans/healthspan.md, Import, The runner). Every step is
// idempotent and every change of the import record is a patch held to the
// version last read, so two browsers never both win a transition.
//
// - The lease: `runner` is taken where it is absent or its `until` has
//   passed, for 60 s, and renewed every 20 s with `results`. A write is sent
//   only while 20 s of the lease remain, and given up 5 s before it ends.
//   A renewal refused stops this browser at once.
// - Setup (planned): the lab's source, then `importing`.
// - Writing (importing): each report, then its results, a PUT at
//   `ifVersion: 0` under the ids the plan derives; a 409 is read and
//   compared, the same is landed, anything else stops the import.
// - Undo: the lease and `undoing` in one patch, a wait past the previous
//   lease's end, the plan's records deleted, then whatever still points at
//   the import through `origin`, read again, and `undone`.

import {
  SubstrateError,
  describe,
  isNetworkError,
  type SubstrateRecord,
} from "@substrate/client"
import {
  Import,
  KIND,
  type ImportDraft,
  type ImportStatus,
  type LabImport,
} from "../kinds"
import { labSourceId, reportId, resultId } from "./ids"
import {
  plannedIds,
  reportProps,
  resultProps,
  sameAsPlanned,
  type UnitKey,
  type ImportTarget,
} from "./records"

export const LEASE_MS = 60_000
export const RENEW_MS = 20_000
/** A write is sent only while this much of the lease remains. */
export const MIN_LEFT_MS = 20_000
/** A request still open this close to the lease's end is given up. */
export const GIVE_UP_MS = 5_000
/** How long Undo waits past the previous lease's end for a write still in
 * flight: a heuristic, since the server bounds a request's body and not its
 * commit. */
export const SETTLE_MS = 30_000

/** The records the runner reads and writes: the client's own calls, and
 * the records pointing at an import through `origin`. */
export interface RunnerStore {
  record(
    kind: string,
    id: string,
    signal?: AbortSignal
  ): Promise<SubstrateRecord | null>
  put(
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    ifVersion?: number,
    signal?: AbortSignal
  ): Promise<SubstrateRecord>
  patch(
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    ifVersion?: number,
    signal?: AbortSignal
  ): Promise<SubstrateRecord>
  delete(kind: string, id: string, signal?: AbortSignal): Promise<void>
  /** A unit's identity in the repository's unit graph, for comparing a
   * row found with the row planned; absent, units compare as spelled. */
  unitKey?(signal?: AbortSignal): Promise<UnitKey>
  /** Every live record of `kinds` whose `origin` is `path`. */
  pointingAt(
    kinds: string[],
    path: string,
    signal?: AbortSignal
  ): Promise<{ kind: string; id: string }[]>
}

/** Time, injectable so the lease's tests run at once. */
export interface Clock {
  now(): number
  /** Resolves after `ms`; rejects when `signal` aborts. */
  sleep(ms: number, signal?: AbortSignal): Promise<void>
  /** A signal aborted at the instant `at`, and how to stop it. */
  deadline(at: number): { signal: AbortSignal; clear: () => void }
}

export const realClock: Clock = {
  now: () => Date.now(),
  sleep: (ms, signal) =>
    new Promise<void>((resolve, reject) => {
      if (signal?.aborted) return reject(new Cancelled())
      const t = setTimeout(done, Math.max(0, ms))
      function done() {
        signal?.removeEventListener("abort", stop)
        resolve()
      }
      function stop() {
        clearTimeout(t)
        reject(new Cancelled())
      }
      signal?.addEventListener("abort", stop, { once: true })
    }),
  deadline: (at) => {
    const c = new AbortController()
    const t = setTimeout(() => c.abort(), Math.max(0, at - Date.now()))
    return { signal: c.signal, clear: () => clearTimeout(t) }
  },
}

/** Where a run stands, for the page. */
export type RunProgress =
  | { phase: "taking" }
  /** Another browser holds a live lease: Undo waits it out. */
  | { phase: "waiting"; until: number }
  | { phase: "setup" }
  | { phase: "writing"; done: number; total: number }
  /** Undo waits past the previous lease for a write still in flight. */
  | { phase: "settling"; until: number }
  | { phase: "deleting"; done: number; total: number }

/** How a run ended in this browser. */
export type RunOutcome =
  | { state: "imported"; results: number }
  /** `remaining`: what still pointed at it when it was read last. */
  | { state: "undone"; remaining: number }
  | { state: "stopped"; problem: string }
  /** Resume refused: another browser's lease is live until then. */
  | { state: "held"; until: number }
  /** The lease moved (another browser took over, or Undo), or this browser
   * gave up waiting: it wrote nothing more. */
  | { state: "lost"; why: string }
  | { state: "failed"; words: string }
  /** This tab stopped it. */
  | { state: "cancelled" }
  /** Nothing to do: it is imported or undone. */
  | { state: "nothing"; status: ImportStatus }

export interface RunOptions {
  store: RunnerStore
  /** This browser's id, which the lease names. */
  device: string
  clock?: Clock
  /** Stops the run from this tab. */
  signal?: AbortSignal
  /** The lab's name, where this browser planned the import: the import
   * record holds the source it names, not the name. */
  labName?: string
  onProgress?: (p: RunProgress) => void
}

class Cancelled extends Error {
  constructor() {
    super("Stopped in this tab.")
  }
}
class Lost extends Error {}
class GaveUp extends Error {
  constructor() {
    super("Gave up waiting on the server before the lease ran out.")
  }
}
/** A refusal that stops the import as `stopped`, with the words why. */
class Stop extends Error {}

function transient(e: unknown): boolean {
  return (
    isNetworkError(e) || (e instanceof SubstrateError && e.transient === true)
  )
}

function conflict(e: unknown): boolean {
  return e instanceof SubstrateError && e.conflict
}

function anySignal(signals: AbortSignal[]): AbortSignal {
  const c = new AbortController()
  for (const s of signals) {
    if (s.aborted) {
      c.abort()
      break
    }
    s.addEventListener("abort", () => c.abort(), { once: true })
  }
  return c.signal
}

const iso = (ms: number) => new Date(ms).toISOString()

class Run {
  private readonly clock: Clock
  private readonly stop = new AbortController()
  private version = 0
  private until = 0
  private renewedAt = 0
  private results = 0
  private imp!: LabImport

  constructor(
    private readonly id: string,
    private readonly opts: RunOptions
  ) {
    this.clock = opts.clock ?? realClock
    if (opts.signal) {
      if (opts.signal.aborted) this.stop.abort()
      else
        opts.signal.addEventListener("abort", () => this.stop.abort(), {
          once: true,
        })
    }
  }

  private progress(p: RunProgress) {
    this.opts.onProgress?.(p)
  }

  /** A request given up `GIVE_UP_MS` before `end` (the lease's end by
   * default), or when this tab stops the run. */
  private async request<T>(
    fn: (signal: AbortSignal) => Promise<T>,
    end = this.until
  ): Promise<T> {
    if (this.stop.signal.aborted) throw new Cancelled()
    const d = this.clock.deadline(end - GIVE_UP_MS)
    if (d.signal.aborted) {
      d.clear()
      throw new GaveUp()
    }
    try {
      return await fn(anySignal([this.stop.signal, d.signal]))
    } catch (e) {
      if (this.stop.signal.aborted) throw new Cancelled()
      if (d.signal.aborted) throw new GaveUp()
      throw e
    } finally {
      d.clear()
    }
  }

  /** A patch of the import on the version last read, the lease renewed in
   * it; refused, the lease is lost. */
  private async patch(draft: Partial<ImportDraft>): Promise<void> {
    const until = this.clock.now() + LEASE_MS
    const body = Import.write({
      runner:
        draft.runner === null
          ? null
          : { device: this.opts.device, until: iso(until) },
      results: this.results,
      ...draft,
    })
    for (let attempt = 0; ; attempt++) {
      try {
        const rec = await this.request((s) =>
          this.opts.store.patch(KIND.import, this.id, body, this.version, s)
        )
        this.version = rec.version
        this.imp = Import.read(rec)
        if (draft.runner !== null) {
          this.until = until
          this.renewedAt = this.clock.now()
        }
        return
      } catch (e) {
        if (conflict(e))
          throw new Lost(
            "Another browser took this import over, or it was undone."
          )
        if (transient(e) && attempt < 3) {
          await this.clock.sleep(1000 * 2 ** attempt, this.stop.signal)
          continue
        }
        throw e
      }
    }
  }

  /** Renews the lease where it is due, before a write: a write is sent
   * only while `MIN_LEFT_MS` of it remain. */
  private async guard(): Promise<void> {
    const now = this.clock.now()
    if (now - this.renewedAt >= RENEW_MS || this.until - now < MIN_LEFT_MS)
      await this.patch({})
    if (this.until - this.clock.now() < MIN_LEFT_MS) throw new GaveUp()
  }

  /** Waits `ms`, renewing the lease on the way. */
  private async wait(ms: number): Promise<void> {
    const end = this.clock.now() + ms
    for (;;) {
      const now = this.clock.now()
      if (now >= end) return
      await this.guard()
      const step = Math.min(end - now, RENEW_MS - (now - this.renewedAt))
      await this.clock.sleep(Math.max(1, step), this.stop.signal)
    }
  }

  /** Takes the lease. Resume is refused while another browser holds a live
   * one; Undo waits it out. Moves `stopped` to `importing` for a resume,
   * and anything to `undoing` for an undo, in the same patch. */
  async take(
    mode: "resume" | "undo"
  ): Promise<{ undo: boolean; prevUntil?: number } | RunOutcome> {
    this.progress({ phase: "taking" })
    let conflicts = 0
    for (;;) {
      const now = this.clock.now()
      const rec = await this.request(
        (s) => this.opts.store.record(KIND.import, this.id, s),
        now + 30_000
      )
      if (!rec)
        return {
          state: "failed",
          words: "This import is not in the repository any more.",
        }
      const imp = Import.read(rec)
      const undo = mode === "undo" || imp.status === "undoing"
      if (!undo && (imp.status === "imported" || imp.status === "undone"))
        return { state: "nothing", status: imp.status }
      const prevUntil = imp.runner ? Date.parse(imp.runner.until) : undefined
      const live =
        prevUntil !== undefined &&
        prevUntil > now &&
        imp.runner?.device !== this.opts.device
      if (live) {
        if (mode !== "undo") return { state: "held", until: prevUntil }
        this.progress({ phase: "waiting", until: prevUntil })
        await this.clock.sleep(prevUntil - now + 1000, this.stop.signal)
        continue
      }
      const draft: Partial<ImportDraft> = {
        runner: { device: this.opts.device, until: iso(now + LEASE_MS) },
      }
      if (undo && imp.status !== "undoing") draft.status = "undoing"
      if (!undo && imp.status === "stopped") {
        draft.status = "importing"
        draft.problem = null
      }
      try {
        const next = await this.request(
          (s) =>
            this.opts.store.patch(
              KIND.import,
              this.id,
              Import.write(draft),
              rec.version,
              s
            ),
          now + 30_000
        )
        this.version = next.version
        this.imp = Import.read(next)
        this.until = now + LEASE_MS
        this.renewedAt = now
        return { undo, prevUntil }
      } catch (e) {
        if (conflict(e) && ++conflicts < 10) continue
        throw e
      }
    }
  }

  /** The lab's source: the one the plan names where it is a lab, else one
   * created under the id derived from the lab's name (a PUT at
   * `ifVersion: 0`, a 409 read and taken where it is a lab, else the next
   * id); then `importing`. */
  private async setup(): Promise<void> {
    this.progress({ phase: "setup" })
    const name = this.opts.labName ?? this.imp.lab ?? this.imp.name
    const ids = [
      ...(this.imp.source ? [this.imp.source] : []),
      ...Array.from({ length: 20 }, (_, i) => labSourceId(name, i + 1)),
    ]
    let taken: string | undefined
    // The source the plan named was a live lab when it was planned: read it
    // before any create, so taking it is no refused write.
    if (this.imp.source) {
      await this.guard()
      const named = this.imp.source
      const found = await this.request((s) =>
        this.opts.store.record(KIND.source, named, s)
      )
      if (found?.properties.class === "lab") taken = named
    }
    for (const id of taken ? [] : ids) {
      await this.guard()
      try {
        await this.request((s) =>
          this.opts.store.put(KIND.source, id, { name, class: "lab" }, 0, s)
        )
        taken = id
        break
      } catch (e) {
        if (e instanceof Lost || e instanceof GaveUp || e instanceof Cancelled)
          throw e
        if (conflict(e)) {
          const found = await this.request((s) =>
            this.opts.store.record(KIND.source, id, s)
          )
          if (found?.properties.class === "lab") {
            taken = id
            break
          }
          continue
        }
        // A named id another writer chose and a purge freed is no create of
        // ours: the next id is.
        if (id === this.imp.source && !transient(e)) continue
        throw e
      }
    }
    if (!taken) throw new Stop("No id was free for the lab's source.")
    await this.patch({ status: "importing", source: taken })
  }

  /** One planned record: created, or found and compared. */
  private async write(
    kind: string,
    id: string,
    props: Record<string, unknown>,
    what: string
  ): Promise<void> {
    for (let attempt = 0; ; attempt++) {
      await this.guard()
      try {
        await this.request((s) => this.opts.store.put(kind, id, props, 0, s))
        return
      } catch (e) {
        if (e instanceof Lost || e instanceof GaveUp || e instanceof Cancelled)
          throw e
        if (conflict(e)) {
          const found = await this.request((s) =>
            this.opts.store.record(kind, id, s)
          )
          const unitKey = await this.request(
            (s) => this.opts.store.unitKey?.(s) ?? Promise.resolve(undefined)
          )
          if (found && sameAsPlanned(kind, props, found, unitKey)) return
          throw new Stop(
            found
              ? `${what} is already in the repository with other values.`
              : `${what} was deleted since it was written. Undo this import, then import the file again.`
          )
        }
        if (transient(e) && attempt < 3) {
          await this.clock.sleep(1000 * 2 ** attempt, this.stop.signal)
          continue
        }
        throw new Stop(`${what} was refused: ${describe(e)}`)
      }
    }
  }

  private async writing(): Promise<RunOutcome> {
    const imp = this.imp
    const source = imp.source!
    let lab = this.opts.labName ?? this.imp.lab
    if (!lab) {
      const rec = await this.request((s) =>
        this.opts.store.record(KIND.source, source, s)
      )
      const n = rec?.properties.name
      lab = typeof n === "string" && n ? n : imp.name
    }
    const target: ImportTarget = { id: this.id, source, lab }
    const total = imp.rows.length
    this.results = 0
    this.progress({ phase: "writing", done: 0, total })
    for (const report of imp.reports) {
      await this.write(
        KIND.labreport,
        reportId(this.id, report.key),
        reportProps(target, report),
        `Report ${report.key}`
      )
      for (const row of imp.rows) {
        if (row.report !== report.key) continue
        await this.write(
          KIND.labresult,
          resultId(this.id, row.key),
          resultProps(target, row),
          `Result ${row.key}${row.name ? ` (${row.name})` : ""}`
        )
        this.results++
        this.progress({ phase: "writing", done: this.results, total })
      }
    }
    await this.patch({
      status: "imported",
      importedAt: iso(this.clock.now()),
      runner: null,
      problem: null,
    })
    return { state: "imported", results: this.results }
  }

  private async undoing(prevUntil: number | undefined): Promise<RunOutcome> {
    if (prevUntil !== undefined) {
      const fence = prevUntil + SETTLE_MS
      if (fence > this.clock.now()) {
        this.progress({ phase: "settling", until: fence })
        await this.wait(fence - this.clock.now())
      }
    }
    const planned = plannedIds(this.id, this.imp)
    const origin = `${KIND.import}/${this.id}`
    const kinds = [KIND.labresult, KIND.labreport]
    let done = 0
    const remove = async (r: { kind: string; id: string }, total: number) => {
      await this.guard()
      await this.request((s) => this.opts.store.delete(r.kind, r.id, s))
      this.progress({ phase: "deleting", done: ++done, total })
    }
    for (const r of planned) await remove(r, planned.length)
    const pointing = async () => {
      await this.guard()
      return this.request((s) => this.opts.store.pointingAt(kinds, origin, s))
    }
    // Results before reports: a report's delete cascades to its results.
    const left = (await pointing()).sort(
      (a, b) =>
        Number(a.kind === KIND.labreport) - Number(b.kind === KIND.labreport)
    )
    done = 0
    for (const r of left) await remove(r, left.length)
    const remaining = await pointing()
    this.results = 0
    await this.patch({ status: "undone", runner: null })
    return { state: "undone", remaining: remaining.length }
  }

  async run(mode: "resume" | "undo"): Promise<RunOutcome> {
    try {
      const taken = await this.take(mode)
      if ("state" in taken) return taken
      if (taken.undo) return await this.undoing(taken.prevUntil)
      if (this.imp.status === "planned") await this.setup()
      return await this.writing()
    } catch (e) {
      if (e instanceof Cancelled) return { state: "cancelled" }
      if (e instanceof Lost) return { state: "lost", why: e.message }
      if (e instanceof GaveUp) return { state: "lost", why: e.message }
      if (e instanceof Stop) {
        try {
          await this.patch({
            status: "stopped",
            problem: e.message,
            runner: null,
          })
          return { state: "stopped", problem: e.message }
        } catch (e2) {
          if (e2 instanceof Lost) return { state: "lost", why: e2.message }
          return { state: "failed", words: describe(e2) }
        }
      }
      return { state: "failed", words: describe(e) }
    }
  }
}

/** Runs an import from where it stands: setup from `planned`, the writing
 * from `importing` (from the start: what landed answers 409 and compares
 * equal) and from `stopped` (moved to `importing` with the lease), the
 * undo again from `undoing`. */
export function resumeImport(
  id: string,
  opts: RunOptions
): Promise<RunOutcome> {
  return new Run(id, opts).run("resume")
}

/** Undoes an import from any state, `undone` included (to delete what
 * landed after it). */
export function undoImport(id: string, opts: RunOptions): Promise<RunOutcome> {
  return new Run(id, opts).run("undo")
}

/** Whether an import can be resumed: every state but `imported` and
 * `undone`. */
export function resumable(status: ImportStatus): boolean {
  return status !== "imported" && status !== "undone"
}
