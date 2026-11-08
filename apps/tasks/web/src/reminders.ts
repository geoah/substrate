import { useEffect, useMemo, useState } from "react"
import {
  dayDate,
  isOpen,
  taskTitle,
  timeWords,
  type TaskItem,
  type TaskList,
} from "./model"
import { go, type Settings } from "./state"

/** What the browser allows: not asked yet, on, refused, or a browser with
 * no notifications at all. */
export type Permission = "notAsked" | "on" | "off" | "unsupported"

export function readPermission(): Permission {
  if (typeof Notification === "undefined") return "unsupported"
  switch (Notification.permission) {
    case "granted":
      return "on"
    case "denied":
      return "off"
    default:
      return "notAsked"
  }
}

export interface Reminder {
  id: string
  task: string
  title: string
  body: string
  at: Date
}

/** The most a system keeps waiting for one app (iOS keeps 64); the soonest
 * go first. */
const limit = 60

/** Every reminder's id starts with it, as the native app's do. */
const prefix = "tasks.reminder."

/** What this browser reminds the owner of (TasksKit's `Reminders.plan`):
 * each open task with a due date still ahead, at its moment, or at the
 * chosen hour of its day. */
export function plan(
  tasks: TaskItem[],
  lists: TaskList[],
  settings: Settings,
  permission: Permission,
  now: Date
): Reminder[] {
  if (!settings.remind || permission !== "on") return []
  const names = new Map(lists.map((l) => [l.id, l.name]))
  const out: Reminder[] = []
  for (const t of tasks) {
    if (!isOpen(t) || !t.due) continue
    const at = t.due.at ?? dayDate(t.due.day, settings.remindHour)
    if (at.getTime() <= now.getTime()) continue
    const list = t.list ? names.get(t.list) : undefined
    const when = t.due.at ? `Due at ${timeWords(t.due.at)}` : "Due today"
    out.push({
      id: prefix + t.id,
      task: t.id,
      title: taskTitle(t),
      body: list ? `${when}, in ${list}` : when,
      at,
    })
  }
  return out
    .sort((a, b) => a.at.getTime() - b.at.getTime() || (a.id < b.id ? -1 : 1))
    .slice(0, limit)
}

/** A reminder's tap: Lists, on the task's list, the task open. */
function opened(task: string, tasks: () => TaskItem[]) {
  window.focus()
  const t = tasks().find((x) => x.id === task)
  go({
    section: "lists",
    selection: t?.list ? { kind: "list", id: t.list } : { kind: "inbox" },
    task,
  })
}

const workerURL = `${import.meta.env.BASE_URL}reminders-sw.js`

/** Shows one reminder: as a page's notification, or through the worker
 * where the browser allows only that (Chrome on Android). */
async function notify(r: Reminder, tasks: () => TaskItem[]) {
  const options: NotificationOptions = {
    body: r.body,
    tag: r.id,
    data: { task: r.task },
    icon: `${import.meta.env.BASE_URL}icon-512.png`,
  }
  try {
    const n = new Notification(r.title, options)
    n.onclick = () => {
      n.close()
      opened(r.task, tasks)
    }
    return
  } catch {
    // The constructor is refused where only a worker may notify.
  }
  if (!("serviceWorker" in navigator)) return
  const reg = await navigator.serviceWorker.register(workerURL, {
    scope: import.meta.env.BASE_URL,
  })
  await navigator.serviceWorker.ready
  await reg.showNotification(r.title, options)
}

/** Keeps this page's reminders in step with the plan while it is open:
 * each the moment it is due, never one whose moment has passed. A browser
 * runs no page that is closed, so none comes then. */
export function useReminders(
  tasks: TaskItem[],
  lists: TaskList[],
  settings: Settings,
  now: Date
): { permission: Permission; planned: number; ask: () => Promise<void> } {
  const [permission, setPermission] = useState(readPermission)

  // The browser's answer may change in its own settings while the page is
  // open; it is read again when the owner comes back to the page.
  useEffect(() => {
    const read = () => setPermission(readPermission())
    document.addEventListener("visibilitychange", read)
    window.addEventListener("focus", read)
    return () => {
      document.removeEventListener("visibilitychange", read)
      window.removeEventListener("focus", read)
    }
  }, [])

  const reminders = useMemo(
    () => plan(tasks, lists, settings, permission, now),
    [tasks, lists, settings, permission, now]
  )

  useEffect(() => {
    const latest = () => tasks
    const timers = reminders
      // setTimeout holds at most 2^31 - 1 ms; a later one is planned again
      // as the minutes pass.
      .filter((r) => r.at.getTime() - Date.now() < 2 ** 31 - 1)
      .map((r) =>
        window.setTimeout(
          () => void notify(r, latest),
          Math.max(0, r.at.getTime() - Date.now())
        )
      )
    return () => timers.forEach((t) => clearTimeout(t))
  }, [reminders, tasks])

  // A tap on a reminder the worker showed comes back as a message.
  useEffect(() => {
    if (!("serviceWorker" in navigator)) return
    const onMessage = (e: MessageEvent<unknown>) => {
      const data = e.data as { type?: unknown; task?: unknown } | null
      if (data?.type === "tasks.reminder" && typeof data.task === "string")
        opened(data.task, () => tasks)
    }
    navigator.serviceWorker.addEventListener("message", onMessage)
    return () =>
      navigator.serviceWorker.removeEventListener("message", onMessage)
  }, [tasks])

  return {
    permission,
    planned: reminders.length,
    // Asked when the owner turns reminders on, never before: the browser
    // asks once, and only an answer to a choice they just made is one they
    // expect.
    ask: async () => {
      if (readPermission() === "notAsked")
        await Notification.requestPermission()
      setPermission(readPermission())
    },
  }
}
