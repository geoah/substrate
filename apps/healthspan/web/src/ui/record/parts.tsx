import { cx } from "@substrate/react"
import { ChevronDown, ChevronsUpDown } from "lucide-react"
import { useState, type ReactNode } from "react"
import { formatDate } from "../../lib/format"
import type { Finding } from "../../rules/types"
import { Badge } from "../Badge"
import { EmptyState } from "../EmptyState"
import { TextButton } from "../IconButton"

const CARD =
  "overflow-hidden rounded-[10px] bg-card [&>*+*]:relative [&>*+*]:before:absolute [&>*+*]:before:inset-x-0 [&>*+*]:before:top-0 [&>*+*]:before:ml-4 [&>*+*]:before:h-px [&>*+*]:before:scale-y-50 [&>*+*]:before:bg-separator"

/** One section of the record: its heading with how many rows it holds,
 * an action beside them, the rows on a card, and what follows the card
 * (the past ones folded, Show all). */
export function RecordSection(props: {
  id: string
  title: string
  count?: number
  action?: ReactNode
  /** Shown on the card where there are no rows. */
  empty?: string
  after?: ReactNode
  children?: ReactNode
}) {
  const rows = props.children
  const hasRows = Array.isArray(rows) ? rows.length > 0 : !!rows
  return (
    <section
      id={`record-${props.id}`}
      aria-labelledby={`record-${props.id}-title`}
      className="mb-7 scroll-mt-16"
    >
      <div className="mb-1.5 flex min-h-6 items-center gap-3 px-4">
        <h2
          id={`record-${props.id}-title`}
          className="min-w-0 flex-1 text-[13px] tracking-wide text-secondary uppercase"
        >
          {props.title}
        </h2>
        {props.count !== undefined && (
          <span className="text-[13px] text-secondary tabular-nums">
            <span className="sr-only">Count: </span>
            {props.count}
          </span>
        )}
        {props.action}
      </div>
      {(hasRows || props.empty) && (
        <div className={CARD}>
          {hasRows ? (
            rows
          ) : (
            <EmptyState
              title={props.empty ?? ""}
              variant="compact"
              className="px-4"
            />
          )}
        </div>
      )}
      {props.after}
    </section>
  )
}

/** Rows on a card of their own, under a fold or a Show all. */
export function RecordCard(props: { children: ReactNode; className?: string }) {
  return <div className={cx(CARD, props.className)}>{props.children}</div>
}

/** The date a row is dated by, or nothing. */
export function RowDate(props: { at?: string }) {
  if (!props.at) return null
  return (
    <time
      dateTime={props.at}
      className="text-[13px] whitespace-nowrap text-secondary tabular-nums"
    >
      {formatDate(props.at)}
    </time>
  )
}

/** One row of the record: its title, a line of facts under it, its status
 * and date at the end (under it where the screen is narrow), then what is
 * always shown, then what it unfolds to. With `detail` the head is the
 * button that unfolds it. */
