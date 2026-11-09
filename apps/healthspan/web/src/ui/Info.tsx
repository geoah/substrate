import { cx } from "@substrate/react"
import type { ReactNode } from "react"

/** The parts of a card's info panel. Each label sits above its text, never
 * beside it in a column, so a long one wraps rather than squeezing. */

/** The panel's heading, with an optional quiet label beside it (a panel's
 * name). */
export function InfoTitle(props: { children: ReactNode; badge?: ReactNode }) {
  return (
    <div className="mb-2 flex flex-wrap items-center gap-x-2 gap-y-1">
      <h4 className="text-[17px] font-semibold">{props.children}</h4>
      {props.badge}
    </div>
  )
}

/** One part: an optional label over its text. */
export function InfoBlock(props: {
  label?: string
  children: ReactNode
  muted?: boolean
}) {
  return (
    <div className="mt-2 first:mt-0">
      {props.label && (
        <p className="text-[13px] text-secondary">{props.label}</p>
      )}
      <div
        className={cx(
          "text-[15px] leading-snug",
          props.muted && "text-secondary"
        )}
      >
        {props.children}
      </div>
    </div>
  )
}

/** The published sources a panel cites, one link a line. */
export function InfoSources(props: {
  sources: { name: string; url: string }[]
  label?: string
}) {
  if (!props.sources.length) return null
  return (
    <InfoBlock label={props.label ?? "Sources"}>
      <ul className="flex flex-col gap-1">
        {props.sources.map((s) => (
          <li key={s.url}>
            <a
              href={s.url}
              target="_blank"
              rel="noreferrer"
              className="text-accent-text underline-offset-2 hover:underline"
            >
              {s.name}
            </a>
          </li>
        ))}
      </ul>
    </InfoBlock>
  )
}
