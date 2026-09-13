/** An app's inputs resolved the way the engine resolves a bundle's
 * (`internal/engine/inputs.go`): the bound record, else the record whose id
 * is `default`, else the sole live record of the kind, else nothing. Two
 * records with neither a binding nor a `default` are `ambiguous`, and the
 * screen offers the picker. A binding that names a record the kind no longer
 * holds, or a record of another kind, is `missing` and NEVER falls through to
 * `default` or the sole record: the owner chose whose data the app shows, and
 * a fallback would change it silently. This twin is v0; the engine projects
 * `status.inputs` on the app row in v1 and this file goes. */

import { useQueries } from "@tanstack/react-query"

import { recordsQueryOptions } from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindByIdentity, splitKind } from "@/lib/definition"
import { recordPath, splitRecordPath } from "@/lib/record-path"
import type { AppSpec, InputSpec } from "./spec"

/** One page comfortably above any configuration kind's record count. */
const INPUT_PAGE = 200

export type InputState =
  | { state: "bound" | "default" | "sole"; record: SubstrateRecord }
  | { state: "ambiguous"; options: SubstrateRecord[] }
  | { state: "missing"; binding: string; options: SubstrateRecord[] }
  | { state: "none" }
  | { state: "unknown-kind" }
  | { state: "loading" }

export function resolveInput(
  binding: string | undefined,
  candidates: SubstrateRecord[],
  kindIdentity?: string
): InputState {
  if (binding) {
    const parts = splitRecordPath(binding)
    const bound =
      parts && (!kindIdentity || parts.kind === kindIdentity)
        ? candidates.find((r) => recordPath(r.kind, r.id) === binding)
        : undefined
    if (bound) return { state: "bound", record: bound }
    return { state: "missing", binding, options: candidates }
  }
  const byDefault = candidates.find((r) => r.id === "default")
  if (byDefault) return { state: "default", record: byDefault }
  if (candidates.length === 1) return { state: "sole", record: candidates[0] }
  if (candidates.length === 0) return { state: "none" }
  return { state: "ambiguous", options: candidates }
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
    default:
      return undefined
  }
}

export interface ResolvedInputs {
  states: Record<string, InputState>
  /** The record per input, undefined while unresolved. */
  records: Record<string, SubstrateRecord | undefined>
  /** Every record of each input's kind, the settings sheet's picker list;
   * empty while the page is loading or the kind is unknown. */
  candidates: Record<string, SubstrateRecord[]>
  /** The kind identities the inputs read, for the live tail. */
  kinds: string[]
}

/** Every input of an app resolved against one page of each input's kind. */
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
  return useQueries({
    queries: entries.map(([, , kind]) => {
      const { authority, pkg, name } = splitKind(kind?.identity ?? "")
      return {
        ...recordsQueryOptions({
          authority,
          package: pkg,
          name,
          first: INPUT_PAGE,
        }),
        enabled: Boolean(kind),
      }
    }),
    combine: (results) => {
      const states: Record<string, InputState> = {}
      const records: Record<string, SubstrateRecord | undefined> = {}
      const candidates: Record<string, SubstrateRecord[]> = {}
      entries.forEach(([name, , kind], i) => {
        const result = results[i]
        let state: InputState
        if (!kind) state = { state: "unknown-kind" }
        else if (!result.data) state = { state: "loading" }
        else
          state = resolveInput(
            app?.bindings[name],
            result.data.records ?? [],
            kind.identity
          )
        states[name] = state
        records[name] = "record" in state ? state.record : undefined
        candidates[name] = kind ? (result.data?.records ?? []) : []
      })
      return {
        states,
        records,
        candidates,
        kinds: entries
          .map(([, , kind]) => kind?.identity)
          .filter((k): k is string => Boolean(k)),
      }
    },
  })
}
