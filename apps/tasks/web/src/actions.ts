import { positionAfter, uuidv7 } from "@substrate/client"
import { useWrites } from "@substrate/react"
import { useMemo } from "react"
import {
  Kinds,
  dueProperties,
  listValue,
  nextTaskPosition,
  type Due,
  type Priority,
  type TaskItem,
  type TaskList,
} from "./model"
import { useTasks } from "./state"

export interface TaskDraft {
  name: string
  description: string
  url: string
  list?: string
  due?: Due
  priority: Priority
}

export function draftOf(t: TaskItem): TaskDraft {
  return {
    name: t.name,
    description: t.description ?? "",
    url: t.url ?? "",
    list: t.list,
    due: t.due,
    priority: t.priority,
  }
}

function sameDue(a?: Due, b?: Due): boolean {
  if (!a || !b) return a === b
  return (
    a.day === b.day && (a.at?.getTime() ?? null) === (b.at?.getTime() ?? null)
  )
}

/** Every write the owner makes, as TaskWrites makes them: a create as a PUT
 * on an id the device chose, a change as a patch of what changed, an edit on
 * the version the owner saw, a state only by patch, and a plain DELETE. */
export function useTaskActions() {
  const writes = useWrites()
  const model = useTasks()
  return useMemo(() => {
    const patch = (
      t: TaskItem,
      properties: Record<string, unknown>,
      ifVersion?: number
    ) => writes.patch(Kinds.task, t.id, properties, ifVersion)
    return {
      complete: (t: TaskItem) => patch(t, { status: "done" }),
      // The stamp is written on the way in and nothing removes it on the way
      // out, so a reopen clears it in the same write.
      reopen: (t: TaskItem) => patch(t, { status: "open", completedAt: null }),
      abandon: (t: TaskItem) => patch(t, { status: "abandoned" }),
      setPriority: (t: TaskItem, priority: Priority) => patch(t, { priority }),
      // Moving to another list rewrites `list` and `position` in one patch.
      move: (t: TaskItem, list: string | undefined) =>
        patch(t, {
          list: list ? listValue(list) : null,
          position: nextTaskPosition(
            model.tasks.filter((x) => x.id !== t.id),
            list
          ),
        }),
      remove: (t: TaskItem) => writes.remove(Kinds.task, t.id),

      create: (d: TaskDraft) => {
        const props: Record<string, unknown> = {
          name: d.name.trim(),
          priority: d.priority,
          position: nextTaskPosition(model.tasks, d.list),
        }
        if (d.description.trim()) props.description = d.description
        if (d.url.trim()) props.url = d.url.trim()
        if (d.list) props.list = listValue(d.list)
        if (d.due) Object.assign(props, dueProperties(d.due))
        for (const k of Object.keys(props))
          if (props[k] === null) delete props[k]
        return writes.create(Kinds.task, uuidv7(), props)
      },

      /** Save patches the fields the owner changed, on the version they saw:
       * a change made on another device meanwhile is a conflict, never
       * written over. */
      save: (seen: TaskItem, d: TaskDraft) => {
        const out: Record<string, unknown> = {}
        const was = draftOf(seen)
        if (d.name.trim() !== was.name) out.name = d.name.trim()
        if (d.description !== was.description)
          out.description = d.description.trim() ? d.description : null
        if (d.url.trim() !== was.url) out.url = d.url.trim() || null
        if (d.priority !== was.priority) out.priority = d.priority
        if (d.list !== was.list) {
          out.list = d.list ? listValue(d.list) : null
          out.position = nextTaskPosition(
            model.tasks.filter((x) => x.id !== seen.id),
            d.list
          )
        }
        if (!sameDue(d.due, was.due)) Object.assign(out, dueProperties(d.due))
        if (!Object.keys(out).length) return Promise.resolve(seen.record)
        return patch(seen, out, seen.version)
      },

      createList: (name: string) =>
        writes.create(Kinds.list, uuidv7(), {
          name: name.trim(),
          position: positionAfter(model.lists.map((l) => l.position)),
        }),
      renameList: (l: TaskList, name: string) =>
        writes.patch(Kinds.list, l.id, { name: name.trim() }, l.version),
      // The list owns its tasks: the server's cascade takes them with it.
      removeList: (l: TaskList) => writes.remove(Kinds.list, l.id),
    }
  }, [writes, model])
}
