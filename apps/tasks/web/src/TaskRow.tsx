import { cx } from "@substrate/react"
import {
  CalendarDays,
  Check,
  Clock,
  Flag,
  Layers,
  ListIcon,
  Sparkle,
  X,
} from "lucide-react"
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type HTMLAttributes,
  type PointerEvent,
  type Ref,
} from "react"
import { undoWindow, type TaskActions } from "./actions"
import { MenuButton } from "./Menu"
import { priorityWords, type Priority, type Status, type Tone } from "./model"
import type { RowView } from "./present"
import { words } from "./words"

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
  disabled?: boolean
}) {
  const { pending } = props
  const filled = pending !== undefined || props.status === "done"
  const ring = useRef<SVGCircleElement>(null)
  // The ring is the window: it starts where the window stands, so a mark
  // drawn again mid-window (the task opened) drains from there.
  useEffect(() => {
    if (pending === undefined || !ring.current) return
    const drain = ring.current.animate(
      [{ strokeDashoffset: 0 }, { strokeDashoffset: 100 }],
      { duration: undoWindow, fill: "forwards" }
    )
    drain.currentTime = Math.max(
      0,
      Math.min(undoWindow, undoWindow - (pending - Date.now()))
    )
    return () => drain.cancel()
  }, [pending])
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        props.onToggle()
      }}
      onPointerDown={(e) => e.stopPropagation()}
      aria-label={props.label}
      disabled={props.disabled}
      className="group relative flex size-11 shrink-0 items-center justify-center rounded-full outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-default"
    >
      {pending !== undefined && (
        <svg
          viewBox="0 0 36 36"
          className="absolute size-9 -rotate-90"
          aria-hidden
        >
          <circle
            ref={ring}
            cx="18"
            cy="18"
            r="16.5"
            fill="none"
            pathLength={100}
            strokeDasharray="100"
            strokeLinecap="round"
            strokeWidth="2.5"
            className="stroke-accent/55"
          />
        </svg>
      )}
      <span
        className={cx(
          "flex size-[26px] items-center justify-center rounded-full border-[1.5px] transition-[transform,background-color,border-color] duration-200",
          filled
            ? "border-accent bg-accent text-white"
            : "border-tertiary group-hover:border-accent",
          pending !== undefined && "scale-[1.08]",
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
 * late task's row also offers where to move it and Won't Do. A finger
 * swipes it as on a phone: from the left, what the mark does (all the way
 * across does it); from the right, Delete and Won't Do, never done by the
 * swipe alone. */
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
  const suggestion = view.suggestion
  const open = t.status === "open" || !!suggestion
  const struck = (!open || view.pending) && !suggestion
  const chips =
    open &&
    (view.due ||
      view.priority ||
      view.priorityLabel ||
      view.listName ||
      view.collection ||
      suggestion)
  const toggle = () =>
    t.status === "open" && !view.pending
      ? actions.complete(t.id)
      : actions.reopen(t.id)
  const writable = !t.item.collection.viewOnly
  // Buttons rather than a mark trail the row: where the row is too narrow
  // for them and a readable title (a phone at a large text size), they go
  // under the title rather than squeeze it.
  const wide =
    (!!suggestion && (suggestion.accept || suggestion.dismiss)) ||
    !!view.decision
  const swipe = useSwipe({
    trailing: suggestion ? (suggestion.dismiss ? 1 : 0) : open ? 2 : 1,
    leading: suggestion ? suggestion.accept : view.toggles,
    onFull: () => (suggestion ? actions.act(t.id, "accept") : toggle()),
  })
  return (
    <div
      ref={elementRef}
      {...rest}
      onPointerDown={(e) => {
        rest.onPointerDown?.(e)
        swipe.handlers.onPointerDown(e)
      }}
      onPointerMove={swipe.handlers.onPointerMove}
      onPointerUp={swipe.handlers.onPointerUp}
      onPointerCancel={swipe.handlers.onPointerCancel}
      className={cx(
        "relative w-full touch-pan-y overflow-hidden bg-card select-none [-webkit-touch-callout:none]",
        className
      )}
    >
      {swipe.offset > 0 && (
        <div className="absolute inset-y-0 left-0 flex">
          <SwipeButton
            label={suggestion ? words.suggestionRow.accept : view.toggle}
            className="bg-ok"
            width={Math.max(swipeWidth, swipe.offset)}
            onClick={() => {
              swipe.close()
              if (suggestion) actions.act(t.id, "accept")
              else toggle()
            }}
          />
        </div>
      )}
      {swipe.offset < 0 && suggestion && (
        <div className="absolute inset-y-0 right-0 flex">
          <SwipeButton
            label={words.suggestionRow.dismiss}
            className="bg-[#8e8e93]"
            width={swipeWidth}
            onClick={() => {
              swipe.close()
              actions.act(t.id, "dismiss")
            }}
          />
        </div>
      )}
      {swipe.offset < 0 && !suggestion && (
        <div className="absolute inset-y-0 right-0 flex">
          {open && t.item.actions.wontDo.length > 0 && (
            <SwipeButton
              label="Won't Do"
              className="bg-[#8e8e93]"
              width={swipeWidth}
              onClick={() => {
                swipe.close()
                actions.abandon(t.id)
              }}
            />
          )}
          {writable && (
            <SwipeButton
              label="Delete"
              className="bg-danger"
              width={swipeWidth}
              onClick={() => {
                swipe.close()
                actions.deleteTask(t.id)
              }}
            />
          )}
        </div>
      )}
      <div
        style={{
          transform: swipe.offset ? `translateX(${swipe.offset}px)` : undefined,
          transition: swipe.dragging ? undefined : "transform 200ms ease",
        }}
        className={cx(
          "relative flex items-center gap-1 pr-1.5 pl-4",
          wide && "flex-wrap",
          selected
            ? "bg-[color-mix(in_srgb,var(--sx-accent)_9%,var(--sx-card))]"
            : "bg-card"
        )}
      >
        <div
          className={cx(
            "min-w-0 flex-1 py-2.5 transition-opacity",
            wide && "min-w-[min(100%,11rem)]",
            (faded || !open) && "opacity-[0.62]"
          )}
        >
          <button
            type="button"
            onClick={() => (swipe.offset ? swipe.close() : onOpen())}
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
              {suggestion && (
                <Sparkle
                  className="mr-1.5 mb-0.5 inline size-3.5 fill-accent text-accent"
                  aria-hidden
                />
              )}
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
                {suggestion && view.collection && (
                  <Chip
                    icon={<Layers className="size-3.5 shrink-0" />}
                    text={view.collection}
                    className="text-secondary"
                  />
                )}
                {suggestion && (
                  <Chip
                    icon={<Sparkle className="size-3.5 shrink-0" />}
                    text={suggestion.written}
                    className="text-secondary"
                  />
                )}
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
                    text={priorityWords[view.priority]}
                    className={priorityClass[view.priority]}
                  />
                )}
                {view.priorityLabel && (
                  <Chip
                    icon={<Flag className="size-3.5 shrink-0" />}
                    text={view.priorityLabel.text}
                    className={
                      view.priorityLabel.pressing
                        ? "text-danger"
                        : "text-secondary"
                    }
                  />
                )}
                {view.listName && (
                  <Chip
                    icon={<ListIcon className="size-3.5 shrink-0" />}
                    text={view.listName}
                    className="text-secondary"
                  />
                )}
                {suggestion?.viewOnly && (
                  <span className="text-[12px] leading-4 font-medium text-secondary">
                    {words.viewOnly}
                  </span>
                )}
                {!suggestion && view.collection && (
                  <Chip
                    icon={<Layers className="size-3.5 shrink-0" />}
                    text={view.collection}
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
        {suggestion ? (
          <SuggestionControls
            view={view}
            onAct={(action) => actions.act(t.id, action)}
            onUndo={() => actions.reopen(t.id)}
          />
        ) : view.decision && view.decision.action !== "complete" ? (
          <UndoControl
            words={words.undo[view.decision.action]}
            onUndo={() => actions.reopen(t.id)}
          />
        ) : (
          <span className={cx("flex", (faded || !open) && "opacity-[0.62]")}>
            <StatusMark
              status={t.status}
              pending={pending}
              label={view.toggle}
              onToggle={toggle}
              disabled={!view.toggles}
            />
          </span>
        )}
      </div>
    </div>
  )
}

/** A suggestion's trailing control: Dismiss beside Accept, or while its
 * decision waits out the window, what was decided and Undo. */
function SuggestionControls(props: {
  view: RowView
  onAct: (action: "accept" | "dismiss") => void
  onUndo: () => void
}) {
  const s = props.view.suggestion!
  const d = props.view.decision
  if (d)
    return <UndoControl words={words.undo[d.action]} onUndo={props.onUndo} />
  if (!s.accept && !s.dismiss) return null
  return (
    <span
      className="ml-auto flex shrink-0 items-center gap-2 pr-1.5 pl-1 @max-sm:pb-2.5"
      onPointerDown={(e) => e.stopPropagation()}
    >
      {s.dismiss && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            props.onAct("dismiss")
          }}
          aria-label={`${words.suggestionRow.dismiss} ${props.view.title}`}
          title={`${words.suggestionRow.dismiss} (D)`}
          className="flex size-11 items-center justify-center rounded-full bg-fill text-secondary outline-none hover:bg-selected focus-visible:ring-2 focus-visible:ring-accent"
        >
          <X className="size-4" strokeWidth={2.5} />
        </button>
      )}
      {s.accept && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            props.onAct("accept")
          }}
          aria-label={`${words.suggestionRow.accept} ${props.view.title}`}
          title={`${words.suggestionRow.accept} (A)`}
          className="h-11 rounded-full bg-accent px-4 text-[15px] font-semibold text-white outline-none hover:opacity-90 focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2"
        >
          {words.suggestionRow.accept}
        </button>
      )}
    </span>
  )
}

