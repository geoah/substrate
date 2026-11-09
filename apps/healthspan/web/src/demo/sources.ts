// The demo's sources and imports (`?demo`): the invented sources of
// demo/shared.ts, what each reports, the nights the band's re-export
// through the phone holds again (one device twice, for a link), and two
// past imports, one imported and one stopped. The records sit in a store in
// this page's memory, read through the live reads' transforms and written
// by the live runner; nothing leaves the page.

import {
  uuidv7,
  type AggregateRow,
  type SubstrateRecord,
} from "@substrate/client"
import { useMemo, useSyncExternalStore } from "react"
import type { MetricRecord } from "../catalogue/types"
import { useBackend } from "../data"
import { useMode } from "../data/mode"
import { settled, type Read } from "../data/read"
import type {
  Done,
  ImportActions,
  SourceSummary,
  SourceWrites,
  SourcesData,
} from "../data/sources"
import type { HeldResult } from "../import/match"
import { MemoryStore } from "../import/memory"
import type { PlannedImport } from "../import/plan"
import { reportProps, resultProps, refPath } from "../import/records"
import { reportId, resultId } from "../import/ids"
import { RunRegistry, type RunState } from "../import/registry"
import { factor } from "../lib/units"
import { Import, KIND, type ImportRow, type LabImport } from "../kinds"
import { buildSourceList } from "../sources/list"
import { keepLinkedTogether, linkProps, readLink } from "../sources/links"
import { priorityWrites } from "../sources/ranks"
import { heldOf, plannedSource, sortImports } from "../sources/records"
import { DEMO_SOURCES, demoSourceRecords, rng } from "./shared"

const DAY = 86_400_000

type DemoKey = keyof typeof DEMO_SOURCES

/** The shared catalogue's metrics the demo reads, as the `labs` and
 * `health` packages ship them: the public catalogue, not anyone's data. */
export const DEMO_METRICS: MetricRecord[] = [
  ["labs/ldl_cholesterol", "LDL cholesterol", "mmol/L", ["22748-8"], 2],
  [
    "labs/ldl_cholesterol_mass",
    "LDL cholesterol (mass)",
    "mg/dL",
    ["2089-1", "13457-7"],
    0,
  ],
  ["labs/hdl_cholesterol", "HDL cholesterol", "mmol/L", ["14646-4"], 2],
  [
    "labs/hdl_cholesterol_mass",
    "HDL cholesterol (mass)",
    "mg/dL",
    ["2085-9"],
    0,
  ],
  ["labs/total_cholesterol", "Total cholesterol", "mmol/L", ["14647-2"], 2],
  [
    "labs/total_cholesterol_mass",
    "Total cholesterol (mass)",
    "mg/dL",
    ["2093-3"],
    0,
  ],
  ["labs/triglycerides", "Triglycerides", "mmol/L", ["14927-8"], 2],
  ["labs/triglycerides_mass", "Triglycerides (mass)", "mg/dL", ["2571-8"], 0],
  ["labs/fasting_glucose", "Fasting glucose", "mmol/L", ["14771-0"], 1],
  ["labs/hba1c", "HbA1c", "percent", ["4548-4"], 1],
  ["labs/tsh", "TSH", "mIU/L", ["3016-3"], 2],
  ["labs/vitamin_d", "Vitamin D", "nmol/L", ["68438-1"], 0],
  ["labs/vitamin_d_mass", "Vitamin D (mass)", "ng/mL", ["62292-8"], 0],
  ["labs/ferritin", "Ferritin", "ug/L", ["2276-4"], 0],
  ["labs/crp", "C-reactive protein", "mg/L", ["1988-5"], 1],
  ["labs/haemoglobin", "Haemoglobin", "g/dL", ["718-7"], 1],
  ["labs/creatinine", "Creatinine", "umol/L", ["14682-9"], 0],
  ["labs/creatinine_mass", "Creatinine (mass)", "mg/dL", ["2160-0"], 2],
  ["labs/alt", "ALT", "U/L", ["1742-6"], 0],
  ["labs/wbc", "White blood cells", "1e9/L", ["6690-2"], 2],
  ["labs/platelets", "Platelets", "1e9/L", ["777-3"], 0],
  ["labs/vitamin_b12", "Vitamin B12", "pmol/L", ["14685-2"], 0],
  ["labs/vitamin_b12_mass", "Vitamin B12 (mass)", "pg/mL", ["2132-9"], 0],
  ["health/body_mass", "Body weight", "kg", [], 1],
  ["health/body_fat_fraction", "Body fat", "percent", [], 1],
  ["health/fat_mass", "Fat mass", "kg", [], 1],
  ["health/lean_mass", "Lean mass", "kg", [], 1],
  ["health/bone_mass", "Bone mass", "kg", [], 2],
  ["health/visceral_fat_rating", "Visceral fat rating", "score", [], 0],
  ["health/bmi", "Body mass index", "kg/m2", [], 1],
].map(([id, name, unit, loinc, precision]) => ({
  id: id as string,
  name: name as string,
  unit: unit as string,
  codes: (loinc as string[]).map((code) => ({
    system: "http://loinc.org",
    code,
  })),
  category: (id as string).startsWith("labs/") ? "labs" : "body",
  precision: precision as number,
}))

