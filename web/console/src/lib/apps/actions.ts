/** The verb dispatch. Every write a view makes goes through `runAction`, so
 * the CAS precondition, the idempotency key, the optimistic cache rewrite and
 * the Undo arm live in one place:
 *
 * - `transition`: `PATCH {properties: {[state]: to}, ifVersion}`; the record
 *   is rewritten in every cached page first and dropped from pages whose
 *   filter it no longer matches (`cond.ts`), a toast offers Undo along the
 *   declared return arm when one exists, a `conflict` refetches and says so,
 *   a `guard` names the move the machine refused.
 * - `patch`: the same over `set` plus the prompted values.
 * - `create`: `POST` with an `Idempotency-Key` minted once per pending form,
 *   seeded from `set`, from `via` and from every `eq` on a writable property
 *   in the filter, so a filtered view creates rows it will show.
 * - `delete`: `DELETE ?ifVersion=`; the button confirms first, always.
 * - `link`: opens the url-typed property's VALUE in a new tab, never a
 *   template.
 * - `open`: pushes `/views/{view}/{record}` through the screen's navigate.
 * - `call`, `chat`: not in this prototype; a toast says so.
 *
 * The UI around a verb (the prompt sheet, the confirm) is the button's; this
 * receives the collected values and runs. */

import type { InfiniteData, QueryClient } from "@tanstack/react-query"

import { toast } from "@/components/ui/toast"
import { createRecord, deleteRecord, patchRecord } from "@/lib/api/records"
import {
  ApiError,
  type KindInfo,
  type Page,
  type RecordFilter,
  type SubstrateRecord,
} from "@/lib/api/types"
import { splitKind } from "@/lib/definition"
import { recordPath } from "@/lib/record-path"
import { ownerWritable } from "@/lib/record-schema"
import { matchesFilter, matchesWhen, specOf } from "./cond"
import { admits, returnArm, stateSpecOf } from "./machine"
import { titleOf } from "./referents"
import type {
  ActionHost,
  ActionSpec,
  Problem,
  ViewContext,
  ViewSpec,
} from "./spec"
import { substituteSet } from "./tokens"

/** How long an Undo stays offered. */
export const UNDO_MS = 5_000

export interface RunActionArgs {
  action: ActionSpec
  spec: ViewSpec
  kind?: KindInfo
  kinds: KindInfo[]
  ctx: ViewContext
  queryClient: QueryClient
  /** The row, for every verb but create. */
  record?: SubstrateRecord
  /** The typed values a form collected for a create or a patch. */
  values?: Record<string, unknown>
  /** Minted once per pending create and reused on retry. */
  idempotencyKey?: string
  /** How an `open` pushes its view with the row as its record. */
  navigate?: (to: { view: string; record: SubstrateRecord }) => void
}

export type ActionOutcome =
  { ok: true; record?: SubstrateRecord } | { ok: false; problems: Problem[] }

function refused(message: string, path = "action"): ActionOutcome {
  return { ok: false, problems: [{ path, message, severity: "error" }] }
}

export interface Transition {
  property: string
  from?: string
  to: string
}

/** The move an action makes on a record: the state property (named, else the
 * kind's sole one), the record's current state and the target. */
export function transitionOf(
  action: ActionSpec,
  kind: KindInfo | undefined,
  record?: SubstrateRecord
): Transition | undefined {
  if (action.verb !== "transition" || !action.to || !kind) return undefined
  const state = stateSpecOf(kind, action.property)
  if (!state) return undefined
  const from = record?.properties[state.name]
  return {
    property: state.name,
    from: typeof from === "string" ? from : undefined,
    to: action.to,
  }
}

/** Whether a row offers the action: its `when` holds, a transition is
 * admitted from the row's state, and a link has a value to open. */