/** A decision waiting out its window, said, with its Undo. */
function UndoControl(props: { words: string; onUndo: () => void }) {
  return (
    <span
      className="ml-auto flex shrink-0 items-center gap-2 pr-1.5 pl-1 @max-sm:pb-2.5"
      onPointerDown={(e) => e.stopPropagation()}
    >
      <span className="text-[15px] text-secondary">{props.words}</span>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation()
          props.onUndo()
        }}
        className="h-11 rounded-full bg-fill px-4 text-[15px] font-semibold outline-none hover:bg-selected focus-visible:ring-2 focus-visible:ring-accent"
      >
        {words.undo.action}
      </button>
    </span>
  )
}

const swipeWidth = 88

function SwipeButton(props: {
  label: string
  className: string
  width: number
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={props.onClick}
      style={{ width: props.width }}
      className={cx(
        "flex h-full items-center justify-center px-2 text-[15px] leading-tight font-semibold text-white",
        props.className
      )}
    >
      {props.label}
    </button>
  )
}

/** The row a finger left swiped open: one at a time, as a list's rows are. */
let closeOpenRow: (() => void) | undefined

/** A row's swipe: a finger moving sideways before it holds the row slides
 * it, and it settles shut, open on its buttons, or, past half the width
 * from the left, does its full-swipe action. */
