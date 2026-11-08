import { LargeTitle, LinkStatus, SidebarItem } from "@substrate/react"
import {
  CalendarDays,
  Inbox,
  ListIcon,
  PlusCircle,
  Settings,
} from "lucide-react"
import { useState } from "react"
import { useTaskActions } from "./actions"
import { byPosition, inSelection, isOpen, type Selection } from "./model"
import { sameSelection, useTasks, type Section } from "./state"
import { ListNameSheet } from "./TaskListScreen"

/** The App menu: Today and No List, the owner's lists in their order with
 * their open counts, Settings, and New List (the kit's AppMenu). */
export function ListsMenu(props: {
  current: Section
  today: string
  sidebar: boolean
  onSelect: (s: Section) => void
}) {
  const model = useTasks()
  const actions = useTaskActions()
  const [creating, setCreating] = useState(false)
  const count = (s: Selection) =>
    model.tasks.filter((t) => isOpen(t) && inSelection(t, s, props.today))
      .length
  const lists = [...model.lists].sort(byPosition)
  const item = (
    s: Section,
    label: string,
    icon: React.ReactNode,
    n?: number
  ) => (
    <SidebarItem
      key={s.kind === "list" ? s.id : s.kind}
      icon={icon}
      label={label}
      count={n}
      active={props.sidebar && sameSelection(props.current, s)}
      onClick={() => props.onSelect(s)}
    />
  )

  return (
    <div className="flex min-h-full flex-col">
      {props.sidebar ? (
        <div className="flex h-[60px] items-center px-5 pt-2 text-[17px] font-semibold">
          Lists
        </div>
      ) : (
        <>
          <div className="h-[60px]" />
          <LargeTitle>Lists</LargeTitle>
          <LinkStatus status={model.status} words={model.words} />
        </>
      )}
      <nav className="flex-1 space-y-0.5 px-3 pb-32">
        {props.sidebar &&
          item(
            { kind: "today" },
            "Today",
            <CalendarDays className="size-5" />,
            count({ kind: "today" })
          )}
        {item(
          { kind: "inbox" },
          "No List",
          <Inbox className="size-5" />,
          count({ kind: "inbox" })
        )}
        <div className="px-3 pt-5 pb-1 text-[15px] font-semibold text-secondary">
          My Lists
        </div>
        {lists.map((l) =>
          item(
            { kind: "list", id: l.id },
            l.name,
            <ListIcon className="size-5" />,
            count({ kind: "list", id: l.id })
          )
        )}
        {lists.length === 0 && model.status === "live" && (
          <p className="px-3 py-2 text-[15px] text-secondary">No lists yet.</p>
        )}
        <button
          type="button"
          onClick={() => setCreating(true)}
          className="flex h-11 w-full items-center gap-3 rounded-[10px] px-3 text-[17px] text-accent-text active:bg-selected"
        >
          <PlusCircle className="size-5" />
          New List
        </button>
        {props.sidebar && (
          <div className="pt-4">
            {item(
              { kind: "settings" },
              "Settings",
              <Settings className="size-5" />
            )}
          </div>
        )}
      </nav>
      {creating && (
        <ListNameSheet
          title="New List"
          onClose={() => setCreating(false)}
          onSave={async (name) => {
            const r = await actions.createList(name)
            if (r) props.onSelect({ kind: "list", id: r.id })
            return !!r
          }}
        />
      )}
    </div>
  )
}
