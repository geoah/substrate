/** A record page's head: the kind's glyph, the title (edited in place where
 * the kind titles itself from a property the owner writes), one meta line,
 * and the page's actions. Everyday mode says what the record is and who
 * added and last changed it; technical mode says its full reference. A
 * provider's copy says where to change it. */

import { useRef, useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import {
  CodeIcon,
  CopyPlusIcon,
  LinkIcon,
  MoreHorizontalIcon,
  Trash2Icon,
  UserRoundIcon,
} from "lucide-react"

import { ago } from "@/components/property-sheet/dates"
import {
  useRecordPatch,
  writeError,
} from "@/components/property-sheet/use-record-patch"
import { useFocusReturn } from "@/components/property-sheet/focus-return"
import { CopyButton } from "@/components/identity/copy-button"
import { KindGlyph } from "@/components/identity/kind-glyph"
import { KindPath, KindRef } from "@/components/identity/kind-ref"
import { ProviderBadge } from "@/components/identity/provider-badge"
import { Button } from "@/components/ui/button"
import { ConfirmDialog } from "@/components/ui/confirm-dialog"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { toast } from "@/components/ui/toast"
import { useTechnicalDetails } from "@/hooks/use-console-preferences"
import { providerOfKind } from "@/lib/actor-identity"
import { splitKind } from "@/lib/api/http"
import { createRecord } from "@/lib/api/records"
import { deleteRecord } from "@/lib/api/sync"
import type { ChangeRow, KindInfo, SubstrateRecord } from "@/lib/api/types"
import { recordTitle } from "@/lib/format"
import { displayPlural, untitled } from "@/lib/kind-names"
import { everyValueYours } from "@/lib/provenance"
import { fieldOf } from "@/lib/record-form"
import { titleEditor } from "@/lib/record-schema"
import { cn } from "@/lib/utils"
import { duplicateProperties, headerFacts, recordLink } from "./record-model"

async function copyLink(record: SubstrateRecord) {
  try {
    await navigator.clipboard.writeText(recordLink(record))
    toast.add({ type: "success", title: "Link copied" })
  } catch {
    toast.add({
      type: "error",
      title: "The link couldn’t be copied",
      description: recordLink(record),
    })
  }
}

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
  // A heading the server derives from a template holding no one property is
  // not typed into: a written `title` there is ignored and the edit reverts.
  const spec = titleEditor(kind)
  const name = spec?.name ?? ""
  const editable = !readOnly && spec !== undefined
  const [editing, setEditing] = useState(false)
  const [text, setText] = useState("")
  const [error, setError] = useState<string>()
  const patch = useRecordPatch(record)
  const busy = useRef(false)
  const button = useRef<HTMLButtonElement>(null)
  useFocusReturn(editing, button)

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
  const shown = title || untitled(record.kind)
  return (
    <h1 className={cn(heading, "mt-2.5 mb-1.5", !title && "text-faint")}>
      {editable ? (
        <button
          ref={button}
          type="button"
          data-slot="record-title"
          onClick={() => {
            const stored = record.properties[name]
            setText(typeof stored === "string" ? stored : title)
            setEditing(true)
          }}
          className="-mx-1 w-[calc(100%+0.5rem)] cursor-text rounded-md px-1 text-left outline-none hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring"
        >
          {shown}
          <span className="sr-only">, edit</span>
        </button>
      ) : (
        shown
      )}
    </h1>
  )
}

export function RecordHeader({
  record,
  kind,
  rows,
  source,
  onSource,
  holders,
  onHolders,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  /** The record's change rows, newest first, as far as they are read. */
  rows: ChangeRow[]
  /** Whether the YAML source is showing (technical mode). */
  source: boolean
  onSource: (on: boolean) => void
  /** Whether the sheet names who holds every value. */
  holders: boolean
  onHolders: (on: boolean) => void
}) {
  const [technical] = useTechnicalDetails()
  const provider = providerOfKind(record.kind)
  const facts = headerFacts(record, rows)
  const path = `${record.kind}/${record.id}`
  const { authority, pkg, name } = splitKind(record.kind)
  const [deleting, setDeleting] = useState(false)
  const navigate = useNavigate()
  const client = useQueryClient()
  const duplicate = useMutation({
    mutationFn: () =>
      createRecord(authority, pkg, name, {
        properties: duplicateProperties(record, kind),
      }),
    onSuccess: async (copy) => {
      await client.invalidateQueries({ queryKey: ["records"] })
      toast.add({ type: "success", title: "Duplicated" })
      void navigate({
        to: "/data/$authority/$pkg/$name/$id",
        params: { authority, pkg, name, id: copy.id },
      })
    },
    onError: (e) =>
      toast.add({
        type: "error",
        title: "It couldn’t be duplicated",
        description: writeError(e),
      }),
  })

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
            <DropdownMenuContent align="end" className="min-w-52">
              <DropdownMenuItem onClick={() => void copyLink(record)}>
                <LinkIcon /> Copy link
              </DropdownMenuItem>
              {!provider && (
                <DropdownMenuItem
                  disabled={duplicate.isPending}
                  onClick={() => duplicate.mutate()}
                >
                  <CopyPlusIcon /> Duplicate
                </DropdownMenuItem>
              )}
              <DropdownMenuCheckboxItem
                checked={holders}
                onCheckedChange={(on) => onHolders(on)}
              >
                <UserRoundIcon /> Who holds each value
              </DropdownMenuCheckboxItem>
              {technical && (
                <DropdownMenuItem onClick={() => onSource(true)}>
                  <CodeIcon /> Open in YAML
                </DropdownMenuItem>
              )}
              {provider ? (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem disabled>
                    Change it in {provider.name}
                  </DropdownMenuItem>
                </>
              ) : (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    onClick={() => setDeleting(true)}
                  >
                    <Trash2Icon /> Delete
                  </DropdownMenuItem>
                </>
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
            {!provider && everyValueYours(record) && (
              <span data-slot="all-yours">Every value is yours</span>
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
    <ConfirmDialog
      title={`Delete “${title}”?`}
      consequence={`It’s removed from ${displayPlural(kind ?? record.kind)}, and anything that points to it will point to nothing. Records set to go with it are removed too. You can’t undo this here.`}
      confirm="Delete"
      destructive
      pending={remove.isPending}
      error={remove.isError ? writeError(remove.error) : undefined}
      onConfirm={() => remove.mutate()}
      onClose={onClose}
    />
  )
}
