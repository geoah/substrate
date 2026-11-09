import { cx } from "@substrate/react"
import { History, Info, Lightbulb } from "lucide-react"
import type { ReactNode } from "react"
import { IconButton } from "./IconButton"
import { Popover } from "./Popover"
import { INK } from "./tone"

export interface MetricCardHeadline {
  /** The big number, formatted ("3.42", "7:18 h", "46%"). */
  value: string
  /** After the value, small and muted ("mmol/L"). */
  unit?: string
  /** The readout line under the value: a date and source, or a period
   * ("25 Sept 2026, Lab A"; "night to 24 Oct"). A node marks the part
   * that gives way first (a long source name, as `Truncates`). */
  context?: ReactNode
  /** A second line under it, reserved whenever given (Labs: the shown
   * result's ranges). Omitted, nothing is reserved. */
  detail?: ReactNode
  /** The end of the headline row: a TrendChip, a status Badge, or a label
   * over a value (`HeadlineStack`). */
  end?: ReactNode
  /** While the headline shows a hovered point rather than the newest
   * value; sets `data-selected`. */
  isSelected?: boolean
}

export interface MetricCardProps {
  /** Element id and `data-metric`, so a link can point at the card. */
  id: string
  title: ReactNode
  /** Badges after the title: the status word, quiet labels. */
  badges?: ReactNode
  /** The info button's panel: its name ("About LDL cholesterol") and what
   * it holds (`InfoBlock`s and `InfoSources`). */
  info?: { label: string; content: ReactNode }
  /** The history button: its name ("LDL cholesterol history") and where it
   * goes, a route's hash. */
  history?: { label: string; href: string }
  /** More buttons at the header's end, after info and history. */
  actions?: ReactNode
  /** A bar in the tone down the card's leading edge. */
  edge?: "danger" | "warning" | "info"
  /** `data-status`, for the filters' checks. */
  status?: string
  headline: MetricCardHeadline
  /** The chart, the ZoneBar, a row of facts or a muted line. Fills the
   * width; it starts at the same height on every card of a row. */
  visual: ReactNode
  /** The suggestion under the visual, a lightbulb before it. Space for
   * three lines is kept on a narrow card and two on a wide one, so a row's
   * charts stay level. */
  note?: string
  /** The details under the visual, shown on every card while the
   * screen's Details switch is on (`isOpen`). */
  details?: { isOpen: boolean; children: ReactNode }
  /** Rings the card for three seconds; the screen clears it. */
  isHighlighted?: boolean
}

/** One metric as a card in a grid, the anatomy Labs and Healthspan share:
 * the title and its badges with the info and history buttons, the
 * headline (the value, its unit and the end slot), the readout line, the
 * visual, the note and the details. The screen builds every part; the card
 * lays them out. A hovered point swaps the headline's value and readout
 * line in place, and the lines keep their height, so the chart never
 * moves. */
