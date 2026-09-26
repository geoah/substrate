/** A record's properties as a sheet: the label (an icon for its datatype and
 * its display label; the key too in technical mode) on the left, the value on
 * the right, 36px rows, and read and edit the same row. A click on a value
 * edits it in place; the chip at the row's end says who holds it and opens
 * where it came from. Empty properties fold into one line that expands. */

import { useMemo, useState } from "react"
import { ChevronDownIcon, ChevronRightIcon, LockIcon } from "lucide-react"

import { useSheetDraft } from "./draft"
import { InlineEditor } from "./inline-editor"
import { OwnershipChip, OwnershipDetail } from "./ownership"
import { DeclaredValue, LooseValue } from "./property-value"
import { editStyle, isBlockValue, propertyIcon } from "./sheet-model"
import { sheetRows, type RowLock, type SheetRow } from "./sheet-rows"
import { useRecordPatch, writeError } from "./use-record-patch"
import {
  IdentityCard,
  IdentityHoverCard,
} from "@/components/identity/identity-hover-card"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { typeLabel } from "@/lib/record-schema"
import { cn } from "@/lib/utils"

const LOCK_WORDS: Record<RowLock, string> = {
  managed: "Set automatically",
  host: "Kept up to date for you; not edited here",
  provider: "Change it in the provider",
  undeclared: "Not part of this collection’s shape, so it isn’t edited here",
}

function Label({ row }: { row: SheetRow }) {
  const [technical] = useTechnicalDetails()
  const { icon: Icon } = propertyIcon(row.spec)
  return (
    <IdentityHoverCard
      trigger={<div />}
      className="flex min-h-9 min-w-0 items-center gap-[7px] self-start pr-1.5 pl-0.5 text-[13.5px] text-muted-foreground"
      card={
        <IdentityCard
          title={row.spec.label}
          description={row.spec.description}
          facts={[
            { label: "Holds", value: typeLabel(row.spec) },
            ...(row.lock
              ? [{ label: "Editing", value: LOCK_WORDS[row.lock] }]
              : []),
          ]}
          reference={row.name}
        />
      }
    >
      <Icon aria-hidden className="size-3.5 shrink-0 text-faint" />
      <span className="flex min-w-0 flex-col leading-tight">
        <span className="truncate">{row.spec.label}</span>
        {technical && row.spec.label !== row.name && (
          <span className="truncate font-mono text-[11px] text-faint">
            {row.name}
          </span>
        )}
      </span>
    </IdentityHoverCard>
  )
}

function Value({ row }: { row: SheetRow }) {
  return row.lock === "undeclared" ? (
    <LooseValue value={row.value} />
  ) : (
    <DeclaredValue spec={row.spec} value={row.value} />
  )
}

export interface PropertySheetProps {
  record: SubstrateRecord
  kind?: KindInfo
  kinds: KindInfo[]
  /** A provider's own copy: nothing is edited here. */
  readOnly?: boolean
}

