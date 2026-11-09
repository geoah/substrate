// The runner's records on the owner's substrate: the session's client, and
// the reverse read for what points at an import.

import type {
  RecordPage,
  SubstrateClient,
  SubstrateRecord,
} from "@substrate/client"
import { parseUnitGraph, unitId, type UnitGraph } from "../labs/units"
import { refPath } from "./records"
import type { RunnerStore } from "./runner"

// The repository's unit graph, read once per session: the import converts
// a lab's units through it and the runner compares them by it, rather than
// asking the server pair by pair, where every pair that does not convert is
// a refused request.
const graphs = new WeakMap<SubstrateClient, Promise<UnitGraph>>()

export function unitGraph(client: SubstrateClient): Promise<UnitGraph> {
  let g = graphs.get(client)
  if (!g) {
    g = client
      .json<unknown>("GET", "api/v1/units")
      .then(parseUnitGraph)
      .catch((e: unknown) => {
        graphs.delete(client)
        throw e
      })
    graphs.set(client, g)
  }
  return g
}

/** Every page of a list read through `filter`, as the records route
 * answers it; `limit` caps the rows. */
export async function listFiltered(
  client: SubstrateClient,
  filter: Record<string, unknown>,
  signal?: AbortSignal,
  limit = 5000
): Promise<SubstrateRecord[]> {
  const out: SubstrateRecord[] = []
  let after: string | undefined
  do {
    const page = await client.json<RecordPage>("GET", "api/v1/records", {
      query: { filter: JSON.stringify(filter), first: "500", after },
      signal,
    })
    out.push(...(page.records ?? []))
    after = page.cursor
  } while (after && out.length < limit)
  return out
}

/** How many records a filter admits: the list's own count where the
 * server answers one, else the rows counted, `keep` deciding which. */
export async function countFiltered(
  client: SubstrateClient,
  filter: Record<string, unknown>,
  keep: (r: SubstrateRecord) => boolean,
  signal?: AbortSignal
): Promise<number> {
  const page = await client.json<RecordPage & { count?: number }>(
    "GET",
    "api/v1/records",
    {
      query: { filter: JSON.stringify(filter), first: "1", count: "1" },
      signal,
    }
  )
  if (typeof page.count === "number") return page.count
  return (await listFiltered(client, filter, signal)).filter(keep).length
}

/** The records of `kinds` whose `origin` is `path`. The server's reverse
 * read finds them; each is checked again here, since a delete follows. */
export async function pointingAt(
  client: SubstrateClient,
  kinds: string[],
  path: string,
  signal?: AbortSignal
): Promise<SubstrateRecord[]> {
  const rows = await listFiltered(
    client,
    { kinds, referencing: { ref: path, property: "origin" } },
    signal
  )
  return rows.filter((r) => refPath(r.properties.origin) === path)
}

export function clientStore(client: SubstrateClient): RunnerStore {
  return {
    record: (kind, id, signal) => client.record(kind, id, signal),
    put: (kind, id, props, ifVersion, signal) =>
      client.put(kind, id, props, ifVersion, signal),
    patch: (kind, id, props, ifVersion, signal) =>
      client.patch(kind, id, props, ifVersion, signal),
    delete: (kind, id, signal) => client.delete(kind, id, signal),
    // Without the graph, units compare as spelled: a row found in the same
    // unit under another name then stops the import, which a resume after
    // the graph answers settles; it never takes another unit for this one.
    unitKey: async () => {
      try {
        const graph = await unitGraph(client)
        return (name) => unitId(graph, name) ?? name
      } catch {
        return (name) => name
      }
    },
    pointingAt: async (kinds, path, signal) =>
      (await pointingAt(client, kinds, path, signal)).map((r) => ({
        kind: r.kind,
        id: r.id,
      })),
  }
}
