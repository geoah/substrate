import { Alert, TabItem, cx, useListed, useMediaQuery } from "@substrate/react"
import { CircleCheck, ListChecks, Plus, Settings } from "lucide-react"
import { useEffect, useState } from "react"
import { useTaskActions } from "./actions"
import { ListsMenu } from "./ListsMenu"
import { Task, type Selection } from "./model"
import { deleteListQuestion, needsYouBadge } from "./present"
import { useReminders } from "./reminders"
import { SettingsScreen } from "./SettingsScreen"
import {
  deletingListStore,
  editorStore,
  go,
  namingStore,
  openTask,
  selectSection,
  show,
  useNow,
  useRoute,
  useSettings,
  useStore,
  useTasks,
  type Section,
} from "./state"
import { TaskDetail } from "./TaskDetail"
import { TaskEditor } from "./TaskEditor"
import { TaskListScreen } from "./TaskListScreen"

/** Tasks on a phone (a tab per section of the App menu and the round + that
 * adds a task, over stacks), a tablet and a desktop (the sections and the
 * lists in a sidebar, the list beside it, the open task in a third
 * column), as the Apple app lays out each size. Where the owner is lives
 * in the address bar. */
export function App() {
  const route = useRoute()
  const model = useTasks()
  const actions = useTaskActions()
  const [settings] = useSettings()
  const now = useNow()
  const wide = useMediaQuery("(min-width: 768px)")
  const threeColumns = useMediaQuery("(min-width: 1000px)")
  const keyboard = useMediaQuery("(hover: hover) and (pointer: fine)") && wide
  const reminders = useReminders(model.tasks, model.lists, settings, now)
  // Whether the tasks were read: a task missing before then may come yet.
  const listed = useListed(Task)

  const { section, selection } = route
  const task = route.task ? model.task(route.task) : undefined

  // A task deleted elsewhere closes; a link to one not yet read waits.
  useEffect(() => {
    if (route.task && !task && listed) go({ ...route, task: undefined }, true)
  }, [route, task, listed])

  // A reminder's task, once its record is read: Lists, on its list, open.
  useEffect(() => {
    if (!route.tapped) return
    const t = model.task(route.tapped)
    if (t)
      go(
        {
          section: "lists",
          selection: t.list ? { kind: "list", id: t.list } : { kind: "inbox" },
          task: t.id,
        },
        true
      )
    else if (listed)
      go({ section: "needs", selection: { kind: "needs" } }, true)
  }, [route.tapped, model, listed])

  useShortcuts({ threeColumns, taskID: route.task, actions })

  const onSection = (s: Section) => selectSection(s, wide)
  const onSelect = (s: Selection) => show(s)

  const detail = task && (
    <TaskDetail
      key={task.id}
      task={task}
      lists={model.lists}
      now={now}
      onBack={threeColumns ? undefined : () => openTask(undefined)}
      beside={threeColumns}
    />
  )

  const listScreen = selection && section !== "settings" && (
    <TaskListScreen
      key={selection.kind === "list" ? selection.id : selection.kind}
      selection={selection}
      selectedTask={threeColumns ? route.task : undefined}
      now={now}
      addsFromTabBar={!wide}
      onBack={!wide && section === "lists" ? () => show(undefined) : undefined}
      onOpenTask={(id) => openTask(id)}
    />
  )

  const overlays = <Overlays wide={wide} />
  const badge = needsYouBadge(model, now)

  if (!wide) {
    return (
      <div className="min-h-full bg-grouped">
        {detail ??
          (section === "settings" ? (
            <SettingsScreen {...reminders} keyboard={keyboard} />
          ) : section === "lists" && !selection ? (
            <ListsMenu
              section={section}
              now={now}
              sidebar={false}
              onSection={onSection}
              onSelect={onSelect}
            />
          ) : (
            listScreen
          ))}
        <PhoneBar
          section={section}
          badge={badge}
          onSection={onSection}
          onAdd={() => actions.newTask()}
        />
        {overlays}
      </div>
    )
  }

  return (
    <div className="flex h-full overflow-hidden">
      <aside
        className={cx(
          "h-full shrink-0 overflow-y-auto border-r border-separator/50 bg-sidebar",
          threeColumns ? "w-[260px] xl:w-[300px]" : "w-[280px]"
        )}
      >
        <ListsMenu
          section={section}
          selection={selection}
          now={now}
          sidebar
          onSection={onSection}
          onSelect={onSelect}
        />
      </aside>
      {section === "settings" ? (
        <main className="h-full flex-1 overflow-y-auto bg-grouped">
          <SettingsScreen {...reminders} keyboard={keyboard} />
        </main>
      ) : !selection ? (
        <main className="h-full flex-1 overflow-y-auto bg-grouped">
          <Unavailable
            icon={<ListChecks className="size-10" strokeWidth={1.5} />}
            title="Choose a list"
          />
        </main>
      ) : threeColumns ? (
        <>
          <main className="h-full min-w-0 flex-1 overflow-y-auto bg-grouped">
            {listScreen}
          </main>
          <section
            aria-label="Task"
            className="h-full w-[min(480px,42vw)] shrink-0 overflow-y-auto border-l border-separator/50 bg-grouped"
          >
            {detail ?? (
              <Unavailable
                icon={<CircleCheck className="size-10" strokeWidth={1.5} />}
                title="No task open"
                message="Choose a task to see it here."
              />
            )}
          </section>
        </>
      ) : (
        <main className="h-full min-w-0 flex-1 overflow-y-auto bg-grouped">
          {detail ?? listScreen}
        </main>
      )}
      {overlays}
    </div>
  )
}

