/** A record's ink on its page: each `blobref` that holds a note gets a
 * section that shows its pages, and, for the owner, edits them, imports a
 * `.inkd` file over them and saves them back. A save uploads a new file and
 * points the property at it. */

import { useMemo, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  DownloadIcon,
  MoreHorizontalIcon,
  PenLineIcon,
  RefreshCwIcon,
  UploadIcon,
} from "lucide-react"

import { SectionHead } from "@/components/identity/section-head"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import {
  blobQueryOptions,
  putBlob,
  readBlobInfo,
  saveBytes,
} from "@/lib/api/blobs"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  backgroundType,
  INK_MEDIA_TYPE,
  inkSummary,
  inkText,
  newInkDocument,
  readInk,
  type InkDocument,
} from "@/lib/ink"
import {
  byteSize,
  inkCounts,
  inkFileName,
  inkFromBytes,
  inkPlan,
} from "@/lib/ink-record"
import { recordTitle } from "@/lib/format"
import { readInkSetup, writeInkSetup } from "@/lib/ink-setup"
import { propSpecs, type PropSpec } from "@/lib/record-schema"

import { InkEditor } from "./ink-editor"
import { InkPageView } from "./ink-page"
import { PdfMarkupSection } from "./pdf-markup"
import {
  importFailure,
  inkSaveFailure,
  type InkSaveFailure,
} from "./save-failure"
import { inkBaseOf, useInkWrite, type InkBase } from "./use-ink-write"
import { SaveFailureNote } from "./save-failure-note"

export function InkSections({
  record,
  kind,
  readOnly,
}: {
  record: SubstrateRecord
  kind?: KindInfo
  readOnly: boolean
}) {
  if (!kind) return null
  const plan = inkPlan(propSpecs(kind), record.properties)
  // Keyed by the record as well as the property: the page reuses these
  // components when it moves to another record, and an editor left open on
  // one note would otherwise save its drawing over the next record's.
  const at = `${record.kind}/${record.id}`
  return (
    <>
      {plan.markup && (
        <PdfMarkupSection
          key={at}
          record={record}
          document={plan.markup.document}
          ink={plan.markup.ink}
          readOnly={readOnly}
        />
      )}
      {plan.ink.map((spec) => (
        <InkPropertySection
          key={`${at}/${spec.name}`}
          record={record}
          spec={spec}
          readOnly={readOnly}
        />
      ))}
    </>
  )
}

/** The note a property's blob holds. */
function useInkValue(record: SubstrateRecord, spec: PropSpec) {
  const blob = readBlobInfo(record.properties[spec.name])
  const bytes = useQuery(blobQueryOptions(blob?.digest))
  // Parsed once per file: a parse makes new stroke objects, and the outline
  // cache and the page's memo are keyed on them.
  const read = useMemo(
    () => (bytes.data ? inkFromBytes(bytes.data) : undefined),
    [bytes.data]
  )
  if (!blob) return { state: "empty" as const }
  // A 0-byte file holds no ink to lose: it reads as a note not drawn yet.
  if (blob.size === 0) return { state: "empty" as const, blob }
  if (bytes.isPending) return { state: "loading" as const, blob }
  if (bytes.isError)
    return {
      state: "error" as const,
      message: `The note didn’t load: ${bytes.error.message}`,
      blob,
      retry: () => void bytes.refetch(),
    }
  if (!read) return { state: "loading" as const, blob }
  return read.ok
    ? {
        state: "ready" as const,
        document: read.document,
        blob,
        bytes: bytes.data,
      }
    : { state: "error" as const, message: read.message, blob }
}

