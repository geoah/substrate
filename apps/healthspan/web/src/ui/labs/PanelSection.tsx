import { cx } from "@substrate/react"
import { ChevronDown } from "lucide-react"
import { useId, type ReactNode } from "react"

/** One panel of cards under its name, folded by a press on the name; its
 * count of cards beside it. */
export function PanelSection(props: {
  title: string
  count: number
  isCollapsed: boolean
  onCollapsedChange: (collapsed: boolean) => void
  children: ReactNode
}) {
  const id = useId()
  return (
    <section className="mb-6" aria-labelledby={`${id}-title`}>
      <h2 id={`${id}-title`} className="mb-1.5">
        <button
          type="button"
          aria-expanded={!props.isCollapsed}
          aria-controls={id}
          onClick={() => props.onCollapsedChange(!props.isCollapsed)}
          className="flex min-h-9 w-full items-center gap-2 rounded-[8px] px-1 text-left text-[13px] font-normal tracking-wide text-secondary uppercase hover:text-label"
        >
          <span className="min-w-0 flex-1 truncate">{props.title}</span>
          <span className="normal-case tabular-nums">{props.count}</span>
          <ChevronDown
            aria-hidden
            className={cx(
              "size-4 shrink-0 motion-safe:transition-transform",
              props.isCollapsed && "-rotate-90"
            )}
          />
        </button>
      </h2>
      <div id={id} hidden={props.isCollapsed}>
        {props.children}
      </div>
    </section>
  )
}
