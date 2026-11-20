import { cx } from "@substrate/react"
import type { ReactNode } from "react"

/** A card screen's toolbar, for `Screen`'s `toolbar` slot: the search
 * first, the filters after it, and the end cluster (the Details switch,
 * the display settings) pushed to the far end. Everything wraps onto more
 * lines on a narrow screen. */
export function Toolbar(props: {
  search?: ReactNode
  children?: ReactNode
  end?: ReactNode
  className?: string
}) {
  return (
    <div
      className={cx(
        "flex w-full min-w-0 flex-wrap items-center gap-2",
        props.className
      )}
    >
      {props.search && (
        <div className="min-w-[180px] flex-[1_1_200px] sm:max-w-[260px]">
          {props.search}
        </div>
      )}
      {props.children}
      {props.end && (
        <div className="ml-auto flex shrink-0 items-center gap-2">
          {props.end}
        </div>
      )}
    </div>
  )
}