/** The `health` metrics the demo's sources report besides, for their
 * names. */
const ALL_METRICS: MetricRecord[] = [
  ...DEMO_METRICS,
  ...[
    ["health/heart_rate", "Heart rate", "1/min", "heart"],
    ["health/resting_heart_rate", "Resting heart rate", "1/min", "heart"],
    ["health/hrv_rmssd", "Heart rate variability (RMSSD)", "ms", "heart"],
    ["health/hrv_sdnn", "Heart rate variability (SDNN)", "ms", "heart"],
    ["health/spo2", "Blood oxygen saturation", "percent", "oxygen"],
    ["health/respiratory_rate", "Respiratory rate", "1/min", "respiration"],
    ["health/steps", "Steps", "steps", "activity"],
    ["health/distance", "Distance", "m", "activity"],
    ["health/vo2_max", "VO2 max", "mL/kg/min", "fitness"],
  ].map(([id, name, unit, category]) => ({
    id,
    name,
    unit,
    codes: [],
    category,
  })),
]

/** What each invented source reports, how recently, and the sessions and
 * reports it holds beside the imports. */
const REPORTS: Record<
  DemoKey,
  {
    metrics: string[]
    daysAgo: number
    nights?: number
    workouts?: number
    reports?: number
  }
> = {
  band: {
    metrics: [
      "health/resting_heart_rate",
      "health/hrv_rmssd",
      "health/respiratory_rate",
      "health/spo2",
    ],
    daysAgo: 0,
    nights: 412,
    workouts: 186,
  },
  watch: {
    metrics: [
      "health/heart_rate",
      "health/resting_heart_rate",
      "health/hrv_sdnn",
      "health/steps",
      "health/distance",
      "health/vo2_max",
    ],
    daysAgo: 0,
    workouts: 238,
  },
  phone: {
    metrics: ["health/steps", "health/distance"],
    daysAgo: 1,
  },
  bandViaPhone: {
    metrics: ["health/resting_heart_rate", "health/hrv_sdnn"],
    daysAgo: 1,
    nights: 409,
  },
  lab: {
    metrics: [
      "labs/ldl_cholesterol",
      "labs/hdl_cholesterol",
      "labs/total_cholesterol",
      "labs/triglycerides",
      "labs/fasting_glucose",
      "labs/hba1c",
      "labs/vitamin_d",
      "labs/tsh",
      "labs/ferritin",
      "labs/crp",
      "labs/creatinine",
      "labs/alt",
    ],
    daysAgo: 38,
    reports: 9,
  },
  clinic: {
    metrics: [
      "labs/haemoglobin",
      "labs/wbc",
      "labs/platelets",
      "labs/ferritin",
      "labs/vitamin_d",
      "labs/crp",
    ],
    daysAgo: 96,
    reports: 5,
  },
  dxa: {
    metrics: [
      "health/body_fat_fraction",
      "health/fat_mass",
      "health/lean_mass",
      "health/bone_mass",
      "health/visceral_fat_rating",
    ],
    daysAgo: 150,
    reports: 4,
  },
  scale: {
    metrics: ["health/body_mass", "health/body_fat_fraction", "health/bmi"],
    daysAgo: 2,
  },
}

/** The ranks the demo starts with: some groups ranked, some not, and one
 * group a source alone reports. */
const PRIORITIES: Partial<Record<DemoKey, Record<string, number>>> = {
  band: { "health/resting_heart_rate": 1, "health/hrv_rmssd": 1 },
  watch: {
    "health/resting_heart_rate": 2,
    "health/hrv_sdnn": 2,
    "health/steps": 1,
    "health/distance": 1,
  },
  phone: { "health/steps": 2, "health/distance": 2 },
  dxa: { "health/body_fat_fraction": 1, "health/lean_mass": 1 },
  scale: { "health/body_fat_fraction": 2 },
}

const metricRef = (id: string) => `${KIND.metric}/${id}`

