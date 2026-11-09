import {
  LargeTitle,
  LinkStatus,
  NavBar,
  cx,
  useLinkedStatus,
} from "@substrate/react"
import {
  ArrowUpDown,
  CalendarX,
  Check,
  ChevronRight,
  CircleCheck,
  Ellipsis,
  Eye,
  EyeOff,
  Pencil,
  Pin,
  PinOff,
  Plus,
  Sparkle,
  Trash2,
} from "lucide-react"
import { Fragment, useEffect, useMemo, useState } from "react"
import { usePending, useTaskActions } from "./actions"
import { Menu, MenuButton, type MenuItem } from "./Menu"
import { Notices } from "./Notices"
import { type Order, type Selection } from "./model"
import {
  presentList,
  type ListView,
  type QueueRow,
  type RowView,
} from "./present"
import { useReorder } from "./reorder"
import {
  filterStore,
  namingStore,
  setSettings,
  show,
  useSettings,
  useStore,
  useTasks,
} from "./state"
import { taskMenu } from "./taskMenu"
import { TaskRow } from "./TaskRow"
import { words } from "./words"

/** One list's tasks: the open ones in the chosen order, then the done ones
 * where shown; on Needs you every open task in its band under the chips
 * that narrow it. A row opens its task, its mark completes it, its menu
 * (a right click, a long press) offers the rest, and in my order it is
 * dragged to its place. */