export function PropertySheet({
  record,
  kind,
  kinds,
  readOnly = false,
}: PropertySheetProps) {
  const draft = useSheetDraft()
  // A draft shows what it may write, arranged as a new record asks for it;
  // its fold holds what is left rather than what is empty.
  const { all, filled, empty } = useMemo(() => {
    const rows = sheetRows(record, kind, readOnly)
    if (!draft) return rows
    const { shown, folded } = draft.arrange(rows.all)
    return { all: [...shown, ...folded], filled: shown, empty: folded }
  }, [record, kind, readOnly, draft])
  const [showEmpty, setShowEmpty] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const [open, setOpen] = useState<string[]>([])
  const [errors, setErrors] = useState<Record<string, string>>({})
  const toggle = useRecordPatch(record)

  const rows = showEmpty ? all : filled
  const rowErrors = draft ? { ...draft.errors, ...errors } : errors

  function setError(name: string, message: string | undefined) {
    setErrors((prev) => {
      const next = { ...prev }
      if (message) next[name] = message
      else delete next[name]
      return next
    })
  }

  function startEdit(row: SheetRow) {
    if (!row.field || editing === row.name) return
    if (row.field.control === "bool") {
      toggle.mutate(
        { [row.name]: row.value !== true },
        {
          onSuccess: () => setError(row.name, undefined),
          onError: (e) => setError(row.name, writeError(e)),
        }
      )
      return
    }
    setEditing(row.name)
  }

  const emptyNames = empty.map((r) => r.spec.label)

  return (
    <div
      data-slot="property-sheet"
      className="my-[18px] grid grid-cols-[minmax(96px,120px)_minmax(0,1fr)] gap-y-0.5 sm:grid-cols-[minmax(120px,170px)_minmax(0,1fr)_auto]"
    >
      {rows.map((row) => {
        const isEditing = editing === row.name && Boolean(row.field)
        const style = row.field ? editStyle(row.field) : "line"
        const panel = isEditing && style === "panel"
        const block = !isEditing && isBlockValue(row.spec, row.value)
        const detailOpen = open.includes(row.name)
        // Where the value comes from sits in its own column, so a long value
        // wraps inside its own and never runs under the chip.
        const locked = Boolean(
          row.lock && row.lock !== "provider" && row.filled
        )
        const chip = Boolean(row.filled && !isEditing && row.meta?.manager)
        const provenance = !isEditing && (locked || chip)
        return (
          <div
            key={row.name}
            className="contents"
            data-property={row.name}
            data-filled={row.filled}
          >
            <Label row={row} />
            <div
              role={row.field && !isEditing ? "button" : undefined}
              tabIndex={row.field && !isEditing ? 0 : undefined}
              aria-label={
                row.field && !isEditing ? `Edit ${row.spec.label}` : undefined
              }
              data-editing={isEditing || undefined}
              onClick={() => startEdit(row)}
              onKeyDown={(e) => {
                if (
                  !isEditing &&
                  (e.key === "Enter" || e.key === " ") &&
                  e.target === e.currentTarget
                ) {
                  e.preventDefault()
                  startEdit(row)
                }
              }}
              className={cn(
                "relative flex min-h-9 min-w-0 flex-wrap items-center gap-1.5 rounded-md px-2 py-[3px] text-sm outline-none",
                !provenance && "sm:col-span-2",
                row.field &&
                  !isEditing &&
                  "cursor-text hover:bg-hover focus-visible:bg-hover",
                !row.field && "cursor-default",
                block && "flex-nowrap items-start py-1.5",
                isEditing &&
                  style === "pop" &&
                  "bg-background ring-1 ring-primary"
              )}
            >
              {isEditing && !panel ? (
                <InlineEditor
                  row={
                    row as SheetRow & { field: NonNullable<SheetRow["field"]> }
                  }
                  record={record}
                  kinds={kinds}
                  onDone={() => setEditing(null)}
                  onError={(m) => setError(row.name, m)}
                />
              ) : (
                <span
                  className={cn(
                    "inline-flex min-w-0 flex-wrap items-center gap-1.5",
                    block && "flex-1"
                  )}
                >
                  <Value row={row} />
                  {row.hint && (
                    <span className="text-xs text-faint">{row.hint}</span>
                  )}
                </span>
              )}
            </div>
            {provenance && (
              <div
                data-slot="provenance"
                className="col-start-2 -mt-1 mb-1 flex min-w-0 items-center gap-1.5 self-start px-1 sm:col-start-3 sm:mt-0 sm:mb-0 sm:min-h-9 sm:justify-end sm:pl-3"
              >
                {locked && (
                  <span
                    title={LOCK_WORDS[row.lock!]}
                    aria-label={LOCK_WORDS[row.lock!]}
                    className="text-faint"
                  >
                    <LockIcon aria-hidden className="size-3" />
                  </span>
                )}
                {chip && (
                  <OwnershipChip
                    row={row}
                    open={detailOpen}
                    onToggle={() =>
                      setOpen((prev) =>
                        prev.includes(row.name)
                          ? prev.filter((n) => n !== row.name)
                          : [...prev, row.name]
                      )
                    }
                  />
                )}
              </div>
            )}
            {panel && (
              <div className="col-span-full mb-2">
                <InlineEditor
                  row={
                    row as SheetRow & { field: NonNullable<SheetRow["field"]> }
                  }
                  record={record}
                  kinds={kinds}
                  onDone={() => setEditing(null)}
                  onError={(m) => setError(row.name, m)}
                />
              </div>
            )}
            {rowErrors[row.name] && (
              <p
                role="alert"
                className="col-span-full -mt-0.5 mb-1.5 rounded-md bg-bad-soft px-2.5 py-1.5 text-[12.5px] text-destructive sm:col-start-2 sm:col-end-4"
              >
                {rowErrors[row.name]}
              </p>
            )}
            {detailOpen && row.meta?.manager && (
              <OwnershipDetail
                row={row}
                record={record}
                readOnly={readOnly}
                onEdit={row.field ? () => setEditing(row.name) : undefined}
              />
            )}
          </div>
        )
      })}
      {empty.length > 0 && (
        <button
          type="button"
          onClick={() => setShowEmpty((v) => !v)}
          aria-expanded={showEmpty}
          className="col-span-full flex items-center gap-1.5 px-0.5 py-1.5 text-left text-[13px] text-faint hover:text-muted-foreground"
        >
          {showEmpty ? (
            <>
              <ChevronRightIcon aria-hidden className="size-3.5" />
              {draft ? "Show fewer" : "Hide empty"}
            </>
          ) : (
            <>
              <ChevronDownIcon aria-hidden className="size-3.5" />
              <span className="truncate">
                {empty.length} {draft ? "more" : "empty"}:{" "}
                {emptyNames.join(", ")}
              </span>
            </>
          )}
        </button>
      )}
      {!filled.length && !empty.length && (
        <p className="col-span-full py-2 text-[13px] text-faint">
          This collection declares no properties beyond the title.
        </p>
      )}
    </div>
  )
}
