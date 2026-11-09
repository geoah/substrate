// The runner (docs/plans/healthspan.md, Import, The runner; Importers):
// every importer's plan is staged and written by this one runner. Every
// step is idempotent and every change of the import record is a patch held
// to the version last read, so two browsers never both win a transition.
//
// - Staging (the browser that read the files): the import record first, in
//   `staging`, under an id minted here; the documents a few at a time, each
//   batch's blobs up and then one guarded patch naming them in `documents`
//   (a blob the collector took before its patch is uploaded again); the
//   plan last, named in the patch that moves it to `planned`. The bytes stay
//   in this browser until the patch naming them is confirmed. Discarding,
//   from any device, takes the lease and deletes the staging record
//   outright; a stager that finds its record gone stops.
// - The lease: `runner` is taken where it is absent or its `until` has
//   passed, for 60 s, and renewed every 20 s with `counts`. A write is sent
//   only while 20 s of the lease remain, and given up 5 s before it ends.
//   A renewal refused stops this browser at once.
// - Setup (planned): the import's source, then `importing`.
// - Writing (importing): the plan's records in its order, each under its
//   derived id. An import's own records (a CSV's, a hand entry's) are
//   compared and stop the import where they differ; a provider's are
//   decided by revision; a source is written where absent; an organization
//   is merged.
// - Undo: the lease and `undoing` in one patch, a wait past the previous
//   lease's end, what the import created deleted, then whatever still points
//   at it through `origin`, read again, and `undone`.

import {
  SubstrateError,
  describe,
  isNetworkError,
  type SubstrateRecord,
} from "@substrate/client"
import {
  Import,
  KIND,
  NO_COUNTS,
  type ImportCounts,
  type ImportDraft,
  type ImportRecord,
  type ImportStatus,
} from "../kinds"
import {
  decideMaintained,
  maintainedByKind,
  mergeInto,
  originIs,
  type Resolved,
} from "./decide"
import { OWN, kindName, labSourceId, recordId, recordPath } from "./ids"
import type { Plan, PlannedRecord, RecordKey } from "./importers/types"
import type { KeptDocument, KeptStaging, StagingKeep } from "./keep"
import {
  blobDigestOf,
  keyOf,
  leftMarkdown,
  parsePlan,
  planBytes,
  storedPlan,
  type StoredPlan,
} from "./planfile"
import { sameAsPlanned, type UnitKey } from "./records"

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
/** The most a record write may be: the import record holds every
 * document's digest, so the plan's documents are held to it before
 * staging. */
export const MAX_RECORD_BYTES = 1 << 20
/** How many documents, and how many bytes of them, go up before the patch
 * naming them: well within the collector's minute. */
export const BATCH_DOCUMENTS = 8
export const BATCH_BYTES = 8 << 20
/** How many times a record whose version moved under a write is decided
 * again before the import stops. */
export const DECISIONS = 3
/** How many times a batch whose blob the collector took is uploaded
 * again. */
export const REUPLOADS = 3
/** The most of `skipped` an import record holds. */
const MAX_SKIPPED = 128 << 10

/** The records the runner reads and writes: the client's own calls, the
 * blob store, and the records pointing at an import through `origin`. */