export function MetricCard({
  id,
  title,
  badges,
  info,
  history,
  actions,
  edge,
  status,
  headline,
  visual,
  note,
  details,
  isHighlighted,
}: MetricCardProps) {
  const text = (n: ReactNode) => (typeof n === "string" && n ? n : undefined)
  return (
    <article
      id={id}
      data-metric={id}
      data-status={status}
      data-edge={edge}
      data-selected={headline.isSelected ? "true" : undefined}
      aria-labelledby={`${id}-title`}
      className={cx(
        "@container relative flex min-w-0 scroll-mt-20 flex-col rounded-[10px] bg-card px-4 pt-3 pb-4",
        isHighlighted &&
          "after:animate-highlight after:pointer-events-none after:absolute after:-inset-0.5 after:rounded-[12px] after:border-2 after:border-accent motion-reduce:after:animate-none"
      )}
      style={edge ? { boxShadow: `inset 3px 0 0 ${INK[edge]}` } : undefined}
    >
      <div className="flex min-h-9 min-w-0 items-start gap-2">
        {/* The title and its badges wrap among themselves; the buttons stay
            at the end of the first line. */}
        <div className="flex min-h-9 min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-1">
          <h3
            id={`${id}-title`}
            className="min-w-0 text-[15px] leading-5 font-semibold break-words"
          >
            {title}
          </h3>
          {badges}
        </div>
        {(info || history || actions) && (
          <span className="-mr-2 flex shrink-0 items-center">
            {info && (
              <Popover
                title={info.label}
                trigger={({ isOpen, triggerProps }) => (
                  <IconButton
                    label={info.label}
                    active={isOpen}
                    {...triggerProps}
                  >
                    <Info className="size-[18px]" aria-hidden />
                  </IconButton>
                )}
              >
                {info.content}
              </Popover>
            )}
            {history && (
              <IconButton label={history.label} href={history.href}>
                <History className="size-[18px]" aria-hidden />
              </IconButton>
            )}
            {actions}
          </span>
        )}
      </div>
      <div className="mt-1 flex min-h-[30px] min-w-0 items-center gap-2">
        <span className="shrink-0 text-[24px] leading-[30px] font-semibold whitespace-nowrap tabular-nums">
          {headline.value}
          {headline.unit && (
            <span className="text-[13px] font-normal text-secondary">
              {" "}
              {headline.unit}
            </span>
          )}
        </span>
        {headline.end !== undefined && (
          // The end gives way first: a long chip ends in an ellipsis.
          <span className="ml-auto flex min-w-0 flex-[0_1_auto] justify-end text-right">
            {headline.end}
          </span>
        )}
      </div>
      {/* Always drawn, so swapping the newest value for a hovered one never
          moves the chart. */}
      <p
        className="flex min-w-0 overflow-hidden text-[13px] leading-[19px] whitespace-nowrap text-secondary"
        title={text(headline.context)}
        aria-live="polite"
      >
        {typeof headline.context === "string" || !headline.context ? (
          <span className="min-w-0 truncate">{headline.context || " "}</span>
        ) : (
          headline.context
        )}
      </p>
      {headline.detail !== undefined && (
        <p
          className="truncate text-[13px] leading-[19px] text-secondary"
          title={text(headline.detail)}
        >
          {headline.detail || " "}
        </p>
      )}
      <div className="mt-auto min-w-0 pt-2">{visual}</div>
      {note !== undefined && (
        <p className="mt-2 flex min-h-[57px] items-start gap-1.5 text-[13px] leading-[19px] @min-[298px]:min-h-[38px]">
          <Lightbulb
            className="mt-[3px] size-[13px] shrink-0 text-secondary"
            aria-hidden
          />
          <span className="min-w-0">{note}</span>
        </p>
      )}
      {details?.isOpen && (
        <div className="mt-2 border-t border-separator/60 pt-2">
          {details.children}
        </div>
      )}
    </article>
  )
}

/** In a readout line, the part that keeps its width ("Selected: 27 Sept
 * 2025, "). */
export function Keeps({ children }: { children: ReactNode }) {
  return <span className="shrink-0 whitespace-pre">{children}</span>
}

/** In a readout line, the part that gives way first, ending in an
 * ellipsis with its whole text on hover (a long source name). */
export function Truncates({ children }: { children: string }) {
  return (
    <span className="min-w-0 truncate" title={children}>
      {children}
    </span>
  )
}

/** A label over a value in the headline's end slot: "Age score effect"
 * over "-0.5 years". */
export function HeadlineStack(props: { label: string; value: ReactNode }) {
  return (
    <span className="flex flex-col items-end">
      <span className="text-[12px] leading-4 text-secondary">
        {props.label}
      </span>
      <span className="text-[15px] leading-5 tabular-nums">{props.value}</span>
    </span>
  )
}

/** The grid of metric cards: as many columns of at least 280 px as fit
 * (260 px in the wide view), every card of a row as tall as the tallest.
 * `lib/display`'s `gridColumns` counts the same. */
export function CardGrid({
  wide,
  children,
  className,
}: {
  wide?: boolean
  children: ReactNode
  className?: string
}) {
  return (
    <div
      className={cx(
        "grid gap-4",
        wide
          ? "grid-cols-[repeat(auto-fill,minmax(260px,1fr))]"
          : "grid-cols-[repeat(auto-fill,minmax(280px,1fr))]",
        className
      )}
    >
      {children}
    </div>
  )
}
