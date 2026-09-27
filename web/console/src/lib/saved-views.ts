/** Saved views of a collection: a named filter, sort, column set, nesting and
 * grouping, kept on the console preference record (`views`) so they follow
 * the person to every browser signed in to the repository.
 *
 * A view is a SHAPE, not a place: it never carries a search (the question of
 * the moment) or a page. Which view is chosen is not stored anywhere either:
 * a view is chosen when what the page shows is what it names, so a shared
 * address that happens to match a view lights its tab, and a change to a
 * filter after choosing one lets go of it. */

/** One saved view, as the record stores it. Every optional field is absent
 * when the view leaves that dimension at the collection's own default, so a
 * field added later reads as "the default" on an older view. */
export interface SavedView {
  id: string
  /** The collection's kind reference. */
  collection: string
  name: string
  /** Filter tokens (`lib/filters.ts` `encodeFilter`). */
  filter?: string[]
  /** `property:dir`; absent is the collection's default order. */
  sort?: string
  /** Column ids in display order; absent leaves the columns as they are. */
  columns?: string[]
  /** Column ids the view hides; absent leaves the columns as they are. */
  hidden?: string[]
  /** `false` turns a nesting collection's tree off; absent is on. */
  nest?: boolean
  /** The property the rows are grouped by; absent is no grouping. */
  group?: string
}

/** What the page shows now, in the terms a view compares. */
export interface ViewState {
  filter: string[]
  sort: string
  nest: boolean
  group?: string
  /** The effective column order. */
  columns: string[]
  /** The columns the reader hides (not the ones hidden for being empty). */
  hidden: string[]
}

const stringList = (value: unknown): string[] | undefined =>
  Array.isArray(value) && value.every((v) => typeof v === "string")
    ? (value as string[])
    : undefined

const text = (value: unknown): string | undefined =>
  typeof value === "string" && value.trim() ? value : undefined

/** The stored `views` value, read defensively: an entry missing its id,
 * collection or name is dropped, and a repeated id keeps its first entry. */
export function viewsFrom(value: unknown): SavedView[] {
  if (!Array.isArray(value)) return []
  const out: SavedView[] = []
  const seen = new Set<string>()
  for (const entry of value) {
    if (typeof entry !== "object" || entry === null) continue
    const e = entry as Record<string, unknown>
    const id = text(e.id)
    const collection = text(e.collection)
    const name = text(e.name)
    if (!id || !collection || !name || seen.has(id)) continue
    seen.add(id)
    const view: SavedView = { id, collection, name }
    const filter = stringList(e.filter)
    if (filter?.length) view.filter = filter
    const sort = text(e.sort)
    if (sort) view.sort = sort
    const columns = stringList(e.columns)
    if (columns?.length) view.columns = columns
    const hidden = stringList(e.hidden)
    if (hidden) view.hidden = hidden
    if (e.nest === false) view.nest = false
    const group = text(e.group)
    if (group) view.group = group
    out.push(view)
  }
  return out
}

/** One collection's views, in the order they were saved. */
export function viewsOf(
  views: readonly SavedView[],
  collection: string
): SavedView[] {
  return views.filter((v) => v.collection === collection)
}

/** A fresh id no view already has. */
export function newViewId(existing: readonly SavedView[]): string {
  const taken = new Set(existing.map((v) => v.id))
  for (;;) {
    const id = Math.random().toString(36).slice(2, 10)
    if (id.length === 8 && !taken.has(id)) return id
  }
}

/** The view the page shows now, under a name. The default sort and an open
 * tree are left out, so the view keeps meaning "the default" if the default
 * ever moves. */
export function viewFromState(
  base: { id: string; collection: string; name: string },
  state: ViewState,
  defaultSort: string
): SavedView {
  const view: SavedView = { ...base, name: base.name.trim() }
  if (state.filter.length) view.filter = [...state.filter]
  if (state.sort !== defaultSort) view.sort = state.sort
  view.columns = [...state.columns]
  view.hidden = [...state.hidden]
  if (!state.nest) view.nest = false
  if (state.group) view.group = state.group
  return view
}

const sameSet = (a: readonly string[], b: readonly string[]) => {
  if (a.length !== b.length) return false
  const set = new Set(a)
  return b.every((x) => set.has(x))
}

/** The page shows what the view names. Columns count only where the view
 * stored them. */
export function viewMatches(
  view: SavedView,
  state: ViewState,
  defaultSort: string
): boolean {
  if (!sameSet(view.filter ?? [], state.filter)) return false
  if ((view.sort ?? defaultSort) !== state.sort) return false
  if ((view.nest ?? true) !== state.nest) return false
  if ((view.group ?? undefined) !== (state.group || undefined)) return false
  if (view.columns) {
    // Only the columns both know are compared: one the collection no longer
    // has, or one it gained since, does not count against the view.
    const shared = new Set(
      view.columns.filter((id) => state.columns.includes(id))
    )
    const theirs = view.columns.filter((id) => shared.has(id))
    const ours = state.columns.filter((id) => shared.has(id))
    if (theirs.some((id, i) => id !== ours[i])) return false
  }
  if (view.hidden) {
    const live = view.hidden.filter((id) => state.columns.includes(id))
    if (!sameSet(live, state.hidden)) return false
  }
  return true
}

/** The collection as it opens: no filter, the default order, the tree on, no
 * grouping. Columns are the reader's own and do not count. */
export function isAllView(state: ViewState, defaultSort: string): boolean {
  return (
    state.filter.length === 0 &&
    state.sort === defaultSort &&
    state.nest &&
    !state.group
  )
}

/** Why a name cannot be used, or undefined when it can. */
export function viewNameProblem(
  name: string,
  views: readonly SavedView[],
  except?: string
): string | undefined {
  const trimmed = name.trim()
  if (!trimmed) return "Give the view a name."
  if (trimmed.toLowerCase() === "all") return "“All” is the collection itself."
  const clash = views.find(
    (v) => v.id !== except && v.name.toLowerCase() === trimmed.toLowerCase()
  )
  return clash ? `There is already a view called “${clash.name}”.` : undefined
}
