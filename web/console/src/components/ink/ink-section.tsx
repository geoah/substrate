/** A record's ink on its page: each `blobref` that holds a note gets a
 * section that shows its pages, and, for the owner, edits them, imports a
 * `.inkd` file over them and saves them back. A save uploads a new file and
 * points the property at it. */

import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  DownloadIcon,
  PenLineIcon,
  RefreshCwIcon,
  UploadIcon,
} from "lucide-react"

import { SectionHead } from "@/components/identity/section-head"
import { useRecordPatch } from "@/components/property-sheet/use-record-patch"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  blobQueryOptions,
  putBlob,
  readBlobInfo,
  saveBytes,
} from "@/lib/api/blobs"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
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
import { propSpecs, type PropSpec } from "@/lib/record-schema"

import { FileButton } from "./blob-field"
import { InkEditor } from "./ink-editor"
import { InkPageView } from "./ink-page"
import { PdfMarkupSection } from "./pdf-markup"
import { inkSaveFailure, type InkSaveFailure } from "./save-failure"
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
  return (
    <>
      {plan.markup && (
        <PdfMarkupSection
          record={record}
          document={plan.markup.document}
          ink={plan.markup.ink}
          readOnly={readOnly}
        />
      )}
      {plan.ink.map((spec) => (
        <InkPropertySection
          key={spec.name}
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
  if (!blob) return { state: "empty" as const }
  if (bytes.isPending) return { state: "loading" as const, blob }
  if (bytes.isError)
    return {
      state: "error" as const,
      message: `The note didn’t load: ${bytes.error.message}`,
      blob,
      retry: () => void bytes.refetch(),
    }
  const read = inkFromBytes(bytes.data)
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
    { document: InkDocument; base: number } | undefined
  >()
  const patch = useRecordPatch(record, editing?.base)
  const [uploading, setUploading] = useState(false)
  const [failure, setFailure] = useState<InkSaveFailure | undefined>()
  const fileName = inkFileName(recordTitle(record.properties), spec.name)

  /** Write `document` (or the file's own bytes, kept exact) into the
   * property. */
  const save = async (document: InkDocument, bytes?: Blob) => {
    setFailure(undefined)
    setUploading(true)
    try {
      const info = await putBlob(bytes ?? inkText(document), {
        name: fileName,
        mediaType: INK_MEDIA_TYPE,
      })
      await patch.mutateAsync({ [spec.name]: info.digest })
      setEditing(undefined)
    } catch (e) {
      setFailure(inkSaveFailure(e, document))
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
    await save(read.document, file)
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
              {ink.state === "ready" && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => saveBytes(ink.bytes, fileName, INK_MEDIA_TYPE)}
                >
                  <DownloadIcon data-icon="inline-start" />
                  Download .inkd
                </Button>
              )}
              {!readOnly && (
                <ImportButton onFile={importFile} disabled={uploading} />
              )}
              {/* Drawing starts from the stored note, or from blank paper
                  where the record names none: never from blank paper over
                  a note that failed to load, which a save would replace. */}
              {!readOnly &&
                (ink.state === "ready" || ink.state === "empty") && (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      setEditing({
                        document:
                          ink.state === "ready"
                            ? ink.document
                            : newInkDocument({ background: "lined" }),
                        base: record.version,
                      })
                    }
                  >
                    <PenLineIcon data-icon="inline-start" />
                    {ink.state === "ready" ? "Draw" : "Start drawing"}
                  </Button>
                )}
            </>
          )
        }
      />
      {editing ? (
        <InkEditor
          initial={editing.document}
          saving={uploading}
          onSave={(document) => void save(document)}
          onCancel={() => {
            setFailure(undefined)
            setEditing(undefined)
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

function ImportButton({
  onFile,
  disabled,
}: {
  onFile: (file: File) => void
  disabled?: boolean
}) {
  return (
    <FileButton
      accept={`${INK_MEDIA_TYPE},.inkd`}
      onFile={onFile}
      disabled={disabled}
      variant="ghost"
    >
      <UploadIcon data-icon="inline-start" />
      Import .inkd
    </FileButton>
  )
}

function InkPages({ ink }: { ink: ReturnType<typeof useInkValue> }) {
  if (ink.state === "loading")
    return <Skeleton className="aspect-[1240/1754] w-full max-w-3xl" />
  if (ink.state === "empty")
    return (
      <p className="text-sm text-muted-foreground">
        No ink yet. Draw here, or import a .inkd file from Inkidian.
      </p>
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
