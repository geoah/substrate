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
 * screen silently ignores.
 *
 * A sheet opened locally keeps the row's IDENTITY and reads the record
 * through the same single-record query the route's sheet uses, never the
 * row itself: a transition moves the version, an agent edits the record
 * behind the screen, and a held copy would draw the old state and send the
 * old `ifVersion`. The selection belongs to the screen and parent it was
 * made on and clears with them. */

import { useMemo, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
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
import { actionApplies } from "@/lib/apps/actions"
import { viaTarget } from "@/lib/apps/queries"
import {
  backMove,
  parseRecordSegment,
  recordSegment,
  SHEET_STATE,
  sheetClose,
} from "@/lib/apps/route"
import type {
  ActionHost,
  ActionSpec,
  ViewContext,
  ViewSpec,
} from "@/lib/apps/spec"
import { recordPath } from "@/lib/record-path"

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

/** One record by kind identity and id: all a tap keeps of its row, and what
 * the sheet's read is keyed on. */
interface Selection {
  kindIdentity: string
  id: string
}

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
  /** The `<kind>/<id>` the open sheet is for: the route's segment, or the
   * local selection's path. */
  sheetSegment?: string
  /** A sheet is open or opening, through the route or locally. */
  sheetOpen: boolean
  openRecord: (record: SubstrateRecord) => void
  closeSheet: () => void
}

/** The single-record read for a selection; disabled while there is none. */
function useRecordRead(selection: Selection | undefined) {
  const { authority, pkg, name } = splitKind(selection?.kindIdentity ?? "")
  return useQuery({
    ...recordQueryOptions(authority, pkg, name, selection?.id ?? ""),
    enabled: Boolean(selection?.kindIdentity && selection?.id),
  })
}

export function useScreenRecord({
  spec,
  kind,
  kinds,
  segment,
  screenKey,
  toRecord,
  toView,
  clear,
}: {
  spec: ViewSpec | undefined
  kind: KindInfo | undefined
  kinds: KindInfo[]
  segment?: string
  /** What names the screen beyond its view: an app's id and screen name,
   * since two screens of one app may show one view under one segment. */
  screenKey?: string
  /** Navigate to this screen with the record segment set. */
  toRecord: (segment: string, nav: SheetNavigation) => void
  /** Navigate to another view with the record segment set (`opens`). */
  toView: (view: string, segment: string) => void
  /** Navigate to this screen with no record segment. */
  clear: (nav: { replace: boolean }) => void
}): ScreenRecord {
  const router = useRouter()
  const queryClient = useQueryClient()
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
  const named: Selection | undefined =
    target && identity ? { kindIdentity: identity, id: target.id } : undefined
  const scoped = role === "parent" || role === "subject"
  const parentRead = useRecordRead(scoped ? named : undefined)

  // A local selection is made on one screen under one parent; another
  // screen of the app or another parent is another page, and a sheet that
  // survived the move would show a record from the page before.
  const owner = `${screenKey ?? ""}\n${spec?.id ?? ""}\n${segment ?? ""}`
  const [local, setLocal] = useState<(Selection & { owner: string }) | null>(
    null
  )
  if (local && local.owner !== owner) setLocal(null)
  const selected = local && local.owner === owner ? local : undefined
  const sheetRead = useRecordRead(role === "sheet" ? named : selected)

  const targetKind = target?.kind
  const parent = useMemo(
    () =>
      scoped && parentRead.data && targetKind
        ? { record: parentRead.data, kind: targetKind }
        : undefined,
    [scoped, parentRead.data, targetKind]
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
    parentPending: scoped && !parentRead.data && !parentRead.error && !noKind,
    parentError: scoped ? (parentRead.error ?? noKind) : undefined,
    sheetRecord: sheetRead.data,
    sheetError:
      role === "sheet"
        ? (sheetRead.error ?? noKind)
        : selected
          ? (sheetRead.error ?? undefined)
          : undefined,
    sheetSegment:
      role === "sheet"
        ? segment
        : selected
          ? recordPath(selected.kindIdentity, selected.id)
          : undefined,
    sheetOpen: role === "sheet" || selected !== undefined,
    openRecord: (row) => {
      if (spec?.opens) {
        toView(spec.opens, recordSegment(row))
        return
      }
      // The row primes the read the sheet is keyed on, already stale, so the
      // sheet opens at once and the read replaces the row on mount; from
      // then on every write and every change reaches it through that key.
      const at = splitKind(row.kind)
      const key = recordQueryOptions(
        at.authority,
        at.pkg,
        at.name,
        row.id
      ).queryKey
      if (queryClient.getQueryData(key) === undefined) {
        queryClient.setQueryData(key, row, { updatedAt: 0 })
      }
      if (!segment) toRecord(recordSegment(row), PUSH)
      else if (role === "sheet") toRecord(recordSegment(row), SWAP)
      else setLocal({ owner, kindIdentity: row.kind, id: row.id })
    },
    closeSheet: () => {
      if (selected) {
        setLocal(null)
        return
      }
      if (role !== "sheet") return
      if (sheetClose({ sheet: entryState }) === "back") router.history.back()
      else clear({ replace: true })
    },
  }
}

export interface ChromeActions {
  /** The host the chrome's buttons run under: the screen's own, or on a
   * detail the subject's kind in place of the view's. */
  host: ActionHost
  /** The detail's subject, the record every chrome action there acts on;
   * absent on every other screen. */
  record?: SubstrateRecord
  primary?: ActionSpec
  header: ActionSpec[]
}

/** The primary and header actions a screen draws in its chrome. On a detail
 * the subject is the row those buttons act on: they are hosted under its
 * kind, offered only while it admits them (`actionApplies`, so a header
 * transition hides along an arm the machine lacks, as a row's would) and
 * handed the record, which is what lets a header transition, patch, delete
 * or open work on the record the page shows. Every other screen has no row
 * for its chrome, and its actions are offered as declared. */
export function chromeActions(
  host: ActionHost,
  screen: Pick<ScreenRecord, "role" | "parent">
): ChromeActions {
  const subject = screen.role === "subject" ? screen.parent : undefined
  const offered = (a: ActionSpec) =>
    !subject ||
    a.verb === "create" ||
    actionApplies(a, subject.record, subject.kind)
  return {
    host: subject ? { ...host, kind: subject.kind } : host,
    record: subject?.record,
    primary: host.spec.actions.find(
      (a) => a.placement === "primary" && offered(a)
    ),
    header: host.spec.actions.filter(
      (a) => a.placement === "header" && offered(a)
    ),
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
