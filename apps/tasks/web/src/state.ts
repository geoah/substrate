import { lifecycleTrait } from "@substrate/client"
import { useKindRecords, useSubstrate } from "@substrate/react"
import { useEffect, useMemo, useSyncExternalStore } from "react"
import {
  arrangeLists,
  readList,
  taskOf,
  type Order,
  type Selection,
  type TaskItem,
  type TaskList,
} from "./model"
import { ownDeclarations } from "./package"
import {
  collectionKind,
  kindDeclaration,
  ownListKind,
  ownTaskKind,
  preferenceKind,
  readSources,
  recordTitle,
  type Collection,
  type Declaration,
  type Envelope,
  type Sources,
  type SourcesInput,
} from "./sources"

export interface TasksModel {
  sources: Sources
  /** Every item of the collections that are on. */
  tasks: TaskItem[]
  /** The app's own lists, in the owner's order. */
  lists: TaskList[]
  /** Every group record held, of any kind a collection groups by. */
  groups: ReadonlyMap<string, TaskList>
  task: (id: string) => TaskItem | undefined
  list: (id: string) => TaskList | undefined
  collection: (kind: string) => Collection | undefined
  /** A kind's declaration, where the server served it or the app ships it. */
  declaration: (kind: string) => Declaration | undefined
  /** The scopes the token holds. */
  grant: readonly string[]
  /** Whether every kind the link reads was read whole, so a task missing
   * from it is gone rather than not read yet. */
  listed: boolean
}

/** The kinds the settings live in, read whatever else is. */
export const settingsKinds = [collectionKind, preferenceKind]

/** The kinds the link reads: the settings, the app's own task and list,
 * and every collection that is on with the group kind it reads. */
export const linkKindsStore = store<readonly string[]>([
  ...settingsKinds,
  ownTaskKind,
  ownListKind,
])

/** The declarations the server's implementors route answered for the
 * token: every kind with a status it reads. Undefined until it answers. */
export const discoveredStore = store<readonly Declaration[] | undefined>(
  undefined
)

/** The declarations of the group kinds the token reads that bind no
 * lifecycle, which the implementors route never lists: read one by one,
 * for their labels and their `position` order. */
export const groupDeclarationsStore = store<ReadonlyMap<string, Declaration>>(
  new Map()
)

let lastMerge:
  | {
      a: readonly Declaration[] | undefined
      b: ReadonlyMap<string, Declaration>
      out: readonly Declaration[] | undefined
    }
  | undefined

/** The two answers as one list, the same array while neither moves. */
function merged(
  a: readonly Declaration[] | undefined,
  b: ReadonlyMap<string, Declaration>
): readonly Declaration[] | undefined {
  if (lastMerge && lastMerge.a === a && lastMerge.b === b) return lastMerge.out
  const out =
    b.size === 0
      ? a
      : [
          ...(a ?? []),
          ...[...b.values()].filter(
            (d) => !a?.some((x) => x.identity === d.identity)
          ),
        ]
  lastMerge = { a, b, out }
  return out
}

/** The device's zone, which days fall in. */
export const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone

/** The declarations the model reads: the server's, with the app's own
 * package's where it has none of its own. */
function declarationsOf(
  discovered: readonly Declaration[] | undefined
): Declaration[] {
  const out = [...(discovered ?? [])]
  for (const d of ownDeclarations)
    if (!out.some((x) => x.identity === d.identity)) out.push(d)
  return out
}

let built:
  | {
      records: ReadonlyMap<string, ReadonlyMap<string, unknown>>
      discovered: readonly Declaration[] | undefined
      grant: readonly string[]
      now: Date
      listed: boolean
      model: TasksModel
    }
  | undefined

/** Every collection's tasks and every group, as the contract reads them:
 * the same object while the records, the declarations, the grant and the
 * minute stand, however many screens ask. */
export function useTasks(): TasksModel {
  const { linked, session } = useSubstrate()
  const kinds = useStore(linkKindsStore)
  const records = useKindRecords(kinds)
  const discovered = merged(
    useStore(discoveredStore),
    useStore(groupDeclarationsStore)
  )
  const now = useNow()
  const listed = useSyncExternalStore(linked.subscribe, () =>
    kinds.every((k) => linked.hasListed(k))
  )
  return modelOf(records, discovered, session.scopes, now, listed)
}

/** The model for these inputs: the last one built while they stand. */
function modelOf(
  records: ReadonlyMap<string, ReadonlyMap<string, unknown>>,
  discovered: readonly Declaration[] | undefined,
  grant: readonly string[],
  now: Date,
  listed: boolean
): TasksModel {
  const b = built
  if (
    b &&
    b.records === records &&
    b.discovered === discovered &&
    b.grant === grant &&
    b.now === now &&
    b.listed === listed
  )
    return b.model
  const all: Envelope[] = []
  for (const m of records.values())
    for (const r of m.values()) all.push(r as Envelope)
  const model = buildModel({
    declarations: declarationsOf(discovered),
    records: all,
    grant,
    now,
    timeZone,
    listed,
  })
  built = { records, discovered, grant, now, listed, model }
  return model
}

