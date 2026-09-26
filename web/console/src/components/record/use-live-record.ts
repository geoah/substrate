/** The record page's live half: it watches the change feed for this one
 * record, so an agent's edit or a sync's fill lands without a reload, and it
 * says which properties moved under the reader, briefly, so the sheet can
 * mark them. An edit made from this console is the reader's own and is not
 * marked. */

import { useEffect, useRef, useState } from "react"

import { wroteHere } from "@/components/property-sheet/use-record-patch"
import { useLiveInvalidation } from "@/hooks/use-live-invalidation"
import type { SubstrateRecord } from "@/lib/api/types"

/** How long a moved property stays marked. */
const MARK_MS = 2400

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)

/** The properties whose values differ between two reads of one record. */
export function movedProperties(
  before: SubstrateRecord,
  after: SubstrateRecord
): string[] {
  const names = new Set([
    ...Object.keys(before.properties),
    ...Object.keys(after.properties),
  ])
  return [...names]
    .filter((n) => !same(before.properties[n], after.properties[n]))
    .sort()
}

/** Watches `record` live; returns the properties that just moved under the
 * reader. */
export function useLiveRecord(record: SubstrateRecord): ReadonlySet<string> {
  useLiveInvalidation({ kinds: [record.kind], recordIds: [record.id] })
  const [moved, setMoved] = useState<ReadonlySet<string>>(() => new Set())
  const seen = useRef(record)
  useEffect(() => {
    const before = seen.current
    seen.current = record
    if (
      before.id !== record.id ||
      before.kind !== record.kind ||
      record.version <= before.version ||
      wroteHere(record.kind, record.id, record.version)
    ) {
      return undefined
    }
    const names = movedProperties(before, record)
    if (!names.length) return undefined
    setMoved(new Set(names))
    const timer = setTimeout(() => setMoved(new Set()), MARK_MS)
    return () => clearTimeout(timer)
  }, [record])
  return moved
}
