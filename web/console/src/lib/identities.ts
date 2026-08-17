/** What a POINTER points at, as records to offer.
 *
 * A `reference` property pins the kind its value names, and a `KindInfo` is an
 * authority and a collection, so the registry the editor already holds says which
 * collection to read. That is the whole of `collectionFor`, and it is the only
 * place that decides.
 *
 * The four host functions need no special case: they are ordinary `function`
 * records, so they list beside a bundle's with their own cards. */

import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import { recordsQueryOptions } from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { recordTitle } from "@/lib/format"
import { TO_ANY } from "@/lib/record-schema"

/** How many records a picker offers before it says so. The collections a
 * declaration usually pins to are registry-shaped (functions, agents,
 * authorities, providers, kinds), so one page is the whole set in every real
 * repository; a pointer at an ordinary data collection is why the cap is said
 * out loud rather than pretended away. */
export const PICKER_PAGE = 200

/** Where a collection lives: the two segments its path is built from. */
export interface Collection {
  authority: string
  collection: string
}

/** Which collection holds the records a pointer may name. A pointer at no
 * declared kind offers nothing, because there is no collection to offer and a
 * whole path is the only thing that names a record. */
export function collectionFor(
  pin: string | undefined,
  kinds: KindInfo[]
): Collection | undefined {
  if (!pin || pin === TO_ANY) return undefined
  const declared = kindByIdentity(kinds, pin)
  if (!declared?.name) return undefined
  return { authority: declared.authority, collection: declared.name }
}

/** One offered record: the id a selection inserts (the pin supplies the kind
 * the write joins onto it), and what a reader needs to recognise it. A
 * function's card is its description, which is exactly what somebody choosing
 * a tool wants to read. */
export interface RecordOption {
  /** The record's own id, which is what the control holds and shows. */
  value: string
  title: string
  description: string
}

/** What the picker is offering, and how that read is going. */
export interface RecordOptions {
  options: RecordOption[]
  loading: boolean
  error?: string
  /** The collection outran the page: what is offered is not all there is, and
   * the control has to say so rather than imply the list is the limit. */
  capped: boolean
}

function stringProp(
  properties: Record<string, unknown>,
  key: string
): string | undefined {
  const value = properties[key]
  return typeof value === "string" && value.trim() ? value.trim() : undefined
}

/** A record as one offered row: its title (the `title` column, else a declared
 * `name`), and its one-liner where it declares one. */
function optionOf(record: SubstrateRecord): RecordOption {
  const props = record.properties ?? {}
  return {
    value: record.id,
    title: recordTitle(props) || stringProp(props, "name") || "",
    description: stringProp(props, "description") ?? "",
  }
}

/** The records a pointer may name, as picker rows. `self` drops one id: a
 * declaration that names itself as its own sub-agent is a loop nobody should
 * be able to spell by accident. */
export function useRecordOptions(
  pin: string | undefined,
  kinds: KindInfo[],
  self?: string
): RecordOptions {
  const collection = collectionFor(pin, kinds)
  const records = useQuery({
    ...recordsQueryOptions({
      authority: collection?.authority ?? "",
      collection: collection?.collection ?? "",
      first: PICKER_PAGE,
    }),
    enabled: Boolean(collection),
  })
  const page = records.data

  return useMemo(() => {
    if (!collection) return { options: [], loading: false, capped: false }
    return {
      options: (page?.records ?? [])
        .filter((r) => !(self && r.id === self))
        .map(optionOf),
      loading: records.isPending,
      error: records.error?.message,
      capped: Boolean(page?.cursor),
    }
  }, [collection, page, self, records.isPending, records.error])
}
