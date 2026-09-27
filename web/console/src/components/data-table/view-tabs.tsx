/** A collection's saved views as a strip of tabs above its grid: **All** (the
 * collection as it opens), each saved view by name, and **Save view**. A tab
 * is chosen when the grid shows what it names (`lib/saved-views.ts`), so no
 * tab is chosen while the reader has changed something none of them names.
 * The view picked last stays marked once the reader changes something, and
 * its menu saves those changes to it or discards them; the chosen view's
 * menu renames or deletes it. Saving, renaming, replacing and deleting each
 * ask first through the one confirmation dialog, and saving is refused while
 * a saved view is chosen. */

import { useId, useState, type ReactNode } from "react"
import { MoreHorizontalIcon, PlusIcon } from "lucide-react"

import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { viewNameProblem, type SavedView } from "@/lib/saved-views"
import { cn } from "@/lib/utils"

type Dialog =
  | { type: "save" }
  | { type: "rename"; view: SavedView }
  | { type: "replace"; view: SavedView }
  | { type: "delete"; view: SavedView }

export function ViewTabs({
  views,
  active,
  edited,
  busy,
  onPick,
  onSave,
  onRename,
  onReplace,
  onDelete,
  className,
}: {
  /** This collection's views, in the order they were saved. */
  views: SavedView[]
  /** The chosen tab: "all", a view's id, or null when none matches. */
  active: string | null
  /** The view picked last, when the page has since moved away from it: its
   * tab stays marked, and its menu saves or discards the changes. */
  edited?: string | null
  /** The preferences are not loaded yet: saving waits. */
  busy?: boolean
  onPick: (view: SavedView | null) => void
  onSave: (name: string) => void
  onRename: (view: SavedView, name: string) => void
  onReplace: (view: SavedView) => void
  onDelete: (view: SavedView) => void
  className?: string
}) {
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const close = () => setDialog(null)
  // A second view of the same shape would only compete for the same tab.
  const shown = views.find((v) => v.id === active)

  return (
    <div
      role="group"
      aria-label="Views"
      className={cn(
        "flex shrink-0 items-end gap-1 overflow-x-auto border-b border-border",
        className
      )}
    >
      <Tab pressed={active === "all"} onClick={() => onPick(null)}>
        All
      </Tab>
      {views.map((view) => {
        const chosen = active === view.id
        const changed = !chosen && edited === view.id
        return (
          <div key={view.id} className="flex shrink-0 items-center">
            <Tab pressed={chosen || changed} onClick={() => onPick(view)}>
              {view.name}
              {changed && (
                <>
                  <span
                    aria-hidden
                    title="Changed since you picked it"
                    className="ml-1.5 inline-block size-1.5 rounded-full bg-primary align-middle"
                  />
                  <span className="sr-only">, changed</span>
                </>
              )}
            </Tab>
            {(chosen || changed) && (
              <DropdownMenu>
                <DropdownMenuTrigger
                  disabled={busy}
                  render={
                    <button
                      type="button"
                      aria-label={`More for the “${view.name}” view`}
                      className="hit-area mb-1 -ml-1 grid size-5 cursor-pointer place-items-center rounded-[4px] text-faint outline-none hover:bg-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
                    />
                  }
                >
                  <MoreHorizontalIcon aria-hidden className="size-3.5" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="min-w-52">
                  {changed && (
                    <>
                      <DropdownMenuItem
                        onClick={() => setDialog({ type: "replace", view })}
                      >
                        Save changes to this view…
                      </DropdownMenuItem>
                      <DropdownMenuItem onClick={() => onPick(view)}>
                        Discard changes
                      </DropdownMenuItem>
                      <DropdownMenuSeparator />
                    </>
                  )}
                  <DropdownMenuItem
                    onClick={() => setDialog({ type: "rename", view })}
                  >
                    Rename…
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    variant="destructive"
                    onClick={() => setDialog({ type: "delete", view })}
                  >
                    Delete…
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>
        )
      })}
      <button
        type="button"
        disabled={busy}
        className="mb-1 ml-1 inline-flex h-7 shrink-0 cursor-pointer items-center gap-1 rounded-md px-2 text-[13px] text-muted-foreground outline-none hover:bg-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-default disabled:opacity-50"
        onClick={() => setDialog({ type: "save" })}
      >
        <PlusIcon aria-hidden className="size-3.5" />
        Save view
      </button>

      {dialog?.type === "save" && (
        <NameDialog
          title="Save this view?"
          consequence="Its filters, sort, columns, nesting and grouping are kept under this name, in every browser signed in to this repository. A search is not kept."
          confirm="Save"
          views={views}
          refusal={
            shown
              ? `“${shown.name}” already shows this. Change a filter, the sort, the columns, nesting or grouping first, or rename “${shown.name}”.`
              : undefined
          }
          onConfirm={(name) => {
            onSave(name)
            close()
          }}
          onClose={close}
        />
      )}
      {dialog?.type === "rename" && (
        <NameDialog
          title={`Rename “${dialog.view.name}”?`}
          consequence="Only the name changes; the view keeps what it shows."
          confirm="Rename"
          initial={dialog.view.name}
          except={dialog.view.id}
          views={views}
          onConfirm={(name) => {
            onRename(dialog.view, name)
            close()
          }}
          onClose={close}
        />
      )}
      {dialog?.type === "replace" && (
        <ConfirmDialog
          title={`Save the changes to “${dialog.view.name}”?`}
          consequence="The view takes the filters, sort, columns, nesting and grouping on the page now, and forgets its own. No records change."
          confirm="Save changes"
          onConfirm={() => {
            onReplace(dialog.view)
            close()
          }}
          onClose={close}
        />
      )}
      {dialog?.type === "delete" && (
        <ConfirmDialog
          title={`Delete the “${dialog.view.name}” view?`}
          consequence="The view is gone from every browser. No records change, and All still shows the whole collection."
          confirm="Delete"
          destructive
          onConfirm={() => {
            onDelete(dialog.view)
            close()
          }}
          onClose={close}
        />
      )}
    </div>
  )
}

function Tab({
  pressed,
  onClick,
  children,
}: {
  pressed: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      className="-mb-px max-w-56 shrink-0 cursor-pointer truncate border-b-2 border-transparent px-2 pt-1 pb-2 text-[13px] text-muted-foreground outline-none hover:text-foreground focus-visible:rounded-sm focus-visible:ring-2 focus-visible:ring-ring/50 aria-pressed:border-foreground aria-pressed:font-medium aria-pressed:text-foreground"
      onClick={onClick}
    >
      {children}
    </button>
  )
}

function NameDialog({
  title,
  consequence,
  confirm,
  initial = "",
  except,
  views,
  refusal,
  onConfirm,
  onClose,
}: {
  title: string
  consequence: string
  confirm: string
  initial?: string
  /** The view being renamed, whose own name is no clash. */
  except?: string
  views: SavedView[]
  /** Why nothing may be saved under any name; shown before a name is typed. */
  refusal?: string
  onConfirm: (name: string) => void
  onClose: () => void
}) {
  const id = useId()
  const [name, setName] = useState(initial)
  const [touched, setTouched] = useState(false)
  const problem = refusal ?? viewNameProblem(name, views, except)
  const shownProblem = refusal ?? (touched ? problem : undefined)
  const submit = () => {
    setTouched(true)
    if (!problem) onConfirm(name.trim())
  }
  return (
    <ConfirmDialog
      title={title}
      consequence={consequence}
      confirm={confirm}
      disabled={Boolean(shownProblem)}
      onConfirm={submit}
      onClose={onClose}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <Field data-invalid={Boolean(shownProblem) || undefined}>
          <FieldLabel htmlFor={`${id}-name`}>Name</FieldLabel>
          <Input
            id={`${id}-name`}
            autoFocus
            autoComplete="off"
            placeholder="Open, Mine, This week…"
            value={name}
            aria-invalid={Boolean(shownProblem) || undefined}
            aria-describedby={shownProblem ? `${id}-problem` : undefined}
            onChange={(e) => {
              setName(e.target.value)
              setTouched(true)
            }}
          />
          {shownProblem && (
            <FieldDescription id={`${id}-problem`} className="text-destructive">
              {shownProblem}
            </FieldDescription>
          )}
        </Field>
      </form>
    </ConfirmDialog>
  )
}