export function actionApplies(
  action: ActionSpec,
  record: SubstrateRecord,
  kind?: KindInfo
): boolean {
  if (action.when && !matchesWhen(record, action.when, kind)) return false
  if (action.verb === "transition") {
    const t = transitionOf(action, kind, record)
    return Boolean(t && kind && admits(kind, t.property, t.from, t.to))
  }
  if (action.verb === "link") {
    return typeof linkOf(action, record) === "string"
  }
  return true
}

/** The row-placed actions a row admits, in declared order. */
export function rowActionsFor(
  host: ActionHost,
  record: SubstrateRecord
): ActionSpec[] {
  return host.spec.actions.filter(
    (a) => a.placement === "row" && actionApplies(a, record, host.kind)
  )
}

/** The url a link opens: the property's value when it is an http(s) url. */
export function linkOf(
  action: ActionSpec,
  record: SubstrateRecord
): string | undefined {
  if (!action.href) return undefined
  const value = record.properties[action.href]
  return typeof value === "string" && /^https?:\/\//i.test(value)
    ? value
    : undefined
}

/** What a create is born with before the form asks anything: `set`, the
 * `via` parent, and every `eq` the filter holds on a writable property. */
export function createSeed(
  action: ActionSpec,
  spec: ViewSpec,
  kind: KindInfo | undefined,
  ctx: ViewContext,
  record?: SubstrateRecord
): { properties: Record<string, unknown>; problems: Problem[] } {
  const properties: Record<string, unknown> = {}
  for (const [name, cond] of Object.entries(spec.filter.properties ?? {})) {
    if (cond.eq === undefined || typeof cond.eq === "object") continue
    if (typeof cond.eq === "string" && cond.eq.startsWith("$")) continue
    const prop = specOf(kind, name)
    if (!prop || prop.managed || !ownerWritable(prop)) continue
    properties[name] =
      prop.kind === "reference" ? { ref: String(cond.eq) } : cond.eq
  }
  if (spec.via && ctx.parent) {
    properties[spec.via] = {
      ref: recordPath(ctx.parent.record.kind, ctx.parent.record.id),
    }
  }
  const set = substituteSet(
    action.set,
    ctx,
    kind,
    `actions.${action.name}.set`,
    record
  )
  return {
    properties: { ...properties, ...set.properties },
    problems: set.problems,
  }
}

// ── cache surgery ───────────────────────────────────────────────────────────

type Cached = Page | InfiniteData<Page>

/** The filter a `["records", a, p, n, ...]` key was read under. */
function filterOfKey(key: readonly unknown[]): RecordFilter | undefined {
  const tail = key[key.length - 1]
  if (!tail || typeof tail !== "object") return undefined
  const filter = (tail as { filter?: RecordFilter | null }).filter
  return filter ?? undefined
}

function rewriteCaches(
  queryClient: QueryClient,
  kind: KindInfo,
  edit: (records: SubstrateRecord[], filter?: RecordFilter) => SubstrateRecord[]
) {
  const { authority, pkg, name } = splitKind(kind.identity)
  const prefix = ["records", authority, pkg, name]
  for (const [key, data] of queryClient.getQueriesData<Cached>({
    queryKey: prefix,
  })) {
    if (!data) continue
    const filter = filterOfKey(key)
    if ("pages" in data) {
      queryClient.setQueryData<InfiniteData<Page>>(key, {
        ...data,
        pages: data.pages.map((page) => ({
          ...page,
          records: edit(page.records ?? [], filter),
        })),
      })
    } else {
      queryClient.setQueryData<Page>(key, {
        ...data,
        records: edit(data.records ?? [], filter),
      })
    }
  }
}

/** Put a record's new shape in every cached page, dropping it from pages
 * whose filter it no longer matches. */
