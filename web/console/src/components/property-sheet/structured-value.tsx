/** A declared object, a list of them, or a keyed map, read and edited in one
 * place. What a value reads as is what it is edited as: a click on any value,
 * however deep, opens that value's own editor where it sits, and Enter or
 * leaving the box saves it as one write of the whole property (there is no
 * write at a nested path), asserting the version the editor opened on. The
 * value around it stays as it was read.
 *
 * A list of objects whose shown fields fit a row reads as a table, one row
 * per item, and as labelled rows where the table has no room. A list whose
 * items carry text or lists of their own reads as stacked items, each headed
 * by the field that names it, its values as a grid of labelled facts and its
 * own lists as sections under it, as deep as the declaration nests. A field
 * no item holds is not shown; an item's menu sets one. A new item is a small
 * form asking for the fields an item must hold, written once it has them.
 * Which field names an item, the order fields read in and which two fields
 * read as one range are inferred, never declared (structured-layout.ts). */

import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react"
import { useIsMutating, useQueryClient } from "@tanstack/react-query"
import {
  ChevronDownIcon,
  ChevronRightIcon,
  ChevronsDownUpIcon,
  ChevronsUpDownIcon,
  MoreHorizontalIcon,
  PencilIcon,
  PlusIcon,
} from "lucide-react"

import { focusLost } from "./focus-return"
import { InlineEditor } from "./inline-editor"
import { Empty, JsonBlock, LeafValue, LooseValue } from "./leaf-value"
import type { Lift } from "./sheet-model"
import {
  recordPatchKey,
  useEditBase,
  useRecordPatch,
  writeError,
} from "./use-record-patch"
import { PropertyField } from "@/components/record/property-field"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Spinner } from "@/components/ui/spinner"
import { useUnits } from "@/hooks/use-units"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { scalarText } from "@/lib/format"
import { enumLabel } from "@/lib/grid-values"
import { coverageWords } from "@/lib/observations-time"
import {
  fieldOf,
  seedField,
  toFieldValue,
  type FieldBag,
  type FormValue,
} from "@/lib/record-form"
import { elementSpec, personWritable, type PropSpec } from "@/lib/record-schema"
import {
  copyOf,
  insertAt,
  moveAt,
  pathKey,
  removeAt,
  setIn,
  unsetIn,
  type Path,
} from "@/lib/structured-edit"
import {
  cellFields,
  cellName,
  cellsFor,
  fieldsOf,
  fieldsToAdd,
  held,
  isNestedField,
  isProseField,
  isStructured,
  itemSummary,
  listLayout,
  objectCells,
  orderedFields,
  rangePairs,
  rangeText,
  type Cell,
  type ListLayout,
  type RangePair,
} from "@/lib/structured-layout"
import type { UnitReader } from "@/lib/unit-reader"
import { formatQuantityText } from "@/lib/units"
import { cn } from "@/lib/utils"

// ── the structure's shared state ────────────────────────────────────────────

/** A new item, or an object that holds nothing yet, being written whole:
 * where it goes and what it starts from. */
interface Draft {
  /** The list it joins (and `index`, the item it is inserted before), or,
   * without an index, the object it is written as. */
  path: Path
  index?: number
  /** What the item starts as: a copy of the item it duplicates, or nothing. */
  base: Record<string, unknown>
  /** The fields its form asks for, in order. */
  fields: string[]
  /** The item it copies, by its number, for the form's heading. */
  copyOf?: number
  /** The field that names the list's items, which its form asks for first. */
  identity?: PropSpec
  /** The control that opened the form, which takes focus back when the
   * form closes unwritten. */
  from: string
}

interface Editing {
  record: SubstrateRecord
  kinds: KindInfo[]
  units: UnitReader
  /** The value whose editor is open, by its path key. */
  open: string | null
  /** Opens a value's editor, once any write before it has landed. */
  start: (path: Path) => void
  /** Closes `key`'s editor, and nothing opened since. */
  stop: (key: string) => void
  errors: Readonly<Record<string, string>>
  setError: (key: string, message: string | undefined) => void
  /** Writes the whole property; a refusal is said under `at`. */
  write: (next: unknown, at: Path) => Promise<boolean>
  pending: boolean
  /** A field an item is being given that its list shows no column for. */
  adding: { item: string; field: string } | null
  addField: (item: Path, field: string, cell: Path) => void
  draft: Draft | null
  setDraft: (draft: Draft | null) => void
  /** The elements focus returns to, by key: each value, each list's add
   * button and each item's menu. */
  anchors: Map<string, HTMLElement>
  /** Moves focus to the anchor `key` once it is on the page. */
  focus: (key: string) => void
}

interface Structure {
  property: string
  /** The property's stored value: what every edit starts from. */
  root: unknown
  edit?: Editing
  collapsed: ReadonlySet<string>
  setCollapsed: (keys: readonly string[], collapse: boolean) => void
}

const StructureContext = createContext<Structure | undefined>(undefined)

function useStructure(): Structure {
  const structure = useContext(StructureContext)
  if (!structure) throw new Error("outside a structured value")
  return structure
}

/** The key the errors of the item holding `path` are kept under. */
const holderOf = (path: Path): Path => path.slice(0, -1)

// ── the entry point ─────────────────────────────────────────────────────────

export interface StructuredValueProps {
  spec: PropSpec
  value: unknown
  /** Where the value may be edited: the record, the registry, and the
   * property the value is held under. Read-only without. */
  edit?: { record: SubstrateRecord; kinds: KindInfo[]; property: string }
  /** The property's own label, which the structure's toolbar sits beside. */
  heading?: ReactNode
  /** What the surface says about the value as a whole (who holds it). */
  aside?: ReactNode
}

export function StructuredValue(props: StructuredValueProps) {
  return props.edit ? (
    <EditableStructure {...props} edit={props.edit} />
  ) : (
    <ReadStructure {...props} />
  )
}

function useCollapsed() {
  const [collapsed, setState] = useState<ReadonlySet<string>>(new Set())
  const setCollapsed = (keys: readonly string[], collapse: boolean) =>
    setState((prev) => {
      const next = new Set(prev)
      for (const key of keys) {
        if (collapse) next.add(key)
        else next.delete(key)
      }
      return next
    })
  return { collapsed, setCollapsed }
}

function ReadStructure({ spec, value, heading, aside }: StructuredValueProps) {
  const { collapsed, setCollapsed } = useCollapsed()
  return (
    <StructureContext.Provider
      value={{ property: "", root: value, collapsed, setCollapsed }}
    >
      <Root spec={spec} value={value} heading={heading} aside={aside} />
    </StructureContext.Provider>
  )
}

