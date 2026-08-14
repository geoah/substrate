/** The actor grammar's data side. Actors are records: the engine mirrors one
 * `core.substrate.reamde.dev/actor` record per declared actor (its id IS the actor name)
 * — EXCEPT a single-writer bundle whose actor shares its authority's name
 * (record 60): that name's one record is the `authority` mirror. So an actor id
 * resolves against `actors` first, then `authorities`; a name in neither is
 * unregistered but its changelog is still real and browsable.
 *
 * Both mirrors are registry-shaped (tens of rows, changing on bundle install),
 * so one cached read serves everything actor-flavored: the facet's known-name
 * list and the actor view's resolution — no per-actor probing. */

import { queryOptions } from "@tanstack/react-query"

import { corePath, request } from "./http"
import type { SubstrateRecord, Page } from "./types"

const ACTORS = "actors"
const AUTHORITIES = "authorities"
/** One generous page reads a mirror whole. */
const MIRROR_PAGE = 500

export interface ActorMirrors {
  /** `core.substrate.reamde.dev/actor` rows, id = actor name. */
  actors: SubstrateRecord[]
  /** `core.substrate.reamde.dev/authority` rows — authority-named actors live here. */
  authorities: SubstrateRecord[]
}

async function fetchMirror(
  plural: string,
  signal?: AbortSignal
): Promise<SubstrateRecord[]> {
  const q = new URLSearchParams({ first: String(MIRROR_PAGE) })
  const page = await request<Page>(
    "GET",
    `${corePath(plural)}?${q}`,
    undefined,
    { signal }
  )
  return page.records ?? []
}

export const actorMirrorsQueryOptions = queryOptions({
  queryKey: ["actors", "mirrors"],
  queryFn: async ({ signal }): Promise<ActorMirrors> => {
    const [actors, authorities] = await Promise.all([
      fetchMirror(ACTORS, signal),
      fetchMirror(AUTHORITIES, signal),
    ])
    return { actors, authorities }
  },
  staleTime: 5 * 60_000,
})

/** Every declared actor name — mirror ids plus each authority's declared
 * `actors` — for the changelog's actor facet. */
export function actorNames(mirrors: ActorMirrors): string[] {
  const names = new Set<string>()
  for (const e of mirrors.actors) if (e.id) names.add(e.id)
  for (const a of mirrors.authorities) {
    const declared = a.properties?.actors
    if (!Array.isArray(declared)) continue
    for (const name of declared)
      if (typeof name === "string" && name) names.add(name)
  }
  return [...names].sort()
}

/** Where an actor's record actually lives. */
export interface ResolvedActor {
  record: SubstrateRecord
  /** The collection holding it — `actors` for mirror rows, `authorities` for
   * record-60 authority-named bundle actors. */
  collection: typeof ACTORS | typeof AUTHORITIES
}

/** null is a definitive "unregistered", not an error — the actor view renders
 * an identity stub over the name's real changelog. */
export function resolveActor(
  mirrors: ActorMirrors,
  id: string
): ResolvedActor | null {
  const mirror = mirrors.actors.find((e) => e.id === id)
  if (mirror) return { record: mirror, collection: ACTORS }
  const authority = mirrors.authorities.find((e) => e.id === id)
  if (authority) return { record: authority, collection: AUTHORITIES }
  return null
}