export function updateCaches(
  queryClient: QueryClient,
  kind: KindInfo,
  updated: SubstrateRecord
) {
  rewriteCaches(queryClient, kind, (records, filter) =>
    records.flatMap((r) => {
      if (r.id !== updated.id) return [r]
      return matchesFilter(updated, filter, kind) ? [updated] : []
    })
  )
  const { authority, pkg, name } = splitKind(kind.identity)
  const single = ["record", authority, pkg, name, updated.id]
  if (queryClient.getQueryData(single) !== undefined) {
    queryClient.setQueryData(single, updated)
  }
}

/** Show a created record at the head of every cached page it matches. */
export function insertIntoCaches(
  queryClient: QueryClient,
  kind: KindInfo,
  created: SubstrateRecord
) {
  let placed = false
  rewriteCaches(queryClient, kind, (records, filter) => {
    if (placed || records.some((r) => r.id === created.id)) return records
    if (!matchesFilter(created, filter, kind)) return records
    placed = true
    return [created, ...records]
  })
}

export function removeFromCaches(
  queryClient: QueryClient,
  kind: KindInfo,
  id: string
) {
  rewriteCaches(queryClient, kind, (records) =>
    records.filter((r) => r.id !== id)
  )
}

export function invalidateKind(queryClient: QueryClient, kind: KindInfo) {
  const { authority, pkg, name } = splitKind(kind.identity)
  for (const head of ["records", "records-count", "record"]) {
    void queryClient.invalidateQueries({
      queryKey: [head, authority, pkg, name],
    })
  }
}

// ── the verbs ───────────────────────────────────────────────────────────────

function describe(error: unknown): string {
  if (error instanceof ApiError && error.problems.length) {
    return error.problems.join("; ")
  }
  return error instanceof Error ? error.message : String(error)
}

function failureToast(title: string, error: unknown) {
  toast.add({ type: "error", title, description: describe(error) })
}

async function runTransition(
  args: RunActionArgs,
  t: Transition,
  offerUndo: boolean
): Promise<ActionOutcome> {
  const { kind, record, queryClient } = args
  if (!kind || !record) return refused("a transition needs a record")
  const { authority, pkg, name } = splitKind(kind.identity)
  const optimistic: SubstrateRecord = {
    ...record,
    properties: { ...record.properties, [t.property]: t.to },
    version: record.version + 1,
  }
  updateCaches(queryClient, kind, optimistic)
  try {
    const saved = await patchRecord(authority, pkg, name, record.id, {
      properties: { [t.property]: t.to },
      ifVersion: record.version,
    })
    updateCaches(queryClient, kind, saved)
    invalidateKind(queryClient, kind)
    const back: Transition = {
      property: t.property,
      from: t.to,
      to: t.from ?? "",
    }
    if (offerUndo && returnArm(kind, t.property, t.from, t.to)) {
      const id = toast.add({
        type: "success",
        title: `${titleOf(saved)}: ${t.to}`,
        timeout: UNDO_MS,
        actionProps: {
          children: "Undo",
          onClick: () => {
            toast.close(id)
            void runTransition({ ...args, record: saved }, back, false)
          },
        },
      })
    } else {
      toast.add({ type: "success", title: `${titleOf(saved)}: ${t.to}` })
    }
    return { ok: true, record: saved }
  } catch (error) {
    updateCaches(queryClient, kind, record)
    invalidateKind(queryClient, kind)
    if (error instanceof ApiError && error.code === "conflict") {
      toast.add({
        type: "warning",
        title: "Changed elsewhere",
        description: "The record moved since you read it; reloaded.",
      })
    } else if (error instanceof ApiError && error.code === "guard") {
      toast.add({
        type: "error",
        title: `${t.from ?? "?"} to ${t.to} is not admitted`,
        description: error.message,
      })
    } else {
      failureToast("Could not move the record", error)
    }
    return refused(describe(error))
  }
}

