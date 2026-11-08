import {
  describe,
  uuidv7,
  type Kind,
  type SubstrateRecord,
} from "@substrate/client"
import { RecordWrites, applyEntry, type LocalWrite } from "@substrate/local"
import { useMemo } from "react"
import { useSubstrate } from "./context"
import { useToast } from "./toast"

export interface WriteOptions {
  /** Takes the words when the browser could not keep the write, in place
   * of the toast: a form says them beside its fields and keeps its draft. */
  onNotKept?: (words: string) => void
}

export interface Writes {
  /** Creates a record under an id the device chose: a PUT with
   * `ifVersion: 0` every retry lands on the same row. */
  create: (
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    opts?: WriteOptions
  ) => Promise<SubstrateRecord | null>
  /** Merges properties into a record (null clears one); a state moves only
   * here. With `ifVersion`, an edit on the version the owner saw: a change
   * made elsewhere meanwhile parks it as a conflict. Without, a change
   * made in passing, sent on top of what the device last saw. */
  patch: (
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    ifVersion?: number,
    opts?: WriteOptions
  ) => Promise<SubstrateRecord | null>
  /** A plain DELETE, no purge, which replaces the record's waiting writes. */
  remove: (kind: string, id: string, opts?: WriteOptions) => Promise<boolean>
}

/** The app's writes, each kept in the outbox before it resolves and shown
 * at once: the record as the owner now sees it, or null (false for a
 * remove) when the browser could not keep it. The server's answer comes
 * later: a write it refuses is parked for the owner (`useParkedWrites`),
 * and one the network holds back is sent when it can be. */
export function useWrites(): Writes {
  const { outbox, linked } = useSubstrate()
  const toast = useToast()
  return useMemo<Writes>(() => {
    const keep = async (
      write: LocalWrite,
      opts: WriteOptions | undefined
    ): Promise<boolean> => {
      try {
        await outbox.write(write)
        return true
      } catch (e) {
        const words = describe(e)
        if (opts?.onNotKept) opts.onNotKept(words)
        else toast(words, "error")
        return false
      }
    }
    const shown = (write: LocalWrite, before: SubstrateRecord | undefined) =>
      write.entries.reduce<SubstrateRecord | undefined>(
        (r, e) => applyEntry(e, r),
        before
      ) ?? null
    return {
      async create(kind, id, properties, opts) {
        const write = RecordWrites.create(kind, id, properties)
        return (await keep(write, opts)) ? shown(write, undefined) : null
      },
      async patch(kind, id, properties, ifVersion, opts) {
        const before = linked.get(kind, id)
        const write =
          ifVersion === undefined
            ? RecordWrites.change(kind, id, properties)
            : RecordWrites.edit(kind, id, properties, ifVersion)
        if (!write.entries.length) return before ?? null
        if (!(await keep(write, opts))) return null
        // A record outside the app's kinds is not shown: answer what was
        // written, so a kept write never reads as one refused.
        const now = new Date().toISOString()
        return (
          shown(write, before) ?? {
            id,
            kind,
            properties: { ...properties },
            version: ifVersion ?? 0,
            createdAt: now,
            updatedAt: now,
          }
        )
      },
      async remove(kind, id, opts) {
        return keep(RecordWrites.delete(kind, id), opts)
      },
    }
  }, [outbox, linked, toast])
}

/** One kind's writes, typed by its declaration: a create from the app's
 * draft under a new id, a change, an edit and a delete. */
export interface KindWrites<D> {
  /** Answers the new record's id, or null when it was not kept. */
  create: (draft: D, opts?: WriteOptions) => Promise<string | null>
  change: (
    id: string,
    properties: Record<string, unknown>,
    opts?: WriteOptions
  ) => Promise<boolean>
  edit: (
    id: string,
    properties: Record<string, unknown>,
    seenVersion: number,
    opts?: WriteOptions
  ) => Promise<boolean>
  remove: (id: string, opts?: WriteOptions) => Promise<boolean>
}

export function useKindWrites<T, D>(kind: Kind<T, D>): KindWrites<D> {
  const writes = useWrites()
  return useMemo<KindWrites<D>>(
    () => ({
      async create(draft, opts) {
        const id = uuidv7()
        const r = await writes.create(kind.kind, id, kind.write(draft), opts)
        return r ? id : null
      },
      async change(id, properties, opts) {
        return !!(await writes.patch(
          kind.kind,
          id,
          properties,
          undefined,
          opts
        ))
      },
      async edit(id, properties, seenVersion, opts) {
        return !!(await writes.patch(
          kind.kind,
          id,
          properties,
          seenVersion,
          opts
        ))
      },
      remove: (id, opts) => writes.remove(kind.kind, id, opts),
    }),
    [writes, kind]
  )
}