function EditableStructure({
  spec,
  value,
  heading,
  aside,
  edit,
}: StructuredValueProps & { edit: NonNullable<StructuredValueProps["edit"]> }) {
  const { record, kinds, property } = edit
  const units = useUnits()
  const client = useQueryClient()
  const patch = useRecordPatch(record)
  const writing = useIsMutating({ mutationKey: recordPatchKey(record) }) > 0
  const { collapsed, setCollapsed } = useCollapsed()
  const [opened, setOpened] = useState<string | null>(null)
  const [queued, setQueued] = useState<string | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [adding, setAdding] = useState<Editing["adding"]>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [anchors] = useState(() => new Map<string, HTMLElement>())
  // A focus asked for before its target is on the page (the item a write
  // just added) waits for the render that brings it.
  const focusNext = useRef<string | null>(null)
  useEffect(() => {
    const key = focusNext.current
    const el = key ? anchors.get(key) : undefined
    if (!el) return
    focusNext.current = null
    el.focus({ preventScroll: false })
  })
  // An editor opened while an earlier write is still landing would assert
  // the version the page holds before that write, and be refused as a
  // conflict with the reader's own edit: it opens once the write has landed
  // and the page has re-read, and stays open through its own write.
  if (!writing && queued) {
    setOpened(queued)
    setQueued(null)
  }
  const open = opened

  // A closed editor takes focus with it; the value it edited takes it back,
  // so Tab carries on from there.
  const last = useRef(open)
  useEffect(() => {
    const prev = last.current
    last.current = open
    if (prev && prev !== open && focusLost()) {
      anchors.get(prev)?.focus({ preventScroll: true })
    }
  }, [open, anchors])

  function setError(key: string, message: string | undefined) {
    setErrors((prev) => {
      if (!message && !(key in prev)) return prev
      const next = { ...prev }
      if (message) next[key] = message
      else delete next[key]
      return next
    })
  }

  const editing: Editing = {
    record,
    kinds,
    units,
    open,
    start: (path) => {
      const key = pathKey(property, path)
      if (client.isMutating({ mutationKey: recordPatchKey(record) }) > 0) {
        setQueued(key)
      } else {
        setOpened(key)
        setQueued(null)
      }
    },
    stop: (key) => {
      setOpened((current) => (current === key ? null : current))
      setQueued((current) => (current === key ? null : current))
      setAdding((current) =>
        current && key.startsWith(`${current.item}/`) ? null : current
      )
    },
    errors,
    setError,
    write: async (next, at) => {
      const key = pathKey(property, at)
      try {
        await patch.mutateAsync({ [property]: next })
        setError(key, undefined)
        return true
      } catch (error) {
        setError(key, writeError(error))
        return false
      }
    },
    pending: patch.isPending,
    adding,
    addField: (item, field, cell) => {
      setAdding({ item: pathKey(property, item), field })
      editing.start(cell)
    },
    draft,
    setDraft,
    anchors,
    focus: (key) => {
      focusNext.current = key
    },
  }

  return (
    <StructureContext.Provider
      value={{ property, root: value, edit: editing, collapsed, setCollapsed }}
    >
      <Root spec={spec} value={value} heading={heading} aside={aside} />
    </StructureContext.Provider>
  )
}

function Root({
  spec,
  value,
  heading,
  aside,
}: {
  spec: PropSpec
  value: unknown
  heading?: ReactNode
  aside?: ReactNode
}) {
  if (spec.keyed) {
    return (
      <div className="flex flex-col gap-1.5">
        {(heading || aside) && (
          <Toolbar heading={heading} aside={aside} count={count(value)} />
        )}
        <KeyedMap spec={spec} value={value} path={[]} />
      </div>
    )
  }
  if (spec.repeated) {
    return (
      <ObjectList
        spec={spec}
        items={Array.isArray(value) ? value : []}
        path={[]}
        heading={heading}
        aside={aside}
      />
    )
  }
  return (
    <div className="group/item flex flex-col gap-1.5">
      {(heading || aside) && (
        <Toolbar heading={heading} aside={aside}>
          <ObjectFields spec={spec} value={value} path={[]} />
        </Toolbar>
      )}
      <ObjectBody spec={spec} value={value} path={[]} />
      <ObjectDraft spec={spec} path={[]} />
    </div>
  )
}

/** The form an object that holds nothing is filled in with, where it reads. */
function ObjectDraft({ spec, path }: { spec: PropSpec; path: Path }) {
  const { property, edit } = useStructure()
  const draft = edit?.draft
  if (
    !draft ||
    draft.index !== undefined ||
    pathKey(property, draft.path) !== pathKey(property, path)
  ) {
    return null
  }
  return <DraftItem spec={spec} draft={draft} />
}

function count(value: unknown): number | undefined {
  if (Array.isArray(value)) return value.length
  if (value && typeof value === "object") return Object.keys(value).length
  return undefined
}

/** A structure's heading line: its label and how many items it holds, what
 * the surface says about it, and what can be done to it as a whole. */
function Toolbar({
  heading,
  aside,
  count,
  children,
  nested = false,
}: {
  heading?: ReactNode
  aside?: ReactNode
  count?: number
  children?: ReactNode
  nested?: boolean
}) {
  return (
    <div
      className={cn(
        "flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1",
        nested ? "min-h-7" : "min-h-9 border-b pb-1"
      )}
    >
      {heading}
      {count !== undefined && count > 0 && (
        <span className="text-xs text-faint tabular-nums">{count}</span>
      )}
      <span className="ml-auto flex items-center gap-1">
        {aside}
        {children}
      </span>
    </div>
  )
}

// ── one object's own fields ─────────────────────────────────────────────────

/** One object's fields: its values as a run of labelled facts, its prose
 * across the full width, and each structure it holds as a section of its
 * own. `skip` is the field its heading already shows. */
function ObjectBody({
  spec,
  value,
  path,
  skip,
}: {
  spec: PropSpec
  value: unknown
  path: Path
  skip?: PropSpec
}) {
  const { property, edit } = useStructure()
  const key = pathKey(property, path)
  if (value !== undefined && value !== null && !isBag(value)) {
    return <JsonBlock value={value} />
  }
  const extra =
    edit?.adding && edit.adding.item === key ? [edit.adding.field] : []
  const cells = objectCells(spec, value, extra, skip).filter(
    (cell) => cell.kind !== "field" || cell.field.name !== skip?.name
  )
  const bag = fieldsOf(value)
  // Prose, and a structure no field declares, take the full width.
  const wide = (cell: Cell) =>
    cell.kind === "loose"
      ? typeof bag[cell.key] === "object" && bag[cell.key] !== null
      : cellFields(cell).some(isProseField)
  const facts = cells.filter(
    (cell) => !wide(cell) && !cellFields(cell).some((f) => isNestedField(f))
  )
  const prose = cells.filter(wide)
  // A structure the object holds is its own section; an empty one shows only
  // where it can be filled in, which is the one way to start it.
  const nested = orderedFields(elementSpec(spec)).filter(
    (f) => isNestedField(f) && (held(bag[f.name]) || (edit && writableField(f)))
  )
  const error = edit?.errors[key]
  if (!facts.length && !prose.length && !nested.length) {
    return (
      <div className="flex flex-col gap-1.5">
        <Empty />
        {error && <ErrorLine message={error} />}
      </div>
    )
  }
  return (
    <div className="flex min-w-0 flex-col gap-2.5">
      {facts.length > 0 && (
        <dl className="flex min-w-0 flex-wrap items-baseline gap-x-6 gap-y-1.5">
          {facts.map((cell) => (
            <Fact key={cellName(cell)} cell={cell}>
              <CellValue cell={cell} item={bag} path={path} />
            </Fact>
          ))}
        </dl>
      )}
      {prose.map((cell) => (
        <dl key={cellName(cell)} className="flex min-w-0">
          <Fact cell={cell}>
            <CellValue cell={cell} item={bag} path={path} />
          </Fact>
        </dl>
      ))}
      {error && <ErrorLine message={error} />}
      {nested.map((field) => (
        <NestedSection
          key={field.name}
          field={field}
          value={bag[field.name]}
          path={[...path, field.name]}
        />
      ))}
    </div>
  )
}

