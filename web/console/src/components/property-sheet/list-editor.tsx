/** A list of values edited as the list it is: one box per item, stacked.
 *
 * Enter adds an item after the one being typed in, Backspace in an empty item
 * removes it, the arrows move between items and Alt with an arrow moves the
 * item itself (as do the up and down buttons and dragging by the grip), and a
 * paste of several lines becomes several items. A prose item (text, markdown)
 * is a box that grows with its lines instead: Enter and a paste stay inside
 * it, and the arrows leave it only from its first or last character. A choice
 * item (an enum) keeps Enter and the arrows for itself. Nothing is written until Save
 * (or ⌘Enter): the write is the WHOLE list in one PATCH carrying the version
 * the page read, and a list emptied on purpose is written as `[]`. */

import {
  useEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type KeyboardEvent,
} from "react"
import {
  ArrowDownIcon,
  ArrowUpIcon,
  GripVerticalIcon,
  PlusIcon,
  XIcon,
} from "lucide-react"

import { listItems, listWrite } from "./sheet-model"
import { type SheetRow } from "./sheet-rows"
import { useRecordPatch, writeError } from "./use-record-patch"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import type { SubstrateRecord } from "@/lib/api/types"
import type { FormField } from "@/lib/record-form"
import { controlFor, elementSpec, humanizeName } from "@/lib/record-schema"
import { cn } from "@/lib/utils"

interface Item {
  key: number
  text: string
}

/** Item keys: stable identities for focus and drag, never written. */
let nextKey = 0
const make = (text: string): Item => ({ key: nextKey++, text })

const FRAME =
  "w-full min-w-0 rounded-md border border-input bg-background px-2 text-sm outline-none focus-visible:border-primary focus-visible:ring-3 focus-visible:ring-primary-soft"
const BOX = `h-8 ${FRAME}`
const PROSE_BOX = `block field-sizing-content min-h-8 resize-none py-1.5 leading-normal ${FRAME}`

type Box = HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement

