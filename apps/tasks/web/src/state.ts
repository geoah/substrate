import { useLive } from "@substrate/react"
import { useEffect, useMemo, useState, useSyncExternalStore } from "react"
import {
  Kinds,
  civilDay,
  readList,
  readTask,
  type Selection,
  type TaskItem,
  type TaskList,
} from "./model"

export interface TasksModel {
  tasks: TaskItem[]
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
      lists,
      task: (id: string) => byTask.get(id),
      list: (id: string) => byList.get(id),
      status: live.status,
      words: live.words,
    }
  }, [live])
}

/** Now, current to the minute: an hour passing makes a timed task overdue. */
export function useNow(): Date {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 30_000)
    return () => clearInterval(t)
  }, [])
  return now
}

export function useToday(): string {
  return civilDay(useNow())
}

// ---- the app's own settings, kept on this device ---------------------------

const settingsKey = "substrate.tasks.settings"
type Settings = { showCompleted: boolean }
const defaults: Settings = { showCompleted: false }
const listeners = new Set<() => void>()
let current: Settings = (() => {
  try {
    return {
      ...defaults,
      ...JSON.parse(localStorage.getItem(settingsKey) ?? "{}"),
    }
  } catch {
    return defaults
  }
})()

export function useSettings(): [Settings, (patch: Partial<Settings>) => void] {
  const value = useSyncExternalStore(
    (fn) => {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    () => current
  )
  const set = (patch: Partial<Settings>) => {
    current = { ...current, ...patch }
    try {
      localStorage.setItem(settingsKey, JSON.stringify(current))
    } catch {
      // a private window keeps nothing
    }
    for (const fn of listeners) fn()
  }
  return [value, set]
}

// ---- where the owner is: the address bar's hash ------------------------------

export type Section = Selection | { kind: "lists" } | { kind: "settings" }

export interface Route {
  section: Section
  task?: string
}

export function parseRoute(hash: string): Route {
  const parts = hash
    .replace(/^#\/?/, "")
    .split("/")
    .filter(Boolean)
    .map(decodeURIComponent)
  switch (parts[0]) {
    case "inbox":
      return { section: { kind: "inbox" }, task: parts[1] }
    case "list":
      return parts[1]
        ? { section: { kind: "list", id: parts[1] }, task: parts[2] }
        : { section: { kind: "lists" } }
    case "lists":
      return { section: { kind: "lists" } }
    case "settings":
      return { section: { kind: "settings" } }
    case "today":
    default:
      return {
        section: { kind: "today" },
        task: parts[0] === "today" ? parts[1] : undefined,
      }
  }
}

export function routeHash(r: Route): string {
  const s = r.section
  const base =
    s.kind === "list"
      ? `#/list/${encodeURIComponent(s.id)}`
      : s.kind === "lists"
        ? "#/lists"
        : `#/${s.kind}`
  return r.task &&
    (s.kind === "today" || s.kind === "inbox" || s.kind === "list")
    ? `${base}/${encodeURIComponent(r.task)}`
    : base
}

export function useRoute(): [Route, (r: Route, replace?: boolean) => void] {
  const hash = useSyncExternalStore(
    (fn) => {
      window.addEventListener("hashchange", fn)
      return () => window.removeEventListener("hashchange", fn)
    },
    () => window.location.hash
  )
  const route = useMemo(() => parseRoute(hash), [hash])
  const go = (r: Route, replace = false) => {
    const next = routeHash(r)
    if (next === window.location.hash) return
    if (replace) {
      window.history.replaceState(null, "", next)
      window.dispatchEvent(new HashChangeEvent("hashchange"))
    } else {
      window.location.hash = next
    }
  }
  return [route, go]
}

export function sameSelection(a: Section, b: Section): boolean {
  return (
    a.kind === b.kind &&
    (a.kind !== "list" || (b.kind === "list" && a.id === b.id))
  )
}
