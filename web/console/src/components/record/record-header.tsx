/** A record page's head: the kind's glyph, the title (edited in place where
 * the kind titles itself from a property the owner writes), one meta line,
 * and the page's actions. Everyday mode says what the record is and who
 * added and last changed it; technical mode says its full reference. A
 * provider's copy says where to change it. */

import { useRef, useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import {
  CodeIcon,
  MoreHorizontalIcon,
  PencilIcon,
  Trash2Icon,
} from "lucide-react"

import { ago } from "@/components/property-sheet/dates"
import {
  useRecordPatch,
  writeError,
} from "@/components/property-sheet/use-record-patch"
import { CopyButton } from "@/components/identity/copy-button"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { KindPath, KindRef } from "@/components/identity/kind-ref"
import { ProviderBadge } from "@/components/identity/provider-badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Spinner } from "@/components/ui/spinner"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { providerOfKind } from "@/lib/actor-identity"
import { splitKind } from "@/lib/api/http"
import { deleteRecord } from "@/lib/api/sync"
import type { ChangeRow, KindInfo, SubstrateRecord } from "@/lib/api/types"
import { recordTitle } from "@/lib/format"
import { displayPlural, untitled } from "@/lib/kind-names"
import { fieldOf } from "@/lib/record-form"
import {
  ownerWritable,
  propSpecsByName,
  systemSpecs,
  titleProperty,
} from "@/lib/record-schema"
import { cn } from "@/lib/utils"
import { headerFacts } from "./record-model"

function Title({
  record,
  kind,
  readOnly,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  readOnly: boolean
}) {
  const title = recordTitle(record.properties)
  const name = titleProperty(kind)
  const spec = kind
    ? [...propSpecsByName(kind), ...systemSpecs(kind)].find(
        (s) => s.name === name
      )
    : undefined
  const editable =
    !readOnly && spec && !spec.managed && ownerWritable(spec) && !spec.repeated
  const [editing, setEditing] = useState(false)
  const [text, setText] = useState("")
  const [error, setError] = useState<string>()
  const patch = useRecordPatch(record)
  const busy = useRef(false)

  async function save() {
    if (busy.current || !spec) return
    busy.current = true
    const next = text.trim()
    const stored = record.properties[name]
    try {
      const held = typeof stored === "string" ? stored : ""
      // A title read through a fallback (`{displayName|name}` with no
      // displayName) that was not changed is not a value to write.
      const unchanged = next === held || (!held && next === title)
      if (!unchanged) {
        await patch.mutateAsync({ [name]: next || null })
      }
      setError(undefined)
      setEditing(false)
    } catch (e) {
      setError(writeError(e))
    } finally {
      busy.current = false
    }
  }

  const heading =
    "text-[32px] leading-[1.15] font-bold tracking-[-0.025em] text-balance break-words"
  if (editing && spec) {
    return (
      <>
        <input
          autoFocus
          aria-label={fieldOf(spec).label}
          value={text}
          disabled={patch.isPending}
          placeholder={untitled(record.kind)}
          onChange={(e) => setText(e.target.value)}
          onBlur={() => void save()}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault()
              void save()
            } else if (e.key === "Escape") {
              busy.current = true
              setEditing(false)
              setError(undefined)
              setTimeout(() => (busy.current = false))
            }
          }}
          className={cn(
            heading,
            "-mx-1 mt-2.5 mb-1.5 w-full rounded-md bg-transparent px-1 ring-1 ring-primary outline-none"
          )}
        />
        {error && (
          <p role="alert" className="mb-1 text-[12.5px] text-destructive">
            {error}
          </p>
        )}
      </>
    )
  }
  return (
    <h1
      className={cn(
        heading,
        "mt-2.5 mb-1.5",
        !title && "text-faint",
        editable && "-mx-1 cursor-text rounded-md px-1 hover:bg-hover"
      )}
      onClick={
        editable
          ? () => {
              const stored = record.properties[name]
              setText(typeof stored === "string" ? stored : title)
              setEditing(true)
            }
          : undefined
      }
    >
      {title || untitled(record.kind)}
    </h1>
  )
}

