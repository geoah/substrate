import { positionAfter, uuidv7 } from "@substrate/client"
import {
  derivedUUID,
  useKindWrites,
  useWrites,
  type KindWrites,
  type Writes,
} from "@substrate/react"
import { useMemo } from "react"
import {
  List,
  Task,
  addDays,
  atTime,
  civilDay,
  inSelection,
  isOpen,
  nextTaskPosition,
  sameDue,
  sortTasks,
  timestamp,
  type Due,
  type Priority,
  type Selection,
  type ListFields,
  type TaskFields,
  type TaskItem,
  type TaskList,
} from "./model"
import { moves as positionMoves } from "./position"
import {
  collectionKind,
  collectionRecordOf,
  command,
  ownTaskKind,
  preferenceKind,
  referenceValue,
  type ActionName,
  type Collection,
} from "./sources"
import {
  currentRoute,
  deletingListStore,
  filterStore,
  go,
  linkKindsStore,
  openEditor,
  settingsStore,
  show,
  store,
  useStore,
  useTasks,
  type TasksModel,
} from "./state"

/** How a draft is due: not at all, on a day, or at a moment. */
export type DueKind = "none" | "day" | "time"

export const dueKinds: { value: DueKind; label: string }[] = [
  { value: "none", label: "No Due Date" },
  { value: "day", label: "On a Day" },
  { value: "time", label: "At a Time" },
]

/** A task as the editor holds it while the owner types (TaskDraft). */
export interface TaskDraft {
  name: string
  notes: string
  link: string
  /** The list's record path. */
  list?: string
  dueKind: DueKind
  day?: string
  at?: Date
  priority: Priority
}

export function draftOf(t: TaskItem): TaskDraft {
  return {
    name: t.name,
    notes: t.description ?? "",
    link: t.url ?? "",
    list: t.list,
    dueKind: !t.due ? "none" : t.due.at ? "time" : "day",
    day: t.due?.day,
    at: t.due?.at,
    priority: t.priority,
  }
}

/** When a draft is due, as the task will hold it. */
export function draftDue(d: TaskDraft): Due | undefined {
  if (d.dueKind === "none" || !d.day) return undefined
  if (d.dueKind === "day") return { day: d.day }
  return d.at ? { day: civilDay(d.at), at: d.at } : undefined
}

/** Where a task that needs the owner can be moved to from its row. */
export type Reschedule = "today" | "tomorrow" | "nextWeek" | "pickDay"

/** How long a decision stays on its row, taken, before the write goes:
 * long enough to see it and take it back. */
export const undoWindow = 3000

/** The decisions that wait out the window before the outbox has them. */
export type Undoable = Exclude<ActionName, "reopen">

/** A decision in its window: what it was, the value it writes, and when
 * the window closes. */
export interface Pending {
  action: Undoable
  value: string
  until: number
}

/** Each task with a decision in its window, by its path. */
export const pendingStore = store<ReadonlyMap<string, Pending>>(new Map())
/** Each window's write, which its timer or the page closing sends. */
const windows = new Map<string, { timer: number; write: () => void }>()

/** The decision whose Undo the bar offers: the latest taken. */
export const lastPendingStore = store<string | undefined>(undefined)

export function usePending(): ReadonlyMap<string, Pending> {
  return useStore(pendingStore)
}

/** Each task marked done in its window, with when the window closes: what
 * the round mark's ring drains over. */
export function useCompleting(): ReadonlyMap<string, number> {
  const pending = usePending()
  return useMemo(() => {
    const out = new Map<string, number>()
    for (const [path, p] of pending)
      if (p.action === "complete") out.set(path, p.until)
    return out
  }, [pending])
}

function settle(path: string) {
  const w = windows.get(path)
  if (w) clearTimeout(w.timer)
  windows.delete(path)
  pendingStore.set((m) => {
    if (!m.has(path)) return m
    const next = new Map(m)
    next.delete(path)
    return next
  })
  if (lastPendingStore.get() === path) lastPendingStore.set(undefined)
}

