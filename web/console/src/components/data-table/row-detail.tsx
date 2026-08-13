/** THE shared expansion renderers (owner ruling, 2026-08-06: one payload/
 * JSON detail renderer reused by every expandable row — changelog, actor log,
 * record activity, sync runs, merge requests all open into this voice).
 *
 * `RowDetail` is the band itself — gutter-aligned, muted, bordered.
 * `ChangeDetail` is a change row spelled out whole: who committed it and when,
 * the changed properties (values where the payload carries them — states,
 * managers), the EFFECTS the write applied said in English, function stances,
 * and whatever else the wire said as JSON so nothing is dropped.
 *
 * The effects are the point. A write records what it did so a rebuild can
 * replay it, and that record used to reach the reader as a raw payload key
 * named after the mechanism. It doesn't any more: `changeEffects` translates it
 * (lib/changelog.ts) and the untranslated truth sits behind the `raw` toggle,
 * where debugging wants it and reading does not. */

import { useState } from "react"
import { ChevronRightIcon } from "lucide-react"

import { ActorChip } from "@/components/actor-chip"
import { CodeBlock } from "@/components/code-block"
import { Button } from "@/components/ui/button"
import type { ChangeRow } from "@/lib/api/types"
import { cellValue, shortDate, shortTime } from "@/lib/format"
import {
  changeEffects,
  changedProperties,
  NAMED_PAYLOAD_KEYS,
} from "@/lib/changelog"
import { cn } from "@/lib/utils"

/** ONE shape for everything inside the band: a muted label in a fixed column,
 * its value beside it. Every section of an expanded row is a `DetailRow`, so
 * the band reads as one list rather than as five little layouts each with its
 * own type size and tone (owner complaint, 2026-08-13). Identifiers wear the
 * data voice inside the value; the label never does. */
export function DetailRow({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className="contents">
      <span className="text-muted-foreground">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  )
}

/** The grid the rows sit in. `minmax(0,1fr)`: a track's implicit min-width is
 * `auto`, so one long unbroken id would widen the value column past the band. */
function DetailGrid({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[5.5rem_minmax(0,1fr)] items-baseline gap-x-3 gap-y-1.5">
      {children}
    </div>
  )
}

/** The band every expanded row opens into: same gutter as the table, same
 * muted surface everywhere. */
export function RowDetail({
  density = "default",
  className,
  children,
}: {
  density?: "default" | "compact"
  className?: string
  children: React.ReactNode
}) {
  return (
    <div className={cn("py-1.5", density === "compact" ? "px-4" : "px-6")}>
      <div
        className={cn(
          "flex flex-col gap-1.5 rounded-md border bg-muted/40 p-2.5 text-xs",
          className
        )}
      >
        {children}
      </div>
    </div>
  )
}

/** name → value pairs the payload actually carries for changed properties:
 * `states` (state-kind values ride the row) and `managers` (who took the
 * property). The row's property list carries no other values. */
function valuedEntries(
  payload: Record<string, unknown> | undefined,
  key: "states" | "managers"
): [string, unknown][] {
  const map = payload?.[key]
  if (typeof map !== "object" || map === null || Array.isArray(map)) return []
  return Object.entries(map as Record<string, unknown>)
}

/** The debugging escape hatch: the whole payload, verbatim, closed by default.
 * Every named section above is a READING of this — when the two disagree, this
 * one is right. */
function RawPayload({ payload }: { payload: Record<string, unknown> }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="flex flex-col gap-1.5">
      <Button
        variant="ghost"
        size="sm"
        className="h-5 w-fit gap-1 px-1 text-xs font-normal text-muted-foreground"
        // The band sits inside an expanded table row; without this the click
        // walks up and collapses the row the reader just opened.
        onClick={(e) => {
          e.stopPropagation()
          setOpen((v) => !v)
        }}
      >
        <ChevronRightIcon
          className={cn("size-3 transition-transform", open && "rotate-90")}
        />
        raw
      </Button>
      {open && (
        <CodeBlock source={JSON.stringify(payload, null, 2)} lang="json" />
      )}
    </div>
  )
}