function isBag(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

/** One labelled value of an object, its label before it on one line. */
function Fact({ cell, children }: { cell: Cell; children: ReactNode }) {
  const fields = cellFields(cell)
  const description = fields.map((f) => f.description).find(Boolean)
  return (
    <div className="flex max-w-full min-w-0 items-baseline gap-2">
      <dt className="shrink-0 text-xs text-faint" title={description}>
        {cell.label}
      </dt>
      <dd className="min-w-0 text-sm">{children}</dd>
    </div>
  )
}

/** A structure one object holds, under its own heading. */
function NestedSection({
  field,
  value,
  path,
}: {
  field: PropSpec
  value: unknown
  path: Path
}) {
  const heading = (
    <h4 className="text-xs font-medium text-muted-foreground">{field.label}</h4>
  )
  if (isStructured(field) && field.repeated) {
    return (
      <ObjectList
        spec={field}
        items={Array.isArray(value) ? value : []}
        path={path}
        heading={heading}
        nested
      />
    )
  }
  if (isStructured(field)) {
    return (
      <section className="flex min-w-0 flex-col gap-1.5">
        <Toolbar heading={heading} nested>
          <ObjectFields spec={field} value={value} path={path} />
        </Toolbar>
        <div className="group/item border-l pl-3">
          <ObjectBody spec={field} value={value} path={path} />
          <ObjectDraft spec={field} path={path} />
        </div>
      </section>
    )
  }
  if (field.keyed) {
    return (
      <NestedMap field={field} value={value} path={path} heading={heading} />
    )
  }
  return (
    <section className="flex min-w-0 flex-col gap-1.5">
      <Toolbar heading={heading} nested />
      <Leaf spec={field} value={value} path={path} />
    </section>
  )
}

// ── a list of objects ───────────────────────────────────────────────────────

function ObjectList({
  spec,
  items,
  path,
  heading,
  aside,
  nested = false,
}: {
  spec: PropSpec
  items: unknown[]
  path: Path
  heading?: ReactNode
  aside?: ReactNode
  nested?: boolean
}) {
  const { property, edit, collapsed, setCollapsed } = useStructure()
  const keys = items.map((_, i) => pathKey(property, [...path, i]))
  const adding = edit?.adding
  const extra =
    adding && keys.includes(adding.item) ? [adding.field] : ([] as string[])
  // The layout is decided by what is stored, so a column set right now never
  // turns a table into stacked items under the open editor.
  const layout = listLayout(spec, items, extra)
  const draft =
    edit?.draft &&
    edit.draft.index !== undefined &&
    pathKey(property, edit.draft.path) === pathKey(property, path)
      ? edit.draft
      : undefined
  const writable = Boolean(edit) && personWritable(spec) && !spec.managed
  const allCollapsed =
    layout.mode === "stack" &&
    keys.length > 1 &&
    keys.every((k) => collapsed.has(k))
  const showToolbar = Boolean(heading || aside || nested)
  return (
    <section className="flex min-w-0 flex-col gap-1.5">
      {showToolbar && (
        <Toolbar
          heading={heading}
          aside={aside}
          count={items.length}
          nested={nested}
        >
          {layout.mode === "stack" && items.length > 1 && (
            <Button
              type="button"
              variant="ghost"
              size="xs"
              className="text-muted-foreground"
              onClick={() => setCollapsed(keys, !allCollapsed)}
            >
              {allCollapsed ? (
                <ChevronsUpDownIcon aria-hidden />
              ) : (
                <ChevronsDownUpIcon aria-hidden />
              )}
              {allCollapsed ? "Expand all" : "Collapse all"}
            </Button>
          )}
          {writable && (
            <AddItemButton
              spec={spec}
              path={path}
              at={items.length}
              identity={layout.identity}
            />
          )}
        </Toolbar>
      )}
      {layout.mode === "table" ? (
        <ItemTable
          spec={spec}
          items={items}
          path={path}
          layout={layout}
          draft={draft}
          writable={writable}
        />
      ) : (
        <ItemStack
          spec={spec}
          items={items}
          path={path}
          layout={layout}
          draft={draft}
          writable={writable}
        />
      )}
      {!items.length && !draft && (
        <p className="px-0.5 text-[13px] text-faint">None yet</p>
      )}
      {edit?.errors[pathKey(property, path)] && (
        <ErrorLine message={edit.errors[pathKey(property, path)]} />
      )}
    </section>
  )
}

/** "Add item": a new item at `at`, asked for as a small form. */
function AddItemButton({
  spec,
  path,
  at,
  identity,
}: {
  spec: PropSpec
  path: Path
  at: number
  identity?: PropSpec
}) {
  const { property, edit } = useStructure()
  if (!edit) return null
  const anchor = `${pathKey(property, path)}#add`
  return (
    <Button
      ref={(el: HTMLButtonElement | null) => {
        if (el) edit.anchors.set(anchor, el)
        else edit.anchors.delete(anchor)
      }}
      type="button"
      variant="ghost"
      size="xs"
      className="text-muted-foreground"
      aria-label={`Add an item to ${spec.label}`}
      disabled={Boolean(edit.draft)}
      onClick={() => edit.setDraft(newDraft(spec, path, at, anchor, identity))}
    >
      <PlusIcon aria-hidden />
      Add item
    </Button>
  )
}

/** The fields a new object's form asks for, in the order its list reads
 * them: every field it must hold, a structure included (written with it, or
 * the write is refused), and the fields with a declared default, which a
 * write stores anyway. */
function draftFields(item: PropSpec, identity?: PropSpec): string[] {
  return orderedFields(item, identity)
    .filter(
      (f) =>
        !f.managed &&
        personWritable(f) &&
        (f.required || (f.default !== undefined && !isNestedField(f)))
    )
    .map((f) => f.name)
}

/** A draft of a new item in the list `spec` at `path`, before `index`. */
function newDraft(
  spec: PropSpec,
  path: Path,
  index: number,
  from: string,
  identity?: PropSpec
): Draft {
  const fields = draftFields(elementSpec(spec), identity)
  return { path, index, base: {}, fields, identity, from }
}

/** Whether an object holding nothing can only be written whole: it must
 * hold a field, so a write of one field alone would be refused. */
function needsDraft(spec: PropSpec, value: unknown): boolean {
  return (
    !held(value) &&
    (spec.fields ?? []).some(
      (f) => f.required && !f.managed && personWritable(f)
    )
  )
}

/** A draft of an item's copy: the copy's own values, offered to change, and
 * its structures carried over as they are. */
function copyDraft(
  spec: PropSpec,
  path: Path,
  index: number,
  value: unknown,
  from: string,
  identity?: PropSpec
): Draft {
  const base = copyOf(fieldsOf(value))
  const item = elementSpec(spec)
  const fields = orderedFields(item, identity).filter(
    (f) =>
      !isNestedField(f) &&
      !f.managed &&
      personWritable(f) &&
      (f.required || held(base[f.name]))
  )
  return {
    path,
    index: index + 1,
    base,
    fields: fields.map((f) => f.name),
    copyOf: index + 1,
    identity,
    from,
  }
}

/** A list's rendered items with a draft's form at the place it will take. */
function withDraft(
  items: ReactNode[],
  index: number,
  draft: ReactNode
): ReactNode[] {
  const at = Math.min(index, items.length)
  return [...items.slice(0, at), draft, ...items.slice(at)]
}

function ItemTable({
  spec,
  items,
  path,
  layout,
  draft,
  writable,
}: {
  spec: PropSpec
  items: unknown[]
  path: Path
  layout: ListLayout
  draft?: Draft
  writable: boolean
}) {
  // The cells, the filler that takes the spare width, and the actions.
  const width = layout.cells.length + 1 + (writable ? 1 : 0)
  const rows: ReactNode[] = items.map((item, i) => (
    <ItemRow
      key={i}
      spec={spec}
      item={item}
      index={i}
      size={items.length}
      path={path}
      layout={layout}
      writable={writable}
      width={width}
    />
  ))
  const body = draft
    ? withDraft(
        rows,
        draft.index ?? rows.length,
        <tr key="draft" className="block @lg/items:table-row">
          <td colSpan={width} className="block py-2 @lg/items:table-cell">
            <DraftItem spec={spec} draft={draft} />
          </td>
        </tr>
      )
    : rows
  if (!items.length && !draft) return null
  return (
    <div className="@container/items min-w-0 overflow-x-auto">
      <table className="block w-full border-collapse text-sm @lg/items:table">
        <thead className="sr-only @lg/items:not-sr-only @lg/items:table-header-group">
          <tr className="border-b">
            {layout.cells.map((cell) => (
              <th
                key={cellName(cell)}
                scope="col"
                title={cellFields(cell)
                  .map((f) => f.description)
                  .find(Boolean)}
                className="py-1.5 pr-6 pl-0.5 text-left align-bottom text-xs font-normal whitespace-nowrap text-faint"
              >
                {cell.label}
              </th>
            ))}
            <th aria-hidden className="w-full p-0" />
            {writable && (
              <th scope="col" className="w-8">
                <span className="sr-only">Actions</span>
              </th>
            )}
          </tr>
        </thead>
        <tbody className="block @lg/items:table-row-group">{body}</tbody>
      </table>
    </div>
  )
}

/** One item as a table row, or as labelled rows where the table has no
 * room. */
function ItemRow({
  spec,
  item,
  index,
  size,
  path,
  layout,
  writable,
  width,
}: {
  spec: PropSpec
  item: unknown
  index: number
  size: number
  path: Path
  layout: ListLayout
  writable: boolean
  width: number
}) {
  const { property, edit } = useStructure()
  const itemPath = [...path, index]
  const error = edit?.errors[pathKey(property, itemPath)]
  const bag = fieldsOf(item)
  return (
    <>
      <tr
        data-item={index + 1}
        className="group/item relative block border-b py-1.5 last:border-b-0 @lg/items:table-row @lg/items:py-0"
      >
        {layout.cells.map((cell) => (
          <td
            key={cellName(cell)}
            className="flex min-w-0 items-baseline gap-3 py-0.5 pr-10 @lg/items:table-cell @lg/items:py-1.5 @lg/items:pr-6 @lg/items:pl-0.5 @lg/items:align-top"
          >
            <span className="w-28 shrink-0 text-xs text-faint @lg/items:hidden">
              {cell.label}
            </span>
            {/* A cell is as wide as what it holds: the filler column takes
                the spare width, and a value never shrinks to make room. */}
            <span className="min-w-0 @lg/items:inline-block @lg/items:w-max @lg/items:max-w-80">
              <CellValue cell={cell} item={bag} path={itemPath} />
            </span>
          </td>
        ))}
        <td aria-hidden className="hidden p-0 @lg/items:table-cell" />
        {writable && (
          <td className="absolute top-1 right-0 @lg/items:static @lg/items:w-8 @lg/items:px-0 @lg/items:py-1 @lg/items:align-top">
            <ItemMenu
              spec={spec}
              item={item}
              index={index}
              size={size}
              path={path}
              layout={layout}
            />
          </td>
        )}
      </tr>
      {error && (
        <tr className="block @lg/items:table-row">
          <td colSpan={width} className="block pb-2 @lg/items:table-cell">
            <ErrorLine message={error} />
          </td>
        </tr>
      )}
    </>
  )
}

function ItemStack({
  spec,
  items,
  path,
  layout,
  draft,
  writable,
}: {
  spec: PropSpec
  items: unknown[]
  path: Path
  layout: ListLayout
  draft?: Draft
  writable: boolean
}) {
  const entries: ReactNode[] = items.map((item, i) => (
    <StackItem
      key={i}
      spec={spec}
      item={item}
      index={i}
      size={items.length}
      path={path}
      layout={layout}
      writable={writable}
    />
  ))
  const body = draft
    ? withDraft(
        entries,
        draft.index ?? entries.length,
        <li key="draft" className="py-2">
          <DraftItem spec={spec} draft={draft} />
        </li>
      )
    : entries
  if (!body.length) return null
  return <ol className="flex min-w-0 flex-col">{body}</ol>
}

/** One item of a stacked list: a heading naming it, and its fields under. */
function StackItem({
  spec,
  item,
  index,
  size,
  path,
  layout,
  writable,
}: {
  spec: PropSpec
  item: unknown
  index: number
  size: number
  path: Path
  layout: ListLayout
  writable: boolean
}) {
  const { property, collapsed, setCollapsed } = useStructure()
  const itemPath = [...path, index]
  const key = pathKey(property, itemPath)
  const folded = collapsed.has(key)
  const identity = layout.identity
  const bag = fieldsOf(item)
  const name = itemName(layout, item, index)
  return (
    <li
      data-item={index + 1}
      className="group/item min-w-0 border-b py-2 last:border-b-0"
    >
      <div className="flex min-h-8 min-w-0 items-center gap-1">
        <button
          type="button"
          className="flex size-6 shrink-0 items-center justify-center rounded-md text-faint hover:bg-hover hover:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          aria-expanded={!folded}
          aria-label={`${folded ? "Expand" : "Collapse"} ${name}`}
          onClick={() => setCollapsed([key], !folded)}
        >
          {folded ? (
            <ChevronRightIcon aria-hidden className="size-3.5" />
          ) : (
            <ChevronDownIcon aria-hidden className="size-3.5" />
          )}
        </button>
        <span className="mr-1.5 w-5 shrink-0 text-right text-xs text-faint tabular-nums">
          {index + 1}
        </span>
        <span className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2 gap-y-0.5">
          {identity ? (
            <>
              <span className="text-xs text-faint">{identity.label}</span>
              <span className="min-w-0 font-medium">
                <Leaf
                  spec={identity}
                  value={bag[identity.name]}
                  path={[...itemPath, identity.name]}
                />
              </span>
            </>
          ) : (
            <span className="text-muted-foreground">Item {index + 1}</span>
          )}
          {folded && <Summary layout={layout} item={item} />}
        </span>
        {writable && (
          <ItemMenu
            spec={spec}
            item={item}
            index={index}
            size={size}
            path={path}
            layout={layout}
          />
        )}
      </div>
      {!folded && (
        <div className="min-w-0 pt-1.5 pb-1 pl-[3.625rem]">
          <ObjectBody
            spec={elementSpec(spec)}
            value={item}
            path={itemPath}
            skip={identity}
          />
        </div>
      )}
    </li>
  )
}

/** What a folded item says beside its name, each value under its label. */
function Summary({ layout, item }: { layout: ListLayout; item: unknown }) {
  const bag = fieldsOf(item)
  const parts = itemSummary(layout, item)
  if (!parts.length) return null
  return (
    <span className="flex min-w-0 flex-wrap items-baseline gap-x-3 text-[13px]">
      {parts.map((part) => (
        <span key={part.label} className="flex items-baseline gap-1">
          <span className="text-xs text-faint">{part.label}</span>
          <span className="text-muted-foreground">
            {part.count !== undefined
              ? part.count
              : part.cell && cellText(part.cell, bag)}
          </span>
        </span>
      ))}
    </span>
  )
}

/** How an item is named in a menu or a question: its identity's words, or
 * its number. */
function itemName(layout: ListLayout, item: unknown, index: number): string {
  const identity = layout.identity
  const value = identity ? fieldsOf(item)[identity.name] : undefined
  const text = identity && held(value) ? valueText(identity, value) : ""
  return text ? `item ${index + 1} (${text})` : `item ${index + 1}`
}

/** An item's actions: add one after it, copy it, move it, set a field it
 * does not hold, remove it. Each is one write but the copy and the new item,
 * which are drafts until they hold what an item must. */
function ItemMenu({
  spec,
  item,
  index,
  size,
  path,
  layout,
}: {
  spec: PropSpec
  item: unknown
  index: number
  size: number
  path: Path
  layout: ListLayout
}) {
  const { property, root, edit } = useStructure()
  const [confirm, setConfirm] = useState(false)
  if (!edit) return null
  const itemPath = [...path, index]
  const anchor = `${pathKey(property, itemPath)}#menu`
  const name = itemName(layout, item, index)
  const addable = fieldsToAdd(layout.fields, item)
  const busy = edit.pending || Boolean(edit.draft)
  // The moved item's menu keeps focus, wherever the item lands.
  const move = (to: number) => {
    edit.focus(`${pathKey(property, [...path, to])}#menu`)
    void edit.write(moveAt(root, path, index, to), path)
  }
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              ref={(el: HTMLButtonElement | null) => {
                if (el) edit.anchors.set(anchor, el)
                else edit.anchors.delete(anchor)
              }}
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label={`Actions for ${name}`}
              className="text-muted-foreground opacity-60 group-hover/item:opacity-100 focus-visible:opacity-100 data-popup-open:opacity-100 pointer-coarse:opacity-100"
            />
          }
        >
          <MoreHorizontalIcon />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48">
          <DropdownMenuItem
            disabled={busy}
            onClick={() =>
              edit.setDraft(
                newDraft(spec, path, index + 1, anchor, layout.identity)
              )
            }
          >
            Add item below
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={busy}
            onClick={() =>
              edit.setDraft(
                copyDraft(spec, path, index, item, anchor, layout.identity)
              )
            }
          >
            Duplicate
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={busy || index === 0}
            onClick={() => move(index - 1)}
          >
            Move up
          </DropdownMenuItem>
          <DropdownMenuItem
            disabled={busy || index === size - 1}
            onClick={() => move(index + 1)}
          >
            Move down
          </DropdownMenuItem>
          {addable.length > 0 && (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger disabled={busy}>
                Set a field
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="max-h-72 w-56">
                {addable.map((field) => (
                  <DropdownMenuItem
                    key={field.name}
                    onClick={() =>
                      edit.addField(
                        itemPath,
                        field.name,
                        cellPathFor(layout, item, itemPath, field.name)
                      )
                    }
                  >
                    {field.label}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            disabled={busy}
            onClick={() => setConfirm(true)}
          >
            Remove
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {confirm && (
        <RemoveItem
          spec={spec}
          item={item}
          index={index}
          size={size}
          path={path}
          layout={layout}
          name={name}
          onClose={() => setConfirm(false)}
        />
      )}
    </>
  )
}

/** "Remove item 3 from Slots?", asked before the write. The removal is
 * worked out from the property as it stood when the question was asked and
 * asserts that version: an item another writer moved in the meantime is
 * refused as a conflict, never removed in its neighbour's place. */
function RemoveItem({
  spec,
  item,
  index,
  size,
  path,
  layout,
  name,
  onClose,
}: {
  spec: PropSpec
  item: unknown
  index: number
  size: number
  path: Path
  layout: ListLayout
  name: string
  onClose: () => void
}) {
  const { property, root, edit } = useStructure()
  const [base] = useState(root)
  const patch = useRecordPatch(edit!.record, useEditBase(edit!.record))
  const [error, setError] = useState<string>()
  if (!edit) return null
  const children = layout.fields.filter(
    (f) =>
      isStructured(f) &&
      f.repeated &&
      Array.isArray(fieldsOf(item)[f.name]) &&
      (fieldsOf(item)[f.name] as unknown[]).length > 0
  )
  async function remove() {
    try {
      await patch.mutateAsync({ [property]: removeAt(base, path, index) })
      // Focus goes to the item that takes this one's place, else the list's
      // add button.
      edit!.focus(
        size > 1
          ? `${pathKey(property, [...path, Math.min(index, size - 2)])}#menu`
          : `${pathKey(property, path)}#add`
      )
      onClose()
    } catch (e) {
      setError(writeError(e))
    }
  }
  return (
    <ConfirmDialog
      title={`Remove ${name} from ${spec.label}?`}
      consequence={
        children.length
          ? `It goes with what it holds: ${children
              .map(
                (f) =>
                  `${(fieldsOf(item)[f.name] as unknown[]).length} in ${f.label}`
              )
              .join(", ")}. The record keeps the change in its history.`
          : "The record keeps the change in its history."
      }
      confirm="Remove"
      destructive
      pending={patch.isPending}
      error={error}
      onConfirm={() => void remove()}
      onClose={onClose}
    />
  )
}

/** Where the editor for `field` of an item opens once the field is shown:
 * its own cell, or the range cell it shares with its other end. */
function cellPathFor(
  layout: ListLayout,
  item: unknown,
  itemPath: Path,
  field: string
): Path {
  // A table row shows its list's columns; a stacked item, what it holds.
  const bag = fieldsOf(item)
  const shown = new Set(
    layout.mode === "table"
      ? layout.cells.flatMap(cellFields).map((f) => f.name)
      : layout.fields
          .filter((f) => f.required || held(bag[f.name]))
          .map((f) => f.name)
  )
  shown.add(field)
  const cells = cellsFor(
    layout.fields.filter((f) => shown.has(f.name)),
    layout.pairs
  )
  const cell = cells.find((c) => cellFields(c).some((f) => f.name === field))
  return [...itemPath, cell ? cellName(cell) : field]
}

// ── one value ───────────────────────────────────────────────────────────────

/** A cell's value: one field's, a range's two ends read as one, or a key
 * no field declares, read as it is stored. */
function CellValue({
  cell,
  item,
  path,
}: {
  cell: Cell
  item: Record<string, unknown>
  path: Path
}) {
  if (cell.kind === "loose") return <LooseValue value={item[cell.key]} />
  if (cell.kind === "range") {
    return <RangeValue pair={cell.pair} item={item} path={path} />
  }
  return (
    <Leaf
      spec={cell.field}
      value={item[cell.field.name]}
      path={[...path, cell.field.name]}
    />
  )
}

/** A cell's value as plain words, for a folded item's summary. */
function cellText(cell: Cell, item: Record<string, unknown>): string {
  if (cell.kind === "loose") return scalarText(item[cell.key])
  if (cell.kind === "range") {
    return rangeText(item[cell.pair.min.name], item[cell.pair.max.name], (v) =>
      valueText(cell.pair.min, v)
    )
  }
  return valueText(cell.field, item[cell.field.name])
}

/** One stored value in words: a duration as "1m 30s", a quantity with its
 * unit, an enum by its label. */
function valueText(spec: PropSpec, value: unknown): string {
  if (spec.kind === "duration" && typeof value === "string") {
    return coverageWords(value)
  }
  if (spec.kind === "quantity") return formatQuantityText(value)
  if (spec.values?.length && typeof value === "string") {
    return enumLabel(spec, value)
  }
  if (
    spec.kind === "reference" &&
    isBag(value) &&
    typeof value.ref === "string"
  ) {
    return value.ref.split("/").pop() ?? value.ref
  }
  if (typeof value === "boolean") return value ? "Yes" : "No"
  return scalarText(value)
}

/** A value as a screen reader says it, in a control's name. */
function spokenValue(spec: PropSpec, value: unknown): string {
  if (!held(value)) return "not set"
  if (Array.isArray(value)) {
    const item = elementSpec(spec)
    return value.map((v) => valueText(item, v) || "set").join(", ")
  }
  return valueText(spec, value) || "set"
}

/** Whether the reader may write this field here. */
function writableField(spec: PropSpec): boolean {
  return !spec.managed && personWritable(spec) && spec.kind !== "secret"
}

/** One value: read, and on a click (or Enter) edited where it sits. */
function Leaf({
  spec,
  value,
  path,
}: {
  spec: PropSpec
  value: unknown
  path: Path
}) {
  const { property, root, edit } = useStructure()
  const key = pathKey(property, path)
  const read = held(value) ? <LeafValue spec={spec} value={value} /> : null
  if (!edit || !writableField(spec) || isStructured(spec)) {
    return read ?? <Empty />
  }
  if (edit.open === key) {
    return <LeafEditor spec={spec} value={value} path={path} />
  }
  const anchor = (el: HTMLElement | null) => {
    if (el) edit.anchors.set(key, el)
    else edit.anchors.delete(key)
  }
  // One flag toggles on a click; a list of them is a list, edited as one.
  if ((spec.kind === "bool" || spec.kind === "boolean") && !spec.repeated) {
    return (
      <button
        ref={anchor}
        type="button"
        role="switch"
        aria-checked={value === true}
        aria-label={spec.label}
        disabled={edit.pending}
        onClick={() =>
          void edit.write(setIn(root, path, value !== true), holderOf(path))
        }
        className="-mx-1 rounded px-1 text-left hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      >
        {held(value) ? value === true ? "Yes" : "No" : <Empty />}
      </button>
    )
  }
  // A reference reads as a link to its record, so the click that edits it is
  // its own button.
  if (spec.kind === "reference" && !spec.repeated && read) {
    return (
      <span className="inline-flex min-w-0 flex-wrap items-center gap-1">
        {read}
        <Button
          ref={anchor}
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label={`Change ${spec.label}`}
          title={`Change ${spec.label}`}
          className="text-faint opacity-0 group-hover/item:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100"
          onClick={() => edit.start(path)}
        >
          <PencilIcon />
        </Button>
      </span>
    )
  }
  return (
    <span
      ref={anchor}
      role="button"
      tabIndex={0}
      // The name is the label, the value and what a press does: a reader
      // moving through a table hears what each cell holds.
      aria-label={`${spec.label}: ${spokenValue(spec, value)}, edit`}
      onClick={() => edit.start(path)}
      onKeyDown={(e) => {
        if (
          (e.key === "Enter" || e.key === " ") &&
          e.target === e.currentTarget
        ) {
          e.preventDefault()
          edit.start(path)
        }
      }}
      className="-mx-1 inline-flex min-w-0 cursor-text items-center rounded px-1 hover:bg-hover focus-visible:bg-hover focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      {read ?? <Empty />}
    </span>
  )
}

/** One value's editor: the sheet's own, writing the whole property with the
 * one value changed, from the property as it stood when the editor opened. */
function LeafEditor({
  spec,
  value,
  path,
}: {
  spec: PropSpec
  value: unknown
  path: Path
}) {
  const { property, root, edit } = useStructure()
  const [base] = useState(root)
  const field = useMemo(() => fieldOf(edit!.units, spec), [edit, spec])
  if (!edit) return null
  const key = pathKey(property, path)
  const lift: Lift = (next) => {
    if (next === null || next === undefined) {
      if (spec.required) {
        return { error: "every item holds one, so it can’t be emptied." }
      }
      return { properties: { [property]: unsetIn(base, path) } }
    }
    return { properties: { [property]: setIn(base, path, next) } }
  }
  return (
    <span className="block min-w-[8rem]">
      <InlineEditor
        row={{ name: spec.name, spec, field, value, filled: held(value) }}
        record={edit.record}
        kinds={edit.kinds}
        lift={lift}
        onDone={() => edit.stop(key)}
        // Said under the item, beside its other values: the message names
        // the one it is about.
        onError={(message) =>
          edit.setError(
            pathKey(property, holderOf(path)),
            message && `${spec.label}: ${message}`
          )
        }
      />
    </span>
  )
}

/** A range's two ends read as one value ("12–15"), each still its own
 * field: a click edits both, side by side. */
function RangeValue({
  pair,
  item,
  path,
}: {
  pair: RangePair
  item: Record<string, unknown>
  path: Path
}) {
  const { property, edit } = useStructure()
  const cellPath = [...path, `${pair.stem}~range`]
  const key = pathKey(property, cellPath)
  const lo = item[pair.min.name]
  const hi = item[pair.max.name]
  const text = rangeText(lo, hi, (v) => valueText(pair.min, v))
  const title = [
    held(lo) ? `${pair.min.label}: ${valueText(pair.min, lo)}` : "",
    held(hi) ? `${pair.max.label}: ${valueText(pair.max, hi)}` : "",
  ]
    .filter(Boolean)
    .join(", ")
  const read = text ? (
    <span className="tabular-nums" title={title}>
      {text}
    </span>
  ) : null
  const writable = writableField(pair.min) && writableField(pair.max)
  if (!edit || !writable) return read ?? <Empty />
  if (edit.open === key) {
    return <RangeEditor pair={pair} item={item} path={path} cell={cellPath} />
  }
  return (
    <span
      ref={(el) => {
        if (el) edit.anchors.set(key, el)
        else edit.anchors.delete(key)
      }}
      role="button"
      tabIndex={0}
      aria-label={`${pair.label}: ${text || "not set"}, edit`}
      onClick={() => edit.start(cellPath)}
      onKeyDown={(e) => {
        if (
          (e.key === "Enter" || e.key === " ") &&
          e.target === e.currentTarget
        ) {
          e.preventDefault()
          edit.start(cellPath)
        }
      }}
      className="-mx-1 inline-flex cursor-text items-center rounded px-1 hover:bg-hover focus-visible:bg-hover focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      {read ?? <Empty />}
    </span>
  )
}

const RANGE_BOX =
  "h-7 w-20 min-w-0 rounded-md border border-input bg-background px-1.5 text-sm tabular-nums outline-none focus-visible:border-primary focus-visible:ring-3 focus-visible:ring-primary-soft"

/** Both ends of a range, side by side. Enter or leaving the pair writes the
 * ends that changed, in one write; Esc writes nothing. An end emptied is
 * taken out of the item, never written as zero. */
function RangeEditor({
  pair,
  item,
  path,
  cell,
}: {
  pair: RangePair
  item: Record<string, unknown>
  path: Path
  cell: Path
}) {
  const { property, root, edit } = useStructure()
  const [base] = useState(root)
  const patch = useRecordPatch(edit!.record, useEditBase(edit!.record))
  const fields = useMemo(
    () => [fieldOf(edit!.units, pair.min), fieldOf(edit!.units, pair.max)],
    [edit, pair]
  )
  const [initial] = useState(() =>
    fields.map((f) => {
      const seeded = seedField(f, item[f.name], false)
      return typeof seeded === "string" ? seeded : ""
    })
  )
  const [texts, setTexts] = useState(initial)
  const done = useRef(false)
  const box = useRef<HTMLDivElement>(null)
  if (!edit) return null
  const key = pathKey(property, cell)
  const errorKey = pathKey(property, path)

  function close() {
    done.current = true
    edit!.setError(errorKey, undefined)
    edit!.stop(key)
  }
  async function commit() {
    if (done.current) return
    let next = base
    let changed = false
    for (const [i, field] of fields.entries()) {
      if (texts[i] === initial[i]) continue
      changed = true
      const submitted = toFieldValue(field, texts[i])
      if (submitted.error) {
        edit!.setError(errorKey, `${field.label}: ${submitted.error}`)
        return
      }
      const at = [...path, field.name]
      if (submitted.value === undefined || submitted.value === null) {
        if (field.required) {
          edit!.setError(errorKey, `${field.label} can’t be empty.`)
          return
        }
        next = unsetIn(next, at)
      } else {
        next = setIn(next, at, submitted.value)
      }
    }
    if (!changed) return close()
    done.current = true
    try {
      await patch.mutateAsync({ [property]: next })
      close()
    } catch (error) {
      done.current = false
      edit!.setError(errorKey, writeError(error))
    }
  }
  return (
    <div
      ref={box}
      className="inline-flex items-center gap-1"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault()
          close()
        } else if (e.key === "Enter") {
          e.preventDefault()
          void commit()
        }
      }}
      onBlur={(e) => {
        if (box.current?.contains(e.relatedTarget)) return
        void commit()
      }}
    >
      {fields.map((field, i) => (
        <span key={field.name} className="contents">
          {i === 1 && (
            <span aria-hidden className="text-faint">
              –
            </span>
          )}
          <input
            autoFocus={i === 0}
            aria-label={field.label}
            placeholder={i === 0 ? "min" : "max"}
            inputMode={field.control === "number" ? "decimal" : undefined}
            disabled={patch.isPending}
            value={texts[i]}
            onChange={(e) =>
              setTexts((prev) =>
                prev.map((t, at) => (at === i ? e.target.value : t))
              )
            }
            className={RANGE_BOX}
          />
        </span>
      ))}
      {patch.isPending && <Spinner className="size-3.5 shrink-0" />}
    </div>
  )
}

// ── a new item ──────────────────────────────────────────────────────────────

/** A new item, an item's copy, or an object that holds nothing yet, asked
 * for as a small form in the place it will take. Nothing is written until
 * it holds every field it must, and then it is written whole, worked out
 * from the property as it stood when the form opened and asserting that
 * version: a list another writer reordered meanwhile is refused as a
 * conflict, never written into the wrong item. `spec` is the list, or the
 * object itself. */
function DraftItem({ spec, draft }: { spec: PropSpec; draft: Draft }) {
  const { property, root, edit } = useStructure()
  const [base] = useState(root)
  const patch = useRecordPatch(edit!.record, useEditBase(edit!.record))
  const item = elementSpec(spec)
  const byName = useMemo(
    () => new Map((item.fields ?? []).map((f) => [f.name, f])),
    [item]
  )
  const [shown, setShown] = useState(draft.fields)
  const [values, setValues] = useState<FieldBag>(() => {
    const out: FieldBag = {}
    for (const name of draft.fields) {
      const spec = byName.get(name)
      if (!spec || !edit) continue
      out[name] = seedField(
        fieldOf(edit.units, spec),
        draft.base[name],
        !(name in draft.base)
      )
    }
    return out
  })
  const [error, setError] = useState<string>()
  const first = useRef<HTMLDivElement>(null)
  useEffect(() => {
    first.current?.scrollIntoView({ block: "nearest" })
    first.current
      ?.querySelector<HTMLElement>("input, textarea, button, [tabindex]")
      ?.focus()
  }, [])
  if (!edit) return null
  const inList = draft.index !== undefined
  const fields = shown
    .map((name) => byName.get(name))
    .filter((f): f is PropSpec => Boolean(f))
  const more = fieldsToAdd(orderedFields(item, draft.identity), {
    ...draft.base,
    ...Object.fromEntries(shown.map((n) => [n, true])),
  })
  const carried = orderedFields(item, draft.identity).filter(
    (f) => isNestedField(f) && held(draft.base[f.name])
  )
  const idPrefix = `draft-${pathKey(property, draft.path)}`

  function add(name: string) {
    const spec = byName.get(name)
    if (!spec || !edit) return
    setShown((prev) => [...prev, name])
    setValues((prev) => ({
      ...prev,
      [name]: seedField(fieldOf(edit.units, spec), undefined, true),
    }))
  }

  function close() {
    edit!.focus(draft.from)
    edit!.setDraft(null)
  }

  async function submit() {
    if (!edit) return
    const next: Record<string, unknown> = { ...draft.base }
    for (const field of fields) {
      const submitted = toFieldValue(
        fieldOf(edit.units, field),
        values[field.name] ?? ""
      )
      if (submitted.error) {
        setError(`${field.label}: ${submitted.error}`)
        return
      }
      if (submitted.value === undefined || submitted.value === null) {
        delete next[field.name]
      } else {
        next[field.name] = submitted.value
      }
    }
    const missing = (item.fields ?? []).filter(
      (f) => f.required && !held(next[f.name])
    )
    if (missing.length) {
      setError(
        `Fill in ${missing.map((f) => f.label).join(", ")}: ${
          inList ? "every item holds" : "it must hold"
        } ${missing.length > 1 ? "them" : "one"}.`
      )
      return
    }
    setError(undefined)
    try {
      await patch.mutateAsync({
        [property]:
          draft.index !== undefined
            ? insertAt(base, draft.path, draft.index, next)
            : setIn(base, draft.path, next),
      })
    } catch (e) {
      setError(writeError(e))
      return
    }
    // Focus goes to what was written: the new item's menu, or back to the
    // control that opened the form.
    edit.focus(
      draft.index !== undefined
        ? `${pathKey(property, [...draft.path, draft.index])}#menu`
        : draft.from
    )
    edit.setDraft(null)
  }

  return (
    <div
      data-slot="structured-draft"
      className="flex min-w-0 flex-col gap-3 rounded-lg border border-primary bg-background p-3 ring-3 ring-primary-soft"
      onKeyDown={(e) => {
        if (e.key === "Escape" && !e.nativeEvent.defaultPrevented) {
          e.preventDefault()
          close()
        } else if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
          e.preventDefault()
          void submit()
        }
      }}
    >
      <p className="text-xs font-medium text-muted-foreground">
        {draft.copyOf
          ? `Copy of item ${draft.copyOf} in ${spec.label}`
          : inList
            ? `New item in ${spec.label}`
            : spec.label}
      </p>
      <div
        ref={first}
        className="grid grid-cols-[repeat(auto-fill,minmax(11rem,1fr))] gap-x-4 gap-y-3"
      >
        {fields.map((field) => (
          <div
            key={field.name}
            className={cn(
              "flex min-w-0 flex-col gap-1",
              (isProseField(field) || isNestedField(field)) && "col-span-full"
            )}
          >
            <label
              htmlFor={`${idPrefix}-${field.name}`}
              className="text-xs text-muted-foreground"
              title={field.description}
            >
              {field.label}
              {field.required && (
                <span className="text-destructive" aria-hidden>
                  {" "}
                  *
                </span>
              )}
            </label>
            <PropertyField
              field={fieldOf(edit.units, field)}
              value={values[field.name] ?? ""}
              onChange={(next: FormValue) =>
                setValues((prev) => ({ ...prev, [field.name]: next }))
              }
              mode="create"
              kinds={edit.kinds}
              self={edit.record.id}
              idPrefix={idPrefix}
              bare
            />
          </div>
        ))}
      </div>
      {carried.length > 0 && (
        <p className="text-xs text-faint">
          Also copies{" "}
          {carried
            .map((f) => {
              const n = count(draft.base[f.name])
              return n === undefined ? f.label : `${f.label} (${n})`
            })
            .join(", ")}
          .
        </p>
      )}
      {error && <ErrorLine message={error} />}
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          size="sm"
          disabled={patch.isPending}
          onClick={() => void submit()}
        >
          {patch.isPending && <Spinner className="size-3.5" />}
          {draft.copyOf ? "Add copy" : inList ? "Add" : "Save"}
        </Button>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          disabled={patch.isPending}
          onClick={close}
        >
          Cancel
        </Button>
        {more.length > 0 && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  className="ml-auto text-muted-foreground"
                />
              }
            >
              <PlusIcon aria-hidden />
              Set a field
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="max-h-72 w-56">
              {more.map((field) => (
                <DropdownMenuItem
                  key={field.name}
                  onClick={() => add(field.name)}
                >
                  {field.label}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
    </div>
  )
}

// ── one object's own controls ───────────────────────────────────────────────

/** A single object's control for the fields it does not hold, in its
 * heading: "Set a field" offers each and opens its editor where the field
 * will read; an object that holds nothing and must hold a field asks for
 * those fields together ("Fill in"), since one alone would be refused. */
function ObjectFields({
  spec,
  value,
  path,
}: {
  spec: PropSpec
  value: unknown
  path: Path
}) {
  const { property, edit } = useStructure()
  if (!edit || !personWritable(spec) || spec.managed) return null
  const anchor = `${pathKey(property, path)}#fields`
  const register = (el: HTMLButtonElement | null) => {
    if (el) edit.anchors.set(anchor, el)
    else edit.anchors.delete(anchor)
  }
  const busy = edit.pending || Boolean(edit.draft)
  if (needsDraft(spec, value)) {
    return (
      <Button
        ref={register}
        type="button"
        variant="ghost"
        size="xs"
        className="text-muted-foreground"
        aria-label={`Fill in ${spec.label}`}
        disabled={busy}
        onClick={() =>
          edit.setDraft({
            path,
            base: {},
            fields: draftFields(spec),
            from: anchor,
          })
        }
      >
        <PlusIcon aria-hidden />
        Fill in
      </Button>
    )
  }
  const fields = orderedFields(spec)
  const addable = fieldsToAdd(fields, value)
  if (!addable.length) return null
  const pairs = rangePairs(fields)
  const bag = fieldsOf(value)
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            ref={register}
            type="button"
            variant="ghost"
            size="xs"
            className="text-muted-foreground"
            aria-label={`Set a field of ${spec.label}`}
            disabled={busy}
          />
        }
      >
        <PlusIcon aria-hidden />
        Set a field
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-h-72 w-56">
        {addable.map((field) => (
          <DropdownMenuItem
            key={field.name}
            onClick={() => {
              const shown = new Set(
                fields
                  .filter((f) => f.required || held(bag[f.name]))
                  .map((f) => f.name)
              )
              shown.add(field.name)
              const cell = cellsFor(
                fields.filter((f) => shown.has(f.name)),
                pairs
              ).find((c) => cellFields(c).some((f) => f.name === field.name))
              edit.addField(path, field.name, [
                ...path,
                cell ? cellName(cell) : field.name,
              ])
            }}
          >
            {field.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** A keyed map one object holds: read as each key beside its value, and
 * edited from its heading with the map's own control, written as the whole
 * property. */
function NestedMap({
  field,
  value,
  path,
  heading,
}: {
  field: PropSpec
  value: unknown
  path: Path
  heading: ReactNode
}) {
  const { property, edit } = useStructure()
  const key = pathKey(property, path)
  const editing = edit?.open === key
  const writable = Boolean(edit) && writableField(field)
  return (
    <section className="flex min-w-0 flex-col gap-1.5">
      <Toolbar heading={heading} count={count(value)} nested>
        {writable && !editing && (
          <Button
            ref={(el: HTMLButtonElement | null) => {
              if (!edit) return
              if (el) edit.anchors.set(key, el)
              else edit.anchors.delete(key)
            }}
            type="button"
            variant="ghost"
            size="xs"
            className="text-muted-foreground"
            aria-label={`Edit ${field.label}`}
            onClick={() => edit?.start(path)}
          >
            <PencilIcon aria-hidden />
            Edit
          </Button>
        )}
      </Toolbar>
      {editing ? (
        <LeafEditor spec={field} value={value} path={path} />
      ) : (
        <KeyedMap spec={field} value={value} path={path} />
      )}
    </section>
  )
}

// ── a keyed map, read ───────────────────────────────────────────────────────

/** A keyed map: each key beside its value, or, where the values are
 * objects, each key heading its object's fields. Read here; a map is edited
 * from its own row. */
function KeyedMap({
  spec,
  value,
  path,
}: {
  spec: PropSpec
  value: unknown
  path: Path
}) {
  const structure = useStructure()
  const entries = Object.entries(fieldsOf(value)).sort(([a], [b]) =>
    a.localeCompare(b)
  )
  if (!entries.length) return <Empty />
  const item = elementSpec(spec)
  const readOnly = { ...structure, edit: undefined }
  if (isStructured(item)) {
    return (
      <StructureContext.Provider value={readOnly}>
        <ol className="flex min-w-0 flex-col">
          {entries.map(([key, held]) => (
            <li key={key} className="min-w-0 border-b py-2 last:border-b-0">
              <p className="mb-1.5 font-mono text-[12.5px] break-all">{key}</p>
              <div className="pl-3">
                <ObjectBody spec={item} value={held} path={[...path, key]} />
              </div>
            </li>
          ))}
        </ol>
      </StructureContext.Provider>
    )
  }
  return (
    <StructureContext.Provider value={readOnly}>
      <dl className="grid min-w-0 grid-cols-[minmax(6rem,auto)_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-sm">
        {entries.map(([key, held]) => (
          <div key={key} className="contents">
            <dt className="font-mono text-[12.5px] break-all text-muted-foreground">
              {key}
            </dt>
            <dd className="min-w-0">
              <LeafValue spec={item} value={held} />
            </dd>
          </div>
        ))}
      </dl>
    </StructureContext.Provider>
  )
}

function ErrorLine({ message }: { message: string }) {
  return (
    <p
      role="alert"
      className="rounded-md bg-bad-soft px-2.5 py-1.5 text-[12.5px] text-destructive"
    >
      {message}
    </p>
  )
}
