import { kindStatus } from "@substrate/client"
import type { Pending, Reschedule } from "./actions"
import {
  bandTitle,
  bands,
  civilDay,
  dayTitle,
  dayWords,
  dueWords,
  inSelection,
  isOpen,
  lateWords,
  listTitle,
  notesPreview,
  priorityWords,
  sortTasks,
  statusWords,
  taskTitle,
  type Band,
  type Priority,
  type Selection,
  type TaskItem,
  type TaskList,
  type Tone,
} from "./model"
import {
  groupOrder,
  kindLabel,
  nameWords,
  ownTaskKind,
  propertyLabel,
  type Collection,
  type GroupSection,
} from "./sources"
import type { Settings, TasksModel } from "./state"
import { words } from "./words"

/** The words TasksPresenter writes for each screen, from the same state:
 * the screens draw these and decide nothing. */

export interface RowView {
  task: TaskItem
  title: string
  pending: boolean
  /** The decision in its window, where one is. */
  decision?: Pending
  /** What the round mark does: "Mark as Done", "Reopen", "Undo". */
  toggle: string
  /** Whether the mark does anything: a view-only collection's does not. */
  toggles: boolean
  due?: { text: string; tone: Tone }
  priority?: Priority
  /** Another collection's priority, in words, and whether it presses. */
  priorityLabel?: { text: string; pressing: boolean }
  /** The group it is in, where the column mixes groups. */
  listName?: string
  /** Its collection, where more than one is on. */
  collection?: string
  band?: Band
  /** Where it can be moved to from its row, where it waits on a day. */
  reschedule?: { title: string; options: { label: string; how: Reschedule }[] }
  /** Won't Do, where it waits on a decision. */
  decides: boolean
  /** A suggestion's row: when it was written, and its Dismiss and Accept
   * where its collection is writable. */
  suggestion?: { written: string; dismiss: boolean; accept: boolean }
  notes?: string
  /** The row read aloud: "Buy milk, due Today, priority High". */
  spoken: string
}

/** A group's name for a row, a heading or a menu: the record's title, "No
 * list" for none, "Missing" for one gone. */
export function groupName(model: TasksModel, t: TaskItem): string | undefined {
  const g = t.item.group
  const c = t.item.collection
  if (g === null || !c.group) return undefined
  if (g === "none")
    return words.lists.none(
      propertyLabel(c.declaration.definition, c.group.property)
    )
  if (g === "missing") return words.lists.missing
  const l = model.list(g)
  return l ? listTitle(l) : words.lists.missing
}

/** When a record was written, as a row says it ("2h ago", "yesterday",
 * "Fri", "3 Nov") and as a page says it ("2 hours ago"). */
export function writtenWords(at: string, now: Date, long = false): string {
  const t = new Date(at)
  const minutes = Math.round((now.getTime() - t.getTime()) / 60_000)
  if (minutes < 1) return "just now"
  if (minutes < 60)
    return long
      ? minutes === 1
        ? "1 minute ago"
        : `${minutes} minutes ago`
      : `${minutes}m ago`
  const days = Math.round(
    (new Date(civilDay(now)).getTime() - new Date(civilDay(t)).getTime()) /
      86_400_000
  )
  if (days < 1) {
    const hours = Math.floor(minutes / 60)
    return long
      ? hours === 1
        ? "1 hour ago"
        : `${hours} hours ago`
      : `${hours}h ago`
  }
  if (days === 1) return "yesterday"
  if (days < 7)
    return new Intl.DateTimeFormat(undefined, {
      weekday: long ? "long" : "short",
    }).format(t)
  return new Intl.DateTimeFormat(undefined, {
    day: "numeric",
    month: "short",
    year: t.getFullYear() === now.getFullYear() ? undefined : "numeric",
  }).format(t)
}