function Unavailable(props: {
  icon: React.ReactNode
  title: string
  message?: string
}) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-2 px-8 text-center text-secondary">
      {props.icon}
      <p className="text-[22px] font-semibold text-label">{props.title}</p>
      {props.message && <p className="text-[17px]">{props.message}</p>}
    </div>
  )
}

/** On a phone: a tab per section of the App menu, Needs you's badge, and
 * apart from them at the bar's trailing end the round + that adds a task. */
function PhoneBar(props: {
  section: Section
  badge: number
  onSection: (s: Section) => void
  onAdd: () => void
}) {
  return (
    <nav
      aria-label="Sections"
      className="fixed inset-x-0 bottom-0 z-30 flex justify-center gap-3 px-4 pb-[max(0.75rem,env(safe-area-inset-bottom))]"
    >
      <div className="flex min-w-0 flex-1 items-stretch gap-1 rounded-full bg-card/80 p-1.5 shadow-[0_2px_16px_rgb(0_0_0/0.12)] ring-1 ring-separator/40 backdrop-blur-xl sm:max-w-sm">
        <TabItem
          icon={<CircleCheck className="size-6" />}
          label="Needs you"
          badge={props.badge}
          active={props.section === "needs"}
          onClick={() => props.onSection("needs")}
        />
        <TabItem
          icon={<ListChecks className="size-6" />}
          label="Lists"
          active={props.section === "lists"}
          onClick={() => props.onSection("lists")}
        />
        <TabItem
          icon={<Settings className="size-6" />}
          label="Settings"
          active={props.section === "settings"}
          onClick={() => props.onSection("settings")}
        />
      </div>
      <button
        type="button"
        onClick={props.onAdd}
        aria-label="New Task"
        className="flex size-[62px] shrink-0 items-center justify-center self-center rounded-full bg-card/80 shadow-[0_2px_16px_rgb(0_0_0/0.12)] ring-1 ring-separator/40 backdrop-blur-xl active:bg-selected"
      >
        <Plus className="size-7" />
      </button>
    </nav>
  )
}

/** What is open over the screens: the editor, a list being named, and the
 * question before a list is deleted. */
