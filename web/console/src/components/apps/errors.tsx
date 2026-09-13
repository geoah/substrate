/* eslint-disable react-refresh/only-export-components -- the strip and the
 * two pure readers it draws with live together so a test can hold the one to
 * the other */
/** The strip above the frame: what went wrong while the app ran, in the
 * order it was reported, newest last. A transform error carries the author's
 * line and column and the strip shows that line of the source under it, a
 * caret beneath the column; a runtime error's line is the author's too
 * because the transform is line-preserving; a grant refusal names the call
 * and the kind the grant lacks; a provenance error says the row needs the
 * owner's review. Folded to the latest line until opened, cleared by the
 * person or by a remount. */

import { useState } from "react"
import { ChevronDownIcon, TriangleAlertIcon, XIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import type { AppError } from "@/lib/apps/spec"
import { cn } from "@/lib/utils"

const PHASE_LABEL: Record<AppError["phase"], string> = {
  transform: "syntax",
  runtime: "runtime",
  grant: "grant",
  provenance: "review",
  host: "host",
}

/** `module:line:column`, as much of it as the error carries. */
export function locationOf(error: AppError): string | undefined {
  if (!error.module && error.line === undefined) return undefined
  const parts = [error.module ?? "source"]
  if (error.line !== undefined) {
    parts.push(String(error.line))
    if (error.column !== undefined) parts.push(String(error.column))
  }
  return parts.join(":")
}

/** The one line of the source or module an error points at. */
export function sourceLine(
  error: AppError,
  source: string,
  modules: Record<string, string>
): string | undefined {
  if (error.line === undefined) return undefined
  const text =
    error.module && error.module !== "source" ? modules[error.module] : source
  if (text === undefined) return undefined
  return text.split("\n")[error.line - 1]
}

function ErrorLine({
  error,
  source,
  modules,
}: {
  error: AppError
  source: string
  modules: Record<string, string>
}) {
  const location = locationOf(error)
  const line = sourceLine(error, source, modules)
  // A one-based column; the caret sits under the character the parser
  // stopped at. Sucrase reports columns one-based as well.
  const caret =
    line !== undefined && error.column !== undefined
      ? " ".repeat(Math.max(0, error.column - 1)) + "^"
      : undefined
  return (
    <li className="flex flex-col gap-1 text-sm">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <span className="rounded border border-warning/40 px-1 text-[0.65rem] tracking-wide text-warning uppercase">
          {PHASE_LABEL[error.phase]}
        </span>
        {location && (
          <span className="data text-xs text-muted-foreground">{location}</span>
        )}
        {error.path && (
          <span className="data text-xs text-muted-foreground">
            {error.path}
          </span>
        )}
        <span className="min-w-0 break-words">{error.message}</span>
      </div>
      {line !== undefined && (
        <pre className="overflow-x-auto rounded bg-background/60 px-2 py-1 data text-xs leading-5 whitespace-pre">
          {line}
          {caret && `\n${caret}`}
        </pre>
      )}
    </li>
  )
}

export function ErrorsStrip({
  errors,
  source,
  modules,
  onClear,
  className,
}: {
  errors: AppError[]
  source: string
  modules: Record<string, string>
  onClear?: () => void
  className?: string
}) {
  const [open, setOpen] = useState(false)
  if (!errors.length) return null
  const latest = errors[errors.length - 1]
  const location = locationOf(latest)
  return (
    <div
      data-errors-strip=""
      className={cn(
        "shrink-0 border-b border-warning/30 bg-warning/10 text-sm text-foreground",
        className
      )}
    >
      <div className="flex min-h-10 items-center gap-1 pr-1 pl-4">
        <button
          type="button"
          className="flex min-h-10 min-w-0 flex-1 items-center gap-2 text-left"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
        >
          <TriangleAlertIcon className="size-4 shrink-0 text-warning" />
          <span className="min-w-0 flex-1 truncate">
            {location && (
              <span className="mr-2 data text-xs text-muted-foreground">
                {location}
              </span>
            )}
            {latest.message}
            {errors.length > 1 && (
              <span className="ml-2 text-xs text-muted-foreground">
                +{errors.length - 1} more
              </span>
            )}
          </span>
          <ChevronDownIcon
            className={cn(
              "size-4 shrink-0 transition-transform",
              open && "rotate-180"
            )}
          />
        </button>
        {onClear && (
          <Button
            variant="ghost"
            size="icon"
            className="size-9 shrink-0"
            aria-label="Clear errors"
            onClick={onClear}
          >
            <XIcon className="size-4" />
          </Button>
        )}
      </div>
      {open && (
        <ul className="flex flex-col gap-3 px-4 pb-3">
          {errors.map((e, i) => (
            <ErrorLine key={i} error={e} source={source} modules={modules} />
          ))}
        </ul>
      )}
    </div>
  )
}