export function row(
  task: TaskItem,
  opts: {
    now: Date
    pending?: Pending
    model?: TasksModel
    /** The column mixes groups: each row names its own. */
    mixed?: boolean
    band?: Band
  }
): RowView {
  const { now, band, model } = opts
  const decision = opts.pending
  const pending = decision !== undefined
  const today = civilDay(now)
  let due = task.due ? dueWords(task.due, now) : undefined
  // On Needs you a late task says how late, not when it was due: the day
  // is in the decision its row offers.
  if (band === "lateOrUrgent" && task.due && due?.tone === "overdue")
    due = { text: lateWords(task.due, now), tone: "overdue" }
  const priority =
    task.own && task.priority !== "none" ? task.priority : undefined
  const priorityLabel =
    !task.own && task.priorityLabel
      ? { text: task.priorityLabel, pressing: task.pressing }
      : undefined
  const listName = opts.mixed && model ? groupName(model, task) : undefined
  const collection =
    model && model.sources.enabled.length > 1
      ? task.item.collection.label.plural
      : undefined
  const title = taskTitle(task)
  const spoken = [title]
  const writable = !task.item.collection.viewOnly
  const proposed = task.status === "proposed"
  if (decision) spoken.push(`${words.undo[decision.action]}, tap to undo`)
  else if (task.status !== "open") spoken.push(statusWords[task.status])
  if (due)
    spoken.push(
      due.tone === "overdue" && band === "lateOrUrgent"
        ? due.text
        : `due ${due.text}`
    )
  if (priority) spoken.push(`priority ${priorityWords[priority]}`)
  if (priorityLabel) spoken.push(`priority ${priorityLabel.text}`)
  if (listName) spoken.push(`in ${listName}`)
  if (collection) spoken.push(collection)
  if (task.description) spoken.push("has notes")
  const deciding = band === "lateOrUrgent" && !pending && writable
  const done = decision?.action === "complete"
  return {
    task,
    title,
    pending,
    decision,
    toggle: done ? "Undo" : isOpen(task) ? "Mark as Done" : "Reopen",
    toggles:
      done ||
      (isOpen(task)
        ? task.item.actions.complete.length > 0
        : task.item.actions.reopen.length > 0),
    due,
    priority,
    priorityLabel,
    listName,
    collection,
    band,
    reschedule:
      deciding &&
      (task.own || task.item.collection.due || task.item.collection.dueDay)
        ? reschedule(task, today)
        : undefined,
    decides: deciding && task.item.actions.wontDo.length > 0,
    suggestion: proposed
      ? {
          written: writtenWords(task.item.createdAt, now),
          dismiss: task.item.actions.dismiss.length > 0,
          accept: task.item.actions.accept.length > 0,
        }
      : undefined,
    notes: task.description ? notesPreview(task.description) : undefined,
    spoken: spoken.join(", "),
  }
}

/** The days to move a task that waits on one to: today where it is late,
 * tomorrow, a week on, or a day picked. */
function reschedule(task: TaskItem, today: string): RowView["reschedule"] {
  const options: { label: string; how: Reschedule }[] = []
  if (!task.due || task.due.day !== today)
    options.push({ label: "Today", how: "today" })
  options.push({ label: "Tomorrow", how: "tomorrow" })
  const week = new Date()
  week.setDate(week.getDate() + 7)
  options.push({ label: dayWords(civilDay(week), today), how: "nextWeek" })
  options.push({ label: "Pick a Day…", how: "pickDay" })
  return { title: task.due ? "Move" : "Pick a Day", options }
}

/** The queue row on Needs you and a group: how many suggestions wait. */
export interface QueueRow {
  title: string
  detail: string
  count: number
}

export interface ListView {
  selection: Selection
  title: string
  summary: { headline: string; detail?: string; overdue?: string }
  /** Needs you's chips, All first, then the pins; empty elsewhere. */
  chips: {
    id: string
    title: string
    count?: number
    selection?: Selection
    selected: boolean
  }[]
  /** The bands Needs you's rows fall in, those with rows alone. */
  bands: { band: Band; title: string; count: number }[]
  clear?: string
  /** The suggestions waiting, on Needs you and a group, where any do. */
  queue?: QueueRow
  /** Needs you's one counted row of the open tasks with no date. */
  noDate?: QueueRow
  open: RowView[]
  openTitle: string
  closed: RowView[]
  closedTitle: string
  showsClosed: boolean
  toggleClosed: string
  quickAdd?: string
  newTask: string
  reorders: boolean
  orderNote?: string
  empty?: { title: string; message: string }
  /** The list's own: Rename List…, Delete List…. */
  list?: TaskList
  /** A group's kind and status, and whether it is pinned. */
  group?: { kind: string; reference: string; pinned: boolean }
  /** The queue's Select, where it lists suggestions. */
  selects: boolean
}