export function TaskListScreen(props: {
  selection: Selection
  selectedTask?: string
  now: Date
  /** A phone: the tab bar carries the round +, so the list draws none. */
  addsFromTabBar: boolean
  onBack?: () => void
  onOpenTask: (id: string) => void
}) {
  const model = useTasks()
  const link = useLinkedStatus()
  const tasksRead = model.listed
  const listsRead = model.listed
  const actions = useTaskActions()
  const [settings] = useSettings()
  const filter = useStore(filterStore)
  const pending = usePending()
  // The rows are presented again only when the records, the settings,
  // the filter, a decision's window or the minute move.
  const view = useMemo(
    () =>
      presentList(props.selection, model, settings, filter, pending, props.now),
    [props.selection, model, settings, filter, pending, props.now]
  )
  const [selecting, setSelecting] = useState(false)
  const [picked, setPicked] = useState<ReadonlySet<string>>(new Set())
  const pick = (id: string) =>
    setPicked((was) => {
      const next = new Set(was)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  const endSelect = () => {
    setSelecting(false)
    setPicked(new Set())
  }
  // The queue's keys: A accepts and D dismisses every suggestion picked;
  // S starts or ends Select. A group's P pins it or takes its pin out.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return
      const target = e.target as HTMLElement | null
      if (
        target &&
        (target.isContentEditable ||
          /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))
      )
        return
      if (
        document.querySelector("[role=dialog],[role=alertdialog],[role=menu]")
      )
        return
      const key = e.key.toLowerCase()
      if (key === "p" && props.selection.kind === "list" && view.group) {
        e.preventDefault()
        actions.pin(props.selection.id, !view.group.pinned)
        return
      }
      if (props.selection.kind !== "suggestions") return
      if (key === "s" && view.selects) {
        e.preventDefault()
        if (selecting) endSelect()
        else setSelecting(true)
        return
      }
      if (key !== "a" && key !== "d") return
      const action = key === "a" ? "accept" : "dismiss"
      if (selecting && picked.size) {
        e.preventDefault()
        actions.actAll([...picked], action)
        endSelect()
      }
    }
    // Ahead of the open suggestion's own A and D, which a choice of many
    // replaces.
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [props.selection, selecting, picked, actions, view.selects, view.group])
  const [quick, setQuick] = useState("")
  const [menu, setMenu] = useState<{
    id: string
    at: { x: number; y: number }
  }>()
  const [spoken, setSpoken] = useState("")

  const reorder = useReorder({
    ids: view.open.map((r) => r.task.id),
    enabled: view.reorders,
    onMove: (id, index) => actions.reorderTask(id, index, props.selection),
    onMenu: (id, at) => setMenu({ id, at }),
    announce: setSpoken,
  })
  // Done rows are not dragged, and hold or right-click for their menu too.
  const closedRows = useReorder({
    ids: view.closed.map((r) => r.task.id),
    enabled: false,
    onMove: () => {},
    onMenu: (id, at) => setMenu({ id, at }),
  })

  const deleted =
    props.selection.kind === "list" &&
    !model.list(props.selection.id) &&
    listsRead
  if (deleted)
    return (
      <div className="mx-auto w-full max-w-2xl">
        <NavBar onBack={props.onBack} />
        <p className="px-8 pt-20 text-center text-[17px] text-secondary">
          This list was deleted.
        </p>
      </div>
    )

  const row = (r: RowView, rows: ReturnType<typeof useReorder>) => {
    const { ref, ...bind } = rows.row(r.task.id)
    if (selecting && r.suggestion)
      return (
        <PickRow
          key={r.task.id}
          view={r}
          picked={picked.has(r.task.id)}
          onPick={() => pick(r.task.id)}
        />
      )
    return (
      <TaskRow
        key={r.task.id}
        view={r}
        pending={
          r.decision?.action === "complete" ? r.decision.until : undefined
        }
        selected={props.selectedTask === r.task.id}
        faded={r.band === "later"}
        actions={actions}
        onOpen={() => props.onOpenTask(r.task.id)}
        elementRef={ref}
        {...bind}
      />
    )
  }

  const listMenuButton = (
    <MenuButton
      label="List Options"
      items={() =>
        listOptions(view, settings.order, (id) => actions.deleteList(id))
      }
      className="flex size-11 items-center justify-center rounded-full bg-card/80 shadow-[0_1px_6px_rgb(0_0_0/0.08)] ring-1 ring-separator/40 backdrop-blur-xl active:bg-selected"
    >
      <Ellipsis className="size-5" />
    </MenuButton>
  )

  const menuTask = menu && model.task(menu.id)
  const showEmpty = view.empty && view.closed.length === 0

  return (
    <div className="@container relative mx-auto flex min-h-dvh w-full max-w-2xl flex-col md:min-h-full">
      <NavBar
        onBack={props.onBack}
        actions={
          view.selects ? (
            <button
              type="button"
              onClick={() => (selecting ? endSelect() : setSelecting(true))}
              title={`${selecting ? words.select.cancel : words.select.select} (S)`}
              className="inline-flex h-11 items-center rounded-full bg-card/80 px-4 text-[17px] shadow-[0_1px_6px_rgb(0_0_0/0.08)] ring-1 ring-separator/40 backdrop-blur-xl active:bg-selected"
            >
              {selecting ? words.select.cancel : words.select.select}
            </button>
          ) : view.group ? (
            <>
              <button
                type="button"
                onClick={() =>
                  props.selection.kind === "list" &&
                  actions.pin(props.selection.id, !view.group!.pinned)
                }
                aria-label={
                  view.group.pinned ? words.group.unpin : words.group.pin
                }
                title={`${view.group.pinned ? words.group.unpin : words.group.pin} (P)`}
                aria-pressed={view.group.pinned}
                className="flex size-11 items-center justify-center rounded-full bg-card/80 text-accent-text shadow-[0_1px_6px_rgb(0_0_0/0.08)] ring-1 ring-separator/40 backdrop-blur-xl active:bg-selected"
              >
                {view.group.pinned ? (
                  <PinOff className="size-5" />
                ) : (
                  <Pin className="size-5" />
                )}
              </button>
              {listMenuButton}
            </>
          ) : (
            (view.selection.kind === "inbox" ||
              view.selection.kind === "missing") &&
            listMenuButton
          )
        }
      />
      <LargeTitle className="pb-1 break-words">{view.title}</LargeTitle>
      <Summary view={view} />
      <LinkStatus status={link.status} words={link.words} />
      <Notices wide={!props.addsFromTabBar} />

      {view.queue && (
        <QueueLink
          row={view.queue}
          icon={<Sparkle className="size-5 fill-accent text-accent" />}
          onClick={() => show({ kind: "suggestions" })}
          className="mt-1 mb-3"
        />
      )}

      {view.chips.length > 0 && (
        <div
          className="flex [scrollbar-width:none] gap-2 overflow-x-auto px-4 pt-1 pb-3"
          role="group"
          aria-label="Show tasks in"
        >
          {view.chips.map((c) => (
            <button
              key={c.id}
              type="button"
              aria-pressed={c.selected}
              onClick={() => filterStore.set(c.selection)}
              className={cx(
                "h-9 shrink-0 rounded-full px-3.5 text-[15px] font-semibold whitespace-nowrap",
                c.selected
                  ? "bg-label text-canvas"
                  : "bg-fill text-label active:bg-selected"
              )}
            >
              {c.title}
              {c.count !== undefined && (
                <span className="ml-1.5 font-normal opacity-70">{c.count}</span>
              )}
            </button>
          ))}
        </div>
      )}

      <div className="flex-1 px-4 pb-44 md:pb-28">
        {view.clear && (
          <p className="flex items-center gap-2 px-1 pb-3 text-[15px] text-secondary">
            <CircleCheck className="size-4" />
            {view.clear}
          </p>
        )}

        {showEmpty && tasksRead && (
          <div className="flex flex-col items-center gap-3 px-6 pt-14 pb-6 text-center">
            <CircleCheck
              className="size-10 text-secondary"
              strokeWidth={1.5}
              aria-hidden
            />
            <p className="text-[22px] font-semibold">{view.empty!.title}</p>
            <p className="max-w-xs text-[17px] text-secondary">
              {view.empty!.message}
            </p>
            <NewTaskButton
              label={view.newTask}
              onClick={() => actions.newTask()}
            />
          </div>
        )}

        {view.bands.length > 0
          ? view.bands.map((b) => (
              <Fragment key={b.band}>
                {b.band !== "later" && (
                  <Heading title={b.title} count={b.count} />
                )}
                {b.band === "later" && <Fold title={b.title} />}
                <Card>
                  {view.open
                    .filter((r) => r.band === b.band)
                    .map((r) => row(r, reorder))}
                </Card>
              </Fragment>
            ))
          : view.open.length > 0 && (
              <>
                {(view.showsClosed && view.closed.length > 0) || view.group ? (
                  <Heading
                    title={view.openTitle}
                    count={view.group ? view.open.length : undefined}
                    note={view.orderNote}
                  />
                ) : (
                  view.orderNote && (
                    <p className="px-1 pb-1.5 text-[13px] text-secondary">
                      {view.orderNote}
                    </p>
                  )
                )}
                <Card>{view.open.map((r) => row(r, reorder))}</Card>
              </>
            )}

        {view.noDate && (
          <QueueLink
            row={view.noDate}
            icon={<CalendarX className="size-5 text-accent-text" />}
            onClick={() => show({ kind: "noDate" })}
            className="-mx-4 mb-6"
          />
        )}

        {view.showsClosed && view.closed.length > 0 && (
          <>
            <Heading title={view.closedTitle} count={view.closed.length} />
            <Card>{view.closed.map((r) => row(r, closedRows))}</Card>
          </>
        )}
      </div>

      {selecting && (
        <div
          className={cx(
            "sticky z-10 flex items-center gap-3 px-4",
            props.addsFromTabBar
              ? "bottom-[max(5.75rem,calc(env(safe-area-inset-bottom)+5.25rem))]"
              : "bottom-4"
          )}
        >
          <div className="flex min-w-0 flex-1 items-center gap-2 rounded-full bg-card/85 p-1.5 pl-4 shadow-[0_2px_16px_rgb(0_0_0/0.10)] ring-1 ring-separator/40 backdrop-blur-xl">
            <span
              className="min-w-0 flex-1 truncate text-[15px] text-secondary"
              aria-live="polite"
            >
              {words.select.picked(picked.size)}
            </span>
            <button
              type="button"
              disabled={!picked.size}
              title={`${words.select.dismiss} (D)`}
              onClick={() => {
                actions.actAll([...picked], "dismiss")
                endSelect()
              }}
              className="h-11 rounded-full bg-fill px-4 text-[15px] font-semibold disabled:opacity-40"
            >
              {words.select.dismiss}
            </button>
            <button
              type="button"
              disabled={!picked.size}
              title={`${words.select.accept} (A)`}
              onClick={() => {
                actions.actAll([...picked], "accept")
                endSelect()
              }}
              className="h-11 rounded-full bg-accent px-4 text-[15px] font-semibold text-white disabled:opacity-40"
            >
              {words.select.accept}
            </button>
          </div>
        </div>
      )}

      {view.quickAdd && !selecting && (
        <div
          className={cx(
            "sticky z-10 flex items-center gap-3 px-4",
            props.addsFromTabBar
              ? "bottom-[max(5.75rem,calc(env(safe-area-inset-bottom)+5.25rem))]"
              : "bottom-4"
          )}
        >
          <form
            className="min-w-0 flex-1"
            onSubmit={(e) => {
              e.preventDefault()
              actions.quickAdd(quick)
              setQuick("")
            }}
          >
            <label className="flex h-[52px] items-center gap-3 rounded-full bg-card/85 px-4 shadow-[0_2px_16px_rgb(0_0_0/0.10)] ring-1 ring-separator/40 backdrop-blur-xl">
              <span className="flex size-[22px] shrink-0 items-center justify-center rounded-full bg-accent text-white">
                <Plus className="size-4" strokeWidth={3} />
              </span>
              <input
                className="min-w-0 flex-1 bg-transparent text-[17px] outline-none placeholder:text-tertiary"
                placeholder={view.quickAdd}
                aria-label={view.quickAdd}
                value={quick}
                onChange={(e) => setQuick(e.target.value)}
                enterKeyHint="done"
                data-quick-add
              />
            </label>
          </form>
          {!props.addsFromTabBar && !showEmpty && (
            <NewTaskButton
              label={view.newTask}
              onClick={() => actions.newTask()}
              compact
            />
          )}
        </div>
      )}

      <div className="sr-only" aria-live="polite">
        {spoken}
      </div>

      {menu && menuTask && (
        <Menu
          at={menu.at}
          label={menuTask.name || "Task"}
          items={taskMenu(menuTask, pending.has(menuTask.id), model, actions)}
          onClose={() => setMenu(undefined)}
        />
      )}
    </div>
  )
}

