import {
  comparePositions,
  positionAfter,
  referencePath,
  refProp,
  stringProp,
  type SubstrateRecord,
} from "@substrate/client"

/** The package's kinds (apps/tasks/package): the same records the iPhone,
 * iPad, Mac and Android apps read and write. */
export const Kinds = {
  task: "substrate.reamde.dev/tasks/task",
  list: "substrate.reamde.dev/tasks/list",
} as const

export const kinds = [Kinds.task, Kinds.list]

export type Priority = "none" | "low" | "medium" | "high" | "urgent"
export type Status = "open" | "done" | "abandoned"

const priorityRank: Record<Priority, number> = {
  none: 0,
  low: 1,
  medium: 2,
  high: 3,
  urgent: 4,
}

export const priorityWords: Record<Priority, string> = {
  none: "None",
  low: "Low",
  medium: "Medium",
  high: "High",
  urgent: "Urgent",
}

export const priorities: { value: Priority; label: string }[] = (
  ["none", "low", "medium", "high", "urgent"] as const
).map((value) => ({ value, label: priorityWords[value] }))

/** A state as a fact (TaskStatus.words); the actions say "Won't Do". */
export const statusWords: Record<Status, string> = {
  open: "Open",
  done: "Done",
  abandoned: "Won't do",
}

/** Whether the package moves a task from one state to the other. */
export function moves(from: Status, to: Status): boolean {
  return (
    (from === "open" && to !== "open") || (from !== "open" && to === "open")
  )
}

/** What the owner calls a task in no list. */
export const inboxTitle = "No List"

/** Each fact's one label, in the read view and the edit view alike. */
export const fieldLabel = {
  name: "Name",
  notes: "Notes",
  link: "Link",
  list: "List",
  due: "Due",
  priority: "Priority",
  status: "Status",
  completed: "Completed",
} as const

/** Due: a civil day, or a moment with the day it falls on where it was
 * written. A timed task carries both (the package's `dueDay` and `dueAt`). */
export type Due = { day: string; at?: Date }

export interface TaskItem {
  id: string
  name: string
  description?: string
  url?: string
  list?: string
  position?: string
  due?: Due
  priority: Priority
  status: Status
  completedAt?: Date
  version: number
  record: SubstrateRecord
}

export interface TaskList {
  id: string
  name: string
  position?: string
  version: number
}

const isPriority = (v: unknown): v is Priority =>
  typeof v === "string" && v in priorityRank
const isStatus = (v: unknown): v is Status =>
  v === "open" || v === "done" || v === "abandoned"

function moment(text: string | undefined): Date | undefined {
  if (!text) return undefined
  const d = new Date(text)
  return Number.isNaN(d.getTime()) ? undefined : d
}

function nonEmpty(text: string | undefined): string | undefined {
  return text && text.trim() ? text : undefined
}

/** A task read leniently, as TasksCodec reads one. */
export function readTask(r: SubstrateRecord): TaskItem {
  const p = r.properties
  const at = moment(stringProp(r, "dueAt"))
  const dayText = stringProp(r, "dueDay")
  const day =
    dayText && /^\d{4}-\d{2}-\d{2}$/.test(dayText)
      ? dayText
      : at
        ? civilDay(at)
        : undefined
  const list = refProp(r, "list")
  return {
    id: r.id,
    name: stringProp(r, "name") ?? "",
    description: nonEmpty(stringProp(r, "description")),
    url: nonEmpty(stringProp(r, "url")),
    list: list && list.kind === Kinds.list ? list.id : undefined,
    position: positionText(p.position),
    due: day ? { day, at } : undefined,
    priority: isPriority(p.priority) ? p.priority : "none",
    status: isStatus(p.status) ? p.status : "open",
    completedAt: moment(stringProp(r, "completedAt")),
    version: r.version,
    record: r,
  }
}

export function readList(r: SubstrateRecord): TaskList {
  return {
    id: r.id,
    name: stringProp(r, "name") ?? "",
    position: positionText(r.properties.position),
    version: r.version,
  }
}

function positionText(v: unknown): string | undefined {
  if (typeof v === "string" && v !== "") return v
  if (typeof v === "number" && Number.isFinite(v)) return String(v)
  return undefined
}

export function listValue(id: string): string {
  return referencePath(Kinds.list, id)
}

export function taskTitle(t: { name: string }): string {
  return t.name || "Untitled task"
}

export function listTitle(l: { name: string }): string {
  return l.name || "Untitled list"
}

// ---- days -------------------------------------------------------------------

/** `YYYY-MM-DD` of a moment on this device's clock. */
export function civilDay(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, "0")
  const day = String(d.getDate()).padStart(2, "0")
  return `${y}-${m}-${day}`
}

/** A day's start on this device's clock, or the hour given. */
export function dayDate(day: string, hour = 0): Date {
  const [y, m, d] = day.split("-").map(Number)
  return new Date(y, m - 1, d, hour)
}

