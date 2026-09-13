/** A ViewSpec, read. `viewListParams` is the whole translation: tokens
 * substituted, the mount's facet selection ANDed in (`ctx.facets`), `via`
 * folded in as an `eq` on the parent's path, a timeline's
 * `window` folded in on the kind's temporal point, the order defaulting to
 * that point else `title`, and the declared page size. A list and a contacts
 * view walk the cursor (`useViewRecords`); a board, a timeline and a detail
 * stop at one page (`useViewPage`) and say so through `incomplete` when a
 * cursor remained. A trait view has no single kind: both hooks fan out over
 * the trait's implementors, one page each, merged, and `unread` names the
 * implementors whose read failed so a partial timeline can label them. */

import {
  queryOptions,
  useInfiniteQuery,
  useQueries,
  useQuery,
} from "@tanstack/react-query"

import { installedVersions, packagesQueryOptions } from "@/lib/api/apps"
import { corePath, request } from "@/lib/api/http"
import { normalizeKinds } from "@/lib/api/kinds"
import {
  recordsInfiniteOptions,
  recordsQueryOptions,
  type ListParams,
} from "@/lib/api/records"
import type {
  KindInfo,
  OperationalList,
  RecordFilter,
  SubstrateRecord,
} from "@/lib/api/types"
import { kindByIdentity, splitKind, temporalProperties } from "@/lib/definition"
import { recordPath } from "@/lib/record-path"
import { applyFacets } from "./facets"
import type { Problem, ViewContext, ViewSpec } from "./spec"
import { parseDuration } from "./time"
import { substituteFilter } from "./tokens"
import { floorProblems } from "./view-spec"

/** The temporal point the kind's traits bind, else nothing. */
export function temporalPoint(kind: KindInfo): string | undefined {
  return temporalProperties(kind)[0]
}

/** The wire's compact orderBy: the declared first key, else the temporal
 * point ascending, else the title. */
export function orderByParam(spec: ViewSpec, kind?: KindInfo): string {
  const first = spec.orderBy[0]
  if (first) return `${first.property}:${first.desc ? "desc" : "asc"}`
  const point = kind ? temporalPoint(kind) : undefined
  return `${point ?? "title"}:asc`
}

/** The kind a `via` reference points at, so a parent record can be fetched
 * from the route alone. */
export function viaTarget(
  spec: ViewSpec,
  kind: KindInfo | undefined,
  kinds: KindInfo[]
): KindInfo | undefined {
  if (!spec.via || !kind) return undefined
  const def = (kind.definition ?? {}) as Record<string, unknown>
  const props = (def.properties ?? {}) as Record<
    string,
    Record<string, unknown>
  >
  const pin = props[spec.via]?.kind
  if (typeof pin !== "string") return undefined
  if (pin.includes("/")) return kindByIdentity(kinds, pin)
  return (
    kinds.find(
      (k) =>
        k.authority === kind.authority &&
        k.package === kind.package &&
        k.name === pin
    ) ?? kinds.find((k) => k.name === pin)
  )
}

export interface ViewQuery {
  /** Absent while a token cannot resolve; `problems` says why. */
  params?: ListParams
  problems: Problem[]
  /** The facet values the read could not honor (`facets.ts`). The read is
   * still sent, held to the view's own filter; these are for the bar. */
  notes?: Problem[]
}

/** The list read for one kind: the view's own, or one implementor of its
 * trait. */
export function viewListParams(
  spec: ViewSpec,
  ctx: ViewContext,
  kind: KindInfo
): ViewQuery {
  const substituted = substituteFilter(spec.filter, ctx)
  const problems = substituted.problems
  const facets = applyFacets(substituted.filter, ctx.facets, kind)
  const filter = facets.filter
  const properties = { ...(filter.properties ?? {}) }
  if (spec.via && ctx.parent) {
    properties[spec.via] = {
      eq: recordPath(ctx.parent.record.kind, ctx.parent.record.id),
    }
  }
  if (spec.layout === "timeline") {
    const point = temporalPoint(kind)
    const past = spec.window.past ? parseDuration(spec.window.past) : undefined
    const future = spec.window.future
      ? parseDuration(spec.window.future)
      : undefined
    if (point && (past !== undefined || future !== undefined)) {
      const now = windowNow()
      properties[point] = {
        ...(properties[point] ?? {}),
        ...(past !== undefined
          ? { gte: new Date(now - past).toISOString() }
          : {}),
        ...(future !== undefined
          ? { lte: new Date(now + future).toISOString() }
          : {}),
      }
    }
  }
  const resolved: RecordFilter = { ...filter, properties }
  if (problems.length) return { problems, notes: facets.problems }
  const { authority, pkg, name } = splitKind(kind.identity)
  return {
    params: {
      authority,
      package: pkg,
      name,
      first: spec.first,
      filter: resolved,
      orderBy: orderByParam(spec, kind),
    },
    problems,
    notes: facets.problems,
  }
}

