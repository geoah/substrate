import { positionAfter, uuidv7 } from "@substrate/client"
import {
  derivedUUID,
  isHeld,
  useKindWrites,
  useOutbox,
  useSubstrate,
  useToast,
  useWrites,
  type KindWrites,
  type OutboxEntry,
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
  propertyLabel,
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
  useTasks,
  type TasksModel,
} from "./state"
import { words } from "./words"

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

/** The decisions that wait out the window before they are sent. */
export type Undoable = Exclude<ActionName, "reopen">

const undoable: readonly string[] = ["accept", "dismiss", "complete", "wontDo"]

/** A decision in its window, as the outbox keeps it: what it was, when the
 * window closes, and the outbox entry Undo withdraws. It is kept there the
 * moment it is made, so a closed page or a crash loses nothing: the outbox
 * sends it once its moment passes, at the next open if the page was
 * closed, and offline whenever the network is back. */
export interface Pending {
  action: Undoable
  until: number
  entry: string
}

/** The decisions still in their window, by task path: the outbox's held
 * writes the app tagged with a decision. */
export function pendingOf(
  entries: readonly OutboxEntry[],
  now = Date.now()
): Map<string, Pending> {
  const out = new Map<string, Pending>()
  for (const e of entries)
    if (e.tag && undoable.includes(e.tag) && isHeld(e, now))
      out.set(`${e.kind}/${e.recordID}`, {
        action: e.tag as Undoable,
        until: e.holdUntil!,
        entry: e.id,
      })
  return out
}

/** The decisions in their window, read again whenever the outbox moves,
 * which it does as each window closes. */
export function usePending(): ReadonlyMap<string, Pending> {
  const outbox = useOutbox()
  return useMemo(() => pendingOf(outbox.entries), [outbox])
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

/** What the actions read and take back of the decisions in their window. */
export interface Decisions {
  pending: () => ReadonlyMap<string, Pending>
  withdraw: (entry: string) => Promise<boolean>
}

/** An action with several values waiting on the owner's choice. */
export interface Choosing {
  path: string
  action: ActionName
  choices: string[]
  edits?: Record<string, unknown>
}

export const choosingStore = store<Choosing | undefined>(undefined)

/** Decisions made and not yet kept by the outbox: a second press in that
 * moment is not a second decision. */
const deciding = new Set<string>()

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
  const { outbox } = useSubstrate()
  const toast = useToast()
  return useMemo(
    () =>
      makeActions({
        model,
        tasks,
        lists,
        writes,
        decisions: {
          pending: () => pendingOf(outbox.getSnapshot().entries),
          withdraw: (entry) => outbox.withdraw(entry),
        },
        refuse: (title, why) => toast(`${title}: ${why}`, "error"),
      }),
    [tasks, lists, writes, model, outbox, toast]
  )
}

/** The actions over one model, the kit's writes and the outbox's held
 * decisions; `useTaskActions` is this under the open repository. */
