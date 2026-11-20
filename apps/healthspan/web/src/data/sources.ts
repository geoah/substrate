// The sources of measured values and the imports (docs/plans/healthspan.md,
// Sources; Import). The contract getting started and the age score read
// sources through, and the Sources and Import screens' reads and writes,
// live (sources/live.ts) and demo (demo/sources.ts).

import type { MetricRecord } from "../catalogue/types"
import { demoSourcesData } from "../demo/sources"
import type { ImportRecord } from "../kinds"
import type { UnitGraph } from "../labs/units"
import type { HeldResult, Convert } from "../import/match"
import type { StoredPlan } from "../import/planfile"
import type { RunState } from "../import/registry"
import type { RunOutcome, StageRequest } from "../import/runner"
import type { MetricGroup, SessionKind } from "../sources/groups"
import type { SourceLink } from "../sources/links"
import { liveSourcesData } from "../sources/live"
import type { Read } from "./read"

/** One `observations/source` record and what it holds. */
export interface SourceSummary {
  /** The id after `observations/source/`. */
  id: string
  name: string
  class?: string
  /** The metrics it reports, by metric id, with its newest value's time. */
  metrics: { metric: string; latest?: string }[]
  /** How many `health/sleep` and `health/workout` records it holds. */
  nights: number
  workouts: number
  /** For a lab: how many reports. */
  reports: number
  /** Its rank per metric, keyed by the metric's id, 1 first. */
  priorities: Record<string, number>
}

/** A write's end, in words where it was not kept. */
export type Done = { ok: true } | { ok: false; words: string }

export interface SourceWrites {
  /** Writes a metric group's order: each source's `priorities` for every
   * metric of the group, its place in `order` (1 first), through the
   * outbox. */
  rankMetrics(group: MetricGroup, order: SourceSummary[]): Promise<Done>
  /** Writes a session group's order, source ids first highest, to the
   * profile. */
  rankSessions(kind: SessionKind, order: string[]): Promise<Done>
  /** Links two or more sources as one device, and moves them together in
   * the night and workout orders shown (`orders`). */
  link(
    name: string,
    sources: string[],
    orders: Record<SessionKind, string[]>
  ): Promise<Done>
  unlink(link: SourceLink): Promise<Done>
}

export interface ImportActions {
  /** One conversion through the repository's unit graph. */
  convert: Convert
  /** The repository's unit graph, which an importer reads through. */
  units: () => Promise<UnitGraph>
  /** The results the repository holds for these metrics on these days. */
  held: (metrics: string[], days: string[]) => Promise<HeldResult[]>
  /** Stages each plan as a new import, one after another, and runs each
   * once it is `planned`: answers how each staging ended, while the runs
   * go on. */
  start: (requests: StageRequest[]) => Promise<RunOutcome[]>
  /** Finishes staging an import this browser began, then runs it. */
  finish: (id: string) => Promise<RunOutcome>
  resume: (id: string) => Promise<RunOutcome>
  undo: (id: string) => Promise<RunOutcome>
  /** Deletes a staging import outright, from any device. */
  discard: (id: string) => Promise<RunOutcome>
}

export interface SourcesData {
  /** Every source, in no particular order. */
  useSourceList(): Read<SourceSummary[]>
  /** Every metric record of the repository: the names the screens show,
   * and the metrics a group's priorities may name. */
  useMetrics(): Read<MetricRecord[]>
  /** The metric records of category `labs` or `body`, which an import
   * matches against. */
  useLabMetrics(): Read<MetricRecord[]>
  useSourceLinks(): Read<SourceLink[]>
  useSourceWrites(): SourceWrites
  /** Every import, newest first. */
  useImports(): Read<ImportRecord[]>
  /** One import; null where it is not in the repository. */
  useImport(id: string): Read<ImportRecord | null>
  /** The plan an import writes, read from its blob; null where it has none
   * yet (staging). */
  usePlan(imp: ImportRecord): Read<StoredPlan | null>
  /** How many records point at an import through `origin`: what it
   * created, whatever its runs counted, and for an undone one what landed
   * after the undo. */
  useCreated(imp: ImportRecord): Read<number>
  /** The staging imports this browser keeps the files of, which it can
   * finish. */
  useKept(): ReadonlySet<string>
  /** A run of an import in this page: its progress and how it ended. */
  useRunState(id: string): RunState
  useImportActions(): ImportActions
}

export const liveSources: SourcesData = liveSourcesData

export const demoSources: SourcesData = demoSourcesData
