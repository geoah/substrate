// Sources and imports on the owner's substrate. The source records, the
// latest read, the counts and the links are reads beside the linked
// records (`useRead`), read again when their kinds change; the imports are
// the app's own linked records. A rank goes through the outbox, a link and
// every step of an import through the client, each followed by an
// invalidation of what it changed.

import { describe, uuidv7, type SubstrateClient } from "@substrate/client"
import {
  useInvalidate,
  useListed,
  useRead,
  useRecord,
  useRecords,
  useSubstrate,
  useWrites,
} from "@substrate/react"
import { useMemo, useSyncExternalStore } from "react"
import type { MetricRecord } from "../catalogue/types"
import { useBackend } from "../data"
import type { Read } from "../data/read"
import type {
  Done,
  ImportActions,
  SourceSummary,
  SourceWrites,
  SourcesData,
} from "../data/sources"
import { indexedDbKeep } from "../import/keep"
import type { HeldResult } from "../import/match"
import { parsePlan, type StoredPlan } from "../import/planfile"
import { RunRegistry, deviceId } from "../import/registry"
import { originKinds, refPath } from "../import/records"
import type { StageRequest } from "../import/runner"
import {
  clientStore,
  countFiltered,
  listFiltered,
  unitGraph,
} from "../import/store"
import { Import, KIND, type ImportRecord } from "../kinds"
import { convert as convertUnit } from "../labs/units"
import { metricPath } from "./groups"
import { buildSourceList } from "./list"
import { keepLinkedTogether, linkProps, readLink } from "./links"
import { priorityWrites } from "./ranks"
import {
  heldOf,
  isLabMetric,
  plannedSource,
  readMetric,
  sortImports,
} from "./records"

const SOURCE_KINDS = [KIND.source]
const LATEST_KINDS = [KIND.labresult, KIND.observation, KIND.trace]
const COUNT_KINDS = [KIND.sleep, KIND.workout, KIND.labreport, KIND.source]
const LINK_KINDS = [KIND.sourcelink]
const METRIC_KINDS = [KIND.metric]

/** Counts per source id of one kind's records, for the sources given. */
async function countsBySource(
  client: SubstrateClient,
  kind: string,
  ids: string[],
  signal: AbortSignal
): Promise<Map<string, number>> {
  const out = new Map<string, number>()
  const total = await countFiltered(
    client,
    { kinds: [kind] },
    () => true,
    signal
  )
  if (!total) return out
  for (const id of ids) {
    const path = `${KIND.source}/${id}`
    const n = await countFiltered(
      client,
      { kinds: [kind], properties: { source: { eq: path } } },
      (r) => refPath(r.properties.source) === path,
      signal
    )
    if (n) out.set(id, n)
  }
  return out
}

/** The most groups a latest read may answer (the server's cap). */
const LATEST_GROUP_CAP = 2000

function useLiveSourceList(): Read<SourceSummary[]> {
  const records = useRead(
    "healthspan sources",
    (client, signal) => listFiltered(client, { kinds: SOURCE_KINDS }, signal),
    { kinds: SOURCE_KINDS }
  )
  // One read per kind, at the server's group cap (a latest read answers one
  // group per metric and source, and its default of 200 is passed by one
  // repository's labs alone), the rows put together.
  const latest = useRead(
    "healthspan sources latest",
    async (client, signal) => {
      const answers = await Promise.all(
        LATEST_KINDS.map((kind) =>
          client.aggregate(
            {
              latest: true,
              groupBy: ["metric", "source"],
              budget: { maxGroups: LATEST_GROUP_CAP },
            },
            { filter: { kinds: [kind] }, signal }
          )
        )
      )
      return { ...answers[0], rows: answers.flatMap((a) => a.rows) }
    },
    { kinds: LATEST_KINDS }
  )
  const counts = useRead(
    "healthspan sources counts",
    async (client, signal) => {
      const sources = await listFiltered(
        client,
        { kinds: SOURCE_KINDS },
        signal
      )
      const all = sources.map((r) => r.id)
      const labs = sources
        .filter((r) => r.properties.class === "lab")
        .map((r) => r.id)
      const others = all.filter((id) => !labs.includes(id))
      const [nights, workouts, reports] = await Promise.all([
        countsBySource(client, KIND.sleep, others, signal),
        countsBySource(client, KIND.workout, others, signal),
        countsBySource(client, KIND.labreport, labs, signal),
      ])
      return { nights, workouts, reports }
    },
    { kinds: COUNT_KINDS }
  )
  const data = useMemo(() => {
    if (!records.data || !latest.data || !counts.data) return undefined
    return buildSourceList({
      sources: records.data,
      latest: latest.data.rows,
      ...counts.data,
    })
  }, [records.data, latest.data, counts.data])
  return {
    data,
    error: records.error ?? latest.error ?? counts.error,
    loading: !data && (records.loading || latest.loading || counts.loading),
    refreshing: records.refreshing || latest.refreshing || counts.refreshing,
    refresh: () => {
      records.refresh()
      latest.refresh()
      counts.refresh()
    },
  }
}

