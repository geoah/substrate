/** The change request page's Now / If applied grid, editable: a click on an
 * If applied value opens the property sheet's own editor for it, and the
 * write lands in a draft of the owner's values (`SheetDraftContext`), never
 * on the record. A row can be left out, and put back. Applying sends the
 * draft as the accept's `adjustedDiff` when it differs from what was
 * suggested (decision 0112); nothing is stored before then. */

import { useMemo, useState } from "react"
import { Undo2Icon, XIcon } from "lucide-react"

import { ChangeLabel, ChangeValue } from "@/components/change-request"
import {
  SheetDraftContext,
  type SheetDraft,
} from "@/components/property-sheet/draft"
import { InlineEditor } from "@/components/property-sheet/inline-editor"
import type { SheetRow } from "@/components/property-sheet/sheet-rows"
import { ActorRef } from "@/components/identity/actor-ref"
import { Pill } from "@/components/identity/pill"
import { Button } from "@/components/ui/button"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { changeLabel } from "@/lib/agent-chat"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { type ReviewRow } from "@/lib/changerequests"
import { fieldOf } from "@/lib/record-form"
import { ownerWritable, type PropSpec } from "@/lib/record-schema"
import { cn } from "@/lib/utils"

const GRID3 =
  "grid grid-cols-[minmax(96px,150px)_minmax(0,1fr)_minmax(0,1fr)] sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)_minmax(0,1fr)]"
const GRID2 =
  "grid grid-cols-[minmax(96px,150px)_minmax(0,1fr)] sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)]"
const HEAD = "pr-4 pb-1.5 text-[12px] font-medium text-faint"
const CELL = "min-w-0 py-2 pr-4 text-[14px]"

