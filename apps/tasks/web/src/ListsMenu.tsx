import { LargeTitle, LinkStatus, cx } from "@substrate/react"
import {
  ChevronRight,
  CircleCheck,
  FolderPlus,
  Inbox,
  ListIcon,
  Pencil,
  Settings,
  Trash2,
} from "lucide-react"
import { useState, type ReactNode } from "react"
import { useTaskActions } from "./actions"
import { Menu, type MenuItem } from "./Menu"
import { Notices } from "./Notices"
import { inboxTitle, listTitle, sameSelection, type Selection } from "./model"
import { openCount } from "./present"
import { useReorder } from "./reorder"
import { namingStore, useTasks, type Section } from "./state"

/** The lists column: what needs the owner and the tasks in no list, then
 * the lists in the owner's order with their open counts, dragged to
 * reorder, each with Rename… and Delete… in its menu. Beside the list
 * (`sidebar`) it starts with Needs you and ends in Settings; on a phone,
 * those are tabs of their own. */
export function ListsMenu(props: {
  section: Section
  selection?: Selection
  now: Date
  sidebar: boolean
  onSection: (s: Section) => void
  onSelect: (s: Selection) => void
}) {
  const model = useTasks()
  const actions = useTaskActions()
  const [menu, setMenu] = useState<{
    id: string
    at: { x: number; y: number }
  }>()
  const [spoken, setSpoken] = useState("")
  const reorder = useReorder({
    ids: model.lists.map((l) => l.id),
    enabled: true,
    onMove: (id, index) => actions.reorderList(id, index),
    onMenu: (id, at) => setMenu({ id, at }),
    announce: setSpoken,
  })

  const active = (s: Selection) =>
    props.sidebar &&
    props.section !== "settings" &&
    sameSelection(props.selection, s)

  const newList = (
    <button
      type="button"
      onClick={() => namingStore.set({ name: "" })}
      aria-label="New List"
      title="New List"
      className="flex size-11 items-center justify-center rounded-full bg-card/80 shadow-[0_1px_6px_rgb(0_0_0/0.08)] ring-1 ring-separator/40 backdrop-blur-xl active:bg-selected"
    >
      <FolderPlus className="size-5" />
    </button>
  )

  const fixed: { s: Selection; title: string; icon: ReactNode }[] = [
    ...(props.sidebar
      ? [
          {
            s: { kind: "needs" } as Selection,
            title: "Needs you",
            icon: <CircleCheck className="size-5" />,
          },
        ]
      : []),
    {
      s: { kind: "inbox" },
      title: inboxTitle,
      icon: <Inbox className="size-5" />,
    },
  ]

  const menuList = menu && model.list(menu.id)
  const listMenu = (id: string, name: string): MenuItem[] => [
    {
      label: "Rename…",
      icon: <Pencil className="size-4" />,
      onSelect: () => namingStore.set({ id, name }),
    },
    {
      label: "Delete…",
      icon: <Trash2 className="size-4" />,
      destructive: true,
      onSelect: () => actions.deleteList(id),
    },
  ]

  const item = (
    s: Selection,
    title: string,
    icon: ReactNode,
    bind?: ReturnType<typeof reorder.row>
  ) => {
    const { open, words } = openCount(model, s, props.now)
    const key = s.kind === "list" ? s.id : s.kind
    const { ref, style, onKeyDown, ...rest } = bind ?? {}
    return (
      <div
        key={key}
        ref={ref}
        style={style}
        {...rest}
        className={cx(
          "touch-manipulation select-none [-webkit-touch-callout:none]",
          props.sidebar ? "rounded-[10px] bg-sidebar" : "bg-card"
        )}
      >
        <button
          type="button"
          onClick={() => props.onSelect(s)}
          onKeyDown={onKeyDown}
          aria-current={active(s) ? "page" : undefined}
          aria-label={`${title}, ${words}`}
          className={cx(
            "flex w-full items-center gap-3 text-left text-[17px] outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-inset",
            props.sidebar
              ? cx(
                  "h-11 rounded-[10px] px-3",
                  active(s) ? "bg-selected" : "hover:bg-selected/50"
                )
              : "min-h-11 px-4 py-2.5 active:bg-selected"
          )}
        >
          <span className="text-accent-text">{icon}</span>
          <span className="min-w-0 flex-1 truncate">{title}</span>
          {open > 0 && <span className="text-secondary">{open}</span>}
          {!props.sidebar && (
            <ChevronRight className="size-4 text-tertiary" aria-hidden />
          )}
        </button>
      </div>
    )
  }

  const lists = model.lists.map((l) =>
    item(
      { kind: "list", id: l.id },
      listTitle(l),
      <ListIcon className="size-5" />,
      reorder.row(l.id)
    )
  )
  const noLists =
    model.lists.length === 0 && model.status === "live"
      ? "No lists yet. Add one to sort your tasks."
      : undefined

  return (
    <div className="flex min-h-full flex-col">
      {props.sidebar ? (
        <div className="flex h-[60px] items-center gap-2 pt-2 pr-3 pl-5">
          <h1 className="flex-1 text-[17px] font-semibold">Lists</h1>
          {newList}
        </div>
      ) : (
        <>
          <div className="sticky top-0 z-20 flex h-[60px] items-center justify-end px-4 pt-2">
            {newList}
          </div>
          <LargeTitle>Lists</LargeTitle>
          <LinkStatus status={model.status} words={model.words} />
          <Notices wide={false} />
        </>
      )}
      <nav
        aria-label="Lists"
        className={cx(
          "flex-1",
          props.sidebar ? "space-y-0.5 px-3 pb-8" : "px-4 pb-32"
        )}
      >
        {props.sidebar ? (
          fixed.map((f) => item(f.s, f.title, f.icon))
        ) : (
          <Card>{fixed.map((f) => item(f.s, f.title, f.icon))}</Card>
        )}
        <h2
          className={cx(
            "pb-1.5 font-semibold text-secondary",
            props.sidebar ? "px-3 pt-5 text-[13px]" : "px-4 pt-2 text-[17px]"
          )}
        >
          My Lists
        </h2>
        {props.sidebar ? lists : lists.length > 0 && <Card>{lists}</Card>}
        {noLists && (
          <p className="px-4 py-2 text-[13px] text-secondary">{noLists}</p>
        )}
        {props.sidebar && (
          <div className="pt-5">
            <button
              type="button"
              onClick={() => props.onSection("settings")}
              aria-current={props.section === "settings" ? "page" : undefined}
              className={cx(
                "flex h-11 w-full items-center gap-3 rounded-[10px] px-3 text-left text-[17px] outline-none focus-visible:ring-2 focus-visible:ring-accent",
                props.section === "settings"
                  ? "bg-selected"
                  : "hover:bg-selected/50"
              )}
            >
              <Settings className="size-5 text-accent-text" />
              Settings
            </button>
          </div>
        )}
      </nav>
      <div className="sr-only" aria-live="polite">
        {spoken}
      </div>
      {menu && menuList && (
        <Menu
          at={menu.at}
          label={listTitle(menuList)}
          items={listMenu(menuList.id, menuList.name)}
          onClose={() => setMenu(undefined)}
        />
      )}
    </div>
  )
}

function Card({ children }: { children: ReactNode }) {
  return (
    <div className="mb-6 rounded-[14px] bg-card [&>*+*]:relative [&>*+*]:before:pointer-events-none [&>*+*]:before:absolute [&>*+*]:before:inset-x-0 [&>*+*]:before:top-0 [&>*+*]:before:ml-[52px] [&>*+*]:before:h-px [&>*+*]:before:scale-y-50 [&>*+*]:before:bg-separator [&>*:first-child]:rounded-t-[14px] [&>*:last-child]:rounded-b-[14px]">
      {children}
    </div>
  )
}