export function ListEditor({
  row,
  record,
  onDone,
  onError,
}: {
  row: SheetRow & { field: FormField }
  record: SubstrateRecord
  onDone: () => void
  onError: (message: string | undefined) => void
}) {
  const { field } = row
  const item = elementSpec(field.spec)
  const prose = controlFor(item) === "prose"
  const patch = useRecordPatch(record)
  const [items, setItems] = useState<Item[]>(() => {
    const seeded = listItems(row.value).map(make)
    return seeded.length ? seeded : [make("")]
  })
  const boxes = useRef(new Map<number, Box>())
  const [focus, setFocus] = useState<{ key: number; end?: boolean } | null>(
    () => ({ key: items[items.length - 1].key, end: true })
  )
  const [dragging, setDragging] = useState<number | null>(null)

  useEffect(() => {
    if (!focus) return
    const box = boxes.current.get(focus.key)
    box?.focus()
    if (!(box instanceof HTMLSelectElement) && box && focus.end) {
      // email and number boxes have no caret to place, and say so by throwing.
      try {
        box.setSelectionRange(box.value.length, box.value.length)
      } catch {
        /* the box keeps the browser's own caret */
      }
    }
  }, [focus])

  const at = (key: number) => items.findIndex((i) => i.key === key)
  const label = (i: number) => `${field.label} ${i + 1}`

  function change(key: number, text: string) {
    setItems((prev) => prev.map((i) => (i.key === key ? { ...i, text } : i)))
  }
  function insertAfter(key: number, texts: string[] = [""]) {
    const added = texts.map(make)
    setItems((prev) => {
      const i = prev.findIndex((p) => p.key === key)
      return [...prev.slice(0, i + 1), ...added, ...prev.slice(i + 1)]
    })
    setFocus({ key: added[added.length - 1].key, end: true })
  }
  function remove(key: number) {
    const i = at(key)
    const rest = items.filter((p) => p.key !== key)
    if (!rest.length) {
      const blank = make("")
      setItems([blank])
      setFocus({ key: blank.key })
      return
    }
    setItems(rest)
    setFocus({ key: rest[Math.max(0, i - 1)].key, end: true })
  }
  function move(key: number, by: -1 | 1) {
    const i = at(key)
    const j = i + by
    if (j < 0 || j >= items.length) return
    const out = [...items]
    ;[out[i], out[j]] = [out[j], out[i]]
    setItems(out)
    setFocus({ key })
  }
  function moveTo(key: number, target: number) {
    const i = at(key)
    const j = at(target)
    if (i < 0 || j < 0 || i === j) return
    const out = [...items]
    const [moved] = out.splice(i, 1)
    out.splice(j, 0, moved)
    setItems(out)
  }

  const cancel = () => {
    onError(undefined)
    onDone()
  }
  async function save() {
    const write = listWrite(
      field,
      row.value,
      items.map((i) => i.text)
    )
    if (write.error) return onError(write.error)
    if (!write.properties) return cancel()
    try {
      await patch.mutateAsync(write.properties)
      onError(undefined)
      onDone()
    } catch (error) {
      onError(writeError(error))
    }
  }

  function onKeyDown(e: KeyboardEvent<Box>, it: Item, i: number) {
    // A prose box keeps Enter for its own lines, and its arrows for its own
    // caret until the caret is already at the box's edge.
    const box = e.currentTarget
    // A choice keeps Enter and the arrows: they open it and step its value.
    const inChoice = box instanceof HTMLSelectElement
    const inProse = box instanceof HTMLTextAreaElement
    const atEdge =
      !inProse ||
      (e.key === "ArrowUp"
        ? box.selectionStart === 0 && box.selectionEnd === 0
        : box.selectionStart === box.value.length &&
          box.selectionEnd === box.value.length)
    if (e.key === "Escape") {
      e.preventDefault()
      cancel()
    } else if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault()
      void save()
    } else if (inChoice && e.key !== "Backspace") {
      return
    } else if (e.key === "Enter" && !inProse) {
      e.preventDefault()
      insertAfter(it.key)
    } else if (e.key === "Backspace" && it.text === "" && items.length > 1) {
      e.preventDefault()
      remove(it.key)
    } else if ((e.key === "ArrowUp" || e.key === "ArrowDown") && atEdge) {
      const by = e.key === "ArrowUp" ? -1 : 1
      if (e.altKey) {
        e.preventDefault()
        move(it.key, by)
      } else if (items[i + by]) {
        e.preventDefault()
        setFocus({ key: items[i + by].key, end: true })
      }
    }
  }

  function onPaste(e: ClipboardEvent<HTMLInputElement>, it: Item) {
    // A pasted paragraph is one prose item's text, not several items.
    if (prose) return
    const text = e.clipboardData.getData("text")
    const lines = text.split(/\r?\n/).map((l) => l.trim())
    if (lines.filter(Boolean).length < 2) return
    e.preventDefault()
    const box = e.currentTarget
    const before = box.value.slice(0, box.selectionStart ?? box.value.length)
    const after = box.value.slice(box.selectionEnd ?? box.value.length)
    const [first, ...rest] = lines.filter(Boolean)
    change(it.key, `${before}${first}`)
    const tail = [...rest]
    tail[tail.length - 1] = `${tail[tail.length - 1]}${after}`
    insertAfter(it.key, tail)
  }

  const inputType =
    field.inputType === "email" || field.inputType === "url"
      ? field.inputType
      : item.kind === "phone"
        ? "tel"
        : ["int", "integer", "number", "float", "decimal"].includes(item.kind)
          ? "number"
          : "text"
  const pending = patch.isPending

  return (
    <div
      data-slot="list-editor"
      className="flex w-full min-w-0 flex-col gap-2 rounded-lg border border-primary bg-background p-3 ring-3 ring-primary-soft"
    >
      <ol aria-label={field.label} className="flex flex-col gap-1.5">
        {items.map((it, i) => (
          <li
            key={it.key}
            data-dragging={dragging === it.key || undefined}
            onDragOver={(e) => {
              if (dragging === null) return
              e.preventDefault()
              moveTo(dragging, it.key)
            }}
            onDrop={(e) => {
              e.preventDefault()
              setDragging(null)
            }}
            className={cn(
              "group/item flex gap-1 data-dragging:opacity-50",
              prose ? "items-start" : "items-center"
            )}
          >
            <span
              draggable
              aria-hidden
              title="Drag to reorder"
              onDragStart={(e) => {
                e.dataTransfer.effectAllowed = "move"
                e.dataTransfer.setData("text/plain", it.text)
                setDragging(it.key)
              }}
              onDragEnd={() => setDragging(null)}
              className="flex h-8 w-4 shrink-0 cursor-grab items-center justify-center text-faint active:cursor-grabbing"
            >
              <GripVerticalIcon className="size-3.5" />
            </span>
            {item.values?.length ? (
              <select
                ref={(el) => {
                  if (el) boxes.current.set(it.key, el)
                  else boxes.current.delete(it.key)
                }}
                aria-label={label(i)}
                value={it.text}
                disabled={pending}
                onChange={(e) => change(it.key, e.target.value)}
                onKeyDown={(e) => onKeyDown(e, it, i)}
                className={BOX}
              >
                <option value="">Choose…</option>
                {item.values
                  .filter((v) => !v.deprecated || v.value === it.text)
                  .map((v) => (
                    <option key={v.value} value={v.value}>
                      {v.label || humanizeName(v.value)}
                    </option>
                  ))}
              </select>
            ) : prose ? (
              <textarea
                ref={(el) => {
                  if (el) boxes.current.set(it.key, el)
                  else boxes.current.delete(it.key)
                }}
                aria-label={label(i)}
                rows={1}
                value={it.text}
                disabled={pending}
                placeholder={
                  field.example ? `e.g. ${field.example}` : undefined
                }
                onChange={(e) => change(it.key, e.target.value)}
                onKeyDown={(e) => onKeyDown(e, it, i)}
                className={PROSE_BOX}
              />
            ) : (
              <input
                ref={(el) => {
                  if (el) boxes.current.set(it.key, el)
                  else boxes.current.delete(it.key)
                }}
                aria-label={label(i)}
                type={inputType}
                value={it.text}
                disabled={pending}
                placeholder={
                  field.example ? `e.g. ${field.example}` : undefined
                }
                onChange={(e) => change(it.key, e.target.value)}
                onKeyDown={(e) => onKeyDown(e, it, i)}
                onPaste={(e) => onPaste(e, it)}
                className={BOX}
              />
            )}
            <span className="flex shrink-0 items-center">
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={`Move ${label(i)} up`}
                title="Move up (Alt ↑)"
                disabled={pending || i === 0}
                onClick={() => move(it.key, -1)}
              >
                <ArrowUpIcon />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={`Move ${label(i)} down`}
                title="Move down (Alt ↓)"
                disabled={pending || i === items.length - 1}
                onClick={() => move(it.key, 1)}
              >
                <ArrowDownIcon />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={`Remove ${label(i)}`}
                title="Remove"
                disabled={pending}
                onClick={() => remove(it.key)}
              >
                <XIcon />
              </Button>
            </span>
          </li>
        ))}
      </ol>
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="self-start text-muted-foreground"
        disabled={pending}
        onClick={() => insertAfter(items[items.length - 1].key)}
      >
        <PlusIcon />
        Add another
      </Button>
      <div className="flex flex-wrap items-center gap-2 border-t pt-2.5">
        <Button size="sm" disabled={pending} onClick={() => void save()}>
          {pending && <Spinner className="size-3.5" />}
          Save
        </Button>
        <Button size="sm" variant="ghost" disabled={pending} onClick={cancel}>
          Cancel
        </Button>
        <span className={cn("ml-auto text-xs text-faint", "max-sm:hidden")}>
          {prose ? "Enter starts a new line" : "Enter adds a line"} · ⌘Enter
          saves · Esc cancels
        </span>
      </div>
    </div>
  )
}