function Overlays({ wide }: { wide: boolean }) {
  const model = useTasks()
  const actions = useTaskActions()
  const editor = useStore(editorStore)
  const naming = useStore(namingStore)
  const deleting = useStore(deletingListStore)
  const deletingList = deleting ? model.list(deleting) : undefined
  const question = deletingList && deleteListQuestion(deletingList, model.tasks)

  return (
    <>
      {editor && (
        <TaskEditor
          key={editor.key}
          seed={editor.seed}
          task={
            editor.seed.kind === "task" ? model.task(editor.seed.id) : undefined
          }
          lists={model.lists}
          onClose={() => editorStore.set(undefined)}
        />
      )}
      {naming && (
        <ListNaming
          key={naming.id ?? "new"}
          id={naming.id}
          initial={naming.name}
          onClose={() => namingStore.set(undefined)}
          onDone={(name) =>
            naming.id
              ? actions.renameList(naming.id, name)
              : actions.createList(name)
          }
        />
      )}
      {question && (
        <Alert
          open
          title={question.title}
          message={question.message}
          onCancel={() => deletingListStore.set(undefined)}
          buttons={[
            {
              label: question.cancel,
              role: "cancel",
              onClick: () => deletingListStore.set(undefined),
            },
            {
              label: question.confirm,
              role: "destructive",
              onClick: () => actions.confirmDeleteList(wide),
            },
          ]}
        />
      )}
    </>
  )
}

/** A list being named: a new one, or one renamed. */
function ListNaming(props: {
  id?: string
  initial: string
  onClose: () => void
  onDone: (name: string) => void
}) {
  const [name, setName] = useState(props.initial)
  const done = () => {
    if (!name.trim()) return
    props.onDone(name)
    props.onClose()
  }
  return (
    <Alert
      open
      title={props.id ? "Rename List" : "New List"}
      message="A task list's name, such as Groceries or Home."
      onCancel={props.onClose}
      buttons={[
        { label: "Cancel", role: "cancel", onClick: props.onClose },
        {
          label: props.id ? "Rename" : "Add",
          onClick: done,
          disabled: !name.trim(),
        },
      ]}
    >
      <form
        className="mt-3"
        onSubmit={(e) => {
          e.preventDefault()
          done()
        }}
      >
        <input
          data-autofocus
          aria-label="Name"
          placeholder="Name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="w-full rounded-lg bg-fill px-2.5 py-2 text-[15px] outline-none focus-visible:ring-2 focus-visible:ring-accent"
        />
      </form>
    </Alert>
  )
}

/** The Mac app's menu commands, and what its lists answer to, as keys:
 * N is New Task (⌘N is the browser's own), ⌘, is Settings, ↑ and ↓ move
 * through the list beside an open task, E edits it, Escape closes it. */
function useShortcuts(opts: {
  threeColumns: boolean
  taskID?: string
  actions: ReturnType<typeof useTaskActions>
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.isComposing) return
      const target = e.target as HTMLElement | null
      const typing =
        !!target &&
        (target.isContentEditable ||
          /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))
      const covered = !!document.querySelector(
        "[role=dialog],[role=alertdialog],[role=menu]"
      )
      const mod = e.metaKey || e.ctrlKey
      if (mod && !e.altKey && !e.shiftKey && e.key === ",") {
        e.preventDefault()
        selectSection("settings", window.innerWidth >= 768)
        return
      }
      if (typing || covered || mod || e.altKey) return
      if (e.key === "n" || e.key === "N") {
        e.preventDefault()
        opts.actions.newTask()
      } else if ((e.key === "e" || e.key === "E") && opts.taskID) {
        e.preventDefault()
        opts.actions.edit(opts.taskID)
      } else if (e.key === "Escape" && opts.taskID && opts.threeColumns) {
        openTask(undefined)
      } else if (
        (e.key === "ArrowDown" || e.key === "ArrowUp") &&
        opts.threeColumns
      ) {
        const rows = [
          ...document.querySelectorAll<HTMLElement>("main [data-task-row]"),
        ]
        if (!rows.length) return
        e.preventDefault()
        const at = rows.findIndex((r) => r.dataset.taskRow === opts.taskID)
        const next =
          at < 0
            ? e.key === "ArrowDown"
              ? 0
              : rows.length - 1
            : Math.max(
                0,
                Math.min(rows.length - 1, at + (e.key === "ArrowDown" ? 1 : -1))
              )
        rows[next].focus()
        openTask(rows[next].dataset.taskRow)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [opts.threeColumns, opts.taskID, opts.actions])
}
