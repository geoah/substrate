import {
  banded,
  bandTitle,
  bands,
  civilDay,
  dayTitle,
  dayWords,
  dueWords,
  inboxTitle,
  inSelection,
  isOpen,
  lateWords,
  listTitle,
  needsYouCount,
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
import type { Reschedule } from "./actions"
import type { Settings, TasksModel } from "./state"

/** The words TasksPresenter writes for each screen, from the same state:
 * the screens draw these and decide nothing. */

export interface RowView {
  task: TaskItem
  title: string
  pending: boolean
  /** What the round mark does: "Mark as Done", "Reopen", "Undo". */
  toggle: string
  due?: { text: string; tone: Tone }
  priority?: Priority
  /** The list it is in, where the column mixes lists (Needs you). */
  listName?: string
  band?: Band
  /** Where it can be moved to from its row, where it waits on a day. */
  reschedule?: { title: string; options: { label: string; how: Reschedule }[] }
  /** Won't Do, where it waits on a decision. */
  decides: boolean
  notes?: string
  /** The row read aloud: "Buy milk, due Today, priority High". */
  spoken: string
}

export function row(
  task: TaskItem,
  opts: {
    now: Date
    pending: boolean
    lists?: TaskList[]
    band?: Band
  }
): RowView {
  const { now, pending, band } = opts
  const today = civilDay(now)
  let due = task.due ? dueWords(task.due, now) : undefined
  // On Needs you a late task says how late, not when it was due: the day
  // is in the decision its row offers.
  if (band === "needsYou" && task.due && due?.tone === "overdue")
    due = { text: lateWords(task.due, now), tone: "overdue" }
  const priority = task.priority === "none" ? undefined : task.priority
  const listName = opts.lists
    ? task.list
      ? (opts.lists.find((l) => l.id === task.list)?.name ?? inboxTitle)
      : inboxTitle
    : undefined
  const title = taskTitle(task)
  const spoken = [title]
  if (pending) spoken.push("marked done, tap to undo")
  else if (task.status !== "open") spoken.push(statusWords[task.status])
  if (due)
    spoken.push(
      due.tone === "overdue" && band === "needsYou"
        ? due.text
        : `due ${due.text}`
    )
  if (priority) spoken.push(`priority ${priorityWords[priority]}`)
  if (listName) spoken.push(`in ${listName}`)
  if (task.description) spoken.push("has notes")
  const deciding = band === "needsYou" && !pending
  return {
    task,
    title,
    pending,
    toggle: pending ? "Undo" : isOpen(task) ? "Mark as Done" : "Reopen",
    due,
    priority,
    listName,
    band,
    reschedule: deciding ? reschedule(task, today) : undefined,
    decides: deciding,
    notes: task.description ? notesPreview(task.description) : undefined,
    spoken: spoken.join(", "),
  }
}

/** The days to move a task that waits on one to: today where it is late,
 * tomorrow, a week on, or the editor. */
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

export interface ListView {
  selection: Selection
  title: string
  summary: { headline: string; detail?: string; overdue?: string }
  /** Needs you's chips, All first; empty elsewhere. */
  chips: {
    id: string
    title: string
    selection?: Selection
    selected: boolean
  }[]
  /** The bands Needs you's rows fall in, those with rows alone. */
  bands: { band: Band; title: string; count: number }[]
  clear?: string
  open: RowView[]
  openTitle: string
  closed: RowView[]
  closedTitle: string
  showsClosed: boolean
  toggleClosed: string
  quickAdd: string
  newTask: string
  reorders: boolean
  orderNote?: string
  empty?: { title: string; message: string }
  /** The list's own: Rename List…, Delete List…. */
  list?: TaskList
}

export function presentList(
  selection: Selection,
  model: TasksModel,
  settings: Settings,
  filter: Selection | undefined,
  completing: ReadonlyMap<string, number>,
  now: Date
): ListView {
  if (selection.kind === "needs")
    return needsYou(model, filter, completing, now)
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
  const list = selection.kind === "list" ? model.list(selection.id) : undefined
  const title =
    selection.kind === "inbox" ? inboxTitle : list ? listTitle(list) : "List"
  const empty = open.length
    ? undefined
    : selection.kind === "inbox"
      ? {
          title: "No tasks",
          message: "Tasks in no list show here. Tap + to add one.",
        }
      : { title: "No open tasks", message: "Tap + to add a task." }
  const rows = (ts: TaskItem[]) =>
    ts.map((t) => row(t, { now, pending: completing.has(t.id) }))
  const openRows = rows(open)
  const late = openRows.filter((r) => r.due?.tone === "overdue").length
  return {
    selection,
    title,
    summary: {
      headline: open.length ? `${open.length} to do` : "All done",
      overdue: late ? `${late} overdue` : undefined,
    },
    chips: [],
    bands: [],
    open: openRows,
    openTitle: "To Do",
    closed: showsClosed ? rows(closed) : [],
    closedTitle: "Done",
    showsClosed,
    toggleClosed: showsClosed ? "Hide Completed" : "Show Completed",
    quickAdd:
      selection.kind === "inbox" ? "Add a task" : `Add a task to ${title}`,
    newTask: "New Task",
    reorders: order === "manual",
    orderNote: order === "due" ? "Sorted by due date" : undefined,
    empty,
    list,
  }
}

/** Every open task across the lists in its band: what waits on a decision,
 * with the decisions on its row; what is coming this week; the rest under
 * Later. The chips narrow it to one list. */
function needsYou(
  model: TasksModel,
  filter: Selection | undefined,
  completing: ReadonlyMap<string, number>,
  now: Date
): ListView {
  const ranked = banded(model.tasks, filter, now)
  const rows = ranked.map(({ task, band }) =>
    row(task, {
      now,
      pending: completing.has(task.id),
      lists: model.lists,
      band,
    })
  )
  const headings = bands
    .map((band) => ({
      band,
      title: bandTitle[band],
      count: ranked.filter((r) => r.band === band).length,
    }))
    .filter((b) => b.count > 0)
  // A chip for every list and the inbox, so a list narrowed to stays chosen
  // while its tasks are done, not only while it holds any.
  const chips: ListView["chips"] = [
    { id: "all", title: "All", selected: !filter },
  ]
  if (
    model.tasks.some((t) => isOpen(t) && t.list === undefined) ||
    filter?.kind === "inbox"
  )
    chips.push({
      id: "inbox",
      title: inboxTitle,
      selection: { kind: "inbox" },
      selected: filter?.kind === "inbox",
    })
  for (const l of model.lists)
    chips.push({
      id: l.id,
      title: listTitle(l),
      selection: { kind: "list", id: l.id },
      selected: filter?.kind === "list" && filter.id === l.id,
    })
  const needing = headings[0]?.band === "needsYou"
  const narrowed =
    filter?.kind === "inbox"
      ? inboxTitle
      : filter?.kind === "list"
        ? model.list(filter.id)?.name
        : undefined
  return {
    selection: { kind: "needs" },
    title: "Needs you",
    summary: {
      headline: needing ? `${headings[0].count} need you` : "Nothing needs you",
      detail: dayTitle(now),
    },
    chips: chips.length > 1 ? chips : [],
    bands: headings,
    clear: rows.length && !needing ? "Nothing needs you right now." : undefined,
    open: rows,
    openTitle: "To Do",
    closed: [],
    closedTitle: "Done",
    showsClosed: false,
    toggleClosed: "Show Completed",
    quickAdd: narrowed
      ? `Add a task for today to ${narrowed}`
      : "Add a task for today",
    newTask: "New Task",
    reorders: false,
    empty: rows.length
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
  }
}

/** The App menu's badge: how many open tasks wait on a decision. */
export function needsYouBadge(model: TasksModel, now: Date): number {
  return needsYouCount(model.tasks, now)
}

/** A sidebar row's open count, and how it is read aloud. */
export function openCount(model: TasksModel, s: Selection, now: Date) {
  const open =
    s.kind === "needs"
      ? needsYouCount(model.tasks, now)
      : model.tasks.filter((t) => isOpen(t) && inSelection(t, s)).length
  return { open, words: open === 1 ? "1 open task" : `${open} open tasks` }
}

/** The question before a list is deleted, naming what goes with it. No
 * promise of a restore: the server's next sweep, within minutes, purges
 * the list and the tasks it owns. */
export function deleteListQuestion(list: TaskList, tasks: TaskItem[]) {
  const count = tasks.filter((t) => t.list === list.id).length
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