/** The instant a timeline's window is cut around, quantized to the hour: the
 * bounds are part of the query key, and a key that moved on every render
 * would refetch on every render. One refetch an hour is the price of a
 * window written as two durations. */
const WINDOW_QUANTUM_MS = 60 * 60_000
export function windowNow(now = Date.now()): number {
  return Math.floor(now / WINDOW_QUANTUM_MS) * WINDOW_QUANTUM_MS
}

/** A stable, disabled stand-in so a hook can be called before the kind is
 * known or while a token is unresolved. */
const NO_LIST: ListParams = { authority: "", package: "", name: "" }

/** The cursor-walking read a list or a contacts view mounts over its kind. */
export function viewRecordsOptions(
  spec: ViewSpec,
  ctx: ViewContext,
  kinds: KindInfo[]
) {
  const kind = spec.kind ? kindByIdentity(kinds, spec.kind) : undefined
  const q: ViewQuery = kind ? viewListParams(spec, ctx, kind) : { problems: [] }
  return {
    ...recordsInfiniteOptions(q.params ?? NO_LIST),
    enabled: Boolean(q.params),
    problems: q.problems,
    notes: q.notes ?? [],
  }
}

/** The one-page read a board, a timeline row or a detail's related section
 * mounts over its kind. */
export function viewPageOptions(
  spec: ViewSpec,
  ctx: ViewContext,
  kinds: KindInfo[]
) {
  const kind = spec.kind ? kindByIdentity(kinds, spec.kind) : undefined
  const q: ViewQuery = kind ? viewListParams(spec, ctx, kind) : { problems: [] }
  return {
    ...recordsQueryOptions(q.params ?? NO_LIST),
    enabled: Boolean(q.params),
    problems: q.problems,
    notes: q.notes ?? [],
  }
}

/** The kinds implementing a trait, as the registry projects them:
 * `GET /api/v1/substrate.reamde.dev/core/trait/{id}/implementors` answers an
 * `OperationalList<KindInfo>`. */
export function implementorsOptions(traitIdentity: string) {
  return queryOptions({
    queryKey: ["trait", "implementors", traitIdentity],
    queryFn: async ({ signal }) => {
      const list = await request<OperationalList<KindInfo>>(
        "GET",
        `${corePath("trait", traitIdentity)}/implementors`,
        undefined,
        { signal }
      )
      return normalizeKinds(list.items ?? [])
    },
    staleTime: 5 * 60_000,
  })
}

/** The trait's implementors, or nothing for a kind view. Unconditional, so a
 * layout may call it beside its record hook. */
export function useImplementors(
  spec: Pick<ViewSpec, "trait" | "kind">
): KindInfo[] | undefined {
  const q = useQuery({
    ...implementorsOptions(spec.trait ?? ""),
    enabled: Boolean(spec.trait),
  })
  return spec.trait ? q.data : undefined
}

/** The kind identities a view reads, for the live tail: its own, or every
 * implementor once they are known. */
export function liveKinds(
  spec: ViewSpec,
  implementors: KindInfo[] | undefined
): string[] {
  if (spec.kind) return [spec.kind]
  return implementors?.map((k) => k.identity) ?? []
}

/** One page per implementor, merged. Sorted on the temporal point when every
 * implementor binds one and the view declares no order of its own; otherwise
 * left in implementor order for the layout to arrange. */
