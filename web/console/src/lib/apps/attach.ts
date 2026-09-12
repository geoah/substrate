/** Where a view attaches to the console, read off the views collection: a
 * `browse` view is a tab on its kind's page, a `record` view a card on a
 * record page, a `home` view a card on the overview, a `launcher` view (or
 * an app) an entry in the phone's bottom bar. Pure functions over the
 * records, so a page decodes once with `useMemo` and a test needs no DOM.
 *
 * A `record` view reaches a record two ways: its `kind` IS the record's, or
 * its `via` is a reference property pinned at the record's kind, which is how
 * "Open tasks" (kind task, via project) lands on every project. */

import { splitKind } from "@/lib/api/http"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { kindByIdentity } from "@/lib/definition"
import { appSpec } from "./app-spec"
import { viaTarget } from "./queries"
import type { Attach, Layout, ViewSpec } from "./spec"
import { viewSpec } from "./view-spec"

export interface AttachedView {
  record: SubstrateRecord
  spec: ViewSpec
}

/** Every view decoded, in id order so two surfaces list them the same way. */
export function decodeViews(
  views: SubstrateRecord[],
  kinds: KindInfo[]
): AttachedView[] {
  return [...views]
    .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
    .map((record) => ({ record, spec: viewSpec(record, kinds) }))
}

function attachedAt(view: AttachedView, at: Attach): boolean {
  return view.spec.attach.includes(at)
}

/** The tabs a kind's page offers beside Records and Definition. */
export function viewsAttachedTo(
  views: SubstrateRecord[],
  kinds: KindInfo[],
  where: { browse: string }
): AttachedView[] {
  return decodeViews(views, kinds).filter(
    (v) => attachedAt(v, "browse") && v.spec.kind === where.browse
  )
}

/** Whether the view's `via` reference is pinned at this kind. */
export function viaPointsAt(
  spec: ViewSpec,
  kinds: KindInfo[],
  kindIdentity: string
): boolean {
  if (!spec.via || !spec.kind) return false
  const own = kindByIdentity(kinds, spec.kind)
  return viaTarget(spec, own, kinds)?.identity === kindIdentity
}

/** The cards a record's page mounts. A view with `via` belongs to the kind
 * its via points at and to no other: on a record of its own kind the scope
 * would be `<via> eq <that record>`, a filter nothing satisfies. A view
 * without `via` belongs to its own kind. */
export function viewsForRecord(
  views: SubstrateRecord[],
  kinds: KindInfo[],
  record: SubstrateRecord
): AttachedView[] {
  return decodeViews(views, kinds).filter(
    (v) =>
      attachedAt(v, "record") &&
      (v.spec.via
        ? viaPointsAt(v.spec, kinds, record.kind)
        : v.spec.kind === record.kind)
  )
}

/** The cards the overview mounts. */
export function homeViews(
  views: SubstrateRecord[],
  kinds: KindInfo[]
): AttachedView[] {
  return decodeViews(views, kinds).filter((v) => attachedAt(v, "home"))
}

/** The view that stands in for a kind's default table: the lowest id among
 * those that say `replaces`, so two cannot both win. */
export function replacingView(
  views: SubstrateRecord[],
  kinds: KindInfo[],
  kindIdentity: string
): AttachedView | undefined {
  return decodeViews(views, kinds).find(
    (v) => v.spec.replaces && v.spec.kind === kindIdentity
  )
}

/** One entry in the phone's bottom bar. */
export interface LauncherEntry {
  /** `view:<id>` or `app:<id>`, unique across both collections. */
  key: string
  label: string
  /** A lucide icon name, kebab-case. */
  icon: string
  /** The console route the entry opens. */
  to: string
}

/** A view declares no icon, so its layout lends one: the kebab names of
 * `components/apps/icon.tsx`'s `LAYOUT_ICONS`, for `AppIcon`. */
export const LAYOUT_ICON_NAMES: Record<Layout, string> = {
  list: "list",
  board: "kanban",
  timeline: "calendar-range",
  contacts: "contact",
  detail: "file-text",
  form: "clipboard-list",
  custom: "code",
}

export const APP_ICON = "layout-grid"

/** The launcher views, then the apps, each in id order. */
export function launcherEntries(
  views: SubstrateRecord[],
  apps: SubstrateRecord[],
  kinds: KindInfo[] = []
): LauncherEntry[] {
  const viewEntries = decodeViews(views, kinds)
    .filter((v) => attachedAt(v, "launcher"))
    .map((v): LauncherEntry => ({
      key: `view:${v.spec.id}`,
      label: v.spec.name,
      icon: LAYOUT_ICON_NAMES[v.spec.layout],
      to: `/views/${encodeURIComponent(v.spec.id)}`,
    }))
  const appEntries = [...apps]
    .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
    .map((record) => appSpec(record, views, kinds, apps))
    .map((app): LauncherEntry => ({
      key: `app:${app.id}`,
      label: app.name,
      icon: app.icon ?? APP_ICON,
      to: `/apps/${encodeURIComponent(app.id)}`,
    }))
  return [...viewEntries, ...appEntries]
}

/** Under the console's own chrome a row tap opens the generic record page:
 * the params of `/data/$authority/$pkg/$name/$id`. */
export function recordPageParams(record: SubstrateRecord) {
  const { authority, pkg, name } = splitKind(record.kind)
  return { authority, pkg, name, id: record.id }
}