// A page closed within a window still sends what the owner decided.
if (typeof window !== "undefined")
  window.addEventListener("pagehide", () => {
    for (const w of [...windows.values()]) w.write()
  })

/** An action with several values waiting on the owner's choice. */
export interface Choosing {
  path: string
  action: ActionName
  choices: string[]
  edits?: Record<string, unknown>
}

export const choosingStore = store<Choosing | undefined>(undefined)

/** Takes back a decision still in its window: nothing was written. */
export function undo(path: string): boolean {
  if (!windows.has(path)) return false
  settle(path)
  return true
}

/** The kinds the link reads now: a pin of a kind outside them may be of a
 * collection turned off, which hides it and never drops it. */
function readKinds(): readonly string[] {
  return linkKindsStore.get()
}

/** Every write the owner makes. A decision on a task of any collection is
 * the contract's one patch (`command`), sent through the outbox after its
 * undo window; the app's own task's other writes go through its codec, as
 * TasksEnrichment and TaskWrites make them: a create as a PUT on an id the
 * device chose, a change as a patch of what changed, an edit on the
 * version the owner saw, a move of order as `Position.moves` gives, and a
 * plain DELETE. The settings records are created on their derived ids and
 * patched once there. What the server refuses waits in Settings. */
export function useTaskActions() {
  const tasks = useKindWrites(Task)
  const lists = useKindWrites(List)
  const writes = useWrites()
  const model = useTasks()
  return useMemo(
    () => makeActions(model, tasks, lists, writes),
    [tasks, lists, writes, model]
  )
}

/** The actions over one model and the kit's writes; `useTaskActions` is
 * this under the open repository. */
