export interface ReadoutLineProps {
  /** The newest value and what it covers. */
  latest: { value: string; context: string }
  /** The active point, if any: its date, its value, and an optional tail
   * ("30-day average 7:05 h"). */
  selected?: { date: string; value: string; extra?: string }
}

/** The readout of a chart that has no card headline to swap: one reserved
 * line above it. At rest it names the newest value, "Latest 7:18 h, night
 * to 24 Oct 2026"; while a point is hovered, stepped or pinned it names
 * that one, "Selected: 12 Jul 2026, 7:12 h, 30-day average 7:05 h". The
 * line keeps its height either way, so the chart never moves. */
export function ReadoutLine({ latest, selected }: ReadoutLineProps) {
  return (
    <p
      className="truncate text-[13px] leading-[19px] text-secondary"
      data-selected={selected ? "true" : undefined}
      aria-live="polite"
    >
      {selected ? (
        <>
          Selected: {selected.date},{" "}
          <span className="text-label tabular-nums">{selected.value}</span>
          {selected.extra ? `, ${selected.extra}` : ""}
        </>
      ) : (
        <>
          Latest <span className="text-label tabular-nums">{latest.value}</span>
          {latest.context ? `, ${latest.context}` : ""}
        </>
      )}
    </p>
  )
}