function useLiveMetrics(): Read<MetricRecord[]> {
  return useRead(
    "healthspan metrics",
    async (client, signal) =>
      (await listFiltered(client, { kinds: METRIC_KINDS }, signal)).map(
        readMetric
      ),
    { kinds: METRIC_KINDS }
  )
}

function useLiveLabMetrics(): Read<MetricRecord[]> {
  const read = useLiveMetrics()
  const data = useMemo(() => read.data?.filter(isLabMetric), [read.data])
  return useMemo(() => ({ ...read, data }), [read, data])
}

function useLiveSourceLinks() {
  return useRead(
    "healthspan source links",
    async (client, signal) =>
      (await listFiltered(client, { kinds: LINK_KINDS }, signal)).map(readLink),
    { kinds: LINK_KINDS }
  )
}

function useLiveSourceWrites(): SourceWrites {
  const writes = useWrites()
  const invalidate = useInvalidate()
  const { client } = useSubstrate()
  const profile = useBackend().profile.useProfileWrites()
  const metrics = useLiveMetrics().data
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
        const known = (metrics ?? []).map((m) => m.id)
        for (const w of priorityWrites(group, order, known)) {
          const r = await writes.change(KIND.source, w.id, {
            priorities: w.priorities,
          })
          if (!r.kept) return { ok: false, words: r.words }
        }
        invalidate(SOURCE_KINDS)
        return { ok: true }
      },
      rankSessions,
      link: async (name, sources, orders) => {
        const id = uuidv7()
        try {
          await client.put(KIND.sourcelink, id, linkProps(name, sources), 0)
        } catch (e) {
          return { ok: false, words: describe(e) }
        }
        invalidate(LINK_KINDS)
        const link = { id, version: 1, name, sources }
        for (const kind of ["nights", "workouts"] as const) {
          const next = keepLinkedTogether(orders[kind], [link])
          if (next.join(" ") !== orders[kind].join(" "))
            await rankSessions(kind, next)
        }
        return { ok: true }
      },
      unlink: async (link) => {
        try {
          await client.delete(KIND.sourcelink, link.id)
        } catch (e) {
          return { ok: false, words: describe(e) }
        }
        invalidate(LINK_KINDS)
        return { ok: true }
      },
    }
  }, [writes, invalidate, client, profile, metrics])
}

function useLiveImports(): Read<ImportRecord[]> {
  const records = useRecords(Import)
  const listed = useListed(Import)
  return useMemo(
    () => ({
      data: listed || records.length ? sortImports(records) : undefined,
      error: undefined,
      loading: !listed && !records.length,
      refreshing: false,
      refresh: () => {},
    }),
    [records, listed]
  )
}

function useLiveImport(id: string): Read<ImportRecord | null> {
  const read = useRecord(Import, id)
  return useMemo(
    () => ({
      data:
        read.state === "present"
          ? read.value
          : read.state === "absent"
            ? null
            : undefined,
      error: read.state === "failed" ? read.words : undefined,
      loading: read.state === "loading",
      refreshing: false,
      refresh: read.state === "failed" ? read.retry : () => {},
    }),
    [read]
  )
}

const registries = new WeakMap<SubstrateClient, RunRegistry>()

function registryFor(client: SubstrateClient): RunRegistry {
  let r = registries.get(client)
  if (!r) {
    r = new RunRegistry(clientStore(client), {
      device: deviceId(),
      lockPrefix: "healthspan.import.",
      keep: indexedDbKeep(client.server),
    })
    registries.set(client, r)
  }
  return r
}

