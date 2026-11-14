/** The actor records. Every hand that writes is one
 * `substrate.reamde.dev/core/actor` record the engine writes: the
 * repository's person, an app the person approved, an installed function or
 * agent, a package's own installs, and the system (decision 0270). A change,
 * a property's manager and every stored attribution name a hand by its
 * record's id, never by a spelling.
 *
 * The records change on an install or an approval, so one cached read serves
 * every actor the console names. It walks every page (`walkPages`, so a
 * cursor refused `compacted` starts the walk over): an actor is never
 * deleted, so a repository that installed and removed enough code holds
 * more than any one page, the disabled ones included, and History filters
 * and names a change by the whole set. */

import { queryOptions } from "@tanstack/react-query"

import { walkPages } from "./compacted"
import {
  CORE_AUTHORITY,
  CORE_PACKAGE,
  CORE_PACKAGE_NAME,
  request,
} from "./http"
import { listPath } from "./records"
import type { Page, SubstrateRecord } from "./types"

/** The page the walk reads at a time. */
export const ACTORS_PAGE = 500

/** The types of actor, the values of an actor record's `type`. */
export type ActorType =
  "person" | "app" | "function" | "agent" | "package" | "system"

/** One actor record, as the console reads it. */
export interface ActorInfo {
  /** The record's id: what a change and a manager link. */
  id: string
  type: ActorType
  /** The name the record gives the hand. */
  name: string
  /** The identity of the function, the agent or the package the actor runs
   * or installs (`<authority>/<package>/<name>`, a package's
   * `<authority>/<package>`), where it has one. */
  link?: string
  /** An app's publisher: its verified authority, its host, or `local`. */
  publisher?: string
  disabled: boolean
}

const TYPES: readonly ActorType[] = [
  "person",
  "app",
  "function",
  "agent",
  "package",
  "system",
]

function refID(value: unknown, kind: string): string | undefined {
  const ref = (value as { ref?: unknown } | undefined)?.ref
  if (typeof ref !== "string") return undefined
  const prefix = `${CORE_PACKAGE}/${kind}/`
  return ref.startsWith(prefix) ? ref.slice(prefix.length) : undefined
}

/** The console's reading of one actor record; undefined for a record of no
 * type it knows. */
export function actorInfoOf(record: SubstrateRecord): ActorInfo | undefined {
  const props = record.properties ?? {}
  const type = props.type
  if (typeof type !== "string" || !TYPES.includes(type as ActorType))
    return undefined
  const link =
    type === "function" || type === "agent" || type === "package"
      ? refID(props[type], type)
      : undefined
  return {
    id: record.id,
    type: type as ActorType,
    name: typeof props.name === "string" ? props.name : "",
    link,
    publisher:
      typeof props.publisher === "string" ? props.publisher : undefined,
    disabled: props.disabled === true,
  }
}

export const actorsQueryOptions = queryOptions({
  queryKey: ["actors"],
  queryFn: async ({ signal }): Promise<ActorInfo[]> => {
    const { pages } = await walkPages(
      `${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}/actor`,
      (after: string | undefined) =>
        request<Page>(
          "GET",
          listPath({
            authority: CORE_AUTHORITY,
            package: CORE_PACKAGE_NAME,
            name: "actor",
            first: ACTORS_PAGE,
            after,
          }),
          undefined,
          { signal }
        ),
      (page) => page.cursor || undefined
    )
    const out: ActorInfo[] = []
    for (const page of pages)
      for (const record of page.records ?? []) {
        const info = actorInfoOf(record)
        if (info) out.push(info)
      }
    return out
  },
  staleTime: 5 * 60_000,
})

/** The id an actor record is linked by in a stored reference
 * (`{ref: "substrate.reamde.dev/core/actor/<id>"}`), undefined for anything
 * else. */
export function actorRefID(value: unknown): string | undefined {
  return refID(value, "actor")
}
