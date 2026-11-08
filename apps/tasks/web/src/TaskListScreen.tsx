import {
  ActionRow,
  BarButton,
  Confirm,
  Field,
  LargeTitle,
  LinkStatus,
  NavBar,
  Section,
  Sheet,
} from "@substrate/react"
import { Ellipsis, Plus, SquarePen } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { useTaskActions, type TaskDraft } from "./actions"
import {
  byDue,
  byPosition,
  inSelection,
  type Selection,
  type TaskItem,
} from "./model"
import { useSettings, useTasks } from "./state"
import { TaskEditor } from "./TaskEditor"
import { TaskRow } from "./TaskRow"

export function selectionTitle(s: Selection, listName?: string): string {
  return s.kind === "today"
    ? "Today"
    : s.kind === "inbox"
      ? "No List"
      : (listName ?? "List")
}

/** A list's open tasks in their order, its completed ones when Settings
 * shows them, and the quick add under them. */
export function TaskListScreen(props: {
  selection: Selection
  selectedTask?: string
  now: Date
  today: string
  onBack?: () => void
  onOpenTask: (id: string) => void
  onListDeleted: () => void
}) {
  const model = useTasks()
  const actions = useTaskActions()
  const [settings, setSettings] = useSettings()
  const { selection, now, today } = props
  const list = selection.kind === "list" ? model.list(selection.id) : undefined
  const title = selectionTitle(selection, list?.name)
  // A task marked done stays in place, ticked, until the owner leaves the list.
  const [recent, setRecent] = useState<Set<string>>(new Set())
  const selectionKey =
    selection.kind === "list" ? `list:${selection.id}` : selection.kind
  useEffect(() => setRecent(new Set()), [selectionKey])

  const [composing, setComposing] = useState(false)
  const [menu, setMenu] = useState(false)
  const [renaming, setRenaming] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [quick, setQuick] = useState("")

  const { open, closed } = useMemo(() => {
    const here = model.tasks.filter(
      (t) =>
        inSelection(t, selection, today) ||
        (recent.has(t.id) && t.status !== "open")
    )
    const sort = selection.kind === "today" ? byDue : byPosition
    const open = here
      .filter((t) => t.status === "open" || recent.has(t.id))
      .sort(sort)
    const closed = here
      .filter((t) => t.status !== "open" && !recent.has(t.id))
      .sort(
        (a, b) =>
          (b.completedAt ?? "").localeCompare(a.completedAt ?? "") ||
          byPosition(a, b)
      )
    return { open, closed }
  }, [model.tasks, selection, today, recent])

  const listNames = useMemo(
    () => new Map(model.lists.map((l) => [l.id, l.name])),
    [model.lists]
  )
  const showList = selection.kind === "today"

  const toggle = (t: TaskItem) => {
    if (t.status === "open") {
      setRecent((r) => new Set(r).add(t.id))
      void actions.complete(t)
    } else {
      void actions.reopen(t)
    }
  }

  const initial: Partial<TaskDraft> =
    selection.kind === "list"
      ? { list: selection.id }
      : selection.kind === "today"
        ? { due: { day: today } }
        : {}

  const quickAdd = () => {
    if (!quick.trim()) return
    void actions.create({
      name: quick,
      description: "",
      url: "",
      priority: "none",
      ...initial,
    })
    setQuick("")
  }

  if (selection.kind === "list" && !list && model.status === "live") {
    return (
      <div className="mx-auto w-full max-w-2xl">
        <NavBar onBack={props.onBack} />
        <p className="px-8 pt-20 text-center text-secondary">
          This list was deleted.
        </p>
      </div>
    )
  }

  const row = (t: TaskItem) => (
    <TaskRow
      key={t.id}
      task={t}
      now={now}
      listName={showList && t.list ? listNames.get(t.list) : undefined}
      selected={props.selectedTask === t.id}
      onOpen={() => props.onOpenTask(t.id)}
      onToggle={() => toggle(t)}
    />
  )

  return (
    <div className="relative mx-auto flex min-h-dvh w-full max-w-2xl flex-col md:min-h-full">
      <NavBar
        onBack={props.onBack}
        actions={
          <div className="flex rounded-full bg-card/80 shadow-[0_1px_6px_rgb(0_0_0/0.08)] ring-1 ring-separator/40 backdrop-blur-xl">
            <button
              type="button"
              className="flex size-11 items-center justify-center"
              onClick={() => setComposing(true)}
              aria-label="New Task"
            >
              <SquarePen className="size-5" />
            </button>
            <button
              type="button"
              className="flex size-11 items-center justify-center"
              onClick={() => setMenu(true)}
              aria-label="More"
            >
              <Ellipsis className="size-5" />
            </button>
          </div>
        }
      />
      <LargeTitle>{title}</LargeTitle>
      <LinkStatus status={model.status} words={model.words} />
      <div className="flex-1 px-4 pb-40">
        {open.length > 0 && <Section>{open.map(row)}</Section>}
        {open.length === 0 && model.status === "live" && (
          <p className="px-4 pt-10 text-center text-[17px] text-secondary">
            {selection.kind === "today" ? "Nothing due today." : "No tasks."}
          </p>
        )}
        {settings.showCompleted && closed.length > 0 && (
          <Section header="Completed">{closed.map(row)}</Section>
        )}
      </div>

      <form
        className="sticky bottom-[max(5.5rem,calc(env(safe-area-inset-bottom)+5rem))] z-10 px-4 md:bottom-4"
        onSubmit={(e) => {
          e.preventDefault()
          quickAdd()
        }}
      >
        <label className="flex h-[52px] items-center gap-3 rounded-full bg-card/85 px-4 shadow-[0_2px_16px_rgb(0_0_0/0.10)] ring-1 ring-separator/40 backdrop-blur-xl">
          <span className="flex size-[22px] items-center justify-center rounded-full bg-accent text-white">
            <Plus className="size-4" strokeWidth={3} />
          </span>
          <input
            className="min-w-0 flex-1 bg-transparent text-[17px] outline-none placeholder:text-tertiary"
            placeholder={
              selection.kind === "today"
                ? "Add a task for today"
                : `Add a task to ${title}`
            }
            value={quick}
            onChange={(e) => setQuick(e.target.value)}
            enterKeyHint="done"
          />
        </label>
      </form>

      {composing && (
        <TaskEditor
          open
          initial={initial}
          lists={model.lists}
          today={today}
          onClose={() => setComposing(false)}
          onSave={async (d) => !!(await actions.create(d))}
        />
      )}

      <Sheet open={menu} onClose={() => setMenu(false)} title={title}>
        <Section>
          <ActionRow
            onClick={() => {
              setSettings({ showCompleted: !settings.showCompleted })
              setMenu(false)
            }}
          >
            {settings.showCompleted
              ? "Hide Completed Tasks"
              : "Show Completed Tasks"}
          </ActionRow>
        </Section>
        {list && (
          <Section>
            <ActionRow
              onClick={() => {
                setMenu(false)
                setRenaming(true)
              }}
            >
              Rename List
            </ActionRow>
            <ActionRow
              destructive
              onClick={() => {
                setMenu(false)
                setDeleting(true)
              }}
            >
              Delete List
            </ActionRow>
          </Section>
        )}
      </Sheet>

      {renaming && list && (
        <ListNameSheet
          title="Rename List"
          initial={list.name}
          onClose={() => setRenaming(false)}
          onSave={async (name) => !!(await actions.renameList(list, name))}
        />
      )}

      {list && (
        <Confirm
          open={deleting}
          title={`Delete “${list.name}”?`}
          message={(() => {
            const n = model.tasks.filter((t) => t.list === list.id).length
            return n === 0
              ? "The list is deleted on every device."
              : `Its ${n === 1 ? "task is" : `${n} tasks are`} deleted with it, on every device.`
          })()}
          confirm="Delete"
          onCancel={() => setDeleting(false)}
          onConfirm={() => {
            setDeleting(false)
            void actions.removeList(list)
            props.onListDeleted()
          }}
        />
      )}
    </div>
  )
}

export function ListNameSheet(props: {
  title: string
  initial?: string
  onClose: () => void
  onSave: (name: string) => Promise<boolean>
}) {
  const [name, setName] = useState(props.initial ?? "")
  const [busy, setBusy] = useState(false)
  const save = async () => {
    if (!name.trim()) return
    setBusy(true)
    const ok = await props.onSave(name)
    setBusy(false)
    if (ok) props.onClose()
  }
  return (
    <Sheet
      open
      onClose={props.onClose}
      title={props.title}
      actions={
        <BarButton prominent disabled={busy || !name.trim()} onClick={save}>
          Done
        </BarButton>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void save()
        }}
      >
        <Section>
          <Field
            label="Name"
            value={name}
            onChange={setName}
            placeholder="Groceries"
            autoFocus
          />
        </Section>
      </form>
    </Sheet>
  )
}
