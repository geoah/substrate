import { cx } from "@substrate/react"
import {
  ArrowDown,
  CalendarDays,
  Check,
  ListIcon,
  StickyNote,
  X,
} from "lucide-react"
import { dueWords, type Priority, type TaskItem, type Tone } from "./model"

const toneClass: Record<Tone, string> = {
  quiet: "text-secondary",
  soon: "text-warn",
  overdue: "text-danger",
}

const priorityMark: Record<Priority, { mark?: string; className: string }> = {
  none: { className: "" },
  low: { className: "text-secondary" },
  medium: { mark: "!", className: "text-caution" },
  high: { mark: "!!", className: "text-warn" },
  urgent: { mark: "!!!", className: "text-danger" },
}

export function PriorityMark({ priority }: { priority: Priority }) {
  const p = priorityMark[priority]
  if (priority === "none") return null
  return (
    <span className={cx("inline-flex items-center gap-1", p.className)}>
      {p.mark ? (
        <span className="font-bold tracking-tighter">{p.mark}</span>
      ) : (
        <ArrowDown className="size-3.5" />
      )}
      {priority[0].toUpperCase() + priority.slice(1)}
    </span>
  )
}

/** The circle a task is marked done by. */
export function StatusCircle(props: { task: TaskItem; onToggle: () => void }) {
  const { status } = props.task
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        props.onToggle()
      }}
      aria-label={
        status === "open"
          ? `Mark ${props.task.name} as done`
          : `Reopen ${props.task.name}`
      }
      className={cx(
        "mt-0.5 flex size-[22px] shrink-0 items-center justify-center rounded-full border-[1.5px] transition-colors",
        status === "open" && "border-tertiary hover:border-accent",
        status === "done" && "border-accent bg-accent text-white",
        status === "abandoned" && "border-tertiary text-secondary"
      )}
    >
      {status === "done" && <Check className="size-3.5" strokeWidth={3} />}
      {status === "abandoned" && <X className="size-3.5" strokeWidth={2.5} />}
    </button>
  )
}

/** One task in a list: its circle, its name, and a line of what it carries,
 * each with its own mark. */
export function TaskRow(props: {
  task: TaskItem
  now: Date
  listName?: string
  selected?: boolean
  onOpen: () => void
  onToggle: () => void
}) {
  const { task, now } = props
  const due = task.due ? dueWords(task.due, now) : undefined
  const closed = task.status !== "open"
  const hasMeta =
    due || task.priority !== "none" || props.listName || task.description
  return (
    <div
      className={cx(
        "flex w-full items-start gap-3 px-4 py-2.5 has-[button:active]:bg-selected",
        props.selected && "bg-selected"
      )}
    >
      <StatusCircle task={task} onToggle={props.onToggle} />
      <button
        type="button"
        onClick={props.onOpen}
        aria-current={props.selected ? "true" : undefined}
        className="min-w-0 flex-1 text-left outline-none focus-visible:underline"
      >
        <span
          className={cx(
            "block text-[17px] leading-snug break-words",
            closed && "text-secondary"
          )}
        >
          {task.name || <span className="text-tertiary">Untitled</span>}
        </span>
        {hasMeta && (
          <span className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[13px]">
            {due && (
              <span
                className={cx(
                  "inline-flex items-center gap-1",
                  closed ? "text-secondary" : toneClass[due.tone]
                )}
              >
                <CalendarDays className="size-3.5" />
                {due.text}
              </span>
            )}
            <PriorityMark priority={task.priority} />
            {props.listName && (
              <span className="inline-flex items-center gap-1 text-secondary">
                <ListIcon className="size-3.5" />
                {props.listName}
              </span>
            )}
            {task.description && (
              <StickyNote
                className="size-3.5 text-tertiary"
                aria-label="Has notes"
              />
            )}
          </span>
        )}
      </button>
    </div>
  )
}