export function makeActions(
  model: TasksModel,
  tasks: KindWrites<unknown, TaskFields>,
  lists: KindWrites<unknown, ListFields>,
  writes: Writes
) {
  const task = (id: string) => model.task(id)
  /** A change to the app's own task through its codec. */
  const change = (t: TaskItem, fields: Partial<TaskFields>) => {
    if (t.own) void tasks.change(t.recordID, fields)
  }
  const writable = (t: TaskItem) => !t.item.collection.viewOnly

  /** A settings record's properties merged in: a patch of the record the
   * repository holds, else a create on the id every device derives,
   * followed by the same patch, so a device that lost the race to create
   * it writes its choice over the winner's row. */
  const settings = async (
    kind: string,
    derivedFrom: string,
    held: { id: string } | undefined,
    create: Record<string, unknown>,
    patch: Record<string, unknown>
  ) => {
    if (held) {
      await writes.change(kind, held.id, patch)
      return
    }
    const id = await derivedUUID(derivedFrom)
    const made = await writes.create(kind, id, { ...create, ...patch })
    if (made.kept) await writes.change(kind, id, patch)
  }

  const actions = {
    /** A decision on a task: accept, dismiss, complete or won't do wait
     * out the undo window, then write; reopen writes at once. Several
     * values and no `to` asks the owner which. */
    act(
      path: string,
      action: ActionName,
      opts: { to?: string; edits?: Record<string, unknown> } = {}
    ) {
      const t = task(path)
      if (!t || windows.has(path)) return
      const result = command(t.item, { action, path, ...opts })
      if (result.type === "none") return
      if (result.type === "choose") {
        choosingStore.set({
          path,
          action,
          choices: result.choices,
          edits: opts.edits,
        })
        return
      }
      const { write } = result
      const send = () =>
        void writes.change(write.kind, write.id, write.properties)
      if (action === "reopen") return send()
      const fire = () => {
        settle(path)
        send()
      }
      windows.set(path, {
        timer: window.setTimeout(fire, undoWindow),
        write: fire,
      })
      if (action === "complete") navigator.vibrate?.(8)
      pendingStore.set((m) =>
        new Map(m).set(path, {
          action,
          value: result.value,
          until: Date.now() + undoWindow,
        })
      )
      lastPendingStore.set(path)
    },
    /** One decision over many suggestions; one with several values takes
     * its first, as the queue's Select offers one decision for all. */
    actAll(paths: string[], action: "accept" | "dismiss") {
      for (const path of paths) {
        const t = task(path)
        const values = t?.item.actions[action] ?? []
        if (values.length) actions.act(path, action, { to: values[0] })
      }
    },
    complete(id: string) {
      actions.act(id, "complete")
    },
    /** Reopen, or within the window, the mark taken back. */
    reopen(id: string) {
      if (undo(id)) return
      actions.act(id, "reopen")
    },
    abandon(id: string) {
      undo(id)
      actions.act(id, "wontDo")
    },
    /** A task's due day moved to a named day, or the editor to pick one. */
    reschedule(id: string, how: Reschedule) {
      const t = task(id)
      if (!t || !writable(t)) return
      if (how === "pickDay") {
        if (t.own) openEditor({ kind: "task", id })
        else go({ ...currentRoute(), task: id })
        return
      }
      const today = civilDay(new Date())
      const day =
        how === "today"
          ? today
          : how === "tomorrow"
            ? addDays(today, 1)
            : addDays(today, 7)
      // A timed task keeps its time of day on the new day.
      const at = t.due?.at
      const due: Due = at
        ? { day, at: atTime(day, at.getHours(), at.getMinutes()) }
        : { day }
      if (sameDue(due, t.due)) return
      if (t.own) change(t, { due })
      else actions.setDue(id, due)
    },
    /** A due date on another collection's task, through its readings: the
     * day where it reads one, the moment where it reads one (a day alone,
     * where it has no day, at the day's start never: it is left unset). */
    setDue(id: string, due: Due | undefined) {
      const t = task(id)
      if (!t || !writable(t)) return
      if (t.own) return change(t, { due: due ?? null })
      const c = t.item.collection
      const props: Record<string, unknown> = {}
      if (c.dueDay) props[c.dueDay] = due ? due.day : null
      if (c.due)
        props[c.due] = due?.at
          ? timestamp(due.at)
          : !due || c.dueDay
            ? null
            : undefined
      for (const k of Object.keys(props))
        if (props[k] === undefined) delete props[k]
      if (Object.keys(props).length)
        void writes.change(t.kind, t.recordID, props)
    },
    setPriority(id: string, priority: Priority) {
      const t = task(id)
      if (!t || !t.own || t.priority === priority) return
      change(t, { priority })
    },
    /** Another collection's priority, by one of its declared values. */
    setPriorityValue(id: string, value: string | null) {
      const t = task(id)
      const c = t?.item.collection
      if (!t || !c?.priority || !writable(t)) return
      void writes.change(t.kind, t.recordID, { [c.priority]: value })
    },
    /** Into another group: the app's own task rewrites `list` and
     * `position` in one patch, after the last of its new list; another's
     * its group reference. */
    move(id: string, list: string | undefined) {
      const t = task(id)
      if (!t || !writable(t) || t.list === list) return
      if (t.own) {
        if (list && !model.list(list)) return
        return change(t, {
          list: list ?? null,
          position: nextTaskPosition(model.tasks, list),
        })
      }
      const group = t.item.collection.group
      if (!group) return
      void writes.change(t.kind, t.recordID, { [group.property]: list ?? null })
    },
    deleteTask(id: string) {
      undo(id)
      const t = task(id)
      if (!t || !writable(t)) return
      void writes.remove(t.kind, t.recordID)
      const r = currentRoute()
      if (r.task === id) go({ ...r, task: undefined }, true)
    },
    /** A task dragged to `index` among a list's open rows, in my order. */
    reorderTask(id: string, index: number, selection: Selection) {
      if (selection.kind !== "list" && selection.kind !== "inbox") return
      if (settingsStore.get().order !== "manual") return
      const open = sortTasks(
        model.tasks.filter(
          (t) => t.own && isOpen(t) && inSelection(t, selection)
        ),
        "manual"
      )
      const changes = positionMoves(
        id,
        index,
        open.map((t) => t.id),
        Object.fromEntries(open.map((t) => [t.id, t.position]))
      )
      for (const [moved, position] of Object.entries(changes)) {
        const t = task(moved)
        if (t && t.position !== position) change(t, { position })
      }
    },
    reorderList(id: string, index: number) {
      const changes = positionMoves(
        id,
        index,
        model.lists.map((l) => l.id),
        Object.fromEntries(model.lists.map((l) => [l.id, l.position]))
      )
      for (const [moved, position] of Object.entries(changes)) {
        const l = model.list(moved)
        if (l && l.position !== position)
          void lists.change(recordID(l.id), { position })
      }
    },

    /** On Needs you a task goes into the list it is narrowed to and is
     * due today, so it lands where the owner is looking. */
    quickAdd(name: string) {
      const trimmed = name.trim()
      if (!trimmed) return
      // Another collection new tasks go to takes its title alone: it is
      // creatable only where every property it requires is one of those.
      const target = model.collection(model.sources.newTasks)
      if (target && target.kind !== ownTaskKind) {
        if (target.creatable && target.title)
          void writes.create(target.kind, uuidv7(), { [target.title]: trimmed })
        return
      }
      const { list, day } = newTaskSeed(model)
      void tasks.create({
        name: trimmed,
        position: nextTaskPosition(model.tasks, list),
        list,
        due: day ? { day } : undefined,
      })
    },
    newTask() {
      openEditor({ kind: "new", ...newTaskSeed(model) })
    },
    edit(id: string) {
      const t = task(id)
      if (t?.own) openEditor({ kind: "task", id })
    },

    /** An editor's save: a create in the list chosen after its last task,
     * or a patch of the fields the owner changed on the version they saw,
     * a moved task put after the last of its new list. Resolves whether
     * the write was kept. */
    async save(
      seen: TaskItem | undefined,
      d: TaskDraft,
      notKept: (words: string) => void
    ): Promise<boolean> {
      const opts = { onNotKept: notKept }
      const name = d.name.trim()
      const notes = d.notes.trim() ? d.notes : null
      const link = d.link.trim() || null
      const due = draftDue(d) ?? null
      if (!seen) {
        const written = await tasks.create(
          {
            name,
            notes,
            link,
            list: d.list,
            position: nextTaskPosition(model.tasks, d.list),
            due,
            priority: d.priority === "none" ? undefined : d.priority,
          },
          opts
        )
        return written.kept
      }
      const was = draftOf(seen)
      const fields: Partial<TaskFields> = {}
      if (d.name !== was.name) fields.name = name
      if (d.notes !== was.notes) fields.notes = notes
      if (d.link !== was.link) fields.link = link
      if (d.list !== was.list) {
        fields.list = d.list ?? null
        fields.position = nextTaskPosition(
          model.tasks.filter((t) => t.id !== seen.id),
          d.list
        )
      }
      if (!sameDue(due ?? undefined, draftDue(was))) fields.due = due
      if (d.priority !== was.priority) fields.priority = d.priority
      if (!Object.keys(fields).length) return true
      const written = await tasks.edit(
        seen.recordID,
        fields,
        seen.version,
        opts
      )
      return written.kept
    },

    createList(name: string) {
      const trimmed = name.trim()
      if (!trimmed) return
      void lists
        .create({
          name: trimmed,
          position: positionAfter(model.lists.map((l) => l.position)),
        })
        .then((w) => w.kept && show({ kind: "list", id: List.ref(w.id) }))
    },
    renameList(id: string, name: string) {
      const l = model.list(id)
      const trimmed = name.trim()
      if (!l || !trimmed || trimmed === l.name) return
      void lists.change(recordID(id), { name: trimmed })
    },
    /** Asks first: the list owns its tasks. */
    deleteList(id: string) {
      if (model.lists.some((l) => l.id === id)) deletingListStore.set(id)
    },
    /** The answer: the list goes, and the server's cascade takes its tasks;
     * the list closes, back to the lists on a phone, No list wider. */
    confirmDeleteList(wide: boolean) {
      const id = deletingListStore.get()
      deletingListStore.set(undefined)
      const l = id ? model.lists.find((x) => x.id === id) : undefined
      if (!l) return
      void lists.remove(recordID(l.id))
      const r = currentRoute()
      if (r.selection?.kind === "list" && r.selection.id === l.id)
        go(
          r.section === "lists" && !wide
            ? { section: "lists" }
            : {
                section: "lists",
                selection: { kind: "inbox", collection: ownTaskKind },
              },
          true
        )
      const filter = filterStore.get()
      if (filter?.kind === "list" && filter.id === l.id)
        filterStore.set(undefined)
    },

    // ---- the settings records --------------------------------------------

    /** The owner's choice about one collection: on or off, what it groups
     * by, or which property a reading uses (`-` for none). */
    setCollection(kind: string, patch: Record<string, unknown>) {
      const all = [...model.sources.held.values()]
      const held = collectionRecordOf(all, kind)
      void settings(collectionKind, `collection:${kind}`, held, { kind }, patch)
    },
    setPreference(patch: Record<string, unknown>) {
      void settings(
        preferenceKind,
        "preference",
        model.sources.preference,
        {},
        patch
      )
    },
    /** The pins with `path` added or taken out. A pin whose record is gone
     * from a kind the app reads is dropped with this write; one of a kind
     * it does not read now stays, hidden. */
    pin(path: string, on: boolean) {
      const raw = model.sources.preference?.properties.pins
      const was = (Array.isArray(raw) ? raw : [])
        .map(referenceValue)
        .filter((p): p is string => !!p)
      const kinds = readKinds()
      const kept = was.filter((p) => {
        if (p === path) return false
        const kind = p.split("/").slice(0, 3).join("/")
        return !kinds.includes(kind) || model.sources.held.has(p)
      })
      actions.setPreference({ pins: on ? [...kept, path] : kept })
    },
  }
  return actions
}

