import { cx } from "@substrate/react"
import { Search, X } from "lucide-react"
import { useEffect, useRef } from "react"

/** The search field of a screen's toolbar: a magnifier, a search input and
 * a clear button once there is text. Controlled. With `debounce`,
 * `onDebouncedChange` fires that many ms after typing stops; `onSubmit`
 * fires on Enter instead, for a search that costs something. */
export function SearchInput({
  value,
  onChange,
  onDebouncedChange,
  onSubmit,
  debounce = 0,
  placeholder = "Search",
  autoFocus,
  ariaLabel,
  className,
}: {
  value: string
  onChange: (value: string) => void
  onDebouncedChange?: (value: string) => void
  onSubmit?: (value: string) => void
  /** ms to wait after typing stops before `onDebouncedChange`. */
  debounce?: number
  placeholder?: string
  autoFocus?: boolean
  ariaLabel?: string
  className?: string
}) {
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])
  const change = (next: string) => {
    onChange(next)
    if (!onDebouncedChange) return
    clearTimeout(timer.current)
    if (debounce <= 0) onDebouncedChange(next)
    else timer.current = setTimeout(() => onDebouncedChange(next), debounce)
  }
  return (
    // The input shows no ring of its own: the field does, as the kit's
    // pickers do.
    <div
      className={cx(
        "relative flex h-9 min-w-0 items-center rounded-[10px] bg-fill has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-offset-2 has-[input:focus-visible]:outline-accent",
        className
      )}
    >
      <Search
        className="pointer-events-none absolute left-2.5 size-4 text-secondary"
        aria-hidden
      />
      <input
        type="search"
        value={value}
        placeholder={placeholder}
        aria-label={ariaLabel ?? placeholder}
        autoFocus={autoFocus}
        onChange={(e) => change(e.target.value)}
        onKeyDown={
          onSubmit
            ? (e) => {
                if (e.key !== "Enter") return
                e.preventDefault()
                onSubmit(e.currentTarget.value)
              }
            : undefined
        }
        className="h-full w-full min-w-0 bg-transparent pr-9 pl-8 text-[15px] outline-none placeholder:text-secondary focus-visible:outline-none [&::-webkit-search-cancel-button]:hidden"
      />
      {value && (
        <button
          type="button"
          aria-label="Clear search"
          onClick={() => change("")}
          className="absolute right-1 inline-flex size-7 items-center justify-center rounded-full text-secondary hover:bg-selected"
        >
          <X className="size-3.5" aria-hidden />
        </button>
      )}
    </div>
  )
}