export interface RunnerStore {
  /** A live record, or null. */
  record(
    kind: string,
    id: string,
    signal?: AbortSignal
  ): Promise<SubstrateRecord | null>
  /** The tombstone at an id (deleted, not yet collected), or null. */
  tombstone(
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
  /** Deletes a record, held to `ifVersion` where given; one already gone
   * is done. */
  delete(
    kind: string,
    id: string,
    ifVersion?: number,
    signal?: AbortSignal
  ): Promise<void>
  /** Stores bytes under their digest; a blob already stored is the same
   * blob. */
  putBlob(
    digest: string,
    bytes: Uint8Array,
    mediaType: string,
    name: string,
    signal?: AbortSignal
  ): Promise<void>
  /** A blob's bytes, which a live record names. */
  blob(digest: string, signal?: AbortSignal): Promise<Uint8Array>
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
  /** Another browser holds a live lease: Undo and Discard wait it out. */
  | { phase: "waiting"; until: number }
  /** Staging: how many of the plan's documents the import names. */
  | { phase: "staging"; done: number; total: number }
  | { phase: "setup" }
  | { phase: "writing"; done: number; total: number }
  /** Undo waits past the previous lease for a write still in flight. */
  | { phase: "settling"; until: number }
  | { phase: "deleting"; done: number; total: number }
  | { phase: "discarding" }

/** How a run ended in this browser. */
export type RunOutcome =
  /** Staged: every document and the plan named, and `planned`. */
  | { state: "planned" }
  | { state: "imported"; counts: ImportCounts }
  /** `remaining`: what still pointed at it when it was read last. */
  | { state: "undone"; remaining: number }
  /** A staging import deleted, by this run or before it. */
  | { state: "discarded" }
  | { state: "stopped"; problem: string }
  /** Resume refused: another browser's lease is live until then. */
  | { state: "held"; until: number }
  /** The lease moved (another browser took over, Undo or Discard), or this
   * browser gave up waiting: it wrote nothing more. */
  | { state: "lost"; why: string }
  | { state: "failed"; words: string }
  /** This tab stopped it. */
  | { state: "cancelled" }
  /** Nothing to do: it is imported or undone, or (for Resume) still
   * staging in the browser that read its files. */
  | { state: "nothing"; status: ImportStatus }

export interface RunOptions {
  store: RunnerStore
  /** This browser's id, which the lease names. */
  device: string
  clock?: Clock
  /** Stops the run from this tab. */
  signal?: AbortSignal
  /** The lab's name, where this browser planned a CSV or a hand entry:
   * the import record holds the source it names, not the name. */
  labName?: string
  /** The plan, where this page holds it (it staged it); else it is read
   * from the import's blob. */
  plan?: StoredPlan
  /** What this browser keeps of the imports it stages. */
  keep?: StagingKeep
  onProgress?: (p: RunProgress) => void
}

/** A plan to stage as a new import. */
export interface StageRequest {
  /** The import's id, minted by the page (a UUIDv7). */
  id: string
  plan: Plan
  /** For a CSV or a hand entry, the lab's name. */
  lab?: string
  /** For a CSV or a hand entry, a lab source already there of that name. */
  source?: string
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
/** The import record is gone: discarded, or deleted. */
class Gone extends Error {}
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

function notFound(e: unknown): boolean {
  return e instanceof SubstrateError && e.status === 404
}

/** A write refused because a blob it names is no longer stored: the
 * collector took it before the write named it. */
export function blobCollected(e: unknown): boolean {
  return (
    e instanceof SubstrateError &&
    e.status === 422 &&
    /blob \S+ is unknown/.test(e.words)
  )
}

function control(e: unknown): boolean {
  return (
    e instanceof Lost ||
    e instanceof GaveUp ||
    e instanceof Cancelled ||
    e instanceof Gone ||
    e instanceof Stop
  )
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

/** The importers whose records are the import's own (a file of lab
 * results, a hand entry): their source is the lab's, decided by name. */
export function ownImporter(importer: string | undefined): boolean {
  return importer === "csv" || importer === "manual"
}

/** Whether an import can be resumed: every state but `staging`,
 * `imported` and `undone`. */
export function resumable(status: ImportStatus): boolean {
  return status !== "staging" && status !== "imported" && status !== "undone"
}

const LABELS: Record<string, string> = {
  labreport: "Report",
  labresult: "Result",
}

/** A planned record in words, for a problem or a note: `Result 3 (LDL)`,
 * or within a sentence `result 3`. */
export function recordWords(
  r: PlannedRecord | RecordKey,
  inSentence = false
): string {
  const name = kindName(r.kind)
  const word = LABELS[name] ?? name.charAt(0).toUpperCase() + name.slice(1)
  const label = inSentence ? word.toLowerCase() : word
  const props = "properties" in r ? r.properties : {}
  const coding = props.coding as { display?: unknown } | undefined
  const display =
    name === "labresult" && typeof coding?.display === "string"
      ? ` (${coding.display})`
      : ""
  return `${label} ${r.key}${display}`
}

function dropNulls(props: Resolved): Resolved {
  const out: Resolved = {}
  for (const [k, v] of Object.entries(props)) if (v !== null) out[k] = v
  return out
}

/** `skipped` held to what an import record may carry. */
function capSkipped(text: string | undefined): string | undefined {
  if (!text || text.length <= MAX_SKIPPED) return text
  const lines = text.split("\n")
  const out: string[] = []
  let size = 0
  for (const line of lines) {
    if (size + line.length + 1 > MAX_SKIPPED - 200) break
    out.push(line)
    size += line.length + 1
  }
  out.push(`- And ${(lines.length - out.length).toLocaleString("en-GB")} more.`)
  return out.join("\n")
}

/** Why a plan cannot be staged, or undefined where it can: the import
 * record names every document's digest, and a record write is held to
 * 1 MiB. */
export function planRefusal(plan: Plan): string | undefined {
  const stored = storedPlan(plan)
  const body = JSON.stringify({
    properties: Import.write({
      name: plan.name,
      importer: plan.importer,
      status: "importing",
      runner: { device: "x".repeat(64), until: iso(0) },
      lab: "x".repeat(256),
      source: "x".repeat(128),
      plan: blobDigestOf(new Uint8Array()),
      documents: stored.documents.map((d) => d.digest),
      counts: { ...NO_COUNTS },
      problem: "x".repeat(2048),
      importedAt: iso(0),
      skipped: capSkipped(leftMarkdown(plan.left)) ?? null,
    }),
    ifVersion: 999_999,
  })
  if (new TextEncoder().encode(body).length <= MAX_RECORD_BYTES) return
  return `It holds ${stored.documents.length.toLocaleString("en-GB")} documents, more than one import can name. Import it in parts.`
}

type Mode = "resume" | "undo" | "discard"
type Tally = keyof Omit<ImportCounts, "left">
/** What a write decides on a record already there. */
type Verdict =
  { kind: "unchanged" | "stale" } | { kind: "write"; props: Resolved }

/** A reference the run left out. */
const LEFT = Symbol("left")

class Run {
  private readonly clock: Clock
  private readonly stop = new AbortController()
  private version = 0
  private until = 0
  private renewedAt = 0
  /** This run's counts, from the start of its writing. */
  private counts?: ImportCounts
  private imp?: ImportRecord
  private plan?: StoredPlan
  /** Setup created the provider's source in this run. */
  private sourceCreated = false
  /** Whether a record outside the plan is there, by path. */
  private readonly exists = new Map<string, boolean>()
  private unitKey?: UnitKey

  constructor(
    private readonly id: string,
    private readonly opts: RunOptions
  ) {
    this.clock = opts.clock ?? realClock
    this.plan = opts.plan
    if (opts.signal) {
      if (opts.signal.aborted) this.stop.abort()
      else
        opts.signal.addEventListener("abort", () => this.stop.abort(), {
          once: true,
        })
    }
  }

  private get store() {
    return this.opts.store
  }

  private get path() {
    return `${KIND.import}/${this.id}`
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

  /** The import record as a patch or a take answered it. */
  private adopt(rec: SubstrateRecord, leaseFrom?: number) {
    this.version = rec.version
    this.imp = Import.read(rec)
    if (leaseFrom !== undefined) {
      this.until = leaseFrom + LEASE_MS
      this.renewedAt = leaseFrom
    }
  }

  /** A patch of the import on the version last read, the lease renewed in
   * it (and the counts so far, while it writes); refused, the lease is
   * lost. */
  private async patch(draft: Partial<ImportDraft>): Promise<void> {
    const now = this.clock.now()
    const body = Import.write({
      runner:
        draft.runner === null
          ? null
          : { device: this.opts.device, until: iso(now + LEASE_MS) },
      ...(this.counts ? { counts: this.counts } : {}),
      ...draft,
    })
    for (let attempt = 0; ; attempt++) {
      try {
        const rec = await this.request((s) =>
          this.store.patch(KIND.import, this.id, body, this.version, s)
        )
        this.adopt(rec, draft.runner === null ? undefined : now)
        return
      } catch (e) {
        if (control(e)) throw e
        if (conflict(e))
          throw new Lost(
            "Another browser took this import over, or it was undone or discarded."
          )
        if (notFound(e)) throw new Gone()
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
   * and anything to `undoing` for an undo, in the same patch. A staging
   * import is the discard's, which takes its own lease. */
  async take(
    mode: Mode
  ): Promise<
    { undo: boolean; discard: boolean; prevUntil?: number } | RunOutcome
  > {
    this.progress({ phase: "taking" })
    let conflicts = 0
    for (;;) {
      const now = this.clock.now()
      const rec = await this.request(
        (s) => this.store.record(KIND.import, this.id, s),
        now + 30_000
      )
      if (!rec)
        return mode === "discard"
          ? { state: "discarded" }
          : {
              state: "failed",
              words: "This import is not in the repository any more.",
            }
      const imp = Import.read(rec)
      if (imp.status === "staging") {
        if (mode === "resume") return { state: "nothing", status: "staging" }
        return { undo: false, discard: true }
      }
      if (mode === "discard") return { state: "nothing", status: imp.status }
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
            this.store.patch(
              KIND.import,
              this.id,
              Import.write(draft),
              rec.version,
              s
            ),
          now + 30_000
        )
        this.adopt(next, now)
        return { undo, discard: false, prevUntil }
      } catch (e) {
        if (conflict(e) && ++conflicts < 10) continue
        throw e
      }
    }
  }

  // Staging.

  /** Takes the lease of a staging import this browser stages, whose record
   * a create met: answers an outcome where it is not this browser's to
   * continue. */
  private async takeStaging(): Promise<RunOutcome | undefined> {
    for (let conflicts = 0; ;) {
      const now = this.clock.now()
      const rec = await this.request(
        (s) => this.store.record(KIND.import, this.id, s),
        now + 30_000
      )
      if (!rec) throw new Gone()
      const imp = Import.read(rec)
      if (imp.status !== "staging") {
        await this.opts.keep?.drop(this.id).catch(() => {})
        return { state: "nothing", status: imp.status }
      }
      const prevUntil = imp.runner ? Date.parse(imp.runner.until) : undefined
      if (
        prevUntil !== undefined &&
        prevUntil > now &&
        imp.runner?.device !== this.opts.device
      )
        return { state: "held", until: prevUntil }
      try {
        const next = await this.request(
          (s) =>
            this.store.patch(
              KIND.import,
              this.id,
              Import.write({
                runner: {
                  device: this.opts.device,
                  until: iso(now + LEASE_MS),
                },
              }),
              rec.version,
              s
            ),
          now + 30_000
        )
        this.adopt(next, now)
        return undefined
      } catch (e) {
        if (notFound(e)) throw new Gone()
        if (conflict(e) && ++conflicts < 10) continue
        throw e
      }
    }
  }

  /** Uploads blobs, then the patch naming them; where the collector took
   * one before the patch named it, uploads them again and patches again. */
  private async nameBlobs(
    blobs: readonly KeptDocument[],
    draft: () => Partial<ImportDraft>
  ): Promise<void> {
    for (let attempt = 0; ; attempt++) {
      for (const b of blobs) {
        await this.guard()
        await this.request((s) =>
          this.store.putBlob(b.digest, b.bytes, b.mediaType, b.name, s)
        )
      }
      try {
        await this.patch(draft())
        return
      } catch (e) {
        if (blobCollected(e) && attempt < REUPLOADS) continue
        throw e
      }
    }
  }

  async staging(entry: KeptStaging): Promise<RunOutcome> {
    this.progress({ phase: "taking" })
    const now = this.clock.now()
    const create: Partial<ImportDraft> = {
      name: entry.name,
      importer: entry.importer,
      status: "staging",
      runner: { device: this.opts.device, until: iso(now + LEASE_MS) },
      documents: [],
    }
    if (entry.lab) create.lab = entry.lab
    if (entry.source) create.source = entry.source
    if (entry.skipped) create.skipped = entry.skipped
    for (let attempt = 0; ; attempt++) {
      try {
        const rec = await this.request(
          (s) =>
            this.store.put(KIND.import, this.id, Import.write(create), 0, s),
          now + 30_000
        )
        this.adopt(rec, now)
        break
      } catch (e) {
        if (transient(e) && attempt < 3) {
          await this.clock.sleep(1000 * 2 ** attempt, this.stop.signal)
          continue
        }
        if (!conflict(e)) throw e
        // Its record is there: this browser's own staging continues.
        const out = await this.takeStaging()
        if (out) return out
        break
      }
    }
    const bytes = new Map(entry.documents.map((d) => [d.digest, d]))
    const total = entry.plan.documents.length
    const named = () => new Set(this.imp?.documents ?? [])
    const todo = entry.plan.documents.filter((d) => !named().has(d.digest))
    this.progress({ phase: "staging", done: total - todo.length, total })
    const batches: KeptDocument[][] = []
    let batch: KeptDocument[] = []
    let size = 0
    for (const d of todo) {
      const kept = bytes.get(d.digest)
      if (!kept)
        throw new Stop(
          `This browser no longer holds ${d.name}. Discard this import, then import the files again.`
        )
      if (
        batch.length &&
        (batch.length >= BATCH_DOCUMENTS ||
          size + kept.bytes.length > BATCH_BYTES)
      ) {
        batches.push(batch)
        batch = []
        size = 0
      }
      batch.push(kept)
      size += kept.bytes.length
    }
    if (batch.length) batches.push(batch)
    let done = total - todo.length
    for (const b of batches) {
      const digests = b.map((d) => d.digest)
      await this.nameBlobs(b, () => {
        const have = named()
        return {
          documents: [
            ...(this.imp?.documents ?? []),
            ...digests.filter((d) => !have.has(d)),
          ],
        }
      })
      await this.opts.keep?.dropDocuments(this.id, digests).catch(() => {})
      done += b.length
      this.progress({ phase: "staging", done, total })
    }
    const planned = planBytes(entry.plan)
    const digest = blobDigestOf(planned)
    await this.nameBlobs(
      [
        {
          digest,
          bytes: planned,
          mediaType: "application/json",
          name: "plan.json",
        },
      ],
      () => ({ plan: digest, status: "planned", runner: null })
    )
    this.plan = entry.plan
    await this.opts.keep?.drop(this.id).catch(() => {})
    return { state: "planned" }
  }

  /** Deletes a staging import outright, from any device: its lease taken
   * (a live one of another browser waited out), held to the version read.
   * It has no plan to undo, and the server then collects its blobs. */
  private async discarding(): Promise<RunOutcome> {
    for (let conflicts = 0; ;) {
      const now = this.clock.now()
      const rec = await this.request(
        (s) => this.store.record(KIND.import, this.id, s),
        now + 30_000
      )
      if (!rec) {
        await this.opts.keep?.drop(this.id).catch(() => {})
        return { state: "discarded" }
      }
      const imp = Import.read(rec)
      if (imp.status !== "staging")
        return { state: "nothing", status: imp.status }
      const prevUntil = imp.runner ? Date.parse(imp.runner.until) : undefined
      if (
        prevUntil !== undefined &&
        prevUntil > now &&
        imp.runner?.device !== this.opts.device
      ) {
        this.progress({ phase: "waiting", until: prevUntil })
        await this.clock.sleep(prevUntil - now + 1000, this.stop.signal)
        continue
      }
      try {
        const next = await this.request(
          (s) =>
            this.store.patch(
              KIND.import,
              this.id,
              Import.write({
                runner: {
                  device: this.opts.device,
                  until: iso(now + LEASE_MS),
                },
              }),
              rec.version,
              s
            ),
          now + 30_000
        )
        this.progress({ phase: "discarding" })
        await this.request(
          (s) => this.store.delete(KIND.import, this.id, next.version, s),
          now + 30_000
        )
        await this.opts.keep?.drop(this.id).catch(() => {})
        return { state: "discarded" }
      } catch (e) {
        if ((conflict(e) || notFound(e)) && ++conflicts < 10) continue
        throw e
      }
    }
  }

  // Writing.

  private async loadPlan(): Promise<StoredPlan> {
    if (this.plan) return this.plan
    const digest = this.imp?.plan
    if (!digest) throw new Stop("This import has no plan.")
    const bytes = await this.request((s) => this.store.blob(digest, s))
    try {
      this.plan = parsePlan(bytes)
    } catch (e) {
      throw new Stop(
        `Its plan could not be read: ${e instanceof Error ? e.message : String(e)}`
      )
    }
    return this.plan
  }

  private async units(): Promise<UnitKey | undefined> {
    if (!this.unitKey && this.store.unitKey)
      this.unitKey = await this.request(
        (s) => this.store.unitKey?.(s) ?? Promise.resolve(undefined)
      )
    return this.unitKey
  }

  /** A CSV's or a hand entry's lab source: the one the import names where
   * it is a lab, else one created under the id derived from the lab's name
   * (a PUT at `ifVersion: 0`, a 409 read and taken where it is a lab, else
   * the next id). */
  private async labSource(): Promise<string> {
    const imp = this.imp!
    const name = this.opts.labName ?? imp.lab ?? imp.name
    const ids = [
      ...(imp.source ? [imp.source] : []),
      ...Array.from({ length: 20 }, (_, i) => labSourceId(name, i + 1)),
    ]
    // The source the plan named was a live lab when it was planned: read it
    // before any create, so taking it is no refused write.
    if (imp.source) {
      await this.guard()
      const named = imp.source
      const found = await this.request((s) =>
        this.store.record(KIND.source, named, s)
      )
      if (found?.properties.class === "lab") return named
    }
    for (const id of ids) {
      await this.guard()
      try {
        await this.request((s) =>
          this.store.put(KIND.source, id, { name, class: "lab" }, 0, s)
        )
        return id
      } catch (e) {
        if (control(e)) throw e
        if (conflict(e)) {
          const found = await this.request((s) =>
            this.store.record(KIND.source, id, s)
          )
          if (found?.properties.class === "lab") return id
          continue
        }
        // A named id another writer chose and a purge freed is no create of
        // ours: the next id is.
        if (id === imp.source && !transient(e)) continue
        throw e
      }
    }
    throw new Stop("No id was free for the lab's source.")
  }

  /** The import's source, then `importing`: for a CSV or a hand entry the
   * lab's, by its name; for a provider its own, `healthspan/source/…` by
   * its derived id, created where absent with the plan's name and class
   * and taken as it is where present, whatever the owner renamed it to. */
  private async setup(plan: StoredPlan): Promise<void> {
    this.progress({ phase: "setup" })
    if (ownImporter(plan.importer)) {
      const source = await this.labSource()
      await this.patch({ status: "importing", source })
      return
    }
    const key = plan.source
    if (!key) {
      await this.patch({ status: "importing" })
      return
    }
    const rec = plan.records.find((r) => keyOf(r) === keyOf(key))
    const id = recordId(key, this.id)
    const props = rec ? dropNulls(rec.properties) : { name: plan.name }
    const tally = await this.writeRecord(
      key.kind,
      id,
      props,
      () => ({ kind: "unchanged" }),
      "The provider's source"
    )
    this.sourceCreated = tally === "created"
    await this.patch({ status: "importing", source: id })
  }

  /** A planned value with every reference resolved to its record's path
   * and every document to its digest; a reference to a record neither in
   * the plan nor already written comes back `LEFT`. */
  private async resolve(
    v: unknown,
    ctx: { plan: StoredPlan; keys: Set<string>; what: string; notes: string[] }
  ): Promise<unknown> {
    if (Array.isArray(v)) {
      const out: unknown[] = []
      for (const x of v) {
        const r = await this.resolve(x, ctx)
        if (r !== LEFT) out.push(r)
      }
      return out
    }
    if (v && typeof v === "object") {
      const o = v as Record<string, unknown>
      if (o.$ref && typeof o.$ref === "object")
        return this.resolveRef(o.$ref as RecordKey, ctx)
      if (typeof o.$doc === "string") return o.$doc
      const out: Record<string, unknown> = {}
      for (const [k, x] of Object.entries(o)) {
        const r = await this.resolve(x, ctx)
        if (r !== LEFT) out[k] = r
      }
      return out
    }
    return v
  }

  private async resolveRef(
    target: RecordKey,
    ctx: { plan: StoredPlan; keys: Set<string>; what: string; notes: string[] }
  ): Promise<string | typeof LEFT> {
    const source = ctx.plan.source
    if (source && keyOf(target) === keyOf(source) && this.imp?.source)
      return `${KIND.source}/${this.imp.source}`
    const path = recordPath(target, this.id)
    if (ctx.keys.has(keyOf(target))) return path
    let there = this.exists.get(path)
    if (there === undefined) {
      const id = recordId(target, this.id)
      there = !!(await this.request((s) =>
        this.store.record(target.kind, id, s)
      ))
      this.exists.set(path, there)
    }
    if (there) return path
    ctx.notes.push(
      `${ctx.what}: its reference to ${recordWords(target, true)} was left out, since no import has written it.`
    )
    if (this.counts) this.counts.left++
    return LEFT
  }

  /** One write held to `ifVersion`: done, or the version moved. Anything
   * else the server refuses stops the import. */
  private async put(
    kind: string,
    id: string,
    props: Resolved,
    ifVersion: number,
    what: string
  ): Promise<"done" | "moved"> {
    for (let attempt = 0; ; attempt++) {
      await this.guard()
      try {
        await this.request((s) => this.store.put(kind, id, props, ifVersion, s))
        return "done"
      } catch (e) {
        if (control(e)) throw e
        if (conflict(e)) return "moved"
        if (transient(e) && attempt < 3) {
          await this.clock.sleep(1000 * 2 ** attempt, this.stop.signal)
          continue
        }
        throw new Stop(`${what} was refused: ${describe(e)}`)
      }
    }
  }

  private read(kind: string, id: string): Promise<SubstrateRecord | null> {
    return this.request((s) => this.store.record(kind, id, s))
  }

  /** A record created where absent (a PUT at `ifVersion: 0`), restored
   * where a tombstone holds its id (a PUT held to the tombstone's version),
   * and otherwise decided on what is there, every write held to the
   * version read and decided again where it moved, `DECISIONS` times at
   * most. */
  private async writeRecord(
    kind: string,
    id: string,
    create: Resolved,
    decide: (found: SubstrateRecord) => Verdict,
    what: string
  ): Promise<Tally> {
    let found: SubstrateRecord | null | undefined
    for (let n = 0; n < DECISIONS; n++) {
      if (found === undefined) {
        if ((await this.put(kind, id, create, 0, what)) === "done")
          return "created"
        found = await this.read(kind, id)
      }
      if (found === null) {
        const tomb = await this.request((s) =>
          this.store.tombstone(kind, id, s)
        )
        if (!tomb) {
          // Purged since: the next round creates it.
          found = undefined
          continue
        }
        if ((await this.put(kind, id, create, tomb.version, what)) === "done")
          return "created"
        found = await this.read(kind, id)
        continue
      }
      const verdict = decide(found)
      if (verdict.kind !== "write") return verdict.kind
      if (
        (await this.put(kind, id, verdict.props, found.version, what)) ===
        "done"
      )
        return "updated"
      found = await this.read(kind, id)
    }
    throw new Stop(
      `${what} changed under every write, ${DECISIONS} times in a row. Resume to try again.`
    )
  }

  /** A CSV's or a hand entry's record, under an id of the import's own:
   * created, or found and compared. The same is a write that landed;
   * anything else stops the import. */
  private async writeOwn(
    kind: string,
    id: string,
    props: Resolved,
    what: string
  ): Promise<Tally> {
    if ((await this.put(kind, id, props, 0, what)) === "done") return "created"
    const found = await this.read(kind, id)
    const unitKey = await this.units()
    if (found && sameAsPlanned(kind, props, found, unitKey)) return "unchanged"
    throw new Stop(
      found
        ? `${what} is already in the repository with other values.`
        : `${what} was deleted since it was written. Undo this import, then import the file again.`
    )
  }

  private async writing(): Promise<RunOutcome> {
    const plan = await this.loadPlan()
    this.counts = { ...NO_COUNTS }
    const keys = new Set(plan.records.map(keyOf))
    const maintained = maintainedByKind(plan.records)
    const unitKey = await this.units()
    const notes: string[] = []
    const origin = this.path
    const total = plan.records.length
    this.progress({ phase: "writing", done: 0, total })
    for (const [i, rec] of plan.records.entries()) {
      const id = recordId(rec, this.id)
      const what = recordWords(rec)
      const props = (await this.resolve(rec.properties, {
        plan,
        keys,
        what,
        notes,
      })) as Resolved
      const revision =
        rec.revision === undefined ? {} : { revision: rec.revision }
      let tally: Tally
      if (rec.namespace === OWN)
        tally = await this.writeOwn(rec.kind, id, { ...props, origin }, what)
      else if (rec.mode === "create") {
        tally = await this.writeRecord(
          rec.kind,
          id,
          dropNulls(props),
          () => ({ kind: "unchanged" }),
          what
        )
        if (
          tally === "unchanged" &&
          this.sourceCreated &&
          plan.source &&
          keyOf(rec) === keyOf(plan.source)
        )
          tally = "created"
      } else if (rec.mode === "merge") {
        tally = await this.writeRecord(
          rec.kind,
          id,
          { ...dropNulls(props), ...revision },
          (found) => {
            const m = mergeInto({ props, revision: rec.revision }, found)
            return m.kind === "update"
              ? { kind: "write", props: m.props }
              : { kind: "unchanged" }
          },
          what
        )
      } else {
        const mine = maintained.get(rec.kind) ?? new Set<string>()
        tally = await this.writeRecord(
          rec.kind,
          id,
          { ...dropNulls(props), ...revision, origin },
          (found) => {
            const d = decideMaintained(
              { props, revision: rec.revision, createOnly: rec.createOnly },
              mine,
              found,
              unitKey
            )
            return d.kind === "raise" || d.kind === "update"
              ? { kind: "write", props: d.props }
              : { kind: d.kind }
          },
          what
        )
      }
      this.counts[tally]++
      this.progress({ phase: "writing", done: i + 1, total })
    }
    const skipped = capSkipped(
      [leftMarkdown(plan.left), ...notes.map((n) => `- ${n}`)]
        .filter(Boolean)
        .join("\n")
    )
    await this.patch({
      status: "imported",
      importedAt: iso(this.clock.now()),
      runner: null,
      problem: null,
      skipped: skipped || null,
    })
    return { state: "imported", counts: { ...this.counts } }
  }

  // Undo.

  /** A provider's record deleted where this import created it: read, and
   * deleted held to the version read only where its `origin` is this
   * import, so a record another import created and this one only updated
   * is never deleted. */
  private async deleteIfCreated(kind: string, id: string): Promise<void> {
    for (let n = 0; n < DECISIONS; n++) {
      await this.guard()
      const found = await this.read(kind, id)
      if (!found || !originIs(found, this.path)) return
      try {
        await this.request((s) => this.store.delete(kind, id, found.version, s))
        return
      } catch (e) {
        if (control(e)) throw e
        if (conflict(e)) continue
        throw e
      }
    }
    throw new Error(
      `${kindName(kind)} ${id} changed under every delete. Undo again to try again.`
    )
  }

  private async undoing(prevUntil: number | undefined): Promise<RunOutcome> {
    if (prevUntil !== undefined) {
      const fence = prevUntil + SETTLE_MS
      if (fence > this.clock.now()) {
        this.progress({ phase: "settling", until: fence })
        await this.wait(fence - this.clock.now())
      }
    }
    let plan: StoredPlan | undefined
    try {
      plan = this.imp?.plan || this.plan ? await this.loadPlan() : undefined
    } catch (e) {
      if (control(e) && !(e instanceof Stop)) throw e
      // What points at it through `origin` is still found below.
      plan = undefined
    }
    // Later records first: a record after every record it names, so a
    // result goes before its report, whose delete cascades to it.
    const planned = (plan?.records ?? [])
      .filter((r) => r.namespace === OWN || r.mode === "maintain")
      .reverse()
    let done = 0
    for (const r of planned) {
      const id = recordId(r, this.id)
      if (r.namespace === OWN) {
        // Its id is the import's own: a 404 is already gone.
        await this.guard()
        await this.request((s) => this.store.delete(r.kind, id, undefined, s))
      } else await this.deleteIfCreated(r.kind, id)
      this.progress({ phase: "deleting", done: ++done, total: planned.length })
    }
    const order = [
      ...new Set([
        ...planned.map((r) => r.kind),
        KIND.labresult,
        KIND.labreport,
      ]),
    ]
    const pointing = async () => {
      await this.guard()
      return this.request((s) => this.store.pointingAt(order, this.path, s))
    }
    const left = (await pointing()).sort(
      (a, b) => order.indexOf(a.kind) - order.indexOf(b.kind)
    )
    done = 0
    for (const r of left) {
      await this.guard()
      await this.request((s) => this.store.delete(r.kind, r.id, undefined, s))
      this.progress({ phase: "deleting", done: ++done, total: left.length })
    }
    const remaining = await pointing()
    await this.patch({ status: "undone", runner: null })
    return { state: "undone", remaining: remaining.length }
  }

  async run(mode: Mode | "stage", entry?: KeptStaging): Promise<RunOutcome> {
    try {
      if (mode === "stage") return await this.staging(entry!)
      const taken = await this.take(mode)
      if ("state" in taken) return taken
      if (taken.discard) return await this.discarding()
      if (taken.undo) return await this.undoing(taken.prevUntil)
      if (this.imp?.status === "planned")
        await this.setup(await this.loadPlan())
      return await this.writing()
    } catch (e) {
      if (e instanceof Cancelled) return { state: "cancelled" }
      if (e instanceof Gone) {
        await this.opts.keep?.drop(this.id).catch(() => {})
        return mode === "stage" || mode === "discard"
          ? { state: "discarded" }
          : {
              state: "failed",
              words: "This import is not in the repository any more.",
            }
      }
      if (e instanceof Lost) return { state: "lost", why: e.message }
      if (e instanceof GaveUp) return { state: "lost", why: e.message }
      if (e instanceof Stop) {
        if (mode === "stage") return { state: "failed", words: e.message }
        try {
          // Only `importing` moves to `stopped`; anything else keeps its
          // state, with the problem and the lease let go.
          const moves = this.imp?.status === "importing"
          await this.patch({
            ...(moves ? { status: "stopped" as const } : {}),
            problem: e.message,
            runner: null,
          })
          return moves
            ? { state: "stopped", problem: e.message }
            : { state: "failed", words: e.message }
        } catch (e2) {
          if (e2 instanceof Lost) return { state: "lost", why: e2.message }
          return { state: "failed", words: describe(e2) }
        }
      }
      return { state: "failed", words: describe(e) }
    }
  }
}

/** Stages a plan as a new import (docs/plans/healthspan.md, Staging): kept
 * in this browser first, then the record, the documents and the plan, and
 * `planned`. A plan whose documents one import cannot name is refused
 * before anything goes up. */
export async function stageImport(
  req: StageRequest,
  opts: RunOptions
): Promise<RunOutcome> {
  const refusal = planRefusal(req.plan)
  if (refusal) return { state: "failed", words: refusal }
  const plan = storedPlan(req.plan)
  const entry: KeptStaging = {
    id: req.id,
    name: req.plan.name,
    importer: req.plan.importer,
    lab: req.lab,
    source: req.source,
    skipped: capSkipped(leftMarkdown(req.plan.left)),
    plan,
    documents: plan.documents.flatMap((d) => {
      const found = req.plan.documents.find((x) => x.digest === d.digest)
      return found ? [{ ...d, bytes: found.bytes }] : []
    }),
    startedAt: (opts.clock ?? realClock).now(),
  }
  // A page that cannot keep the bytes still stages from its memory; it
  // only cannot finish after a reload.
  await opts.keep?.put(entry).catch(() => {})
  return new Run(req.id, opts).run("stage", entry)
}

/** Finishes staging an import this browser began, from what it keeps. */
export async function finishStaging(
  id: string,
  opts: RunOptions
): Promise<RunOutcome> {
  const entry = await opts.keep?.get(id)
  if (!entry)
    return {
      state: "failed",
      words:
        "This browser no longer holds its files. Discard it, then import them again.",
    }
  return new Run(id, { ...opts, plan: entry.plan }).run("stage", entry)
}

/** Runs an import from where it stands: setup from `planned`, the writing
 * from `importing` (from the start: what landed answers 409 and is met
 * again) and from `stopped` (moved to `importing` with the lease), the
 * undo again from `undoing`. A staging import is not another device's to
 * resume. */
export function resumeImport(
  id: string,
  opts: RunOptions
): Promise<RunOutcome> {
  return new Run(id, opts).run("resume")
}

/** Undoes an import from any state, `undone` included (to delete what
 * landed after it); a staging import is discarded instead. */
export function undoImport(id: string, opts: RunOptions): Promise<RunOutcome> {
  return new Run(id, opts).run("undo")
}

/** Discards a staging import from any device: its record deleted
 * outright, once no other browser's lease is live. */
export function discardImport(
  id: string,
  opts: RunOptions
): Promise<RunOutcome> {
  return new Run(id, opts).run("discard")
}
