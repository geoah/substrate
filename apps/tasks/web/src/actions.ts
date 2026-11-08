import { positionAfter } from "@substrate/client"
import { useKindWrites } from "@substrate/react"
import { useMemo } from "react"
import {
  List,
  Task,
  addDays,
  atTime,
  civilDay,
  inSelection,
  isOpen,
  moves,
  nextTaskPosition,
  sameDue,
  sortTasks,
  type Due,
  type Priority,
  type Selection,
  type Status,
  type TaskFields,
  type TaskItem,
  type TaskList,
} from "./model"
import { moves as positionMoves } from "./position"
import {
  currentRoute,
  deletingListStore,
  filterStore,
  go,
  openEditor,
  settingsStore,
  show,
  store,
  useStore,
  useTasks,
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

/** How long a task marked done stays on its row, filled, before the write
 * goes: long enough to see the mark and take it back. */
export const undoWindow = 3000

/** Each task marked done whose undo window is open, with when it closes. */
export const completingStore = store<ReadonlyMap<string, number>>(new Map())
/** Each window's write, which its timer or the page closing sends. */
const windows = new Map<string, { timer: number; write: () => void }>()

export function useCompleting(): ReadonlyMap<string, number> {
  return useStore(completingStore)
}

function settle(id: string) {
  const w = windows.get(id)
  if (w) clearTimeout(w.timer)
  windows.delete(id)
  completingStore.set((m) => {
    const next = new Map(m)
    next.delete(id)
    return next
  })
}

// A page closed within a window still sends what the owner marked.
if (typeof window !== "undefined")
  window.addEventListener("pagehide", () => {
    for (const w of [...windows.values()]) w.write()
  })

/** Every write the owner makes, as TasksEnrichment and TaskWrites make
 * them, through the two kinds' codec: a create as a PUT on an id the device
 * chose, a change as a patch of what changed, an edit on the version the
 * owner saw, a state only by patch, a move of order as the positions
 * `Position.moves` gives, and a plain DELETE. Each is kept in the outbox
 * before it resolves; what the server refuses waits in Settings. */
export function useTaskActions() {
  const tasks = useKindWrites(Task)
  const lists = useKindWrites(List)
  const model = useTasks()
  return useMemo(() => {
    const task = (id: string) => model.task(id)
    const change = (t: TaskItem, fields: Partial<TaskFields>) =>
      void tasks.change(t.id, fields)

    const status = (t: TaskItem, to: Status) => {
      if (moves(t.status, to)) change(t, { status: to })
    }

    /** A completion still in its window is taken back before anything
     * else happens to the task. */
    const undo = (id: string) => {
      if (!windows.has(id)) return false
      settle(id)
      return true
    }

    const actions = {
      /** Marks the task done after the undo window; reopen within it takes
       * the mark back before anything is written. */
      complete(id: string) {
        const t = task(id)
        if (!t || windows.has(id) || !moves(t.status, "done")) return
        const write = () => {
          settle(id)
          const now = task(id)
          if (now) status(now, "done")
        }
        windows.set(id, { timer: window.setTimeout(write, undoWindow), write })
        navigator.vibrate?.(8)
        completingStore.set((m) => new Map(m).set(id, Date.now() + undoWindow))
      },
      reopen(id: string) {
        if (undo(id)) return
        const t = task(id)
        if (t) status(t, "open")
      },
      abandon(id: string) {
        undo(id)
        const t = task(id)
        if (t) status(t, "abandoned")
      },
      /** A task's due day moved to a named day, or the editor to pick one. */
      reschedule(id: string, how: Reschedule) {
        const t = task(id)
        if (!t) return
        if (how === "pickDay") return openEditor({ kind: "task", id })
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
        change(t, { due })
      },
      setPriority(id: string, priority: Priority) {
        const t = task(id)
        if (!t || t.priority === priority) return
        change(t, { priority })
      },
      /** Moving to another list rewrites `list` and `position` in one patch,
       * after the last of its new list. */
      move(id: string, list: string | undefined) {
        const t = task(id)
        if (!t || t.list === list || (list && !model.list(list))) return
        change(t, {
          list: list ?? null,
          position: nextTaskPosition(model.tasks, list),
        })
      },
      deleteTask(id: string) {
        undo(id)
        if (!task(id)) return
        void tasks.remove(id)
        const r = currentRoute()
        if (r.task === id) go({ ...r, task: undefined }, true)
      },
      /** A task dragged to `index` among a list's open rows, in my order. */
      reorderTask(id: string, index: number, selection: Selection) {
        if (selection.kind === "needs") return
        if (settingsStore.get().order !== "manual") return
        const open = sortTasks(
          model.tasks.filter((t) => isOpen(t) && inSelection(t, selection)),
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
            void lists.change(l.id, { position })
        }
      },

      /** On Needs you a task goes into the list it is narrowed to and is
       * due today, so it lands where the owner is looking. */
      quickAdd(name: string) {
        const trimmed = name.trim()
        if (!trimmed) return
        const { list, day } = newTaskSeed()
        void tasks.create({
          name: trimmed,
          position: nextTaskPosition(model.tasks, list),
          list,
          due: day ? { day } : undefined,
        })
      },
      newTask() {
        openEditor({ kind: "new", ...newTaskSeed() })
      },
      edit(id: string) {
        if (task(id)) openEditor({ kind: "task", id })
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
        const written = await tasks.edit(seen.id, fields, seen.version, opts)
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
          .then((w) => w.kept && show({ kind: "list", id: w.id }))
      },
      renameList(id: string, name: string) {
        const l = model.list(id)
        const trimmed = name.trim()
        if (!l || !trimmed || trimmed === l.name) return
        void lists.change(id, { name: trimmed })
      },
      /** Asks first: the list owns its tasks. */
      deleteList(id: string) {
        if (model.list(id)) deletingListStore.set(id)
      },
      /** The answer: the list goes, and the server's cascade takes its tasks;
       * the list closes, back to the lists on a phone, No List wider. */
      confirmDeleteList(wide: boolean) {
        const id = deletingListStore.get()
        deletingListStore.set(undefined)
        const l = id ? model.list(id) : undefined
        if (!l) return
        void lists.remove(l.id)
        const r = currentRoute()
        if (r.selection?.kind === "list" && r.selection.id === l.id)
          go(
            r.section === "lists" && !wide
              ? { section: "lists" }
              : { section: "lists", selection: { kind: "inbox" } },
            true
          )
        const filter = filterStore.get()
        if (filter?.kind === "list" && filter.id === l.id)
          filterStore.set(undefined)
      },
    }
    return actions
  }, [tasks, lists, model])
}

export type TaskActions = ReturnType<typeof useTaskActions>

/** Where a new task goes from where the owner is: Needs you's narrowed
 * list and today, or the list shown. */
function newTaskSeed(): { list?: string; day?: string } {
  const r = currentRoute()
  if (r.section !== "lists" || !r.selection) {
    const filter = filterStore.get()
    return {
      list: filter?.kind === "list" ? filter.id : undefined,
      day: civilDay(new Date()),
    }
  }
  return { list: r.selection.kind === "list" ? r.selection.id : undefined }
}

export function listName(lists: TaskList[], id: string | undefined) {
  return id ? lists.find((l) => l.id === id)?.name : undefined
}