export type TaskActions = ReturnType<typeof useTaskActions>

/** A record path's id: what follows its kind's three segments. */
export function recordID(path: string): string {
  return path.split("/").slice(3).join("/")
}

/** Where a new task goes from where the owner is: Needs you's narrowed
 * list and today, or the list shown, where it is one of the app's own. */
function newTaskSeed(model: TasksModel): { list?: string; day?: string } {
  const r = currentRoute()
  const own = (id: string | undefined) =>
    id && model.lists.some((l) => l.id === id) ? id : undefined
  if (r.section !== "lists" || !r.selection) {
    const filter = filterStore.get()
    return {
      list: own(filter?.kind === "list" ? filter.id : undefined),
      day: r.selection?.kind === "noDate" ? undefined : civilDay(new Date()),
    }
  }
  return { list: own(r.selection.kind === "list" ? r.selection.id : undefined) }
}

export function listName(lists: TaskList[], id: string | undefined) {
  return id ? lists.find((l) => l.id === id)?.name : undefined
}

/** The scopes the app asks for beyond its first approval: write access to
 * every collection that is on, and read access to each group kind one
 * groups by that the grant does not read yet. */
export function extraScopes(collections: Collection[]): string[] {
  const out = new Set<string>()
  for (const c of collections) {
    if (!c.enabled) continue
    if (c.kind !== ownTaskKind) out.add(`write:${c.kind}`)
    const chosen = c.record?.properties.group
    const ref =
      typeof chosen === "string"
        ? c.references.find((r) => r.property === chosen)
        : undefined
    if (ref && !ref.readable) out.add(`read:${ref.kind}`)
  }
  return [...out].sort()
}