/** What the list holds at a glance, under its title. */
function Summary({ view }: { view: ListView }) {
  return (
    <p className="flex flex-wrap items-baseline gap-x-3 px-4 pb-3 text-[15px] text-secondary">
      {view.summary.detail && <span>{view.summary.detail}</span>}
      {view.summary.headline && (
        <span className="font-medium text-label">{view.summary.headline}</span>
      )}
      {view.summary.overdue && (
        <span className="font-medium text-danger">{view.summary.overdue}</span>
      )}
    </p>
  )
}

/** The ⋯ menu: Sort Tasks By, Show or Hide Completed, then the list's
 * Rename List… and Delete List…. */
function listOptions(
  view: ListView,
  order: Order,
  deleteList: (id: string) => void
): MenuItem[] {
  const items: MenuItem[] = [
    {
      kind: "submenu",
      label: "Sort Tasks By",
      icon: <ArrowUpDown className="size-4" />,
      items: [
        { value: "manual" as const, label: "My Order" },
        { value: "due" as const, label: "Due Date" },
      ].map((o) => ({
        label: o.label,
        checked: order === o.value,
        onSelect: () => setSettings({ order: o.value }),
      })),
    },
    {
      label: view.toggleClosed,
      icon: view.showsClosed ? (
        <EyeOff className="size-4" />
      ) : (
        <Eye className="size-4" />
      ),
      onSelect: () => setSettings({ showCompleted: !view.showsClosed }),
    },
  ]
  const list = view.list
  if (list)
    items.push(
      { kind: "divider" },
      {
        label: "Rename List…",
        icon: <Pencil className="size-4" />,
        onSelect: () => namingStore.set({ id: list.id, name: list.name }),
      },
      {
        label: "Delete List…",
        icon: <Trash2 className="size-4" />,
        destructive: true,
        onSelect: () => deleteList(list.id),
      }
    )
  return items
}

