/** A property edited where it is read. A click on a value opens the editor
 * its datatype earns: a text box, a number, a date and time, a textarea for
 * prose, a list to pick from for an enum, the moves a state may make, a
 * record picker for a reference, and for the shapes that need room (lists,
 * objects, maps, JSON) the whole control in a panel under the row. Enter or
 * leaving the box saves, Esc cancels, and a save is one property's PATCH
 * carrying the version the page read. */

import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react"
import { useQuery } from "@tanstack/react-query"
import { ArrowRightIcon, CheckIcon } from "lucide-react"

import { fromLocalInput, toLocalInput } from "./dates"
import { editStyle, propertyWrite } from "./sheet-model"
import { type SheetRow } from "./sheet-rows"
import { useRecordPatch, writeError } from "./use-record-patch"
import { StateBadge } from "@/components/identity/state-badge"
import { PropertyField } from "@/components/record/property-field"
import { RecordCombobox } from "@/components/record/record-combobox"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { useRecordOptions } from "@/lib/identities"
import { recordTitleQueryOptions } from "@/lib/reference-titles"
import { seedField, type FormField, type FormValue } from "@/lib/record-form"
import { humanizeName, movesFrom } from "@/lib/record-schema"
import { cn } from "@/lib/utils"

export interface InlineEditorProps {
  row: SheetRow & { field: FormField }
  record: SubstrateRecord
  kinds: KindInfo[]
  onDone: () => void
  onError: (message: string | undefined) => void
}

export function InlineEditor(props: InlineEditorProps) {
  const style = editStyle(props.row.field)
  const control = props.row.field.control
  if (control === "state") return <StateMoves {...props} />
  if (control === "select") return <EnumPicker {...props} />
  if (control === "reference" && style === "line") {
    return <ReferencePicker {...props} />
  }
  if (style === "line") return <LineEditor {...props} />
  return <PanelEditor {...props} />
}

/** Saves one write and reports how it went. */
function useSave({ row, record, onDone, onError }: InlineEditorProps) {
  const patch = useRecordPatch(record)
  async function save(next: FormValue) {
    const write = propertyWrite(row.field, row.value, next)
    if (write.error) {
      onError(write.error)
      return
    }
    if (!write.properties) {
      onError(undefined)
      onDone()
      return
    }
    try {
      await patch.mutateAsync(write.properties)
      onError(undefined)
      onDone()
    } catch (error) {
      onError(writeError(error))
    }
  }
  return { save, pending: patch.isPending }
}

const INPUT =
  "w-full min-w-0 rounded-md border border-primary bg-background px-2 py-1 text-sm text-foreground ring-3 ring-primary-soft outline-none"

function LineEditor(props: InlineEditorProps) {
  const { row, onDone, onError } = props
  const { field } = row
  const { save, pending } = useSave(props)
  const isDate = field.control === "datetime" && field.spec.kind !== "date"
  const [initial] = useState(() => {
    const seeded = seedField(field, row.value, false)
    const s = typeof seeded === "string" ? seeded : ""
    return isDate ? toLocalInput(s) : s
  })
  const [text, setText] = useState(initial)
  const done = useRef(false)

  function commit() {
    if (done.current) return
    // Leaving the box as it opened is not an edit. The draft is compared,
    // not the stored value: a date shown to the minute would otherwise write
    // back the stored instant without its seconds.
    if (text === initial) return cancel()
    done.current = true
    void save(isDate ? fromLocalInput(text) : text).finally(() => {
      done.current = false
    })
  }
  function cancel() {
    done.current = true
    onError(undefined)
    onDone()
  }
  function onKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault()
      cancel()
    } else if (
      e.key === "Enter" &&
      (field.control !== "prose" || e.metaKey || e.ctrlKey)
    ) {
      e.preventDefault()
      commit()
    }
  }

  const common = {
    autoFocus: true,
    "aria-label": field.label,
    disabled: pending,
    value: text,
    onKeyDown,
    onBlur: commit,
  }
  return (
    <div className="flex w-full min-w-0 items-center gap-2">
      {field.control === "prose" ? (
        <textarea
          {...common}
          rows={Math.min(10, Math.max(3, text.split("\n").length + 1))}
          className={cn(INPUT, "resize-y leading-relaxed")}
          onChange={(e) => setText(e.target.value)}
        />
      ) : (
        <input
          {...common}
          type={
            field.control === "secret"
              ? "password"
              : field.control === "number"
                ? "number"
                : field.spec.kind === "date"
                  ? "date"
                  : isDate
                    ? "datetime-local"
                    : field.inputType
          }
          placeholder={
            field.control === "secret"
              ? "A new value; the stored one never reads back"
              : field.example
                ? `e.g. ${field.example}`
                : undefined
          }
          className={INPUT}
          onChange={(e) => setText(e.target.value)}
        />
      )}
      {pending && <Spinner className="size-3.5 shrink-0" />}
    </div>
  )
}