function at(today: Date, daysAgo: number, hour = 9): string {
  const d = new Date(today.getTime() - daysAgo * DAY)
  d.setHours(hour, 0, 0, 0)
  return d.toISOString()
}

const offsetAt = (iso: string) => {
  const d = new Date(iso)
  const m = -d.getTimezoneOffset()
  const sign = m < 0 ? "-" : "+"
  const p = (n: number) => String(Math.abs(n)).padStart(2, "0")
  const local = new Date(d.getTime() + m * 60_000).toISOString().slice(0, 19)
  return `${local}${sign}${p(Math.floor(Math.abs(m) / 60))}:${p(Math.abs(m) % 60)}`
}

interface PastImport {
  name: string
  source: DemoKey
  daysAgo: number
  rows: Omit<ImportRow, "key" | "report" | "takenAt">[][]
  /** How many of its results landed; all where absent. */
  landed?: number
  problem?: string
}

const PAST: PastImport[] = [
  {
    name: "clinic-results.csv",
    source: "clinic",
    daysAgo: 120,
    rows: [
      [
        {
          metric: "labs/ldl_cholesterol",
          name: "LDL cholesterol",
          value: "3.1",
          unit: "mmol/L",
          high: "3.0",
          flag: "H",
        },
        {
          metric: "labs/hdl_cholesterol",
          name: "HDL cholesterol",
          value: "1.42",
          unit: "mmol/L",
          low: "1.0",
        },
        {
          metric: "labs/total_cholesterol",
          name: "Total cholesterol",
          value: "5.2",
          unit: "mmol/L",
          high: "5.0",
          flag: "H",
        },
        {
          metric: "labs/triglycerides",
          name: "Triglycerides",
          value: "1.18",
          unit: "mmol/L",
          high: "1.7",
        },
        {
          metric: "labs/hba1c",
          name: "HbA1c",
          value: "5.4",
          unit: "percent",
          low: "4.0",
          high: "5.6",
        },
        {
          metric: "labs/vitamin_d",
          name: "Vitamin D",
          value: "68",
          unit: "nmol/L",
          low: "50",
        },
      ],
    ],
  },
  {
    name: "spring-panel.csv",
    source: "lab",
    daysAgo: 220,
    rows: [
      [
        {
          metric: "labs/tsh",
          name: "TSH",
          value: "1.9",
          unit: "mIU/L",
          low: "0.4",
          high: "4.0",
        },
        {
          metric: "labs/crp",
          name: "CRP",
          value: "0.6",
          unit: "mg/L",
          comparator: "<",
          high: "5",
        },
        {
          metric: "labs/ferritin",
          name: "Ferritin",
          value: "112",
          unit: "ug/L",
          low: "30",
          high: "400",
        },
      ],
      [
        {
          metric: "labs/creatinine",
          name: "Creatinine",
          value: "84",
          unit: "umol/L",
          low: "59",
          high: "104",
        },
        {
          metric: "labs/alt",
          name: "ALT",
          value: "22",
          unit: "U/L",
          high: "41",
        },
      ],
    ],
    landed: 2,
    problem:
      "Result 3 (Ferritin) was refused: the unit it was written in does not convert to the metric's unit.",
  },
]

interface World {
  store: MemoryStore
  registry: RunRegistry
  today: Date
}

let world: World | undefined

function seedPast(store: MemoryStore, today: Date) {
  for (const p of PAST) {
    const when = today.getTime() - p.daysAgo * DAY
    const id = uuidv7(when)
    const source = DEMO_SOURCES[p.source]
    const reports = p.rows.map((_, i) => ({
      key: String(i + 1),
      takenAt: offsetAt(at(today, p.daysAgo - i * 7, 8)),
    }))
    const rows: ImportRow[] = []
    p.rows.forEach((group, i) =>
      group.forEach((r) =>
        rows.push({
          ...r,
          key: String(rows.length + 1),
          report: String(i + 1),
          takenAt: reports[i].takenAt,
        })
      )
    )
    const landed = p.landed ?? rows.length
    const target = { id, source: source.id, lab: source.name }
    const stamp = at(today, p.daysAgo, 10)
    const record = (
      kind: string,
      rid: string,
      properties: Record<string, unknown>
    ): SubstrateRecord => ({
      id: rid,
      kind,
      properties,
      version: 1,
      createdAt: stamp,
      updatedAt: stamp,
    })
    store.seed(
      record(KIND.import, id, {
        ...Import.write({
          name: p.name,
          format: "csv",
          status: p.problem ? "stopped" : "imported",
          source: source.id,
          rows,
          reports,
          results: landed,
          ...(p.problem ? { problem: p.problem } : { importedAt: stamp }),
        }),
      })
    )
    const reportsLanded = new Set(rows.slice(0, landed).map((r) => r.report))
    for (const rep of reports)
      if (!p.problem || reportsLanded.has(rep.key))
        store.seed(
          record(
            KIND.labreport,
            reportId(id, rep.key),
            reportProps(target, rep)
          )
        )
    for (const r of rows.slice(0, landed))
      store.seed(
        record(KIND.labresult, resultId(id, r.key), resultProps(target, r))
      )
  }
}

