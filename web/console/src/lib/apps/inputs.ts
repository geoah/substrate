/** An app's inputs resolved the way the engine resolves a bundle's
 * (`internal/engine/inputs.go`): the bound record, else the record whose id
 * is `default`, else the sole live record of the kind, else nothing. Two
 * records with neither a binding nor a `default` are `ambiguous`, and the
 * screen offers the picker. A binding that names a record the kind no longer
 * holds, or a record of another kind, is `missing` and NEVER falls through to
 * `default` or the sole record: the owner chose whose data the app shows, and
 * a fallback would change it silently.
 *
 * Each step is its own read, never a page searched: a binding and `default`
 * are fetched BY ID, and none/sole/ambiguous is told apart by a two-record
 * probe, because nothing in `app.inputs.kind` keeps an input's kind small
 * and a page has a far side. The picker's rows are a separate page, with a
 * cursor when the kind holds more. A read that fails is `error`, not a
 * loading state that never ends. This twin is v0; the engine projects
 * `status.inputs` on the app row in v1 and this file goes. */

import { useQueries } from "@tanstack/react-query"

import { recordQueryOptions, recordsQueryOptions } from "@/lib/api/records"
import { ApiError, type KindInfo, type SubstrateRecord } from "@/lib/api/types"
import { kindByIdentity, splitKind } from "@/lib/definition"
import { splitRecordPath } from "@/lib/record-path"
import type { AppSpec, InputSpec } from "./spec"

/** The well-known id of resolution step 2 (engine/inputs.go). */
export const INPUT_DEFAULT_ID = "default"

/** Two rows tell none, one and several apart; a third says nothing more. */
const PROBE = 2

/** The picker's page. */
export const INPUT_OPTIONS_PAGE = 50

export type InputState =
  | { state: "bound" | "default" | "sole"; record: SubstrateRecord }
  | { state: "ambiguous"; options: SubstrateRecord[]; more?: boolean }
  | {
      state: "missing"
      binding: string
      options: SubstrateRecord[]
      more?: boolean
    }
  | { state: "none" }
  | { state: "unknown-kind" }
  | { state: "loading" }
  | { state: "error"; message: string }

/** What the reads answered so far. `undefined` is unread; `null` is the
 * server saying there is no such record. */
export interface InputReads {
  /** The bound record, fetched by the binding's id. */
  bound?: SubstrateRecord | null
  /** The record whose id is `default`, fetched by that id. */
  byDefault?: SubstrateRecord | null
  /** The first two live records of the kind, in the collection's order. */
  probe?: SubstrateRecord[]
}

/** The three-step rule over the reads, each step waiting on its own read
 * alone. A binding of another kind or of no path at all is `missing` before
 * anything is fetched. */
export function resolveInput(
  binding: string | undefined,
  reads: InputReads,
  kindIdentity?: string
): InputState {
  if (binding) {
    const parts = splitRecordPath(binding)
    if (!parts || (kindIdentity && parts.kind !== kindIdentity)) {
      return { state: "missing", binding, options: [] }
    }
    if (reads.bound === undefined) return { state: "loading" }
    if (reads.bound === null) return { state: "missing", binding, options: [] }
    return { state: "bound", record: reads.bound }
  }
  if (reads.byDefault === undefined) return { state: "loading" }
  if (reads.byDefault) return { state: "default", record: reads.byDefault }
  if (reads.probe === undefined) return { state: "loading" }
  if (reads.probe.length === 0) return { state: "none" }
  if (reads.probe.length === 1) return { state: "sole", record: reads.probe[0] }
  return { state: "ambiguous", options: reads.probe }
}

/** What an unresolved input says on the screen; undefined once it resolves. */
export function inputStatus(
  name: string,
  state: InputState,
  kind?: KindInfo
): string | undefined {
  const noun = kind?.name ?? "record"
  switch (state.state) {
    case "missing":
      return `\`${name}\` is bound to ${state.binding}, which no longer exists`
    case "none":
      return `no ${noun} connected yet for \`${name}\``
    case "unknown-kind":
      return `\`${name}\` needs a kind this repository does not have`
    case "ambiguous":
      return `pick which ${noun} is \`${name}\``
    case "error":
      return `\`${name}\` could not be read: ${state.message}`
    default:
      return undefined
  }
}

export interface ResolvedInputs {
  states: Record<string, InputState>
  /** The shape a `ViewContext.inputs` takes: the record per input, undefined
   * while unresolved. */
  records: Record<string, SubstrateRecord | undefined>
  /** The kind identities the inputs read, for the live tail. */
  kinds: string[]
}

