import { useLive } from "@substrate/react"
import { useEffect, useMemo, useState, useSyncExternalStore } from "react"
import {
  Kinds,
  arrangeLists,
  readList,
  readTask,
  type Order,
  type Selection,
  type TaskItem,
  type TaskList,
} from "./model"

export interface TasksModel {
  tasks: TaskItem[]
  /** In the owner's order. */
  lists: TaskList[]
  task: (id: string) => TaskItem | undefined
  list: (id: string) => TaskList | undefined
  status: string
  words?: string
}

/** The live records read as tasks and lists. */
export function useTasks(): TasksModel {
  const live = useLive()
  return useMemo(() => {
    const tasks: TaskItem[] = []
    const lists: TaskList[] = []
    for (const r of live.records.values()) {
      if (r.kind === Kinds.task) tasks.push(readTask(r))
      else if (r.kind === Kinds.list) lists.push(readList(r))
    }
    const listIDs = new Set(lists.map((l) => l.id))
    // A task whose list was deleted goes with it (onDelete: cascade): until
    // the collector's pass, the app shows none of its tasks.
    const shown = tasks.filter(
      (t) => t.list === undefined || listIDs.has(t.list)
    )
    const byTask = new Map(shown.map((t) => [t.id, t]))
    const byList = new Map(lists.map((l) => [l.id, l]))
    return {
      tasks: shown,
      lists: arrangeLists(lists),
      task: (id: string) => byTask.get(id),
      list: (id: string) => byList.get(id),
      status: live.status,
      words: live.words,
    }
  }, [live])
}

/** Now, presented again when the words drawn stop being true: at the next
 * minute, so midnight moves "Tomorrow" to "Today" and a timed task falls
 * due as its minute comes. */
export function useNow(): Date {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const t = setTimeout(
      () => setNow(new Date()),
      60_000 - (now.getTime() % 60_000) + 50
    )
    return () => clearTimeout(t)
  }, [now])
  return now
}

// ---- a value many screens read ------------------------------------------------

export interface Store<T> {
  get: () => T
  set: (next: T | ((value: T) => T)) => void
  subscribe: (fn: () => void) => () => void
}

export function store<T>(initial: T): Store<T> {
  let value = initial
  const listeners = new Set<() => void>()
  return {
    get: () => value,
    set(next) {
      const v =
        typeof next === "function" ? (next as (value: T) => T)(value) : next
      if (Object.is(v, value)) return
      value = v
      for (const fn of listeners) fn()
    },
    subscribe(fn) {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
  }
}

export function useStore<T>(s: Store<T>): T {
  return useSyncExternalStore(s.subscribe, s.get)
}

// ---- the app's own settings, kept on this device (TasksSettings) -------------

export interface Settings {
  /** Off until the owner turns it on, which is when the browser is asked
   * whether the app may notify. */
  remind: boolean
  remindHour: number
  order: Order
  showCompleted: boolean
}

export const reminderHours = [7, 8, 9, 12, 18]

const settingsKey = "substrate.tasks.settings"
const defaults: Settings = {
  remind: false,
  remindHour: 9,
  order: "manual",
  showCompleted: false,
}

function readSettings(): Settings {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(settingsKey) ?? "{}")
    const s = (saved && typeof saved === "object" ? saved : {}) as Partial<
      Record<keyof Settings, unknown>
    >
    const hour = typeof s.remindHour === "number" ? s.remindHour : NaN
    return {
      remind: s.remind === true,
      remindHour: reminderHours.includes(hour) ? hour : defaults.remindHour,
      order: s.order === "due" ? "due" : "manual",
      showCompleted: s.showCompleted === true,
    }
  } catch {
    return defaults
  }
}

export const settingsStore = store<Settings>(readSettings())

export function useSettings(): [Settings, (patch: Partial<Settings>) => void] {
  const value = useStore(settingsStore)
  return [value, setSettings]
}

export function setSettings(patch: Partial<Settings>) {
  settingsStore.set((s) => ({ ...s, ...patch }))
  try {
    localStorage.setItem(settingsKey, JSON.stringify(settingsStore.get()))
  } catch {
    // a private window keeps nothing
  }
}

// ---- where the owner is: the address bar's hash ------------------------------

