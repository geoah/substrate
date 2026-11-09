import { LargeTitle, LinkStatus, cx, useLinkedStatus } from "@substrate/react"
import {
  CalendarX,
  ChevronRight,
  CircleCheck,
  CircleHelp,
  Folder,
  FolderPlus,
  Inbox,
  Layers,
  ListIcon,
  Pencil,
  Pin,
  Search,
  Settings,
  Sparkle,
  Trash2,
} from "lucide-react"
import { useState, type ReactNode } from "react"
import { useTaskActions } from "./actions"
import { Menu, type MenuItem } from "./Menu"
import { Notices } from "./Notices"
import { listTitle, sameSelection, selectionKey, type Selection } from "./model"
import { openCount, presentLists, type ListsSection } from "./present"
import { useReorder } from "./reorder"
import { ownListKind } from "./sources"
import { go, namingStore, useTasks, type Section } from "./state"
import { words } from "./words"

/** How many of a group kind's groups the sidebar lists, the busiest first,
 * before "All N…". */
const busiest = 5

type Row = ListsSection["rows"][number]

/** The lists column. On a phone, Lists: search, Pinned, one section per
 * group kind headed by its label with its full reference under it, each
 * listing its groups with open tasks, "No list" and "Missing" where any
 * are, and "N more with nothing open". Beside the list (`sidebar`), the
 * App menu: Needs you, Suggestions, No date, Pinned, each group kind's
 * busiest groups with "All N…", and Settings. */
