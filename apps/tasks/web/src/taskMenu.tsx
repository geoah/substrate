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
import {
  fieldLabel,
  inboxTitle,
  listTitle,
  priorities,
  type TaskItem,
  type TaskList,
} from "./model"

/** What a task offers from its row's menu (TaskMenuWords): its state, Edit,
 * its priority, its list, and Delete Task. */
export function taskMenu(
  t: TaskItem,
  pending: boolean,
  lists: TaskList[],
  actions: TaskActions
): MenuItem[] {
  const state: MenuItem[] = pending
    ? [
        {
          label: "Undo",
          icon: <Undo2 className="size-4" />,
          onSelect: () => actions.reopen(t.id),
        },
      ]
    : t.status === "open"
      ? [
          {
            label: "Mark as Done",
            icon: <CircleCheck className="size-4" />,
            onSelect: () => actions.complete(t.id),
          },
          {
            label: "Won't Do",
            icon: <CircleX className="size-4" />,
            onSelect: () => actions.abandon(t.id),
          },
        ]
      : [
          {
            label: "Reopen",
            icon: <Circle className="size-4" />,
            onSelect: () => actions.reopen(t.id),
          },
        ]
  return [
    ...state,
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
    },
    {
      kind: "submenu",
      label: "Move to List",
      icon: <ArrowRightLeft className="size-4" />,
      items: [
        { id: undefined, title: inboxTitle },
        ...lists.map((l) => ({
          id: l.id as string | undefined,
          title: listTitle(l),
        })),
      ].map((l) => ({
        label: l.title,
        checked: t.list === l.id,
        onSelect: () => actions.move(t.id, l.id),
      })),
    },
    { kind: "divider" },
    {
      label: "Delete Task",
      icon: <Trash2 className="size-4" />,
      destructive: true,
      onSelect: () => actions.deleteTask(t.id),
    },
  ]
}
