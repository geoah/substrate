import { comparePositions, positionAfter } from "@substrate/client"
import {
  defineKind,
  refProp,
  stringProp,
  type SubstrateRecord,
} from "@substrate/react"
import { nameWords, ownTaskKind, type Band, type Item } from "./sources"

export type Priority = "none" | "low" | "medium" | "high" | "urgent"
/** A task's standing as its row and its mark show it: a dropped value of
 * any kind is the app's own `abandoned`, a proposed one a suggestion. */
export type Status = "open" | "done" | "abandoned" | "proposed"

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
  proposed: "Suggestion",
}

/** What the owner calls a task of the app's own in no list. */
export const inboxTitle = "No list"

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

/** A task of any collection as the screens read it. Its `id` is its record
 * path, `<kind>/<id>`: two kinds may hold one id. */
export interface TaskItem {
  id: string
  kind: string
  /** The record's own id within its kind. */
  recordID: string
  /** Of the app's own kind, which the editor edits whole. */
  own: boolean
  /** The contract's reading of it: standing, actions, group, band. */
  item: Item
  name: string
  description?: string
  url?: string
  /** The group record's path; absent for none, and for a group the token
   * does not hold (`item.group` says `missing`). */
  list?: string
  position?: string
  due?: Due
  priority: Priority
  /** A priority of another kind, by its value in words. */
  priorityLabel?: string
  pressing: boolean
  status: Status
  completedAt?: Date
  version: number
}

/** A group record: one of the app's own lists, or a record of any kind a
 * collection groups by. Its `id` is its record path. */
export interface TaskList {
  id: string
  name: string
  position?: string
  version: number
}

/** What a write of a task names, as TasksCodec writes one: a create its
 * name and what it holds, never its state, which moves only by patch, so a
 * create sent again after the task was done never moves it back; a change
 * or an edit the fields it sets, null clearing one. */
export interface TaskFields {
  name: string
  notes?: string | null
  link?: string | null
  /** A list's record path; null is no list. */
  list?: string | null
  position?: string
  due?: Due | null
  priority?: Priority
  status?: Status
}

export interface ListFields {
  name: string
  position?: string
}

const isPriority = (v: unknown): v is Priority =>
  typeof v === "string" && v in priorityRank
const isStatus = (v: unknown): v is Status =>
  v === "open" || v === "done" || v === "abandoned" || v === "proposed"

function moment(text: string | undefined): Date | undefined {
  if (!text) return undefined
  const d = new Date(text)
  return Number.isNaN(d.getTime()) ? undefined : d
}

function nonEmpty(text: string | undefined): string | undefined {
  return text && text.trim() ? text : undefined
}

/** The app's own task read leniently, as TasksCodec reads one: a value the
 * app does not know reads as the kind's default, so a newer build's record
 * never hides a task. */
export function readTask(r: SubstrateRecord): Omit<TaskItem, "item"> {
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
  const priority = isPriority(p.priority) ? p.priority : "none"
  return {
    id: `${r.kind}/${r.id}`,
    kind: r.kind,
    recordID: r.id,
    own: true,
    name: stringProp(r, "name") ?? "",
    description: nonEmpty(stringProp(r, "description")),
    url: nonEmpty(stringProp(r, "url")),
    list: list && List.is(list) ? List.ref(list.id) : undefined,
    position: positionText(p.position),
    due: day ? { day, at } : undefined,
    priority,
    pressing: priorityRank[priority] >= priorityRank.high,
    status: isStatus(p.status) ? p.status : "open",
    completedAt: moment(stringProp(r, "completedAt")),
    version: r.version,
  }
}

const standingStatus: Record<Item["standing"], Status> = {
  open: "open",
  done: "done",
  dropped: "abandoned",
  proposed: "proposed",
}

/** Any collection's item as the screens read a task: the app's own through
 * its codec, any other through the contract's readings. */
