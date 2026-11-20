import { cx } from "@substrate/react"
import { ChevronDown } from "lucide-react"
import { useId, useState, type HTMLAttributes, type ReactNode } from "react"

export interface DisclosureProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  "title"
> {
  /** The words of the button that folds it. */
  title: ReactNode
  /** Open at first, uncontrolled. Default closed. */
  defaultIsOpen?: boolean
  /** Open, controlled; pair with `onOpenChange`. */
  isOpen?: boolean
  onOpenChange?: (isOpen: boolean) => void
  /** A count after the title, inside the button. */
  meta?: ReactNode
  /** Controls at the header's end, beside the button and never inside it. */
  actions?: ReactNode
  children: ReactNode
}

/** Secondary content folded under a button: the chart key on a phone,
 * past prescriptions, how the score works. The whole title row is the
 * button (unless `actions` sit beside it), its chevron turning as it
 * opens. */
export function Disclosure({
  title,
  defaultIsOpen,
  isOpen,
  onOpenChange,
  meta,
  actions,
  className,
  children,
  ...rest
}: DisclosureProps) {
  const panelId = useId()
  const [own, setOwn] = useState(defaultIsOpen ?? false)
  const open = isOpen ?? own
  const toggle = () => {
    if (isOpen === undefined) setOwn(!open)
    onOpenChange?.(!open)
  }
  return (
    <div {...rest} className={className} data-open={open || undefined}>
      <div className="flex items-center gap-2">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={panelId}
          onClick={toggle}
          className="flex min-h-11 min-w-0 flex-1 items-center gap-2 rounded-[8px] text-left text-[15px] font-medium"
        >
          <span className="min-w-0 flex-1">{title}</span>
          {meta != null && (
            <span className="text-[13px] font-normal text-secondary tabular-nums">
              {meta}
            </span>
          )}
          <ChevronDown
            aria-hidden
            className={cx(
              "size-4 shrink-0 text-secondary motion-safe:transition-transform",
              open && "rotate-180"
            )}
          />
        </button>
        {actions != null && (
          <div className="flex shrink-0 items-center gap-2">{actions}</div>
        )}
      </div>
      <div id={panelId} hidden={!open} className="pb-2">
        {children}
      </div>
    </div>
  )
}