function useTraitPages(spec: ViewSpec, ctx: ViewContext) {
  const implementors = useImplementors(spec) ?? []
  const reads = implementors.map((kind) => ({
    kind,
    query: viewListParams(spec, ctx, kind),
  }))
  const results = useQueries({
    queries: reads.map(({ query }) => ({
      ...recordsQueryOptions(query.params ?? NO_LIST),
      enabled: Boolean(query.params),
    })),
  })
  const records: SubstrateRecord[] = []
  const unread: string[] = []
  let incomplete = false
  let isPending = false
  results.forEach((result, i) => {
    if (result.isError) unread.push(reads[i].kind.identity)
    if (result.isPending && reads[i].query.params) isPending = true
    if (result.data?.cursor) incomplete = true
    records.push(...(result.data?.records ?? []))
  })
  const points = implementors.map(temporalPoint)
  if (!spec.orderBy.length && points.length && points.every(Boolean)) {
    const pointOf = new Map(implementors.map((k, i) => [k.identity, points[i]]))
    records.sort((a, b) => {
      const ta = Date.parse(String(a.properties[pointOf.get(a.kind) ?? ""]))
      const tb = Date.parse(String(b.properties[pointOf.get(b.kind) ?? ""]))
      return (
        (Number.isNaN(ta) ? Infinity : ta) - (Number.isNaN(tb) ? Infinity : tb)
      )
    })
  }
  const problems = reads.flatMap(({ query }) => query.problems)
  const notes = reads.flatMap(({ query }) => query.notes ?? [])
  return {
    records,
    problems,
    notes,
    isPending: Boolean(spec.trait) && (isPending || !reads.length),
    unread,
    incomplete,
    refetch: () => results.forEach((r) => void r.refetch()),
  }
}

export interface ViewRecords {
  records: SubstrateRecord[]
  /** Token problems that kept the read from being sent. */
  problems: Problem[]
  /** Facet values the read did not honor; the read was sent without them. */
  notes?: Problem[]
  isPending: boolean
  error: Error | null
  hasNextPage: boolean
  isFetchingNextPage: boolean
  fetchNextPage: () => void
  refetch: () => void
  /** A trait view stopped at one page per implementor and one had more. */
  incomplete: boolean
  /** The implementors whose read failed. */
  unread: string[]
}

/** What a collection layout reads: every loaded page flattened, with the
 * walk's controls. A layout that stops at one page simply never calls
 * `fetchNextPage`. */
export function useViewRecords(
  spec: ViewSpec,
  ctx: ViewContext,
  kinds: KindInfo[]
): ViewRecords {
  const { problems, notes, ...options } = viewRecordsOptions(spec, ctx, kinds)
  const q = useInfiniteQuery(options)
  const trait = useTraitPages(spec, ctx)
  if (spec.trait) {
    return {
      ...trait,
      error: null,
      hasNextPage: false,
      isFetchingNextPage: false,
      fetchNextPage: () => {},
    }
  }
  return {
    records: q.data?.pages.flatMap((p) => p.records ?? []) ?? [],
    problems,
    notes,
    isPending: options.enabled ? q.isPending : false,
    error: q.error,
    hasNextPage: q.hasNextPage,
    isFetchingNextPage: q.isFetchingNextPage,
    fetchNextPage: () => void q.fetchNextPage(),
    refetch: () => void q.refetch(),
    incomplete: false,
    unread: [],
  }
}

export interface ViewPage {
  records: SubstrateRecord[]
  problems: Problem[]
  /** Facet values the read did not honor; the read was sent without them. */
  notes?: Problem[]
  isPending: boolean
  error: Error | null
  /** A cursor remained: the layout shows the first `spec.first` rows and
   * says that more were not loaded. */
  incomplete: boolean
  /** The implementors whose read failed (a trait view). */
  unread: string[]
  refetch: () => void
}

/** The one-page read a board, a timeline and a detail's related section
 * mount. */
export function useViewPage(
  spec: ViewSpec,
  ctx: ViewContext,
  kinds: KindInfo[]
): ViewPage {
  const { problems, notes, ...options } = viewPageOptions(spec, ctx, kinds)
  const q = useQuery(options)
  const trait = useTraitPages(spec, ctx)
  if (spec.trait) return { ...trait, error: null }
  return {
    records: q.data?.records ?? [],
    problems,
    notes,
    isPending: options.enabled ? q.isPending : false,
    error: q.error,
    incomplete: Boolean(q.data?.cursor),
    unread: [],
    refetch: () => void q.refetch(),
  }
}

const NO_PROBLEMS: Problem[] = []

/** The floors in `requiresAtLeast` the repository is below, off the package
 * rows (`floorProblems`); `undefined` while a view that declares a floor
 * waits on that read, so no rows are drawn against a version not yet seen.
 * A view with no floor reads nothing. A read that failed reports no
 * shortfall: the presence gate has already spoken for the view's own kind,
 * and the tail refetches the rows. */
export function useFloorProblems(
  spec: Pick<ViewSpec, "requiresAtLeast"> | undefined
): Problem[] | undefined {
  const declared = Boolean(spec && Object.keys(spec.requiresAtLeast).length)
  const packages = useQuery({ ...packagesQueryOptions(), enabled: declared })
  if (!spec || !declared) return NO_PROBLEMS
  if (packages.isPending) return undefined
  return floorProblems(spec, installedVersions(packages.data?.records))
}
