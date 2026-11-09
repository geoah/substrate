// The age score and the signals (docs/plans/healthspan.md, The age score
// from the shared kinds). The contract Today reads its findings through
// and the Healthspan screens read everything through, live on the owner's
// substrate or on the demo member, both through healthspan/read.ts's one
// transform.

import type {
  AggregateAnswer,
  AggregateRequest,
  SubstrateClient,
} from "@substrate/client"
import { useRead, type SubstrateRecord } from "@substrate/react"
import { useMemo } from "react"
import { demoHealthspanRaw, demoState } from "../demo/healthspan"
import {
  bodyMetricIds,
  ENGINE_WINDOW_DAYS,
  HRV_SDNN_METRIC,
  SIGNAL_METRICS,
  toHealthspan,
  VO2_LOOKBACK_DAYS,
  windowOf,
  type HealthspanRaw,
} from "../healthspan/read"
import { METRIC_PREFIX } from "../healthspan/records"
import { healthspanView, type HealthspanView } from "../healthspan/view"
import { KIND } from "../kinds"
import type { Finding } from "../rules/types"
import { useBackend } from "./index"
import { useMode } from "./mode"
import type { Sex } from "../kinds"
import type { Owner } from "./profile"
import { pending, settled, type Read } from "./read"

export type { HealthspanView }

export interface HealthspanData {
  /** The series, the age score, the averages and the findings, or
   * pending until the owner and every read have answered. */
  useHealthspan(): Read<HealthspanView>
  /** A contributor that slipped, a signal out of its band, a feed that
   * stopped, for Today. */
  useHealthspanFindings(): Read<Finding[]>
}

/** The owner's details as the age score reads them. */
function profileOf(o: Owner | undefined) {
  return { birthDate: o?.birthDate?.value, sex: o?.sex?.value }
}

// ---- live ----------------------------------------------------------------------

const ref = (id: string) => `${METRIC_PREFIX}${id}`
const empty: AggregateAnswer = {
  rows: [],
  exact: true,
  head: { changelog: 0, generation: "" },
}

/** An aggregate whose failure is no data: a read beside the score, which
 * a repository may refuse (a metric with no values yet). */
const optional =
  (read: (c: SubstrateClient, s: AbortSignal) => Promise<AggregateAnswer>) =>
  async (c: SubstrateClient, s: AbortSignal): Promise<AggregateAnswer> => {
    try {
      return await read(c, s)
    } catch (e) {
      if (s.aborted) throw e
      return empty
    }
  }

function zone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone
  } catch {
    return undefined
  }
}

const records = async (
  c: SubstrateClient,
  kinds: string[],
  properties: Record<string, unknown> | undefined,
  signal: AbortSignal
): Promise<SubstrateRecord[]> =>
  (await c.listAll({ kinds, properties }, 20_000, signal)).records ?? []

type ReadOf<T> = (c: SubstrateClient, signal: AbortSignal) => Promise<T>

/** Every live read, as a function of a client: the screens run each
 * through the kit's `useRead`, a script through `readHealthspan`. The
 * aggregates name only the metrics the repository declares, since a read
 * naming one it does not is refused whole. */