/** A list that drops under the value, closed by Esc or a click outside. */
function EditPop({
  onClose,
  children,
  label,
}: {
  onClose: () => void
  children: ReactNode
  label: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    function away(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    function key(e: globalThis.KeyboardEvent) {
      if (e.key === "Escape") onClose()
    }
    document.addEventListener("mousedown", away)
    document.addEventListener("keydown", key)
    return () => {
      document.removeEventListener("mousedown", away)
      document.removeEventListener("keydown", key)
    }
  }, [onClose])
  useEffect(() => {
    ref.current?.querySelector<HTMLElement>("[data-option]")?.focus()
  }, [])
  return (
    <div
      ref={ref}
      role="listbox"
      aria-label={label}
      data-slot="edit-pop"
      onClick={(e) => e.stopPropagation()}
      className="absolute top-[calc(100%+4px)] left-0 z-30 flex w-64 max-w-[calc(100vw-2rem)] flex-col rounded-lg border border-border-strong bg-background p-1.5 shadow-card"
    >
      {children}
    </div>
  )
}

function Option({
  chosen,
  onPick,
  children,
  hint,
  disabled,
}: {
  chosen?: boolean
  onPick: () => void
  children: ReactNode
  hint?: ReactNode
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={chosen}
      data-option
      disabled={disabled}
      onClick={onPick}
      className="flex h-[30px] w-full items-center gap-2 rounded-[5px] px-2 text-left text-[13px] outline-none hover:bg-hover focus-visible:bg-hover disabled:opacity-50"
    >
      {children}
      <span className="ml-auto flex items-center gap-1 text-xs text-faint">
        {hint}
        {chosen && <CheckIcon className="size-3.5" />}
      </span>
    </button>
  )
}

function StateMoves(props: InlineEditorProps) {
  const { row, onDone, onError } = props
  const { save, pending } = useSave(props)
  const current = typeof row.value === "string" ? row.value : ""
  const moves = movesFrom(row.field.spec, current)
  const cancel = () => {
    onError(undefined)
    onDone()
  }
  return (
    <>
      <StateBadge value={current} initial={row.field.spec.initial} />
      <EditPop onClose={cancel} label={`Move ${row.field.label}`}>
        <div className="px-2 pt-1 pb-1.5 text-xs text-faint">
          {moves.length ? "Move to" : `No moves from ${current || "here"}`}
        </div>
        {moves.map((move) => (
          <Option
            key={move.to}
            disabled={pending}
            onPick={() => void save(move.to)}
            hint={stampHint(move.stamps)}
          >
            <ArrowRightIcon className="size-3 text-faint" />
            <StateBadge value={move.to} initial={row.field.spec.initial} />
          </Option>
        ))}
        {pending && (
          <div className="flex items-center gap-2 px-2 py-1 text-xs text-faint">
            <Spinner className="size-3" /> Saving
          </div>
        )}
      </EditPop>
    </>
  )
}