/** The App menu's sections (TasksMenu). */
export type Section = "needs" | "lists" | "settings"

export interface Route {
  section: Section
  /** The list column's; undefined in Lists before one is chosen, where a
   * phone shows the lists themselves. */
  selection?: Selection
  task?: string
  /** A reminder's task, opened in its list once its record is read. */
  tapped?: string
}

export function parseRoute(hash: string): Route {
  const parts = hash
    .replace(/^#\/?/, "")
    .split("/")
    .filter(Boolean)
    .map(decodeURIComponent)
  switch (parts[0]) {
    case "inbox":
      return { section: "lists", selection: { kind: "inbox" }, task: parts[1] }
    case "list":
      return parts[1]
        ? {
            section: "lists",
            selection: { kind: "list", id: parts[1] },
            task: parts[2],
          }
        : { section: "lists" }
    case "lists":
      return { section: "lists" }
    case "settings":
      return { section: "settings" }
    case "reminder":
      return { section: "lists", tapped: parts[1] }
    case "needs":
    case "today":
      return { section: "needs", selection: { kind: "needs" }, task: parts[1] }
    default:
      return { section: "needs", selection: { kind: "needs" } }
  }
}

export function routeHash(r: Route): string {
  if (r.tapped) return `#/reminder/${encodeURIComponent(r.tapped)}`
  if (r.section === "settings") return "#/settings"
  const s = r.selection
  const base =
    s?.kind === "list"
      ? `#/list/${encodeURIComponent(s.id)}`
      : s?.kind === "inbox"
        ? "#/inbox"
        : s?.kind === "needs"
          ? "#/needs"
          : "#/lists"
  return s && r.task ? `${base}/${encodeURIComponent(r.task)}` : base
}

function readHash() {
  return window.location.hash
}

function subscribeHash(fn: () => void) {
  window.addEventListener("hashchange", fn)
  return () => window.removeEventListener("hashchange", fn)
}

export function currentRoute(): Route {
  return parseRoute(window.location.hash)
}

export function go(r: Route, replace = false) {
  const next = routeHash(r)
  if (next === window.location.hash) return
  if (replace) {
    window.history.replaceState(null, "", next)
    window.dispatchEvent(new HashChangeEvent("hashchange"))
  } else {
    window.location.hash = next
  }
}

export function useRoute(): Route {
  const hash = useSyncExternalStore(subscribeHash, readHash)
  return useMemo(() => parseRoute(hash), [hash])
}

/** A section chosen: Needs you shows its rows; Lists keeps the list shown
 * wide, and shows the lists themselves on a phone. */
export function selectSection(section: Section, wide: boolean) {
  const r = currentRoute()
  if (section === r.section && !r.task) {
    if (section === "lists" && !wide && r.selection) go({ section })
    return
  }
  if (section === "needs") go({ section, selection: { kind: "needs" } })
  else if (section === "lists")
    go({
      section,
      selection: wide
        ? r.selection?.kind === "needs" || !r.selection
          ? { kind: "inbox" }
          : r.selection
        : undefined,
    })
  else go({ section })
}

/** A list shown, or none: on a phone, back to the lists. */
export function show(selection: Selection | undefined) {
  if (selection?.kind === "needs") go({ section: "needs", selection })
  else go({ section: "lists", selection })
}

export function openTask(id: string | undefined) {
  const r = currentRoute()
  go({ ...r, task: id })
}

// ---- Needs you narrowed to one list or the inbox ------------------------------

export const filterStore = store<Selection | undefined>(undefined)

// ---- what is open over the screens --------------------------------------------

/** What an editor opened on: a task (as it stood then), or a new task in
 * the list it names, due on the day it names. */
export type EditorSeed =
  { kind: "task"; id: string } | { kind: "new"; list?: string; day?: string }

export const editorStore = store<{ key: number; seed: EditorSeed } | undefined>(
  undefined
)

let editorKey = 0
export function openEditor(seed: EditorSeed) {
  editorStore.set({ key: ++editorKey, seed })
}

/** A list being named: a new one, or one renamed. */
export const namingStore = store<{ id?: string; name: string } | undefined>(
  undefined
)

/** The list whose delete waits on the owner's answer. */
export const deletingListStore = store<string | undefined>(undefined)
