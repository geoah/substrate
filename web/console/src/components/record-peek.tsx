/** A record cell's reference with its hover card. Kept as the name existing
 * surfaces import; the mark itself is `RecordRef`, whose hover card reads the
 * record's facts on hover, never before. */

import { RecordRef } from "@/components/identity/record-ref"
import type { KindInfo } from "@/lib/api/types"

/** What a peek needs to name a record: its kind reference and its id, plus
 * whatever title the caller already had. */
export interface PeekTarget {
  id: string
  /** The record's kind REFERENCE. */
  kind: string
  title?: string
}

export function RecordPeek({
  target,
}: {
  target: PeekTarget
  /** The registry. The mark reads it from the query cache itself now. */
  types?: KindInfo[]
}) {
  return <RecordRef kind={target.kind} id={target.id} title={target.title} />
}