export function RecordRow(props: {
  title: ReactNode
  description?: ReactNode
  /** The status words and the date. */
  meta?: ReactNode
  /** Shown under the head, always. */
  body?: ReactNode
  /** Shown once unfolded. */
  detail?: ReactNode
  expanded?: boolean
  onExpandedChange?: (expanded: boolean) => void
  /** Pointed at from a link: marked for a moment. */
  highlighted?: boolean
  id?: string
}) {
  const [own, setOwn] = useState(false)
  const open = props.expanded ?? own
  const toggle = () => {
    if (props.expanded === undefined) setOwn(!open)
    props.onExpandedChange?.(!open)
  }
  const head = (
    <>
      <span className="flex flex-wrap items-start gap-x-3 gap-y-1">
        <span className="min-w-[min(100%,10rem)] flex-1 text-[17px] break-words">
          {props.title}
        </span>
        {(props.meta || props.detail) && (
          <span className="ml-auto flex shrink-0 items-center gap-2 pt-0.5">
            {props.meta}
            {props.detail && (
              <ChevronDown
                aria-hidden
                className={cx(
                  "size-4 shrink-0 text-secondary motion-safe:transition-transform",
                  open && "rotate-180"
                )}
              />
            )}
          </span>
        )}
      </span>
      {props.description && (
        <span className="mt-0.5 block text-[15px] break-words text-secondary">
          {props.description}
        </span>
      )}
    </>
  )
  const headClass = "block w-full text-left"
  return (
    <div
      id={props.id}
      className={cx(
        "scroll-mt-20 px-4 py-2.5",
        props.highlighted &&
          "after:animate-highlight relative after:pointer-events-none after:absolute after:inset-0.5 after:rounded-[8px] after:border-2 after:border-accent motion-reduce:after:animate-none"
      )}
    >
      {props.detail ? (
        <button
          type="button"
          aria-expanded={open}
          onClick={toggle}
          className={cx(headClass, "min-h-11 rounded-[6px]")}
        >
          {head}
        </button>
      ) : (
        <div className={headClass}>{head}</div>
      )}
      {props.body && <div className="mt-2">{props.body}</div>}
      {props.detail && open && (
        <div className="mt-2 text-[15px]">{props.detail}</div>
      )}
    </div>
  )
}

/** Labelled facts, each label above its value, so neither is cut. */
export function Facts(props: { items: { label: string; value: ReactNode }[] }) {
  if (!props.items.length) return null
  return (
    <dl className="flex flex-col gap-2">
      {props.items.map((it) => (
        <div key={it.label}>
          <dt className="text-[13px] text-secondary">{it.label}</dt>
          <dd className="text-[15px] break-words">{it.value}</dd>
        </div>
      ))}
    </dl>
  )
}

/** One finding: what, why in a sentence, its word and the date it is
 * from, opening where it points. */
export function FindingRow({ finding: f }: { finding: Finding }) {
  return (
    <a href={f.href} className="block min-h-11 px-4 py-2.5 active:bg-selected">
      <span className="flex flex-wrap items-start gap-x-3 gap-y-1">
        <span className="min-w-[min(100%,10rem)] flex-1 text-[17px] break-words">
          {f.title}
        </span>
        <span className="ml-auto flex shrink-0 items-center gap-2 pt-0.5">
          <Badge label={f.word} tone={f.tone} />
          <RowDate at={f.since} />
        </span>
      </span>
      <span className="mt-0.5 block text-[15px] break-words text-secondary">
        {f.detail}
      </span>
    </a>
  )
}

/** A list's first ten, and the button showing the rest. */
export function useFirstTen<T>(
  list: T[],
  /** Show them all where this row is further down. */
  isOpen: (item: T) => boolean
): { shown: T[]; more: ReactNode } {
  const [all, setAll] = useState(false)
  const deep = list.findIndex(isOpen) >= 10
  const shown = all || deep ? list : list.slice(0, 10)
  const more =
    shown.length < list.length ? (
      <TextButton className="px-4" onClick={() => setAll(true)}>
        {`Show all ${list.length}`}
      </TextButton>
    ) : null
  return { shown, more }
}

/** A short choice in a toolbar whose options may be long (a provider's
 * name): the label and the chosen option on a pill, the platform's picker
 * over it, so it fits any width. */
export function PickerPill<T extends string>(props: {
  label: string
  value: T
  options: { value: T; label: string }[]
  onChange: (value: T) => void
}) {
  const current =
    props.options.find((o) => o.value === props.value)?.label ?? props.value
  return (
    <label className="relative inline-flex h-8 max-w-full min-w-0 items-center gap-1.5 rounded-full bg-fill px-3 text-[13px] font-medium hover:bg-selected has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-accent">
      <span className="shrink-0 text-secondary">{props.label}</span>
      <span className="min-w-0 truncate">{current}</span>
      <ChevronsUpDown
        className="size-3.5 shrink-0 text-secondary"
        aria-hidden
      />
      <select
        className="absolute inset-0 cursor-pointer opacity-0"
        aria-label={props.label}
        value={props.value}
        onChange={(e) => props.onChange(e.target.value as T)}
      >
        {props.options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </label>
  )
}