export function taskOf(item: Item): TaskItem {
  const status = standingStatus[item.standing]
  const list =
    item.group && item.group !== "none" && item.group !== "missing"
      ? item.group
      : undefined
  if (item.kind === ownTaskKind) {
    const t = readTask(item.record as SubstrateRecord)
    return { ...t, item, status, list }
  }
  const p = item.record.properties
  const description = p.description
  const url = p.url
  return {
    id: item.path,
    kind: item.kind,
    recordID: item.id,
    own: false,
    item,
    name: item.title,
    description:
      typeof description === "string" ? nonEmpty(description) : undefined,
    url: typeof url === "string" ? nonEmpty(url) : undefined,
    list,
    position: item.order,
    due: item.due,
    priority: "none",
    priorityLabel: item.priority ? nameWords(item.priority) : undefined,
    pressing: item.pressing,
    status,
    version: item.version,
  }
}

export function readList(r: SubstrateRecord): TaskList {
  return {
    id: `${r.kind}/${r.id}`,
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

/** A task's fields as properties: only what the draft names, so nothing
 * the app does not know is ever written over. */
export function writeTask(d: Partial<TaskFields>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  if (d.name !== undefined) out.name = d.name
  if (d.notes !== undefined) out.description = d.notes
  if (d.link !== undefined) out.url = d.link
  if (d.list !== undefined) out.list = d.list
  if (d.position !== undefined) out.position = d.position
  if (d.due !== undefined) Object.assign(out, dueProperties(d.due ?? undefined))
  if (d.priority !== undefined) out.priority = d.priority
  if (d.status !== undefined) {
    out.status = d.status
    // The move to done stamps completedAt and nothing removes it on the
    // way back, so a reopen clears it in the same write.
    if (d.status === "open") out.completedAt = null
  }
  return out
}

export function writeList(d: Partial<ListFields>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  if (d.name !== undefined) out.name = d.name
  if (d.position !== undefined) out.position = d.position
  return out
}

/** The package's kinds (apps/tasks/package): the same records the iPhone,
 * iPad, Mac and Android apps read and write, through the same codec. */
export const List = defineKind("substrate.reamde.dev/tasks/list", {
  read: readList,
  write: writeList,
})

export const Task = defineKind("substrate.reamde.dev/tasks/task", {
  read: readTask,
  write: writeTask,
})

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

/** What the list column shows: Needs you (the home screen), the suggestion
 * queue, the open tasks with no date, a collection's tasks in no group (or
 * every one of them where it groups by nothing), those whose group is gone
 * or out of reach, or one group's, by its record path. */
export type Selection =
  | { kind: "needs" }
  | { kind: "suggestions" }
  | { kind: "noDate" }
  | { kind: "inbox"; collection: string }
  | { kind: "missing"; collection: string }
  | { kind: "list"; id: string }

export function selectionKey(s: Selection): string {
  switch (s.kind) {
    case "list":
      return `list:${s.id}`
    case "inbox":
    case "missing":
      return `${s.kind}:${s.collection}`
    default:
      return s.kind
  }
}

export function sameSelection(a?: Selection, b?: Selection): boolean {
  if (!a || !b) return a === b
  return selectionKey(a) === selectionKey(b)
}

/** The list a task added here goes into; undefined is none. */
export function selectionList(s: Selection | undefined): string | undefined {
  return s?.kind === "list" ? s.id : undefined
}

export function isOpen(t: TaskItem): boolean {
  return t.status === "open"
}

/** The tasks a selection shows, open and closed alike, suggestions apart:
 * Needs you's are every open task, the queue's every suggestion. */
export function inSelection(t: TaskItem, s: Selection): boolean {
  switch (s.kind) {
    case "needs":
      return isOpen(t)
    case "suggestions":
      return t.status === "proposed"
    case "noDate":
      return t.item.band === "noDate"
    case "inbox":
      return (
        t.kind === s.collection &&
        t.status !== "proposed" &&
        (t.item.group === "none" || t.item.group === null)
      )
    case "missing":
      return (
        t.kind === s.collection &&
        t.status !== "proposed" &&
        t.item.group === "missing"
      )
    case "list":
      return t.item.group === s.id && t.status !== "proposed"
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
    tasks.filter((t) => t.own && t.list === list).map((t) => t.position)
  )
}

// ---- Needs you's bands --------------------------------------------------------

export type { Band }

/** The bands Needs you lists, in order; No date is one counted row. */
export const bands: Band[] = ["lateOrUrgent", "comingUp", "later"]

export const bandTitle: Record<Band, string> = {
  lateOrUrgent: "Late or urgent",
  comingUp: "Coming up",
  later: "Later",
  noDate: "No date",
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