function queueRow(n: number): QueueRow | undefined {
  return n > 0
    ? {
        title: words.queueRow.title(n),
        detail: words.queueRow.detail,
        count: n,
      }
    : undefined
}

/** A group record's kind in words, with its status where its kind binds
 * `lifecycle` and the app knows its declaration. */
export function groupKind(model: TasksModel, path: string): string {
  const kind = path.split("/").slice(0, 3).join("/")
  const declaration = model.declaration(kind)
  const label = kindLabel(kind, declaration).singular
  const status = declaration ? kindStatus(declaration.definition) : undefined
  const r = model.sources.held.get(path)
  const value = status && r ? r.properties[status.property] : undefined
  return words.group.kind(
    label,
    typeof value === "string" && value ? nameWords(value) : undefined
  )
}

export function presentList(
  selection: Selection,
  model: TasksModel,
  settings: Settings,
  filter: Selection | undefined,
  pending: ReadonlyMap<string, Pending>,
  now: Date
): ListView {
  if (selection.kind === "needs") return needsYou(model, filter, pending, now)
  if (selection.kind === "suggestions" || selection.kind === "noDate")
    return queue(selection, model, pending, now)
  const order = settings.order
  const showsClosed = settings.showCompleted
  const tasks = model.tasks.filter((t) => inSelection(t, selection))
  const open = sortTasks(tasks.filter(isOpen), order)
  const closed = tasks
    .filter((t) => !isOpen(t))
    .sort(
      (a, b) =>
        (b.completedAt?.getTime() ?? 0) - (a.completedAt?.getTime() ?? 0) ||
        (b.id > a.id ? 1 : -1)
    )
  const collection =
    selection.kind === "list"
      ? undefined
      : model.collection(selection.collection)
  const list = selection.kind === "list" ? model.list(selection.id) : undefined
  const own = list && model.lists.some((l) => l.id === list.id)
  const title = selectionTitle(selection, model)
  const rows = (ts: TaskItem[]) =>
    ts.map((t) => row(t, { now, pending: pending.get(t.id), model }))
  const openRows = rows(open)
  const late = openRows.filter((r) => r.due?.tone === "overdue").length
  const suggested = model.tasks.filter(
    (t) =>
      t.status === "proposed" &&
      selection.kind === "list" &&
      t.item.group === selection.id
  ).length
  const ownCollection =
    selection.kind === "list" ? !!own : selection.collection === ownTaskKind
  const inbox = selection.kind === "inbox"
  return {
    selection,
    title,
    summary:
      selection.kind === "list"
        ? {
            headline: words.group.open(open.length),
            detail: groupKind(model, selection.id),
          }
        : {
            headline: open.length ? `${open.length} to do` : "All done",
            overdue: late ? `${late} overdue` : undefined,
          },
    chips: [],
    bands: [],
    queue: queueRow(suggested),
    open: openRows,
    openTitle: words.group.todo,
    closed: showsClosed ? rows(closed) : [],
    closedTitle: words.group.done,
    showsClosed,
    toggleClosed: showsClosed ? "Hide Completed" : "Show Completed",
    quickAdd: ownCollection
      ? inbox
        ? "Add a task"
        : `Add a task to ${title}`
      : undefined,
    newTask: "New Task",
    reorders:
      order === "manual" && ownCollection && selection.kind !== "missing",
    orderNote: order === "due" ? "Sorted by due date" : undefined,
    empty: open.length
      ? undefined
      : inbox && collection
        ? {
            title: "No tasks",
            message: ownCollection
              ? "Tasks in no list show here. Tap + to add one."
              : `${collection.label.plural} in no group show here.`,
          }
        : {
            title: "No open tasks",
            message: ownCollection ? "Tap + to add a task." : "",
          },
    list: own ? list : undefined,
    group:
      selection.kind === "list"
        ? {
            kind: groupKind(model, selection.id),
            reference: selection.id.split("/").slice(0, 3).join("/"),
            pinned: model.sources.pins.includes(selection.id),
          }
        : undefined,
    selects: false,
  }
}

