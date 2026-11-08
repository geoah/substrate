import { TabBar, TabItem, cx, useMediaQuery } from "@substrate/react"
import { CalendarDays, ListChecks, Settings } from "lucide-react"
import { useEffect } from "react"
import { civilDay, inSelection, isOpen, type Selection } from "./model"
import { ListsMenu } from "./ListsMenu"
import { SettingsScreen } from "./SettingsScreen"
import { TaskDetail } from "./TaskDetail"
import { TaskListScreen } from "./TaskListScreen"
import { useNow, useRoute, useTasks, type Section } from "./state"

/** Tasks on a phone (a tab bar over stacks), a tablet (the lists beside a
 * list, the task over it) and a desktop (three columns), as the Apple app
 * lays out each size. Where the owner is lives in the address bar. */
export function App() {
  const [route, go] = useRoute()
  const model = useTasks()
  const now = useNow()
  const today = civilDay(now)
  const wide = useMediaQuery("(min-width: 768px)")
  const threeColumns = useMediaQuery("(min-width: 1180px)")

  const section = route.section
  // Wide, the lists are always beside the list, so "lists" alone means Today.
  const shown: Section =
    wide && section.kind === "lists" ? { kind: "today" } : section
  const selection: Selection | undefined =
    shown.kind === "today" || shown.kind === "inbox" || shown.kind === "list"
      ? shown
      : undefined
  const task = route.task ? model.task(route.task) : undefined

  // A task deleted elsewhere closes; a deep link to one not yet loaded waits.
  useEffect(() => {
    if (route.task && !task && model.status === "live") go({ section }, true)
  }, [route.task, task, model.status, section, go])

  const open = (s: Section) => go({ section: s })
  const openTask = (id: string) => go({ section: shown, task: id })
  const closeTask = () => go({ section: shown })
  const backToLists = () => go({ section: { kind: "lists" } })

  const dueToday = model.tasks.filter(
    (t) => isOpen(t) && inSelection(t, { kind: "today" }, today)
  ).length

  const detail = task && (
    <TaskDetail
      key={task.id}
      task={task}
      lists={model.lists}
      now={now}
      today={today}
      onBack={threeColumns ? undefined : closeTask}
      onDeleted={closeTask}
    />
  )

  const listScreen = selection && (
    <TaskListScreen
      selection={selection}
      selectedTask={threeColumns ? route.task : undefined}
      now={now}
      today={today}
      onBack={!wide && selection.kind !== "today" ? backToLists : undefined}
      onOpenTask={openTask}
      onListDeleted={() =>
        go({ section: wide ? { kind: "today" } : { kind: "lists" } })
      }
    />
  )

  if (!wide) {
    const tab =
      section.kind === "today"
        ? "today"
        : section.kind === "settings"
          ? "settings"
          : "lists"
    return (
      <div className="min-h-full bg-grouped">
        {detail ??
          (section.kind === "lists" ? (
            <ListsMenu
              current={section}
              today={today}
              sidebar={false}
              onSelect={open}
            />
          ) : section.kind === "settings" ? (
            <SettingsScreen />
          ) : (
            listScreen
          ))}
        <TabBar>
          <TabItem
            icon={<CalendarDays className="size-6" />}
            label="Today"
            badge={dueToday}
            active={tab === "today"}
            onClick={() => open({ kind: "today" })}
          />
          <TabItem
            icon={<ListChecks className="size-6" />}
            label="Lists"
            active={tab === "lists"}
            onClick={() => open({ kind: "lists" })}
          />
          <TabItem
            icon={<Settings className="size-6" />}
            label="Settings"
            active={tab === "settings"}
            onClick={() => open({ kind: "settings" })}
          />
        </TabBar>
      </div>
    )
  }

  return (
    <div className="flex h-full overflow-hidden">
      <aside className="h-full w-[300px] shrink-0 overflow-y-auto border-r border-separator/50 bg-sidebar">
        <ListsMenu current={shown} today={today} sidebar onSelect={open} />
      </aside>
      {shown.kind === "settings" ? (
        <main className="h-full flex-1 overflow-y-auto bg-grouped">
          <SettingsScreen />
        </main>
      ) : threeColumns ? (
        <>
          <main className="h-full min-w-0 flex-1 overflow-y-auto bg-canvas">
            {listScreen}
          </main>
          <section
            className={cx(
              "h-full w-[min(480px,40vw)] shrink-0 overflow-y-auto border-l border-separator/50 bg-grouped"
            )}
          >
            {detail ?? (
              <div className="flex h-full items-center justify-center text-[17px] text-tertiary">
                No Task Selected
              </div>
            )}
          </section>
        </>
      ) : (
        <main className="h-full min-w-0 flex-1 overflow-y-auto bg-grouped">
          {detail ?? listScreen}
        </main>
      )}
    </div>
  )
}
