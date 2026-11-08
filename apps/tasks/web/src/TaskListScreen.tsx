import { LargeTitle, LinkStatus, NavBar, cx } from "@substrate/react"
import {
  ArrowUpDown,
  CircleCheck,
  Ellipsis,
  Eye,
  EyeOff,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react"
import { Fragment, useState } from "react"
import { useCompleting, useTaskActions } from "./actions"
import { Menu, MenuButton, type MenuItem } from "./Menu"
import { inboxTitle, type Order, type Selection } from "./model"
import { presentList, type ListView, type RowView } from "./present"
import { useReorder } from "./reorder"
import {
  filterStore,
  namingStore,
  setSettings,
  useSettings,
  useStore,
  useTasks,
} from "./state"
import { taskMenu } from "./taskMenu"
import { TaskRow } from "./TaskRow"

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
  const actions = useTaskActions()
  const [settings] = useSettings()
  const filter = useStore(filterStore)
  const completing = useCompleting()
  const view = presentList(
    props.selection,
    model,
    settings,
    filter,
    completing,
    props.now
  )
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
    props.selection.kind === "list" && !view.list && model.status === "live"
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
    return (
      <TaskRow
        key={r.task.id}
        view={r}
        pending={completing.get(r.task.id)}
        selected={props.selectedTask === r.task.id}
        faded={r.band === "later"}
        actions={actions}
        onOpen={() => props.onOpenTask(r.task.id)}
        elementRef={ref}
        {...bind}
      />
    )
  }

  const menuTask = menu && model.task(menu.id)
  const showEmpty = view.empty && view.closed.length === 0

  return (
    <div className="@container relative mx-auto flex min-h-dvh w-full max-w-2xl flex-col md:min-h-full">
      <NavBar
        onBack={props.onBack}
        actions={
          view.selection.kind !== "needs" && (
            <MenuButton
              label="List Options"
              items={() =>
                listOptions(view, settings.order, actions.deleteList)
              }
              className="flex size-11 items-center justify-center rounded-full bg-card/80 shadow-[0_1px_6px_rgb(0_0_0/0.08)] ring-1 ring-separator/40 backdrop-blur-xl active:bg-selected"
            >
              <Ellipsis className="size-5" />
            </MenuButton>
          )
        }
      />
      <LargeTitle className="pb-1 break-words">{view.title}</LargeTitle>
      <Summary view={view} />
      <LinkStatus status={model.status} words={model.words} />

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

        {showEmpty && model.status === "live" && (
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
            <NewTaskButton label={view.newTask} onClick={actions.newTask} />
          </div>
        )}

        {view.bands.length > 0
          ? view.bands.map((b) => (
              <Fragment key={b.band}>
                {b.band === "upcoming" && (
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
                {view.showsClosed && view.closed.length > 0 ? (
                  <Heading title={view.openTitle} note={view.orderNote} />
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

        {view.showsClosed && view.closed.length > 0 && (
          <>
            <Heading title={view.closedTitle} count={view.closed.length} />
            <Card>{view.closed.map((r) => row(r, closedRows))}</Card>
          </>
        )}
      </div>

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
            onClick={actions.newTask}
            compact
          />
        )}
      </div>

      <div className="sr-only" aria-live="polite">
        {spoken}
      </div>

      {menu && menuTask && (
        <Menu
          at={menu.at}
          label={menuTask.name || "Task"}
          items={taskMenu(
            menuTask,
            completing.has(menuTask.id),
            model.lists,
            actions
          )}
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
      <span className="font-medium text-label">{view.summary.headline}</span>
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
    <div className="mb-6 rounded-[14px] bg-card [&>*+*]:relative [&>*+*]:before:pointer-events-none [&>*+*]:before:absolute [&>*+*]:before:inset-x-0 [&>*+*]:before:top-0 [&>*+*]:before:ml-4 [&>*+*]:before:h-px [&>*+*]:before:scale-y-50 [&>*+*]:before:bg-separator [&>*:first-child]:rounded-t-[14px] [&>*:last-child]:rounded-b-[14px]">
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

export function selectionTitle(s: Selection, name?: string) {
  return s.kind === "needs"
    ? "Needs you"
    : s.kind === "inbox"
      ? inboxTitle
      : name || "Untitled list"
}