/** The suggestion queue, newest first, or the open tasks with no date,
 * the most urgent first. */
function queue(
  selection: Selection,
  model: TasksModel,
  pending: ReadonlyMap<string, Pending>,
  now: Date
): ListView {
  const suggestions = selection.kind === "suggestions"
  const paths = suggestions ? model.sources.suggestions : model.sources.noDate
  const open = paths
    .map((p) => model.task(p))
    .filter((t): t is TaskItem => !!t)
    .map((t) => row(t, { now, pending: pending.get(t.id), model, mixed: true }))
  return {
    selection,
    title: suggestions ? words.queue.title : words.noDateRow.title,
    summary: {
      headline: "",
      detail: suggestions ? words.queue.detail : words.noDateRow.detail,
    },
    chips: [],
    bands: [],
    open,
    openTitle: words.group.todo,
    closed: [],
    closedTitle: words.group.done,
    showsClosed: false,
    toggleClosed: "Show Completed",
    newTask: "New Task",
    reorders: false,
    empty: open.length
      ? undefined
      : suggestions
        ? { title: words.queue.emptyTitle, message: words.queue.emptyMessage }
        : { title: "All clear", message: "Every open task has a date." },
    selects:
      suggestions &&
      open.some((r) => r.suggestion?.accept || r.suggestion?.dismiss),
  }
}

/** Every open task across the collections in its band: what is late or
 * urgent, with the decisions on its row; what is coming this week; the
 * rest dated under Later; the undated in one counted row. The queue row
 * leads, and the chips narrow it to one pinned group. */
function needsYou(
  model: TasksModel,
  filter: Selection | undefined,
  pending: ReadonlyMap<string, Pending>,
  now: Date
): ListView {
  const { needsYou: n } = model.sources
  const narrowed = (t: TaskItem) =>
    !filter || (filter.kind === "list" && t.item.group === filter.id)
  const ranked = bands.flatMap((band) =>
    (band === "lateOrUrgent"
      ? n.lateOrUrgent
      : band === "comingUp"
        ? n.comingUp
        : n.later
    )
      .map((p) => model.task(p))
      .filter((t): t is TaskItem => !!t && narrowed(t))
      .map((task) => ({ task, band }))
  )
  const rows = ranked.map(({ task, band }) =>
    row(task, { now, pending: pending.get(task.id), model, mixed: true, band })
  )
  const headings = bands
    .map((band) => ({
      band,
      title: bandTitle[band],
      count: ranked.filter((r) => r.band === band).length,
    }))
    .filter((b) => b.count > 0)
  const openIn = (path: string) =>
    model.tasks.filter((t) => isOpen(t) && t.item.group === path).length
  const chips: ListView["chips"] = [
    { id: "all", title: "All", selected: !filter },
    ...model.sources.pins.map((path) => ({
      id: path,
      title: model.list(path)?.name || path,
      count: openIn(path),
      selection: { kind: "list" as const, id: path },
      selected: filter?.kind === "list" && filter.id === path,
    })),
  ]
  const noDate = model.tasks.filter(
    (t) => t.item.band === "noDate" && narrowed(t)
  ).length
  const suggestions = model.tasks.filter(
    (t) => t.status === "proposed" && narrowed(t)
  ).length
  const needing = headings[0]?.band === "lateOrUrgent"
  const narrowedTo =
    filter?.kind === "list" && model.lists.some((l) => l.id === filter.id)
      ? model.list(filter.id)?.name
      : undefined
  const anything = rows.length > 0 || noDate > 0 || suggestions > 0
  return {
    selection: { kind: "needs" },
    title: "Needs you",
    summary: {
      headline: needing ? `${headings[0].count} need you` : "Nothing needs you",
      detail: dayTitle(now),
    },
    chips: chips.length > 1 ? chips : [],
    bands: headings,
    clear: anything && !needing ? "Nothing needs you right now." : undefined,
    queue: queueRow(suggestions),
    noDate: noDate
      ? {
          title: words.noDateRow.title,
          detail: words.noDateRow.detail,
          count: noDate,
        }
      : undefined,
    open: rows,
    openTitle: words.group.todo,
    closed: [],
    closedTitle: words.group.done,
    showsClosed: false,
    toggleClosed: "Show Completed",
    quickAdd: narrowedTo
      ? `Add a task for today to ${narrowedTo}`
      : "Add a task for today",
    newTask: "New Task",
    reorders: false,
    empty: anything
      ? undefined
      : !filter
        ? {
            title: "All clear",
            message: "Nothing is open. Tap + to add a task.",
          }
        : {
            title: "All clear",
            message: "Nothing is open here. Choose All to see every list.",
          },
    selects: false,
  }
}