export function healthspanReads(today: Date) {
  const win = windowOf(today, ENGINE_WINDOW_DAYS)
  const recent = windowOf(today, 182)
  const vo2Win = windowOf(today, VO2_LOOKBACK_DAYS)
  const day = { calendar: "day" as const, fallbackTimezone: zone() }
  const aggregate =
    (body: AggregateRequest, kinds: string[]): ReadOf<AggregateAnswer> =>
    (c, signal) =>
      c.aggregate(body, { filter: { kinds }, signal })
  const none: ReadOf<AggregateAnswer> = () => Promise.resolve(empty)
  return {
    window: win,
    sources: ((c, s) => records(c, [KIND.source], undefined, s)) as ReadOf<
      SubstrateRecord[]
    >,
    metrics: ((c, s) => records(c, [KIND.metric], undefined, s)) as ReadOf<
      SubstrateRecord[]
    >,
    sessions: ((c, s) =>
      records(
        c,
        [KIND.sleep, KIND.workout],
        { at: { gte: win.from } },
        s
      )) as ReadOf<SubstrateRecord[]>,
    rhr: ((c, s) =>
      records(
        c,
        [KIND.observation],
        {
          metric: { eq: ref("health/resting_heart_rate") },
          form: { eq: "summary" },
          at: { gte: win.from },
        },
        s
      )) as ReadOf<SubstrateRecord[]>,
    signals: (ids: readonly string[]): ReadOf<AggregateAnswer> =>
      ids.length
        ? aggregate(
            {
              metrics: ids.map((id) => ({
                ref: ref(id),
                from: "summaries",
                statistic: "vendor",
              })),
              range: { gte: win.gte, lt: win.lt, recordedLocal: true },
              bucket: { ...day, place: "end" },
              reducers: ["last"],
              sources: { policy: "priority" },
              budget: { maxBuckets: 5000 },
            },
            [KIND.observation]
          )
        : none,
    hrvSdnn: (declared: boolean): ReadOf<AggregateAnswer> =>
      declared
        ? optional(
            aggregate(
              {
                metric: { ref: ref(HRV_SDNN_METRIC) },
                range: { gte: recent.gte, lt: recent.lt, recordedLocal: true },
                bucket: day,
                reducers: ["avg"],
                sources: { policy: "priority" },
              },
              [KIND.observation]
            )
          )
        : none,
    steps: (declared: boolean): ReadOf<AggregateAnswer> =>
      declared
        ? aggregate(
            {
              metric: { ref: ref("health/steps") },
              range: { gte: win.gte, lt: win.lt, recordedLocal: true },
              bucket: day,
              reducers: ["sum"],
              from: "prefer-raw",
              sources: { policy: "priority" },
            },
            [KIND.observation, KIND.trace]
          )
        : none,
    vo2: (declared: boolean): ReadOf<AggregateAnswer> =>
      declared
        ? optional(
            aggregate(
              {
                metric: { ref: ref("health/vo2_max") },
                range: { gte: vo2Win.gte, lt: vo2Win.lt, recordedLocal: true },
                bucket: day,
                reducers: ["last"],
                sources: { policy: "priority" },
              },
              [KIND.observation]
            )
          )
        : none,
    height: (declared: boolean): ReadOf<AggregateAnswer> =>
      declared
        ? optional(
            aggregate({ latest: true, metric: { ref: ref("health/height") } }, [
              KIND.observation,
            ])
          )
        : none,
    body: (ids: readonly string[]): ReadOf<SubstrateRecord[]> =>
      ids.length
        ? (c, s) =>
            records(c, [KIND.labresult], { metric: { in: ids.map(ref) } }, s)
        : () => Promise.resolve([]),
  }
}

/** What the metric records say the reads may name. */
export function declaredOf(metrics: readonly SubstrateRecord[]) {
  const declared = new Set(metrics.map((m) => m.id))
  const body = metrics.filter((m) => m.properties.category === "body")
  return {
    signals: SIGNAL_METRICS.filter((id) => declared.has(id)),
    sdnn: declared.has(HRV_SDNN_METRIC),
    steps: declared.has("health/steps"),
    vo2: declared.has("health/vo2_max"),
    height: declared.has("health/height"),
    bodyMetrics: body,
    bodyIds: bodyMetricIds(body).filter((id) => declared.has(id)),
  }
}

/** Every read at once, for a script: the records and answers the screens'
 * transform (healthspan/read.ts `toHealthspan`) and the engine's input
 * (healthspan/fromShared.ts) take. */
export async function readHealthspan(
  client: SubstrateClient,
  today: Date,
  signal: AbortSignal = new AbortController().signal
): Promise<HealthspanRaw> {
  const r = healthspanReads(today)
  const [sources, metrics, sessions, rhr] = await Promise.all([
    r.sources(client, signal),
    r.metrics(client, signal),
    r.sessions(client, signal),
    r.rhr(client, signal),
  ])
  const d = declaredOf(metrics)
  const [signals, hrvSdnn, steps, vo2, height, body] = await Promise.all([
    r.signals(d.signals)(client, signal),
    r.hrvSdnn(d.sdnn)(client, signal),
    r.steps(d.steps)(client, signal),
    r.vo2(d.vo2)(client, signal),
    r.height(d.height)(client, signal),
    r.body(d.bodyIds)(client, signal),
  ])
  return {
    sources,
    sessions,
    rhr,
    signals,
    hrvSdnn,
    steps,
    vo2,
    metrics: d.bodyMetrics,
    body,
    height,
  }
}

/** Every read the screens need, each through the kit's `useRead` so the
 * kit reads it again when its kinds change (the samples by staleAfter). */