export function makeActions(deps: {
  model: TasksModel
  tasks: KindWrites<unknown, TaskFields>
  lists: KindWrites<unknown, ListFields>
  writes: Writes
  decisions: Decisions
  /** Says why a write was not made, where no form stands to say it. */
  refuse: (title: string, why: string) => void
  /** The undo window, in ms; the app's own unless a test says. */
  undoWindow?: number
}) {
  const { model, tasks, lists, writes, decisions } = deps
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
    const id = derivedUUID(derivedFrom)
    const made = await writes.create(kind, id, { ...create, ...patch })
    if (made.kept) await writes.change(kind, id, patch)
  }

  const actions = {
    /** A decision on a task: the contract's one patch, an edit on the
     * version the owner saw, so a change another device made meanwhile
     * parks it as a conflict rather than being written over. Accept,
     * dismiss, complete and won't do are kept held through the undo
     * window and sent after it; reopen is sent at once. Several values and
     * no `to` asks the owner which. */
    act(
      path: string,
      action: ActionName,
      opts: { to?: string; edits?: Record<string, unknown> } = {}
    ) {
      const t = task(path)
      if (!t || deciding.has(path) || decisions.pending().has(path)) return
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
      const edit = (hold?: { until: number; tag: string }) =>
        writes.edit(
          write.kind,
          write.id,
          write.properties,
          write.ifVersion,
          hold ? { hold } : undefined
        )
      if (action === "reopen") {
        void edit()
        return
      }
      deciding.add(path)
      const until = Date.now() + (deps.undoWindow ?? undoWindow)
      void edit({ until, tag: action }).finally(() => deciding.delete(path))
      if (action === "complete") navigator.vibrate?.(8)
    },
    /** Takes back a decision still in its window: nothing is sent. Resolves
     * whether it was taken back; undefined where none waits. */
    undo(path: string): Promise<boolean> | undefined {
      const p = decisions.pending().get(path)
      return p ? decisions.withdraw(p.entry) : undefined
    },
    /** The latest decision still in its window, taken back. */
    undoLatest(): boolean {
      let latest: [string, Pending] | undefined
      for (const entry of decisions.pending())
        if (!latest || entry[1].until > latest[1].until) latest = entry
      if (!latest) return false
      void decisions.withdraw(latest[1].entry)
      return true
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
      if (actions.undo(id)) return
      actions.act(id, "reopen")
    },
    abandon(id: string) {
      const taken = actions.undo(id)
      if (taken) void taken.then(() => actions.act(id, "wontDo"))
      else actions.act(id, "wontDo")
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
      void actions.undo(id)
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
      const { list, day } = newTaskSeed(model)
      const target = newTaskTarget(model)
      if ("refusal" in target)
        return deps.refuse(words.create.refused, target.refusal)
      if (target.collection.kind !== ownTaskKind) {
        const made = encodeNew(target.collection, {
          name: trimmed,
          list,
          due: day ? { day } : undefined,
        })
        if ("refusal" in made)
          return deps.refuse(words.create.refused, made.refusal)
        void writes.create(target.collection.kind, uuidv7(), made.properties)
        return
      }
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
      notKept: (words: string, title?: string) => void
    ): Promise<boolean> {
      const opts = { onNotKept: (w: string) => notKept(w) }
      const name = d.name.trim()
      const notes = d.notes.trim() ? d.notes : null
      const link = d.link.trim() || null
      const due = draftDue(d) ?? null
      if (!seen) {
        const target = newTaskTarget(model)
        if ("refusal" in target) {
          notKept(target.refusal, words.create.refused)
          return false
        }
        if (target.collection.kind !== ownTaskKind) {
          const made = encodeNew(target.collection, {
            name,
            notes,
            link,
            list: d.list,
            due: due ?? undefined,
            priority: d.priority === "none" ? undefined : d.priority,
          })
          if ("refusal" in made) {
            notKept(made.refusal, words.create.refused)
            return false
          }
          const written = await writes.create(
            target.collection.kind,
            uuidv7(),
            made.properties,
            opts
          )
          return written.kept
        }
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

/** The scopes every sign-in again asks for beyond the app's request: those
 * the token already holds, so approving more never drops what an earlier
 * approval gave. Nothing is inferred from the settings records, which
 * another device may have written: a scope is asked for only by the
 * owner's own action (an Approve, through `signInAgain`). */
export function heldScopes(
  held: readonly string[],
  requested: readonly string[]
): string[] {
  return held.filter((s) => !requested.includes(s)).sort()
}

/** Where a new task is written: the collection "Save new tasks to" names,
 * else the app's own task; or why a task cannot be added there. */
export function newTaskTarget(
  model: TasksModel
): { collection: Collection } | { refusal: string } {
  const kind = model.sources.newTasks
  const c = model.collection(kind)
  if (!c) return { refusal: `Substrate Tasks does not read ${kind}.` }
  const name = c.label.plural
  if (!c.enabled) return { refusal: `${name} is turned off in Collections.` }
  if (c.viewOnly)
    return { refusal: `Substrate Tasks can't change your ${name}.` }
  if (!c.title)
    return { refusal: `${name} has no title Substrate Tasks writes.` }
  const missing = required(c).filter((p) => !edited(c).has(p))
  if (missing.length) return { refusal: needs(c, missing) }
  return { collection: c }
}

function required(c: Collection): string[] {
  const props = (c.declaration.definition.properties ?? {}) as Record<
    string,
    { required?: unknown }
  >
  return Object.entries(props)
    .filter(([, spec]) => spec.required === true)
    .map(([p]) => p)
}

function edited(c: Collection): Set<string> {
  return new Set(
    [c.title, c.due, c.dueDay, c.priority, c.order, c.group?.property].filter(
      (p): p is string => !!p
    )
  )
}

function needs(c: Collection, props: string[]): string {
  const labels = props.map((p) =>
    propertyLabel(c.declaration.definition, p).toLowerCase()
  )
  return `${c.label.plural} needs ${labels.join(" and ")}, which Substrate Tasks does not fill in.`
}

/** A new task's fields as another collection's properties, through its
 * readings: the title, the due day and moment (a day alone, where it reads
 * no due day, is left out rather than made midnight), the priority where
 * it is one of its values, the group where it is of its group kind, the
 * notes and link where it declares them. Refused where a property it
 * requires is left empty. */
export function encodeNew(
  c: Collection,
  f: {
    name: string
    notes?: string | null
    link?: string | null
    list?: string
    due?: Due
    priority?: string
  }
): { properties: Record<string, unknown> } | { refusal: string } {
  const props = (c.declaration.definition.properties ?? {}) as Record<
    string,
    { type?: unknown }
  >
  const typed = (p: string, types: string[]) =>
    typeof props[p]?.type === "string" && types.includes(props[p].type)
  const out: Record<string, unknown> = {}
  if (c.title) out[c.title] = f.name
  if (f.notes && typed("description", ["string", "markdown"]))
    out.description = f.notes
  if (f.link && typed("url", ["url", "string"])) out.url = f.link
  if (f.due) {
    if (c.dueDay) out[c.dueDay] = f.due.day
    if (c.due && f.due.at) out[c.due] = timestamp(f.due.at)
  }
  if (f.priority && c.priority && c.priorities.includes(f.priority))
    out[c.priority] = f.priority
  if (f.list && c.group && f.list.startsWith(`${c.group.kind}/`))
    out[c.group.property] = f.list
  const empty = required(c).filter(
    (p) => out[p] === undefined || out[p] === null || out[p] === ""
  )
  if (empty.length) return { refusal: needs(c, empty) }
  return { properties: out }
}
