import { cx } from "@substrate/react"
import { CalendarDays, Check, Clock, Flag, ListIcon, X } from "lucide-react"
import type { HTMLAttributes, Ref } from "react"
import { undoWindow, type TaskActions } from "./actions"
import { MenuButton } from "./Menu"
import type { Priority, Status, Tone } from "./model"
import type { RowView } from "./present"

export const toneClass: Record<Tone, string> = {
  quiet: "text-secondary",
  soon: "text-warn",
  overdue: "text-danger",
}

/** A priority's colour, the cards' and the detail's alike: red is the most
 * urgent (and lateness), orange high, the rest quiet. */
export const priorityClass: Record<Priority, string> = {
  none: "text-secondary",
  low: "text-secondary",
  medium: "text-secondary",
  high: "text-warn",
  urgent: "text-danger",
}

/** The round mark: open, done (filled; while the undo window is open, with
 * a ring that drains over it), or abandoned. A second tap within the
 * window takes the mark back. */
export function StatusMark(props: {
  status: Status
  /** When the undo window closes, while it is open. */
  pending?: number
  label: string
  onToggle: () => void
}) {
  const filled = props.pending !== undefined || props.status === "done"
  const left =
    props.pending !== undefined
      ? Math.max(0, props.pending - Date.now())
      : undefined
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        props.onToggle()
      }}
      onPointerDown={(e) => e.stopPropagation()}
      aria-label={props.label}
      className="group relative flex size-11 shrink-0 items-center justify-center rounded-full outline-none focus-visible:ring-2 focus-visible:ring-accent"
    >
      {left !== undefined && (
        <svg
          viewBox="0 0 36 36"
          className="absolute size-9 -rotate-90"
          aria-hidden
        >
          <circle
            cx="18"
            cy="18"
            r="16.5"
            fill="none"
            pathLength={100}
            strokeDasharray="100"
            strokeLinecap="round"
            strokeWidth="2.5"
            className="stroke-accent/55"
            style={{
              animation: `tasks-drain ${undoWindow}ms linear forwards`,
              animationDelay: `${left - undoWindow}ms`,
            }}
          />
        </svg>
      )}
      <span
        className={cx(
          "flex size-[26px] items-center justify-center rounded-full border-[1.5px] transition-[transform,background-color,border-color] duration-200",
          filled
            ? "border-accent bg-accent text-white"
            : "border-tertiary group-hover:border-accent",
          props.pending !== undefined && "scale-[1.08]",
          props.status === "abandoned" && "text-secondary"
        )}
      >
        {filled && <Check className="size-4" strokeWidth={3} />}
        {!filled && props.status === "abandoned" && (
          <X className="size-4" strokeWidth={2.5} />
        )}
      </span>
    </button>
  )
}

/** A fact beside a task's name, each under its own mark. An overdue date is
 * the one filled. */
function Chip(props: {
  icon: React.ReactNode
  text: string
  className: string
  emphasis?: boolean
}) {
  return (
    <span
      className={cx(
        "inline-flex max-w-full items-center gap-1 rounded-full py-0.5 text-[12px] leading-4",
        props.emphasis ? "bg-danger/10 px-1.5 font-semibold" : "font-medium",
        props.className
      )}
    >
      {props.icon}
      <span className="min-w-0 break-words">{props.text}</span>
    </span>
  )
}

/** A task's row in its section's card: its words on the left, which open
 * it, and on the right the round mark, which completes it. On Needs you a
 * late task's row also offers where to move it and Won't Do. */
export function TaskRow(
  props: {
    view: RowView
    pending?: number
    selected?: boolean
    faded?: boolean
    actions: TaskActions
    onOpen: () => void
    elementRef?: Ref<HTMLDivElement>
  } & Omit<HTMLAttributes<HTMLDivElement>, "onToggle">
) {
  const {
    view,
    pending,
    selected,
    faded,
    actions,
    onOpen,
    elementRef,
    className,
    ...rest
  } = props
  const t = view.task
  const open = t.status === "open"
  const struck = !open || view.pending
  const chips = open && (view.due || view.priority || view.listName)
  return (
    <div
      ref={elementRef}
      {...rest}
      className={cx(
        "flex w-full touch-manipulation items-center gap-1 pr-1.5 pl-4 select-none [-webkit-touch-callout:none]",
        selected
          ? "bg-[color-mix(in_srgb,var(--sx-accent)_9%,var(--sx-card))]"
          : "bg-card",
        className
      )}
    >
      <div
        className={cx(
          "min-w-0 flex-1 py-2.5 transition-opacity",
          (faded || !open) && "opacity-[0.62]"
        )}
      >
        <button
          type="button"
          onClick={onOpen}
          aria-label={view.spoken}
          aria-current={selected ? "true" : undefined}
          data-task-row={t.id}
          className="block w-full rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <span
            className={cx(
              "block text-[17px] leading-snug font-medium break-words",
              struck ? "text-secondary line-through" : "text-label"
            )}
          >
            {view.title}
          </span>
          {view.notes && (
            <span
              className={cx(
                "mt-0.5 block truncate text-[15px] text-secondary",
                struck && "line-through"
              )}
            >
              {view.notes}
            </span>
          )}
          {chips && (
            <span className="mt-1 flex flex-wrap items-center gap-x-2.5 gap-y-1">
              {view.due && (
                <Chip
                  icon={<Clock className="size-3.5 shrink-0" />}
                  text={view.due.text}
                  className={toneClass[view.due.tone]}
                  emphasis={view.due.tone === "overdue"}
                />
              )}
              {view.priority && (
                <Chip
                  icon={<Flag className="size-3.5 shrink-0" />}
                  text={view.priority[0].toUpperCase() + view.priority.slice(1)}
                  className={priorityClass[view.priority]}
                />
              )}
              {view.listName && (
                <Chip
                  icon={<ListIcon className="size-3.5 shrink-0" />}
                  text={view.listName}
                  className="text-secondary"
                />
              )}
            </span>
          )}
        </button>
        {(view.reschedule || view.decides) && (
          <div
            className="mt-2 flex flex-wrap gap-2"
            onPointerDown={(e) => e.stopPropagation()}
          >
            {view.reschedule && (
              <MenuButton
                label={view.reschedule.title}
                items={() =>
                  view.reschedule!.options.map((o) => ({
                    label: o.label,
                    onSelect: () => actions.reschedule(t.id, o.how),
                  }))
                }
                className="inline-flex h-9 items-center gap-1.5 rounded-full bg-label px-3.5 text-[15px] font-semibold text-canvas active:opacity-80"
              >
                <CalendarDays className="size-4" />
                {view.reschedule.title}
              </MenuButton>
            )}
            {view.decides && (
              <button
                type="button"
                onClick={() => actions.abandon(t.id)}
                className="inline-flex h-9 items-center rounded-full bg-fill px-3.5 text-[15px] font-semibold active:bg-selected"
              >
                Won't Do
              </button>
            )}
          </div>
        )}
      </div>
      <span className={cx("flex", (faded || !open) && "opacity-[0.62]")}>
        <StatusMark
          status={t.status}
          pending={pending}
          label={view.toggle}
          onToggle={() =>
            open && !view.pending
              ? actions.complete(t.id)
              : actions.reopen(t.id)
          }
        />
      </span>
    </div>
  )
}