export function ChangeDetail({ row }: { row: ChangeRow }) {
  const payload = row.payload ?? {}
  const properties = changedProperties(row)
  const states = valuedEntries(row.payload, "states")
  const managers = valuedEntries(row.payload, "managers")
  const effects = changeEffects(row)
  const stateOf = new Map(states)
  const rest = Object.fromEntries(
    Object.entries(payload).filter(([k]) => !NAMED_PAYLOAD_KEYS.has(k))
  )
  const op = [
    row.op,
    payload.created === true ? "created" : "",
    payload.restored === true ? "restored" : "",
  ]
    .filter(Boolean)
    .join(", ")
  return (
    <DetailGrid>
      <DetailRow label="when">
        <span className="data" title={row.ts}>
          {shortDate(row.ts)} {shortTime(row.ts, true)}
        </span>
      </DetailRow>
      <DetailRow label="by">
        <ActorChip actor={row.actor} />
      </DetailRow>
      <DetailRow label="change">
        <span className="data">
          {op} <span className="text-muted-foreground">· seq {row.seq}</span>
        </span>
      </DetailRow>
      {properties.length > 0 && (
        <DetailRow
          label={properties.length === 1 ? "property" : "properties"}
        >
          {states.length > 0 ? (
            // A state property's new value rides the row; a plain one's does
            // not, so only the states get an arrow.
            <div className="flex flex-col gap-0.5">
              {properties.map((name) => (
                <span key={name} className="truncate data">
                  {name}
                  {stateOf.has(name) && (
                    <span className="text-muted-foreground">
                      {" → "}
                      {cellValue(stateOf.get(name))}
                    </span>
                  )}
                </span>
              ))}
            </div>
          ) : (
            <span className="data">{properties.join(", ")}</span>
          )}
        </DetailRow>
      )}
      {managers.length > 0 && (
        <DetailRow label={managers.length === 1 ? "manager" : "managers"}>
          <div className="flex flex-col gap-1">
            {managers.map(([name, actor]) => (
              <span key={name} className="flex items-center gap-1.5">
                <span className="data">{name}</span>
                <span className="text-muted-foreground">→</span>
                <ActorChip actor={String(actor)} />
              </span>
            ))}
          </div>
        </DetailRow>
      )}
      {effects.length > 0 && (
        <DetailRow
          label={
            effects.length === 1 ? "1 change" : `${effects.length} changes`
          }
        >
          <ul className="flex flex-col gap-0.5">
            {effects.map((effect, i) => (
              <li key={i} className="min-w-0">
                {effect.verb}
                {effect.target && (
                  <span className="data break-all"> {effect.target}</span>
                )}
                {effect.detail && (
                  <span className="text-muted-foreground"> — {effect.detail}</span>
                )}
              </li>
            ))}
          </ul>
        </DetailRow>
      )}
      {(row.triggers ?? []).map((tr) => (
        <DetailRow key={tr.trigger} label="trigger">
          <span className="flex flex-wrap items-center gap-1.5">
            <span className="data">{tr.trigger}</span>
            <span className="text-muted-foreground">→</span>
            <span className="data">{tr.callable}</span>
            <span
              className={cn(
                "data",
                tr.state === "parked"
                  ? "text-destructive"
                  : "text-muted-foreground"
              )}
            >
              {tr.state}
            </span>
            {tr.error && (
              <span className="data text-destructive">{tr.error}</span>
            )}
          </span>
        </DetailRow>
      ))}
      {Object.keys(rest).length > 0 && (
        <DetailRow label="payload">
          <CodeBlock source={JSON.stringify(rest, null, 2)} lang="json" />
        </DetailRow>
      )}
      {/* The disclosure names itself, so it spans both columns rather than
          repeating "raw" as a label beside a button reading the same. */}
      {Object.keys(payload).length > 0 && (
        <div className="col-span-2 min-w-0">
          <RawPayload payload={payload} />
        </div>
      )}
    </DetailGrid>
  )
}