/** A selection's title: Needs you, the queue, No date, a group's name,
 * "No list" or "Missing" for a collection, or a collection's own name
 * where it groups by nothing. */
export function selectionTitle(s: Selection, model: TasksModel): string {
  switch (s.kind) {
    case "needs":
      return "Needs you"
    case "suggestions":
      return words.queue.title
    case "noDate":
      return words.noDateRow.title
    case "list": {
      const l = model.list(s.id)
      return l ? listTitle(l) : words.lists.missing
    }
    case "missing":
      return words.lists.missing
    case "inbox": {
      const c = model.collection(s.collection)
      if (!c) return words.lists.none("list")
      return c.group
        ? words.lists.none(
            propertyLabel(c.declaration.definition, c.group.property)
          )
        : c.label.plural
    }
  }
}

/** The App menu's badge: how many open tasks are late or urgent. */
export function needsYouBadge(model: TasksModel): number {
  return model.sources.needsYou.badge
}

/** A sidebar row's open count, and how it is read aloud. */
export function openCount(model: TasksModel, s: Selection) {
  const open =
    s.kind === "needs"
      ? model.sources.needsYou.badge
      : s.kind === "suggestions"
        ? model.sources.suggestions.length
        : s.kind === "noDate"
          ? model.sources.noDate.length
          : model.tasks.filter((t) => isOpen(t) && inSelection(t, s)).length
  const noun = s.kind === "suggestions" ? "suggestion" : "open task"
  return { open, words: open === 1 ? `1 ${noun}` : `${open} ${noun}s` }
}

/** One section of Lists: a group kind, headed by its plural label with its
 * full reference under it, its groups with open tasks, each collection's
 * none and missing rows, and how many more hold nothing open. */
export interface ListsSection {
  kind: string
  title: string
  reference: string
  rows: {
    selection: Selection
    title: string
    detail?: string
    open: number
    pinned: boolean
    own: boolean
  }[]
  /** Its groups with nothing open, behind "N more with nothing open". */
  more: { selection: Selection; title: string; detail?: string }[]
  moreTitle?: string
}