function useLiveRaw(today: Date): Read<HealthspanRaw> {
  const r = useMemo(() => healthspanReads(today), [today])
  const at = r.window.gte
  const sources = useRead("healthspan:sources", r.sources, {
    kinds: [KIND.source],
  })
  const metrics = useRead("healthspan:metrics", r.metrics, {
    kinds: [KIND.metric],
  })
  const sessions = useRead(`healthspan:sessions:${at}`, r.sessions, {
    kinds: [KIND.sleep, KIND.workout],
  })
  const rhr = useRead(`healthspan:rhr:${at}`, r.rhr, {
    kinds: [KIND.observation],
  })
  const d = useMemo(
    () => (metrics.data ? declaredOf(metrics.data) : undefined),
    [metrics.data]
  )
  const key = (name: string, ...parts: unknown[]) =>
    d ? `healthspan:${name}:${at}:${parts.join(",")}` : null
  // A read under the priority policy follows the sources' `priorities` and
  // the source links too: a rank or a link changed here or on another
  // device reads it again.
  const ranked = (...kinds: string[]) => [
    ...kinds,
    KIND.source,
    KIND.sourcelink,
  ]
  const signals = useRead(
    key("signals", d?.signals),
    r.signals(d?.signals ?? []),
    { kinds: ranked(KIND.observation) }
  )
  const hrvSdnn = useRead(key("sdnn", d?.sdnn), r.hrvSdnn(!!d?.sdnn), {
    kinds: ranked(KIND.observation),
  })
  const steps = useRead(key("steps", d?.steps), r.steps(!!d?.steps), {
    kinds: ranked(KIND.observation, KIND.trace),
  })
  const vo2 = useRead(key("vo2", d?.vo2), r.vo2(!!d?.vo2), {
    kinds: ranked(KIND.observation),
  })
  const height = useRead(key("height", d?.height), r.height(!!d?.height), {
    kinds: ranked(KIND.observation),
  })
  const body = useRead(key("body", d?.bodyIds), r.body(d?.bodyIds ?? []), {
    kinds: [KIND.labresult],
  })

  const reads = [
    sources,
    metrics,
    sessions,
    rhr,
    signals,
    hrvSdnn,
    steps,
    vo2,
    height,
    body,
  ]
  const error = reads.find((x) => x.error !== undefined && !x.data)?.error
  const loading = reads.some((x) => x.data === undefined)
  const refreshing = reads.some((x) => x.refreshing)
  const bodyMetrics = d?.bodyMetrics
  const data = useMemo<HealthspanRaw | undefined>(
    () =>
      sources.data &&
      sessions.data &&
      rhr.data &&
      signals.data &&
      hrvSdnn.data &&
      steps.data &&
      vo2.data &&
      body.data &&
      bodyMetrics
        ? {
            sources: sources.data,
            sessions: sessions.data,
            rhr: rhr.data,
            signals: signals.data,
            hrvSdnn: hrvSdnn.data,
            steps: steps.data,
            vo2: vo2.data,
            metrics: bodyMetrics,
            body: body.data,
            height: height.data,
          }
        : undefined,
    [
      sources.data,
      sessions.data,
      rhr.data,
      signals.data,
      hrvSdnn.data,
      steps.data,
      vo2.data,
      bodyMetrics,
      body.data,
      height.data,
    ]
  )
  const refreshAll = () => {
    for (const x of reads) x.refresh()
  }
  return {
    data,
    error,
    loading: loading && error === undefined,
    refreshing,
    refresh: refreshAll,
  }
}

/** The owner's parts the screens depend on, as one key, so a new owner
 * object with the same values works nothing out again. */
function ownerKey(o: Owner | undefined): string {
  if (!o) return ""
  const p = profileOf(o)
  return JSON.stringify([
    p.birthDate,
    p.sex,
    o.profile?.nightSources ?? [],
    o.profile?.workoutSources ?? [],
  ])
}

function ownerOf(key: string) {
  const [birthDate, sex, nightSources, workoutSources] = JSON.parse(key) as [
    string | undefined,
    Sex | undefined,
    string[],
    string[],
  ]
  return {
    profile: { birthDate, sex },
    orders: { nightSources, workoutSources },
  }
}

function useLiveHealthspan(): Read<HealthspanView> {
  const { today } = useMode()
  const owner = useBackend().profile.useOwner()
  const raw = useLiveRaw(today)
  const key = ownerKey(owner.data)
  const view = useMemo(() => {
    if (!raw.data || !key) return undefined
    const o = ownerOf(key)
    return healthspanView(toHealthspan(raw.data, o.orders), o.profile, today)
  }, [raw.data, key, today])
  if (!view)
    return {
      ...pending<HealthspanView>(),
      error: raw.error ?? owner.error,
      loading: raw.error === undefined && owner.error === undefined,
      refresh: raw.refresh,
    }
  return {
    data: view,
    error: raw.error,
    loading: false,
    refreshing: raw.refreshing,
    refresh: raw.refresh,
  }
}

function findingsOf(read: Read<HealthspanView>): Read<Finding[]> {
  return { ...read, data: read.data?.findings }
}

export const liveHealthspan: HealthspanData = {
  useHealthspan: useLiveHealthspan,
  useHealthspanFindings: () => findingsOf(useLiveHealthspan()),
}

// ---- demo ----------------------------------------------------------------------

function useDemoHealthspan(): Read<HealthspanView> {
  const { today, params } = useMode()
  const owner = useBackend().profile.useOwner()
  const state = demoState(params.get("healthspan"))
  const key = ownerKey(owner.data)
  return useMemo(() => {
    if (!key) return pending<HealthspanView>()
    const o = ownerOf(key)
    const series = toHealthspan(demoHealthspanRaw(today, state), o.orders)
    return settled(healthspanView(series, o.profile, today))
  }, [today, state, key])
}

export const demoHealthspan: HealthspanData = {
  useHealthspan: useDemoHealthspan,
  useHealthspanFindings: () => findingsOf(useDemoHealthspan()),
}