/** The screens' model of the contract's reading: every item as a task,
 * the app's own lists in the owner's order, and every group record held. */
export function buildModel(
  input: SourcesInput & { listed: boolean }
): TasksModel {
  const { declarations, grant, listed } = input
  const byDeclaration = new Map(declarations.map((d) => [d.identity, d]))
  const sources = readSources(input)
  const tasks = [...sources.items.values()].map(taskOf)
  const byTask = new Map(tasks.map((t) => [t.id, t]))
  const lists: TaskList[] = []
  const groups = new Map<string, TaskList>()
  const groupKinds = new Set(
    sources.enabled.flatMap((c) => (c.group ? [c.group.kind] : []))
  )
  for (const [path, r] of sources.held) {
    if (r.kind === ownListKind) {
      const l = readList(r as never)
      lists.push(l)
      groups.set(path, l)
    } else if (groupKinds.has(r.kind))
      groups.set(path, {
        id: path,
        name: recordTitle(r),
        version: r.version,
      })
  }
  const byKind = new Map(sources.collections.map((c) => [c.kind, c]))
  const model: TasksModel = {
    sources,
    tasks,
    lists: arrangeLists(lists),
    groups,
    task: (id) => byTask.get(id),
    list: (id) => groups.get(id),
    collection: (kind) => byKind.get(kind),
    declaration: (kind) => byDeclaration.get(kind),
    grant,
    listed,
  }
  return model
}

/** Keeps the link's kinds to the collections that are on and the group
 * kinds they read; the settings kinds stay whatever changes. */
export function useLinkKinds(model: TasksModel) {
  const next = useMemo(() => {
    const kinds = new Set([...settingsKinds, ownTaskKind, ownListKind])
    for (const c of model.sources.enabled) {
      kinds.add(c.kind)
      if (c.group) kinds.add(c.group.kind)
    }
    return [...kinds]
  }, [model.sources.enabled])
  useEffect(() => {
    const was = linkKindsStore.get()
    if (was.length !== next.length || next.some((k) => !was.includes(k)))
      linkKindsStore.set(next)
  }, [next])
}

/** Finds the kinds with a status the token reads: at sign-in and after an
 * approval (each a new session, so a new client), and again whenever the
 * page comes back to the front, since the change feed an app's token
 * follows carries no declarations. */
export function useDiscovery(model: TasksModel) {
  const { client } = useSubstrate()
  // The group kinds a collection that is on reads, wanting a declaration:
  // read again after an approval (a new client) or a grouping change.
  const wanted = [
    ...new Set(
      model.sources.enabled.flatMap((c) =>
        c.group && !model.declaration(c.group.kind) ? [c.group.kind] : []
      )
    ),
  ]
    .sort()
    .join(" ")
  useEffect(() => {
    if (!wanted) return
    let current = true
    for (const kind of wanted.split(" "))
      void client.record(kindDeclaration, kind).then(
        (r) => {
          if (!current || !r) return
          const definition = r.properties
          const label = definition.label as Declaration["label"]
          groupDeclarationsStore.set((m) =>
            new Map(m).set(kind, { identity: kind, label, definition })
          )
        },
        () => {
          // Unread, the kind is named in words and ordered by title.
        }
      )
    return () => {
      current = false
    }
  }, [client, wanted])
  useEffect(() => {
    let current = true
    const ask = () => {
      void client.implementors(lifecycleTrait).then(
        (kinds) => {
          if (!current) return
          discoveredStore.set(
            kinds.map((k) => ({
              identity: k.identity,
              label: k.label,
              definition: k.definition,
            }))
          )
        },
        () => {
          // A server that cannot answer leaves the app's own kinds read.
        }
      )
    }
    ask()
    const visible = () => {
      if (document.visibilityState === "visible") ask()
    }
    document.addEventListener("visibilitychange", visible)
    return () => {
      current = false
      document.removeEventListener("visibilitychange", visible)
    }
  }, [client])
}

const nowStore = store(new Date())
let ticking = false

/** Now, presented again when the words drawn stop being true: at the next
 * minute, so midnight moves "Tomorrow" to "Today" and a timed task falls
 * due as its minute comes. One clock for every screen. */