export function addDays(day: string, n: number): string {
  const d = dayDate(day, 12)
  d.setDate(d.getDate() + n)
  return civilDay(d)
}

/** Whole days from `a` to `b`, counted on the calendar. */
export function daysBetween(a: string, b: string): number {
  return Math.round(
    (dayDate(b, 12).getTime() - dayDate(a, 12).getTime()) / 86_400_000
  )
}

/** A moment on this device's clock: a day and a time of day. A time a clock
 * change skips lands on the day it names. */
export function atTime(day: string, hours: number, minutes: number): Date {
  const d = dayDate(day)
  d.setHours(hours, minutes, 0, 0)
  return civilDay(d) === day ? d : dayDate(day)
}

/** The wire's timestamp, to the second. */
export function timestamp(d: Date): string {
  return d.toISOString().replace(/\.\d{3}Z$/, "Z")
}

/** `dueDay` and `dueAt` as a pair: a day alone clears the moment. */
export function dueProperties(due: Due | undefined): Record<string, unknown> {
  if (!due) return { dueDay: null, dueAt: null }
  if (!due.at) return { dueDay: due.day, dueAt: null }
  return { dueDay: civilDay(due.at), dueAt: timestamp(due.at) }
}

export function sameDue(a?: Due, b?: Due): boolean {
  if (!a || !b) return a === b
  return (
    a.day === b.day && (a.at?.getTime() ?? null) === (b.at?.getTime() ?? null)
  )
}

/** The moment a due date sorts by: its time, or its day's start. */
function dueKey(due: Due): number {
  return due.at ? due.at.getTime() : dayDate(due.day).getTime()
}

export function isOverdue(due: Due, now: Date): boolean {
  return due.at ? due.at.getTime() < now.getTime() : due.day < civilDay(now)
}

// ---- where a task is shown ------------------------------------------------

/** What the list column shows: Needs you (the home screen), the tasks in no
 * list, or one list's. */
export type Selection =
  { kind: "needs" } | { kind: "inbox" } | { kind: "list"; id: string }

export function sameSelection(a?: Selection, b?: Selection): boolean {
  if (!a || !b) return a === b
  return (
    a.kind === b.kind &&
    (a.kind !== "list" || (b.kind === "list" && a.id === b.id))
  )
}

/** The list a task added here goes into; undefined is none. */
export function selectionList(s: Selection | undefined): string | undefined {
  return s?.kind === "list" ? s.id : undefined
}

export function isOpen(t: TaskItem): boolean {
  return t.status === "open"
}

/** The tasks a selection shows, open and closed alike; Needs you's are every
 * open task, whatever its list. */
export function inSelection(t: TaskItem, s: Selection): boolean {
  switch (s.kind) {
    case "inbox":
      return t.list === undefined
    case "list":
      return t.list === s.id
    case "needs":
      return isOpen(t)
  }
}

// ---- order -------------------------------------------------------------------

/** How a list's open tasks are ordered, as Settings chooses (TaskOrder). */
export type Order = "manual" | "due"

