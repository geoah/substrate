/** The record segment of `/views/$id/$record` and `/apps/$id/$screen/$record`
 * is the record's FULL PATH, `<kind>/<id>`, carried as one percent-encoded
 * segment, because a trait view spans kinds and a bare id could not be
 * reopened after a reload. A bare id is accepted too when the view has one
 * kind to complete it from. The router encodes and decodes the segment; this
 * only builds and reads the decoded value, and says what the two moves that
 * leave a segment do to history. */

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { recordPath, splitRecordPath } from "@/lib/record-path"

export function recordSegment(record: SubstrateRecord): string {
  return recordPath(record.kind, record.id)
}

export interface SegmentTarget {
  /** The kind identity the segment names, or the fallback's. */
  kindIdentity?: string
  kind?: KindInfo
  id: string
}

/** A decoded segment read back: a full path splits into its kind and id; a
 * bare id takes `fallback`, the one kind the view reads. */
export function parseRecordSegment(
  segment: string,
  fallback: KindInfo | undefined,
  kinds: KindInfo[]
): SegmentTarget {
  const parts = splitRecordPath(segment)
  if (parts) {
    return {
      kindIdentity: parts.kind,
      kind: kindByIdentity(kinds, parts.kind),
      id: parts.id,
    }
  }
  return { kindIdentity: fallback?.identity, kind: fallback, id: segment }
}

/** The history state a row tap pushes the record segment with. The sheet's
 * entry sits on top of the screen's own, so popping it is the one move that
 * leaves history as it was; a segment reached any other way (a reload, a
 * shared link, a forward) has no such entry, and the screen is replaced in
 * place instead. A push without this stamp would make every close a third
 * entry and the next back a reopen. */
export const SHEET_STATE = { sheet: true } as const

export interface SheetState {
  sheet?: boolean
}

export type SheetClose = "back" | "replace"

/** What closing the sheet does to history, from the entry's own state. */
export function sheetClose(state: SheetState | undefined): SheetClose {
  return state?.sheet ? "back" : "replace"
}

export type BackMove = "close" | "back" | "launcher"

/** What the chrome's back arrow does: an open sheet closes first, a screen
 * with history behind it goes back one entry, and a screen opened cold has
 * nowhere to go but the launcher. */
export function backMove(sheetOpen: boolean, canGoBack: boolean): BackMove {
  if (sheetOpen) return "close"
  return canGoBack ? "back" : "launcher"
}