function useSwipe(opts: {
  trailing: number
  leading: boolean
  onFull: () => void
}) {
  const [offset, setOffset] = useState(0)
  const [dragging, setDragging] = useState(false)
  const gesture = useRef<
    | {
        id: number
        x: number
        y: number
        at: number
        base: number
        width: number
        active: boolean
      }
    | undefined
  >(undefined)
  const close = useCallback(() => setOffset(0), [])
  const lead = opts.leading ? swipeWidth : 0
  const trail = swipeWidth * opts.trailing

  useEffect(() => {
    if (!offset) return
    if (closeOpenRow !== close) closeOpenRow?.()
    closeOpenRow = close
    return () => {
      if (closeOpenRow === close) closeOpenRow = undefined
    }
  }, [offset, close])

  const handlers = {
    onPointerDown: (e: PointerEvent<HTMLDivElement>) => {
      if (e.pointerType !== "touch" || !e.isPrimary) return
      gesture.current = {
        id: e.pointerId,
        x: e.clientX,
        y: e.clientY,
        at: Date.now(),
        base: offset,
        width: e.currentTarget.offsetWidth,
        active: false,
      }
    },
    onPointerMove: (e: PointerEvent<HTMLDivElement>) => {
      const g = gesture.current
      if (!g || e.pointerId !== g.id) return
      const dx = e.clientX - g.x
      const dy = e.clientY - g.y
      if (!g.active) {
        // A hold first is the row's drag or its menu, not a swipe.
        if (Date.now() - g.at > 400 || Math.abs(dy) > 10) {
          gesture.current = undefined
          return
        }
        if (Math.abs(dx) < 10 || Math.abs(dx) < Math.abs(dy) * 1.2) return
        g.active = true
        setDragging(true)
        e.currentTarget.setPointerCapture(e.pointerId)
      }
      setOffset(
        Math.max(
          trail ? -trail - 24 : 0,
          Math.min(lead ? g.width * 0.9 : 0, g.base + dx)
        )
      )
    },
    onPointerUp: (e: PointerEvent<HTMLDivElement>) => {
      const g = gesture.current
      gesture.current = undefined
      if (!g?.active || e.pointerId !== g.id) return
      setDragging(false)
      const x = g.base + (e.clientX - g.x)
      if (lead && x > g.width * 0.5) {
        setOffset(0)
        opts.onFull()
      } else if (lead && x > lead / 2) setOffset(lead)
      else if (trail && x < -trail / 2) setOffset(-trail)
      else setOffset(0)
    },
    onPointerCancel: () => {
      if (gesture.current?.active) setDragging(false)
      gesture.current = undefined
      setOffset((o) => (o > lead / 2 ? lead : o < -trail / 2 ? -trail : 0))
    },
  }
  return { offset, dragging, close, handlers }
}