/** My order: by position, tasks with none last, then by id. */
export function byPosition(
  a: { position?: string; id: string },
  b: { position?: string; id: string }
): number {
  return (
    comparePositions(a.position, b.position) ||
    (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
  )
}

/** Soonest due first, tasks with no due date last, then my order. */
export function byDue(a: TaskItem, b: TaskItem): number {
  if (a.due && b.due) {
    const d = dueKey(a.due) - dueKey(b.due)
    return d || byPosition(a, b)
  }
  if (a.due) return -1
  if (b.due) return 1
  return byPosition(a, b)
}

export function sortTasks(tasks: TaskItem[], order: Order): TaskItem[] {
  return [...tasks].sort(order === "due" ? byDue : byPosition)
}

/** Lists by position, those with none last, then by name and id. */
export function arrangeLists(lists: TaskList[]): TaskList[] {
  return [...lists].sort(
    (a, b) =>
      comparePositions(a.position, b.position) ||
      a.name.toLowerCase().localeCompare(b.name.toLowerCase()) ||
      (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
  )
}

export function nextTaskPosition(
  tasks: TaskItem[],
  list: string | undefined
): string {
  return positionAfter(
    tasks.filter((t) => t.list === list).map((t) => t.position)
  )
}

// ---- Needs you's bands (TaskBand) -------------------------------------------

/** Where an open task falls on Needs you: waiting on a decision, due within
 * the week, or later. */
export type Band = "needsYou" | "upcoming" | "later"

export const bands: Band[] = ["needsYou", "upcoming", "later"]

export const bandTitle: Record<Band, string> = {
  needsYou: "Needs you",
  upcoming: "Coming up",
  later: "Later",
}

/** How far Coming up reaches past today. */
const horizonDays = 7

/** Late, or high and urgent with no day: waiting on a decision. */
export function bandOf(t: TaskItem, now: Date): Band {
  if (t.due) {
    if (isOverdue(t.due, now)) return "needsYou"
    return t.due.day <= addDays(civilDay(now), horizonDays)
      ? "upcoming"
      : "later"
  }
  return priorityRank[t.priority] >= priorityRank.high ? "needsYou" : "later"
}

/** Every open task, narrowed to `filter`, in its band, ranked: what is late
 * by how late, then what has no day by how urgent; what is coming by when;
 * the rest by when, the undated after by how urgent; ties in my order. */
export function banded(
  tasks: TaskItem[],
  filter: Selection | undefined,
  now: Date
): { task: TaskItem; band: Band }[] {
  return tasks
    .filter((t) => isOpen(t) && (!filter || inSelection(t, filter)))
    .map((task) => ({ task, band: bandOf(task, now) }))
    .sort((a, b) => {
      if (a.band !== b.band)
        return bands.indexOf(a.band) - bands.indexOf(b.band)
      const x = a.task
      const y = b.task
      if (x.due && y.due)
        return dueKey(x.due) - dueKey(y.due) || byPosition(x, y)
      if (x.due) return -1
      if (y.due) return 1
      return (
        priorityRank[y.priority] - priorityRank[x.priority] || byPosition(x, y)
      )
    })
}

export function needsYouCount(tasks: TaskItem[], now: Date): number {
  return tasks.filter((t) => isOpen(t) && bandOf(t, now) === "needsYou").length
}

// ---- words -------------------------------------------------------------------

export type Tone = "quiet" | "soon" | "overdue"

const timeFormat = new Intl.DateTimeFormat(undefined, {
  hour: "numeric",
  minute: "2-digit",
})

/** A time as the locale writes one: "15:00", or "3:00 PM". */
export function timeWords(d: Date): string {
  return timeFormat.format(d)
}

/** "Today", "Tomorrow", "Yesterday", else the weekday and the date, with
 * the year where it is not this one (DueWords.dayWords). */
export function dayWords(day: string, today: string): string {
  if (day === today) return "Today"
  if (day === addDays(today, 1)) return "Tomorrow"
  if (day === addDays(today, -1)) return "Yesterday"
  const d = dayDate(day, 12)
  return new Intl.DateTimeFormat(undefined, {
    weekday: "short",
    day: "numeric",
    month: "short",
    year: day.slice(0, 4) === today.slice(0, 4) ? undefined : "numeric",
  }).format(d)
}

/** A due date as a row says it: "Tomorrow, 15:00", "Overdue: Yesterday". */
export function dueWords(due: Due, now: Date): { text: string; tone: Tone } {
  const today = civilDay(now)
  const day = dayWords(due.day, today)
  const text = due.at ? `${day}, ${timeWords(due.at)}` : day
  if (isOverdue(due, now)) return { text: `Overdue: ${text}`, tone: "overdue" }
  return { text, tone: due.day === today ? "soon" : "quiet" }
}

/** How late a task is: "3 days late", or for one due at a time earlier
 * today, "Late since 15:00". */
export function lateWords(due: Due, now: Date): string {
  const days = daysBetween(due.day, civilDay(now))
  if (days >= 1) return days === 1 ? "1 day late" : `${days} days late`
  if (due.at) return `Late since ${timeWords(due.at)}`
  return "Late"
}

/** A day and a time in full, as the task's own screen says it: "Friday, 6
 * November 2026 at 15:00". */
export function fullWords(due: Due): string {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "full",
    timeStyle: due.at ? "short" : undefined,
  }).format(due.at ?? dayDate(due.day, 12))
}

/** Today as Needs you's summary says it: "Sunday, 8 November". */
export function dayTitle(now: Date): string {
  return new Intl.DateTimeFormat(undefined, {
    weekday: "long",
    day: "numeric",
    month: "long",
  }).format(now)
}

/** Notes as a row shows them: their words on one line, at most 160
 * characters, ending in "…" where they go on. Notes are markdown, which
 * the task's own screen draws, so the line drops its marks. */
export function notesPreview(notes: string): string {
  const plain = notes
    .replace(/^\s*(#{1,6}|>|[-*+]|\d+[.)])\s+(\[[ xX]\]\s+)?/gm, "")
    .replace(/\[([^\]]+)\]\([^)\s]+\)/g, "$1")
    .replace(/(\*\*|__|~~|`)(.+?)\1/g, "$2")
    .replace(/(^|[^\w*])\*(?!\s)(.+?)\*(?![\w*])/g, "$1$2")
    .replace(/(^|[^\w_])_(?!\s)(.+?)_(?![\w_])/g, "$1$2")
  const words = plain.split(/\s+/).filter(Boolean).join(" ")
  return words.length > 160 ? `${words.slice(0, 159)}…` : words
}

/** "9:00 AM", or "09:00", as the locale writes an hour. */
export function hourWords(hour: number): string {
  return timeFormat.format(new Date(2026, 0, 1, hour))
}
