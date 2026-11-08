import { describe, type SubstrateRecord } from "@substrate/client"
import { useMemo } from "react"
import { useSubstrate } from "./SubstrateApp"
import { useToast } from "./toast"

export interface Writes {
  /** Creates a record under an id the device chose: a PUT every retry lands
   * on the same row. */
  create: (
    kind: string,
    id: string,
    properties: Record<string, unknown>
  ) => Promise<SubstrateRecord | null>
  /** Merges properties into a record (null clears one); a state moves only
   * here. `ifVersion` holds the write to the version the owner saw. */
  patch: (
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    ifVersion?: number
  ) => Promise<SubstrateRecord | null>
  /** A plain DELETE, no purge. */
  remove: (kind: string, id: string) => Promise<boolean>
}

function merged(
  record: SubstrateRecord,
  properties: Record<string, unknown>
): SubstrateRecord {
  const next = { ...record.properties }
  for (const [k, v] of Object.entries(properties)) {
    if (v === null) delete next[k]
    else next[k] = v
  }
  return { ...record, properties: next }
}

/** The app's writes: each shown at once, replaced by what the server
 * answers, and undone with the server's words when it is refused. */
export function useWrites(): Writes {
  const { client, collection } = useSubstrate()
  const toast = useToast()
  return useMemo<Writes>(
    () => ({
      async create(kind, id, properties) {
        const now = new Date().toISOString()
        const undo = collection.apply(kind, id, {
          id,
          kind,
          properties,
          version: 0,
          createdAt: now,
          updatedAt: now,
        })
        try {
          const r = await client.put(kind, id, properties, 0)
          collection.landed(r)
          return r
        } catch (e) {
          undo()
          toast(describe(e), "error")
          return null
        }
      },
      async patch(kind, id, properties, ifVersion) {
        const here = collection.get(kind, id)
        const undo = here
          ? collection.apply(kind, id, merged(here, properties))
          : () => {}
        try {
          const r = await client.patch(kind, id, properties, ifVersion)
          collection.landed(r)
          return r
        } catch (e) {
          undo()
          const conflict = (e as { conflict?: boolean }).conflict
          toast(
            conflict
              ? "It changed on another device meanwhile. Look again and retry."
              : describe(e),
            "error"
          )
          if (conflict) void collection.refresh(kind, id)
          return null
        }
      },
      async remove(kind, id) {
        const undo = collection.apply(kind, id, null)
        try {
          await client.delete(kind, id)
          return true
        } catch (e) {
          undo()
          toast(describe(e), "error")
          return false
        }
      },
    }),
    [client, collection, toast]
  )
}