export function RecordHeader({
  record,
  kind,
  rows,
  source,
  onSource,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  /** The record's change rows, newest first, as far as they are read. */
  rows: ChangeRow[]
  /** Whether the YAML source is showing (technical mode). */
  source: boolean
  onSource: (on: boolean) => void
}) {
  const [technical] = useTechnicalDetails()
  const provider = providerOfKind(record.kind)
  const facts = headerFacts(record, rows)
  const path = `${record.kind}/${record.id}`
  const { authority, pkg, name } = splitKind(record.kind)
  const [deleting, setDeleting] = useState(false)

  return (
    <header data-slot="record-header">
      <div className="flex items-start justify-between gap-3">
        <KindGlyph kind={kind ?? record.kind} size="lg" />
        <div className="flex items-center gap-1">
          {technical && (
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="View source"
              title="View source (YAML)"
              aria-pressed={source}
              className={cn(source && "bg-selection text-primary-text")}
              onClick={() => onSource(!source)}
            >
              <CodeIcon />
            </Button>
          )}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="More"
                  title="More"
                />
              }
            >
              <MoreHorizontalIcon />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="min-w-44">
              {technical && (
                <DropdownMenuItem
                  render={
                    <Link
                      to="/data/$authority/$pkg/$name/$id/edit"
                      params={{ authority, pkg, name, id: record.id }}
                    />
                  }
                >
                  <PencilIcon /> Edit YAML
                </DropdownMenuItem>
              )}
              {technical && !provider && <DropdownMenuSeparator />}
              {!provider && (
                <DropdownMenuItem
                  variant="destructive"
                  onClick={() => setDeleting(true)}
                >
                  <Trash2Icon /> Delete
                </DropdownMenuItem>
              )}
              {provider && !technical && (
                <DropdownMenuItem disabled>
                  Change it in {provider.name}
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
      <Title record={record} kind={kind} readOnly={Boolean(provider)} />
      <div className="flex flex-wrap items-center gap-x-3.5 gap-y-1.5 text-[12.5px] text-faint">
        {technical ? (
          <span className="inline-flex min-w-0 flex-wrap items-center gap-1">
            <KindPath reference={record.kind} className="text-[12px]" />
            <span className="font-mono text-[12px] text-muted-foreground">
              / {record.id}
            </span>
            <CopyButton value={path} label="Copy the record’s reference" />
          </span>
        ) : (
          <>
            <KindRef kind={kind ?? record.kind} />
            <span title={record.createdAt}>
              {provider ? "Brought in" : "Added"}
              {facts.addedBy ? ` by ${facts.addedBy}` : ""}{" "}
              {ago(record.createdAt)}
            </span>
            {facts.changed && (
              <span title={record.updatedAt}>
                Changed {ago(record.updatedAt)}
                {facts.changedBy ? ` by ${facts.changedBy}` : ""}
              </span>
            )}
          </>
        )}
      </div>
      {provider && (
        <div className="mt-4 flex items-center gap-2.5 rounded-lg border bg-panel px-3 py-2.5 text-[13px] text-muted-foreground">
          <ProviderBadge provider={provider} />
          <span>
            A copy of what {provider.name} has. Change it in {provider.name} and
            it updates here.
          </span>
        </div>
      )}
      {deleting && (
        <DeleteDialog
          record={record}
          kind={kind}
          onClose={() => setDeleting(false)}
        />
      )}
    </header>
  )
}

function DeleteDialog({
  record,
  kind,
  onClose,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  onClose: () => void
}) {
  const navigate = useNavigate()
  const client = useQueryClient()
  const { authority, pkg, name } = splitKind(record.kind)
  const title = recordTitle(record.properties) || untitled(record.kind)
  const remove = useMutation({
    mutationFn: () => deleteRecord(record),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ["records"] })
      void navigate({
        to: "/data/$authority/$pkg/$name",
        params: { authority, pkg, name },
      })
    },
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => !open && !remove.isPending && onClose()}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Delete “{title}”?</DialogTitle>
          <DialogDescription>
            It’s removed from {displayPlural(kind ?? record.kind)}, and anything
            that points to it will point to nothing. Records set to go with it
            are removed too. You can’t undo this here.
          </DialogDescription>
        </DialogHeader>
        {remove.isError && (
          <p role="alert" className="text-sm text-destructive">
            {writeError(remove.error)}
          </p>
        )}
        <DialogFooter>
          <Button
            variant="outline"
            disabled={remove.isPending}
            onClick={onClose}
          >
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={remove.isPending}
            onClick={() => remove.mutate()}
          >
            {remove.isPending && <Spinner className="size-3.5" />}
            Delete
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