/** One card per section, hairlines between its rows. */
function Card({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-6 rounded-[14px] bg-card [&>*+*]:relative [&>*+*]:before:pointer-events-none [&>*+*]:before:absolute [&>*+*]:before:inset-x-0 [&>*+*]:before:top-0 [&>*+*]:before:z-[1] [&>*+*]:before:ml-4 [&>*+*]:before:h-px [&>*+*]:before:scale-y-50 [&>*+*]:before:bg-separator [&>*:first-child]:rounded-t-[14px] [&>*:last-child]:rounded-b-[14px]">
      {children}
    </div>
  )
}

/** A section's name, its count in a pill beside it, and what orders it. */
function Heading(props: { title: string; count?: number; note?: string }) {
  return (
    <div className="px-1 pt-1 pb-2">
      <h2 className="flex items-baseline gap-2 text-[15px] font-semibold">
        {props.title}
        {props.count !== undefined && (
          <span className="rounded-full bg-fill px-[7px] py-px text-[12px] font-semibold text-secondary">
            {props.count}
          </span>
        )}
      </h2>
      {props.note && (
        <p className="mt-0.5 text-[13px] text-secondary">{props.note}</p>
      )}
    </div>
  )
}

/** The Later band's fold: its name between two dashed lines. */
function Fold({ title }: { title: string }) {
  return (
    <div
      className="flex items-center gap-2.5 px-1 pt-2.5 pb-2.5"
      role="heading"
      aria-level={2}
    >
      <span className="h-px flex-1 border-t border-dashed border-separator" />
      <span className="text-[12px] font-semibold tracking-[0.6px] text-tertiary uppercase">
        {title}
      </span>
      <span className="h-px flex-1 border-t border-dashed border-separator" />
    </div>
  )
}

