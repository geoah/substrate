/** The approval page's building blocks: a labelled fact, a warning, a
 * refusal, and the technical view's terms, identifiers and counted lists. */

import type { ReactNode } from "react"
import { ChevronRightIcon, TriangleAlertIcon } from "lucide-react"

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"

/** One labelled fact on the page: a small heading over its value. */
export function Fact({
  label,
  children,
}: {
  label: string
  children: ReactNode
}) {
  return (
    <section aria-label={label} className="flex flex-col gap-1.5">
      <h2 className="text-xs font-medium text-muted-foreground">{label}</h2>
      {children}
    </section>
  )
}

export function Warning({
  title,
  children,
}: {
  title?: string
  children: ReactNode
}) {
  return (
    <div
      role="note"
      className="flex gap-2.5 rounded-lg border border-warning/40 bg-warn-soft px-3 py-2.5 text-[13px]"
    >
      <TriangleAlertIcon
        aria-hidden
        className="mt-0.5 size-4 shrink-0 text-warning"
      />
      <div className="flex min-w-0 flex-col gap-0.5">
        {title && <p className="font-medium">{title}</p>}
        <div>{children}</div>
      </div>
    </div>
  )
}

/** A refusal or a stop, in the destructive voice: what happened over why. */
export function Problem({
  title,
  children,
}: {
  title: string
  children?: ReactNode
}) {
  return (
    <div
      role="alert"
      className="flex gap-2.5 rounded-lg border border-destructive/40 bg-destructive/5 px-3 py-2.5 text-[13px]"
    >
      <TriangleAlertIcon
        aria-hidden
        className="mt-0.5 size-4 shrink-0 text-destructive"
      />
      <div className="flex min-w-0 flex-col gap-0.5">
        <p className="font-medium text-destructive">{title}</p>
        {children && (
          <div className="[overflow-wrap:anywhere] text-muted-foreground">
            {children}
          </div>
        )}
      </div>
    </div>
  )
}

/** A term and its value in the technical view's two-column list. */
export function Term({
  name,
  children,
}: {
  name: string
  children: ReactNode
}) {
  return (
    <>
      <dt className="text-muted-foreground">{name}</dt>
      <dd className="min-w-0 [overflow-wrap:anywhere]">{children}</dd>
    </>
  )
}

/** An identifier: a kind reference, a package id, a trigger id. */
export function Mono({ children }: { children: ReactNode }) {
  return <span className="font-mono [overflow-wrap:anywhere]">{children}</span>
}

/** A list the technical view counts and opens on demand: "Kinds (3)",
 * named for a screen reader by whose it is, since a row lists its own and
 * each requirement's. */
export function Disclosure({
  label,
  count,
  of,
  children,
}: {
  label: string
  count: number
  /** Whose list it is: "Apple Health". */
  of: string
  children: ReactNode
}) {
  return (
    <Collapsible className="flex flex-col gap-1">
      <CollapsibleTrigger
        aria-label={`${of} ${label.toLowerCase()} (${count})`}
        className="group flex w-fit cursor-pointer items-center gap-1 text-muted-foreground hover:text-foreground"
      >
        <ChevronRightIcon
          aria-hidden
          className="size-3 transition-transform group-data-[panel-open]:rotate-90"
        />
        {label} ({count})
      </CollapsibleTrigger>
      <CollapsibleContent className="pl-4">{children}</CollapsibleContent>
    </Collapsible>
  )
}
