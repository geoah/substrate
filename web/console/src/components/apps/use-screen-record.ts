/** What the route's record segment means to a screen, and how a row tap
 * moves. The segment SCOPES the view through `via` when the view declares
 * one, is the SUBJECT of a detail, and otherwise opens that record's sheet.
 * A tap pushes `opens` with the row when the view names one; else it opens
 * the sheet, through the route while the segment is free (so back closes it)
 * and locally once the segment is spent on a parent.
 *
 * The sheet's read is keyed on the KIND THE SEGMENT NAMES, never on the
 * registry having that kind: a trait view has no kind of its own, and a full
 * path says everything the request needs. A read that fails, or a segment
 * that names no kind, is `sheetError`, so the URL never claims a record the
 * screen silently ignores. */

import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  useCanGoBack,
  useNavigate,
  useRouter,
  useRouterState,
  type HistoryState,
} from "@tanstack/react-router"

import { splitKind } from "@/lib/api/http"
import { recordQueryOptions } from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { viaTarget } from "@/lib/apps/queries"
import {
  backMove,
  parseRecordSegment,
  recordSegment,
  SHEET_STATE,
  sheetClose,
} from "@/lib/apps/route"
import type { ViewContext, ViewSpec } from "@/lib/apps/spec"

declare module "@tanstack/react-router" {
  interface HistoryState {
    /** `SHEET_STATE`: this entry was pushed by a row tap on the screen
     * below it, so closing the sheet may pop it. */
    sheet?: boolean
  }
}

/** How a screen navigates to itself with a record segment: a tap pushes an
 * entry stamped `SHEET_STATE`; a tap while a sheet is already open swaps the
 * segment in place and keeps the entry's own stamp, so the close after it
 * still knows what sits below. */
export interface SheetNavigation {
  replace: boolean
  state: HistoryState | ((prev: HistoryState) => HistoryState)
}

const PUSH: SheetNavigation = { replace: false, state: SHEET_STATE }
const SWAP: SheetNavigation = { replace: true, state: (prev) => prev }

export type SegmentRole = "parent" | "subject" | "sheet"

export interface ScreenRecord {
  role?: SegmentRole
  /** The parent for the view's context, once loaded. */
  parent?: ViewContext["parent"]
  /** The segment names a parent that has not arrived yet. */
  parentPending: boolean
  parentError?: Error
  /** The record whose sheet is open, once read. */
  sheetRecord?: SubstrateRecord
  /** The segment asked for a record the sheet cannot show: the read failed
   * (gone, refused) or the segment names no kind to read it from. */
  sheetError?: Error
  /** A sheet is open or opening, through the route or locally. */
  sheetOpen: boolean
  openRecord: (record: SubstrateRecord) => void
  closeSheet: () => void
}

export function useScreenRecord({
  spec,
  kind,
  kinds,
  segment,
  toRecord,
  toView,
  clear,
}: {
  spec: ViewSpec | undefined
  kind: KindInfo | undefined
  kinds: KindInfo[]
  segment?: string
  /** Navigate to this screen with the record segment set. */
  toRecord: (segment: string, nav: SheetNavigation) => void
  /** Navigate to another view with the record segment set (`opens`). */
  toView: (view: string, segment: string) => void
  /** Navigate to this screen with no record segment. */
  clear: (nav: { replace: boolean }) => void
}): ScreenRecord {
  const router = useRouter()
  const entryState = useRouterState({ select: (s) => s.location.state.sheet })
  const role: SegmentRole | undefined =
    segment && spec
      ? spec.via
        ? "parent"
        : spec.layout === "detail"
          ? "subject"
          : "sheet"
      : undefined
  const fallback =
    role === "parent" && spec ? viaTarget(spec, kind, kinds) : kind
  const target = segment
    ? parseRecordSegment(segment, fallback, kinds)
    : undefined
  const identity = target?.kindIdentity ?? ""
  const { authority, pkg, name } = splitKind(identity)
  const record = useQuery({
    ...recordQueryOptions(authority, pkg, name, target?.id ?? ""),
    enabled: Boolean(identity && target?.id),
  })
  const [local, setLocal] = useState<SubstrateRecord | null>(null)

  const scoped = role === "parent" || role === "subject"
  const targetKind = target?.kind
  const parent = useMemo(
    () =>
      scoped && record.data && targetKind
        ? { record: record.data, kind: targetKind }
        : undefined,
    [scoped, record.data, targetKind]
  )
  const noKind =
    target && !identity
      ? new Error(
          `${segment} names no kind; a record here is addressed by its full \`<kind>/<id>\` path`
        )
      : target && scoped && !targetKind
        ? new Error(`no kind ${identity} for ${segment}`)
        : undefined

  return {
    role,
    parent,
    parentPending: scoped && !record.data && !record.error && !noKind,
    parentError: scoped ? (record.error ?? noKind) : undefined,
    sheetRecord: role === "sheet" ? record.data : (local ?? undefined),
    sheetError: role === "sheet" ? (record.error ?? noKind) : undefined,
    sheetOpen: role === "sheet" || local !== null,
    openRecord: (row) => {
      if (spec?.opens) toView(spec.opens, recordSegment(row))
      else if (!segment) toRecord(recordSegment(row), PUSH)
      else if (role === "sheet") toRecord(recordSegment(row), SWAP)
      else setLocal(row)
    },
    closeSheet: () => {
      if (local) {
        setLocal(null)
        return
      }
      if (role !== "sheet") return
      if (sheetClose({ sheet: entryState }) === "back") router.history.back()
      else clear({ replace: true })
    },
  }
}

/** The chrome's back arrow: an open sheet closes first; then the entry
 * behind this one when the router has one; else the launcher, which is where
 * a screen opened cold came from in spirit. */
export function useBack(
  screen: Pick<ScreenRecord, "sheetOpen" | "closeSheet">
): () => void {
  const router = useRouter()
  const navigate = useNavigate()
  const canGoBack = useCanGoBack()
  return () => {
    switch (backMove(screen.sheetOpen, canGoBack)) {
      case "close":
        screen.closeSheet()
        return
      case "back":
        router.history.back()
        return
      default:
        void navigate({ to: "/apps" })
    }
  }
}