/** "fills in Completed at": what a move stamps, said beside it. */
function stampHint(stamps: string[]): string | undefined {
  if (!stamps.length) return undefined
  return `fills in ${stamps.map((s) => humanizeName(s)).join(", ")}`
}

function EnumPicker(props: InlineEditorProps) {
  const { row, onDone, onError } = props
  const { save, pending } = useSave(props)
  const current = typeof row.value === "string" ? row.value : ""
  const cancel = () => {
    onError(undefined)
    onDone()
  }
  return (
    <>
      <span className="text-muted-foreground">
        {row.field.options?.find((o) => o.value === current)?.label ||
          (current ? humanizeName(current) : "Choose…")}
      </span>
      <EditPop onClose={cancel} label={`Choose ${row.field.label}`}>
        {(row.field.options ?? []).map((option) => (
          <Option
            key={option.value}
            chosen={option.value === current}
            disabled={pending}
            onPick={() => void save(option.value)}
          >
            {option.label || humanizeName(option.value)}
          </Option>
        ))}
        {!row.field.required && current && (
          <Option disabled={pending} onPick={() => void save(null)}>
            <span className="text-muted-foreground">Clear</span>
          </Option>
        )}
      </EditPop>
    </>
  )
}

function ReferencePicker(props: InlineEditorProps) {
  const { row, kinds, record, onDone, onError } = props
  const { save, pending } = useSave(props)
  const pin = row.field.spec.to ?? ""
  const offered = useRecordOptions(pin, kinds, record.id)
  const chosen = useRef(false)
  const seeded = seedField(row.field, row.value, false) as {
    kind: string
    id: string
  }
  // The chosen record may sit past the loaded page; its title is read.
  const title = useQuery({
    ...recordTitleQueryOptions(seeded.kind || pin, seeded.id),
    enabled: Boolean(seeded.id),
  })
  return (
    <div className="flex w-full min-w-0 items-center gap-2">
      <RecordCombobox
        {...offered}
        defaultOpen
        ariaLabel={row.field.label}
        value={seeded.id}
        valueTitle={title.data ?? undefined}
        placeholder="Choose…"
        onSelect={(id) => {
          chosen.current = true
          void save({ kind: pin, id })
        }}
        onClear={
          row.field.required
            ? undefined
            : () => {
                chosen.current = true
                void save(null)
              }
        }
        onOpenChange={(open) => {
          if (!open && !chosen.current) {
            onError(undefined)
            onDone()
          }
        }}
      />
      {pending && <Spinner className="size-3.5 shrink-0" />}
    </div>
  )
}

/** The shapes that need room: the whole control, and an explicit save. */
function PanelEditor(props: InlineEditorProps) {
  const { row, kinds, record, onDone, onError } = props
  const { save, pending } = useSave(props)
  const [initial] = useState<FormValue>(() =>
    seedField(row.field, row.value, false)
  )
  const [value, setValue] = useState<FormValue>(initial)
  // A save with nothing changed closes: the seeded draft is the stored value
  // as the control shows it, which may round what is stored.
  const commit = () => {
    if (value === initial) {
      onError(undefined)
      onDone()
    } else void save(value)
  }
  return (
    <div
      className="flex w-full min-w-0 flex-col gap-3 rounded-lg border border-primary bg-background p-3 ring-3 ring-primary-soft"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          onError(undefined)
          onDone()
        }
      }}
    >
      <PropertyField
        field={row.field}
        value={value}
        onChange={setValue}
        mode="patch"
        kinds={kinds}
        self={record.id}
        idPrefix="sheet"
        bare
      />
      <div className="flex items-center gap-2">
        <Button size="sm" disabled={pending} onClick={commit}>
          {pending && <Spinner className="size-3.5" />}
          Save
        </Button>
        <Button
          size="sm"
          variant="ghost"
          disabled={pending}
          onClick={() => {
            onError(undefined)
            onDone()
          }}
        >
          Cancel
        </Button>
      </div>
    </div>
  )
}
