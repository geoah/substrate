/** Copies one value. Sits beside an identifier; never inside a link, and a
 * click on it never reaches the row or link around it. */

import { useEffect, useState } from "react"
import { CheckIcon, CopyIcon } from "lucide-react"

import { cn } from "@/lib/utils"

export function CopyButton({
  value,
  label,
  className,
}: {
  value: string
  /** What is copied, for the accessible name: "Copy record id". */
  label?: string
  className?: string
}) {
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    if (!copied) return undefined
    const timer = setTimeout(() => setCopied(false), 1500)
    return () => clearTimeout(timer)
  }, [copied])
  const Icon = copied ? CheckIcon : CopyIcon
  return (
    <button
      type="button"
      aria-label={label ?? `Copy ${value}`}
      title={copied ? "Copied" : "Copy"}
      className={cn(
        "inline-grid size-5 shrink-0 cursor-pointer place-items-center rounded-[4px] align-middle text-faint transition-colors hover:bg-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:outline-none",
        className
      )}
      onClick={(event) => {
        event.preventDefault()
        event.stopPropagation()
        void navigator.clipboard?.writeText(value).then(
          () => setCopied(true),
          () => {}
        )
      }}
    >
      <Icon aria-hidden className="size-[13px]" />
    </button>
  )
}
