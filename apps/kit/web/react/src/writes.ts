import {
  describe,
  uuidv7,
  type Kind,
  type SubstrateRecord,
} from "@substrate/client"
import { RecordWrites, type LocalWrite } from "@substrate/local"
import { useMemo } from "react"
import { useSubstrate } from "./context"
import { readThrough } from "./hooks"
import { useToast } from "./toast"

export interface WriteOptions {
  /** Takes the words when the browser could not keep the write, in place
   * of the toast: a form says them beside its fields and keeps its draft. */
  onNotKept?: (words: string) => void
}

/** What a write answers once the browser has it, or could not keep it.
 * `kept` means the outbox holds it, to be sent when the server can be
 * reached; it never means the server took it. A write the server then
 * refuses waits for the owner in Settings (`useParkedWrites`). */
export type WriteResult<R> =
  | {
      kept: true
      /** The record's id: for a create, the one the device minted. */
      id: string
      /** The record as the owner now sees it, the write laid over this
       * browser's copy. Undefined after a delete, for a record of a kind
       * the app does not read, and for a change to a record not read
       * here: the write is kept all the same. */
      shown: R | undefined
    }
  | { kept: false; words: string }

/** The app's writes by kind reference and properties, each kept in the
 * outbox before it resolves and shown at once. `useKindWrites` is the same
 * writes typed by a kind's declaration, which is what an app uses. */
export interface Writes {
  /** A create under an id the device chose: a PUT with `ifVersion: 0`, so
   * every retry lands on the same row. A property given null holds
   * nothing on a record not made yet, and is left out. */
  create: (
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    opts?: WriteOptions
  ) => Promise<WriteResult<SubstrateRecord>>
  /** A change made in passing (a toggle, a move): the properties merged in
   * (null clears one), sent on the version this browser last saw and sent
   * once more on top if somebody wrote the record meanwhile. A state moves
   * only here or in an edit. */
  change: (
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    opts?: WriteOptions
  ) => Promise<WriteResult<SubstrateRecord>>
  /** An edit the owner made on what they saw at `seenVersion`: never merged
   * over, so a change made elsewhere meanwhile parks it as a conflict for
   * the owner to resolve. */
  edit: (
    kind: string,
    id: string,
    properties: Record<string, unknown>,
    seenVersion: number,
    opts?: WriteOptions
  ) => Promise<WriteResult<SubstrateRecord>>
  /** A plain DELETE, no purge, which replaces the record's waiting
   * writes. */
  remove: (
    kind: string,
    id: string,
    opts?: WriteOptions
  ) => Promise<WriteResult<SubstrateRecord>>
}

export function useWrites(): Writes {
  const { outbox, linked } = useSubstrate()
  const toast = useToast()
  return useMemo<Writes>(() => {
    const notKept = (
      e: unknown,
      opts: WriteOptions | undefined
    ): WriteResult<never> => {
      const words = describe(e)
      if (opts?.onNotKept) opts.onNotKept(words)
      else toast(words, "error")
      return { kept: false, words }
    }
    const keep = async (
      kind: string,
      id: string,
      make: () => LocalWrite,
      opts: WriteOptions | undefined
    ): Promise<WriteResult<SubstrateRecord>> => {
      let write: LocalWrite
      try {
        write = make()
        await outbox.write(write)
      } catch (e) {
        return notKept(e, opts)
      }
      // The linked records follow the outbox, so the write is laid over
      // their copy already.
      const shown = linked.kinds.includes(kind)
        ? linked.recordSnapshot(kind, id)
        : undefined
      return { kept: true, id, shown }
    }
    return {
      create: (kind, id, properties, opts) =>
        keep(
          kind,
          id,
          () =>
            RecordWrites.create(
              kind,
              id,
              Object.fromEntries(
                Object.entries(properties).filter(([, v]) => v !== null)
              )
            ),
          opts
        ),
      change: (kind, id, properties, opts) =>
        keep(kind, id, () => RecordWrites.change(kind, id, properties), opts),
      edit: (kind, id, properties, seenVersion, opts) =>
        keep(
          kind,
          id,
          () => RecordWrites.edit(kind, id, properties, seenVersion),
          opts
        ),
      remove: (kind, id, opts) =>
        keep(kind, id, () => RecordWrites.delete(kind, id), opts),
    }
  }, [outbox, linked, toast])
}

/** One kind's writes, typed by its declaration: each draft written through
 * the kind's codec, and the record answered read through it. A codec that
 * throws is a write not kept, said as any other. */
export interface KindWrites<T, D> {
  /** A create of the whole draft under a new id. */
  create: (draft: D, opts?: WriteOptions) => Promise<WriteResult<T>>
  /** A change made in passing: the fields the draft names. */
  change: (
    id: string,
    draft: Partial<D>,
    opts?: WriteOptions
  ) => Promise<WriteResult<T>>
  /** An edit of the fields the draft names, on the version the owner saw. */
  edit: (
    id: string,
    draft: Partial<D>,
    seenVersion: number,
    opts?: WriteOptions
  ) => Promise<WriteResult<T>>
  remove: (id: string, opts?: WriteOptions) => Promise<WriteResult<T>>
}

export function useKindWrites<T, D>(kind: Kind<T, D>): KindWrites<T, D> {
  const writes = useWrites()
  const toast = useToast()
  return useMemo<KindWrites<T, D>>(() => {
    const typed = (r: WriteResult<SubstrateRecord>): WriteResult<T> =>
      r.kept
        ? {
            kept: true,
            id: r.id,
            shown: r.shown && readThrough(kind, r.shown),
          }
        : r
    // The codec runs before the write is made, so its error reaches the
    // owner the way the outbox's does.
    const encoded = async (
      draft: Partial<D>,
      opts: WriteOptions | undefined,
      send: (
        props: Record<string, unknown>
      ) => Promise<WriteResult<SubstrateRecord>>
    ): Promise<WriteResult<T>> => {
      let props: Record<string, unknown>
      try {
        props = kind.write(draft)
      } catch (e) {
        const words = describe(e)
        if (opts?.onNotKept) opts.onNotKept(words)
        else toast(words, "error")
        return { kept: false, words }
      }
      return typed(await send(props))
    }
    return {
      create: (draft, opts) =>
        encoded(draft, opts, (props) =>
          writes.create(kind.kind, uuidv7(), props, opts)
        ),
      change: (id, draft, opts) =>
        encoded(draft, opts, (props) =>
          writes.change(kind.kind, id, props, opts)
        ),
      edit: (id, draft, seenVersion, opts) =>
        encoded(draft, opts, (props) =>
          writes.edit(kind.kind, id, props, seenVersion, opts)
        ),
      remove: async (id, opts) =>
        typed(await writes.remove(kind.kind, id, opts)),
    }
  }, [writes, kind, toast])
}