export function useNow(): Date {
  useEffect(() => {
    if (ticking) return
    ticking = true
    const tick = () => {
      nowStore.set(new Date())
      setTimeout(tick, 60_000 - (Date.now() % 60_000) + 50)
    }
    setTimeout(tick, 60_000 - (Date.now() % 60_000) + 50)
  }, [])
  return useStore(nowStore)
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

/** A page under Settings: Collections, or one collection's. */
export type SettingsPage =
  { page: "collections" } | { page: "collection"; kind: string }

export interface Route {
  section: Section
  /** The list column's; undefined in Lists before one is chosen, where a
   * phone shows the lists themselves. */
  selection?: Selection
  /** The open task, by its record path. */
  task?: string
  /** A reminder's task, opened in its list once its record is read. */
  tapped?: string
  settings?: SettingsPage
}

export function parseRoute(hash: string): Route {
  const parts = hash
    .replace(/^#\/?/, "")
    .split("/")
    .filter(Boolean)
    .map(decodeURIComponent)
  const lists = (selection: Selection, task?: string): Route => ({
    section: "lists",
    selection,
    task,
  })
  switch (parts[0]) {
    case "inbox":
      return lists({ kind: "inbox", collection: ownTaskKind }, parts[1])
    case "none":
      return parts[1]
        ? lists({ kind: "inbox", collection: parts[1] }, parts[2])
        : { section: "lists" }
    case "missing":
      return parts[1]
        ? lists({ kind: "missing", collection: parts[1] }, parts[2])
        : { section: "lists" }
    case "list":
      return parts[1]
        ? lists({ kind: "list", id: parts[1] }, parts[2])
        : { section: "lists" }
    case "lists":
      return { section: "lists" }
    case "settings":
      return parts[1] === "collections"
        ? { section: "settings", settings: { page: "collections" } }
        : parts[1] === "collection" && parts[2]
          ? {
              section: "settings",
              settings: { page: "collection", kind: parts[2] },
            }
          : { section: "settings" }
    case "reminder":
      return { section: "lists", tapped: parts[1] }
    case "suggestions":
      return {
        section: "needs",
        selection: { kind: "suggestions" },
        task: parts[1],
      }
    case "nodate":
      return { section: "needs", selection: { kind: "noDate" }, task: parts[1] }
    case "needs":
    case "today":
      return { section: "needs", selection: { kind: "needs" }, task: parts[1] }
    default:
      return { section: "needs", selection: { kind: "needs" } }
  }
}

const seg = encodeURIComponent

export function routeHash(r: Route): string {
  if (r.tapped) return `#/reminder/${seg(r.tapped)}`
  if (r.section === "settings")
    return r.settings?.page === "collections"
      ? "#/settings/collections"
      : r.settings?.page === "collection"
        ? `#/settings/collection/${seg(r.settings.kind)}`
        : "#/settings"
  const s = r.selection
  let base: string
  switch (s?.kind) {
    case "list":
      base = `#/list/${seg(s.id)}`
      break
    case "inbox":
      base = `#/none/${seg(s.collection)}`
      break
    case "missing":
      base = `#/missing/${seg(s.collection)}`
      break
    case "suggestions":
      base = "#/suggestions"
      break
    case "noDate":
      base = "#/nodate"
      break
    case "needs":
      base = "#/needs"
      break
    default:
      base = "#/lists"
  }
  return s && r.task ? `${base}/${seg(r.task)}` : base
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

/** Where a selection lives: Needs you's screens under Needs you, the
 * groups under Lists. */
export function sectionOf(s: Selection): Section {
  return s.kind === "needs" || s.kind === "suggestions" || s.kind === "noDate"
    ? "needs"
    : "lists"
}

/** A section chosen: Needs you shows its rows; Lists keeps the group shown
 * wide, and shows the lists themselves on a phone. */
export function selectSection(section: Section, wide: boolean) {
  const r = currentRoute()
  if (section === r.section && !r.task && !r.settings) {
    if (section === "lists" && !wide && r.selection) go({ section })
    return
  }
  if (section === "needs") go({ section, selection: { kind: "needs" } })
  else if (section === "lists")
    go({
      section,
      selection:
        wide && r.selection && sectionOf(r.selection) === "lists"
          ? r.selection
          : undefined,
    })
  else go({ section })
}

/** A list shown, or none: on a phone, back to the lists. */
export function show(selection: Selection | undefined) {
  if (selection) go({ section: sectionOf(selection), selection })
  else go({ section: "lists" })
}

export function openTask(id: string | undefined) {
  const r = currentRoute()
  go({ ...r, task: id })
}

export function openSettings(page?: SettingsPage) {
  go({ section: "settings", settings: page })
}

// ---- Needs you narrowed to one list or the inbox ------------------------------

export const filterStore = store<Selection | undefined>(undefined)

// ---- what is open over the screens --------------------------------------------

/** What an editor opened on: a task (as it stood then, by its path), or a
 * new task in the list it names (by its path), due on the day it names. */
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
