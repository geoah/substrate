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
import { Button } from "@/components/ui/button"
import type { ChangeRow } from "@/lib/api/types"
import { cellValue, shortDate, shortTime } from "@/lib/format"
import {
  changeEffects,
  changedProperties,
  NAMED_PAYLOAD_KEYS,
} from "@/lib/changelog"
import { cn } from "@/lib/utils"

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
        <pre className="data overflow-x-auto rounded-sm bg-background/60 p-2">
          {JSON.stringify(payload, null, 2)}
        </pre>
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
  return (
    <>
      <div className="flex flex-wrap items-center gap-x-1.5 gap-y-1 data text-muted-foreground">
        <span>
          seq {row.seq}, {shortDate(row.ts)} {shortTime(row.ts, true)}, {row.op}
          {payload.created === true && ", created"}
          {payload.restored === true && ", restored"}
        </span>
        <span>by</span>
        <ActorChip actor={row.actor} />
      </div>
      {properties.length > 0 &&
        (states.length > 0 ? (
          // values ride along (state transitions) — a name → value grid
          <div>
            <span className="text-muted-foreground">
              {properties.length === 1 ? "property" : "properties"}
            </span>
            <div className="mt-0.5 grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-2.5 gap-y-0.5">
              {properties.map((name) => (
                <div key={name} className="contents">
                  <span className="data">{name}</span>
                  <span className="truncate data text-muted-foreground">
                    {stateOf.has(name)
                      ? `→ ${cellValue(stateOf.get(name))}`
                      : ""}
                  </span>
                </div>
              ))}
            </div>
          </div>
        ) : (
          // names only (the property list carries no values) — inline
          <div className="flex flex-wrap items-baseline gap-x-1.5 gap-y-0.5">
            <span className="text-muted-foreground">
              {properties.length === 1 ? "property" : "properties"}
            </span>
            <span className="data">{properties.join(", ")}</span>
          </div>
        ))}
      {managers.length > 0 && (
        <div className="flex flex-col gap-0.5">
          {managers.map(([name, actor]) => (
            <div key={name} className="flex items-baseline gap-1.5">
              <span className="text-muted-foreground">manager</span>
              <span className="data">{name}</span>
              <span className="text-muted-foreground">→</span>
              <ActorChip actor={String(actor)} />
            </div>
          ))}
        </div>
      )}
      {effects.length > 0 && (
        <div>
          <span className="text-muted-foreground">
            {effects.length === 1 ? "1 change" : `${effects.length} changes`}
          </span>
          <ul className="mt-0.5 flex flex-col gap-0.5">
            {effects.map((effect, i) => (
              <li
                key={i}
                className="flex flex-wrap items-baseline gap-x-1.5 gap-y-0.5"
              >
                <span className="text-foreground">{effect.verb}</span>
                {effect.target && (
                  <span className="data break-all" title={effect.target}>
                    {effect.target}
                  </span>
                )}
                {effect.detail && (
                  <span className="data text-muted-foreground">
                    — {effect.detail}
                  </span>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
      {(row.triggers ?? []).map((tr) => (
        <div key={tr.trigger} className="flex items-baseline gap-1.5">
          <span className="text-muted-foreground">trigger</span>
          <span className="data">{tr.trigger}</span>
          <span className="text-muted-foreground">→ {tr.callable}</span>
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
          {tr.error && <span className="data text-destructive">{tr.error}</span>}
        </div>
      ))}
      {Object.keys(rest).length > 0 && (
        <pre className="data overflow-x-auto rounded-sm bg-background/60 p-2">
          {JSON.stringify(rest, null, 2)}
        </pre>
      )}
      {Object.keys(payload).length > 0 && <RawPayload payload={payload} />}
    </>
  )
}