export function presentLists(
  model: TasksModel,
  query = ""
): {
  search: string
  pinned: ListsSection["rows"]
  sections: ListsSection[]
  /** Collections that group by nothing: one list of each's every task. */
  ungrouped: {
    selection: Selection
    title: string
    reference: string
    open: number
  }[]
} {
  const q = query.trim().toLowerCase()
  const matches = (title: string) => !q || title.toLowerCase().includes(q)
  const byKind = new Map<string, GroupSection[]>()
  for (const g of model.sources.groups) {
    const list = byKind.get(g.kind) ?? []
    list.push(g)
    byKind.set(g.kind, list)
  }
  const pins = new Set(model.sources.pins)
  const ownLists = new Set(model.lists.map((l) => l.id))
  const sections: ListsSection[] = []
  for (const [kind, groups] of byKind) {
    const label = kindLabel(kind, model.declaration(kind))
    const rows: ListsSection["rows"] = []
    const counted = new Set<string>()
    for (const g of groups)
      for (const r of g.rows) {
        if (r.group === "none" || r.group === "missing") continue
        counted.add(r.group)
      }
    const order = groupOrder(
      model.declaration(kind)?.definition,
      model.sources.held
    )
    const titled = [...counted]
      .sort(order)
      .map((path) => ({ path, title: model.list(path)?.name || path }))
    for (const { path, title } of titled) {
      if (!matches(title)) continue
      const open = groups.reduce(
        (n, g) => n + (g.rows.find((r) => r.group === path)?.open ?? 0),
        0
      )
      rows.push({
        selection: { kind: "list", id: path },
        title,
        detail: groupKind(model, path),
        open,
        pinned: pins.has(path),
        own: ownLists.has(path),
      })
    }
    for (const g of groups) {
      const c = model.collection(g.collection)!
      const reference = propertyLabel(c.declaration.definition, g.property)
      const none = g.rows.find((r) => r.group === "none")
      const missing = g.rows.find((r) => r.group === "missing")
      const noneTitle = words.lists.none(reference)
      if ((none || g.collection === ownTaskKind) && matches(noneTitle))
        rows.push({
          selection: { kind: "inbox", collection: g.collection },
          title: noneTitle,
          detail: words.lists.noneDetail(reference),
          open: none?.open ?? 0,
          pinned: false,
          own: false,
        })
      if (missing && matches(words.lists.missing))
        rows.push({
          selection: { kind: "missing", collection: g.collection },
          title: words.lists.missing,
          detail: words.lists.missingDetail(reference),
          open: missing.open,
          pinned: false,
          own: false,
        })
    }
    const more = [...model.groups.values()]
      .filter(
        (l) =>
          l.id.startsWith(`${kind}/`) &&
          !counted.has(l.id) &&
          matches(listTitle(l))
      )
      .sort((a, b) => order(a.id, b.id))
      .map((l) => ({
        selection: { kind: "list" as const, id: l.id },
        title: listTitle(l),
        detail: groupKind(model, l.id),
      }))
    if (!rows.length && !more.length && q) continue
    sections.push({
      kind,
      title: label.plural,
      reference: kind,
      rows,
      more,
      moreTitle: more.length ? words.lists.more(more.length) : undefined,
    })
  }
  const pinned = model.sources.pins
    .map((path) => {
      const l = model.list(path)
      if (!l || !matches(listTitle(l))) return undefined
      return {
        selection: { kind: "list", id: path } as Selection,
        title: listTitle(l),
        detail: groupKind(model, path),
        open: model.tasks.filter((t) => isOpen(t) && t.item.group === path)
          .length,
        pinned: true,
        own: ownLists.has(path),
      }
    })
    .filter((r): r is NonNullable<typeof r> => !!r)
  const ungrouped = model.sources.enabled
    .filter((c: Collection) => !c.group && matches(c.label.plural))
    .map((c) => ({
      selection: { kind: "inbox" as const, collection: c.kind },
      title: c.label.plural,
      reference: c.kind,
      open: model.tasks.filter((t) => t.kind === c.kind && isOpen(t)).length,
    }))
  const plurals = [...byKind.keys()].map(
    (k) => kindLabel(k, model.declaration(k)).plural
  )
  return {
    search: words.lists.search(plurals.length ? plurals : ["Lists"]),
    pinned,
    sections,
    ungrouped,
  }
}

/** The question before a list is deleted, naming what goes with it. No
 * promise of a restore: the server's next sweep, within minutes, purges
 * the list and the tasks it owns. */
export function deleteListQuestion(list: TaskList, tasks: TaskItem[]) {
  const count = tasks.filter((t) => t.own && t.list === list.id).length
  return {
    title: `Delete ${list.name ? `“${list.name}”` : "this list"}?`,
    message:
      count === 0
        ? "The list has no tasks."
        : count === 1
          ? "Its 1 task is deleted with it."
          : `Its ${count} tasks are deleted with it.`,
    confirm:
      count === 0
        ? "Delete List"
        : count === 1
          ? "Delete List and 1 Task"
          : `Delete List and ${count} Tasks`,
    cancel: "Cancel",
  }
}