function useLivePlan(imp: ImportRecord): Read<StoredPlan | null> {
  const { client } = useSubstrate()
  const digest = imp.plan
  const read = useRead(
    digest ? `healthspan plan ${digest}` : null,
    async (c, signal) =>
      registryFor(client).plan(imp.id) ??
      parsePlan(await clientStore(c).blob(digest!, signal)),
    // A blob is the same bytes forever: nothing changes it.
    { staleAfter: Number.POSITIVE_INFINITY }
  )
  return useMemo(
    () => (digest ? read : { ...read, data: null, loading: false }),
    [digest, read]
  )
}

function useLiveCreated(imp: ImportRecord): Read<number> {
  const path = `${KIND.import}/${imp.id}`
  const kinds = originKinds(imp.importer)
  return useRead(
    imp.status === "staging" ? null : `healthspan created ${imp.id}`,
    async (client, signal) => {
      let n = 0
      // The lab kinds every repository holds; the clinical ones apart, so a
      // repository without them still counts the rest.
      for (const group of [
        kinds.filter((k) => k.includes("/labs/")),
        kinds.filter((k) => !k.includes("/labs/")),
      ]) {
        if (!group.length) continue
        try {
          n += await countFiltered(
            client,
            { kinds: group, referencing: { ref: path, property: "origin" } },
            (r) => refPath(r.properties.origin) === path,
            signal
          )
        } catch (e) {
          if (group[0].includes("/labs/")) throw e
        }
      }
      return n
    },
    { kinds }
  )
}

function useLiveKept(): ReadonlySet<string> {
  const { client } = useSubstrate()
  const registry = registryFor(client)
  return useSyncExternalStore(registry.subscribe, registry.keptIds)
}

function useLiveRunState(id: string) {
  const { client } = useSubstrate()
  const registry = registryFor(client)
  return useSyncExternalStore(registry.subscribe, () => registry.state(id))
}

const WRITTEN = [KIND.labresult, KIND.labreport, KIND.source, KIND.import]

function useLiveImportActions(): ImportActions {
  const { client } = useSubstrate()
  const invalidate = useInvalidate()
  return useMemo<ImportActions>(() => {
    const registry = registryFor(client)
    const after = async <T>(p: Promise<T>): Promise<T> => {
      try {
        return await p
      } finally {
        invalidate(WRITTEN)
      }
    }
    return {
      convert: async (value, from, to) => {
        const v = convertUnit(await unitGraph(client), Number(value), from, to)
        return v === undefined ? undefined : String(v)
      },
      units: () => unitGraph(client),
      held: async (metrics, days) => {
        if (!metrics.length || !days.length) return []
        const sorted = [...days].sort()
        const from = new Date(`${sorted[0]}T00:00:00`)
        from.setDate(from.getDate() - 1)
        const to = new Date(`${sorted[sorted.length - 1]}T00:00:00`)
        to.setDate(to.getDate() + 2)
        try {
          const rows = await listFiltered(client, {
            kinds: [KIND.labresult],
            properties: {
              metric: { in: metrics.map(metricPath) },
              at: { gte: from.toISOString(), lt: to.toISOString() },
            },
          })
          return rows.flatMap((r): HeldResult[] => heldOf(r))
        } catch {
          // Without the read, nothing is marked: every row stays ticked.
          return []
        }
      },
      start: async (requests: StageRequest[]) => {
        // A lab of the same name the repository holds is named at once.
        const labs = requests.some((r) => r.lab)
          ? await listFiltered(client, { kinds: SOURCE_KINDS })
          : []
        const out = await registry.start(
          requests.map((r) =>
            r.lab && !r.source
              ? { ...r, source: plannedSource(r.lab, labs) }
              : r
          )
        )
        invalidate([KIND.import])
        return out
      },
      finish: (id) => after(registry.finish(id)),
      resume: (id) => after(registry.resume(id)),
      undo: (id) => after(registry.undo(id)),
      discard: (id) => after(registry.discard(id)),
    }
  }, [client, invalidate])
}

export const liveSourcesData: SourcesData = {
  useSourceList: useLiveSourceList,
  useMetrics: useLiveMetrics,
  useLabMetrics: useLiveLabMetrics,
  useSourceLinks: useLiveSourceLinks,
  useSourceWrites: useLiveSourceWrites,
  useImports: useLiveImports,
  useImport: useLiveImport,
  usePlan: useLivePlan,
  useCreated: useLiveCreated,
  useKept: useLiveKept,
  useRunState: useLiveRunState,
  useImportActions: useLiveImportActions,
}