function worldFor(today: Date): World {
  if (world && world.today.getTime() === today.getTime()) return world
  const store = new MemoryStore(today.getTime())
  for (const r of demoSourceRecords()) {
    const key = (Object.keys(DEMO_SOURCES) as DemoKey[]).find(
      (k) => DEMO_SOURCES[k].id === r.id
    )
    const pr = key ? PRIORITIES[key] : undefined
    store.seed(
      pr
        ? {
            ...r,
            properties: {
              ...r.properties,
              priorities: Object.fromEntries(
                Object.entries(pr).map(([m, v]) => [metricRef(m), v])
              ),
            },
          }
        : r
    )
  }
  seedPast(store, today)
  world = {
    store,
    today,
    registry: new RunRegistry(store, {
      device: "demo-browser",
      lockPrefix: "healthspan.demo.import.",
    }),
  }
  return world
}

function useWorld(): World {
  const { today } = useMode()
  const w = worldFor(today)
  useSyncExternalStore(w.store.subscribe, () => w.store.version)
  return w
}

/** The latest read's rows the demo's sources answer, then what its
 * imports wrote. */
function latestRows(w: World): AggregateRow[] {
  const random = rng(7)
  const rows: AggregateRow[] = []
  for (const key of Object.keys(REPORTS) as DemoKey[]) {
    const r = REPORTS[key]
    for (const m of r.metrics) {
      const t = at(w.today, r.daysAgo, 6 + Math.floor(random() * 12))
      rows.push({
        start: t,
        end: t,
        group: {
          metric: metricRef(m),
          source: `${KIND.source}/${DEMO_SOURCES[key].id}`,
        },
        values: [{ reducer: "last", value: (random() * 100).toFixed(1) }],
        count: 1,
      })
    }
  }
  for (const r of w.store.all(KIND.labresult)) {
    const metric = refPath(r.properties.metric)
    const source = refPath(r.properties.source)
    const t = r.properties.at
    if (!metric || !source || typeof t !== "string") continue
    rows.push({
      start: t,
      end: t,
      group: { metric, source },
      values: [{ reducer: "last", value: null }],
      count: 1,
    })
  }
  return rows
}

function countBy(records: SubstrateRecord[]): Map<string, number> {
  const out = new Map<string, number>()
  const prefix = `${KIND.source}/`
  for (const r of records) {
    const p = refPath(r.properties.source)
    if (!p?.startsWith(prefix)) continue
    const id = p.slice(prefix.length)
    out.set(id, (out.get(id) ?? 0) + 1)
  }
  return out
}

function useDemoSourceList(): Read<SourceSummary[]> {
  const w = useWorld()
  const version = w.store.version
  return useMemo(() => {
    void version
    const nights = new Map<string, number>()
    const workouts = new Map<string, number>()
    const reports = countBy(w.store.all(KIND.labreport))
    for (const key of Object.keys(REPORTS) as DemoKey[]) {
      const r = REPORTS[key]
      const id = DEMO_SOURCES[key].id
      if (r.nights) nights.set(id, r.nights)
      if (r.workouts) workouts.set(id, r.workouts)
      if (r.reports) reports.set(id, (reports.get(id) ?? 0) + r.reports)
    }
    return settled(
      buildSourceList({
        sources: w.store.all(KIND.source),
        latest: latestRows(w),
        nights,
        workouts,
        reports,
      })
    )
  }, [w, version])
}

function useDemoLinks() {
  const w = useWorld()
  const version = w.store.version
  return useMemo(() => {
    void version
    return settled(w.store.all(KIND.sourcelink).map(readLink))
  }, [w, version])
}