/** The picker's first page of one kind, with a cursor when there is more. */
export function inputOptionsQueryOptions(kindIdentity: string) {
  const { authority, pkg, name } = splitKind(kindIdentity)
  return recordsQueryOptions({
    authority,
    package: pkg,
    name,
    first: INPUT_OPTIONS_PAGE,
  })
}

/** A single-record read's answer as a step sees it: the record, `null` for
 * the server's not-found, `undefined` while unread; any other failure is the
 * error itself. */
function byId(result: {
  data: SubstrateRecord | undefined
  error: Error | null
}): SubstrateRecord | null | undefined | Error {
  if (result.data) return result.data
  if (result.error instanceof ApiError && result.error.code === "not_found") {
    return null
  }
  return result.error ?? undefined
}

/** A stable, disabled stand-in so every input mounts the same three reads. */
const NO_KIND = { authority: "", pkg: "", name: "" }

/** Every input of an app resolved: three reads each (the binding by id,
 * `default` by id, the probe), enabled by what the step needs, then one
 * page of options for an input the owner has to pick. */
export function useAppInputs(
  app: AppSpec | undefined,
  kinds: KindInfo[]
): ResolvedInputs {
  const entries: [string, InputSpec, KindInfo | undefined][] = Object.entries(
    app?.inputs ?? {}
  ).map(([name, input]) => [
    name,
    input,
    input.kind ? kindByIdentity(kinds, input.kind) : undefined,
  ])
  const steps = useQueries({
    queries: entries.flatMap(([name, , kind]) => {
      const {
        authority,
        pkg,
        name: coll,
      } = kind ? splitKind(kind.identity) : NO_KIND
      const binding = app?.bindings[name]
      const parts = binding ? splitRecordPath(binding) : undefined
      const boundId =
        kind && parts && parts.kind === kind.identity ? parts.id : undefined
      return [
        {
          ...recordQueryOptions(authority, pkg, coll, boundId ?? ""),
          enabled: Boolean(kind && boundId),
        },
        {
          ...recordQueryOptions(authority, pkg, coll, INPUT_DEFAULT_ID),
          enabled: Boolean(kind && !binding),
        },
        {
          ...recordsQueryOptions({
            authority,
            package: pkg,
            name: coll,
            first: PROBE,
          }),
          enabled: Boolean(kind && !binding),
        },
      ]
    }),
    combine: (results) =>
      entries.map(([name, , kind], i): InputState => {
        if (!kind) return { state: "unknown-kind" }
        const [bound, byDefault, probe] = results.slice(i * 3, i * 3 + 3)
        const boundRead = byId(
          bound as { data: SubstrateRecord | undefined; error: Error | null }
        )
        const defaultRead = byId(
          byDefault as {
            data: SubstrateRecord | undefined
            error: Error | null
          }
        )
        // Only the step that decides may fail it: a binding is its own read;
        // without one, `default` answers before the probe is heard, so a
        // probe that failed beside a resolved `default` says nothing.
        const decider = app?.bindings[name]
          ? boundRead
          : defaultRead === null
            ? probe.error
            : defaultRead
        if (decider instanceof Error) {
          return { state: "error", message: decider.message }
        }
        const page = probe.data as { records?: SubstrateRecord[] } | undefined
        return resolveInput(
          app?.bindings[name],
          {
            bound: boundRead as SubstrateRecord | null | undefined,
            byDefault: defaultRead as SubstrateRecord | null | undefined,
            probe: page ? (page.records ?? []) : undefined,
          },
          kind.identity
        )
      }),
  })
  const pickable = (state: InputState) =>
    state.state === "ambiguous" || state.state === "missing"
  const options = useQueries({
    queries: entries.map(([, , kind], i) => ({
      ...inputOptionsQueryOptions(kind?.identity ?? ""),
      enabled: Boolean(kind) && pickable(steps[i]),
    })),
    combine: (results) =>
      results.map((r) => ({
        records: r.data?.records,
        more: Boolean(r.data?.cursor),
      })),
  })
  const states: Record<string, InputState> = {}
  const records: Record<string, SubstrateRecord | undefined> = {}
  entries.forEach(([name], i) => {
    let state = steps[i]
    const page = options[i]
    if (pickable(state) && page.records) {
      state = { ...state, options: page.records, more: page.more }
    }
    states[name] = state
    records[name] = "record" in state ? state.record : undefined
  })
  return {
    states,
    records,
    kinds: entries
      .map(([, , kind]) => kind?.identity)
      .filter((k): k is string => Boolean(k)),
  }
}
