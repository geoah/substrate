// The runner's records on the owner's substrate: the session's client, and
// the reverse read for what points at an import.

import {
  SubstrateClient,
  failure,
  type RecordPage,
  type SubstrateRecord,
} from "@substrate/client"
import {
  parseUnitGraph,
  unitId,
  unitSpellingKey,
  type UnitGraph,
} from "../labs/units"
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

/** A blob's bytes up, by their digest: `PUT /api/v1/blobs/{digest}`, the
 * name as `?name=` and the media type as `Content-Type`. */
async function putBlob(
  client: SubstrateClient,
  digest: string,
  bytes: Uint8Array,
  mediaType: string,
  name: string,
  signal?: AbortSignal
): Promise<void> {
  const headers = client.headers()
  headers.set("Content-Type", mediaType || "application/octet-stream")
  const res = await fetch(
    client.url(`api/v1/blobs/${encodeURIComponent(digest)}`, { name }),
    {
      method: "PUT",
      headers,
      body: bytes as Uint8Array<ArrayBuffer>,
      signal,
      redirect: "error",
    }
  )
  if (!res.ok) throw await failure(res)
}

/** A blob's bytes, which a live record of a kind the token reads names. */
async function getBlob(
  client: SubstrateClient,
  digest: string,
  signal?: AbortSignal
): Promise<Uint8Array> {
  const res = await fetch(
    client.url(`api/v1/blobs/${encodeURIComponent(digest)}`),
    {
      headers: client.headers("*/*"),
      signal,
      redirect: "error",
    }
  )
  if (!res.ok) throw await failure(res)
  return new Uint8Array(await res.arrayBuffer())
}

export function clientStore(client: SubstrateClient): RunnerStore {
  return {
    record: (kind, id, signal) => client.record(kind, id, signal),
    tombstone: async (kind, id, signal) => {
      const page = await client.json<RecordPage>("GET", "api/v1/records", {
        query: {
          filter: JSON.stringify({ kinds: [kind], ids: [id], deleted: true }),
          first: "1",
        },
        signal,
      })
      return page.records?.find((r) => r.id === id && r.kind === kind) ?? null
    },
    put: (kind, id, props, ifVersion, signal) =>
      client.put(kind, id, props, ifVersion, signal),
    patch: (kind, id, props, ifVersion, signal) =>
      client.patch(kind, id, props, ifVersion, signal),
    delete: async (kind, id, ifVersion, signal) => {
      await client.send("DELETE", SubstrateClient.recordPath(kind, id), {
        query: {
          ifVersion: ifVersion === undefined ? undefined : String(ifVersion),
        },
        accept: [404],
        signal,
      })
    },
    putBlob: (digest, bytes, mediaType, name, signal) =>
      putBlob(client, digest, bytes, mediaType, name, signal),
    blob: (digest, signal) => getBlob(client, digest, signal),
    // A unit the graph does not hold, or every unit when the graph cannot
    // be read, compares by the server's spelling key, which is how it was
    // stored: a row found in the same unit under another name then stops
    // the import, which a resume after the graph answers settles; it never
    // takes another unit for this one.
    unitKey: async () => {
      try {
        const graph = await unitGraph(client)
        return (name) => unitId(graph, name) ?? unitSpellingKey(name)
      } catch {
        return unitSpellingKey
      }
    },
    pointingAt: async (kinds, path, signal) =>
      (await pointingAt(client, kinds, path, signal)).map((r) => ({
        kind: r.kind,
        id: r.id,
      })),
  }
}