function useDemoSourceWrites(): SourceWrites {
  const w = useWorld()
  const profile = useBackend().profile.useProfileWrites()
  return useMemo<SourceWrites>(() => {
    const rankSessions = async (
      kind: "nights" | "workouts",
      order: string[]
    ): Promise<Done> => {
      await profile.change(
        kind === "nights" ? { nightSources: order } : { workoutSources: order }
      )
      return { ok: true }
    }
    return {
      rankMetrics: async (group, order) => {
        for (const p of priorityWrites(
          group,
          order,
          ALL_METRICS.map((m) => m.id)
        ))
          await w.store.patch(KIND.source, p.id, { priorities: p.priorities })
        return { ok: true }
      },
      rankSessions,
      link: async (name, sources, orders) => {
        const id = uuidv7()
        await w.store.put(KIND.sourcelink, id, linkProps(name, sources), 0)
        const link = { id, version: 1, name, sources }
        for (const kind of ["nights", "workouts"] as const) {
          const next = keepLinkedTogether(orders[kind], [link])
          if (next.join(" ") !== orders[kind].join(" "))
            await rankSessions(kind, next)
        }
        return { ok: true }
      },
      unlink: async (link) => {
        await w.store.delete(KIND.sourcelink, link.id)
        return { ok: true }
      },
    }
  }, [w, profile])
}

function useDemoImports(): Read<LabImport[]> {
  const w = useWorld()
  const version = w.store.version
  return useMemo(() => {
    void version
    return settled(
      sortImports(w.store.all(KIND.import).map((r) => Import.read(r)))
    )
  }, [w, version])
}

function useDemoImport(id: string): Read<LabImport | null> {
  const list = useDemoImports().data ?? []
  const found = list.find((i) => i.id === id) ?? null
  return useMemo(() => settled(found), [found])
}

function useDemoStillPointing(id: string, enabled: boolean): Read<number> {
  const w = useWorld()
  const version = w.store.version
  return useMemo(() => {
    void version
    if (!enabled) return settled(0)
    const path = `${KIND.import}/${id}`
    const n = [
      ...w.store.all(KIND.labresult),
      ...w.store.all(KIND.labreport),
    ].filter((r) => refPath(r.properties.origin) === path).length
    return settled(n)
  }, [w, version, id, enabled])
}

function useDemoRunState(id: string): RunState {
  const w = useWorld()
  return useSyncExternalStore(w.registry.subscribe, () => w.registry.state(id))
}

/** Conversions the demo knows: the linear ones the app's own table holds,
 * and the few more its metrics need. */
const MORE: Record<string, Record<string, number>> = {
  "mmol/L": { "umol/L": 1000 },
  "umol/L": { "mmol/L": 0.001 },
  "mg/dL": { "mg/L": 10 },
  "mg/L": { "mg/dL": 0.1 },
  "g/dL": { "g/L": 10 },
  "nmol/L": { "pmol/L": 1000 },
  "pmol/L": { "nmol/L": 0.001 },
  percent: { "%": 1 },
  "%": { percent: 1 },
}

export function demoConvert(
  value: string,
  from: string,
  to: string
): string | undefined {
  const f = factor(from, to) ?? MORE[from]?.[to]
  if (f === undefined) return undefined
  return String(Number(value) * f)
}

function useDemoImportActions(): ImportActions {
  const w = useWorld()
  return useMemo<ImportActions>(
    () => ({
      convert: (value, from, to) =>
        Promise.resolve(demoConvert(value, from, to)),
      held: (metrics, days) => {
        const want = new Set(metrics)
        const on = new Set(days)
        return Promise.resolve(
          w.store
            .all(KIND.labresult)
            .flatMap((r): HeldResult[] => heldOf(r))
            .filter((h) => want.has(h.metric) && on.has(h.day))
        )
      },
      start: async (planned: PlannedImport[]) => {
        const ids: string[] = []
        for (const p of planned) {
          const id = uuidv7()
          const source = plannedSource(p.lab, w.store.all(KIND.source))
          await w.store.put(
            KIND.import,
            id,
            Import.write({
              name: p.name,
              format: p.format,
              lab: p.lab,
              ...(source ? { source } : {}),
              rows: p.rows,
              reports: p.reports,
              results: 0,
              ...(p.skipped ? { skipped: p.skipped } : {}),
            }),
            0
          )
          w.registry.rememberLab(id, p.lab)
          ids.push(id)
        }
        void (async () => {
          for (const id of ids) await w.registry.resume(id)
        })()
        return ids
      },
      resume: (id) => w.registry.resume(id),
      undo: (id) => w.registry.undo(id),
    }),
    [w]
  )
}

export const demoSourcesData: SourcesData = {
  useSourceList: useDemoSourceList,
  useMetrics: () => settled(ALL_METRICS),
  useLabMetrics: () => settled(DEMO_METRICS),
  useSourceLinks: useDemoLinks,
  useSourceWrites: useDemoSourceWrites,
  useImports: useDemoImports,
  useImport: useDemoImport,
  useStillPointing: useDemoStillPointing,
  useRunState: useDemoRunState,
  useImportActions: useDemoImportActions,
}