async function runPatch(args: RunActionArgs): Promise<ActionOutcome> {
  const { action, kind, record, ctx, queryClient, values } = args
  if (!kind || !record) return refused("a patch needs a record")
  const set = substituteSet(
    action.set,
    ctx,
    kind,
    `actions.${action.name}.set`,
    record
  )
  if (set.problems.length) return { ok: false, problems: set.problems }
  const properties = { ...set.properties, ...(values ?? {}) }
  const { authority, pkg, name } = splitKind(kind.identity)
  const optimistic: SubstrateRecord = {
    ...record,
    properties: { ...record.properties, ...properties },
    version: record.version + 1,
  }
  updateCaches(queryClient, kind, optimistic)
  try {
    const saved = await patchRecord(authority, pkg, name, record.id, {
      properties,
      ifVersion: record.version,
    })
    updateCaches(queryClient, kind, saved)
    invalidateKind(queryClient, kind)
    toast.add({ type: "success", title: `${titleOf(saved)} updated` })
    return { ok: true, record: saved }
  } catch (error) {
    updateCaches(queryClient, kind, record)
    invalidateKind(queryClient, kind)
    if (error instanceof ApiError && error.code === "conflict") {
      toast.add({
        type: "warning",
        title: "Changed elsewhere",
        description: "The record moved since you read it; reloaded.",
      })
    } else {
      failureToast("Could not update the record", error)
    }
    return refused(describe(error))
  }
}

async function runCreate(args: RunActionArgs): Promise<ActionOutcome> {
  const { action, spec, kind, ctx, queryClient, values, record } = args
  if (!kind) return refused("a create needs the view's kind")
  const seed = createSeed(action, spec, kind, ctx, record)
  if (seed.problems.length) return { ok: false, problems: seed.problems }
  const properties = { ...seed.properties, ...(values ?? {}) }
  const { authority, pkg, name } = splitKind(kind.identity)
  try {
    const created = await createRecord(
      authority,
      pkg,
      name,
      { properties },
      { idempotencyKey: args.idempotencyKey }
    )
    insertIntoCaches(queryClient, kind, created)
    invalidateKind(queryClient, kind)
    toast.add({ type: "success", title: `${titleOf(created)} created` })
    return { ok: true, record: created }
  } catch (error) {
    failureToast(`Could not create the ${kind.name}`, error)
    return refused(describe(error))
  }
}

async function runDelete(args: RunActionArgs): Promise<ActionOutcome> {
  const { kind, record, queryClient } = args
  if (!kind || !record) return refused("a delete needs a record")
  const { authority, pkg, name } = splitKind(kind.identity)
  try {
    await deleteRecord(authority, pkg, name, record.id, record.version)
    removeFromCaches(queryClient, kind, record.id)
    invalidateKind(queryClient, kind)
    toast.add({ type: "success", title: `${titleOf(record)} deleted` })
    return { ok: true }
  } catch (error) {
    invalidateKind(queryClient, kind)
    failureToast("Could not delete the record", error)
    return refused(describe(error))
  }
}

export async function runAction(args: RunActionArgs): Promise<ActionOutcome> {
  const { action, kind, record } = args
  switch (action.verb) {
    case "transition": {
      const t = transitionOf(action, kind, record)
      if (!t) return refused("the transition names no state")
      return runTransition(args, t, true)
    }
    case "patch":
      return runPatch(args)
    case "create":
      return runCreate(args)
    case "delete":
      return runDelete(args)
    case "link": {
      const url = record ? linkOf(action, record) : undefined
      if (!url) return refused(`${action.href ?? "href"} holds no url`)
      window.open(url, "_blank", "noopener,noreferrer")
      return { ok: true }
    }
    case "open":
      if (!action.view || !record) {
        return refused("open needs a view and a row")
      }
      args.navigate?.({ view: action.view, record })
      return { ok: true }
    case "call":
    case "chat":
      toast.add({
        type: "info",
        title: `${action.label}: not in this prototype`,
        description: `A ${action.verb} lands with the function and agent sheets.`,
      })
      return refused(`${action.verb} is not in this prototype`)
    default:
      return refused(`unknown verb ${String(action.verb)}`)
  }
}