/** New Task: the capsule, or in a narrow column the round + alone. */
export function NewTaskButton(props: {
  label: string
  onClick: () => void
  compact?: boolean
}) {
  return (
    <button
      type="button"
      onClick={props.onClick}
      title={`${props.label} (N)`}
      aria-label={props.label}
      className={cx(
        "inline-flex h-[52px] shrink-0 items-center justify-center gap-2 rounded-full bg-accent text-[17px] font-semibold text-white shadow-[0_2px_16px_rgb(0_0_0/0.14)] active:opacity-80",
        props.compact ? "w-[52px] @md:w-auto @md:px-5" : "px-5"
      )}
    >
      <Plus className="size-5" strokeWidth={2.5} />
      <span className={props.compact ? "hidden @md:inline" : undefined}>
        {props.label}
      </span>
    </button>
  )
}

/** A row that opens a screen of its own: the queue's on Needs you and a
 * group, Needs you's No date. Its title, what it is, and its count where
 * the title does not say it. */
function QueueLink(props: {
  row: QueueRow
  icon: React.ReactNode
  onClick: () => void
  className?: string
}) {
  const counted = props.row.title === words.noDateRow.title
  return (
    <div className={cx("px-4", props.className)}>
      <button
        type="button"
        onClick={props.onClick}
        className="flex min-h-11 w-full items-center gap-3 rounded-[14px] bg-card px-4 py-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-accent active:bg-selected"
      >
        <span className="shrink-0">{props.icon}</span>
        <span className="min-w-0 flex-1">
          <span className="block text-[17px] font-semibold">
            {props.row.title}
          </span>
          <span className="block text-[15px] text-secondary">
            {props.row.detail}
          </span>
        </span>
        {counted && (
          <span className="text-[17px] text-secondary">{props.row.count}</span>
        )}
        <ChevronRight className="size-4 shrink-0 text-tertiary" aria-hidden />
      </button>
    </div>
  )
}

/** A suggestion's row while Select is on: a box that picks it. */
function PickRow(props: {
  view: RowView
  picked: boolean
  onPick: () => void
}) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={props.picked}
      onClick={props.onPick}
      className="flex min-h-11 w-full items-center gap-3 bg-card px-4 py-2.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-inset"
    >
      <span
        className={cx(
          "flex size-[26px] shrink-0 items-center justify-center rounded-full border-[1.5px]",
          props.picked
            ? "border-accent bg-accent text-white"
            : "border-tertiary"
        )}
      >
        {props.picked && <Check className="size-4" strokeWidth={3} />}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-[17px] leading-snug font-medium break-words">
          {props.view.title}
        </span>
        <span className="block text-[13px] text-secondary">
          {[props.view.collection, props.view.suggestion?.written]
            .filter(Boolean)
            .join(" · ")}
        </span>
      </span>
    </button>
  )
}