export function ListsMenu(props: {
  section: Section
  selection?: Selection
  now: Date
  sidebar: boolean
  onSection: (s: Section) => void
  onSelect: (s: Selection) => void
  /** Wide, the full Lists in the list column: no App menu of its own. */
  column?: boolean
}) {
  const model = useTasks()
  const link = useLinkedStatus()
  const actions = useTaskActions()
  const [query, setQuery] = useState("")
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())
  const [menu, setMenu] = useState<{
    id: string
    at: { x: number; y: number }
  }>()
  const lists = presentLists(model, props.sidebar ? "" : query)
  const [spoken, setSpoken] = useState("")

  /** The groups a section draws: on a phone every one with open tasks, in
   * the section's order; beside the list the busiest, in the same order. */
  const drawn = (s: ListsSection): Row[] => {
    const groups = s.rows.filter((r) => r.selection.kind === "list")
    if (!props.sidebar) return groups
    const top = new Set(
      [...groups].sort((a, b) => b.open - a.open).slice(0, busiest)
    )
    return groups.filter((g) => top.has(g))
  }
  // The app's own lists drag into the owner's order, as before: the rows
  // drawn are some of the lists, so a drop lands beside the same neighbour
  // among them all.
  const ownSection = lists.sections.find((s) => s.kind === ownListKind)
  const ownIDs = ownSection
    ? drawn(ownSection)
        .filter((r) => r.own)
        .map((r) => (r.selection as { id: string }).id)
    : []
  const reorder = useReorder({
    ids: ownIDs,
    enabled: !query.trim(),
    onMove: (id, index) => {
      const all = model.lists.map((l) => l.id).filter((x) => x !== id)
      const shown = ownIDs.filter((x) => x !== id)
      const after = shown[index]
      const before = shown[index - 1]
      const at =
        after !== undefined
          ? all.indexOf(after)
          : before !== undefined
            ? all.indexOf(before) + 1
            : 0
      actions.reorderList(id, at)
    },
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

  const menuList = menu && model.lists.find((l) => l.id === menu.id)
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

  const iconOf = (r: { selection: Selection; own?: boolean }) =>
    r.selection.kind === "inbox" ? (
      <Inbox className="size-5" />
    ) : r.selection.kind === "missing" ? (
      <CircleHelp className="size-5" />
    ) : r.own ? (
      <ListIcon className="size-5" />
    ) : (
      <Folder className="size-5" />
    )

  const item = (r: {
    selection: Selection
    title: string
    detail?: string
    open?: number
    icon: ReactNode
    pinned?: boolean
    own?: boolean
    spoken?: string
  }) => {
    const key = selectionKey(r.selection)
    const listID =
      r.own && r.selection.kind === "list" ? r.selection.id : undefined
    const {
      ref,
      style,
      onKeyDown,
      ...bind
    }: Partial<ReturnType<typeof reorder.row>> =
      listID && ownIDs.includes(listID) ? reorder.row(listID) : {}
    return (
      <div
        key={key}
        ref={ref}
        style={style}
        {...bind}
        onContextMenu={
          bind.onContextMenu ??
          (listID
            ? (e) => {
                e.preventDefault()
                setMenu({ id: listID, at: { x: e.clientX, y: e.clientY } })
              }
            : undefined)
        }
        className={cx(
          "touch-manipulation select-none [-webkit-touch-callout:none]",
          props.sidebar ? "rounded-[10px] bg-sidebar" : "bg-card"
        )}
      >
        <button
          type="button"
          onClick={() => props.onSelect(r.selection)}
          onKeyDown={onKeyDown}
          aria-current={active(r.selection) ? "page" : undefined}
          aria-label={r.spoken ?? `${r.title}, ${r.open ?? 0} open`}
          className={cx(
            "flex w-full items-center gap-3 text-left text-[17px] outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-inset",
            props.sidebar
              ? cx(
                  "min-h-11 rounded-[10px] px-3 py-1.5",
                  active(r.selection) ? "bg-selected" : "hover:bg-selected/50"
                )
              : "min-h-11 px-4 py-2.5 active:bg-selected"
          )}
        >
          <span className="text-accent-text">{r.icon}</span>
          <span className="min-w-0 flex-1">
            <span className="block truncate">{r.title}</span>
            {r.detail && !props.sidebar && (
              <span className="block text-[13px] text-secondary">
                {r.detail}
              </span>
            )}
          </span>
          {r.pinned && !props.sidebar && (
            <Pin className="size-4 text-tertiary" aria-hidden />
          )}
          {!!r.open && <span className="text-secondary">{r.open}</span>}
          {!props.sidebar && (
            <ChevronRight className="size-4 text-tertiary" aria-hidden />
          )}
        </button>
      </div>
    )
  }

  const heading = (title: string, reference?: string) => (
    <div
      className={cx(
        "pb-1.5",
        props.sidebar
          ? "px-3 pt-5"
          : "flex flex-wrap items-baseline gap-x-2 px-4 pt-2"
      )}
    >
      <h2
        className={cx(
          "font-semibold",
          props.sidebar ? "text-[13px] text-secondary" : "text-[17px]"
        )}
      >
        {title}
      </h2>
      {reference && (
        <p
          className={cx(
            "font-mono break-all text-tertiary",
            props.sidebar ? "text-[11px]" : "text-[13px]"
          )}
        >
          {reference}
        </p>
      )}
    </div>
  )

  const group = (rows: ReactNode[]) =>
    props.sidebar ? rows : rows.length > 0 && <Card>{rows}</Card>

  const fixed = props.sidebar
    ? [
        {
          selection: { kind: "needs" } as Selection,
          title: "Needs you",
          icon: <CircleCheck className="size-5" />,
        },
        {
          selection: { kind: "suggestions" } as Selection,
          title: words.queue.title,
          icon: <Sparkle className="size-5" />,
        },
        {
          selection: { kind: "noDate" } as Selection,
          title: words.noDateRow.title,
          icon: <CalendarX className="size-5" />,
        },
      ].map((f) => {
        const { open, words: spoken } = openCount(model, f.selection)
        return item({ ...f, open, spoken: `${f.title}, ${spoken}` })
      })
    : []

  const sectionRows = (s: ListsSection) => {
    const groups = s.rows.filter((r) => r.selection.kind === "list")
    const rest = s.rows.filter((r) => r.selection.kind !== "list")
    if (props.sidebar) {
      const shown = drawn(s)
      const total = groups.length + s.more.length
      const out = [...shown, ...rest].map((r: Row) =>
        item({ ...r, icon: iconOf(r) })
      )
      if (total > shown.length)
        out.push(
          <button
            key={`all:${s.kind}`}
            type="button"
            onClick={() => go({ section: "lists" })}
            className="flex min-h-11 w-full items-center gap-3 rounded-[10px] px-3 text-left text-[17px] text-secondary outline-none hover:bg-selected/50 focus-visible:ring-2 focus-visible:ring-accent"
          >
            <Search className="size-5" />
            All {total} {s.title.toLowerCase()}…
          </button>
        )
      return out
    }
    const out = [...groups, ...rest].map((r) => item({ ...r, icon: iconOf(r) }))
    if (s.more.length) {
      if (expanded.has(s.kind) || query.trim())
        out.push(
          ...s.more.map((m) =>
            item({
              ...m,
              own: model.lists.some(
                (l) => l.id === (m.selection as { id: string }).id
              ),
              icon: iconOf({
                ...m,
                own: model.lists.some(
                  (l) => l.id === (m.selection as { id: string }).id
                ),
              }),
            })
          )
        )
      else
        out.push(
          <button
            key={`more:${s.kind}`}
            type="button"
            onClick={() => setExpanded((e) => new Set(e).add(s.kind))}
            className="flex min-h-11 w-full items-center gap-3 px-4 py-2.5 pl-[52px] text-left text-[17px] text-secondary active:bg-selected"
          >
            <span className="flex-1">{s.moreTitle}</span>
            <ChevronRight className="size-4 text-tertiary" aria-hidden />
          </button>
        )
    }
    return out
  }

  const nothing =
    !lists.sections.length && !lists.ungrouped.length && model.listed

  return (
    <div className="flex min-h-full flex-col">
      {props.sidebar ? (
        <div className="flex h-[60px] items-center gap-2 pt-2 pr-3 pl-5">
          <h2 className="flex-1 text-[17px] font-semibold">Tasks</h2>
          {newList}
        </div>
      ) : (
        <>
          <div className="sticky top-0 z-20 flex h-[60px] items-center justify-end px-4 pt-2">
            {newList}
          </div>
          <LargeTitle>Lists</LargeTitle>
          {!props.column && (
            <>
              <LinkStatus status={link.status} words={link.words} />
              <Notices wide={false} />
            </>
          )}
          <label className="mx-4 mb-4 flex h-11 items-center gap-2 rounded-[10px] bg-fill px-3">
            <Search className="size-5 text-secondary" aria-hidden />
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={lists.search}
              aria-label={lists.search}
              className="min-w-0 flex-1 bg-transparent text-[17px] outline-none placeholder:text-secondary"
            />
          </label>
        </>
      )}
      <nav
        aria-label="Lists"
        className={cx(
          "flex-1",
          props.sidebar ? "space-y-0.5 px-3 pb-8" : "px-4 pb-32"
        )}
      >
        {fixed}
        {lists.pinned.length > 0 && (
          <>
            {heading(words.lists.pinned)}
            {group(
              lists.pinned.map((r) =>
                item({
                  ...r,
                  detail: r.detail,
                  icon: iconOf(r),
                })
              )
            )}
          </>
        )}
        {lists.sections.map((s) => (
          <div key={s.kind}>
            {heading(s.title, s.reference)}
            {group(sectionRows(s))}
          </div>
        ))}
        {lists.ungrouped.length > 0 && (
          <>
            {heading(words.lists.ungrouped)}
            {group(
              lists.ungrouped.map((u) =>
                item({
                  selection: u.selection,
                  title: u.title,
                  detail: u.reference,
                  open: u.open,
                  icon: <Layers className="size-5" />,
                })
              )
            )}
          </>
        )}
        {nothing && (
          <p className="px-4 py-2 text-[13px] text-secondary">
            No lists yet. Add one to sort your tasks.
          </p>
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