export function InkPropertySection({
  record,
  spec,
  readOnly,
}: {
  record: SubstrateRecord
  spec: PropSpec
  readOnly: boolean
}) {
  const ink = useInkValue(record, spec)
  // The version the drawing began from, held while the editor is open: the
  // page re-reads a record that moves under the reader, and a save asserting
  // the newer version would silently replace the other save.
  const [editing, setEditing] = useState<
    { document: InkDocument; base: InkBase } | undefined
  >()
  const writeInk = useInkWrite(record, spec.name)
  const [uploading, setUploading] = useState(false)
  const [failure, setFailure] = useState<InkSaveFailure | undefined>()
  const fileName = inkFileName(recordTitle(record.properties), spec.name)

  /** Write `document` (or the file's own bytes, kept exact) into the
   * property, from `base`: where the drawing began, or the page as it is
   * for an import. */
  const save = async (
    document: InkDocument,
    base: InkBase,
    bytes?: Blob
  ): Promise<boolean> => {
    setFailure(undefined)
    setUploading(true)
    try {
      const info = await putBlob(bytes ?? inkText(document), {
        name: fileName,
        mediaType: INK_MEDIA_TYPE,
      })
      await writeInk(info.digest, base)
      if (!bytes)
        writeInkSetup({
          size: { width: document.page.width, height: document.page.height },
          background: backgroundType(document.pages[0]?.background),
        })
      setEditing(undefined)
      return true
    } catch (e) {
      setFailure(bytes ? importFailure(e) : inkSaveFailure(e, document))
      return false
    } finally {
      setUploading(false)
    }
  }

  const importFile = async (file: File) => {
    const read = readInk(await file.text())
    if (!read.ok) {
      setFailure({ message: read.message })
      return
    }
    await save(read.document, inkBaseOf(record, spec.name), file)
  }

  const summary = ink.state === "ready" ? inkSummary(ink.document) : undefined
  const where = ink.blob
    ? `${ink.blob.name ?? "a file"}, ${byteSize(ink.blob.size)}`
    : "no file yet"
  const hint = summary ? `${inkCounts(summary)} · ${where}` : where

  return (
    <section data-slot="ink-section" aria-label={spec.label}>
      <SectionHead
        title={spec.label}
        hint={hint}
        actions={
          editing ? undefined : (
            <>
              {/* Drawing starts from the stored note, or from blank paper
                  where the record names none: never from blank paper over
                  a note that failed to load, which a save would replace. */}
              {!readOnly &&
                (ink.state === "ready" || ink.state === "empty") && (
                  <Button
                    type="button"
                    size="sm"
                    onClick={() =>
                      setEditing({
                        document:
                          ink.state === "ready" ? ink.document : blankNote(),
                        base: inkBaseOf(record, spec.name),
                      })
                    }
                  >
                    <PenLineIcon data-icon="inline-start" />
                    {ink.state === "ready" ? "Draw" : "Start drawing"}
                  </Button>
                )}
              {(ink.state === "ready" || !readOnly) && (
                <FileMenu
                  onDownload={
                    ink.state === "ready"
                      ? () => saveBytes(ink.bytes, fileName, INK_MEDIA_TYPE)
                      : undefined
                  }
                  onImport={readOnly ? undefined : importFile}
                  disabled={uploading}
                />
              )}
            </>
          )
        }
      />
      {editing ? (
        <InkEditor
          initial={editing.document}
          heading={
            <span className="block truncate text-sm font-medium">
              {recordTitle(record.properties) || spec.label}
            </span>
          }
          saving={uploading}
          onSave={(document) => void save(document, editing.base)}
          onCancel={() => {
            setFailure(undefined)
            setEditing(undefined)
          }}
          discard={{
            title: "Discard your changes?",
            consequence:
              "What you drew since opening it goes. The saved ink stays as it was.",
          }}
          note={
            failure ? (
              <SaveFailureNote failure={failure} fileName={fileName} />
            ) : (
              "Saving stores a new .inkd file and points this note at it."
            )
          }
        />
      ) : (
        <>
          <InkPages ink={ink} />
          {failure && (
            <p className="mt-2 text-xs text-destructive">{failure.message}</p>
          )}
        </>
      )}
    </section>
  )
}

/** The note's file, in a menu beside Draw: import an Inkidian `.inkd` over
 * it, or download it. */
function FileMenu({
  onDownload,
  onImport,
  disabled,
}: {
  onDownload?: () => void
  onImport?: (file: File) => void
  disabled?: boolean
}) {
  const input = useRef<HTMLInputElement>(null)
  return (
    <>
      {onImport && (
        <input
          ref={input}
          type="file"
          accept={`${INK_MEDIA_TYPE},.inkd`}
          className="hidden"
          aria-label="Import .inkd file"
          onChange={(e) => {
            const file = e.target.files?.[0]
            e.target.value = ""
            if (file) onImport(file)
          }}
        />
      )}
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={disabled}
              aria-label="The note's file"
            />
          }
        >
          <MoreHorizontalIcon data-icon="inline-start" />
          File
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-48">
          {onImport && (
            <DropdownMenuItem onClick={() => input.current?.click()}>
              <UploadIcon />
              Import .inkd…
            </DropdownMenuItem>
          )}
          {onDownload && (
            <DropdownMenuItem onClick={onDownload}>
              <DownloadIcon />
              Download .inkd
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </>
  )
}

/** A note not drawn yet starts on the paper this device used last. */
function blankNote(): InkDocument {
  const setup = readInkSetup()
  return newInkDocument({ size: setup.size, background: setup.background })
}

/** The paper an empty note shows: one lined A4 page, drawn as paper. */
const EMPTY_PAPER = newInkDocument({ background: "lined" })

function InkPages({ ink }: { ink: ReturnType<typeof useInkValue> }) {
  if (ink.state === "loading")
    return <Skeleton className="aspect-[1240/1754] w-full max-w-3xl" />
  if (ink.state === "empty")
    return (
      <div className="flex max-w-sm flex-col gap-2" data-slot="ink-empty">
        <p className="text-sm text-muted-foreground">No handwriting yet.</p>
        <div className="overflow-hidden rounded-md border opacity-70 shadow-sm">
          <InkPageView
            document={EMPTY_PAPER}
            pageIndex={0}
            label="Blank page"
          />
        </div>
      </div>
    )
  if (ink.state === "error")
    return (
      <div className="flex flex-wrap items-center gap-2">
        <p className="text-sm text-destructive">{ink.message}</p>
        {"retry" in ink && (
          <Button type="button" variant="outline" size="sm" onClick={ink.retry}>
            <RefreshCwIcon data-icon="inline-start" />
            Retry
          </Button>
        )}
      </div>
    )
  const { document } = ink
  return (
    <ol className="grid max-w-3xl gap-4" data-slot="ink-pages">
      {document.pages.map((page, i) => (
        <li key={page.id ?? i} className="flex flex-col gap-1">
          <div className="overflow-hidden rounded-md border shadow-sm">
            <InkPageView
              document={document}
              pageIndex={i}
              label={`Page ${i + 1} of ${document.pages.length}`}
            />
          </div>
          {document.pages.length > 1 && (
            <span className="text-center text-xs text-muted-foreground tabular-nums">
              Page {i + 1} of {document.pages.length}
            </span>
          )}
        </li>
      ))}
    </ol>
  )
}
