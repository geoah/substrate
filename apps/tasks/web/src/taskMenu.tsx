import {
  ArrowRightLeft,
  Circle,
  CircleCheck,
  CircleX,
  Flag,
  Pencil,
  Trash2,
  Undo2,
} from "lucide-react"
import type { TaskActions } from "./actions"
import type { MenuItem } from "./Menu"
import { fieldLabel, listTitle, priorities, type TaskItem } from "./model"
import { nameWords, propertyLabel } from "./sources"
import type { TasksModel } from "./state"
import { words } from "./words"

/** What a task offers from its row's menu (TaskMenuWords): its decisions
 * as its collection offers them, Edit for the app's own, its priority, its
 * group, and Delete Task, each only where its collection is writable. */
export function taskMenu(
  t: TaskItem,
  pending: boolean,
  model: TasksModel,
  actions: TaskActions
): MenuItem[] {
  const a = t.item.actions
  const c = t.item.collection
  const state: MenuItem[] = []
  if (pending)
    state.push({
      label: words.undo.action,
      icon: <Undo2 className="size-4" />,
      onSelect: () => actions.reopen(t.id),
    })
  else {
    if (a.accept.length)
      state.push({
        label: words.suggestionRow.accept,
        icon: <CircleCheck className="size-4" />,
        onSelect: () => actions.act(t.id, "accept"),
      })
    if (a.dismiss.length)
      state.push({
        label: words.suggestionRow.dismiss,
        icon: <CircleX className="size-4" />,
        onSelect: () => actions.act(t.id, "dismiss"),
      })
    if (a.complete.length)
      state.push({
        label: "Mark as Done",
        icon: <CircleCheck className="size-4" />,
        onSelect: () => actions.complete(t.id),
      })
    if (a.wontDo.length)
      state.push({
        label: "Won't Do",
        icon: <CircleX className="size-4" />,
        onSelect: () => actions.abandon(t.id),
      })
    if (a.reopen.length)
      state.push({
        label: "Reopen",
        icon: <Circle className="size-4" />,
        onSelect: () => actions.reopen(t.id),
      })
  }
  if (c.viewOnly) return state
  const items: MenuItem[] = [...state]
  if (t.own)
    items.push(
      {
        label: "Edit",
        icon: <Pencil className="size-4" />,
        onSelect: () => actions.edit(t.id),
      },
      {
        kind: "submenu",
        label: fieldLabel.priority,
        icon: <Flag className="size-4" />,
        items: priorities.map((p) => ({
          label: p.label,
          checked: t.priority === p.value,
          onSelect: () => actions.setPriority(t.id, p.value),
        })),
      }
    )
  else if (c.priority)
    items.push({
      kind: "submenu",
      label: propertyLabel(c.declaration.definition, c.priority),
      icon: <Flag className="size-4" />,
      items: c.priorities.map((v) => ({
        label: nameWords(v),
        checked: t.item.priority === v,
        onSelect: () => actions.setPriorityValue(t.id, v),
      })),
    })
  if (c.group) {
    const group = c.group
    const reference = propertyLabel(c.declaration.definition, group.property)
    const choices = [...model.groups.values()].filter((l) =>
      l.id.startsWith(`${group.kind}/`)
    )
    items.push({
      kind: "submenu",
      label: t.own ? "Move to List" : `Move to ${reference}`,
      icon: <ArrowRightLeft className="size-4" />,
      items: [
        {
          id: undefined as string | undefined,
          title: words.lists.none(reference),
        },
        ...choices.map((l) => ({ id: l.id, title: listTitle(l) })),
      ].map((l) => ({
        label: l.title,
        checked: t.list === l.id && t.item.group !== "missing",
        onSelect: () => actions.move(t.id, l.id),
      })),
    })
  }
  items.push(
    { kind: "divider" },
    {
      label: "Delete Task",
      icon: <Trash2 className="size-4" />,
      destructive: true,
      onSelect: () => actions.deleteTask(t.id),
    }
  )
  return items
}