export function ReviewComparison({
  rows,
  compare,
  specs,
  kind,
  kinds,
  target,
  edited,
  onEdit,
  emptyText,
  create,
}: {
  rows: ReviewRow[]
  /** Show the record as it is now beside the values (a patch). */
  compare: boolean
  specs: Map<string, PropSpec>
  kind?: KindInfo
  kinds: KindInfo[]
  target?: SubstrateRecord
  /** The owner's values as they stand. */
  edited: Record<string, unknown>
  onEdit: (next: Record<string, unknown>) => void
  emptyText: string
  /** A create: a left-out row is not added, and a state is where the
   * machine starts it. */
  create?: boolean
}) {
  const [technical] = useTechnicalDetails()
  const [editing, setEditing] = useState<string | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})

  const draft: SheetDraft = useMemo(
    () => ({
      write(props) {
        const next = { ...edited }
        for (const [key, value] of Object.entries(props)) {
          // A create has nothing to clear: an emptied value is not added.
          if (value === null && create) delete next[key]
          else next[key] = value
        }
        onEdit(next)
      },
      arrange: (all) => ({ shown: all, folded: [] }),
      errors: {},
    }),
    [edited, onEdit, create]
  )

  // The record an editor opens on: the target (or the record a create would
  // make) with the owner's values over it.
  const record: SubstrateRecord = {
    id: target?.id ?? "draft",
    kind: kind?.identity ?? target?.kind ?? "",
    properties: { ...(target?.properties ?? {}), ...edited },
    labels: {},
    version: target?.version ?? 0,
    createdAt: "",
    updatedAt: "",
  }

  function sheetRow(row: ReviewRow): SheetRow | undefined {
    const spec = specs.get(row.key)
    if (!spec || spec.managed || !ownerWritable(spec)) return undefined
    // A state moves from where the record stands, along its transitions; a
    // new record starts where the machine starts it.
    if (spec.kind === "state" && (create || !target)) return undefined
    const value = spec.kind === "state" ? row.before : row.after
    return {
      name: row.key,
      spec,
      field: fieldOf(spec),
      value,
      filled: value !== undefined && value !== null && value !== "",
    }
  }

  function leaveOut(key: string) {
    const next = { ...edited }
    delete next[key]
    onEdit(next)
    setEditing(null)
  }

  function putBack(row: ReviewRow) {
    onEdit({ ...edited, [row.key]: row.suggested })
  }

  const cols = compare ? 3 : 2
  return (
    <SheetDraftContext.Provider value={draft}>
      <div data-slot="change-comparison" className={compare ? GRID3 : GRID2}>
        <span className={HEAD} />
        {compare && <span className={HEAD}>Now</span>}
        <span className={cn(HEAD, "text-primary-text")}>If applied</span>
        {rows.length === 0 && (
          <p
            className={cn(
              "border-t py-3 text-[13px] text-muted-foreground",
              compare ? "col-span-3" : "col-span-2"
            )}
          >
            {emptyText}
          </p>
        )}
        {rows.map((row) => {
          const spec = specs.get(row.key)
          const label = changeLabel(row.key, spec)
          const sheet = row.leftOut ? undefined : sheetRow(row)
          const isEditing = editing === row.key && sheet?.field
          return (
            <div key={row.key} className="contents" data-property={row.key}>
              <span className={cn(CELL, "border-t")}>
                <ChangeLabel name={row.key} spec={spec} />
                {technical && (
                  <span className="block font-mono text-[11.5px] text-faint">
                    {row.key}
                  </span>
                )}
              </span>
              {compare && (
                <span className={cn(CELL, "border-t text-muted-foreground")}>
                  <ChangeValue
                    value={row.before === undefined ? "" : row.before}
                    spec={spec}
                  />
                  {technical && row.manager && !row.unchanged && (
                    <span className="mt-1 flex items-center gap-1.5 text-[12px] text-faint">
                      set by <ActorRef actor={row.manager} />
                    </span>
                  )}
                </span>
              )}
              <span
                className={cn(
                  CELL,
                  "flex items-start gap-1 border-t py-1 pr-0"
                )}
              >
                {row.leftOut ? (
                  <span className="flex min-h-9 flex-1 flex-wrap items-center gap-x-2 px-2 text-[13px] text-faint">
                    {create ? "Not added" : "Left as it is"}
                    <Button
                      size="xs"
                      variant="ghost"
                      className="text-muted-foreground"
                      onClick={() => putBack(row)}
                    >
                      <Undo2Icon />
                      Put it back
                    </Button>
                  </span>
                ) : isEditing ? (
                  <span className="min-h-9 min-w-0 flex-1 rounded-md bg-background px-2 py-[3px] ring-1 ring-primary">
                    <InlineEditor
                      row={
                        sheet as SheetRow & {
                          field: NonNullable<SheetRow["field"]>
                        }
                      }
                      record={record}
                      kinds={kinds}
                      onDone={() => setEditing(null)}
                      onError={(message) =>
                        setErrors((prev) => {
                          const next = { ...prev }
                          if (message) next[row.key] = message
                          else delete next[row.key]
                          return next
                        })
                      }
                    />
                  </span>
                ) : (
                  <span
                    role={sheet ? "button" : undefined}
                    tabIndex={sheet ? 0 : undefined}
                    aria-label={
                      sheet
                        ? `${label}: change the value it applies`
                        : undefined
                    }
                    onClick={() => sheet && setEditing(row.key)}
                    onKeyDown={(e) => {
                      if (
                        sheet &&
                        (e.key === "Enter" || e.key === " ") &&
                        e.target === e.currentTarget
                      ) {
                        e.preventDefault()
                        setEditing(row.key)
                      }
                    }}
                    className={cn(
                      "flex min-h-9 min-w-0 flex-1 flex-wrap items-center gap-1.5 rounded-md px-2 py-[3px] outline-none",
                      sheet &&
                        "cursor-text hover:bg-hover focus-visible:bg-hover focus-visible:ring-2 focus-visible:ring-ring"
                    )}
                  >
                    <ChangeValue value={row.after} spec={spec} />
                    {row.edited && (
                      <Pill tone="accent" dot={false}>
                        Your edit
                      </Pill>
                    )}
                    {row.unchanged && (
                      <span className="text-[12.5px] text-faint">
                        Already set
                      </span>
                    )}
                  </span>
                )}
                {!row.leftOut && (
                  <span className="flex shrink-0 items-center pt-1.5">
                    {row.edited && (
                      <Button
                        size="icon-xs"
                        variant="ghost"
                        aria-label={`Use the suggested ${label}`}
                        title="Use the suggested value"
                        className="text-faint hover:text-foreground"
                        onClick={() => putBack(row)}
                      >
                        <Undo2Icon />
                      </Button>
                    )}
                    <Button
                      size="icon-xs"
                      variant="ghost"
                      aria-label={`Leave out ${label}`}
                      title={create ? "Don’t add this" : "Leave this as it is"}
                      className="text-faint hover:text-foreground"
                      onClick={() => leaveOut(row.key)}
                    >
                      <XIcon />
                    </Button>
                  </span>
                )}
              </span>
              {errors[row.key] && (
                <p
                  role="alert"
                  className={cn(
                    "mb-1.5 rounded-md bg-bad-soft px-2.5 py-1.5 text-[12.5px] text-destructive",
                    cols === 3 ? "col-start-3" : "col-start-2"
                  )}
                >
                  {errors[row.key]}
                </p>
              )}
            </div>
          )
        })}
      </div>
    </SheetDraftContext.Provider>
  )
}

/** A decided request the owner adjusted: what was suggested beside what was
 * applied, row by row. */
export function SuggestedAndApplied({
  rows,
  specs,
}: {
  rows: Array<{
    key: string
    suggested: unknown
    applied: unknown
    effect: "same" | "changed" | "left out" | "added"
  }>
  specs: Map<string, PropSpec>
}) {
  return (
    <div data-slot="change-adjusted" className={GRID3}>
      <span className={HEAD} />
      <span className={HEAD}>Suggested</span>
      <span className={cn(HEAD, "text-primary-text")}>Applied</span>
      {rows.map((row) => {
        const spec = specs.get(row.key)
        return (
          <div key={row.key} className="contents">
            <span className={cn(CELL, "border-t")}>
              <ChangeLabel name={row.key} spec={spec} />
            </span>
            <span className={cn(CELL, "border-t text-muted-foreground")}>
              {row.effect === "added" ? (
                <span className="text-faint">Not suggested</span>
              ) : (
                <ChangeValue value={row.suggested} spec={spec} />
              )}
            </span>
            <span className={cn(CELL, "border-t")}>
              {row.effect === "left out" ? (
                <span className="text-faint">Left out</span>
              ) : (
                <span className="inline-flex flex-wrap items-center gap-1.5">
                  <ChangeValue value={row.applied} spec={spec} />
                  {row.effect !== "same" && (
                    <Pill tone="accent" dot={false}>
                      Your edit
                    </Pill>
                  )}
                </span>
              )}
            </span>
          </div>
        )
      })}
    </div>
  )
}
