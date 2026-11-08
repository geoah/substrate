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

export const priorities: { value: Priority; label: string }[] = [
  { value: "none", label: "None" },
  { value: "low", label: "Low" },
  { value: "medium", label: "Medium" },
  { value: "high", label: "High" },
  { value: "urgent", label: "Urgent" },
]

export const statusLabel: Record<Status, string> = {
  open: "Open",
  done: "Done",
  abandoned: "Won't Do",
}

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
  completedAt?: string
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
  v === "none" ||
  v === "low" ||
  v === "medium" ||
  v === "high" ||
  v === "urgent"
const isStatus = (v: unknown): v is Status =>
  v === "open" || v === "done" || v === "abandoned"

/** A task read leniently, as TasksCodec reads one. */
export function readTask(r: SubstrateRecord): TaskItem {
  const p = r.properties
  const atText = stringProp(r, "dueAt")
  const at = atText ? new Date(atText) : undefined
  const validAt = at && !Number.isNaN(at.getTime()) ? at : undefined
  const dayText = stringProp(r, "dueDay")
  const day =
    dayText && /^\d{4}-\d{2}-\d{2}$/.test(dayText)
      ? dayText
      : validAt
        ? civilDay(validAt)
        : undefined
  const list = refProp(r, "list")
  return {
    id: r.id,
    name: stringProp(r, "name") ?? "",
    description: stringProp(r, "description"),
    url: stringProp(r, "url"),
    list: list && list.kind === Kinds.list ? list.id : undefined,
    position: positionText(p.position),
    due: day ? { day, at: validAt } : undefined,
    priority: isPriority(p.priority) ? p.priority : "none",
    status: isStatus(p.status) ? p.status : "open",
    completedAt: stringProp(r, "completedAt"),
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

/** `YYYY-MM-DD` of a moment on this device's clock. */
export function civilDay(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, "0")
  const day = String(d.getDate()).padStart(2, "0")
  return `${y}-${m}-${day}`
}

export function dayDate(day: string): Date {
  const [y, m, d] = day.split("-").map(Number)
  return new Date(y, m - 1, d)
}

export function addDays(day: string, n: number): string {
  const d = dayDate(day)
  d.setDate(d.getDate() + n)
  return civilDay(d)
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

export type Selection =
  { kind: "today" } | { kind: "inbox" } | { kind: "list"; id: string }

export function isOpen(t: TaskItem): boolean {
  return t.status === "open"
}

export function inSelection(t: TaskItem, s: Selection, today: string): boolean {
  switch (s.kind) {
    case "inbox":
      return t.list === undefined
    case "list":
      return t.list === s.id
    case "today":
      return isOpen(t) && !!t.due && t.due.day <= today
  }
}

/** The manual order: position as a number, then the name, then the id. */
export function byPosition(
  a: { position?: string; name: string; id: string },
  b: typeof a
): number {
  return (
    comparePositions(a.position, b.position) ||
    a.name.toLowerCase().localeCompare(b.name.toLowerCase()) ||
    a.id.localeCompare(b.id)
  )
}

function dueInstant(t: TaskItem): number | undefined {
  if (!t.due) return undefined
  return t.due.at ? t.due.at.getTime() : dayDate(t.due.day).getTime()
}

/** Today's order: soonest due first, then the manual order. */
export function byDue(a: TaskItem, b: TaskItem): number {
  const x = dueInstant(a)
  const y = dueInstant(b)
  if (x !== undefined && y !== undefined && x !== y) return x - y
  if (x !== undefined && y === undefined) return -1
  if (x === undefined && y !== undefined) return 1
  return byPosition(a, b)
}

export function nextTaskPosition(
  tasks: TaskItem[],
  list: string | undefined
): string {
  return positionAfter(
    tasks.filter((t) => t.list === list).map((t) => t.position)
  )
}

export type Tone = "quiet" | "soon" | "overdue"

/** A due date's words and tone, as a row shows it: Today, Tomorrow, a
 * weekday this week, else the date; a time beside it when it has one. */
export function dueWords(due: Due, now: Date): { text: string; tone: Tone } {
  const today = civilDay(now)
  const time = due.at
    ? new Intl.DateTimeFormat(undefined, {
        hour: "2-digit",
        minute: "2-digit",
      }).format(due.at)
    : undefined
  let day: string
  if (due.day === today) day = "Today"
  else if (due.day === addDays(today, 1)) day = "Tomorrow"
  else if (due.day === addDays(today, -1)) day = "Yesterday"
  else if (due.day > today && due.day <= addDays(today, 6))
    day = new Intl.DateTimeFormat(undefined, { weekday: "long" }).format(
      dayDate(due.day)
    )
  else {
    const d = dayDate(due.day)
    day = new Intl.DateTimeFormat(undefined, {
      weekday: "short",
      day: "numeric",
      month: "short",
      year: d.getFullYear() === now.getFullYear() ? undefined : "numeric",
    }).format(d)
  }
  const overdue = due.at ? due.at.getTime() < now.getTime() : due.day < today
  const tone: Tone = overdue ? "overdue" : due.day === today ? "soon" : "quiet"
  return { text: time ? `${day}, ${time}` : day, tone }
}

/** The due date in full, as the task's own screen shows it. */
export function dueLong(due: Due): string {
  const day = new Intl.DateTimeFormat(undefined, { dateStyle: "full" }).format(
    dayDate(due.day)
  )
  if (!due.at) return day
  const time = new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  }).format(due.at)
  return `${day} at ${time}`
}
