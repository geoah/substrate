import { cx } from "@substrate/react"
import { ChevronDown } from "lucide-react"

/** A filter that picks one of many (a panel, a source) where a row of
 * toggles would not fit: the platform's own picker, drawn as a pill that
 * names the filter and its choice ("Panel: All panels"). `block` fills its
 * row, for the Filters sheet. */
export function FilterSelect<T extends string>(props: {
  label: string
  value: T
  options: { value: T; label: string }[]
  onChange: (value: T) => void
  block?: boolean
}) {
  const current =
    props.options.find((o) => o.value === props.value)?.label ?? props.value
  return (
    <label
      className={cx(
        "relative inline-flex h-8 min-w-0 items-center gap-1 rounded-full bg-fill pr-2.5 pl-3 text-[13px] font-medium hover:bg-selected has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-accent",
        props.block ? "w-full" : "max-w-[260px] shrink-0"
      )}
    >
      <span className="shrink-0 text-secondary">{props.label}:</span>
      <span className="min-w-0 flex-1 truncate">{current}</span>
      <ChevronDown className="size-3.5 shrink-0 text-secondary" aria-hidden />
      <select
        className="absolute inset-0 cursor-pointer opacity-0"
        value={props.value}
        aria-label={props.label}
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
