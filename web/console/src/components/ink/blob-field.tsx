/** The form control for a file: a `blobref` is uploaded, never typed as a
 * digest, and says what it holds in a line, the file's name and size. A
 * property declared to hold ink also starts a blank `.inkd` note. */

import { useRef, useState } from "react"
import { FileIcon, PenLineIcon, UploadIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { heldBlobInfo, holdBlob, isBlobDigest, putBlob } from "@/lib/api/blobs"
import type { BlobInfo } from "@/lib/api/types"
import {
  INK_EXTENSION,
  INK_MEDIA_TYPE,
  inkText,
  newInkDocument,
  readInk,
} from "@/lib/ink"
import { byteSize, inkFileName } from "@/lib/ink-record"
import { acceptFor, declaredFormat, essence } from "@/lib/media-types"
import type { PropSpec } from "@/lib/record-schema"
import { cn } from "@/lib/utils"

export function FileButton({
  accept,
  onFile,
  disabled,
  variant = "outline",
  children,
}: {
  accept?: string
  onFile: (file: File) => void
  disabled?: boolean
  variant?: "outline" | "ghost"
  children: React.ReactNode
}) {
  const input = useRef<HTMLInputElement>(null)
  return (
    <>
      <input
        ref={input}
        type="file"
        accept={accept}
        className="hidden"
        onChange={(e) => {
          const file = e.target.files?.[0]
          e.target.value = ""
          if (file) onFile(file)
        }}
      />
      <Button
        type="button"
        variant={variant}
        size="sm"
        disabled={disabled}
        onClick={() => input.current?.click()}
      >
        {children}
      </Button>
    </>
  )
}

export function BlobField({
  id,
  spec,
  value,
  onChange,
  invalid,
  fileName,
}: {
  id: string
  spec: PropSpec
  /** The digest the form holds. */
  value: string
  onChange: (digest: string) => void
  invalid?: boolean
  /** The name a blank note is stored under: the record page's rule, the
   * heading else the property's name. */
  fileName?: string
}) {
  const [stored, setStored] = useState<BlobInfo | undefined>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | undefined>()
  // A property declared for ink alone takes only ink; one that also admits
  // other files reads an upload as ink when the file says it is one.
  const inkOnly = declaredFormat(spec) === "inkd"
  const offersInk = (spec.mediaTypes ?? []).some(
    (t) => essence(t) === INK_MEDIA_TYPE
  )

  const store = async (
    bytes: Blob | string,
    meta: { name?: string; mediaType?: string }
  ) => {
    setBusy(true)
    setError(undefined)
    try {
      const info = await putBlob(bytes, meta)
      // Uploaded now, for the name, the size and any refusal; held so the
      // record write stores it again, however long the form stays open.
      holdBlob(info.digest, bytes, meta)
      setStored(info)
      onChange(info.digest)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const upload = async (file: File) => {
    const ink =
      inkOnly ||
      essence(file.type) === INK_MEDIA_TYPE ||
      file.name.toLowerCase().endsWith(INK_EXTENSION)
    if (ink) {
      const read = readInk(await file.text())
      if (!read.ok) {
        setError(read.message)
        return
      }
    }
    // The file's own bytes, unchanged: an Inkidian note round-trips exactly.
    await store(file, {
      name: file.name,
      mediaType: ink ? INK_MEDIA_TYPE : file.type || undefined,
    })
  }

  // A field mounted again (the sheet's panel reopened) has no upload of its
  // own to read; the bytes the form holds still know the name.
  const shown = stored && stored.digest === value ? stored : heldBlobInfo(value)
  return (
    <div id={id} className="flex flex-col gap-1.5" data-slot="blob-field">
      <div
        className={cn(
          "flex min-h-8 items-center gap-2 rounded-md border border-input px-2.5 py-1 text-sm",
          invalid && "border-destructive"
        )}
      >
        <FileIcon className="size-4 shrink-0 text-muted-foreground" />
        {shown ? (
          <span className="min-w-0 truncate">
            {shown.name ?? "file"}{" "}
            <span className="text-muted-foreground">
              {byteSize(shown.size)}
            </span>
          </span>
        ) : isBlobDigest(value) ? (
          <span className="min-w-0 truncate font-mono text-xs text-muted-foreground">
            {value}
          </span>
        ) : (
          <span className="text-muted-foreground">No file yet</span>
        )}
      </div>
      <div className="flex flex-wrap gap-1.5">
        <FileButton accept={acceptFor(spec)} onFile={upload} disabled={busy}>
          <UploadIcon data-icon="inline-start" />
          {busy ? "Uploading…" : inkOnly ? "Import .inkd file" : "Upload file"}
        </FileButton>
        {offersInk && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() =>
              void store(inkText(newInkDocument({ background: "lined" })), {
                name: fileName ?? inkFileName(undefined, spec.name),
                mediaType: INK_MEDIA_TYPE,
              })
            }
          >
            <PenLineIcon data-icon="inline-start" />
            Start a blank note
          </Button>
        )}
      </div>
      {error && <p className="text-xs text-destructive">{error}</p>}
    </div>
  )
}
